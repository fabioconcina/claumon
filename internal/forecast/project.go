package forecast

import "math"

// z90 is Phi^{-1}(0.9), the one-sided 80% CI half-width multiplier.
const z90 = 1.2815515655446004

// ProjectForecast implements §4: turn (u_now, posterior rate, calibration,
// horizon) into a Gaussian forecast at reset with 80% CI.
//
// Inputs are dimensionless u, per-hour rate, per-hour^2 rate-variance, per-hour
// path-noise variance, and a horizon in hours. F is left unclipped; Lower and
// Upper are clipped to [uNow, 1] for display, since utilization within a
// reset window is monotone non-decreasing.
func ProjectForecast(uNow, rHat, tauPostSq, sigmaSessionSq, deltaTHours float64) Forecast {
	f := uNow + rHat*deltaTHours
	rateVar := deltaTHours * deltaTHours * tauPostSq
	pathVar := deltaTHours * sigmaSessionSq
	sigmaF := math.Sqrt(rateVar + pathVar)
	floor := clip01(uNow)
	return Forecast{
		F:      f,
		SigmaF: sigmaF,
		Lower:  math.Max(floor, clip01(f-z90*sigmaF)),
		Upper:  math.Max(floor, clip01(f+z90*sigmaF)),
		DeltaT: deltaTHours,
	}
}
