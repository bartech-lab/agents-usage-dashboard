package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/bandwidth"
	"golang.org/x/net/proxy"
)

type integrationNoopBandwidthTracker struct{}

func (n *integrationNoopBandwidthTracker) Reset() {}

func (n *integrationNoopBandwidthTracker) GetTotalBandwidth() int64 { return 0 }

func (n *integrationNoopBandwidthTracker) GetWriteBytes() int64 { return 0 }

func (n *integrationNoopBandwidthTracker) GetReadBytes() int64 { return 0 }

func (n *integrationNoopBandwidthTracker) TrackConnection(_ context.Context, conn net.Conn) net.Conn {
	return conn
}

type integrationMockHTTPClient struct {
	servers        map[string]*httptest.Server
	jar            fhttp.CookieJar
	proxyURL       string
	followRedirect bool

	bandwidth bandwidth.BandwidthTracker
}

func newIntegrationMockHTTPClient(servers map[string]*httptest.Server) *integrationMockHTTPClient {
	return &integrationMockHTTPClient{
		servers:        servers,
		jar:            newIntegrationMemoryJar(),
		followRedirect: true,
		bandwidth:      &integrationNoopBandwidthTracker{},
	}
}

type integrationMemoryJar struct {
	byHost map[string][]*fhttp.Cookie
}

func newIntegrationMemoryJar() *integrationMemoryJar {
	return &integrationMemoryJar{byHost: map[string][]*fhttp.Cookie{}}
}

func (m *integrationMemoryJar) SetCookies(u *url.URL, cookies []*fhttp.Cookie) {
	if u == nil {
		return
	}

	host := u.Hostname()
	stored := make([]*fhttp.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		clone := *cookie
		stored = append(stored, &clone)
	}
	m.byHost[host] = stored
}

func (m *integrationMemoryJar) Cookies(u *url.URL) []*fhttp.Cookie {
	if u == nil {
		return nil
	}

	host := u.Hostname()
	stored := m.byHost[host]
	result := make([]*fhttp.Cookie, 0, len(stored))
	for _, cookie := range stored {
		if cookie == nil {
			continue
		}
		clone := *cookie
		result = append(result, &clone)
	}
	return result
}

func (m *integrationMockHTTPClient) Do(req *fhttp.Request) (*fhttp.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("request URL is required")
	}

	host := req.URL.Hostname()
	server, ok := m.servers[host]
	if !ok {
		return nil, fmt.Errorf("no mock server registered for host %q", host)
	}

	var bodyBytes []byte
	if req.Body != nil {
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		bodyBytes = payload
	}

	targetURL := server.URL + req.URL.RequestURI()
	stdReq, err := stdhttp.NewRequest(req.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	for key, values := range req.Header {
		for _, value := range values {
			stdReq.Header.Add(key, value)
		}
	}

	if req.Host != "" {
		stdReq.Host = req.Host
	}

	stdResp, err := server.Client().Do(stdReq)
	if err != nil {
		return nil, err
	}

	resp := &fhttp.Response{
		Status:        stdResp.Status,
		StatusCode:    stdResp.StatusCode,
		Header:        fhttp.Header{},
		Body:          stdResp.Body,
		ContentLength: stdResp.ContentLength,
		Request:       req,
	}

	for key, values := range stdResp.Header {
		copied := make([]string, len(values))
		copy(copied, values)
		resp.Header[key] = copied
	}

	return resp, nil
}

