package limitshare

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

const window = 5 * time.Hour

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAttribute(t *testing.T) {
	// A window that started at t0 and resets at t0+5h.
	reset := t0.Add(window)
	nextReset := reset.Add(window)

	tests := []struct {
		name         string
		snaps        []Snapshot
		events       []Event
		eventsFrom   time.Time
		want         map[string]float64
		unattributed float64
	}{
		{
			name: "initial window split by cost",
			snaps: []Snapshot{
				{Time: at(30), Pct: 10, ResetAt: reset},
			},
			events: []Event{
				{Session: "a", Time: at(5), Cost: 3},
				{Session: "b", Time: at(20), Cost: 1},
			},
			eventsFrom: t0.Add(-time.Hour),
			want:       map[string]float64{"a": 7.5, "b": 2.5},
		},
		{
			name: "zero-delta polls accumulate into the next increase",
			snaps: []Snapshot{
				{Time: at(10), Pct: 0, ResetAt: reset},
				{Time: at(12), Pct: 0, ResetAt: reset},
				{Time: at(14), Pct: 1, ResetAt: reset},
			},
			events: []Event{
				{Session: "a", Time: at(11), Cost: 1},
				{Session: "b", Time: at(13), Cost: 1},
			},
			eventsFrom: t0.Add(-time.Hour),
			want:       map[string]float64{"a": 0.5, "b": 0.5},
		},
		{
			name: "increase with no local events is unattributed",
			snaps: []Snapshot{
				{Time: at(10), Pct: 2, ResetAt: reset},
				{Time: at(12), Pct: 5, ResetAt: reset},
			},
			events: []Event{
				{Session: "a", Time: at(5), Cost: 1},
			},
			eventsFrom:   t0.Add(-time.Hour),
			want:         map[string]float64{"a": 2},
			unattributed: 3,
		},
		{
			name: "window start before event coverage discards the first partial segment",
			snaps: []Snapshot{
				{Time: at(60), Pct: 20, ResetAt: reset},
				{Time: at(62), Pct: 20, ResetAt: reset},
				{Time: at(64), Pct: 21, ResetAt: reset},
				{Time: at(66), Pct: 23, ResetAt: reset},
			},
			events: []Event{
				{Session: "old", Time: at(61), Cost: 5},
				{Session: "a", Time: at(65), Cost: 1},
			},
			eventsFrom: at(30),
			want:       map[string]float64{"a": 2},
		},
		{
			name: "rollover attributes the new window only to events after its start",
			snaps: []Snapshot{
				{Time: at(290), Pct: 50, ResetAt: reset},
				{Time: at(310), Pct: 4, ResetAt: nextReset},
			},
			events: []Event{
				{Session: "a", Time: at(250), Cost: 50},
				{Session: "old-tail", Time: at(295), Cost: 9},
				{Session: "b", Time: at(305), Cost: 1},
			},
			eventsFrom: t0.Add(-time.Hour),
			want:       map[string]float64{"a": 50, "b": 4},
		},
		{
			name: "reset drift within tolerance stays in the same window",
			snaps: []Snapshot{
				{Time: at(10), Pct: 2, ResetAt: reset},
				{Time: at(12), Pct: 3, ResetAt: reset.Add(time.Minute)},
			},
			events: []Event{
				{Session: "a", Time: at(5), Cost: 1},
				{Session: "b", Time: at(11), Cost: 1},
			},
			eventsFrom: t0.Add(-time.Hour),
			want:       map[string]float64{"a": 2, "b": 1},
		},
		{
			name: "window ends, then a new one starts after an idle gap",
			snaps: []Snapshot{
				{Time: at(10), Pct: 2, ResetAt: reset},
				{Time: at(400), Pct: 0},
				{Time: at(500), Pct: 3, ResetAt: at(480).Add(window)},
			},
			events: []Event{
				{Session: "a", Time: at(5), Cost: 1},
				{Session: "idle-gap", Time: at(450), Cost: 1},
				{Session: "b", Time: at(490), Cost: 1},
			},
			eventsFrom: t0.Add(-time.Hour),
			want:       map[string]float64{"a": 2, "b": 3},
		},
		{
			name:       "no snapshots",
			eventsFrom: t0,
			want:       map[string]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Attribute(tt.snaps, tt.events, window, tt.eventsFrom)
			if len(got.BySession) != len(tt.want) {
				t.Fatalf("BySession = %v, want %v", got.BySession, tt.want)
			}
			for s, w := range tt.want {
				if !approx(got.BySession[s], w) {
					t.Errorf("BySession[%q] = %v, want %v (all: %v)", s, got.BySession[s], w, got.BySession)
				}
			}
			if !approx(got.Unattributed, tt.unattributed) {
				t.Errorf("Unattributed = %v, want %v", got.Unattributed, tt.unattributed)
			}
		})
	}
}

