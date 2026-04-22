package main

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newMockHTTPClient(servers map[string]*httptest.Server) *integrationMockHTTPClient {
	return newIntegrationMockHTTPClient(servers)
}

func TestGenerateZAIJWT_ValidIDSecretFormat(t *testing.T) {
	apiKey := "test-id.test-secret"
	tokenString, err := generateZAIJWT(apiKey)
	if err != nil {
		t.Fatalf("generateZAIJWT() unexpected error: %v", err)
	}

	parsed, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("ParseUnverified() error: %v", err)
	}

	if got := parsed.Header["sign_type"]; got != "SIGN" {
		t.Fatalf("header sign_type = %v, want SIGN", got)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type = %T, want jwt.MapClaims", parsed.Claims)
	}

	if got := claims["api_key"]; got != "test-id" {
		t.Fatalf("claim api_key = %v, want test-id", got)
	}

	timestamp, ok := claims["timestamp"].(float64)
	if !ok {
		t.Fatalf("timestamp claim type = %T, want float64", claims["timestamp"])
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("exp claim type = %T, want float64", claims["exp"])
	}

	if timestamp < 1e12 {
		t.Fatalf("timestamp = %v, want millisecond epoch", timestamp)
	}

	if exp < 1e12 {
		t.Fatalf("exp = %v, want millisecond epoch", exp)
	}

	delta := exp - timestamp
	if delta < 3595000 || delta > 3605000 {
		t.Fatalf("exp-timestamp delta = %v, want about 3600000ms", delta)
	}

	nowMillis := float64(time.Now().UnixMilli())
	if timestamp < nowMillis-2000 || timestamp > nowMillis+2000 {
		t.Fatalf("timestamp = %v outside expected now range around %v", timestamp, nowMillis)
	}
}

func TestGenerateZAIJWT_NonIDSecretReturnsAsIs(t *testing.T) {
	rawToken := "raw-token"
	got, err := generateZAIJWT(rawToken)
	if err != nil {
		t.Fatalf("generateZAIJWT() unexpected error: %v", err)
	}

	if got != rawToken {
		t.Fatalf("token = %q, want %q", got, rawToken)
	}

	if strings.Count(got, ".") != 0 {
		t.Fatalf("token = %q appears to be JWT, expected raw token", got)
	}
}

func TestFetchOpenCodeGo(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/workspace/wrk_test/go" {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}

		cookieHeader := r.Header.Get("Cookie")
		if !strings.Contains(cookieHeader, "auth=Fe26.2-test") {
			w.WriteHeader(stdhttp.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<div data-slot="usage">
			<div data-slot="usage-item"><div data-slot="progress"><div data-slot="progress-bar" style="width:12%"></div></div><span data-slot="reset-time">Resets in 3 hours 8 minutes</span></div>
			<div data-slot="usage-item"><div data-slot="progress"><div data-slot="progress-bar" style="width:34.5%"></div></div><span data-slot="reset-time">Resets in 4 days 13 hours</span></div>
			<div data-slot="usage-item"><div data-slot="progress"><div data-slot="progress-bar" style="width:67.8%"></div></div><span data-slot="reset-time">Resets in 29 days 22 hours</span></div>
		</div>`))
	}))
	defer server.Close()

	client := newMockHTTPClient(map[string]*httptest.Server{"opencode.ai": server})
	data, err := fetchOpenCodeGo(client, "wrk_test", map[string]map[string]string{"opencode.ai": {"auth": "Fe26.2-test"}})
	if err != nil {
		t.Fatalf("fetchOpenCodeGo() error: %v", err)
	}

	if data.Status != "ok" {
		t.Fatalf("status = %q, want ok", data.Status)
	}
	if data.Plan != "OpenCode Go" {
		t.Fatalf("plan = %q, want OpenCode Go", data.Plan)
	}
	if data.Session == nil || data.Weekly == nil || data.Monthly == nil {
		t.Fatalf("expected all three usage windows, got session=%v weekly=%v monthly=%v", data.Session, data.Weekly, data.Monthly)
	}
	if data.Monthly.UsagePct != 67.8 {
		t.Fatalf("monthly usage_pct = %v, want 67.8", data.Monthly.UsagePct)
	}
	if data.Session.RemainingSeconds != 11280 {
		t.Fatalf("session remaining_seconds = %d, want 11280", data.Session.RemainingSeconds)
	}
}

func TestFetchOpenCodeGo_MissingWorkspaceID(t *testing.T) {
	data, err := fetchOpenCodeGo(newMockHTTPClient(nil), "", map[string]map[string]string{"opencode.ai": {"auth": "Fe26.2-test"}})
	if err != nil {
		t.Fatalf("fetchOpenCodeGo() error: %v", err)
	}
	if data.Status != "offline" {
		t.Fatalf("status = %q, want offline", data.Status)
	}
}

func TestFetchOpenCodeGo_MissingCookie(t *testing.T) {
	data, err := fetchOpenCodeGo(newMockHTTPClient(nil), "wrk_test", nil)
	if err != nil {
		t.Fatalf("fetchOpenCodeGo() error: %v", err)
	}
	if data.Status != "offline" {
		t.Fatalf("status = %q, want offline", data.Status)
	}
}
