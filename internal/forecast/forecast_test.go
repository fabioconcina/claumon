package forecast

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// approxEq compares floats with a relative tolerance, falling back to an
// absolute tolerance for values near zero.
func approxEq(t *testing.T, name string, got, want, rtol, atol float64) {
	t.Helper()
	diff := math.Abs(got - want)
	if diff <= atol {
		return
	}
	if math.Abs(want) > 0 && diff/math.Abs(want) <= rtol {
		return
	}
	t.Errorf("%s: got %.6g, want %.6g (diff %.3g, rtol %.1g, atol %.1g)", name, got, want, diff, rtol, atol)
}

// workedExampleSnapshots reproduces the four snapshots from §9 (relative to
// 12:30, ending at 13:00).
func workedExampleSnapshots(base time.Time) []Snapshot {
	return []Snapshot{
		{Time: base, U: 0.270},
		{Time: base.Add(10 * time.Minute), U: 0.280},
		{Time: base.Add(20 * time.Minute), U: 0.295},
		{Time: base.Add(30 * time.Minute), U: 0.300},
	}
}

func TestOLSWorkedExample(t *testing.T) {
	base := time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC)
	snaps := workedExampleSnapshots(base)

	rOLS, seSq, ok := olsFit(snaps)
	if !ok {
		t.Fatal("olsFit failed")
	}
	approxEq(t, "rOLS", rOLS, 0.0630, 0.01, 0)
	approxEq(t, "SEolsSq", seSq, 6.30e-5, 0.05, 0)
}

func TestPosteriorWorkedExample(t *testing.T) {
	base := time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC)
	snaps := workedExampleSnapshots(base)

	prior := Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20}
	post := EstimatePosterior(snaps, prior)

	if !post.UsedOLS {
		t.Fatal("expected OLS to be used")
	}
	approxEq(t, "tauPostSq", post.TauPostSq, 6.19e-5, 0.02, 0)
	approxEq(t, "rHat", post.RHat, 0.0633, 0.01, 0)
}

func TestForecastWorkedExample(t *testing.T) {
	fc := ProjectForecast(0.30, 0.0633, 6.19e-5, 2.5e-3, 3.0)
	approxEq(t, "F", fc.F, 0.490, 0.005, 0)
	approxEq(t, "sigmaF", fc.SigmaF, 0.090, 0.02, 0.002)
	approxEq(t, "lower CI", fc.Lower, 0.375, 0.02, 0.005)
	approxEq(t, "upper CI", fc.Upper, 0.605, 0.02, 0.005)
}

func TestForecastVariancePieces(t *testing.T) {
	// Pure rate uncertainty: path noise = 0, variance is quadratic in horizon.
	fc := ProjectForecast(0, 0.1, 1e-3, 0, 2)
	approxEq(t, "rate-only sigmaF^2", fc.SigmaF*fc.SigmaF, 4*1e-3, 1e-9, 1e-9)

	// Pure path noise: rate variance = 0, variance is linear in horizon.
	fc = ProjectForecast(0, 0.1, 0, 1e-3, 4)
	approxEq(t, "path-only sigmaF^2", fc.SigmaF*fc.SigmaF, 4*1e-3, 1e-9, 1e-9)
}

func TestForecastCIClipping(t *testing.T) {
	fc := ProjectForecast(0.95, 0.5, 0, 1e-2, 1.0)
	if fc.Upper != 1.0 {
		t.Errorf("upper bound should clip to 1, got %v", fc.Upper)
	}
	if fc.F <= 1.0 {
		t.Errorf("unclipped F should exceed 1, got %v", fc.F)
	}
}

func TestForecastLowerBoundFlooredAtUNow(t *testing.T) {
	// Small rate, wide variance: the raw Gaussian lower tail dips below uNow,
	// which is unphysical (utilization within a window only grows). Lower must
	// be floored at uNow, not at 0.
	fc := ProjectForecast(0.06, 0.0, 1e-4, 5e-3, 126.0)
	if fc.Lower < 0.06-1e-9 {
		t.Errorf("lower bound should be floored at uNow=0.06, got %v", fc.Lower)
	}
	if fc.Upper <= 0.06 {
		t.Errorf("upper bound should exceed uNow given positive sigma, got %v", fc.Upper)
	}
}

