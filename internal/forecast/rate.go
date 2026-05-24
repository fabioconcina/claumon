package forecast

import (
	"math"
	"time"
)

// olsFit fits u_i = alpha + r * t_i by ordinary least squares. The times are
// supplied in hours; the caller is responsible for the unit choice.
//
// Returns (rHat, seOlsSq, ok). ok is false when n < 3 or S_tt == 0.
func olsFit(snaps []Snapshot) (float64, float64, bool) {
	n := len(snaps)
	if n < 3 {
		return 0, 0, false
	}

	t0 := snaps[0].Time
	ts := make([]float64, n)
	us := make([]float64, n)
	var tBar, uBar float64
	for i, s := range snaps {
		ts[i] = s.Time.Sub(t0).Hours()
		us[i] = s.U
		tBar += ts[i]
		uBar += us[i]
	}
	tBar /= float64(n)
	uBar /= float64(n)

	var sTT, sTU float64
	for i := 0; i < n; i++ {
		dt := ts[i] - tBar
		du := us[i] - uBar
		sTT += dt * dt
		sTU += dt * du
	}
	if sTT <= 0 {
		return 0, 0, false
	}

	rHat := sTU / sTT
	alpha := uBar - rHat*tBar

	var rss float64
	for i := 0; i < n; i++ {
		res := us[i] - alpha - rHat*ts[i]
		rss += res * res
	}
	sigmaEpsSq := rss / float64(n-2)
	seOlsSq := sigmaEpsSq / sTT
	return rHat, seOlsSq, true
}

// EstimatePosterior performs §3: fit OLS on the current window (if n >= 3) and
// fuse with the prior via normal-normal conjugacy. Falls back to the prior when
// OLS is not estimable.
func EstimatePosterior(recent []Snapshot, prior Prior) Posterior {
	rOLS, seSq, ok := olsFit(recent)
	if !ok || seSq <= 0 || prior.Tau0Sq <= 0 {
		return Posterior{
			RHat:      prior.Mu0,
			TauPostSq: prior.Tau0Sq,
			N:         len(recent),
			UsedOLS:   false,
		}
	}

	precPrior := 1.0 / prior.Tau0Sq
	precData := 1.0 / seSq
	postPrec := precPrior + precData
	tauPostSq := 1.0 / postPrec
	rHat := tauPostSq * (prior.Mu0*precPrior + rOLS*precData)

	if math.IsNaN(rHat) || math.IsInf(rHat, 0) {
		return Posterior{
			RHat:      prior.Mu0,
			TauPostSq: prior.Tau0Sq,
			N:         len(recent),
			UsedOLS:   false,
		}
	}

	return Posterior{
		RHat:      rHat,
		TauPostSq: tauPostSq,
		N:         len(recent),
		SEolsSq:   seSq,
		UsedOLS:   true,
	}
}

// FitPrior derives the Gaussian prior on r from completed past sessions, with
// the §3 noise correction subtracting the average path-noise contribution.
//
// Each Session is one historical session with its final utilization u* and
// total duration D (hours). Sessions with D <= 0 are skipped. When fewer than
// two usable sessions remain the prior is undefined and ok=false.
//
// sigmaSessionSq is the most recent calibration value; pass 0 on the very
// first fit (the correction is then a no-op).
func FitPrior(sessions []Session, sigmaSessionSq, varianceEps float64) (Prior, bool) {
	rhos := make([]float64, 0, len(sessions))
	invDs := make([]float64, 0, len(sessions))
	for _, s := range sessions {
		if s.DurationHours <= 0 {
			continue
		}
		rhos = append(rhos, s.UFinal/s.DurationHours)
		invDs = append(invDs, 1.0/s.DurationHours)
	}
	if len(rhos) < 2 {
		return Prior{}, false
	}

	mu0 := mean(rhos)
	rawVar := sampleVar(rhos, mu0)
	correction := sigmaSessionSq * mean(invDs)
	tau0Sq := rawVar - correction
	if tau0Sq < varianceEps {
		tau0Sq = varianceEps
	}

	return Prior{
		Mu0:       mu0,
		Tau0Sq:    tau0Sq,
		NSessions: len(rhos),
	}, true
}

// Session is one completed historical session, used by FitPrior and the
// calibration replay. Reset is the reset time (end of the window) and the
// snapshots are timestamped observations strictly within the session.
type Session struct {
	Reset         time.Time
	DurationHours float64
	UFinal        float64
	Snapshots     []Snapshot
}

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func sampleVar(xs []float64, m float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var s float64
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return s / float64(len(xs)-1)
}