func (m *integrationMockHTTPClient) Get(target string) (*fhttp.Response, error) {
	req, err := fhttp.NewRequest(fhttp.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	return m.Do(req)
}

func (m *integrationMockHTTPClient) Head(target string) (*fhttp.Response, error) {
	req, err := fhttp.NewRequest(fhttp.MethodHead, target, nil)
	if err != nil {
		return nil, err
	}
	return m.Do(req)
}

func (m *integrationMockHTTPClient) Post(target string, contentType string, body io.Reader) (*fhttp.Response, error) {
	req, err := fhttp.NewRequest(fhttp.MethodPost, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return m.Do(req)
}

func (m *integrationMockHTTPClient) GetCookies(u *url.URL) []*fhttp.Cookie {
	if m.jar == nil {
		return nil
	}
	return m.jar.Cookies(u)
}

func (m *integrationMockHTTPClient) SetCookies(u *url.URL, cookies []*fhttp.Cookie) {
	if m.jar == nil {
		return
	}
	m.jar.SetCookies(u, cookies)
}

func (m *integrationMockHTTPClient) SetCookieJar(jar fhttp.CookieJar) {
	m.jar = jar
}

func (m *integrationMockHTTPClient) GetCookieJar() fhttp.CookieJar {
	return m.jar
}

func (m *integrationMockHTTPClient) SetProxy(proxyURL string) error {
	m.proxyURL = proxyURL
	return nil
}

func (m *integrationMockHTTPClient) GetProxy() string {
	return m.proxyURL
}

func (m *integrationMockHTTPClient) SetFollowRedirect(followRedirect bool) {
	m.followRedirect = followRedirect
}

func (m *integrationMockHTTPClient) GetFollowRedirect() bool {
	return m.followRedirect
}

func (m *integrationMockHTTPClient) CloseIdleConnections() {}

func (m *integrationMockHTTPClient) GetBandwidthTracker() bandwidth.BandwidthTracker {
	return m.bandwidth
}

func (m *integrationMockHTTPClient) GetDialer() proxy.ContextDialer {
	return nil
}

func (m *integrationMockHTTPClient) GetTLSDialer() tls_client.TLSDialerFunc {
	return nil
}

func TestFetchKimiIntegration(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/apiv2/kimi.gateway.billing.v1.BillingService/GetUsages":
			json.NewEncoder(w).Encode(map[string]any{
				"usages": []map[string]any{{
					"detail": map[string]any{
						"percent":   40.0,
						"used":      400,
						"limit":     1000,
						"remaining": 600,
						"resetTime": time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
					},
					"limits": []map[string]any{{
						"detail": map[string]any{
							"percent":   10.0,
							"used":      10,
							"limit":     100,
							"remaining": 90,
							"resetTime": time.Now().Add(4 * time.Hour).UTC().Format(time.RFC3339),
						},
					}},
				}},
			})
		case "/apiv2/kimi.gateway.order.v1.SubscriptionService/GetSubscription":
			json.NewEncoder(w).Encode(map[string]any{
				"subscription": map[string]any{
					"goods": map[string]any{"title": "Kimi Pro"},
				},
			})
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newIntegrationMockHTTPClient(map[string]*httptest.Server{"www.kimi.com": server})
	auth := ProviderAuth{Cookies: map[string]map[string]string{"www.kimi.com": {"kimi-auth": "token"}}}

	data, err := fetchKimi(client, auth)
	if err != nil {
		t.Fatalf("fetchKimi returned error: %v", err)
	}

	if data.Status != "ok" {
		t.Fatalf("expected status ok, got %q", data.Status)
	}
	if data.Plan != "Kimi Pro" {
		t.Fatalf("expected Kimi Pro plan, got %q", data.Plan)
	}
	if data.Session == nil || data.Weekly == nil {
		t.Fatalf("expected session and weekly windows to be set")
	}
}

func TestFetchClaudeIntegration(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/api/organizations":
			json.NewEncoder(w).Encode([]map[string]any{{
				"uuid":         "org-123",
				"capabilities": []string{"chat"},
			}})
		case "/api/organizations/org-123/usage":
			json.NewEncoder(w).Encode(map[string]any{
				"five_hour": map[string]any{
					"utilization": 20.0,
					"resets_at":   time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
				},
				"seven_day": map[string]any{
					"utilization": 30.0,
					"resets_at":   time.Now().Add(6 * 24 * time.Hour).UTC().Format(time.RFC3339),
				},
				"seven_day_sonnet": map[string]any{"utilization": 18.0},
				"seven_day_opus":   map[string]any{"utilization": 2.0},
			})
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newIntegrationMockHTTPClient(map[string]*httptest.Server{"claude.ai": server})
	data, orgID, err := fetchClaude(client, map[string]string{"sessionKey": "cookie"}, nil)
	if err != nil {
		t.Fatalf("fetchClaude returned error: %v", err)
	}

	if orgID == nil || *orgID != "org-123" {
		t.Fatalf("expected resolved org org-123, got %#v", orgID)
	}
	if data.Status != "ok" {
		t.Fatalf("expected status ok, got %q", data.Status)
	}
	if data.Models == nil || data.Models.Sonnet != 18.0 {
		t.Fatalf("expected claude model usage to be parsed")
	}
}

func TestFetchZAIIntegration(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}

		now := float64(time.Now().Add(2 * time.Hour).UnixMilli())
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"level": "pro",
				"limits": []map[string]any{
					{"percentage": 12.5, "type": "TOKENS_LIMIT", "unit": 3, "nextResetTime": now},
					{"percentage": 61.0, "type": "TOKENS_LIMIT", "unit": 6, "nextResetTime": now},
				},
			},
		})
	}))
	defer server.Close()

	client := newIntegrationMockHTTPClient(map[string]*httptest.Server{"api.z.ai": server})
	data, err := fetchZAI(client, "kid.secret")
	if err != nil {
		t.Fatalf("fetchZAI returned error: %v", err)
	}

	if data.Status != "ok" {
		t.Fatalf("expected status ok, got %q", data.Status)
	}
	if data.Plan != "pro" {
		t.Fatalf("expected plan pro, got %q", data.Plan)
	}
	if data.Session == nil || data.Weekly == nil {
		t.Fatalf("expected session and weekly windows to be set")
	}
}

