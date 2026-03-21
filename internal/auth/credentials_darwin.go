package auth

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

func loadFromOSCredentialStore() ([]byte, error) {
	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("getting current user: %w", err)
	}

	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials",
		"-a", u.Username,
		"-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("macOS keychain lookup failed: %w", err)
	}

	return []byte(strings.TrimSpace(string(out))), nil
}