func TestEstimatePosteriorFallsBackToPrior(t *testing.T) {
	prior := Prior{Mu0: 0.05, Tau0Sq: 1e-3, NSessions: 5}
	post := EstimatePosterior(nil, prior)
	if post.UsedOLS {
		t.Error("expected OLS-not-used with no snapshots")
	}
	if post.RHat != prior.Mu0 || post.TauPostSq != prior.Tau0Sq {
		t.Errorf("expected prior passthrough, got rHat=%v tauPostSq=%v", post.RHat, post.TauPostSq)
	}
}

func TestFilterRecentResetDetection(t *testing.T) {
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	snaps := []Snapshot{
		{Time: now.Add(-20 * time.Minute), U: 0.80},
		{Time: now.Add(-15 * time.Minute), U: 0.85},
		{Time: now.Add(-10 * time.Minute), U: 0.05}, // reset
		{Time: now.Add(-5 * time.Minute), U: 0.10},
		{Time: now, U: 0.12},
	}
	out := filterRecent(snaps, now, 30*time.Minute)
	if len(out) != 3 {
		t.Fatalf("expected 3 post-reset snapshots, got %d", len(out))
	}
	if out[0].U != 0.05 {
		t.Errorf("expected first kept snapshot to be the reset point, got %v", out[0].U)
	}
}

func TestRunWorkedExample(t *testing.T) {
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 5, 24, 16, 0, 0, 0, time.UTC)
	base := now.Add(-30 * time.Minute)

	res, ok := Run(Input{
		Now:         now,
		Reset:       reset,
		UNow:        0.30,
		Snapshots:   workedExampleSnapshots(base),
		Prior:       Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20},
		Calibration: Calibration{SigmaSessionSq: 2.5e-3},
		Thresholds:  []float64{1.0},
	}, DefaultConfig())

	if !ok {
		t.Fatal("Run returned !ok")
	}
	approxEq(t, "F", res.Forecast.F, 0.490, 0.01, 0.005)
	approxEq(t, "rHat", res.Posterior.RHat, 0.0633, 0.01, 0)

	eta := res.ETAs[1.0]
	if eta == nil {
		t.Fatal("expected ETA struct (even if median nil)")
	}
	if eta.Median != nil {
		t.Errorf("expected median ETA == nil (threshold unreachable), got %v", *eta.Median)
	}
	if eta.PInf < 0.5 {
		t.Errorf("expected p_inf >= 0.5, got %v", eta.PInf)
	}
}

func TestETAAlreadyCrossed(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	post := Posterior{RHat: 0.1, TauPostSq: 1e-4, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 1e-3}

	eta := EstimateETA(now, reset, 0.9, post, cal, 0.8, DefaultConfig())
	if eta == nil || eta.Median == nil {
		t.Fatal("expected median == now for already-crossed threshold")
	}
	if !eta.Median.Equal(now) {
		t.Errorf("expected median == now, got %v", *eta.Median)
	}
}

func TestETAReproducible(t *testing.T) {
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	post := Posterior{RHat: 0.25, TauPostSq: 4e-4, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 2.5e-3}

	a := EstimateETA(now, reset, 0.30, post, cal, 1.0, DefaultConfig())
	b := EstimateETA(now, reset, 0.30, post, cal, 1.0, DefaultConfig())
	if (a.Median == nil) != (b.Median == nil) {
		t.Fatal("non-reproducible median nil-ness")
	}
	if a.Median != nil && !a.Median.Equal(*b.Median) {
		t.Errorf("non-reproducible median: %v vs %v", *a.Median, *b.Median)
	}
	if a.PInf != b.PInf {
		t.Errorf("non-reproducible p_inf: %v vs %v", a.PInf, b.PInf)
	}
}

func TestETAReachableThresholdHasFiniteMedian(t *testing.T) {
	// Strong positive drift, low path noise, threshold close to u_now.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	post := Posterior{RHat: 0.5, TauPostSq: 1e-5, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 1e-4}

	eta := EstimateETA(now, reset, 0.30, post, cal, 0.60, DefaultConfig())
	if eta == nil || eta.Median == nil {
		t.Fatal("expected finite median ETA")
	}
	// Deterministic crossing: (0.6 - 0.3)/0.5 = 0.6 h = 36 min after now.
	want := now.Add(36 * time.Minute)
	delta := eta.Median.Sub(want)
	if delta < -10*time.Minute || delta > 10*time.Minute {
		t.Errorf("median ETA too far from deterministic crossing: got %v, want ~%v", *eta.Median, want)
	}
}

