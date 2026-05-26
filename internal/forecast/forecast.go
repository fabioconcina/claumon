// Package forecast projects open rate-limit windows to reset and estimates
// threshold ETAs. The math is specified in MODEL.pdf (LaTeX source in
// MODEL.tex); this file is the public entry point. See rate.go, eta.go,
// calibration.go for the pieces.
package forecast

import "time"

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

// Calibration is the single scalar learned from history that controls path
// noise: variance accumulated per hour of waiting.
type Calibration struct {
	SigmaSessionSq float64 // (utilization)^2 per hour
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

// Forecast is the projected utilization at reset with its 80% CI. Lower and
// Upper are clipped to [uNow, 1] for display (utilization only grows within a
// window); F is unclipped so ETA logic can reason about it.
type Forecast struct {
	F      float64 // point forecast at reset (unclipped)
	SigmaF float64 // sqrt(rate-variance term + path-noise term)
	Lower  float64 // 80% CI lower, clipped
	Upper  float64 // 80% CI upper, clipped
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

// Confidence is a coarse tag derived from effective sample size.
type Confidence int

const (
	ConfLow Confidence = iota
	ConfMedium
	ConfHigh
)

func (c Confidence) String() string {
	switch c {
	case ConfHigh:
		return "High"
	case ConfMedium:
		return "Medium"
	default:
		return "Low"
	}
}

// Result bundles the per-gauge output of one forecast call.
type Result struct {
	Forecast   Forecast
	Posterior  Posterior
	ETAs       map[float64]*ETA // keyed by threshold (e.g. 1.0 for 100%)
	Confidence Confidence
}

// Config controls knobs the spec leaves as parameters. Use DefaultConfig and
// override fields as needed; zero-value fields fall back to defaults.
type Config struct {
	TauRecent   time.Duration // §3 recency window (default 30 min)
	MCTraj      int           // §6 trajectories (default 500)
	MCStep      time.Duration // §6 step size (default 5 min)
	HighNEff    float64       // §7 (default 50)
	MediumNEff  float64       // §7 (default 15)
	VarianceEps float64       // floor for variance estimates (default 1e-6)
}

func DefaultConfig() Config {
	return Config{
		TauRecent:   30 * time.Minute,
		MCTraj:      500,
		MCStep:      5 * time.Minute,
		HighNEff:    50,
		MediumNEff:  15,
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
	if c.HighNEff == 0 {
		c.HighNEff = d.HighNEff
	}
	if c.MediumNEff == 0 {
		c.MediumNEff = d.MediumNEff
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
	fc := ProjectForecast(in.UNow, post.RHat, post.TauPostSq, in.Calibration.SigmaSessionSq, deltaT)

	etas := make(map[float64]*ETA, len(in.Thresholds))
	for _, thr := range in.Thresholds {
		etas[thr] = EstimateETA(in.Now, in.Reset, in.UNow, post, in.Calibration, thr, cfg)
	}

	tag := confidenceTag(post, in.Prior, cfg)

	return Result{
		Forecast:   fc,
		Posterior:  post,
		ETAs:       etas,
		Confidence: tag,
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

func confidenceTag(post Posterior, prior Prior, cfg Config) Confidence {
	var nEff float64
	if post.UsedOLS && post.SEolsSq > 0 {
		nEff = prior.Tau0Sq/post.SEolsSq + float64(prior.NSessions)
	} else {
		nEff = float64(prior.NSessions)
	}
	if post.N > 0 && float64(post.N) < nEff {
		nEff = float64(post.N)
	}
	switch {
	case nEff >= cfg.HighNEff:
		return ConfHigh
	case nEff >= cfg.MediumNEff:
		return ConfMedium
	default:
		return ConfLow
	}
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
