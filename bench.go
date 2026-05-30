package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
	"github.com/fabioconcina/claumon/internal/forecast/bench"
	"github.com/fabioconcina/claumon/internal/store"
)

// runBench is the `claumon bench` subcommand: a forecast model benchmark.
//
//	claumon bench export [--gauge session] [--out file.json]
//	    freeze the store's completed sessions into a reproducible fixture
//	claumon bench run [--dataset file.json | --synthetic linear|decel|abandonment|bursty]
//	                  [--protocol loso|temporal] [--gauge session]
//	    train/score the strategy set out-of-sample and print the table
func runBench() {
	if len(os.Args) < 3 {
		fmt.Println("usage: claumon bench <export|run> [flags]")
		os.Exit(2)
	}
	switch os.Args[2] {
	case "export":
		benchExport()
	case "run":
		benchRun()
	default:
		fmt.Printf("unknown bench subcommand %q (want export|run)\n", os.Args[2])
		os.Exit(2)
	}
}

func benchExport() {
	fs := flag.NewFlagSet("bench export", flag.ExitOnError)
	gauge := fs.String("gauge", "session", "gauge: session or weekly")
	out := fs.String("out", "", "output fixture path (default bench-<gauge>.json)")
	db := fs.String("db", "", "SQLite DB to read (default: this machine's configured DB). Point at an exported DB from another device.")
	_ = fs.Parse(os.Args[3:])
	if *out == "" {
		*out = "bench-" + *gauge + ".json"
	}

	dbPath := *db
	if dbPath == "" {
		dbPath = loadConfig().DBPath
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store (%s): %v", dbPath, err)
	}
	defer st.Close()
	sessions := loadBenchSessions(st, *gauge)
	ds := bench.Dataset{Name: fmt.Sprintf("export:%s", *gauge), Sessions: sessions}
	if err := bench.Save(*out, ds); err != nil {
		log.Fatalf("save fixture: %v", err)
	}
	fmt.Printf("wrote %s (%s)\n", *out, ds.String())
}

func benchRun() {
	fs := flag.NewFlagSet("bench run", flag.ExitOnError)
	dataset := fs.String("dataset", "", "fixture file to load")
	synth := fs.String("synthetic", "", "synthetic regime: linear|decel|abandonment|bursty")
	nSynth := fs.Int("n", 60, "synthetic session count")
	seed := fs.Int64("seed", 1, "synthetic seed")
	protocol := fs.String("protocol", "loso", "loso or temporal")
	trainFrac := fs.Float64("train-frac", 0.6, "temporal split training fraction")
	gauge := fs.String("gauge", "session", "gauge when loading live store (no --dataset/--synthetic)")
	_ = fs.Parse(os.Args[3:])

	var ds bench.Dataset
	switch {
	case *synth != "":
		ds = bench.Synthetic(*synth, *nSynth, *seed)
	case *dataset != "":
		var err error
		ds, err = bench.Load(*dataset)
		if err != nil {
			log.Fatalf("load fixture: %v", err)
		}
	default:
		cfg := loadConfig()
		st, err := store.Open(cfg.DBPath)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
		defer st.Close()
		ds = bench.Dataset{Name: "live:" + *gauge, Sessions: loadBenchSessions(st, *gauge)}
	}

	if len(ds.Sessions) < 3 {
		log.Fatalf("need >=3 sessions, got %d (%s)", len(ds.Sessions), ds.Name)
	}

	var folds []bench.Fold
	switch *protocol {
	case "loso":
		folds = bench.LOSO(ds.Sessions)
	case "temporal":
		folds = []bench.Fold{bench.TemporalSplit(ds.Sessions, *trainFrac)}
	default:
		log.Fatalf("protocol must be loso or temporal")
	}

	fmt.Printf("%s\n", ds.String())
	rs := bench.RunSegmented(bench.DefaultStrategies(), folds, bench.Options{})
	fmt.Print(bench.ReportSegmented(ds.Name, *protocol, rs))
}

func loadBenchSessions(st *store.Store, gauge string) []forecast.Session {
	rows, err := st.GetCompletedSessions(gauge, time.Now(), 0)
	if err != nil {
		log.Fatalf("load sessions: %v", err)
	}
	dur := forecast.SessionDuration
	if gauge == "weekly" {
		dur = forecast.WeeklyDuration
	}
	sessions := make([]forecast.Session, len(rows))
	for i, r := range rows {
		snaps := make([]forecast.Snapshot, len(r.Snapshots))
		for j, sn := range r.Snapshots {
			snaps[j] = forecast.Snapshot{Time: sn.Time, U: sn.U}
		}
		sessions[i] = forecast.Session{Reset: r.ResetAt, DurationHours: dur.Hours(), UFinal: r.UFinal, Snapshots: snaps}
	}
	return sessions
}
