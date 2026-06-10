// Package forecast projects open rate-limit windows to reset and estimates
// threshold ETAs. The math is specified in MODEL.pdf (LaTeX source in
// MODEL.tex); this file is the public entry point. See rate.go, eta.go,
// calibration.go for the pieces.
package forecast

import (
	"time"
)

// ModelVersion identifies the math being implemented. Bump it whenever
// MODEL.tex changes in a way that shifts forecast distributions, ETAs, or
// calibration semantics - bug fixes that match the spec don't count. The
// CHANGELOG in MODEL.tex tracks what each bump means, and retired specs
// live under internal/forecast/archive/<this-value>/.
const ModelVersion = "v2.1"

// UIThresholdPct is the crossing threshold (in percent) the dashboard forecasts
// against. The gauge poller and the trajectory popup both use it, so the two
// surfaces simulate the same regime and report the same "ETA to X%". The MC
// threshold is folded into the RNG seed, so using one value here keeps the
// gauge and popup from drawing different sample paths (and thus different 80%
// intervals) - the bug that came from the popup hardcoding a different value.
const UIThresholdPct = 100.0

// Snapshot is one observed utilization point.
type Snapshot struct {
	Time time.Time
	U    float64 // utilization in [0, 1]
}

// Prior carries the Gaussian prior on the rate r, plus the historical session
// count used to size the prior. Mu0 has units of utilization per hour.
type Prior struct {
	Mu0       float64
	Tau0Sq    float64
	NSessions int
}

// Calibration carries the two scalars learned from history.
// SigmaSessionSq is path-noise variance per hour (linear coefficient of the
// §5 regression). BarTauSq is the historical-average rate variance per hour^2
// (quadratic coefficient): used in §4 as a floor on the per-forecast
// tau_post^2 when the conjugate update underestimates rate uncertainty (which
// happens when the within-session rate isn't really constant, contradicting
// assumption A1). Floor is applied at use sites; zero BarTauSq disables the
// floor for backward-compat in tests that construct Calibration directly.
type Calibration struct {
	SigmaSessionSq float64 // (utilization)^2 per hour
	BarTauSq       float64 // (utilization per hour)^2 historical avg rate variance
}

// EffectiveRateVar returns the rate variance to use for the forecast spread,
// flooring the per-forecast tau_post^2 with the historical bar tau^2 from
// calibration. See MODEL §4: when the within-session rate is not truly
// constant the conjugate posterior systematically understates the rate
// uncertainty relevant to predicting the end-of-session utilization. The
// historical regression's b-hat is an unbiased estimator of that effective
// uncertainty under the same generative model.
func EffectiveRateVar(tauPostSq, barTauSq float64) float64 {
	if barTauSq > tauPostSq {
		return barTauSq
	}
	return tauPostSq
}

// Posterior is the result of combining OLS on the current window with the
// prior. UsedOLS is false when n < 3 and we fell back to the prior.
type Posterior struct {
	RHat      float64 // posterior mean rate (per hour)
	TauPostSq float64 // posterior variance
	N         int     // snapshots considered in OLS
	SEolsSq   float64 // OLS variance (0 if !UsedOLS)
	UsedOLS   bool
}

// Forecast is the projected utilization at reset with its 80% CI. F is the
// mean (u_now + r_hat*deltaT) and SigmaF is its analytic moment spread. Lower
// and Upper are the 10th/90th percentiles of the monotone Monte Carlo terminal
// distribution (see Run): because every increment is >= 0, Lower is naturally
// >= u_now and the interval is right-skewed, replacing the clipped symmetric
// z-interval of model v1.x. Both edges are reported uncapped (model v2.1):
// values above 1 measure projected demand beyond the window limit, and capping
// only the upper edge inverted the interval whenever p10 exceeded 1.
type Forecast struct {
	F      float64 // point forecast at reset (mean, unclipped)
	SigmaF float64 // sqrt(rate-variance term + path-noise term)
	Lower  float64 // 80% CI lower, from MC terminal p10 (>= u_now)
	Upper  float64 // 80% CI upper, from MC terminal p90 (uncapped)
	DeltaT float64 // remaining horizon in hours
}

// ETA summarises the threshold first-passage distribution. Median is nil when
// at least half of the Monte Carlo trajectories never crossed. Upper is nil
// when between 10% and 50% never crossed (open-ended upper bound).
type ETA struct {
	Median *time.Time
	Lower  *time.Time
	Upper  *time.Time
	PInf   float64 // fraction of MC trajectories that never crossed
}

// Result bundles the per-gauge output of one forecast call.
type Result struct {
	Forecast  Forecast
	Posterior Posterior
	ETAs      map[float64]*ETA // keyed by threshold (e.g. 1.0 for 100%)
}

