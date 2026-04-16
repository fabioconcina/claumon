package service

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func startupDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

func vbsPath() string {
	return filepath.Join(startupDir(), "claumon.vbs")
}

// Install creates a VBScript in the Startup folder that launches claumon
// hidden (no console window). No admin privileges required.
func Install(execPath string) error {
	clearMarkOfTheWeb(execPath)

	// VBScript launches the exe hidden (0 = hidden window)
	script := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"%s\", 0, False\r\n", execPath)

	if err := os.WriteFile(vbsPath(), []byte(script), 0644); err != nil {
		return fmt.Errorf("writing startup script: %w", err)
	}

	// Start it now
	if err := exec.Command("wscript.exe", vbsPath()).Start(); err != nil {
		return fmt.Errorf("starting claumon: %w", err)
	}
	return nil
}

func Uninstall() error {
	// Kill any running instance
	exec.Command("taskkill", "/f", "/im", "claumon.exe").Run()

	if err := os.Remove(vbsPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing startup script: %w", err)
	}
	return nil
}

func Status() (string, error) {
	_, err := os.Stat(vbsPath())
	installed := err == nil

	out, _ := exec.Command("tasklist", "/fi", "imagename eq claumon.exe", "/fo", "csv", "/nh").CombinedOutput()
	running := strings.Contains(string(out), "claumon.exe")

	switch {
	case installed && running:
		return "running", nil
	case installed:
		return "installed (not running)", nil
	default:
		return "not installed", nil
	}
}

func Restart() error {
	killOtherInstances()
	waitForPortFree(configuredPort(), 3*time.Second)

	path := vbsPath()
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("startup script not found — is the service installed?")
	}

	// Re-read the VBS to find the exe path and clear MotW
	if data, err := os.ReadFile(path); err == nil {
		if execPath := extractVBSPath(string(data)); execPath != "" {
			clearMarkOfTheWeb(execPath)
		}
	}

	if err := exec.Command("wscript.exe", path).Start(); err != nil {
		return fmt.Errorf("restarting claumon: %w", err)
	}
	return nil
}

// killOtherInstances kills all claumon.exe processes except the current one.
func killOtherInstances() {
	myPID := os.Getpid()
	out, err := exec.Command("tasklist", "/fi", "imagename eq claumon.exe", "/fo", "csv", "/nh").CombinedOutput()
	if err != nil {
		// Fallback: kill all (old behavior)
		exec.Command("taskkill", "/f", "/im", "claumon.exe").Run()
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\",\"")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], "\"")
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == myPID {
			continue
		}
		exec.Command("taskkill", "/f", "/pid", strconv.Itoa(pid)).Run()
	}
}

// configuredPort reads the port from the user's config file, defaulting to 3131.
func configuredPort() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return 3131
	}
	data, err := os.ReadFile(filepath.Join(home, ".claumon", "config.json"))
	if err != nil {
		return 3131
	}
	var cfg struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Port == 0 {
		return 3131
	}
	return cfg.Port
}

// waitForPortFree polls until the given TCP port is no longer in use,
// up to the specified timeout. This prevents the new service from failing
// to bind after force-killing the old process.
func waitForPortFree(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			ln.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// clearMarkOfTheWeb removes the Zone.Identifier alternate data stream
// that Windows applies to downloaded files.
func clearMarkOfTheWeb(path string) {
	os.Remove(path + ":Zone.Identifier")
}

// extractVBSPath pulls the exe path from the VBS script content.
func extractVBSPath(content string) string {
	// Script format: CreateObject("WScript.Shell").Run "C:\...\claumon.exe", 0, False
	start := strings.Index(content, ".Run \"")
	if start == -1 {
		return ""
	}
	rest := content[start+6:]
	end := strings.Index(rest, "\"")
	if end == -1 {
		return ""
	}
	return rest[:end]
}
