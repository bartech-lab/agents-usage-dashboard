package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
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

type Provider interface {
	ID() string
	IsEnabled() bool
	Fetch(client tls_client.HttpClient) (*ProviderData, error)
}

type ZAIProvider struct {
	cfg *ZAIConfig
}

func (p *ZAIProvider) ID() string { return "zai" }

func (p *ZAIProvider) IsEnabled() bool { return p.cfg != nil && p.cfg.Enabled }

func (p *ZAIProvider) Fetch(client tls_client.HttpClient) (*ProviderData, error) {
	if p.cfg == nil {
		return &ProviderData{Status: "offline", Error: "ZAI config missing"}, nil
	}
	return fetchZAI(client, p.cfg.APIKey)
}

type KimiProvider struct {
	cfg *ProviderAuth
}

func (p *KimiProvider) ID() string { return "kimi" }

func (p *KimiProvider) IsEnabled() bool { return p.cfg != nil && p.cfg.Enabled }

func (p *KimiProvider) Fetch(client tls_client.HttpClient) (*ProviderData, error) {
	if p.cfg == nil {
		return &ProviderData{Status: "offline", Error: "Kimi config missing"}, nil
	}
	return fetchKimi(client, *p.cfg)
}

type CodexProvider struct {
	cfg *CodexProviderConfig
}

func (p *CodexProvider) ID() string { return "codex" }

func (p *CodexProvider) IsEnabled() bool { return p.cfg != nil && p.cfg.Enabled }

func (p *CodexProvider) Fetch(client tls_client.HttpClient) (*ProviderData, error) {
	if p.cfg == nil {
		return &ProviderData{Status: "offline", Error: "Codex config missing"}, nil
	}
	if p.cfg.OAuth == nil || p.cfg.OAuth.TokenFile == "" {
		return nil, fmt.Errorf("codex oauth not configured (set codex.oauth.token_file in config.yaml)")
	}
	return fetchCodexViaOAuth(client, p.cfg.OAuth.TokenFile)
}

type ClaudeProvider struct {
	cfg   *ProviderAuth
	orgID *string
}

func (p *ClaudeProvider) ID() string { return "claude" }

func (p *ClaudeProvider) IsEnabled() bool { return p.cfg != nil && p.cfg.Enabled }

func (p *ClaudeProvider) Fetch(client tls_client.HttpClient) (*ProviderData, error) {
	if p.cfg == nil {
		return &ProviderData{Status: "offline", Error: "Claude config missing"}, nil
	}
	data, orgID, err := fetchClaude(client, flattenCookies(p.cfg.Cookies), p.orgID)
	if err == nil && orgID != nil && strings.TrimSpace(*orgID) != "" {
		p.orgID = orgID
	}
	return data, err
}

type OpenCodeGoProvider struct {
	cfg *OpenCodeGoConfig
}

func (p *OpenCodeGoProvider) ID() string { return "opencodego" }

func (p *OpenCodeGoProvider) IsEnabled() bool { return p.cfg != nil && p.cfg.Enabled }

func (p *OpenCodeGoProvider) Fetch(client tls_client.HttpClient) (*ProviderData, error) {
	if p.cfg == nil {
		return &ProviderData{Status: "offline", Error: "OpenCode Go config missing"}, nil
	}
	return fetchOpenCodeGo(client, p.cfg.WorkspaceID, p.cfg.Cookies)
}

func buildProviders(cfg *ProvidersConfig) []Provider {
	if cfg == nil {
		return nil
	}

	return []Provider{
		&ZAIProvider{cfg: &cfg.Zai},
		&KimiProvider{cfg: &cfg.Kimi},
		&CodexProvider{cfg: &cfg.Codex},
		&ClaudeProvider{cfg: &cfg.Claude},
		&OpenCodeGoProvider{cfg: &cfg.OpenCodeGo},
	}
}

// CodexOAuthTokens represents the structure of ~/.codex/auth.json
type CodexOAuthTokens struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// checkTokenExpiration checks if OAuth tokens are potentially expired
func checkTokenExpiration(lastRefresh string) string {
	if lastRefresh == "" {
		return ""
	}

	lastRefreshTime, err := time.Parse(time.RFC3339, lastRefresh)
	if err != nil {
		return ""
	}

	age := time.Since(lastRefreshTime)

	if age > 30*24*time.Hour {
		days := int(age.Hours() / 24)
		return fmt.Sprintf("OAuth tokens are %d days old (may expire soon - run: codex login)", days)
	}

	if age > 7*24*time.Hour {
		days := int(age.Hours() / 24)
		return fmt.Sprintf("OAuth tokens are %d days old", days)
	}

	return ""
}