func TestFitPriorRecoversMean(t *testing.T) {
	// Sessions where r_s is exactly known and W_s == 0; mu0 should equal the
	// mean of u*/D.
	sessions := []Session{
		{DurationHours: 5, UFinal: 0.5},  // rho = 0.10
		{DurationHours: 5, UFinal: 0.4},  // rho = 0.08
		{DurationHours: 5, UFinal: 0.3},  // rho = 0.06
		{DurationHours: 5, UFinal: 0.45}, // rho = 0.09
	}
	p, ok := FitPrior(sessions, 0, 1e-6)
	if !ok {
		t.Fatal("FitPrior failed")
	}
	approxEq(t, "mu0", p.Mu0, 0.0825, 1e-6, 1e-9)
	if p.Tau0Sq <= 0 {
		t.Errorf("expected positive tau0Sq, got %v", p.Tau0Sq)
	}
}

func TestFitPriorNoiseCorrectionFloors(t *testing.T) {
	// Identical rho across sessions -> sample variance == 0. The correction is
	// non-negative, so subtracting any positive sigma^2 contribution must
	// floor at varianceEps, never go negative.
	sessions := []Session{
		{DurationHours: 4, UFinal: 0.40},
		{DurationHours: 4, UFinal: 0.40},
		{DurationHours: 4, UFinal: 0.40},
	}
	p, ok := FitPrior(sessions, 5e-3, 1e-6)
	if !ok {
		t.Fatal("FitPrior failed")
	}
	if p.Tau0Sq != 1e-6 {
		t.Errorf("expected tau0Sq to floor at 1e-6, got %v", p.Tau0Sq)
	}
}

func TestRunSuppressedWhenPriorEmpty(t *testing.T) {
	now := time.Now()
	_, ok := Run(Input{
		Now:   now,
		Reset: now.Add(time.Hour),
		UNow:  0.3,
		Prior: Prior{Mu0: 0.1, Tau0Sq: 1e-3, NSessions: 1},
	}, DefaultConfig())
	if ok {
		t.Error("expected Run to suppress forecast with NSessions < 2")
	}
}

func TestFitNoiseRegressionRecoversBothCoefficients(t *testing.T) {
	// e^2 = a * delta + b * delta^2 with known a, b; add small noise and
	// verify both come back close.
	const aTrue = 4e-3
	const bTrue = 1e-4
	rng := newDetRNG(7)

	deltas := make([]float64, 0, 400)
	eSqs := make([]float64, 0, 400)
	for d := 0.25; d <= 4.0; d += 0.1 {
		for k := 0; k < 20; k++ {
			noise := 1e-5 * rng.norm()
			deltas = append(deltas, d)
			eSqs = append(eSqs, aTrue*d+bTrue*d*d+noise)
		}
	}
	aHat, bHat := fitNoiseRegression(deltas, eSqs)
	approxEq(t, "aHat", aHat, aTrue, 0.05, 1e-5)
	approxEq(t, "bHat", bHat, bTrue, 0.10, 1e-5)
}

func TestFitNoiseRegressionSingular(t *testing.T) {
	// Single delta -> x and x^2 are perfectly collinear, regression is
	// singular.
	deltas := []float64{1.0, 1.0, 1.0}
	eSqs := []float64{0.1, 0.2, 0.3}
	a, b := fitNoiseRegression(deltas, eSqs)
	if !math.IsNaN(a) || !math.IsNaN(b) {
		t.Errorf("expected NaN coefficients on singular design, got (%v, %v)", a, b)
	}
}

func TestCalibrationFloorsOnTooFewSamples(t *testing.T) {
	cal := CalibrateSigmaSession(nil, Prior{Mu0: 0.1, Tau0Sq: 1e-3, NSessions: 5},
		DefaultConfig(), 6, 30*time.Minute)
	if cal.SigmaSessionSq != DefaultConfig().VarianceEps {
		t.Errorf("expected variance floor with no sessions, got %v", cal.SigmaSessionSq)
	}
}

