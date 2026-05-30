package forecast

import (
	"math"
	"time"
)

// CalibrateSigmaSession implements §5: replay the forecaster across past
// completed sessions, collect (delta, e^2) pairs, and recover sigma_session^2
// as the coefficient of delta in a joint no-intercept, horizon-weighted
// (w = 1/delta^2) least-squares fit of e^2 on [delta, delta^2]. See
// fitNoiseRegression for why the fit is weighted.
//
// forecastsPerSession (default 6) controls how many replay points are sampled
// per session, uniformly between the earliest feasible forecast time
// (t_start + tauRecent) and reset - minHorizon. Sessions with fewer than 3
// snapshots in the recency window at any replay point are skipped.
//
// prior is the rate prior used during replay (the same one a live forecast
// would have used). Pass the previously fit prior; bootstrapping from the
// first run is handled by the caller (sigma=0 -> no correction in FitPrior).
func CalibrateSigmaSession(sessions []Session, prior Prior, cfg Config, forecastsPerSession int, minHorizon time.Duration) Calibration {
	cfg = cfg.withDefaults()
	if forecastsPerSession <= 0 {
		forecastsPerSession = 6
	}
	if minHorizon <= 0 {
		minHorizon = 30 * time.Minute
	}

	type pair struct{ delta, eSq float64 }
	var samples []pair

	for _, s := range sessions {
		if len(s.Snapshots) < 3 || s.DurationHours <= 0 {
			continue
		}
		tStart := s.Snapshots[0].Time
		earliest := tStart.Add(cfg.TauRecent)
		latest := s.Reset.Add(-minHorizon)
		if !latest.After(earliest) {
			continue
		}

		for k := 0; k < forecastsPerSession; k++ {
			frac := (float64(k) + 0.5) / float64(forecastsPerSession)
			tf := earliest.Add(time.Duration(frac * float64(latest.Sub(earliest))))

			recent := filterRecent(s.Snapshots, tf, cfg.TauRecent)
			if len(recent) < 3 {
				continue
			}
			uAtTf, ok := interpAt(s.Snapshots, tf)
			if !ok {
				continue
			}
			post := EstimatePosterior(recent, prior)
			delta := s.Reset.Sub(tf).Hours()
			if delta <= 0 {
				continue
			}
			fHat := uAtTf + post.RHat*delta
			e := s.UFinal - fHat
			samples = append(samples, pair{delta: delta, eSq: e * e})
		}
	}

	if len(samples) < 2 {
		return Calibration{SigmaSessionSq: cfg.VarianceEps}
	}
	deltas := make([]float64, len(samples))
	eSqs := make([]float64, len(samples))
	for i, p := range samples {
		deltas[i] = p.delta
		eSqs[i] = p.eSq
	}
	// aHat is the linear coefficient (path noise per hour). bHat is the
	// quadratic coefficient: the historical-average rate variance per hour^2.
	// Originally bHat was discarded (the per-forecast tau_post^2 was treated
	// as strictly more informative), but in practice the conjugate update
	// shrinks tau_post^2 well below bHat whenever the within-session rate is
	// not truly constant, leading to severely under-spread CIs. We now keep
	// bHat as a floor; see EffectiveRateVar.
	aHat, bHat := fitNoiseRegression(deltas, eSqs)
	if math.IsNaN(aHat) || math.IsInf(aHat, 0) || aHat < cfg.VarianceEps {
		aHat = cfg.VarianceEps
	}
	if math.IsNaN(bHat) || math.IsInf(bHat, 0) || bHat < 0 {
		bHat = 0
	}
	return Calibration{SigmaSessionSq: aHat, BarTauSq: bHat}
}

// fitNoiseRegression fits z = a*x + b*x^2 with no intercept by weighted least
// squares (weight w = 1/x^2) and returns (a, b). Used by §5 to separate the
// linear path-noise term from the quadratic rate-uncertainty term. NaN is
// returned for both coefficients when the design matrix is singular.
//
// The weighting corrects heteroskedasticity: here z is a squared forecast
// error e^2, whose own variance scales as (E[e^2])^2 and so grows steeply with
// the horizon x. Unweighted OLS is then dominated by the few large-x points
// and inflates the linear coefficient a, over-predicting variance at short
// horizons by an order of magnitude (and, downstream, over-subtracting in the
// FitPrior noise correction until tau_0^2 floors out). Weighting by 1/x^2 is a
// fixed proxy for 1/Var[e^2] that puts the short- and long-horizon points on a
// comparable scale; the quadratic term b stays identified by the long-horizon
// points (only they carry information about x^2). See MODEL §5.
//
// With w = 1/x^2 the normal equations for minimizing sum w*(z - a*x - b*x^2)^2
// collapse to: a*n + b*Sx = S(z/x); a*Sx + b*Sxx = Sz.
func fitNoiseRegression(xs, zs []float64) (float64, float64) {
	if len(xs) != len(zs) || len(xs) < 2 {
		return math.NaN(), math.NaN()
	}
	var n, sx, sxx, szOverX, sz float64
	for i := range xs {
		x := xs[i]
		if x <= 0 {
			continue // x is a remaining horizon in hours; always > 0 in replay
		}
		z := zs[i]
		n++
		sx += x
		sxx += x * x
		szOverX += z / x
		sz += z
	}
	det := n*sxx - sx*sx
	if det == 0 {
		return math.NaN(), math.NaN()
	}
	aHat := (szOverX*sxx - sz*sx) / det
	bHat := (n*sz - sx*szOverX) / det
	return aHat, bHat
}

// interpAt returns u at time tf by linear interpolation between adjacent
// snapshots, or by clipping to the nearest endpoint when tf is outside the
// range. ok=false if there are no snapshots.
func interpAt(snaps []Snapshot, tf time.Time) (float64, bool) {
	if len(snaps) == 0 {
		return 0, false
	}
	if !tf.After(snaps[0].Time) {
		return snaps[0].U, true
	}
	last := snaps[len(snaps)-1]
	if !tf.Before(last.Time) {
		return last.U, true
	}
	for i := 1; i < len(snaps); i++ {
		if !snaps[i].Time.Before(tf) {
			a, b := snaps[i-1], snaps[i]
			total := b.Time.Sub(a.Time).Seconds()
			if total == 0 {
				return a.U, true
			}
			frac := tf.Sub(a.Time).Seconds() / total
			return a.U + frac*(b.U-a.U), true
		}
	}
	return last.U, true
}
