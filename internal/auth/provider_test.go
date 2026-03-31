package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCredsFile(t *testing.T, dir string, content string) {
	t.Helper()
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNewProvider_ValidCredentials(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "valid-token",
			"expiresAt": `+expiresInFuture()+`,
			"subscriptionType": "pro",
			"rateLimitTier": "tier1"
		}
	}`)

	p := NewProvider(dir, "")
	if !p.HasToken() {
		t.Fatal("expected HasToken() = true")
	}
	if got := p.GetToken(); got != "valid-token" {
		t.Errorf("GetToken() = %q, want %q", got, "valid-token")
	}
	status, msg := p.Status()
	if status != AuthOK {
		t.Errorf("Status() = %q %q, want ok", status, msg)
	}
}

func TestNewProvider_NoCredentials(t *testing.T) {
	dir := t.TempDir()
	// No credentials file

	p := NewProvider(dir, "")
	if p.HasToken() {
		t.Fatal("expected HasToken() = false")
	}
	status, _ := p.Status()
	if status != AuthExpired {
		t.Errorf("Status() = %q, want expired", status)
	}
}

func TestProvider_ExpiredToken(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "old-token",
			"expiresAt": 1000000000
		}
	}`)

	p := NewProvider(dir, "")
	status, _ := p.Status()
	if status != AuthExpired {
		t.Errorf("Status() = %q, want expired", status)
	}
}

func TestProvider_Reload_PicksUpNewToken(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "token-v1",
			"expiresAt": `+expiresInFuture()+`
		}
	}`)

	p := NewProvider(dir, "")
	if got := p.GetToken(); got != "token-v1" {
		t.Fatalf("initial token = %q, want token-v1", got)
	}

	// Update credentials file
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "token-v2",
			"expiresAt": `+expiresInFuture()+`
		}
	}`)

	// Force reload (bypass throttle by resetting lastReload)
	p.mu.Lock()
	p.lastReload = time.Time{}
	p.mu.Unlock()

	if err := p.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	if got := p.GetToken(); got != "token-v2" {
		t.Errorf("after reload token = %q, want token-v2", got)
	}
	status, _ := p.Status()
	if status != AuthOK {
		t.Errorf("Status() after reload = %q, want ok", status)
	}
}

func TestProvider_Reload_Throttled(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "token-v1",
			"expiresAt": `+expiresInFuture()+`
		}
	}`)

	p := NewProvider(dir, "")

	// Update credentials file
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "token-v2",
			"expiresAt": `+expiresInFuture()+`
		}
	}`)

	// Reload should be throttled (lastReload was just set)
	if err := p.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	// Token should still be v1 because reload was throttled
	if got := p.GetToken(); got != "token-v1" {
		t.Errorf("after throttled reload token = %q, want token-v1", got)
	}
}

func TestProvider_GetToken_ProactiveReload(t *testing.T) {
	dir := t.TempDir()
	// Token expires in 2 minutes (within the 5min margin)
	expiresAt := time.Now().Add(2 * time.Minute).Unix()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "expiring-token",
			"expiresAt": `+formatUnix(expiresAt)+`
		}
	}`)

	p := NewProvider(dir, "")

	// Write new credentials with a fresh token
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "fresh-token",
			"expiresAt": `+expiresInFuture()+`
		}
	}`)

	// Reset throttle so proactive reload works
	p.mu.Lock()
	p.lastReload = time.Time{}
	p.mu.Unlock()

	// GetToken should detect the expiring token and reload
	got := p.GetToken()
	if got != "fresh-token" {
		t.Errorf("GetToken() = %q, want fresh-token (proactive reload)", got)
	}
}

func TestProvider_Credentials(t *testing.T) {
	dir := t.TempDir()
	writeCredsFile(t, dir, `{
		"claudeAiOauth": {
			"accessToken": "tok",
			"subscriptionType": "pro",
			"rateLimitTier": "tier1"
		}
	}`)

	p := NewProvider(dir, "")
	c := p.Credentials()
	if c.SubscriptionType != "pro" {
		t.Errorf("SubscriptionType = %q, want pro", c.SubscriptionType)
	}
	if c.RateLimitTier != "tier1" {
		t.Errorf("RateLimitTier = %q, want tier1", c.RateLimitTier)
	}
}

func expiresInFuture() string {
	return formatUnix(time.Now().Add(24 * time.Hour).Unix())
}

func formatUnix(ts int64) string {
	return fmt.Sprintf("%d", ts)
}
