package bench

import "math"

// Scoring rules for a Gaussian predictive against a realized value y (the
// session's final utilization). CRPS and pinball are proper - they can't be
// improved by reporting a wider interval than you believe - so they rank
// strategies honestly, unlike coverage or MAE alone.

const invSqrtPi = 0.5641895835477563 // 1/sqrt(pi)

func phi(x float64) float64    { return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi) }
func bigPhi(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// crps returns the CRPS of a predictive against y, dispatching on its
// representation: an unbiased sample estimator when the predictive carries MC
// draws (the monotone v2.0 forecast), else the exact Gaussian closed form. Both
// estimate the same quantity with no systematic bias, so values are comparable
// across strategies even when only some arms are sampled.
func crps(p Predictive, y float64) float64 {
	if len(p.Sample) > 0 {
		return crpsSample(p.Sample, y)
	}
	return crpsGaussian(p.Mu, p.Sigma, y)
}

// crpsGaussian is the closed-form CRPS of N(mu, sigma^2) at y. Lower is better.
// CRPS(N(mu,s),y) = s*[ w*(2*Phi(w)-1) + 2*phi(w) - 1/sqrt(pi) ], w=(y-mu)/s.
func crpsGaussian(mu, sigma, y float64) float64 {
	if sigma <= 0 {
		return math.Abs(y - mu) // degenerate: CRPS reduces to absolute error
	}
	w := (y - mu) / sigma
	return sigma * (w*(2*bigPhi(w)-1) + 2*phi(w) - invSqrtPi)
}

// crpsSample is the unbiased sample estimator of CRPS from a SORTED sample.
// CRPS = E|X-y| - 1/2 E|X-X'|; the pairwise term divides by K(K-1) (off-diagonal
// pairs only), which removes the ~1/K low bias of the naive divide-by-K^2
// plug-in - so a sampled arm is not flattered relative to an analytic one. The
// sorted identity Sum_{i<j}(x_(j)-x_(i)) = Sum_i (2i+1-K) x_(i) makes it O(K).
func crpsSample(sorted []float64, y float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	if n == 1 {
		return math.Abs(sorted[0] - y)
	}
	var term1, weighted float64
	for i, x := range sorted {
		term1 += math.Abs(x - y)
		weighted += float64(2*i+1-n) * x // i 0-indexed
	}
	term1 /= float64(n)
	return term1 - weighted/(float64(n)*float64(n-1)) // weighted/(n(n-1)) = 1/2 E|X-X'|
}

// quantileOf returns the tau-quantile of a predictive: an empirical quantile of
// the sorted sample when present, else the Gaussian Mu + z(tau)*Sigma. Each arm
// is thus scored against its own correct quantiles (a skewed predictive is not
// forced through a symmetric z-interval).
func quantileOf(p Predictive, tau float64) float64 {
	if len(p.Sample) > 0 {
		return sampleQuantile(p.Sample, tau)
	}
	return p.Mu + zFor(tau)*p.Sigma
}

// sampleQuantile is the tau-quantile of a SORTED sample by linear interpolation
// on rank (same convention as the forecaster's MC percentile).
func sampleQuantile(sorted []float64, tau float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	if n == 1 {
		return sorted[0]
	}
	rank := tau * float64(n-1)
	if rank <= 0 {
		return sorted[0]
	}
	if rank >= float64(n-1) {
		return sorted[n-1]
	}
	lo := int(math.Floor(rank))
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[lo+1]*frac
}

// pinball is the quantile (pinball) loss at level tau for forecast quantile q.
func pinball(y, q, tau float64) float64 {
	if y >= q {
		return (y - q) * tau
	}
	return (q - y) * (1 - tau)
}

// zFor returns the standard-normal quantile for a few fixed levels we report.
func zFor(tau float64) float64 {
	switch tau {
	case 0.1:
		return -1.2815515594
	case 0.5:
		return 0
	case 0.9:
		return 1.2815515594
	}
	// generic fallback (rational approximation, adequate for benchmark levels)
	return ppndGeneric(tau)
}

// meanPinball averages pinball loss over the 10/50/90 levels, using each
// predictive's own quantiles (empirical for a sampled arm, Gaussian otherwise).
func meanPinball(p Predictive, y float64) float64 {
	levels := [3]float64{0.1, 0.5, 0.9}
	var sum float64
	for _, tau := range levels {
		sum += pinball(y, quantileOf(p, tau), tau)
	}
	return sum / 3
}

// covered80 reports whether y falls in the central 80% interval of p, using the
// predictive's own 10th/90th quantiles.
func covered80(p Predictive, y float64) bool {
	return y >= quantileOf(p, 0.1) && y <= quantileOf(p, 0.9)
}

// ppndGeneric is a Beasley-Springer-Moro style inverse normal CDF, used only
// for non-standard levels (the reported 10/50/90 are tabulated above).
func ppndGeneric(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	plow := 0.02425
	phigh := 1 - plow
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= phigh:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
}
