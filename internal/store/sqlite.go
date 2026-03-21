package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

	db, err := sql.Open("sqlite", dbPath)
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
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveUsageSnapshot(sessionPct, weeklyPct float64, sessionReset, weeklyReset string, rawJSON json.RawMessage) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_snapshots (session_pct, weekly_pct, session_reset_at, weekly_reset_at, raw_json)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionPct, weeklyPct, sessionReset, weeklyReset, string(rawJSON),
	)
	return err
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
	return err
}

func (s *Store) GetHistory(days int) ([]DailyAggregate, error) {
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := s.db.Query(
		`SELECT date, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, session_count, message_count
		 FROM daily_aggregates WHERE date >= ? ORDER BY date ASC`, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DailyAggregate
	for rows.Next() {
		var a DailyAggregate
		if err := rows.Scan(&a.Date, &a.InputTokens, &a.OutputTokens, &a.CacheReadTokens, &a.CacheCreateTokens, &a.CostUSD, &a.SessionCount, &a.MessageCount); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *Store) GetTodaySummary() (*DailyAggregate, error) {
	today := time.Now().Format("2006-01-02")
	var a DailyAggregate
	err := s.db.QueryRow(
		`SELECT date, input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, cost_usd, session_count, message_count
		 FROM daily_aggregates WHERE date = ?`, today,
	).Scan(&a.Date, &a.InputTokens, &a.OutputTokens, &a.CacheReadTokens, &a.CacheCreateTokens, &a.CostUSD, &a.SessionCount, &a.MessageCount)
	if err == sql.ErrNoRows {
		return &DailyAggregate{Date: today}, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

