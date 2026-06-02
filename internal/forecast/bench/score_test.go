package bench

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// The unbiased sample CRPS of a large Gaussian sample must converge to the
// closed-form Gaussian CRPS. This pins the estimator (the divide-by-K(K-1)
// form): a biased plug-in would not converge to crpsGaussian, and the two
// scoring paths must agree for cross-strategy comparisons to be honest.
func TestCRPSSampleConvergesToGaussian(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	const mu, sigma = 0.40, 0.15
	const k = 50000
	sample := make([]float64, k)
	for i := range sample {
		sample[i] = mu + sigma*rng.NormFloat64()
	}
	sort.Float64s(sample)

	for _, y := range []float64{0.05, 0.40, 0.85} {
		got := crpsSample(sample, y)
		want := crpsGaussian(mu, sigma, y)
		if math.Abs(got-want) > 0.005 {
			t.Errorf("crpsSample at y=%.2f: got %.5f, want ~%.5f (closed-form Gaussian)", y, got, want)
		}
	}
}

// Degenerate inputs collapse cleanly: a one-element sample reduces CRPS to
// absolute error, and the quantile is that single value.
func TestSampleScorersDegenerate(t *testing.T) {
	if got := crpsSample([]float64{0.3}, 0.5); math.Abs(got-0.2) > 1e-12 {
		t.Errorf("single-sample CRPS should be |0.3-0.5|=0.2, got %v", got)
	}
	if got := sampleQuantile([]float64{0.42}, 0.9); got != 0.42 {
		t.Errorf("single-sample quantile should be the value, got %v", got)
	}
}

// Empirical quantiles of a sorted sample are monotone in tau and land at u_now
// or above for a floored predictive - the property the v2.0 CI relies on.
func TestSampleQuantileMonotone(t *testing.T) {
	sample := []float64{0.30, 0.30, 0.31, 0.35, 0.40, 0.55, 0.72} // sorted, floored at 0.30
	lo := sampleQuantile(sample, 0.1)
	mid := sampleQuantile(sample, 0.5)
	hi := sampleQuantile(sample, 0.9)
	if !(lo <= mid && mid <= hi) {
		t.Errorf("quantiles not monotone: lo=%v mid=%v hi=%v", lo, mid, hi)
	}
	if lo < 0.30-1e-12 {
		t.Errorf("lower quantile dipped below the sample floor: %v", lo)
	}
}