func TestAttributeIgnoresEventOrder(t *testing.T) {
	reset := t0.Add(window)
	snaps := []Snapshot{{Time: at(30), Pct: 4, ResetAt: reset}}
	events := []Event{
		{Session: "b", Time: at(20), Cost: 1},
		{Session: "a", Time: at(5), Cost: 1},
	}
	got := Attribute(snaps, events, window, t0.Add(-time.Hour))
	if !approx(got.BySession["a"], 2) || !approx(got.BySession["b"], 2) {
		t.Errorf("BySession = %v, want a=2 b=2", got.BySession)
	}
	if events[0].Session != "b" {
		t.Error("Attribute must not reorder the caller's slice")
	}
}

func TestShare(t *testing.T) {
	reset := t0.Add(window)
	snaps := []Snapshot{
		{Time: at(10), Pct: 2, ResetAt: reset},
		{Time: at(20), Pct: 4, ResetAt: reset},
	}
	events := []Event{
		{Session: "a", Time: at(5), Cost: 1},
		{Session: "a", Time: at(15), Cost: 1},
		{Session: "b", Time: at(25), Cost: 1},
	}
	res := Attribute(snaps, events, window, t0.Add(-time.Hour))

	if got, ok := res.Share("a", []time.Time{at(5), at(15)}); !ok || !approx(got, 4) {
		t.Errorf("Share(a) = %v, %v, want 4, true", got, ok)
	}
	// b's only event came after the last poll: not reflected in the gauge yet.
	if _, ok := res.Share("b", []time.Time{at(25)}); ok {
		t.Error("Share(b) should have no estimate before a poll reflects it")
	}
	// A session that ran before the window claumon saw has no estimate.
	if _, ok := res.Share("c", []time.Time{t0.Add(-2 * time.Hour)}); ok {
		t.Error("Share(c) should have no estimate outside the covered spans")
	}
}

func TestShareMissedWindow(t *testing.T) {
	// claumon polled window 1, missed window 2 entirely (polling backed off),
	// and polled again in window 3.
	reset1 := t0.Add(window)
	start3 := t0.Add(2*window + time.Hour)
	snaps := []Snapshot{
		{Time: at(60), Pct: 5, ResetAt: reset1},
		{Time: start3.Add(30 * time.Minute), Pct: 3, ResetAt: start3.Add(window)},
	}
	events := []Event{
		{Session: "w1", Time: at(30), Cost: 1},
		{Session: "missed", Time: reset1.Add(time.Hour), Cost: 1},
		{Session: "w3", Time: start3.Add(10 * time.Minute), Cost: 1},
	}
	res := Attribute(snaps, events, window, t0.Add(-time.Hour))

	if _, ok := res.Share("missed", []time.Time{reset1.Add(time.Hour)}); ok {
		t.Error("session active only in the missed window should have no estimate, not 0%")
	}
	// A session spanning the gap is undercounted, so it gets no estimate either.
	if _, ok := res.Share("w1", []time.Time{at(30), reset1.Add(time.Hour)}); ok {
		t.Error("session with activity in the missed window should have no estimate")
	}
	if got, ok := res.Share("w1", []time.Time{at(30)}); !ok || !approx(got, 5) {
		t.Errorf("Share(w1) = %v, %v, want 5, true", got, ok)
	}
	if got, ok := res.Share("w3", []time.Time{start3.Add(10 * time.Minute)}); !ok || !approx(got, 3) {
		t.Errorf("Share(w3) = %v, %v, want 3, true", got, ok)
	}
}

func TestShareAcrossRegularRollover(t *testing.T) {
	// Polls a minute apart across a window change: the unpolled minute at the
	// end of the first window doesn't cost a long session its estimate.
	reset := t0.Add(window)
	snaps := []Snapshot{
		{Time: at(299), Pct: 40, ResetAt: reset},
		{Time: at(301), Pct: 1, ResetAt: reset.Add(window)},
	}
	events := []Event{
		{Session: "long", Time: at(100), Cost: 1},
		{Session: "long", Time: at(300), Cost: 1},
		{Session: "long", Time: at(300).Add(30 * time.Second), Cost: 1},
	}
	res := Attribute(snaps, events, window, t0.Add(-time.Hour))
	times := []time.Time{at(100), at(300), at(300).Add(30 * time.Second)}
	if got, ok := res.Share("long", times); !ok || !approx(got, 41) {
		t.Errorf("Share(long) = %v, %v, want 41, true", got, ok)
	}
}
