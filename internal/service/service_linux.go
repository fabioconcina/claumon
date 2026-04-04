package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const linuxServiceName = "claumon"

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", linuxServiceName+".service"), nil
}

func unitContent(execPath string) string {
	return fmt.Sprintf(`[Unit]
Description=claumon — Claude Code dashboard
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, execPath)
}

func Install(execPath string) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating systemd user dir: %w", err)
	}

	if err := os.WriteFile(path, []byte(unitContent(execPath)), 0644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", linuxServiceName).Run(); err != nil {
		return fmt.Errorf("enabling service: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "start", linuxServiceName).Run(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	return nil
}

func Uninstall() error {
	exec.Command("systemctl", "--user", "stop", linuxServiceName).Run()
	exec.Command("systemctl", "--user", "disable", linuxServiceName).Run()

	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("systemctl", "--user", "is-active", linuxServiceName).CombinedOutput()
	status := strings.TrimSpace(string(out))
	if err != nil {
		path, pathErr := unitPath()
		if pathErr != nil {
			return "", pathErr
		}
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return "not installed", nil
		}
		return status, nil
	}
	return status, nil
}

func Restart() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("service not installed — run 'claumon service install' first")
	}
	if err := exec.Command("systemctl", "--user", "restart", linuxServiceName).Run(); err != nil {
		return fmt.Errorf("restarting service: %w", err)
	}
	return nil
}
