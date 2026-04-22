package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	return path
}

func TestLoadConfig_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, `
refresh_interval: 30s
server_port: 9000
providers:
  zai:
    api_key: test-id.test-secret
`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if config.RefreshInterval != 30*time.Second {
		t.Fatalf("RefreshInterval = %v, want %v", config.RefreshInterval, 30*time.Second)
	}

	if config.ServerPort != 9000 {
		t.Fatalf("ServerPort = %d, want 9000", config.ServerPort)
	}

	if config.Providers.Zai.APIKey != "test-id.test-secret" {
		t.Fatalf("Providers.Zai.APIKey = %q, want test-id.test-secret", config.Providers.Zai.APIKey)
	}
}

func TestLoadConfig_InterpolatesEnvironmentVariables(t *testing.T) {
	t.Setenv("TEST_ZAI_API_KEY", "env-id.env-secret")

	path := writeTempConfig(t, `
providers:
  zai:
    api_key: ${TEST_ZAI_API_KEY}
`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if config.Providers.Zai.APIKey != "env-id.env-secret" {
		t.Fatalf("Providers.Zai.APIKey = %q, want env-id.env-secret", config.Providers.Zai.APIKey)
	}
}

func TestLoadConfig_AppliesDefaults(t *testing.T) {
	path := writeTempConfig(t, `
providers:
  zai:
    api_key: default-id.default-secret
`)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if config.RefreshInterval != DefaultRefreshInterval {
		t.Fatalf("RefreshInterval = %v, want default %v", config.RefreshInterval, DefaultRefreshInterval)
	}

	if config.ServerPort != DefaultServerPort {
		t.Fatalf("ServerPort = %d, want default %d", config.ServerPort, DefaultServerPort)
	}
}

func TestLoadConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErrPart string
	}{
		{
			name: "no providers configured",
			content: `
refresh_interval: 30s
server_port: 9000
`,
			wantErrPart: "at least one provider must be configured",
		},
		{
			name: "invalid port",
			content: `
server_port: 70000
providers:
  zai:
    api_key: test-id.test-secret
`,
			wantErrPart: "server_port must be between 1 and 65535",
		},
		{
			name: "invalid interval",
			content: `
refresh_interval: -1s
providers:
  zai:
    api_key: test-id.test-secret
`,
			wantErrPart: "refresh_interval must be positive",
		},
		{
			name: "opencodego missing auth cookie",
			content: `
providers:
  opencodego:
    workspace_id: wrk_test
    cookies:
      "opencode.ai":
        "other": "value"
`,
			wantErrPart: "opencodego requires an auth cookie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.content)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("LoadConfig() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("LoadConfig() error = %q, want to contain %q", err.Error(), tt.wantErrPart)
			}
		})
	}
}

func TestLoadConfig_ZAIIsOptional(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "kimi only",
			content: `
providers:
  kimi:
    cookies:
      "kimi.com":
        "kimi-auth": "test-token"
`,
		},
		{
			name: "claude only",
			content: `
providers:
  claude:
    cookies:
      "claude.ai":
        "sessionKey": "test-session"
`,
		},
		{
			name: "codex only",
			content: `
providers:
  codex:
    oauth:
      token_file: /tmp/test-codex-auth.json
`,
		},
		{
			name: "zai only",
			content: `
providers:
  zai:
    api_key: test-id.test-secret
`,
		},
		{
			name: "opencodego only",
			content: `
providers:
  opencodego:
    workspace_id: wrk_test
    cookies:
      "opencode.ai":
        "auth": "Fe26.2-test"
`,
		},
		{
			name: "empty zai with other providers",
			content: `
providers:
  zai:
    api_key: ""
  kimi:
    cookies:
      "kimi.com":
        "kimi-auth": "test-token"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.content)
			_, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
		})
	}
}
