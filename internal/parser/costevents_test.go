package parser

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSessionFileCostEventsDedupesByMessageID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")

	// msg_1 is logged twice (one line per content block) with the same usage.
	content := `{"type":"user","timestamp":"2026-03-20T10:00:00Z","message":{"role":"user","content":"hi"}}
{"type":"assistant","timestamp":"2026-03-20T10:00:01Z","message":{"id":"msg_1","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":1000,"output_tokens":100,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","timestamp":"2026-03-20T10:00:02Z","message":{"id":"msg_1","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"tool_use","name":"Read"}],"usage":{"input_tokens":1000,"output_tokens":100,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","timestamp":"2026-03-20T10:00:05Z","message":{"id":"msg_2","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":2000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	events, err := parseSessionFileCostEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	want1 := costFor("claude-sonnet-4-6", 1000, 100, 0, 0)
	if math.Abs(events[0].CostUSD-want1) > 1e-12 {
		t.Errorf("events[0].CostUSD = %v, want %v", events[0].CostUSD, want1)
	}
	if !events[0].Time.Equal(time.Date(2026, 3, 20, 10, 0, 2, 0, time.UTC)) {
		t.Errorf("events[0].Time = %v, want the last line's timestamp", events[0].Time)
	}
	if events[1].CostUSD <= 0 {
		t.Errorf("events[1].CostUSD = %v, want > 0", events[1].CostUSD)
	}
}

func TestCostEventsBetween(t *testing.T) {
	claudeDir := t.TempDir()
	proj := filepath.Join(claudeDir, "projects", "-tmp-proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}

	content := `{"type":"assistant","timestamp":"2026-03-20T09:00:00Z","message":{"id":"m1","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":1}}}
{"type":"assistant","timestamp":"2026-03-20T11:00:00Z","message":{"id":"m2","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":1}}}
{"type":"assistant","timestamp":"2026-03-20T13:00:00Z","message":{"id":"m3","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":1}}}
`
	if err := os.WriteFile(filepath.Join(proj, "sess-a.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(proj, "sess-old.jsonl")
	if err := os.WriteFile(stale, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	got, err := CostEventsBetween(claudeDir, time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC), time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got["sess-a"]) != 1 {
		t.Errorf("sess-a events = %d, want 1 (only the one inside the range)", len(got["sess-a"]))
	}
	if _, ok := got["sess-old"]; ok {
		t.Error("file last modified before from should be skipped")
	}
}
