package parser

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fabioconcina/claumon/internal/memory"
)

// PIDInfo represents a running Claude Code session from ~/.claude/sessions/{PID}.json.
type PIDInfo struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
	Title      string `json:"title,omitempty"`
	Project    string `json:"project,omitempty"`
}

// ProcessStatus holds the live status of a running session process.
type ProcessStatus struct {
	PID       int
	SessionID string
}

// DiscoverPIDFiles reads ~/.claude/sessions/*.json and returns entries with running PIDs.
func DiscoverPIDFiles(claudeDir string) []PIDInfo {
	sessionsDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}

	var results []PIDInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessionsDir, e.Name()))
		if err != nil {
			continue
		}

		var info PIDInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}

		// Verify PID matches filename (filename is {PID}.json)
		namePID, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || namePID != info.PID {
			continue
		}

		if !isProcessRunning(info.PID) {
			continue
		}

		// Enrich with session title/project from JSONL
		if info.SessionID != "" {
			if path := FindSessionFile(claudeDir, info.SessionID); path != "" {
				if s, err := ParseSessionFile(path); err == nil {
					info.Title = s.Title
				}
				// Extract project name from path: .../projects/{encoded-name}/session.jsonl
				projDir := filepath.Base(filepath.Dir(path))
				info.Project = memory.DecodePath(projDir)
			}
		}

		results = append(results, info)
	}
	return results
}

// BuildProcessMap returns a map from session ID to ProcessStatus for all running sessions.
func BuildProcessMap(claudeDir string) map[string]ProcessStatus {
	pids := DiscoverPIDFiles(claudeDir)
	m := make(map[string]ProcessStatus, len(pids))
	for _, p := range pids {
		m[p.SessionID] = ProcessStatus{
			PID:       p.PID,
			SessionID: p.SessionID,
		}
	}
	return m
}

// EnrichSessionsWithProcessStatus sets IsRunning, IsStuck, PID, and IdleMinutes
// on each SessionSummary based on live process data.
func EnrichSessionsWithProcessStatus(sessions []*SessionSummary, claudeDir string, stuckThreshold time.Duration) {
	procMap := BuildProcessMap(claudeDir)
	now := time.Now()

	for _, s := range sessions {
		ps, ok := procMap[s.ID]
		if !ok {
			continue
		}
		s.IsRunning = true
		s.PID = ps.PID

		idle := now.Sub(s.LastActivity)
		s.IdleMinutes = float64(int(idle.Minutes()*10)) / 10 // one decimal place

		if idle >= stuckThreshold {
			s.IsStuck = true
		}
	}
}

// KillSession sends an interrupt to the process running a given session.
func KillSession(claudeDir, sessionID string) error {
	procMap := BuildProcessMap(claudeDir)
	ps, ok := procMap[sessionID]
	if !ok {
		return fmt.Errorf("session %s is not running", sessionID)
	}

	log.Printf("[process] Interrupting PID %d (session %s)", ps.PID, sessionID)
	return interruptProcess(ps.PID)
}

// KillProcess sends an interrupt to a Claude Code process by PID.
// It verifies the PID belongs to a known Claude session before killing.
func KillProcess(claudeDir string, pid int) error {
	pids := DiscoverPIDFiles(claudeDir)
	for _, p := range pids {
		if p.PID == pid {
			log.Printf("[process] Interrupting PID %d (session %s)", pid, p.SessionID)
			return interruptProcess(pid)
		}
	}
	return fmt.Errorf("PID %d is not a known Claude process", pid)
}
