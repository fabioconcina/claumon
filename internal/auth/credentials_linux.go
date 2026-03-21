package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

func loadFromOSCredentialStore() ([]byte, error) {
	// VS Code uses libsecret on Linux (GNOME Keyring / KWallet).
	// secret-tool is the CLI for libsecret.
	out, err := exec.Command("secret-tool", "lookup",
		"service", "Claude Code-credentials",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("libsecret lookup failed (is secret-tool installed?): %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return nil, fmt.Errorf("no credentials found in libsecret")
	}

	return []byte(result), nil
}
