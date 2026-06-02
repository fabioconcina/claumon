package bench

import (
	"math"
	"testing"
)

// Golden synthetic benchmark. These tests pin RELATIONSHIPS between strategies
// on known-ground-truth regimes, not exact metric values - so they stay stable
// across model tweaks while still catching a change that breaks a property we
// rely on. Each assertion encodes a lesson from the model investigation; if one
// fails, the model's qualitative behavior has regressed.
//
// Datasets are generated with a fixed seed, so runs are reproducible.

const goldenN = 60
const goldenSeed = 1

func runRegime(regime, protocol string, trainFrac float64) []Metrics {
	ds := Synthetic(regime, goldenN, goldenSeed)
	var folds []Fold
	if protocol == "temporal" {
		folds = []Fold{TemporalSplit(ds.Sessions, trainFrac)}
	} else {
		folds = LOSO(ds.Sessions)
	}
	return Run(DefaultStrategies(), folds, Options{})
}

func byName(t *testing.T, ms []Metrics, name string) Metrics {
	t.Helper()
	for _, m := range ms {
		if m.Strategy == name {
			return m
		}
	}
	t.Fatalf("strategy %q not found in results", name)
	return Metrics{}
}

// linear: a constant within-session rate. Recency is estimating the right
// thing, so it must NOT hurt - current ≈ mu0 and both ~unbiased. This is the
// fairness guard: if calibration ever makes recency hurt on constant-rate data,
// something is wrong.
func TestGoldenLinearRecencyHarmless(t *testing.T) {
	ms := runRegime("linear", "loso", 0)
	cur := byName(t, ms, "current")
	mu0 := byName(t, ms, "mu0")
	if math.Abs(cur.Biaspp) > 3 {
		t.Errorf("current should be ~unbiased on linear, bias=%.2f pp", cur.Biaspp)
	}
	if math.Abs(mu0.Biaspp) > 3 {
		t.Errorf("mu0 should be ~unbiased on linear, bias=%.2f pp", mu0.Biaspp)
	}
	if cur.CRPS > 1.25*mu0.CRPS {
		t.Errorf("recency must not materially hurt on linear: current CRPS %.4f vs mu0 %.4f", cur.CRPS, mu0.CRPS)
	}
	if cur.CRPSSkill < 0.30 {
		t.Errorf("current should beat climatology comfortably on linear, skill=%.2f", cur.CRPSSkill)
	}
	if cur.Coverage80 < 0.60 || cur.Coverage80 > 0.95 {
		t.Errorf("80%% interval miscalibrated on linear: coverage=%.2f", cur.Coverage80)
	}
}

// decel: the rate decays within the session, so extrapolating the recent slope
// over-predicts. current must be worse than mu0 and biased high. This is the
// concave-overshoot finding that drove the whole point-estimate investigation.
// Under v2.0 (scored on the actual skewed MC predictive, not a moment Gaussian)
// the CRPS gap narrows to ~6% from the v1.x ~10%+: the right-skewed law's mode
// sits below the mean, so it covers the low truth (y < F) a little better. The
// relationship is unchanged - current is still worse and still biased high - so
// the threshold is relaxed to a material-gap check and the bias check carries
// the "clearly over-predicts" signal.
func TestGoldenDecelRecencyOverpredicts(t *testing.T) {
	ms := runRegime("decel", "loso", 0)
	cur := byName(t, ms, "current")
	mu0 := byName(t, ms, "mu0")
	if cur.CRPS <= 1.03*mu0.CRPS {
		t.Errorf("recency should be materially worse than mu0 on decel: current CRPS %.4f vs mu0 %.4f", cur.CRPS, mu0.CRPS)
	}
	if cur.Biaspp <= 3 {
		t.Errorf("current should over-predict on decel, bias=%.2f pp", cur.Biaspp)
	}
}

// abandonment: active, then the rate goes to zero. mu0 (assume historical rate
// continues) beats recency here, but still over-predicts because growth has
// stopped - the headroom an idle-detector would exploit.
func TestGoldenAbandonmentMu0BeatsRecency(t *testing.T) {
	ms := runRegime("abandonment", "loso", 0)
	cur := byName(t, ms, "current")
	mu0 := byName(t, ms, "mu0")
	if mu0.CRPS >= 0.95*cur.CRPS {
		t.Errorf("mu0 should beat recency on abandonment: mu0 CRPS %.4f vs current %.4f", mu0.CRPS, cur.CRPS)
	}
}

// drift: the rate grows across sessions. Under a TEMPORAL split (train on the
// past, test on the future) mu0's prior is stale and under-predicts, while
// recency tracks the trend. current must beat mu0 and mu0 must under-cover.
// This is the main-device finding: don't discard recency where usage drifts.
func TestGoldenDriftRecencyAdaptsTemporal(t *testing.T) {
	ms := runRegime("drift", "temporal", 0.6)
	cur := byName(t, ms, "current")
	mu0 := byName(t, ms, "mu0")
	if cur.CRPS >= 0.85*mu0.CRPS {
		t.Errorf("recency must adapt and beat mu0 under temporal drift: current CRPS %.4f vs mu0 %.4f", cur.CRPS, mu0.CRPS)
	}
	if mu0.Coverage80 >= 0.60 {
		t.Errorf("mu0 should under-cover under temporal drift (stale prior), coverage=%.2f", mu0.Coverage80)
	}
	if cur.Coverage80 < 0.60 {
		t.Errorf("current should stay roughly calibrated under temporal drift, coverage=%.2f", cur.Coverage80)
	}
}

// Smoke: every regime generates usable sessions and the harness scores all
// strategies without dropping points.
func TestGoldenRegimesProduceData(t *testing.T) {
	for _, r := range []string{"linear", "decel", "abandonment", "bursty", "drift"} {
		ds := Synthetic(r, goldenN, goldenSeed)
		if len(ds.Sessions) != goldenN {
			t.Errorf("%s: got %d sessions, want %d", r, len(ds.Sessions), goldenN)
		}
		ms := Run(DefaultStrategies(), LOSO(ds.Sessions), Options{})
		for _, m := range ms {
			if m.N == 0 {
				t.Errorf("%s: strategy %s scored 0 points", r, m.Strategy)
			}
		}
	}
}
