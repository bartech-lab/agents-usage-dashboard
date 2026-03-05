package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/tls-client"
	"github.com/golang-jwt/jwt/v5"
)

type FlexInt int

func (fi *FlexInt) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*fi = FlexInt(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	var parsed int
	if _, err := fmt.Sscanf(s, "%d", &parsed); err != nil {
		return err
	}
	*fi = FlexInt(parsed)
	return nil
}

// Provider represents a data provider for the dashboard
type Provider struct {
	Name string
	Type string
}

func fetchCodex(client tls_client.HttpClient, cookies map[string]string) (*ProviderData, error) {
	cookieHeader := buildCookieHeader(cookies)
	if cookieHeader == "" {
		return nil, fmt.Errorf("codex cookies are required")
	}

	sessionReq, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/api/auth/session", nil)
	if err != nil {
		return nil, fmt.Errorf("create codex session request: %w", err)
	}
	sessionReq.Header.Set("Cookie", cookieHeader)

	sessionResp, err := client.Do(sessionReq)
	if err != nil {
		return nil, fmt.Errorf("fetch codex session: %w", err)
	}
	defer sessionResp.Body.Close()

	if sessionResp.StatusCode < 200 || sessionResp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex session request failed: status %d", sessionResp.StatusCode)
	}

	var sessionData struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(sessionResp.Body).Decode(&sessionData); err != nil {
		return nil, fmt.Errorf("decode codex session response: %w", err)
	}
	if sessionData.AccessToken == "" {
		return nil, fmt.Errorf("codex access token missing from session response")
	}

	usageReq, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("create codex usage request: %w", err)
	}
	usageReq.Header.Set("Authorization", "Bearer "+sessionData.AccessToken)

	usageResp, err := client.Do(usageReq)
	if err != nil {
		return nil, fmt.Errorf("fetch codex usage: %w", err)
	}
	defer usageResp.Body.Close()

	if usageResp.StatusCode < 200 || usageResp.StatusCode >= 300 {
		return nil, fmt.Errorf("codex usage request failed: status %d", usageResp.StatusCode)
	}

	var usageData struct {
		PlanType     string `json:"plan_type"`
		Plan         string `json:"plan"`
		LimitReached bool   `json:"limit_reached"`
		RateLimit    struct {
			LimitReached  bool `json:"limit_reached"`
			PrimaryWindow struct {
				UsedPercent  *float64 `json:"used_percent"`
				UsagePercent *float64 `json:"usage_percent"`
				ResetAt      float64  `json:"reset_at"`
			} `json:"primary_window"`
			SecondaryWindow struct {
				UsedPercent  *float64 `json:"used_percent"`
				UsagePercent *float64 `json:"usage_percent"`
				ResetAt      float64  `json:"reset_at"`
			} `json:"secondary_window"`
		} `json:"rate_limit"`
		Credits *Credits `json:"credits"`
	}
	if err := json.NewDecoder(usageResp.Body).Decode(&usageData); err != nil {
		return nil, fmt.Errorf("decode codex usage response: %w", err)
	}

	plan := usageData.PlanType
	if plan == "" {
		plan = usageData.Plan
	}
	if plan == "" {
		plan = "unknown"
	}

	sessionPct := pickPercent(usageData.RateLimit.PrimaryWindow.UsedPercent, usageData.RateLimit.PrimaryWindow.UsagePercent)
	weeklyPct := pickPercent(usageData.RateLimit.SecondaryWindow.UsedPercent, usageData.RateLimit.SecondaryWindow.UsagePercent)

	sessionRemaining := 0
	if remaining := remainingFromUnix(usageData.RateLimit.PrimaryWindow.ResetAt); remaining != nil {
		sessionRemaining = *remaining
	}

	weeklyRemaining := 0
	if remaining := remainingFromUnix(usageData.RateLimit.SecondaryWindow.ResetAt); remaining != nil {
		weeklyRemaining = *remaining
	}

	result := &ProviderData{
		Status:       "ok",
		Plan:         plan,
		LimitReached: usageData.LimitReached || usageData.RateLimit.LimitReached,
		Session: &UsageWindow{
			UsagePct:         sessionPct,
			ResetAt:          unixToISO(usageData.RateLimit.PrimaryWindow.ResetAt),
			RemainingSeconds: sessionRemaining,
		},
		Weekly: &UsageWindow{
			UsagePct:         weeklyPct,
			ResetAt:          unixToISO(usageData.RateLimit.SecondaryWindow.ResetAt),
			RemainingSeconds: weeklyRemaining,
		},
		Credits:        usageData.Credits,
		DailyBreakdown: []DailyEntry{},
	}

	breakdownReq, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/wham/usage/daily-token-usage-breakdown", nil)
	if err == nil {
		breakdownReq.Header.Set("Authorization", "Bearer "+sessionData.AccessToken)

		breakdownResp, breakdownErr := client.Do(breakdownReq)
		if breakdownErr == nil {
			defer breakdownResp.Body.Close()
			if breakdownResp.StatusCode >= 200 && breakdownResp.StatusCode < 300 {
				var breakdownData struct {
					Data []DailyEntry `json:"data"`
				}
				if decodeErr := json.NewDecoder(breakdownResp.Body).Decode(&breakdownData); decodeErr == nil {
					result.DailyBreakdown = breakdownData.Data
				}
			}
		}
	}

	return result, nil
}

