package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fabioconcina/claumon/internal/store"
)

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

func TestHandleHistory(t *testing.T) {
	srv, st := setupTestServer(t)

	st.UpsertDailyAggregate(store.DailyAggregate{
		Date:         "2026-03-20",
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

	var result []store.DailyAggregate
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(result))
	}
}

func TestHandleHistoryEmpty(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/history", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	if string(body) != "[]\n" {
		t.Errorf("expected empty array, got %q", string(body))
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
