// Package bench is the forecast model benchmark harness. It scores forecast
// strategies out-of-sample (leave-one-session-out and temporal holdout) on
// reproducible datasets - frozen exports of real session history and synthetic
// regimes with known ground-truth dynamics - using proper scoring rules (CRPS,
// pinball) alongside interpretable coverage/MAE/bias breakdowns.
//
// It deliberately does not import internal/store; freezing real data into a
// fixture is the CLI's job (claumon bench export). This package only loads
// fixtures and generates synthetic data, so benchmark runs are reproducible.
package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
)

// Dataset is a named, reproducible collection of completed sessions.
type Dataset struct {
	Name     string
	Sessions []forecast.Session
}

// --- fixture (de)serialization -------------------------------------------

type snapJSON struct {
	T time.Time `json:"t"`
	U float64   `json:"u"`
}

type sessionJSON struct {
	Reset         time.Time  `json:"reset"`
	DurationHours float64    `json:"duration_hours"`
	UFinal        float64    `json:"u_final"`
	Snapshots     []snapJSON `json:"snapshots"`
}

type fixtureJSON struct {
	Name     string        `json:"name"`
	Sessions []sessionJSON `json:"sessions"`
}

// Save writes a dataset to a JSON fixture file.
func Save(path string, ds Dataset) error {
	fx := fixtureJSON{Name: ds.Name}
	for _, s := range ds.Sessions {
		sj := sessionJSON{Reset: s.Reset, DurationHours: s.DurationHours, UFinal: s.UFinal}
		for _, sn := range s.Snapshots {
			sj.Snapshots = append(sj.Snapshots, snapJSON{T: sn.Time, U: sn.U})
		}
		fx.Sessions = append(fx.Sessions, sj)
	}
	b, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads a dataset from a JSON fixture file.
func Load(path string) (Dataset, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Dataset{}, err
	}
	var fx fixtureJSON
	if err := json.Unmarshal(b, &fx); err != nil {
		return Dataset{}, err
	}
	ds := Dataset{Name: fx.Name}
	for _, sj := range fx.Sessions {
		s := forecast.Session{Reset: sj.Reset, DurationHours: sj.DurationHours, UFinal: sj.UFinal}
		for _, sn := range sj.Snapshots {
			s.Snapshots = append(s.Snapshots, forecast.Snapshot{Time: sn.T, U: sn.U})
		}
		ds.Sessions = append(ds.Sessions, s)
	}
	return ds, nil
}

// --- synthetic regimes ----------------------------------------------------

// Synthetic builds n sessions of the given regime with a fixed seed, so the
// dataset is reproducible. Regimes have known dynamics, which lets us check a
// strategy behaves correctly without real-data confounds:
//
//	linear        constant within-session rate         -> recency should be unbiased
//	decel         rate decays exponentially with time  -> recency over-predicts
//	abandonment   active for a while, then rate 0       -> regime switch; idle-detector wins
//	bursty        alternating active/idle 30-min blocks -> recent slope noisy/biased
//	drift         rate grows across sessions (linear    -> non-stationary; under a
//	              within each)                              temporal split recency must
//	                                                        adapt and beat mu0
//
// All trajectories are monotone non-decreasing (clamped increments), matching
// real utilization. Sessions are 5h with 5-minute snapshots.
func Synthetic(regime string, n int, seed int64) Dataset {
	rng := rand.New(rand.NewSource(seed))
	const stepMin = 5
	const noiseSq = 5e-4
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ds := Dataset{Name: "synthetic:" + regime}

	for i := 0; i < n; i++ {
		start := base.Add(time.Duration(i) * 6 * time.Hour)
		reset := start.Add(forecast.SessionDuration)
		dur := forecast.SessionDuration.Hours()

		// per-session parameters
		var rate0, tau, tStop, pActive, rateHigh float64
		switch regime {
		case "linear":
			rate0 = clampPos(0.08 + 0.03*rng.NormFloat64())
		case "decel":
			rate0 = clampPos(0.25 + 0.08*rng.NormFloat64())
			tau = 1.5 // hours
		case "abandonment":
			rate0 = clampPos(0.12 + 0.04*rng.NormFloat64())
			tStop = 0.5 + 2.5*rng.Float64() // active for 0.5..3h
		case "bursty":
			rateHigh = clampPos(0.18 + 0.06*rng.NormFloat64())
			pActive = 0.5
		case "drift":
			// per-session rate ramps from ~0.04/h (early) to ~0.14/h (late);
			// constant within each session, so only the cross-session trend
			// matters - a temporal split must let recency track it.
			denom := float64(n - 1)
			if denom < 1 {
				denom = 1
			}
			rate0 = clampPos(0.04 + 0.10*(float64(i)/denom) + 0.02*rng.NormFloat64())
		default:
			rate0 = 0.08
		}

		snaps := []forecast.Snapshot{{Time: start, U: 0}}
		u := 0.0
		for m := stepMin; float64(m)/60.0 <= dur+1e-9; m += stepMin {
			tHours := float64(m) / 60.0
			dt := float64(stepMin) / 60.0
			var r float64
			switch regime {
			case "linear":
				r = rate0
			case "decel":
				r = rate0 * math.Exp(-tHours/tau)
			case "abandonment":
				if tHours <= tStop {
					r = rate0
				}
			case "bursty":
				// new active/idle draw each 30-min block
				block := int(tHours / 0.5)
				if blockActive(rng, block, i, pActive) {
					r = rateHigh
				}
			case "drift":
				r = rate0
			default:
				r = rate0
			}
			inc := r*dt + math.Sqrt(noiseSq*dt)*rng.NormFloat64()
			if inc < 0 {
				inc = 0
			}
			u += inc
			if u > 1 {
				u = 1
			}
			snaps = append(snaps, forecast.Snapshot{Time: start.Add(time.Duration(m) * time.Minute), U: u})
		}
		ds.Sessions = append(ds.Sessions, forecast.Session{
			Reset: reset, DurationHours: dur, UFinal: u, Snapshots: snaps,
		})
	}
	return ds
}

func clampPos(x float64) float64 {
	if x < 1e-3 {
		return 1e-3
	}
	return x
}

// blockActive returns a deterministic active/idle decision per (session, block)
// so bursty sessions are reproducible without threading extra rng state.
func blockActive(rng *rand.Rand, block, session int, p float64) bool {
	return rng.Float64() < p
}

// String summarises a dataset.
func (d Dataset) String() string {
	var moved int
	var sumU float64
	for _, s := range d.Sessions {
		sumU += s.UFinal
		if len(s.Snapshots) >= 3 {
			moved++
		}
	}
	mean := 0.0
	if len(d.Sessions) > 0 {
		mean = sumU / float64(len(d.Sessions))
	}
	return fmt.Sprintf("%s: %d sessions, mean u*=%.1f%%", d.Name, len(d.Sessions), 100*mean)
}
