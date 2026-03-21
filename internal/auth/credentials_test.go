package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	data := []byte(`{
		"claudeAiOauth": {
			"accessToken": "test-token-123",
			"refreshToken": "refresh-456",
			"expiresAt": 1700000000,
			"scopes": ["user:read"],
			"subscriptionType": "pro",
			"rateLimitTier": "tier1"
		},
		"organizationUuid": "org-789"
	}`)

	creds, err := parse(data)
	if err != nil {
		t.Fatalf("parse() error: %v", err)
	}

	if creds.AccessToken != "test-token-123" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "test-token-123")
	}
	if creds.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %q, want %q", creds.RefreshToken, "refresh-456")
	}
	if creds.SubscriptionType != "pro" {
		t.Errorf("SubscriptionType = %q, want %q", creds.SubscriptionType, "pro")
	}
	if creds.RateLimitTier != "tier1" {
		t.Errorf("RateLimitTier = %q, want %q", creds.RateLimitTier, "tier1")
	}
}

func TestParseNoAccessToken(t *testing.T) {
	data := []byte(`{"claudeAiOauth": {"refreshToken": "abc"}}`)
	_, err := parse(data)
	if err == nil {
		t.Fatal("parse() should error when no access token")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := parse([]byte(`not json`))
	if err == nil {
		t.Fatal("parse() should error on invalid JSON")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, ".credentials.json")

	data := []byte(`{
		"claudeAiOauth": {
			"accessToken": "file-token",
			"subscriptionType": "pro",
			"rateLimitTier": "tier1"
		}
	}`)
	if err := os.WriteFile(credsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	creds, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if creds.AccessToken != "file-token" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "file-token")
	}
}

func TestLoadFromExplicitPath(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom-creds.json")

	data := []byte(`{
		"claudeAiOauth": {
			"accessToken": "custom-token",
			"subscriptionType": "team",
			"rateLimitTier": "tier2"
		}
	}`)
	if err := os.WriteFile(customPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	creds, err := Load("/nonexistent", customPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if creds.AccessToken != "custom-token" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "custom-token")
	}
}
