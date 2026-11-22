package server

import (
	"log"
	"math"
	"net/http"
	"time"

	"github.com/fabioconcina/claumon/internal/limitshare"
	"github.com/fabioconcina/claumon/internal/parser"
)

const (
	sessionWindow = 5 * time.Hour
	weeklyWindow  = 7 * 24 * time.Hour
)

// LimitShare is a session's estimated share of the 5-hour and weekly limits,
// in percentage points. A nil field means there is no estimate.
type LimitShare struct {
	SessionPct *float64 `json:"session_pct"`
	WeeklyPct  *float64 `json:"weekly_pct"`
}

// HandleSessionLimitShare estimates how much of each limit one session used.
// It runs on demand, when the session detail panel opens, because it reads
// every session file active around the session.
func (h *Handlers) HandleSessionLimitShare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := parser.FindSessionFile(h.claudeDir, id)
	if path == "" {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}
	own, err := parser.SessionCostEvents(path)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, h.limitShare(id, own))
}

func (h *Handlers) limitShare(id string, own []parser.CostEvent) LimitShare {
	var out LimitShare
	if h.store == nil || len(own) == 0 {
		return out
	}

	times := make([]time.Time, len(own))
	first, last := own[0].Time, own[0].Time
	for i, e := range own {
		times[i] = e.Time
		if e.Time.Before(first) {
			first = e.Time
		}
		if e.Time.After(last) {
			last = e.Time
		}
	}

	// A window the session ran in opened at most one window before its first
	// message, and its last message shows up in the gauge before that
	// window ends. A window open at the first snapshot loaded can itself
	// have opened a full window earlier, so events go back one more.
	eventsFrom := first.Add(-2 * weeklyWindow)
	byID, err := parser.CostEventsBetween(h.claudeDir, eventsFrom, last.Add(weeklyWindow))
	if err != nil {
		log.Printf("[limitshare] Loading cost events: %v", err)
		return out
	}
	var events []limitshare.Event
	for sid, evs := range byID {
		for _, e := range evs {
			events = append(events, limitshare.Event{Session: sid, Time: e.Time, Cost: e.CostUSD})
		}
	}

	gauges := []struct {
		name   string
		window time.Duration
		dst    **float64
	}{
		{"session", sessionWindow, &out.SessionPct},
		{"weekly", weeklyWindow, &out.WeeklyPct},
	}
	for _, g := range gauges {
		stored, err := h.store.GetGaugeSnapshots(g.name, first.Add(-g.window), last.Add(g.window))
		if err != nil {
			log.Printf("[limitshare] Loading %s snapshots: %v", g.name, err)
			continue
		}
		snaps := make([]limitshare.Snapshot, len(stored))
		for i, st := range stored {
			snaps[i] = limitshare.Snapshot{Time: st.Time, Pct: st.Pct}
			if st.ResetAt != "" {
				if t, err := time.Parse(time.RFC3339, st.ResetAt); err == nil {
					snaps[i].ResetAt = t
				}
			}
		}

		res := limitshare.Attribute(snaps, events, g.window, eventsFrom)
		if pct, ok := res.Share(id, times); ok {
			v := math.Round(pct*100) / 100
			*g.dst = &v
		}
	}
	return out
}
