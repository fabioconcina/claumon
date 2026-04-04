package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	darwinLabel = "com.claumon.dashboard"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", darwinLabel+".plist"), nil
}

func plistContent(execPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	logPath := filepath.Join(home, ".claumon", "claumon.log")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>`, darwinLabel, execPath, logPath, logPath), nil
}

func Install(execPath string) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	// Ensure log directory exists
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claumon"), 0755); err != nil {
		return fmt.Errorf("creating log dir: %w", err)
	}

	clearProvenance(execPath)

	content, err := plistContent(execPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("loading plist: %w", err)
	}

	return nil
}

func Uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "unload", path).Run()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("launchctl", "list", darwinLabel).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find service") {
			return "not installed", nil
		}
		return "not running", nil
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "PID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				pid := strings.TrimSpace(parts[1])
				if pid != "0" && pid != "" {
					return fmt.Sprintf("running (PID %s)", pid), nil
				}
			}
		}
	}
	return "installed (not running)", nil
}

func Restart() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("service not installed — run 'claumon service install' first")
	}

	// Read the plist to find the binary path and clear provenance before restart
	data, err := os.ReadFile(path)
	if err == nil {
		if execPath := extractExecPath(string(data)); execPath != "" {
			clearProvenance(execPath)
		}
	}

	exec.Command("launchctl", "unload", path).Run()
	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("reloading plist: %w", err)
	}
	return nil
}

// clearProvenance removes macOS quarantine/provenance attributes and re-signs
// the binary so AppleSystemPolicy allows execution.
func clearProvenance(path string) {
	exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
	exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	exec.Command("codesign", "--force", "--sign", "-", path).Run()
}

// extractExecPath parses the binary path from a launchd plist.
func extractExecPath(plist string) string {
	// Find the first <string> after <key>ProgramArguments</key><array>
	const marker = "<key>ProgramArguments</key>"
	idx := strings.Index(plist, marker)
	if idx < 0 {
		return ""
	}
	rest := plist[idx+len(marker):]
	const open = "<string>"
	const close = "</string>"
	si := strings.Index(rest, open)
	if si < 0 {
		return ""
	}
	rest = rest[si+len(open):]
	ei := strings.Index(rest, close)
	if ei < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:ei])
}
