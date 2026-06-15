package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
	"github.com/fabioconcina/claumon/internal/store"
)

// emptyForecastStore is a no-history stub: enough to give Handlers a non-nil
// *forecast.Service so HandleForecastSample runs past its nil guard.
type emptyForecastStore struct{}

func (emptyForecastStore) GetWindowSnapshots(gauge, resetAt string, since time.Time) ([]forecast.StoreSnapshot, error) {
	return nil, nil
}
func (emptyForecastStore) GetCompletedSessions(gauge string, before time.Time, limit int) ([]forecast.StoreSession, error) {
	return nil, nil
}

func setupTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(filepath.Join(claudeDir, "projects"), 0755)

	dbPath := filepath.Join(dir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := New(claudeDir, st, os.DirFS(dir))
	return srv, st
}

func TestHandleInfo(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.Handlers.SubscriptionType = "pro"
	srv.Handlers.RateLimitTier = "tier1"

	req := httptest.NewRequest("GET", "/api/info", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)

	if result["subscription_type"] != "pro" {
		t.Errorf("subscription_type = %v, want %q", result["subscription_type"], "pro")
	}
	if result["is_api_billing"] != false {
		t.Errorf("is_api_billing = %v, want false", result["is_api_billing"])
	}
}

func TestHandleUsageEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/usage", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["available"] != false {
		t.Errorf("expected available=false when no usage data")
	}
}

func TestHandleUsageWithData(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.Handlers.SetLatestUsage(map[string]interface{}{
		"session_pct": 42.5,
		"weekly_pct":  10.0,
	})

	req := httptest.NewRequest("GET", "/api/usage", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["session_pct"] != 42.5 {
		t.Errorf("session_pct = %v, want 42.5", result["session_pct"])
	}
}

// At 100% usage there's no headroom to simulate, so the sample endpoint must
// report available=false with reason "at_limit" — letting the modal show a
// meaningful "limit reached" message instead of a blank/"no forecast" state.
func TestHandleForecastSampleAtLimit(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.Handlers.Forecast = forecast.NewService(emptyForecastStore{}, forecast.DefaultConfig())
	srv.Handlers.SetLatestUsage(map[string]interface{}{
		"session_pct":      100.0,
		"session_reset_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})

	req := httptest.NewRequest("GET", "/api/forecast/sample?gauge=session", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["available"] != false {
		t.Errorf("available = %v, want false at 100%%", result["available"])
	}
	if result["reason"] != "at_limit" {
		t.Errorf("reason = %v, want %q at 100%%", result["reason"], "at_limit")
	}
}

func TestHandleHistory(t *testing.T) {
	srv, st := setupTestServer(t)

	st.UpsertDailyAggregate(store.DailyAggregate{
		Date:         time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"),
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.05,
	})

	req := httptest.NewRequest("GET", "/api/history?days=7", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// History is a continuous zero-filled series of `days` entries; the one
	// populated day should appear within it.
	var result []store.DailyAggregate
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 7 {
		t.Fatalf("expected 7 continuous history entries, got %d", len(result))
	}
	var found bool
	for _, d := range result {
		if d.InputTokens == 1000 && d.CostUSD == 0.05 {
			found = true
		}
	}
	if !found {
		t.Errorf("populated day not found in history series: %+v", result)
	}
}

func TestHandleHistoryEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/history", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	// With no data, history is still a continuous series of zero-filled days
	// (default 14) so the chart renders a continuous calendar, not gaps.
	var result []store.DailyAggregate
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 14 {
		t.Fatalf("expected 14 zero-filled entries, got %d", len(result))
	}
	for _, d := range result {
		if d.InputTokens != 0 || d.CostUSD != 0 || d.SessionCount != 0 {
			t.Errorf("expected zero-filled day, got %+v", d)
		}
	}
}

func TestHandleMemories(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/memories", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleMemoriesStaleness(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/memories/staleness", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if result["alerts"] == nil {
		t.Error("expected alerts field in staleness response")
	}
}

func TestHandleMemoriesGraph(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/memories/graph", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleMemoriesSearch(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/memories/search?q=test", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleSessionsEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "[]\n" {
		t.Errorf("expected empty array, got %q", string(body))
	}
}

func TestHandleSessionDetailNotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/sessions/nonexistent-id", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestWriteJSONContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"hello": "world"})

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if cors := w.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS header = %q, want %q", cors, "*")
	}
}
