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
	Codex  ProviderAuth `yaml:"codex"`
	Kimi   ProviderAuth `yaml:"kimi"`
	Claude ProviderAuth `yaml:"claude"`
	Zai    ZAIConfig    `yaml:"zai"`
}

// ProviderAuth contains cookie-based authentication for providers
type ProviderAuth struct {
	Cookies map[string]map[string]string `yaml:"cookies"`
}

// ZAIConfig contains API key authentication for ZAI provider
type ZAIConfig struct {
	APIKey string `yaml:"api_key"`
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

	// Check ZAI
	if config.Providers.Zai.APIKey != "" {
		hasConfiguredProvider = true
	}

	// Check cookie-based providers
	if len(config.Providers.Codex.Cookies) > 0 {
		hasConfiguredProvider = true
	}
	if len(config.Providers.Kimi.Cookies) > 0 {
		hasConfiguredProvider = true
	}
	if len(config.Providers.Claude.Cookies) > 0 {
		hasConfiguredProvider = true
	}

	if !hasConfiguredProvider {
		return fmt.Errorf("at least one provider must be configured")
	}

	return nil
}
