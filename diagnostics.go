//go:build benchtools

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
	"github.com/fabioconcina/claumon/internal/store"
)

// runDiagnostics replays the forecaster across past completed sessions and
// prints calibration metrics (80% CI coverage, MAE of F, ETA accuracy).
// Useful for deciding whether a model change actually improves accuracy.
func runDiagnostics() {
	fs := flag.NewFlagSet("diagnostics", flag.ExitOnError)
	gauge := fs.String("gauge", "session", "gauge to score: session or weekly")
	limit := fs.Int("limit", 200, "max completed sessions to replay (0 = all)")
	perSession := fs.Int("per-session", 6, "forecast points per session")
	threshold := fs.Float64("threshold", 100.0, "threshold percent for ETA scoring")
	_ = fs.Parse(os.Args[2:])

	cfg := loadConfig()
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if *gauge != "session" && *gauge != "weekly" {
		log.Fatalf("gauge must be session or weekly, got %q", *gauge)
	}

	rows, err := st.GetCompletedSessions(*gauge, time.Now(), *limit)
	if err != nil {
		log.Fatalf("load sessions: %v", err)
	}
	if len(rows) < 2 {
		log.Fatalf("need at least 2 completed sessions, found %d", len(rows))
	}

	dur := forecast.SessionDuration
	if *gauge == "weekly" {
		dur = forecast.WeeklyDuration
	}
	sessions := make([]forecast.Session, len(rows))
	for i, r := range rows {
		snaps := make([]forecast.Snapshot, len(r.Snapshots))
		for j, sn := range r.Snapshots {
			snaps[j] = forecast.Snapshot{Time: sn.Time, U: sn.U}
		}
		sessions[i] = forecast.Session{
			Reset:         r.ResetAt,
			DurationHours: dur.Hours(),
			UFinal:        r.UFinal,
			Snapshots:     snaps,
		}
	}

	// Fit prior and calibration just as the live service does on refit.
	fcCfg := forecast.DefaultConfig()
	prior, ok := forecast.FitPrior(sessions, 0, fcCfg.VarianceEps)
	if !ok {
		log.Fatalf("FitPrior: not enough usable sessions")
	}
	cal := forecast.CalibrateSigmaSession(sessions, prior, fcCfg, 6, 30*time.Minute)
	prior2, ok := forecast.FitPrior(sessions, cal.SigmaSessionSq, fcCfg.VarianceEps)
	if ok {
		prior = prior2
	}

	fmt.Printf("gauge=%s sessions=%d mu0=%.4f tau0Sq=%.2e sigmaSessionSq=%.2e barTauSq=%.2e\n\n",
		*gauge, prior.NSessions, prior.Mu0, prior.Tau0Sq, cal.SigmaSessionSq, cal.BarTauSq)

	d := forecast.Score(sessions, prior, cal, fcCfg, *perSession, 30*time.Minute, *threshold)
	fmt.Print(d.Report())
}