func TestCalibrationEndToEndMonotone(t *testing.T) {
	// Real utilization is monotone within a window (it's cumulative usage),
	// so the reset-detection rule in filterRecent doesn't misfire. Generate
	// monotone sessions whose increments have variance proportional to dt
	// (the path-noise structure §5 calibrates), and check we recover
	// sigma^2 within a factor of three. The spec self-flags this estimator
	// as having residual bias from the tau_post^2 / delta_f correlation, so
	// we keep the tolerance loose.
	const trueSigmaSq = 5e-4
	const rate = 0.10
	prior := Prior{Mu0: rate, Tau0Sq: 1e-3, NSessions: 40}
	rng := newDetRNG(1234)

	var sessions []Session
	for s := 0; s < 40; s++ {
		start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(s) * 6 * time.Hour)
		reset := start.Add(5 * time.Hour)
		snaps := []Snapshot{{Time: start, U: 0}}
		for tt := start.Add(5 * time.Minute); !tt.After(reset); tt = tt.Add(5 * time.Minute) {
			dt := tt.Sub(snaps[len(snaps)-1].Time).Hours()
			inc := rate*dt + math.Sqrt(trueSigmaSq*dt)*rng.norm()
			if inc < 0 {
				inc = 0 // enforce monotonicity, matching real utilization
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

	cal := CalibrateSigmaSession(sessions, prior, DefaultConfig(), 6, 30*time.Minute)
	if cal.SigmaSessionSq < trueSigmaSq/3 || cal.SigmaSessionSq > 3*trueSigmaSq {
		t.Errorf("sigma^2 recovery out of band: got %v, want within [%v, %v]",
			cal.SigmaSessionSq, trueSigmaSq/3, 3*trueSigmaSq)
	}
}

func TestEffectiveRateVarTakesMax(t *testing.T) {
	if got := EffectiveRateVar(1e-4, 5e-3); got != 5e-3 {
		t.Errorf("expected floor to win, got %v", got)
	}
	if got := EffectiveRateVar(5e-3, 1e-4); got != 5e-3 {
		t.Errorf("expected conjugate to win, got %v", got)
	}
	if got := EffectiveRateVar(1e-4, 0); got != 1e-4 {
		t.Errorf("zero floor should be a no-op, got %v", got)
	}
}

func TestRunAppliesBarTauSqFloor(t *testing.T) {
	// Same scenario, same OLS posterior; the only difference is whether
	// Calibration carries a BarTauSq. With the floor active, the CI must be
	// wider.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	base := now.Add(-30 * time.Minute)
	in := Input{
		Now:        now,
		Reset:      reset,
		UNow:       0.30,
		Snapshots:  workedExampleSnapshots(base),
		Prior:      Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20},
		Thresholds: []float64{1.0},
	}

	in.Calibration = Calibration{SigmaSessionSq: 2.5e-3}
	resNarrow, ok := Run(in, DefaultConfig())
	if !ok {
		t.Fatal("Run without floor failed")
	}

	in.Calibration = Calibration{SigmaSessionSq: 2.5e-3, BarTauSq: 3.6e-3}
	resWide, ok := Run(in, DefaultConfig())
	if !ok {
		t.Fatal("Run with floor failed")
	}

	if resWide.Forecast.SigmaF <= resNarrow.Forecast.SigmaF {
		t.Errorf("expected sigmaF to widen under floor: narrow=%v wide=%v",
			resNarrow.Forecast.SigmaF, resWide.Forecast.SigmaF)
	}
	// Point forecast must be unchanged (floor affects spread only).
	approxEq(t, "F unchanged", resWide.Forecast.F, resNarrow.Forecast.F, 1e-12, 1e-12)
	// Worked example with bar_tau^2=3.6e-3 gives sigmaF ~ 0.20 (vs ~0.09 raw).
	approxEq(t, "wide sigmaF", resWide.Forecast.SigmaF, 0.200, 0.02, 0.005)
}

func TestCalibrationStoresBHat(t *testing.T) {
	// Generate synthetic sessions where the end-of-session error variance
	// scales like b*delta^2 + a*delta with known a, b. The end-to-end
	// CalibrateSigmaSession should recover bHat into BarTauSq.
	const trueSigmaSq = 5e-4
	const trueRateVar = 2e-3
	prior := Prior{Mu0: 0.10, Tau0Sq: trueRateVar, NSessions: 40}
	rng := newDetRNG(99)

	var sessions []Session
	for s := 0; s < 60; s++ {
		start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(s) * 6 * time.Hour)
		reset := start.Add(5 * time.Hour)
		// Each session has its own r drawn from N(mu0, tau0Sq).
		r := 0.10 + math.Sqrt(trueRateVar)*rng.norm()
		snaps := []Snapshot{{Time: start, U: 0}}
		for tt := start.Add(5 * time.Minute); !tt.After(reset); tt = tt.Add(5 * time.Minute) {
			dt := tt.Sub(snaps[len(snaps)-1].Time).Hours()
			inc := r*dt + math.Sqrt(trueSigmaSq*dt)*rng.norm()
			if inc < 0 {
				inc = 0
			}
			snaps = append(snaps, Snapshot{Time: tt, U: snaps[len(snaps)-1].U + inc})
		}
		sessions = append(sessions, Session{
			Reset: reset, DurationHours: 5, UFinal: snaps[len(snaps)-1].U, Snapshots: snaps,
		})
	}

	cal := CalibrateSigmaSession(sessions, prior, DefaultConfig(), 6, 30*time.Minute)
	if cal.BarTauSq <= 0 {
		t.Errorf("expected positive BarTauSq, got %v", cal.BarTauSq)
	}
	// Loose tolerance: regression has known finite-sample bias and the
	// tau_post^2/delta covariance contamination the spec calls out.
	if cal.BarTauSq < trueRateVar/4 || cal.BarTauSq > 4*trueRateVar {
		t.Errorf("BarTauSq out of band: got %v, want within [%v, %v]",
			cal.BarTauSq, trueRateVar/4, 4*trueRateVar)
	}
}

