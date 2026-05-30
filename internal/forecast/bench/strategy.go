package bench

import (
	"math"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
)

// Predictive is a Gaussian predictive distribution for utilization at reset.
// Strategies return a full distribution (not just a point) so proper scoring
// rules can reward calibration and sharpness together.
type Predictive struct {
	Mu    float64
	Sigma float64
}

// Strategy is a forecast method that can be trained on a set of sessions and
// then produce a predictive for a held-out session at a given forecast time.
// State is opaque and round-tripped back into Predict, so each fold's fit is
// isolated (no leakage across LOSO folds).
type Strategy interface {
	Name() string
	Train(train []forecast.Session, cfg forecast.Config) any
	Predict(state any, history []forecast.Snapshot, uNow float64, now, reset time.Time, cfg forecast.Config) (Predictive, bool)
}

// fitState carries the standard per-device fit (prior + calibration) used by
// the model strategies, plus the climatology moments for baselines.
type fitState struct {
	prior    forecast.Prior
	cal      forecast.Calibration
	meanU    float64 // mean final utilization (climatology center)
	sdU      float64 // sd of final utilization (climatology spread)
}

// fitStandard runs the live two-pass fit: prior (sigma=0), calibrate, refit
// prior with the sigma correction - exactly what Service.Refit does.
func fitStandard(train []forecast.Session, cfg forecast.Config) fitState {
	var st fitState
	prior, ok := forecast.FitPrior(train, 0, cfg.VarianceEps)
	if ok {
		cal := forecast.CalibrateSigmaSession(train, prior, cfg, 6, 30*time.Minute)
		if p2, ok2 := forecast.FitPrior(train, cal.SigmaSessionSq, cfg.VarianceEps); ok2 {
			prior = p2
		}
		st.prior = prior
		st.cal = cal
	}
	// climatology moments
	var sum, sumSq float64
	var n float64
	for _, s := range train {
		sum += s.UFinal
		sumSq += s.UFinal * s.UFinal
		n++
	}
	if n > 0 {
		st.meanU = sum / n
		if n > 1 {
			v := (sumSq - sum*sum/n) / (n - 1)
			if v < 0 {
				v = 0
			}
			st.sdU = math.Sqrt(v)
		}
	}
	return st
}

// --- Current: the deployed pipeline (conjugate + weighted-sigma calibration) -

type Current struct{}

func (Current) Name() string { return "current" }
func (Current) Train(train []forecast.Session, cfg forecast.Config) any {
	return fitStandard(train, cfg)
}
func (Current) Predict(state any, history []forecast.Snapshot, uNow float64, now, reset time.Time, cfg forecast.Config) (Predictive, bool) {
	st := state.(fitState)
	in := forecast.Input{
		Now: now, Reset: reset, UNow: uNow,
		Snapshots: history, Prior: st.prior, Calibration: st.cal,
	}
	res, ok := forecast.Run(in, cfg)
	if !ok {
		return Predictive{}, false
	}
	return Predictive{Mu: res.Forecast.F, Sigma: res.Forecast.SigmaF}, true
}

// --- Mu0: discard recency, forecast at the historical average rate ----------
// Same calibrated spread as Current (rate variance floored at bar_tau^2), only
// the center differs, so the comparison isolates the point-estimate choice.

type Mu0 struct{}

func (Mu0) Name() string { return "mu0" }
func (Mu0) Train(train []forecast.Session, cfg forecast.Config) any {
	return fitStandard(train, cfg)
}
func (Mu0) Predict(state any, history []forecast.Snapshot, uNow float64, now, reset time.Time, cfg forecast.Config) (Predictive, bool) {
	st := state.(fitState)
	if st.prior.NSessions < 2 {
		return Predictive{}, false
	}
	dt := reset.Sub(now).Hours()
	if dt <= 0 {
		return Predictive{}, false
	}
	fc := forecast.ProjectForecast(uNow, st.prior.Mu0, st.cal.BarTauSq, st.cal.SigmaSessionSq, dt)
	return Predictive{Mu: fc.F, Sigma: fc.SigmaF}, true
}

// --- FlatNow baseline: predict no further growth ---------------------------

type FlatNow struct{}

func (FlatNow) Name() string { return "flat-now" }
func (FlatNow) Train(train []forecast.Session, cfg forecast.Config) any {
	return fitStandard(train, cfg)
}
func (FlatNow) Predict(state any, history []forecast.Snapshot, uNow float64, now, reset time.Time, cfg forecast.Config) (Predictive, bool) {
	st := state.(fitState)
	dt := reset.Sub(now).Hours()
	if dt <= 0 {
		return Predictive{}, false
	}
	fc := forecast.ProjectForecast(uNow, 0, st.cal.BarTauSq, st.cal.SigmaSessionSq, dt)
	return Predictive{Mu: fc.F, Sigma: fc.SigmaF}, true
}

// --- Climatology baseline: ignore the session, predict the historical mean --
// The reference forecast for CRPS skill: any useful model must beat it.

type Climatology struct{}

func (Climatology) Name() string { return "climatology" }
func (Climatology) Train(train []forecast.Session, cfg forecast.Config) any {
	return fitStandard(train, cfg)
}
func (Climatology) Predict(state any, history []forecast.Snapshot, uNow float64, now, reset time.Time, cfg forecast.Config) (Predictive, bool) {
	st := state.(fitState)
	sd := st.sdU
	if sd <= 0 {
		sd = 0.1
	}
	return Predictive{Mu: st.meanU, Sigma: sd}, true
}

// DefaultStrategies is the standard comparison set.
func DefaultStrategies() []Strategy {
	return []Strategy{Current{}, Mu0{}, FlatNow{}, Climatology{}}
}
