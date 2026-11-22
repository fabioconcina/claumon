package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type DailyAggregate struct {
	Date              string  `json:"date"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	CacheCreateTokens int     `json:"cache_create_tokens"`
	CostUSD           float64 `json:"cost_usd"`
	SessionCount      int     `json:"session_count"`
	MessageCount      int     `json:"message_count"`
}

func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	// busy_timeout makes a write wait for a concurrent writer instead of
	// failing at once with SQLITE_BUSY (startup runs the backfill, the daily
	// aggregate, the usage poll and the prune together). Set in the DSN so it
	// applies to every pooled connection, not just the one a PRAGMA hits.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS usage_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			session_pct REAL,
			weekly_pct REAL,
			session_reset_at TEXT,
			weekly_reset_at TEXT,
			raw_json TEXT
		);
		CREATE TABLE IF NOT EXISTS daily_aggregates (
			date TEXT PRIMARY KEY,
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0,
			cache_create_tokens INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			session_count INTEGER DEFAULT 0,
			message_count INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_snapshots_timestamp ON usage_snapshots(timestamp);
	`)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	if err := canonicalizeResetAts(db); err != nil {
		return fmt.Errorf("canonicalizing reset_at: %w", err)
	}
	return nil
}

// canonicalizeResetAts rewrites any reset_at strings that aren't in the
// canonical 1-minute-rounded UTC form. Idempotent — already-canonical rows
// don't match the predicate. Necessary because pre-fix data has sub-second
// drift across polls; see NormalizeResetAt for the why.
func canonicalizeResetAts(db *sql.DB) error {
	// Only touch rows whose reset_at isn't already canonical (length == 20 and
	// ends in "Z" with ":00" seconds). Anything else gets read, normalized,
	// written back.
	rows, err := db.Query(`SELECT id, session_reset_at, weekly_reset_at FROM usage_snapshots
		WHERE (session_reset_at != '' AND session_reset_at NOT GLOB '????-??-??T??:??:00Z')
		   OR (weekly_reset_at  != '' AND weekly_reset_at  NOT GLOB '????-??-??T??:??:00Z')`)
	if err != nil {
		return err
	}
	type update struct {
		id      int64
		session string
		weekly  string
	}
	var updates []update
	for rows.Next() {
		var id int64
		var sess, wk string
		if err := rows.Scan(&id, &sess, &wk); err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, update{id, NormalizeResetAt(sess), NormalizeResetAt(wk)})
	}
	rows.Close()
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`UPDATE usage_snapshots SET session_reset_at = ?, weekly_reset_at = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, u := range updates {
		if _, err := stmt.Exec(u.session, u.weekly, u.id); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("[migrate] canonicalized %d reset_at rows", len(updates))
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveUsageSnapshot(sessionPct, weeklyPct float64, sessionReset, weeklyReset string, rawJSON json.RawMessage) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_snapshots (session_pct, weekly_pct, session_reset_at, weekly_reset_at, raw_json)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionPct, weeklyPct,
		NormalizeResetAt(sessionReset), NormalizeResetAt(weeklyReset),
		string(rawJSON),
	)
	if err != nil {
		return fmt.Errorf("saving usage snapshot: %w", err)
	}
	return nil
}

// LatestSnapshotTime returns the timestamp (UTC) of the most recent usage
// snapshot. ok is false when no snapshot has ever been saved.
func (s *Store) LatestSnapshotTime() (t time.Time, ok bool, err error) {
	var ts string
	err = s.db.QueryRow(`SELECT timestamp FROM usage_snapshots ORDER BY timestamp DESC LIMIT 1`).Scan(&ts)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading latest snapshot time: %w", err)
	}
	t, err = parseSnapshotTime(ts)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parsing latest snapshot time %q: %w", ts, err)
	}
	return t, true, nil
}

// GaugeSnapshot is one stored poll of a single gauge.
type GaugeSnapshot struct {
	Time    time.Time
	Pct     float64
	ResetAt string // canonical reset_at, empty when no window was active
}

// GetGaugeSnapshots returns the snapshots of the given gauge ("session" or
// "weekly") taken in [since, until], in ascending time order.
func (s *Store) GetGaugeSnapshots(gauge string, since, until time.Time) ([]GaugeSnapshot, error) {
	pctCol, resetCol, err := gaugeColumns(gauge)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`SELECT timestamp, COALESCE(%s, 0), COALESCE(%s, '') FROM usage_snapshots
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC`, pctCol, resetCol)
	const layout = "2006-01-02 15:04:05"
	rows, err := s.db.Query(q, since.UTC().Format(layout), until.UTC().Format(layout))
	if err != nil {
		return nil, fmt.Errorf("query gauge snapshots: %w", err)
	}
	defer rows.Close()

	var out []GaugeSnapshot
	for rows.Next() {
		var ts string
		var snap GaugeSnapshot
		if err := rows.Scan(&ts, &snap.Pct, &snap.ResetAt); err != nil {
			return nil, fmt.Errorf("scan gauge snapshot: %w", err)
		}
		t, err := parseSnapshotTime(ts)
		if err != nil {
			continue
		}
		snap.Time = t
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *Store) UpsertDailyAggregate(agg DailyAggregate) error {
	_, err := s.db.Exec(`
		INSERT INTO daily_aggregates (date, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, session_count, message_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cache_read_tokens = excluded.cache_read_tokens,
			cache_create_tokens = excluded.cache_create_tokens,
			cost_usd = excluded.cost_usd,
			session_count = excluded.session_count,
			message_count = excluded.message_count
	`, agg.Date, agg.InputTokens, agg.OutputTokens, agg.CacheReadTokens, agg.CacheCreateTokens, agg.CostUSD, agg.SessionCount, agg.MessageCount)
	if err != nil {
		return fmt.Errorf("upserting daily aggregate for %s: %w", agg.Date, err)
	}
	return nil
}

func (s *Store) Prune(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02")

	res, err := s.db.Exec("DELETE FROM usage_snapshots WHERE timestamp < ?", cutoff)
	if err != nil {
		return fmt.Errorf("pruning snapshots: %w", err)
	}
	snapshots, _ := res.RowsAffected()

	res, err = s.db.Exec("DELETE FROM daily_aggregates WHERE date < ?", cutoff)
	if err != nil {
		return fmt.Errorf("pruning aggregates: %w", err)
	}
	aggregates, _ := res.RowsAffected()

	if snapshots > 0 || aggregates > 0 {
		log.Printf("[prune] Removed %d snapshots and %d aggregates older than %s", snapshots, aggregates, cutoff)
	}
	return nil
}

func (s *Store) GetHistory(days int) ([]DailyAggregate, error) {
	now := time.Now()
	// Inclusive window of `days` calendar days ending today.
	since := now.AddDate(0, 0, -(days - 1))
	rows, err := s.db.Query(
		`SELECT date, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, session_count, message_count
		 FROM daily_aggregates WHERE date >= ? ORDER BY date ASC`, since.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	byDate := make(map[string]DailyAggregate)
	for rows.Next() {
		var a DailyAggregate
		if err := rows.Scan(&a.Date, &a.InputTokens, &a.OutputTokens, &a.CacheReadTokens, &a.CacheCreateTokens, &a.CostUSD, &a.SessionCount, &a.MessageCount); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		byDate[a.Date] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating history rows: %w", err)
	}

	// Zero-fill the continuous date range from `since` to today so idle days
	// render as empty bars instead of collapsing the calendar. Days bucket by
	// local date, matching how the parser aggregates sessions.
	result := make([]DailyAggregate, 0, days)
	for d := since; !d.After(now); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		if a, ok := byDate[date]; ok {
			result = append(result, a)
		} else {
			result = append(result, DailyAggregate{Date: date})
		}
	}
	return result, nil
}