func fetchClaude(client tls_client.HttpClient, cookies map[string]string, orgID *string) (*ProviderData, *string, error) {
	cookieHeader := buildCookieHeader(cookies)
	if cookieHeader == "" {
		return nil, nil, fmt.Errorf("claude cookies are required")
	}

	selectedOrgID := ""
	if orgID != nil {
		selectedOrgID = strings.TrimSpace(*orgID)
	}

	headers := map[string]string{
		"Cookie":                    cookieHeader,
		"anthropic-client-platform": "web_claude.ai",
		"anthropic-client-version":  "1.0.0",
	}

	if selectedOrgID == "" {
		orgReq, err := http.NewRequest(http.MethodGet, "https://claude.ai/api/organizations", nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create claude organizations request: %w", err)
		}
		for key, value := range headers {
			orgReq.Header.Set(key, value)
		}

		orgResp, err := client.Do(orgReq)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch claude organizations: %w", err)
		}
		defer orgResp.Body.Close()

		if orgResp.StatusCode < 200 || orgResp.StatusCode >= 300 {
			return nil, nil, fmt.Errorf("claude organizations request failed: status %d", orgResp.StatusCode)
		}

		var rawOrgs interface{}
		if err := json.NewDecoder(orgResp.Body).Decode(&rawOrgs); err != nil {
			return nil, nil, fmt.Errorf("decode claude organizations response: %w", err)
		}

		orgs, ok := rawOrgs.([]interface{})
		if !ok || len(orgs) == 0 {
			return nil, nil, fmt.Errorf("claude organizations response missing organizations")
		}

		fallbackUUID := ""
		if firstOrg, ok := orgs[0].(map[string]interface{}); ok {
			fallbackUUID, _ = firstOrg["uuid"].(string)
		}

		for _, org := range orgs {
			orgMap, ok := org.(map[string]interface{})
			if !ok {
				continue
			}

			uuid, _ := orgMap["uuid"].(string)
			caps, ok := orgMap["capabilities"].([]interface{})
			if !ok {
				continue
			}

			for _, cap := range caps {
				if capStr, ok := cap.(string); ok && capStr == "chat" && uuid != "" {
					selectedOrgID = uuid
					break
				}
			}
			if selectedOrgID != "" {
				break
			}
		}

		if selectedOrgID == "" {
			selectedOrgID = fallbackUUID
		}
		if selectedOrgID == "" {
			return nil, nil, fmt.Errorf("claude organization uuid not found")
		}
	}

	usageReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://claude.ai/api/organizations/%s/usage", selectedOrgID), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create claude usage request: %w", err)
	}
	for key, value := range headers {
		usageReq.Header.Set(key, value)
	}

	usageResp, err := client.Do(usageReq)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch claude usage: %w", err)
	}
	defer usageResp.Body.Close()

	if usageResp.StatusCode < 200 || usageResp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("claude usage request failed: status %d", usageResp.StatusCode)
	}

	var usageRaw map[string]interface{}
	if err := json.NewDecoder(usageResp.Body).Decode(&usageRaw); err != nil {
		return nil, nil, fmt.Errorf("decode claude usage response: %w", err)
	}

	mapFromAny := func(value interface{}) map[string]interface{} {
		m, ok := value.(map[string]interface{})
		if !ok {
			return map[string]interface{}{}
		}
		return m
	}

	floatFromAny := func(value interface{}) float64 {
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		default:
			return 0
		}
	}

	stringFromAny := func(value interface{}) string {
		s, ok := value.(string)
		if !ok {
			return ""
		}
		return s
	}

	fiveHour := mapFromAny(usageRaw["five_hour"])
	sevenDay := mapFromAny(usageRaw["seven_day"])
	sevenDaySonnet := mapFromAny(usageRaw["seven_day_sonnet"])
	sevenDayOpus := mapFromAny(usageRaw["seven_day_opus"])

	sessionResetAt := stringFromAny(fiveHour["resets_at"])
	weeklyResetAt := stringFromAny(sevenDay["resets_at"])

	sessionRemaining := 0
	if remaining := remainingFromISO(sessionResetAt); remaining != nil {
		sessionRemaining = *remaining
	}

	weeklyRemaining := 0
	if remaining := remainingFromISO(weeklyResetAt); remaining != nil {
		weeklyRemaining = *remaining
	}

	data := &ProviderData{
		Status: "ok",
		Plan:   "Pro",
		Session: &UsageWindow{
			UsagePct:         floatFromAny(fiveHour["utilization"]),
			ResetAt:          sessionResetAt,
			RemainingSeconds: sessionRemaining,
		},
		Weekly: &UsageWindow{
			UsagePct:         floatFromAny(sevenDay["utilization"]),
			ResetAt:          weeklyResetAt,
			RemainingSeconds: weeklyRemaining,
		},
		Models: &ClaudeModels{
			Sonnet: floatFromAny(sevenDaySonnet["utilization"]),
			Opus:   floatFromAny(sevenDayOpus["utilization"]),
		},
	}

	resolvedOrgID := selectedOrgID
	return data, &resolvedOrgID, nil
}

