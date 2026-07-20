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

// aggregateCount returns the number of rows persisted in daily_aggregates,
// independent of GetHistory's zero-filled, windowed view.
func aggregateCount(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM daily_aggregates").Scan(&n); err != nil {
		t.Fatalf("count aggregates: %v", err)
	}
	return n
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
	// GetHistory returns a continuous series of `days` entries ending today,
	// zero-filling idle days. Today's entry is the last one.
	if len(history) != 30 {
		t.Fatalf("expected 30 continuous entries, got %d", len(history))
	}
	last := history[len(history)-1]
	if last.Date != today {
		t.Errorf("last entry date = %s, want today %s", last.Date, today)
	}
	if last.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", last.InputTokens)
	}
	if last.CostUSD != 0.05 {
		t.Errorf("CostUSD = %f, want 0.05", last.CostUSD)
	}
}

func TestUpsertOverwrites(t *testing.T) {
	st := openTestStore(t)

	today := time.Now().Format("2006-01-02")
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 100})
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 999})

	history, _ := st.GetHistory(30)
	if len(history) != 30 {
		t.Fatalf("expected 30 continuous entries, got %d", len(history))
	}
	last := history[len(history)-1]
	if last.InputTokens != 999 {
		t.Errorf("InputTokens = %d, want 999 (upsert should overwrite)", last.InputTokens)
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

func TestLatestSnapshotTime(t *testing.T) {
	st := openTestStore(t)

	// No snapshots yet: ok is false, no error.
	if _, ok, err := st.LatestSnapshotTime(); err != nil || ok {
		t.Fatalf("LatestSnapshotTime() on empty store = ok %v, err %v; want ok false, nil", ok, err)
	}

	raw := json.RawMessage(`{}`)
	if err := st.SaveUsageSnapshot(10, 20, "", "", raw); err != nil {
		t.Fatalf("SaveUsageSnapshot: %v", err)
	}

	before := time.Now().UTC()
	last, ok, err := st.LatestSnapshotTime()
	if err != nil || !ok {
		t.Fatalf("LatestSnapshotTime() = ok %v, err %v; want ok true, nil", ok, err)
	}
	// CURRENT_TIMESTAMP has second precision, so the stored time can round down
	// to just before the save; allow a couple of seconds of slack either way.
	if diff := before.Sub(last); diff > 3*time.Second || diff < -3*time.Second {
		t.Errorf("latest snapshot time %v is not within 3s of now %v (diff %v)", last, before, diff)
	}
}

func TestGetHistoryRespectsDays(t *testing.T) {
	st := openTestStore(t)

	// An out-of-window entry must not leak into the series.
	st.UpsertDailyAggregate(DailyAggregate{Date: "2020-01-01", InputTokens: 100})
	today := time.Now().Format("2006-01-02")
	st.UpsertDailyAggregate(DailyAggregate{Date: today, InputTokens: 42})

	history, _ := st.GetHistory(7)
	if len(history) != 7 {
		t.Fatalf("expected 7 continuous entries, got %d", len(history))
	}
	for _, h := range history {
		if h.Date == "2020-01-01" {
			t.Errorf("out-of-window date leaked into history")
		}
	}
	last := history[len(history)-1]
	if last.Date != today || last.InputTokens != 42 {
		t.Errorf("last entry = {%s, %d}, want {%s, 42}", last.Date, last.InputTokens, today)
	}

	// days=1 yields exactly today, zero-filled when no usage recorded.
	one, _ := st.GetHistory(1)
	if len(one) != 1 || one[0].Date != today {
		t.Errorf("GetHistory(1) = %+v, want single entry for today %s", one, today)
	}
}

func TestGetHistoryZeroFillsGaps(t *testing.T) {
	st := openTestStore(t)

	now := time.Now()
	d0 := now.Format("2006-01-02")
	d2 := now.AddDate(0, 0, -2).Format("2006-01-02")
	d3 := now.AddDate(0, 0, -3).Format("2006-01-02")
	// Populate today and 3 days ago; leave the days in between empty.
	st.UpsertDailyAggregate(DailyAggregate{Date: d0, InputTokens: 10})
	st.UpsertDailyAggregate(DailyAggregate{Date: d3, InputTokens: 30})

	history, _ := st.GetHistory(5)
	if len(history) != 5 {
		t.Fatalf("expected 5 continuous days, got %d", len(history))
	}

	// Dates must be strictly consecutive ascending, no gaps.
	for i := 1; i < len(history); i++ {
		prev, _ := time.Parse("2006-01-02", history[i-1].Date)
		cur, _ := time.Parse("2006-01-02", history[i].Date)
		if !cur.Equal(prev.AddDate(0, 0, 1)) {
			t.Errorf("dates not continuous at %d: %s then %s", i, history[i-1].Date, history[i].Date)
		}
	}

	byDate := make(map[string]DailyAggregate, len(history))
	for _, h := range history {
		byDate[h.Date] = h
	}
	if byDate[d0].InputTokens != 10 {
		t.Errorf("today input = %d, want 10", byDate[d0].InputTokens)
	}
	if byDate[d3].InputTokens != 30 {
		t.Errorf("d-3 input = %d, want 30", byDate[d3].InputTokens)
	}
	if byDate[d2].InputTokens != 0 {
		t.Errorf("gap day d-2 input = %d, want 0 (zero-filled)", byDate[d2].InputTokens)
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
	if c := aggregateCount(t, st); c != 1 {
		t.Fatalf("expected 1 aggregate after prune, got %d", c)
	}
	var remaining string
	st.db.QueryRow("SELECT date FROM daily_aggregates").Scan(&remaining)
	if remaining != today {
		t.Errorf("remaining aggregate date = %s, want %s", remaining, today)
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

	if c := aggregateCount(t, st); c != 3 {
		t.Errorf("expected all 3 aggregates to survive prune, got %d", c)
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

	if c := aggregateCount(t, st); c != 1 {
		t.Fatalf("expected 1 aggregate after prune(7), got %d", c)
	}
	var remaining string
	st.db.QueryRow("SELECT date FROM daily_aggregates").Scan(&remaining)
	if remaining != recent {
		t.Errorf("remaining aggregate date = %s, want %s", remaining, recent)
	}

	var count int
	st.db.QueryRow("SELECT COUNT(*) FROM usage_snapshots").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 snapshot after prune(7), got %d", count)
	}
}
