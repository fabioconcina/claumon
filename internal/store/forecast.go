package store

import (
	"fmt"
	"time"
)

// ForecastSnapshot is one snapshot in a session/weekly window, used by the
// forecast package.
type ForecastSnapshot struct {
	Time time.Time
	U    float64 // utilization as a fraction in [0, 1]
}

// ForecastSession is one completed past window: its reset time, final
// utilization, and snapshots.
type ForecastSession struct {
	ResetAt   time.Time
	UFinal    float64
	Snapshots []ForecastSnapshot
}

// pctToFraction converts the stored 0-100 percent value to the 0-1 fraction
// the forecast model uses.
func pctToFraction(p float64) float64 {
	return p / 100.0
}

// gaugeColumns maps a gauge ("session" or "weekly") to its percent and
// reset-at column names in usage_snapshots.
func gaugeColumns(gauge string) (pctCol, resetCol string, err error) {
	switch gauge {
	case "session":
		return "session_pct", "session_reset_at", nil
	case "weekly":
		return "weekly_pct", "weekly_reset_at", nil
	default:
		return "", "", fmt.Errorf("unknown gauge: %s", gauge)
	}
}

// canonicalResetLayout is the canonical form for reset_at strings stored in
// the DB: zero fractional seconds, UTC zone. All snapshots from one window
// land on the same string so GROUP BY matches.
const canonicalResetLayout = "2006-01-02T15:04:05Z"

// NormalizeResetAt rounds an RFC3339 reset timestamp to the nearest minute
// and returns it in the canonical form.
//
// The Anthropic API recomputes reset_at as (now + remaining) on each poll,
// so the same nominal window drifts sub-second across polls and occasionally
// straddles a minute boundary. Without normalization, GROUP BY shatters every
// window into singletons, the forecast prior never has enough snapshots per
// window, and the feature silently produces nothing.
//
// Empty input returns empty. Unparseable input is passed through unchanged so
// the call is safe at API boundaries.
func NormalizeResetAt(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	t = t.UTC().Add(30 * time.Second).Truncate(time.Minute)
	return t.Format(canonicalResetLayout)
}

// GetWindowSnapshots returns snapshots within the current open window for the
// given gauge (sessionReset or weeklyReset RFC3339 string), since `since`.
// Used per-poll to feed the OLS recency fit.
//
// gauge is either "session" or "weekly". Snapshots are returned in ascending
// time order.
//
// Note: matching is by exact string equality on the reset_at column. This
// relies on the Anthropic API returning the same RFC3339 string for the same
// window across polls. If the format ever drifts mid-window (e.g. trailing
// "Z" vs "+00:00"), windows will split; normalize at write time to harden.
func (s *Store) GetWindowSnapshots(gauge, resetAt string, since time.Time) ([]ForecastSnapshot, error) {
	pctCol, resetCol, err := gaugeColumns(gauge)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`SELECT timestamp, %s FROM usage_snapshots
		WHERE %s = ? AND timestamp >= ?
		ORDER BY timestamp ASC`, pctCol, resetCol)
	rows, err := s.db.Query(q, NormalizeResetAt(resetAt), since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("query window snapshots: %w", err)
	}
	defer rows.Close()

	var out []ForecastSnapshot
	for rows.Next() {
		var ts string
		var pct float64
		if err := rows.Scan(&ts, &pct); err != nil {
			return nil, fmt.Errorf("scan window snapshot: %w", err)
		}
		t, err := parseSnapshotTime(ts)
		if err != nil {
			continue
		}
		out = append(out, ForecastSnapshot{Time: t, U: pctToFraction(pct)})
	}
	return out, rows.Err()
}

// GetCompletedSessions returns completed past windows for the given gauge:
// distinct reset_at values strictly before `before`, with the maximum
// observed utilization and all snapshots in that window. Used to fit the
// prior and to calibrate sigma_session.
//
// Newest sessions come first. limit caps the count (0 means no limit).
func (s *Store) GetCompletedSessions(gauge string, before time.Time, limit int) ([]ForecastSession, error) {
	pctCol, resetCol, err := gaugeColumns(gauge)
	if err != nil {
		return nil, err
	}

	beforeStr := before.UTC().Format(time.RFC3339)
	q := fmt.Sprintf(`SELECT %s AS reset_at, MAX(%s) AS u_final
		FROM usage_snapshots
		WHERE %s != '' AND %s < ?
		GROUP BY %s
		ORDER BY %s DESC`, resetCol, pctCol, resetCol, resetCol, resetCol, resetCol)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q, beforeStr)
	if err != nil {
		return nil, fmt.Errorf("query completed sessions: %w", err)
	}
	defer rows.Close()

	type meta struct {
		reset  string
		final  float64
		parsed time.Time
	}
	var metas []meta
	for rows.Next() {
		var m meta
		if err := rows.Scan(&m.reset, &m.final); err != nil {
			return nil, fmt.Errorf("scan completed session: %w", err)
		}
		t, err := time.Parse(time.RFC3339, m.reset)
		if err != nil {
			continue
		}
		m.parsed = t
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completed sessions: %w", err)
	}

	out := make([]ForecastSession, 0, len(metas))
	snapQ := fmt.Sprintf(`SELECT timestamp, %s FROM usage_snapshots
		WHERE %s = ? ORDER BY timestamp ASC`, pctCol, resetCol)
	for _, m := range metas {
		srows, err := s.db.Query(snapQ, m.reset)
		if err != nil {
			return nil, fmt.Errorf("query session snapshots: %w", err)
		}
		var snaps []ForecastSnapshot
		for srows.Next() {
			var ts string
			var pct float64
			if err := srows.Scan(&ts, &pct); err != nil {
				srows.Close()
				return nil, fmt.Errorf("scan session snapshot: %w", err)
			}
			t, err := parseSnapshotTime(ts)
			if err != nil {
				continue
			}
			snaps = append(snaps, ForecastSnapshot{Time: t, U: pctToFraction(pct)})
		}
		srows.Close()
		if len(snaps) < 2 {
			continue
		}
		out = append(out, ForecastSession{
			ResetAt:   m.parsed,
			UFinal:    pctToFraction(m.final),
			Snapshots: snaps,
		})
	}
	return out, nil
}

// parseSnapshotTime parses the stored DATETIME column. SQLite returns
// "YYYY-MM-DD HH:MM:SS" for the default CURRENT_TIMESTAMP.
func parseSnapshotTime(s string) (time.Time, error) {
	// Try the SQLite default format first.
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	// Fall back to RFC3339 in case the driver returns it that way.
	return time.Parse(time.RFC3339, s)
}
