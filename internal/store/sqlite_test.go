package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
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

	today := time.Now().Format("2006-01-02")
	agg := DailyAggregate{
		Date:         today,
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

	today := time.Now().Format("2006-01-02")
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 100})
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 999})

	history, _ := st.GetHistory(30)
	if len(history) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(history))
	}
	if history[0].InputTokens != 999 {
		t.Errorf("InputTokens = %d, want 999 (upsert should overwrite)", history[0].InputTokens)
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

func TestPruneRemovesOldData(t *testing.T) {
	st := openTestStore(t)

	// Insert old aggregate
	st.UpsertDailyAggregate(DailyAggregate{Date: "2020-01-01", InputTokens: 100})
	// Insert recent aggregate
	today := time.Now().Format("2006-01-02")
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 200})

	// Insert old snapshot (manually set timestamp)
	st.db.Exec("INSERT INTO usage_snapshots (timestamp, session_pct, weekly_pct) VALUES ('2020-01-01 00:00:00', 10, 20)")
	// Insert recent snapshot
	st.SaveUsageSnapshot(50.0, 25.0, "", "", json.RawMessage(`{}`))

	if err := st.Prune(90); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// Old aggregate should be gone, recent should remain
	history, _ := st.GetHistory(99999)
	if len(history) != 1 {
		t.Fatalf("expected 1 aggregate after prune, got %d", len(history))
	}
	if history[0].Date != today {
		t.Errorf("remaining aggregate date = %s, want %s", history[0].Date, today)
	}

	// Old snapshot should be gone, recent should remain
	var count int
	st.db.QueryRow("SELECT COUNT(*) FROM usage_snapshots").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 snapshot after prune, got %d", count)
	}
}

func TestPruneKeepsAllWithinRetention(t *testing.T) {
	st := openTestStore(t)

	// Insert aggregates for the last 3 days
	for i := 0; i < 3; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		st.UpsertDailyAggregate(DailyAggregate{Date: date, InputTokens: i * 100})
	}

	if err := st.Prune(90); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	history, _ := st.GetHistory(99999)
	if len(history) != 3 {
		t.Errorf("expected all 3 aggregates to survive prune, got %d", len(history))
	}
}

func TestPruneRespectsRetentionDays(t *testing.T) {
	st := openTestStore(t)

	// Insert one aggregate at exactly 10 days ago, one at 5 days ago
	old := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	recent := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	st.UpsertDailyAggregate(DailyAggregate{Date: old, InputTokens: 100})
	st.UpsertDailyAggregate(DailyAggregate{Date: recent, InputTokens: 200})

	// Insert matching snapshots
	st.db.Exec(fmt.Sprintf("INSERT INTO usage_snapshots (timestamp, session_pct, weekly_pct) VALUES ('%s 12:00:00', 10, 20)", old))
	st.db.Exec(fmt.Sprintf("INSERT INTO usage_snapshots (timestamp, session_pct, weekly_pct) VALUES ('%s 12:00:00', 30, 40)", recent))

	// Prune with 7-day retention: should remove the 10-day-old entries
	if err := st.Prune(7); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	history, _ := st.GetHistory(99999)
	if len(history) != 1 {
		t.Fatalf("expected 1 aggregate after prune(7), got %d", len(history))
	}
	if history[0].Date != recent {
		t.Errorf("remaining aggregate date = %s, want %s", history[0].Date, recent)
	}

	var count int
	st.db.QueryRow("SELECT COUNT(*) FROM usage_snapshots").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 snapshot after prune(7), got %d", count)
	}
}
