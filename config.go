package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration loaded from YAML
type Config struct {
	RefreshInterval time.Duration   `yaml:"refresh_interval"`
	ServerPort      int             `yaml:"server_port"`
	Providers       ProvidersConfig `yaml:"providers"`
}

// ProvidersConfig contains authentication settings for all providers
type ProvidersConfig struct {
	Kimi   ProviderAuth        `yaml:"kimi"`
	Zai    ZAIConfig           `yaml:"zai"`
	Codex  CodexProviderConfig `yaml:"codex"`
	Claude ProviderAuth        `yaml:"claude"`
}

// CodexProviderConfig contains Codex-specific configuration
type CodexProviderConfig struct {
	Enabled bool         `yaml:"enabled"`
	OAuth   *OAuthConfig `yaml:"oauth,omitempty"`
}

// ProviderAuth contains cookie-based authentication for providers
type ProviderAuth struct {
	Enabled bool                         `yaml:"enabled"`
	Cookies map[string]map[string]string `yaml:"cookies"`
}

// ZAIConfig contains API key authentication for ZAI provider
type ZAIConfig struct {
	Enabled bool   `yaml:"enabled"`
	APIKey  string `yaml:"api_key"`
}

// OAuthConfig contains OAuth token file configuration
type OAuthConfig struct {
	TokenFile string `yaml:"token_file"`
}

// DefaultRefreshInterval is the default refresh interval if not specified
const DefaultRefreshInterval = 5 * time.Minute

// DefaultServerPort is the default server port if not specified
const DefaultServerPort = 8777

// envVarPattern matches ${ENV_VAR} syntax
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateEnvVars replaces ${ENV_VAR} patterns with actual environment variable values
func interpolateEnvVars(input string) string {
	return envVarPattern.ReplaceAllStringFunc(input, func(match string) string {
		// Extract the variable name from ${VAR}
		varName := match[2 : len(match)-1]
		if value := os.Getenv(varName); value != "" {
			return value
		}
		// Return original if env var not found
		return match
	})
}

// LoadConfig reads and parses the configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// First pass: interpolate environment variables
	interpolatedData := string(data)
	lines := strings.Split(interpolatedData, "\n")
	for i, line := range lines {
		lines[i] = interpolateEnvVars(line)
	}
	interpolatedData = strings.Join(lines, "\n")

	var config Config
	if err := yaml.Unmarshal([]byte(interpolatedData), &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Apply defaults
	if config.RefreshInterval == 0 {
		config.RefreshInterval = DefaultRefreshInterval
	}
	if config.ServerPort == 0 {
		config.ServerPort = DefaultServerPort
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// validateConfig checks that the configuration has all required fields
func validateConfig(config *Config) error {
	// Validate refresh interval
	if config.RefreshInterval < 0 {
		return fmt.Errorf("refresh_interval must be positive")
	}

	// Validate server port
	if config.ServerPort < 1 || config.ServerPort > 65535 {
		return fmt.Errorf("server_port must be between 1 and 65535")
	}

	// Validate that at least one provider is configured
	hasConfiguredProvider := false

	// Check ZAI - needs API key
	if config.Providers.Zai.APIKey != "" {
		hasConfiguredProvider = true
	}

	// Check Codex - needs OAuth token file
	if config.Providers.Codex.OAuth != nil && config.Providers.Codex.OAuth.TokenFile != "" {
		hasConfiguredProvider = true
	}

	// Check Kimi - needs cookies
	if len(config.Providers.Kimi.Cookies) > 0 {
		for _, domainCookies := range config.Providers.Kimi.Cookies {
			if len(domainCookies) > 0 {
				hasConfiguredProvider = true
				break
			}
		}
	}

	// Check Claude - needs cookies
	if len(config.Providers.Claude.Cookies) > 0 {
		for _, domainCookies := range config.Providers.Claude.Cookies {
			if len(domainCookies) > 0 {
				hasConfiguredProvider = true
				break
			}
		}
	}

	if !hasConfiguredProvider {
		return fmt.Errorf("at least one provider must be configured with credentials (check your config.yaml and .env files)")
	}

	return nil
}

// Save writes the configuration back to a YAML file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