// Config controls knobs the spec leaves as parameters. Use DefaultConfig and
// override fields as needed; zero-value fields fall back to defaults.
type Config struct {
	TauRecent   time.Duration // §3 recency window (default 30 min)
	MCTraj      int           // §8 trajectories (default 500)
	MCStep      time.Duration // §8 step size (default 5 min)
	VarianceEps float64       // floor for variance estimates (default 1e-6)
}

func DefaultConfig() Config {
	return Config{
		TauRecent:   30 * time.Minute,
		MCTraj:      500,
		MCStep:      5 * time.Minute,
		VarianceEps: 1e-6,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.TauRecent == 0 {
		c.TauRecent = d.TauRecent
	}
	if c.MCTraj == 0 {
		c.MCTraj = d.MCTraj
	}
	if c.MCStep == 0 {
		c.MCStep = d.MCStep
	}
	if c.VarianceEps == 0 {
		c.VarianceEps = d.VarianceEps
	}
	return c
}

// Input is everything one forecast call needs.
type Input struct {
	Now         time.Time
	Reset       time.Time
	UNow        float64
	Snapshots   []Snapshot // assumed sorted by Time, may include older points
	Prior       Prior
	Calibration Calibration
	Thresholds  []float64 // e.g. {1.0} for 100%
}

// Run produces a full forecast for one gauge. See MODEL.pdf.
func Run(in Input, cfg Config) (Result, bool) {
	cfg = cfg.withDefaults()

	if in.Prior.NSessions < 2 {
		// §8: prior empty -> suppress forecast.
		return Result{}, false
	}
	if !in.Reset.After(in.Now) {
		return Result{}, false
	}

	recent := filterRecent(in.Snapshots, in.Now, cfg.TauRecent)
	post := EstimatePosterior(recent, in.Prior)

	deltaT := in.Reset.Sub(in.Now).Hours()
	rateVar := EffectiveRateVar(post.TauPostSq, in.Calibration.BarTauSq)
	fc := ProjectForecast(in.UNow, post.RHat, rateVar, in.Calibration.SigmaSessionSq, deltaT)

	// Replace the symmetric z-quantile CI with the 80% interval of the monotone
	// MC terminal distribution: Lower is then naturally >= u_now (no clip) and
	// the band is right-skewed. F and SigmaF keep their analytic moment values.
	// This single MC run is shared with the matching threshold's ETA below
	// (same seed -> same crossings), so we never simulate it twice.
	ciThr := 1.0
	if len(in.Thresholds) > 0 {
		ciThr = in.Thresholds[0]
	}
	var ciSamples *Samples
	if s, ok := runMC(in.Now, in.Reset, in.UNow, post, in.Calibration, ciThr, cfg, false); ok && len(s.Terminal) > 0 {
		applyTerminalCI(&fc, s.Terminal)
		ciSamples = &s
	}

	etas := make(map[float64]*ETA, len(in.Thresholds))
	for _, thr := range in.Thresholds {
		// Reuse the CI run for its own threshold instead of repeating the
		// identical MC. EstimateETA still owns the threshold<=uNow short-circuit,
		// so we only short-cut when that branch wouldn't fire.
		if ciSamples != nil && thr == ciThr && thr > in.UNow {
			etas[thr] = summarizeETA(in.Now, *ciSamples)
			continue
		}
		etas[thr] = EstimateETA(in.Now, in.Reset, in.UNow, post, in.Calibration, thr, cfg)
	}

	return Result{
		Forecast:  fc,
		Posterior: post,
		ETAs:      etas,
	}, true
}

// filterRecent returns snapshots in (now-tau, now]. It also handles §8's
// mid-window reset detection: if a drop (u_{i+1} < u_i) appears, the slice is
// truncated to the post-drop tail.
func filterRecent(all []Snapshot, now time.Time, tau time.Duration) []Snapshot {
	cutoff := now.Add(-tau)
	start := 0
	for i, s := range all {
		if !s.Time.Before(cutoff) {
			start = i
			break
		}
		start = i + 1
	}
	window := all[start:]

	dropAt := -1
	for i := 1; i < len(window); i++ {
		if window[i].U < window[i-1].U {
			dropAt = i
		}
	}
	if dropAt >= 0 {
		window = window[dropAt:]
	}

	out := make([]Snapshot, 0, len(window))
	for _, s := range window {
		if !s.Time.After(now) {
			out = append(out, s)
		}
	}
	return out
}

// applyTerminalCI replaces the analytic symmetric z-interval that
// ProjectForecast leaves on fc.Lower/fc.Upper with the monotone MC terminal
// p10/p90 (the model v2.1 CI), both uncapped. Values above 1 measure projected
// demand beyond the window limit; capping only the upper bound (v2.0) inverted
// the interval when p10 exceeded 1. Both Run and SampleFor call this so the
// gauge line and the modal footer report the same 80% interval and can't
// drift apart. No-op when terminal is empty.
func applyTerminalCI(fc *Forecast, terminal []float64) {
	if len(terminal) == 0 {
		return
	}
	fc.Lower, fc.Upper = terminalCI(terminal)
}

func clip01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
