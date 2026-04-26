package auth

import (
	"log"
	"sync"
	"time"
)

// Provider wraps Credentials with thread-safe access and automatic reload
// when the token is expired or approaching expiry.
type Provider struct {
	mu              sync.RWMutex
	creds           *Credentials
	claudeDir       string
	credentialsPath string
	lastReload      time.Time
	authStatus      string // "ok" or "expired"
	authMessage     string
}

const (
	AuthOK      = "ok"
	AuthExpired = "expired"

	expiryMargin    = 5 * time.Minute
	minReloadInterval = 30 * time.Second
)

// NewProvider creates a Provider with an initial credential load.
// If credentials cannot be loaded, the provider starts in expired state.
func NewProvider(claudeDir, credentialsPath string) *Provider {
	p := &Provider{
		claudeDir:       claudeDir,
		credentialsPath: credentialsPath,
		authStatus:      AuthExpired,
		authMessage:     "No credentials available. Run 'claude /login' to authenticate.",
	}

	creds, err := Load(claudeDir, credentialsPath)
	if err != nil {
		log.Printf("[auth] Could not load credentials: %v", err)
		log.Printf("[auth] Usage API will be unavailable. Run 'claude /login' to authenticate.")
		p.creds = &Credentials{}
		return p
	}

	p.creds = creds
	p.lastReload = time.Now()
	p.updateStatus()
	log.Printf("[auth] Loaded credentials: subscription=%s tier=%s", creds.SubscriptionType, creds.RateLimitTier)
	return p
}

// GetToken returns the current access token. If the token is expiring soon,
// it attempts a reload first.
func (p *Provider) GetToken() string {
	p.mu.RLock()
	expiring := p.isExpiringSoon(expiryMargin)
	token := p.creds.AccessToken
	p.mu.RUnlock()

	if expiring {
		if err := p.Reload(); err != nil {
			log.Printf("[auth] Proactive reload failed: %v", err)
		} else {
			p.mu.RLock()
			token = p.creds.AccessToken
			p.mu.RUnlock()
		}
	}

	return token
}

// Reload re-reads credentials from disk/OS store. Throttled to once per minReloadInterval.
func (p *Provider) Reload() error {
	p.mu.Lock()
	if time.Since(p.lastReload) < minReloadInterval {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	creds, err := Load(p.claudeDir, p.credentialsPath)
	if err != nil {
		p.mu.Lock()
		p.authStatus = AuthExpired
		p.authMessage = "Could not reload credentials. Run 'claude /login' to authenticate."
		p.lastReload = time.Now()
		p.mu.Unlock()
		return err
	}

	p.mu.Lock()
	oldToken := p.creds.AccessToken
	p.creds = creds
	p.lastReload = time.Now()
	p.updateStatus()
	if creds.AccessToken != oldToken && oldToken != "" {
		log.Printf("[auth] Credentials reloaded (token changed)")
	}
	p.mu.Unlock()
	return nil
}

// Status returns the current auth status and a user-facing message.
func (p *Provider) Status() (string, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.authStatus, p.authMessage
}

// MarkExpired forces the auth status to expired. Used when the API rejects
// the current token (401) so subsequent polls skip the API until credentials
// are refreshed externally, regardless of what creds.ExpiresAt reports.
func (p *Provider) MarkExpired(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authStatus = AuthExpired
	if message != "" {
		p.authMessage = message
	} else {
		p.authMessage = "Token expired — start a Claude Code session to refresh."
	}
}

// Credentials returns a snapshot of the current credentials.
func (p *Provider) Credentials() *Credentials {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.creds
}

// HasToken reports whether an access token is available.
func (p *Provider) HasToken() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.creds.AccessToken != ""
}

// isExpiringSoon returns true if ExpiresAt is set and within the given margin.
// Must be called with at least a read lock held.
func (p *Provider) isExpiringSoon(margin time.Duration) bool {
	if p.creds.ExpiresAt == 0 {
		return false
	}
	return time.Now().Add(margin).After(time.Unix(p.creds.ExpiresAt, 0))
}

// updateStatus sets authStatus based on current credentials.
// Must be called with the write lock held.
func (p *Provider) updateStatus() {
	if p.creds.AccessToken == "" {
		p.authStatus = AuthExpired
		p.authMessage = "No credentials available. Run 'claude /login' to authenticate."
		return
	}
	if p.creds.ExpiresAt != 0 && time.Now().After(time.Unix(p.creds.ExpiresAt, 0)) {
		p.authStatus = AuthExpired
		p.authMessage = "Token expired — start a Claude Code session to refresh."
		return
	}
	p.authStatus = AuthOK
	p.authMessage = ""
}