func TestSampleGammaMeanVar(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const mean, variance = 0.5, 0.05
	const n = 200000
	var sum, sumSq float64
	for i := 0; i < n; i++ {
		x := sampleGammaMeanVar(rng, mean, variance)
		if x < 0 {
			t.Fatalf("negative gamma draw: %v", x)
		}
		sum += x
		sumSq += x * x
	}
	gotMean := sum / n
	gotVar := sumSq/n - gotMean*gotMean
	approxEq(t, "gamma mean", gotMean, mean, 0.02, 0.005)
	approxEq(t, "gamma var", gotVar, variance, 0.05, 0.005)

	// Also exercise the shape<1 branch (mean^2/var < 1), which is the common
	// per-step regime (many near-zero increments, occasional jumps).
	rng = rand.New(rand.NewSource(7))
	var s2 float64
	for i := 0; i < n; i++ {
		s2 += sampleGammaMeanVar(rng, 0.01, 0.05) // shape = 0.002
	}
	approxEq(t, "small-shape gamma mean", s2/n, 0.01, 0.1, 0.002)

	// Degenerate cases collapse cleanly.
	if got := sampleGammaMeanVar(rng, 0, 0.1); got != 0 {
		t.Errorf("non-positive mean should give 0, got %v", got)
	}
	if got := sampleGammaMeanVar(rng, 0.3, 0); got != 0.3 {
		t.Errorf("non-positive variance should give the mean, got %v", got)
	}
}

func TestSampleMCPathsAreMonotone(t *testing.T) {
	// Core realism property of model v2.0: with positive-only increments the MC
	// trajectories never decrease (so the fan-chart no longer dips).
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(4 * time.Hour)
	post := Posterior{RHat: 0.12, TauPostSq: 3e-3, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 5e-3, BarTauSq: 3e-3}
	const uNow = 0.20

	s, ok := SampleMC(now, reset, uNow, post, cal, 1.0, DefaultConfig())
	if !ok {
		t.Fatal("SampleMC returned !ok")
	}
	if len(s.Trajectories) == 0 {
		t.Fatal("expected trajectories")
	}
	for k, path := range s.Trajectories {
		if path[0] != uNow {
			t.Fatalf("path %d does not start at uNow: %v", k, path[0])
		}
		for j := 1; j < len(path); j++ {
			if path[j] < path[j-1]-1e-12 {
				t.Fatalf("path %d decreased at step %d: %v -> %v", k, j, path[j-1], path[j])
			}
		}
	}
}

func TestForecastCILowerNeverBelowUNow(t *testing.T) {
	// Near-zero drift, large noise, long horizon: the old Gaussian lower bound
	// dove far below uNow and had to be clipped there. The monotone process
	// cannot produce a terminal value below uNow, so the p10 lower bound is at
	// or above uNow by construction.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := now.Add(100 * time.Hour)
	post := Posterior{RHat: 0.0, TauPostSq: 1e-2, UsedOLS: true}
	cal := Calibration{SigmaSessionSq: 5e-3, BarTauSq: 1e-2}
	const uNow = 0.06

	s, ok := SampleMC(now, reset, uNow, post, cal, 1.0, DefaultConfig())
	if !ok {
		t.Fatal("SampleMC returned !ok")
	}
	for k, v := range s.Terminal {
		if v < uNow-1e-12 {
			t.Fatalf("terminal %d below uNow: %v < %v", k, v, uNow)
		}
	}
	lo, hi := terminalCI(s.Terminal)
	if lo < uNow-1e-12 {
		t.Errorf("CI lower below uNow: %v < %v", lo, uNow)
	}
	if hi < lo {
		t.Errorf("CI upper below lower: hi=%v lo=%v", hi, lo)
	}
}

