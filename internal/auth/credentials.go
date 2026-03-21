package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type Credentials struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAt        int64
	SubscriptionType string
	RateLimitTier    string
	OrgUUID          string
}

type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"`
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
		RateLimitTier    string   `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
	OrganizationUuid string `json:"organizationUuid"`
}

func Load(claudeDir string, credentialsPath string) (*Credentials, error) {
	// Try explicit credentials path first, then default
	path := credentialsPath
	if path == "" {
		path = filepath.Join(claudeDir, ".credentials.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Fall back to OS credential store (VS Code extension stores credentials there)
		log.Printf("Credentials file not found, trying OS credential store...")
		data, err = loadFromOSCredentialStore()
		if err != nil {
			return nil, fmt.Errorf("no credentials file and OS credential store lookup failed: %w", err)
		}
	}

	return parse(data)
}

func parse(data []byte) (*Credentials, error) {
	var f credentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}

	if f.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("no access token found in credentials")
	}

	return &Credentials{
		AccessToken:      f.ClaudeAiOauth.AccessToken,
		RefreshToken:     f.ClaudeAiOauth.RefreshToken,
		ExpiresAt:        f.ClaudeAiOauth.ExpiresAt,
		SubscriptionType: f.ClaudeAiOauth.SubscriptionType,
		RateLimitTier:    f.ClaudeAiOauth.RateLimitTier,
		OrgUUID:          f.OrganizationUuid,
	}, nil
}
