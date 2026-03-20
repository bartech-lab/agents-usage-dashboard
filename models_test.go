package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRemainingFromISO_ValidTimestamp(t *testing.T) {
	iso := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	remaining := remainingFromISO(iso)
	if remaining == nil {
		t.Fatalf("remainingFromISO() = nil, want value")
	}
	if *remaining < 100 || *remaining > 130 {
		t.Fatalf("remainingFromISO() = %d, want around 120 seconds", *remaining)
	}
}

func TestRemainingFromUnix_ValidTimestamp(t *testing.T) {
	ts := float64(time.Now().Add(90 * time.Second).Unix())
	remaining := remainingFromUnix(ts)
	if remaining == nil {
		t.Fatalf("remainingFromUnix() = nil, want value")
	}
	if *remaining < 70 || *remaining > 100 {
		t.Fatalf("remainingFromUnix() = %d, want around 90 seconds", *remaining)
	}
}

func TestUnixToISO_Conversion(t *testing.T) {
	ts := float64(1709577600)
	got := unixToISO(ts)
	if got != "2024-03-04T18:40:00Z" {
		t.Fatalf("unixToISO() = %q, want %q", got, "2024-03-04T18:40:00Z")
	}
}

func TestTimeHelpers_EdgeCases(t *testing.T) {
	if remaining := remainingFromISO(""); remaining != nil {
		t.Fatalf("remainingFromISO(\"\") = %v, want nil", *remaining)
	}

	if remaining := remainingFromISO("not-a-time"); remaining != nil {
		t.Fatalf("remainingFromISO(invalid) = %v, want nil", *remaining)
	}

	if remaining := remainingFromUnix(0); remaining != nil {
		t.Fatalf("remainingFromUnix(0) = %v, want nil", *remaining)
	}

	if got := unixToISO(0); got != "" {
		t.Fatalf("unixToISO(0) = %q, want empty string", got)
	}
}

func TestProviderData_JSONMarshalUnmarshal(t *testing.T) {
	input := ProviderData{
		Status: "ok",
		Plan:   "Pro",
		Session: &UsageWindow{
			UsagePct:         33.5,
			ResetAt:          "2026-03-04T18:00:00Z",
			RemainingSeconds: 120,
		},
		Credits: &Credits{Balance: 12.5, HasCredits: true},
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var decoded ProviderData
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if decoded.Status != "ok" || decoded.Plan != "Pro" {
		t.Fatalf("decoded top-level fields mismatch: %+v", decoded)
	}
	if decoded.Session == nil || decoded.Session.UsagePct != 33.5 {
		t.Fatalf("decoded session mismatch: %+v", decoded.Session)
	}
	if decoded.Credits == nil || !decoded.Credits.HasCredits || decoded.Credits.Balance != 12.5 {
		t.Fatalf("decoded credits mismatch: %+v", decoded.Credits)
	}
}

func TestCredits_UnmarshalStringBalance(t *testing.T) {
	input := []byte(`{"balance":"0","has_credits":false}`)

	var credits Credits
	if err := json.Unmarshal(input, &credits); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if credits.Balance != 0 {
		t.Fatalf("credits.Balance = %v, want 0", credits.Balance)
	}
	if credits.HasCredits {
		t.Fatalf("credits.HasCredits = true, want false")
	}
}

func TestCredits_UnmarshalNumericBalance(t *testing.T) {
	input := []byte(`{"balance":12.5,"has_credits":true}`)

	var credits Credits
	if err := json.Unmarshal(input, &credits); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if credits.Balance != 12.5 {
		t.Fatalf("credits.Balance = %v, want 12.5", credits.Balance)
	}
	if !credits.HasCredits {
		t.Fatalf("credits.HasCredits = false, want true")
	}
}

func TestUsageWindow_JSONOmitempty(t *testing.T) {
	window := UsageWindow{UsagePct: 10, RemainingSeconds: 5}

	encoded, err := json.Marshal(window)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	jsonStr := string(encoded)
	if strings.Contains(jsonStr, "reset_at") {
		t.Fatalf("expected reset_at to be omitted, got %s", jsonStr)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	for _, key := range []string{"used", "limit", "remaining"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("expected %q to be omitted, got %s", key, jsonStr)
		}
	}
	if !strings.Contains(jsonStr, "remaining_seconds") {
		t.Fatalf("expected remaining_seconds field to be present, got %s", jsonStr)
	}
}

func TestCacheData_JSONStructure(t *testing.T) {
	cache := CacheData{
		Zai:           &ProviderData{Status: "ok", Plan: "free"},
		Kimi:          &ProviderData{Status: "offline"},
		Codex:         &ProviderData{Status: "ok", Plan: "plus"},
		Claude:        &ProviderData{Status: "ok", Plan: "pro"},
		LastFetch:     "2026-03-04T17:00:00Z",
		NextRefreshAt: "2026-03-04T17:05:00Z",
	}

	encoded, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	for _, key := range []string{"zai", "kimi", "codex", "claude", "last_fetch", "next_refresh_at"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("expected key %q in marshaled JSON", key)
		}
	}
}
