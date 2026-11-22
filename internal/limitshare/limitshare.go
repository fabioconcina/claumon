// Package limitshare estimates how much of a rate-limit window (the 5-hour
// session limit or the 7-day weekly limit) each Claude Code session used.
//
// The usage API only reports one aggregate utilization percentage per
// window, so per-session shares are an estimate: every increase observed
// between two usage snapshots is split across the local assistant messages
// sent since the previous increase, proportionally to their API-equivalent
// cost. Increases with no local messages behind them (claude.ai, another
// machine) are reported as unattributed.
package limitshare

import (
	"sort"
	"time"
)

// Snapshot is one poll of a single gauge.
type Snapshot struct {
	Time    time.Time
	Pct     float64   // utilization, 0-100
	ResetAt time.Time // zero when no window is active
}

// Event is one assistant message with its estimated cost.
type Event struct {
	Session string
	Time    time.Time
	Cost    float64
}

// Interval is the time span (From, To].
type Interval struct {
	From, To time.Time
}

func (iv Interval) contains(t time.Time) bool {
	return t.After(iv.From) && !t.After(iv.To)
}

// Result holds the estimated percentage points of the gauge used by each
// session.
type Result struct {
	BySession map[string]float64
	// Unattributed is usage that rose while no local session was active.
	Unattributed float64
	// Covered lists the spans in which every gauge increase was split across
	// the events behind it. Events outside them and before Through fell in a
	// gap (a window claumon never polled, or usage older than the loaded
	// events), so their share is unknown.
	Covered []Interval
	// Through is the time of the last snapshot. Events after it are not
	// reflected in the gauge yet.
	Through time.Time
}

// Share returns the estimated share of the session whose events happened at
// times. ok is false when any of them fell in a gap, or when none of them is
// reflected in the gauge yet: a partial sum would read as a measurement.
func (r Result) Share(session string, times []time.Time) (pct float64, ok bool) {
	counted := 0
	for _, t := range times {
		if t.After(r.Through) {
			continue
		}
		if !r.covers(t) {
			return 0, false
		}
		counted++
	}
	if counted == 0 {
		return 0, false
	}
	return r.BySession[session], true
}

func (r Result) covers(t time.Time) bool {
	i := sort.Search(len(r.Covered), func(i int) bool { return !r.Covered[i].To.Before(t) })
	return i < len(r.Covered) && r.Covered[i].contains(t)
}

const (
	// resetTolerance absorbs reset_at drift between polls of the same window
	// that minute normalization at write time does not catch.
	resetTolerance = 2 * time.Minute
	// windowStartSlack widens a window's derived start, since reset_at is
	// rounded to the minute.
	windowStartSlack = time.Minute
	// maxPollGap is the longest stretch without a snapshot that still counts
	// as covered across a window change. Usage in the unpolled tail of a
	// window is lost, which is negligible for a regular poll interval but not
	// for a missed window.
	maxPollGap = 15 * time.Minute
)

// Attribute splits the gauge increases in snaps across events. window is the
// gauge's length (5h or 7d). eventsFrom is how far back events were loaded:
// a window that started before it can't be attributed from its start, so
// attribution only begins at that window's next observed increase.
//
// Snapshots must be in ascending time order. events may be in any order.
func Attribute(snaps []Snapshot, events []Event, window time.Duration, eventsFrom time.Time) Result {
	res := Result{BySession: map[string]float64{}}
	if len(snaps) == 0 {
		return res
	}

	sorted := make([]Event, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	// distribute splits pct across events in (from, to].
	distribute := func(pct float64, from, to time.Time) {
		lo := sort.Search(len(sorted), func(i int) bool { return sorted[i].Time.After(from) })
		hi := sort.Search(len(sorted), func(i int) bool { return sorted[i].Time.After(to) })
		var total float64
		for _, e := range sorted[lo:hi] {
			total += e.Cost
		}
		if total <= 0 {
			res.Unattributed += pct
			return
		}
		for _, e := range sorted[lo:hi] {
			res.BySession[e.Session] += pct * e.Cost / total
		}
	}

	// segStart is the exclusive start of the usage not yet reflected in an
	// attributed increase. known is false while that start is unknown.
	var segStart time.Time
	known := false
	begin := func(t time.Time) {
		segStart = t
		known = true
		if n := len(res.Covered); n > 0 && t.Sub(res.Covered[n-1].To) <= maxPollGap {
			return
		}
		res.Covered = append(res.Covered, Interval{From: t, To: t})
	}
	// extend marks everything up to t as covered.
	extend := func(t time.Time) {
		if known {
			res.Covered[len(res.Covered)-1].To = t
		}
	}

	first := snaps[0]
	if !first.ResetAt.IsZero() {
		if start := first.ResetAt.Add(-window - windowStartSlack); !start.Before(eventsFrom) {
			begin(start)
			if first.Pct > 0 {
				distribute(first.Pct, segStart, first.Time)
				segStart = first.Time
			}
			extend(first.Time)
		}
	}

	for i := 1; i < len(snaps); i++ {
		prev, cur := snaps[i-1], snaps[i]

		if cur.ResetAt.IsZero() {
			// No active window: anything pending was already counted or lost
			// to rounding. The next window starts fresh.
			if cur.Time.Sub(prev.Time) <= maxPollGap {
				extend(cur.Time)
			}
			known = false
			continue
		}

		if isNewWindow(prev, cur, window) {
			start := cur.ResetAt.Add(-window - windowStartSlack)
			if start.Before(prev.Time) {
				start = prev.Time
			}
			begin(start)
			if cur.Pct > 0 {
				distribute(cur.Pct, segStart, cur.Time)
				segStart = cur.Time
			}
			extend(cur.Time)
			continue
		}

		delta := cur.Pct - prev.Pct
		if delta > 0 && !known {
			// The usage behind this increase may predate the loaded events.
			begin(cur.Time)
			continue
		}
		if delta > 0 {
			distribute(delta, segStart, cur.Time)
			segStart = cur.Time
		}
		extend(cur.Time)
	}

	res.Through = snaps[len(snaps)-1].Time
	return res
}

// isNewWindow reports whether cur belongs to a different window than prev.
func isNewWindow(prev, cur Snapshot, window time.Duration) bool {
	if prev.ResetAt.IsZero() {
		return true
	}
	diff := cur.ResetAt.Sub(prev.ResetAt)
	if diff < 0 {
		diff = -diff
	}
	if diff <= resetTolerance {
		return false
	}
	// Windows don't overlap: one active at prev.Time can't be followed by one
	// that started before it. Treat that as reset_at drift.
	start := cur.ResetAt.Add(-window)
	return !start.Before(prev.Time.Add(-windowStartSlack))
}