// fetchCodexViaOAuth fetches usage data using OAuth tokens from Codex CLI
func fetchCodexViaOAuth(client tls_client.HttpClient, tokenFile string) (*ProviderData, error) {
	// Expand environment variables
	tokenFile = strings.TrimSpace(tokenFile)
	if strings.Contains(tokenFile, "${") {
		tokenFile = strings.ReplaceAll(tokenFile, "${HOME}", os.Getenv("HOME"))
		tokenFile = strings.ReplaceAll(tokenFile, "${USER}", os.Getenv("USER"))
	}

	// Read token file
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("codex oauth token file not found: %s (install codex cli and run: codex login)", tokenFile)
		}
		return nil, fmt.Errorf("read codex oauth tokens from %s: %w", tokenFile, err)
	}

	// Parse tokens
	var tokens CodexOAuthTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse codex oauth tokens: %w (ensure ~/.codex/auth.json is valid JSON)", err)
	}

	if tokens.Tokens.AccessToken == "" {
		return nil, fmt.Errorf("codex oauth access_token missing in %s (re-run: codex login)", tokenFile)
	}

	// Check token age
	expirationWarning := checkTokenExpiration(tokens.LastRefresh)

	// Create API request
	req, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("create codex usage request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tokens.Tokens.AccessToken)
	req.Header.Set("User-Agent", "AgentsDashboard/1.0")
	req.Header.Set("Accept", "application/json")

	if tokens.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", tokens.Tokens.AccountID)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch codex usage API: %w", err)
	}
	defer resp.Body.Close()

	// Handle errors
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("codex oauth token expired (status 401 - re-run: codex login)")
	}

	if resp.StatusCode == 403 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("codex usage API forbidden (status 403): %s", string(body))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("codex usage API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
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

	if err := json.NewDecoder(resp.Body).Decode(&usageData); err != nil {
		return nil, fmt.Errorf("decode codex usage response: %w", err)
	}

	// Build result
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
		Credits: usageData.Credits,
	}

	if expirationWarning != "" {
		log.Printf("Codex token warning: %s", expirationWarning)
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

var (
	openCodeGoProgressPattern = regexp.MustCompile(`data-slot="progress-bar"[^>]*style="[^"]*width\s*:\s*([0-9]+(?:\.[0-9]+)?)%`)
	openCodeGoResetPattern    = regexp.MustCompile(`(?s)data-slot="reset-time"[^>]*>(.*?)</span>`)
	openCodeGoTagPattern      = regexp.MustCompile(`<[^>]+>`)
	openCodeGoCommentPattern  = regexp.MustCompile(`<!--.*?-->`)
	openCodeGoDurationPattern = regexp.MustCompile(`(?i)(\d+)\s*(day|days|hour|hours|minute|minutes|min|mins|second|seconds|sec|secs)`)
)

func fetchOpenCodeGo(client tls_client.HttpClient, workspaceID string, cookiesByDomain map[string]map[string]string) (*ProviderData, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return &ProviderData{Status: "offline", Error: "No OpenCode Go workspace_id configured"}, nil
	}

	cookies := map[string]string{}
	for domain, domainCookies := range cookiesByDomain {
		if !strings.Contains(domain, "opencode.ai") {
			continue
		}
		for name, value := range domainCookies {
			if strings.TrimSpace(value) != "" {
				cookies[name] = value
			}
		}
	}

	authCookie := strings.TrimSpace(cookies["auth"])
	if authCookie == "" {
		return &ProviderData{Status: "offline", Error: "No opencode.ai auth cookie found"}, nil
	}

	reqURL := fmt.Sprintf("https://opencode.ai/workspace/%s/go", workspaceID)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create OpenCode Go workspace request: %w", err)
	}

	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Cookie", buildCookieHeader(cookies))
	req.Header.Set("Referer", "https://opencode.ai/workspace/"+workspaceID+"/usage")
	req.Header.Set("Origin", "https://opencode.ai")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch OpenCode Go workspace page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &ProviderData{Status: "error", Error: "OpenCode auth cookie unauthorized"}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("OpenCode Go page request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read OpenCode Go page response: %w", err)
	}
	body := string(bodyBytes)

	progressMatches := openCodeGoProgressPattern.FindAllStringSubmatch(body, -1)
	if len(progressMatches) < 3 {
		return nil, fmt.Errorf("parse OpenCode Go usage: expected 3 usage bars, got %d", len(progressMatches))
	}

	resetMatches := openCodeGoResetPattern.FindAllStringSubmatch(body, -1)

	percentages := make([]float64, 3)
	for i := 0; i < 3; i++ {
		value, err := strconv.ParseFloat(progressMatches[i][1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse OpenCode Go usage percentage %q: %w", progressMatches[i][1], err)
		}
		percentages[i] = value
	}

	remainingSeconds := [3]int{}
	for i := 0; i < len(resetMatches) && i < 3; i++ {
		raw := openCodeGoTagPattern.ReplaceAllString(resetMatches[i][1], " ")
		raw = openCodeGoCommentPattern.ReplaceAllString(raw, " ")
		raw = strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "Resets in"))
		remainingSeconds[i] = parseOpenCodeGoDuration(raw)
	}

	return &ProviderData{
		Status: "ok",
		Plan:   "OpenCode Go",
		Session: &UsageWindow{
			UsagePct:         percentages[0],
			RemainingSeconds: remainingSeconds[0],
		},
		Weekly: &UsageWindow{
			UsagePct:         percentages[1],
			RemainingSeconds: remainingSeconds[1],
		},
		Monthly: &UsageWindow{
			UsagePct:         percentages[2],
			RemainingSeconds: remainingSeconds[2],
		},
	}, nil
}

func parseOpenCodeGoDuration(input string) int {
	if strings.TrimSpace(input) == "" {
		return 0
	}

	matches := openCodeGoDurationPattern.FindAllStringSubmatch(strings.ToLower(input), -1)
	if len(matches) == 0 {
		return 0
	}

	total := 0
	for _, match := range matches {
		count, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch match[2] {
		case "day", "days":
			total += count * 24 * 60 * 60
		case "hour", "hours":
			total += count * 60 * 60
		case "minute", "minutes", "min", "mins":
			total += count * 60
		case "second", "seconds", "sec", "secs":
			total += count
		}
	}

	if total < 0 {
		return 0
	}
	return total
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
