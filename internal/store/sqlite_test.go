package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenAndClose(t *testing.T) {
	st := openTestStore(t)
	if err := st.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestUpsertAndGetHistory(t *testing.T) {
	st := openTestStore(t)

	agg := DailyAggregate{
		Date:         "2026-03-20",
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.05,
		SessionCount: 2,
		MessageCount: 10,
	}
	if err := st.UpsertDailyAggregate(agg); err != nil {
		t.Fatalf("UpsertDailyAggregate: %v", err)
	}

	history, err := st.GetHistory(30)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(history))
	}
	if history[0].InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", history[0].InputTokens)
	}
	if history[0].CostUSD != 0.05 {
		t.Errorf("CostUSD = %f, want 0.05", history[0].CostUSD)
	}
}

func TestUpsertOverwrites(t *testing.T) {
	st := openTestStore(t)

	st.UpsertDailyAggregate(DailyAggregate{Date: "2026-03-20", InputTokens: 100})
	st.UpsertDailyAggregate(DailyAggregate{Date: "2026-03-20", InputTokens: 999})

	history, _ := st.GetHistory(30)
	if len(history) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(history))
	}
	if history[0].InputTokens != 999 {
		t.Errorf("InputTokens = %d, want 999 (upsert should overwrite)", history[0].InputTokens)
	}
}

func TestGetTodaySummaryEmpty(t *testing.T) {
	st := openTestStore(t)

	summary, err := st.GetTodaySummary()
	if err != nil {
		t.Fatalf("GetTodaySummary: %v", err)
	}
	if summary.InputTokens != 0 {
		t.Errorf("expected 0 input tokens for empty day, got %d", summary.InputTokens)
	}
}

func TestSaveUsageSnapshot(t *testing.T) {
	st := openTestStore(t)

	raw := json.RawMessage(`{"test": true}`)
	if err := st.SaveUsageSnapshot(50.0, 25.0, "2026-03-20T15:00:00Z", "2026-03-25T00:00:00Z", raw); err != nil {
		t.Fatalf("SaveUsageSnapshot: %v", err)
	}

	var count int
	err := st.db.QueryRow("SELECT COUNT(*) FROM usage_snapshots").Scan(&count)
	if err != nil {
		t.Fatalf("query snapshot count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 snapshot, got %d", count)
	}

	var sessionPct float64
	err = st.db.QueryRow("SELECT session_pct FROM usage_snapshots LIMIT 1").Scan(&sessionPct)
	if err != nil {
		t.Fatalf("query session_pct: %v", err)
	}
	if sessionPct != 50.0 {
		t.Errorf("session_pct = %v, want 50.0", sessionPct)
	}
}

func TestGetHistoryRespectsDays(t *testing.T) {
	st := openTestStore(t)

	// Insert an old entry that should be excluded with days=1
	st.UpsertDailyAggregate(DailyAggregate{Date: "2020-01-01", InputTokens: 100})

	history, _ := st.GetHistory(1)
	if len(history) != 0 {
		t.Errorf("expected 0 entries for days=1, got %d", len(history))
	}

	history, _ = st.GetHistory(99999)
	if len(history) != 1 {
		t.Errorf("expected 1 entry for large days range, got %d", len(history))
	}
}
