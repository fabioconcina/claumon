package bench

import "math"

// Scoring rules for a Gaussian predictive against a realized value y (the
// session's final utilization). CRPS and pinball are proper - they can't be
// improved by reporting a wider interval than you believe - so they rank
// strategies honestly, unlike coverage or MAE alone.

const invSqrtPi = 0.5641895835477563 // 1/sqrt(pi)

func phi(x float64) float64  { return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi) }
func bigPhi(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// crpsGaussian is the closed-form CRPS of N(mu, sigma^2) at y. Lower is better.
// CRPS(N(mu,s),y) = s*[ w*(2*Phi(w)-1) + 2*phi(w) - 1/sqrt(pi) ], w=(y-mu)/s.
func crpsGaussian(mu, sigma, y float64) float64 {
	if sigma <= 0 {
		return math.Abs(y - mu) // degenerate: CRPS reduces to absolute error
	}
	w := (y - mu) / sigma
	return sigma * (w*(2*bigPhi(w)-1) + 2*phi(w) - invSqrtPi)
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

// meanPinball averages pinball loss over the 10/50/90 levels for a Gaussian.
func meanPinball(p Predictive, y float64) float64 {
	levels := [3]float64{0.1, 0.5, 0.9}
	var sum float64
	for _, tau := range levels {
		q := p.Mu + zFor(tau)*p.Sigma
		sum += pinball(y, q, tau)
	}
	return sum / 3
}

// covered80 reports whether y falls in the central 80% interval of p.
func covered80(p Predictive, y float64) bool {
	lo := p.Mu + zFor(0.1)*p.Sigma
	hi := p.Mu + zFor(0.9)*p.Sigma
	return y >= lo && y <= hi
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
