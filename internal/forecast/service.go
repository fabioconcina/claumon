package forecast

import (
	"log"
	"sync"
	"time"
)

// SessionDuration is the fixed 5-hour Claude session window.
const SessionDuration = 5 * time.Hour

// WeeklyDuration is the 7-day weekly window.
const WeeklyDuration = 7 * 24 * time.Hour

// GaugeKind is "session" or "weekly".
type GaugeKind string

const (
	GaugeSession GaugeKind = "session"
	GaugeWeekly  GaugeKind = "weekly"
)

// Store is the minimal interface the service needs from the persistence
// layer. Defined here so the forecast package doesn't import internal/store
// directly (and so tests can stub it out).
type Store interface {
	GetWindowSnapshots(gauge, resetAt string, since time.Time) ([]StoreSnapshot, error)
	GetCompletedSessions(gauge string, before time.Time, limit int) ([]StoreSession, error)
}

// StoreSnapshot mirrors store.ForecastSnapshot. Kept separate so this package
// has no upward dependency on internal/store.
type StoreSnapshot struct {
	Time time.Time
	U    float64
}

// StoreSession mirrors store.ForecastSession.
type StoreSession struct {
	ResetAt   time.Time
	UFinal    float64
	Snapshots []StoreSnapshot
}

// State is the per-gauge fitted state: prior on r and calibration on path
// noise. It's regenerated daily.
type State struct {
	Prior       Prior
	Calibration Calibration
	FitAt       time.Time
}

// Service owns the per-gauge fitted state and produces forecasts on demand.
// The state is fit at startup and re-fit by Refit (typically called daily).
type Service struct {
	st  Store
	cfg Config

	mu     sync.RWMutex
	states map[GaugeKind]State
}

// NewService constructs a service. Call Refit before the first forecast to
// populate prior + calibration; ForecastFor returns (_, false) for any gauge
// that has not been fit yet.
func NewService(st Store, cfg Config) *Service {
	return &Service{
		st:     st,
		cfg:    cfg.withDefaults(),
		states: make(map[GaugeKind]State),
	}
}

// Refit refreshes the prior and calibration for one gauge from the store.
// Returns false when there is not enough history to fit.
func (s *Service) Refit(gauge GaugeKind, now time.Time) bool {
	sessions, err := s.st.GetCompletedSessions(string(gauge), now, 200)
	if err != nil {
		log.Printf("[forecast] %s: load completed sessions: %v", gauge, err)
		return false
	}
	fcSessions := make([]Session, 0, len(sessions))
	dur := durationFor(gauge)
	for _, sess := range sessions {
		// FitPrior only needs UFinal and DurationHours; CalibrateSigmaSession
		// applies its own <3-snapshot filter internally. Keep all sessions
		// here so the prior gets the widest possible sample.
		snaps := make([]Snapshot, len(sess.Snapshots))
		for i, sn := range sess.Snapshots {
			snaps[i] = Snapshot{Time: sn.Time, U: sn.U}
		}
		fcSessions = append(fcSessions, Session{
			Reset:         sess.ResetAt,
			DurationHours: dur.Hours(),
			UFinal:        sess.UFinal,
			Snapshots:     snaps,
		})
	}

	// First-pass prior with sigma=0 (no path-noise correction).
	prior, ok := FitPrior(fcSessions, 0, s.cfg.VarianceEps)
	if !ok {
		log.Printf("[forecast] %s: prior fit skipped (%d usable sessions)", gauge, len(fcSessions))
		return false
	}
	// Calibrate sigma using that prior.
	cal := CalibrateSigmaSession(fcSessions, prior, s.cfg, 6, 30*time.Minute)
	// Refit the prior with the new sigma to apply the noise correction. The
	// spec calls out this loose coupling and says the daily refit cycle
	// converges quickly; one extra pass is enough.
	prior2, ok := FitPrior(fcSessions, cal.SigmaSessionSq, s.cfg.VarianceEps)
	if ok {
		prior = prior2
	}

	s.mu.Lock()
	s.states[gauge] = State{Prior: prior, Calibration: cal, FitAt: now}
	s.mu.Unlock()
	log.Printf("[forecast] %s: refit — sessions=%d mu0=%.4f tau0^2=%.2e sigma^2=%.2e",
		gauge, prior.NSessions, prior.Mu0, prior.Tau0Sq, cal.SigmaSessionSq)
	return true
}

// RefitAll refits every gauge. Best-effort; failures are logged.
func (s *Service) RefitAll(now time.Time) {
	for _, g := range []GaugeKind{GaugeSession, GaugeWeekly} {
		s.Refit(g, now)
	}
}

// State returns a copy of the fitted state for one gauge.
func (s *Service) State(gauge GaugeKind) (State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[gauge]
	return st, ok
}

// ForecastFor produces a forecast for one gauge at the given moment. It pulls
// the recent snapshot window from the store, runs Run, and returns the
// result. Returns ok=false when there is no fitted state, no open window, or
// no recent snapshots.
func (s *Service) ForecastFor(gauge GaugeKind, resetAt string, uNowPct float64, now time.Time, thresholdsPct []float64) (Result, bool) {
	reset, err := time.Parse(time.RFC3339, resetAt)
	if err != nil || !reset.After(now) {
		return Result{}, false
	}
	state, ok := s.State(gauge)
	if !ok {
		return Result{}, false
	}

	since := now.Add(-2 * s.cfg.TauRecent) // small slack so the recency filter sees enough points
	snaps, err := s.st.GetWindowSnapshots(string(gauge), resetAt, since)
	if err != nil {
		log.Printf("[forecast] %s: load window snapshots: %v", gauge, err)
		return Result{}, false
	}
	// The caller writes the current snapshot to the store before invoking
	// ForecastFor, so it's already in `snaps` (at CURRENT_TIMESTAMP, ~ms
	// behind `now`). We don't append uNowPct again to avoid double-weighting
	// the present in the OLS fit.
	uNow := uNowPct / 100.0

	in := Input{
		Now:         now,
		Reset:       reset,
		UNow:        uNow,
		Snapshots:   storeSnapsToForecast(snaps),
		Prior:       state.Prior,
		Calibration: state.Calibration,
		Thresholds:  pctSliceToFraction(thresholdsPct),
	}
	return Run(in, s.cfg)
}

func storeSnapsToForecast(ss []StoreSnapshot) []Snapshot {
	out := make([]Snapshot, len(ss))
	for i, s := range ss {
		out[i] = Snapshot{Time: s.Time, U: s.U}
	}
	return out
}

func pctSliceToFraction(pcts []float64) []float64 {
	out := make([]float64, len(pcts))
	for i, p := range pcts {
		out[i] = p / 100.0
	}
	return out
}

func durationFor(g GaugeKind) time.Duration {
	switch g {
	case GaugeWeekly:
		return WeeklyDuration
	default:
		return SessionDuration
	}
}
