//go:build windows

package parser

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const stillActiveExitCode = 259

func isProcessRunning(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActiveExitCode
}

// interruptProcess terminates a process by PID. Windows does not support
// POSIX-style SIGINT across arbitrary processes, so we TerminateProcess
// instead. This is not graceful, but aligns with the user-facing "stop"
// action on Windows.
func interruptProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("opening process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return fmt.Errorf("terminating process %d: %w", pid, err)
	}
	return nil
}