func TestProviderFetches_ErrorResponses(t *testing.T) {
	chatgpt := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/api/auth/session" {
			w.WriteHeader(stdhttp.StatusUnauthorized)
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	defer chatgpt.Close()

	kimi := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if strings.Contains(r.URL.Path, "GetUsages") {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			w.Write([]byte("boom"))
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	defer kimi.Close()

	claude := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/api/organizations" {
			w.WriteHeader(stdhttp.StatusBadGateway)
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	defer claude.Close()

	zai := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/api/monitor/usage/quota/limit" {
			w.WriteHeader(stdhttp.StatusTooManyRequests)
			return
		}
		w.WriteHeader(stdhttp.StatusNotFound)
	}))
	defer zai.Close()

	kimiClient := newIntegrationMockHTTPClient(map[string]*httptest.Server{"www.kimi.com": kimi})
	kimiData, kimiErr := fetchKimi(kimiClient, ProviderAuth{Cookies: map[string]map[string]string{"www.kimi.com": {"kimi-auth": "token"}}})
	if kimiErr != nil {
		t.Fatalf("expected fetchKimi no transport error, got %v", kimiErr)
	}
	if kimiData.Status != "error" {
		t.Fatalf("expected fetchKimi status error, got %q", kimiData.Status)
	}

	claudeClient := newIntegrationMockHTTPClient(map[string]*httptest.Server{"claude.ai": claude})
	if _, _, err := fetchClaude(claudeClient, map[string]string{"sessionKey": "cookie"}, nil); err == nil {
		t.Fatalf("expected fetchClaude to return an error")
	}

	zaiClient := newIntegrationMockHTTPClient(map[string]*httptest.Server{"api.z.ai": zai})
	if _, err := fetchZAI(zaiClient, "kid.secret"); err == nil {
		t.Fatalf("expected fetchZAI to return an error")
	}
}

func TestSchedulerFetchAll_Integration(t *testing.T) {
	providers := newProviderTestServers(false)
	defer providers.close()

	tokenFile := writeIntegrationCodexTokenFile(t)
	cfg := newIntegrationConfigWithTestingTokenFile(tokenFile)
	scheduler := NewScheduler(cfg.RefreshInterval, buildProviders(&cfg.Providers), newIntegrationMockHTTPClient(providers.servers()))
	scheduler.fetchAll()

	cache := scheduler.GetCache()
	if cache.Codex.Status != "ok" || cache.Kimi.Status != "ok" || cache.Claude.Status != "ok" || cache.Zai.Status != "ok" {
		t.Fatalf("expected all providers to be ok, got zai=%q kimi=%q codex=%q claude=%q", cache.Zai.Status, cache.Kimi.Status, cache.Codex.Status, cache.Claude.Status)
	}
	if cache.LastFetch == "" || cache.NextRefreshAt == "" {
		t.Fatalf("expected cache timestamps to be set")
	}
}

func TestSchedulerFetchAll_StaleDataPreserved(t *testing.T) {
	providers := newProviderTestServers(false)
	defer providers.close()

	client := newIntegrationMockHTTPClient(providers.servers())
	tokenFile := writeIntegrationCodexTokenFile(t)
	cfg := newIntegrationConfigWithTestingTokenFile(tokenFile)
	scheduler := NewScheduler(cfg.RefreshInterval, buildProviders(&cfg.Providers), client)

	scheduler.fetchAll()
	first := scheduler.GetCache()
	if first.Codex.Status != "ok" {
		t.Fatalf("expected initial codex status ok, got %q", first.Codex.Status)
	}
	if first.Codex.LastSuccess == "" {
		t.Fatalf("expected initial codex last_success to be populated")
	}

	providers.failCodex = true
	scheduler.fetchAll()
	second := scheduler.GetCache()

	if second.Codex.Status != "stale" {
		t.Fatalf("expected codex status stale after failure, got %q", second.Codex.Status)
	}
	if second.Codex.Error == "" {
		t.Fatalf("expected codex stale error to be populated")
	}
	if second.Codex.LastSuccess != first.Codex.LastSuccess {
		t.Fatalf("expected last_success to be preserved")
	}
	if second.Codex.Session == nil || first.Codex.Session == nil || second.Codex.Session.UsagePct != first.Codex.Session.UsagePct {
		t.Fatalf("expected stale cache to preserve previous session usage")
	}
}

func newIntegrationConfig() *Config {
	return newIntegrationConfigWithTestingTokenFile("")
}

