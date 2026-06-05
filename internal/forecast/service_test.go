package forecast

import (
	"testing"
	"time"
)

// stubStore returns a fixed snapshot/session set, filtering snapshots by the
// `since` cutoff the way the real store does so the two service entry points
// see the same window-relative data.
type stubStore struct {
	snaps    []StoreSnapshot
	sessions []StoreSession
}

func (s *stubStore) GetWindowSnapshots(_, _ string, since time.Time) ([]StoreSnapshot, error) {
	out := make([]StoreSnapshot, 0, len(s.snaps))
	for _, sn := range s.snaps {
		if !sn.Time.Before(since) {
			out = append(out, sn)
		}
	}
	return out, nil
}

func (s *stubStore) GetCompletedSessions(_ string, _ time.Time, _ int) ([]StoreSession, error) {
	return s.sessions, nil
}

// TestSampleForCIMatchesForecastFor is a regression test for the v2.0 CI drift:
// the gauge line (ForecastFor -> Run) reports the monotone MC terminal p10/p90,
// while the modal footer (SampleFor) used to ship ProjectForecast's leftover
// symmetric z-interval. For identical inputs both must report the same 80% CI,
// since both derive it from the same deterministic MC. The threshold is part of
// the MC seed, so both surfaces must pass UIThresholdPct (the popup once
// hardcoded a different value, which silently reseeded the MC and split the CI).
func TestSampleForCIMatchesForecastFor(t *testing.T) {
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	resetAt := reset.Format(time.RFC3339)

	// Monotone snapshots inside the recency window so both entry points fit the
	// same posterior regardless of their differing `since` cutoffs.
	snaps := []StoreSnapshot{
		{Time: now.Add(-30 * time.Minute), U: 0.40},
		{Time: now.Add(-20 * time.Minute), U: 0.44},
		{Time: now.Add(-10 * time.Minute), U: 0.47},
		{Time: now, U: 0.50},
	}
	svc := NewService(&stubStore{snaps: snaps}, DefaultConfig())
	svc.states[GaugeSession] = State{
		Prior:       Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20},
		Calibration: Calibration{SigmaSessionSq: 2.5e-3, BarTauSq: 3.6e-3},
		FitAt:       now,
	}

	const uNowPct, thresholdPct = 50.0, UIThresholdPct

	res, ok := svc.ForecastFor(GaugeSession, resetAt, uNowPct, now, []float64{thresholdPct})
	if !ok {
		t.Fatal("ForecastFor returned ok=false")
	}
	payload, ok := svc.SampleFor(GaugeSession, resetAt, uNowPct, now, thresholdPct, 50, 60)
	if !ok {
		t.Fatal("SampleFor returned ok=false")
	}

	if payload.CILo != res.Forecast.Lower {
		t.Errorf("CI lower mismatch: modal=%.6f gauge=%.6f", payload.CILo, res.Forecast.Lower)
	}
	if payload.CIHi != res.Forecast.Upper {
		t.Errorf("CI upper mismatch: modal=%.6f gauge=%.6f", payload.CIHi, res.Forecast.Upper)
	}

	// Sanity: it's a real MC interval, not a degenerate fallback. The lower
	// bound is floored at u_now by the monotone process.
	uNow := uNowPct / 100.0
	if !(res.Forecast.Lower >= uNow && res.Forecast.Lower < res.Forecast.Upper) {
		t.Errorf("expected u_now <= lower < upper, got lower=%.6f upper=%.6f u_now=%.2f",
			res.Forecast.Lower, res.Forecast.Upper, uNow)
	}
}
