package service

import (
	"net"
	"testing"
	"time"
)

func TestExtractVBSPath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "standard script",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Users\\test\\claumon.exe\", 0, False\r\n",
			want:    `C:\Users\test\claumon.exe`,
		},
		{
			name:    "path with spaces",
			content: "CreateObject(\"WScript.Shell\").Run \"C:\\Program Files\\claumon\\claumon.exe\", 0, False\r\n",
			want:    `C:\Program Files\claumon\claumon.exe`,
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "no Run keyword",
			content: "something else entirely",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVBSPath(tt.content)
			if got != tt.want {
				t.Errorf("extractVBSPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaitForPortFree_AlreadyFree(t *testing.T) {
	start := time.Now()
	waitForPortFree(0, 2*time.Second) // port 0 is never bound
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("waitForPortFree took %v on a free port, expected near-instant", elapsed)
	}
}

func TestWaitForPortFree_BecomesAvailable(t *testing.T) {
	// Bind a port, then release it after a short delay
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		time.Sleep(300 * time.Millisecond)
		ln.Close()
	}()

	start := time.Now()
	waitForPortFree(port, 3*time.Second)
	elapsed := time.Since(start)

	if elapsed < 200*time.Millisecond {
		t.Errorf("waitForPortFree returned too quickly (%v), port should have been busy", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("waitForPortFree took too long (%v), port should have freed after ~300ms", elapsed)
	}
}

func TestConfiguredPort_Default(t *testing.T) {
	// Without a config file at the expected path, should return default
	port := configuredPort()
	if port != 3131 {
		t.Errorf("configuredPort() = %d, want 3131", port)
	}
}
