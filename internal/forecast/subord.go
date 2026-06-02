package forecast

import (
	"math"
	"math/rand"
)

// Gamma sampling for the monotone (positive-increment) Monte Carlo in eta.go.
// Utilization within a window only grows, so the §8 simulation draws both the
// per-path rate and each per-step increment from a Gamma law: every draw is
// >= 0, so trajectories never dip below their start, while the first two
// moments still match what the Brownian model used.

// sampleGammaMeanVar draws a non-negative Gamma variate specified by its mean
// and variance rather than shape/scale. Degenerate cases collapse gracefully:
// a non-positive mean yields 0 (no growth - e.g. a flat or idle stretch), and a
// non-positive variance yields the mean exactly (deterministic drift).
func sampleGammaMeanVar(rng *rand.Rand, mean, variance float64) float64 {
	if mean <= 0 {
		return 0
	}
	if variance <= 0 {
		return mean
	}
	shape := mean * mean / variance
	scale := variance / mean
	return sampleGamma(rng, shape, scale)
}

// sampleGamma draws from Gamma(shape, scale) by Marsaglia & Tsang (2000). It
// needs only standard-normal and uniform draws, both taken from the
// deterministic rng eta.go seeds, so trajectories remain reproducible for a
// fixed seed. shape < 1 is handled by the standard boost; a non-positive shape
// returns 0.
func sampleGamma(rng *rand.Rand, shape, scale float64) float64 {
	if shape <= 0 {
		return 0
	}
	if shape < 1 {
		// Gamma(shape) == Gamma(shape+1) * U^(1/shape).
		u := rng.Float64()
		for u <= 0 {
			u = rng.Float64()
		}
		return sampleGamma(rng, shape+1, scale) * math.Pow(u, 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		x := rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		x2 := x * x
		// Squeeze, then the exact acceptance test.
		if u < 1-0.0331*x2*x2 {
			return d * v * scale
		}
		if math.Log(u) < 0.5*x2+d*(1-v+math.Log(v)) {
			return d * v * scale
		}
	}
}
