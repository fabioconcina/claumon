package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const windowsTaskName = "claumon"

func Install(execPath string) error {
	clearMarkOfTheWeb(execPath)
	err := exec.Command("schtasks", "/create",
		"/tn", windowsTaskName,
		"/tr", execPath,
		"/sc", "onlogon",
		"/rl", "limited",
		"/f",
	).Run()
	if err != nil {
		return fmt.Errorf("creating scheduled task: %w", err)
	}

	if err := exec.Command("schtasks", "/run", "/tn", windowsTaskName).Run(); err != nil {
		return fmt.Errorf("starting task: %w", err)
	}
	return nil
}

func Uninstall() error {
	exec.Command("schtasks", "/end", "/tn", windowsTaskName).Run()

	if err := exec.Command("schtasks", "/delete", "/tn", windowsTaskName, "/f").Run(); err != nil {
		return fmt.Errorf("deleting scheduled task: %w", err)
	}
	return nil
}

func Status() (string, error) {
	out, err := exec.Command("schtasks", "/query", "/tn", windowsTaskName, "/fo", "list").CombinedOutput()
	if err != nil {
		return "not installed", nil
	}
	outStr := string(out)
	if strings.Contains(outStr, "Running") {
		return "running", nil
	}
	if strings.Contains(outStr, "Ready") {
		return "installed (not running)", nil
	}
	return "installed (status unknown)", nil
}

func Restart() error {
	exec.Command("schtasks", "/end", "/tn", windowsTaskName).Run()
	// Query the task to find the binary path and clear Mark of the Web
	if out, err := exec.Command("schtasks", "/query", "/tn", windowsTaskName, "/fo", "list", "/v").CombinedOutput(); err == nil {
		if execPath := extractTaskAction(string(out)); execPath != "" {
			clearMarkOfTheWeb(execPath)
		}
	}
	if err := exec.Command("schtasks", "/run", "/tn", windowsTaskName).Run(); err != nil {
		return fmt.Errorf("restarting task: %w", err)
	}
	return nil
}

// clearMarkOfTheWeb removes the Zone.Identifier alternate data stream
// that Windows applies to downloaded files.
func clearMarkOfTheWeb(path string) {
	os.Remove(path + ":Zone.Identifier")
}

// extractTaskAction parses the binary path from schtasks /query /v output.
func extractTaskAction(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Task To Run:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