func newIntegrationConfigWithTestingTokenFile(tokenFile string) *Config {
	return &Config{
		RefreshInterval: 2 * time.Minute,
		Providers: ProvidersConfig{
			Codex: CodexProviderConfig{
				Enabled: true,
				OAuth:   &OAuthConfig{TokenFile: tokenFile},
			},
			Kimi:   ProviderAuth{Enabled: true, Cookies: map[string]map[string]string{"www.kimi.com": {"kimi-auth": "token"}}},
			Claude: ProviderAuth{Enabled: true, Cookies: map[string]map[string]string{"claude.ai": {"sessionKey": "cookie"}}},
			Zai:    ZAIConfig{Enabled: true, APIKey: "kid.secret"},
		},
	}
}

func writeIntegrationCodexTokenFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-auth.json")
	content := `{"tokens":{"access_token":"test-token","account_id":"test-account"},"last_refresh":"2026-03-20T08:00:00Z"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write integration codex token file: %v", err)
	}

	return path
}

type providerTestServers struct {
	chatgpt   *httptest.Server
	kimi      *httptest.Server
	claude    *httptest.Server
	zai       *httptest.Server
	failCodex bool
}

func newProviderTestServers(failCodex bool) *providerTestServers {
	state := &providerTestServers{failCodex: failCodex}

	state.chatgpt = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/api/auth/session":
			json.NewEncoder(w).Encode(map[string]string{"accessToken": "test-token"})
		case "/backend-api/wham/usage":
			if state.failCodex {
				w.WriteHeader(stdhttp.StatusServiceUnavailable)
				w.Write([]byte("temporarily unavailable"))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"plan_type": "plus",
				"rate_limit": map[string]any{
					"primary_window": map[string]any{
						"used_percent": 45.0,
						"reset_at":     float64(time.Now().Add(20 * time.Minute).Unix()),
					},
					"secondary_window": map[string]any{
						"used_percent": 35.0,
						"reset_at":     float64(time.Now().Add(24 * time.Hour).Unix()),
					},
				},
			})
		case "/backend-api/wham/usage/daily-token-usage-breakdown":
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"day": "2026-03-02", "value": 95.0}},
			})
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))

	state.kimi = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/apiv2/kimi.gateway.billing.v1.BillingService/GetUsages":
			json.NewEncoder(w).Encode(map[string]any{
				"usages": []map[string]any{{
					"detail": map[string]any{
						"percent":   32.0,
						"used":      320,
						"limit":     1000,
						"remaining": 680,
						"resetTime": time.Now().Add(5 * 24 * time.Hour).UTC().Format(time.RFC3339),
					},
					"limits": []map[string]any{{
						"detail": map[string]any{
							"percent":   22.0,
							"used":      22,
							"limit":     100,
							"remaining": 78,
							"resetTime": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339),
						},
					}},
				}},
			})
		case "/apiv2/kimi.gateway.order.v1.SubscriptionService/GetSubscription":
			json.NewEncoder(w).Encode(map[string]any{
				"subscription": map[string]any{"goods": map[string]any{"title": "Kimi Pro"}},
			})
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))

	state.claude = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/api/organizations":
			json.NewEncoder(w).Encode([]map[string]any{{
				"uuid":         "org-abc",
				"capabilities": []string{"chat"},
			}})
		case "/api/organizations/org-abc/usage":
			json.NewEncoder(w).Encode(map[string]any{
				"five_hour":        map[string]any{"utilization": 11.0, "resets_at": time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)},
				"seven_day":        map[string]any{"utilization": 28.0, "resets_at": time.Now().Add(4 * 24 * time.Hour).UTC().Format(time.RFC3339)},
				"seven_day_sonnet": map[string]any{"utilization": 9.0},
				"seven_day_opus":   map[string]any{"utilization": 1.0},
			})
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))

	state.zai = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}

		reset := float64(time.Now().Add(90 * time.Minute).UnixMilli())
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"level": "pro",
				"limits": []map[string]any{
					{"percentage": 6.0, "type": "TOKENS_LIMIT", "unit": 3, "nextResetTime": reset},
					{"percentage": 55.0, "type": "TOKENS_LIMIT", "unit": 6, "nextResetTime": reset},
				},
			},
		})
	}))

	return state
}

func (p *providerTestServers) servers() map[string]*httptest.Server {
	return map[string]*httptest.Server{
		"chatgpt.com":  p.chatgpt,
		"www.kimi.com": p.kimi,
		"claude.ai":    p.claude,
		"api.z.ai":     p.zai,
	}
}

func (p *providerTestServers) close() {
	p.chatgpt.Close()
	p.kimi.Close()
	p.claude.Close()
	p.zai.Close()
}