func generateZAIJWT(apiKey string) (string, error) {
	parts := strings.SplitN(apiKey, ".", 2)
	if len(parts) != 2 {
		return apiKey, nil
	}

	kid, secret := parts[0], parts[1]
	now := time.Now().UnixMilli()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"api_key":   kid,
		"exp":       now + 3600*1000,
		"timestamp": now,
	})
	token.Header["sign_type"] = "SIGN"

	signedToken, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign zai jwt: %w", err)
	}

	return signedToken, nil
}

func fetchZAI(client tls_client.HttpClient, apiKey string) (*ProviderData, error) {
	if apiKey == "" {
		return &ProviderData{Status: "offline", Error: "No ZAI_API_KEY configured"}, nil
	}

	token, err := generateZAIJWT(apiKey)
	if err != nil {
		return nil, fmt.Errorf("generate zai jwt: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.z.ai/api/monitor/usage/quota/limit", nil)
	if err != nil {
		return nil, fmt.Errorf("create zai usage request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://chat.z.ai")
	req.Header.Set("Referer", "https://chat.z.ai/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch zai usage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("zai usage request failed: status %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Level  string `json:"level"`
			Limits []struct {
				Percentage    float64  `json:"percentage"`
				Type          string   `json:"type"`
				Unit          int      `json:"unit"`
				NextResetTime *float64 `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode zai usage response: %w", err)
	}

	plan := payload.Data.Level
	if plan == "" {
		plan = "unknown"
	}

	sessionPct := 0.0
	weeklyPct := 0.0
	sessionRemaining := 0
	weeklyRemaining := 0

	for _, limit := range payload.Data.Limits {
		remaining := 0
		if limit.NextResetTime != nil {
			remaining = int(time.Until(time.UnixMilli(int64(*limit.NextResetTime))).Seconds())
			if remaining < 0 {
				remaining = 0
			}
		}

		switch {
		case limit.Type == "TOKENS_LIMIT" && limit.Unit == 6:
			weeklyPct = limit.Percentage
			weeklyRemaining = remaining
		case limit.Type == "TOKENS_LIMIT" && limit.Unit == 3:
			sessionPct = limit.Percentage
			sessionRemaining = remaining
		case limit.Type == "TIME_LIMIT":
			if sessionPct == 0 {
				sessionPct = limit.Percentage
				sessionRemaining = remaining
			}
		}
	}

	return &ProviderData{
		Status: "ok",
		Plan:   plan,
		Session: &UsageWindow{
			UsagePct:         sessionPct,
			RemainingSeconds: sessionRemaining,
		},
		Weekly: &UsageWindow{
			UsagePct:         weeklyPct,
			RemainingSeconds: weeklyRemaining,
		},
	}, nil
}

func pickPercent(usedPercent *float64, usagePercent *float64) float64 {
	if usedPercent != nil {
		return *usedPercent
	}
	if usagePercent != nil {
		return *usagePercent
	}
	return 0
}

func buildCookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}

	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)

	pairs := make([]string, 0, len(cookies))
	for _, name := range names {
		pairs = append(pairs, fmt.Sprintf("%s=%s", name, cookies[name]))
	}

	return strings.Join(pairs, "; ")
}

func fetchKimi(client tls_client.HttpClient, auth ProviderAuth) (*ProviderData, error) {
	token := ""
	for domain, cookies := range auth.Cookies {
		if !strings.Contains(domain, "kimi.com") {
			continue
		}
		if value := cookies["kimi-auth"]; value != "" {
			token = value
			break
		}
	}

	if token == "" {
		return &ProviderData{Status: "offline", Error: "No kimi-auth cookie found"}, nil
	}

	headers := http.Header{
		"Authorization":            {"Bearer " + token},
		"Cookie":                   {"kimi-auth=" + token},
		"Content-Type":             {"application/json"},
		"Origin":                   {"https://www.kimi.com"},
		"Referer":                  {"https://www.kimi.com/code/console"},
		"Accept":                   {"*/*"},
		"connect-protocol-version": {"1"},
		"x-msh-platform":           {"web"},
		"x-language":               {"en-US"},
	}

	usagePayload := []byte(`{"scope":["FEATURE_CODING"]}`)
	usageReq, err := http.NewRequest(http.MethodPost, "https://www.kimi.com/apiv2/kimi.gateway.billing.v1.BillingService/GetUsages", bytes.NewReader(usagePayload))
	if err != nil {
		return nil, fmt.Errorf("create kimi usages request: %w", err)
	}
	usageReq.Header = headers.Clone()

	usageResp, err := client.Do(usageReq)
	if err != nil {
		return nil, fmt.Errorf("fetch kimi usages: %w", err)
	}
	defer usageResp.Body.Close()

	if usageResp.StatusCode < 200 || usageResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(usageResp.Body, 512))
		return &ProviderData{Status: "error", Error: fmt.Sprintf("GetUsages HTTP %d: %s", usageResp.StatusCode, strings.TrimSpace(string(body)))}, nil
	}

	var usageData struct {
		Usages []struct {
			Detail struct {
				Percent   float64 `json:"percent"`
				Used      FlexInt `json:"used"`
				Limit     FlexInt `json:"limit"`
				Remaining FlexInt `json:"remaining"`
				ResetTime string  `json:"resetTime"`
			} `json:"detail"`
			Limits []struct {
				Detail struct {
					Percent   float64 `json:"percent"`
					Used      FlexInt `json:"used"`
					Limit     FlexInt `json:"limit"`
					Remaining FlexInt `json:"remaining"`
					ResetTime string  `json:"resetTime"`
				} `json:"detail"`
			} `json:"limits"`
		} `json:"usages"`
	}

	if err := json.NewDecoder(usageResp.Body).Decode(&usageData); err != nil {
		return nil, fmt.Errorf("decode kimi usages response: %w", err)
	}
	if len(usageData.Usages) == 0 {
		return &ProviderData{Status: "error", Error: "Empty usages response"}, nil
	}

	weeklyDetail := usageData.Usages[0].Detail
	weeklyRemaining := 0
	if remaining := remainingFromISO(weeklyDetail.ResetTime); remaining != nil {
		weeklyRemaining = *remaining
	}

	// Calculate weekly percentage like Python: round((used / limit) * 100, 1)
	weeklyPct := 0.0
	if weeklyDetail.Limit > 0 {
		weeklyPct = math.Round(float64(weeklyDetail.Used)/float64(weeklyDetail.Limit)*100*10) / 10
	}

	result := &ProviderData{
		Status: "ok",
		Plan:   "unknown",
		Weekly: &UsageWindow{
			UsagePct:         weeklyPct,
			ResetAt:          weeklyDetail.ResetTime,
			RemainingSeconds: weeklyRemaining,
			Used:             int(weeklyDetail.Used),
			Limit:            int(weeklyDetail.Limit),
			Remaining:        int(weeklyDetail.Remaining),
		},
	}

	if len(usageData.Usages[0].Limits) > 0 {
		sessionDetail := usageData.Usages[0].Limits[0].Detail
		sessionRemaining := 0
		if remaining := remainingFromISO(sessionDetail.ResetTime); remaining != nil {
			sessionRemaining = *remaining
		}

		// Calculate session percentage like Python: round((used / limit) * 100, 1)
		// Note: Kimi API returns "remaining" not "used" for rate limits
		sessionPct := 0.0
		if sessionDetail.Limit > 0 {
			sessionUsed := sessionDetail.Limit - sessionDetail.Remaining
			sessionPct = math.Round(float64(sessionUsed)/float64(sessionDetail.Limit)*100*10) / 10
		}

		result.Session = &UsageWindow{
			UsagePct:         sessionPct,
			ResetAt:          sessionDetail.ResetTime,
			RemainingSeconds: sessionRemaining,
			Used:             int(sessionDetail.Limit - sessionDetail.Remaining),
			Limit:            int(sessionDetail.Limit),
			Remaining:        int(sessionDetail.Remaining),
		}
	}

	subReq, err := http.NewRequest(http.MethodPost, "https://www.kimi.com/apiv2/kimi.gateway.order.v1.SubscriptionService/GetSubscription", bytes.NewReader([]byte("{}")))
	if err != nil {
		return result, nil
	}
	subReq.Header = headers.Clone()

	subResp, err := client.Do(subReq)
	if err != nil {
		return result, nil
	}
	defer subResp.Body.Close()

	if subResp.StatusCode >= 200 && subResp.StatusCode < 300 {
		var subData struct {
			Subscription struct {
				Goods struct {
					Title string `json:"title"`
				} `json:"goods"`
			} `json:"subscription"`
		}
		if err := json.NewDecoder(subResp.Body).Decode(&subData); err == nil && subData.Subscription.Goods.Title != "" {
			result.Plan = subData.Subscription.Goods.Title
		}
	}

	return result, nil
}
