package server

import (
	"testing"

	"github.com/fabioconcina/claumon/internal/forecast"
)

// TestApplyCachedHeadline verifies the popup's headline (projected %, 80% CI,
// matching-threshold ETA) is replaced with the gauge's cached forecast, so the
// two surfaces report identical numbers instead of two independent simulations.
func TestApplyCachedHeadline(t *testing.T) {
	// Freshly simulated sample: deliberately different from the cached gauge
	// values to prove the override actually takes effect.
	s := forecast.SamplePayload{
		F:            0.61,
		CILo:         0.55,
		CIHi:         0.88,
		ThresholdPct: forecast.UIThresholdPct,
		ETA: forecast.ETAPayload{
			ThresholdPct: forecast.UIThresholdPct,
			Median:       "2026-06-05T20:00:00Z",
		},
	}
	cached := forecast.Payload{
		ProjectedPct: 64.0,
		Lower80Pct:   60.0,
		Upper80Pct:   92.0,
		ETAs: []forecast.ETAPayload{
			{ThresholdPct: forecast.UIThresholdPct, Median: "2026-06-05T21:30:00Z"},
		},
	}

	got := applyCachedHeadline(s, cached)

	if got.F != 0.64 {
		t.Errorf("F = %v, want 0.64 (cached projected)", got.F)
	}
	if got.CILo != 0.60 || got.CIHi != 0.92 {
		t.Errorf("CI = [%v, %v], want [0.60, 0.92] (cached 80%% CI)", got.CILo, got.CIHi)
	}
	if got.ETA.Median != "2026-06-05T21:30:00Z" {
		t.Errorf("ETA.Median = %q, want cached %q", got.ETA.Median, "2026-06-05T21:30:00Z")
	}
}

// TestApplyCachedHeadlineNoMatchingETA leaves the sample's ETA untouched when
// the cached forecast has no entry for the sample's threshold.
func TestApplyCachedHeadlineNoMatchingETA(t *testing.T) {
	s := forecast.SamplePayload{
		ThresholdPct: forecast.UIThresholdPct,
		ETA:          forecast.ETAPayload{ThresholdPct: forecast.UIThresholdPct, Median: "keep-me"},
	}
	cached := forecast.Payload{
		ProjectedPct: 50.0, Lower80Pct: 45.0, Upper80Pct: 70.0,
		ETAs: []forecast.ETAPayload{{ThresholdPct: 80.0, Median: "other-threshold"}},
	}

	got := applyCachedHeadline(s, cached)

	if got.CILo != 0.45 || got.CIHi != 0.70 {
		t.Errorf("CI = [%v, %v], want [0.45, 0.70]", got.CILo, got.CIHi)
	}
	if got.ETA.Median != "keep-me" {
		t.Errorf("ETA.Median = %q, want it left untouched (no matching threshold)", got.ETA.Median)
	}
}
