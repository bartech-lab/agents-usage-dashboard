package main

import (
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
