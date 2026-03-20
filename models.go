package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// UsageWindow for session/weekly with provider-specific extensions
type UsageWindow struct {
	UsagePct         float64 `json:"usage_pct"`
	ResetAt          string  `json:"reset_at,omitempty"`
	RemainingSeconds int     `json:"remaining_seconds"`
	// Kimi-specific fields (omitempty so they don't appear for other providers)
	Used      int `json:"used,omitempty"`
	Limit     int `json:"limit,omitempty"`
	Remaining int `json:"remaining,omitempty"`
}

// ClaudeModels for Claude-specific model usage data
type ClaudeModels struct {
	Sonnet float64 `json:"sonnet"`
	Opus   float64 `json:"opus"`
}

// Credits for Codex-specific credits data
type Credits struct {
	Balance    float64 `json:"balance"`
	HasCredits bool    `json:"has_credits"`
}

func (c *Credits) UnmarshalJSON(data []byte) error {
	type rawCredits struct {
		Balance    any  `json:"balance"`
		HasCredits bool `json:"has_credits"`
	}

	var raw rawCredits
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.HasCredits = raw.HasCredits

	switch value := raw.Balance.(type) {
	case nil:
		c.Balance = 0
	case float64:
		c.Balance = value
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("parse credits.balance %q: %w", value, err)
		}
		c.Balance = parsed
	default:
		return &json.UnmarshalTypeError{Value: "credits.balance", Type: reflect.TypeOf(c.Balance)}
	}

	return nil
}

// ProviderData holds usage data for a specific AI provider
type ProviderData struct {
	Status         string        `json:"status"`
	Plan           string        `json:"plan,omitempty"`
	LimitReached   bool          `json:"limit_reached,omitempty"` // Codex only
	Session        *UsageWindow  `json:"session,omitempty"`
	Weekly         *UsageWindow  `json:"weekly,omitempty"`
	Models         *ClaudeModels `json:"models,omitempty"`  // Claude only
	Credits        *Credits      `json:"credits,omitempty"` // Codex only
	DailyBreakdown []DailyEntry  `json:"daily_breakdown,omitempty"`
	Error          string        `json:"error,omitempty"`
	LastSuccess    string        `json:"last_success,omitempty"`
}

// DailyEntry represents a single day's usage data
type DailyEntry struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// CacheData holds cached data for all providers
type CacheData struct {
	Codex         *ProviderData `json:"codex"`
	Kimi          *ProviderData `json:"kimi"`
	Claude        *ProviderData `json:"claude"`
	Zai           *ProviderData `json:"zai"`
	LastFetch     string        `json:"last_fetch"`
	NextRefreshAt string        `json:"next_refresh_at"`
}

// remainingFromISO returns seconds remaining until an ISO-8601 reset timestamp
func remainingFromISO(isoStr string) *int {
	if isoStr == "" {
		return nil
	}

	// Parse ISO-8601 timestamp (handle "Z" suffix)
	layout := time.RFC3339
	if isoStr[len(isoStr)-1] == 'Z' {
		isoStr = isoStr[:len(isoStr)-1] + "+00:00"
	}

	resetTime, err := time.Parse(layout, isoStr)
	if err != nil {
		return nil
	}

	remaining := int(time.Until(resetTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

// remainingFromUnix returns seconds remaining until a Unix epoch reset timestamp
func remainingFromUnix(ts float64) *int {
	if ts == 0 {
		return nil
	}

	resetTime := time.Unix(int64(ts), 0)
	remaining := int(time.Until(resetTime).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

// unixToISO converts Unix epoch timestamp to ISO-8601 string for frontend
func unixToISO(ts float64) string {
	if ts == 0 {
		return ""
	}

	t := time.Unix(int64(ts), 0).UTC()
	return t.Format(time.RFC3339)
}
