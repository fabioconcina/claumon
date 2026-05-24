package forecast

import (
	"testing"
	"time"
)

func benchInput() (Input, Config) {
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	base := now.Add(-30 * time.Minute)
	return Input{
		Now:         now,
		Reset:       reset,
		UNow:        0.30,
		Snapshots:   workedExampleSnapshots(base),
		Prior:       Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20},
		Calibration: Calibration{SigmaSessionSq: 2.5e-3},
		Thresholds:  []float64{1.0},
	}, DefaultConfig()
}

func BenchmarkRun(b *testing.B) {
	in, cfg := benchInput()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Run(in, cfg)
	}
}

func BenchmarkEstimatePosterior(b *testing.B) {
	in, _ := benchInput()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimatePosterior(in.Snapshots, in.Prior)
	}
}

func BenchmarkEstimateETA(b *testing.B) {
	in, cfg := benchInput()
	post := EstimatePosterior(in.Snapshots, in.Prior)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimateETA(in.Now, in.Reset, in.UNow, post, in.Calibration, 1.0, cfg)
	}
}

func BenchmarkEstimateETAWeekly(b *testing.B) {
	// Weekly horizon stresses the MC inner loop: nSteps grows linearly with
	// the horizon, so a 7-day window runs ~2000 steps × K trajectories. This
	// is the actual cost path for the weekly gauge each poll.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(7 * 24 * time.Hour)
	post := Posterior{RHat: 0.005, TauPostSq: 1e-5, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 1e-4}
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimateETA(now, reset, 0.30, post, cal, 1.0, cfg)
	}
}

func BenchmarkEstimateETAReachable(b *testing.B) {
	// ETA actually crosses (mostly finite trajectories) - exercises the
	// percentile + sort path more than the all-infinite case.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	post := Posterior{RHat: 0.5, TauPostSq: 1e-5, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 1e-4}
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimateETA(now, reset, 0.30, post, cal, 0.60, cfg)
	}
}

func BenchmarkCalibrateSigmaSession(b *testing.B) {
	rng := newDetRNG(1234)
	rate := 0.10
	var sessions []Session
	for s := 0; s < 40; s++ {
		start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(s) * 6 * time.Hour)
		reset := start.Add(5 * time.Hour)
		snaps := []Snapshot{{Time: start, U: 0}}
		for tt := start.Add(5 * time.Minute); !tt.After(reset); tt = tt.Add(5 * time.Minute) {
			dt := tt.Sub(snaps[len(snaps)-1].Time).Hours()
			inc := rate*dt + 0.005*rng.norm()
			if inc < 0 {
				inc = 0
			}
			snaps = append(snaps, Snapshot{Time: tt, U: snaps[len(snaps)-1].U + inc})
		}
		sessions = append(sessions, Session{
			Reset:         reset,
			DurationHours: 5,
			UFinal:        snaps[len(snaps)-1].U,
			Snapshots:     snaps,
		})
	}
	prior := Prior{Mu0: rate, Tau0Sq: 1e-3, NSessions: 40}
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalibrateSigmaSession(sessions, prior, cfg, 6, 30*time.Minute)
	}
}
