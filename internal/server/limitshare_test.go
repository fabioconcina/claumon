package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabioconcina/claumon/internal/parser"
	"github.com/fabioconcina/claumon/internal/pricing"
)

func TestSessionLimitShare(t *testing.T) {
	parser.SetPricingTable(pricing.Load(nil))
	srv, st := setupTestServer(t)
	claudeDir := srv.Handlers.claudeDir

	now := time.Now().UTC()
	proj := filepath.Join(claudeDir, "projects", "-tmp-proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	// Session "big" costs 3x session "small", both inside the current window.
	line := func(id string, ts time.Time, in int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-sonnet-4-6","usage":{"input_tokens":%d,"output_tokens":0}}}`+"\n",
			ts.Format(time.RFC3339), id, in)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(proj, name+".jsonl"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("big", line("m1", now.Add(-30*time.Minute), 3000))
	write("small", line("m2", now.Add(-10*time.Minute), 1000))

	// The 5h window opened 1h ago; the weekly window 1 day ago.
	sessionReset := now.Add(4 * time.Hour).Format(time.RFC3339)
	weeklyReset := now.Add(6 * 24 * time.Hour).Format(time.RFC3339)
	if err := st.SaveUsageSnapshot(8, 2, sessionReset, weeklyReset, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}

	want := map[string][2]float64{"big": {6, 1.5}, "small": {2, 0.5}}
	for id, w := range want {
		got := getLimitShare(t, srv, id)
		if got.SessionPct == nil || *got.SessionPct != w[0] {
			t.Errorf("%s SessionPct = %v, want %v", id, got.SessionPct, w[0])
		}
		if got.WeeklyPct == nil || *got.WeeklyPct != w[1] {
			t.Errorf("%s WeeklyPct = %v, want %v", id, got.WeeklyPct, w[1])
		}
	}

	// "late" only ran after the last poll: no estimate yet, not 0%.
	write("late", line("m3", now.Add(time.Minute), 1000))
	if got := getLimitShare(t, srv, "late"); got.SessionPct != nil || got.WeeklyPct != nil {
		t.Errorf("late = %v / %v, want no estimate", got.SessionPct, got.WeeklyPct)
	}
}

func TestSessionLimitShareWithoutSnapshots(t *testing.T) {
	srv, _ := setupTestServer(t)
	proj := filepath.Join(srv.Handlers.claudeDir, "projects", "-tmp-proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":"m1","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":1}}}`+"\n",
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(proj, "x.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got := getLimitShare(t, srv, "x")
	if got.SessionPct != nil || got.WeeklyPct != nil {
		t.Errorf("expected no estimate without snapshots, got %v / %v", got.SessionPct, got.WeeklyPct)
	}
}

func TestSessionLimitShareNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/sessions/nope/limit-share", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func getLimitShare(t *testing.T, srv *Server, id string) LimitShare {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/sessions/"+id+"/limit-share", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, body %s", id, w.Code, w.Body.String())
	}
	var got LimitShare
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}
