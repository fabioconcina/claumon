package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDiscoverPIDFiles_OwnPID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pid := os.Getpid()
	info := PIDInfo{
		PID:       pid,
		SessionID: "test-session-123",
		CWD:       "/tmp",
		StartedAt: time.Now().UnixMilli(),
		Kind:      "interactive",
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(pid)+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	results := DiscoverPIDFiles(tmpDir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].PID != pid {
		t.Errorf("expected PID %d, got %d", pid, results[0].PID)
	}
	if results[0].SessionID != "test-session-123" {
		t.Errorf("unexpected session ID: %s", results[0].SessionID)
	}
}

func TestDiscoverPIDFiles_DeadPID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	deadPID := 99999999
	info := PIDInfo{PID: deadPID, SessionID: "dead-session"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(deadPID)+".json"), data, 0o644)

	results := DiscoverPIDFiles(tmpDir)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for dead PID, got %d", len(results))
	}
}

func TestDiscoverPIDFiles_MismatchedFilename(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	info := PIDInfo{PID: os.Getpid(), SessionID: "mismatched"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, "12345.json"), data, 0o644)

	results := DiscoverPIDFiles(tmpDir)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for mismatched PID, got %d", len(results))
	}
}

func TestBuildProcessMap(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	pid := os.Getpid()
	info := PIDInfo{PID: pid, SessionID: "map-test-session"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(pid)+".json"), data, 0o644)

	m := BuildProcessMap(tmpDir)
	ps, ok := m["map-test-session"]
	if !ok {
		t.Fatal("expected session in process map")
	}
	if ps.PID != pid {
		t.Errorf("expected PID %d, got %d", pid, ps.PID)
	}
}

func TestEnrichSessionsWithProcessStatus(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	pid := os.Getpid()
	info := PIDInfo{PID: pid, SessionID: "enrich-session"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(pid)+".json"), data, 0o644)

	sessions := []*SessionSummary{
		{ID: "enrich-session", LastActivity: time.Now().Add(-5 * time.Minute)},
		{ID: "other-session", LastActivity: time.Now().Add(-1 * time.Minute)},
	}

	EnrichSessionsWithProcessStatus(sessions, tmpDir, 10*time.Minute)

	if !sessions[0].IsRunning {
		t.Error("expected enrich-session to be running")
	}
	if sessions[0].PID != pid {
		t.Errorf("expected PID %d, got %d", pid, sessions[0].PID)
	}
	if sessions[0].IsStuck {
		t.Error("session idle 5m should not be stuck with 10m threshold")
	}
	if sessions[0].IdleMinutes < 4.5 || sessions[0].IdleMinutes > 6.0 {
		t.Errorf("unexpected idle minutes: %.1f", sessions[0].IdleMinutes)
	}
	if sessions[1].IsRunning {
		t.Error("other-session should not be running")
	}
}

func TestEnrichSessionsWithProcessStatus_Stuck(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	pid := os.Getpid()
	info := PIDInfo{PID: pid, SessionID: "stuck-session"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(sessionsDir, strconv.Itoa(pid)+".json"), data, 0o644)

	sessions := []*SessionSummary{
		{ID: "stuck-session", LastActivity: time.Now().Add(-15 * time.Minute)},
	}

	EnrichSessionsWithProcessStatus(sessions, tmpDir, 10*time.Minute)

	if !sessions[0].IsStuck {
		t.Error("session idle 15m should be stuck with 10m threshold")
	}
}

func TestKillSession_UnknownSession(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "sessions"), 0o755)

	err := KillSession(tmpDir, "nonexistent-session")
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestIsProcessRunning(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Error("expected own process to be running")
	}
	if isProcessRunning(99999999) {
		t.Error("expected bogus PID to not be running")
	}
}