func TestRunForecastCIWellFormed(t *testing.T) {
	// Through the full Run path: the CI now comes from the MC terminal
	// distribution, so uNow <= Lower <= F <= Upper.
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 5, 24, 16, 0, 0, 0, time.UTC)
	base := now.Add(-30 * time.Minute)
	res, ok := Run(Input{
		Now:         now,
		Reset:       reset,
		UNow:        0.30,
		Snapshots:   workedExampleSnapshots(base),
		Prior:       Prior{Mu0: 0.080, Tau0Sq: 3.6e-3, NSessions: 20},
		Calibration: Calibration{SigmaSessionSq: 2.5e-3, BarTauSq: 3.6e-3},
		Thresholds:  []float64{1.0},
	}, DefaultConfig())
	if !ok {
		t.Fatal("Run returned !ok")
	}
	fc := res.Forecast
	if fc.Lower < 0.30-1e-9 {
		t.Errorf("Lower below uNow: %v", fc.Lower)
	}
	if fc.Lower > fc.F+1e-9 {
		t.Errorf("Lower should be <= F: lower=%v F=%v", fc.Lower, fc.F)
	}
	if fc.Upper < fc.F-1e-9 {
		t.Errorf("Upper should be >= F: upper=%v F=%v", fc.Upper, fc.F)
	}
}

func TestRunForecastCIOvershootNotInverted(t *testing.T) {
	// Regression: high utilization plus a steep rate projects demand well past
	// 100%. v2.0 capped only the upper edge at 1, so once p10 exceeded 1 the
	// reported interval inverted (e.g. "80% CI 134%-100%"). v2.1 reports both
	// edges uncapped: the interval must bracket F and stay ordered.
	now := time.Date(2026, 6, 10, 21, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	base := now.Add(-30 * time.Minute)
	res, ok := Run(Input{
		Now:   now,
		Reset: reset,
		UNow:  0.92,
		Snapshots: []Snapshot{
			{Time: base, U: 0.80},
			{Time: base.Add(10 * time.Minute), U: 0.84},
			{Time: base.Add(20 * time.Minute), U: 0.88},
			{Time: base.Add(30 * time.Minute), U: 0.92},
		},
		Prior:       Prior{Mu0: 0.20, Tau0Sq: 3.6e-3, NSessions: 20},
		Calibration: Calibration{SigmaSessionSq: 2.5e-3, BarTauSq: 3.6e-3},
		Thresholds:  []float64{1.0},
	}, DefaultConfig())
	if !ok {
		t.Fatal("Run returned !ok")
	}
	fc := res.Forecast
	if fc.F <= 1 {
		t.Fatalf("scenario should project past 100%%, got F=%v", fc.F)
	}
	if fc.Upper < fc.Lower {
		t.Errorf("inverted CI: upper=%v < lower=%v", fc.Upper, fc.Lower)
	}
	if fc.Lower > fc.F+1e-9 || fc.Upper < fc.F-1e-9 {
		t.Errorf("CI [%v, %v] does not bracket F=%v", fc.Lower, fc.Upper, fc.F)
	}
	if fc.Upper <= 1 {
		t.Errorf("upper edge should be uncapped and exceed 1, got %v", fc.Upper)
	}
}

// detRNG is a tiny xorshift64 generator with a Box-Muller wrapper, used only
// in tests to keep them reproducible without pulling math/rand into a global
// state.
type detRNG struct{ s uint64 }

func newDetRNG(seed uint64) *detRNG {
	if seed == 0 {
		seed = 1
	}
	return &detRNG{s: seed}
}

func (r *detRNG) u64() uint64 {
	x := r.s
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	r.s = x
	return x
}

func (r *detRNG) uniform() float64 {
	return float64(r.u64()>>11) / (1 << 53)
}

func (r *detRNG) norm() float64 {
	for {
		u := r.uniform()
		v := r.uniform()
		if u <= 0 {
			continue
		}
		return math.Sqrt(-2*math.Log(u)) * math.Cos(2*math.Pi*v)
	}
}
