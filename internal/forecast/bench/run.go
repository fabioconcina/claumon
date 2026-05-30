package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fabioconcina/claumon/internal/forecast"
)

// Options controls the replay sampling, matching the calibration/diagnostics
// defaults so benchmark points line up with the rest of the pipeline.
type Options struct {
	Config     forecast.Config
	PerSession int
	MinHorizon time.Duration
}

func (o Options) withDefaults() Options {
	if o.PerSession <= 0 {
		o.PerSession = 6
	}
	if o.MinHorizon <= 0 {
		o.MinHorizon = 30 * time.Minute
	}
	if o.Config.TauRecent == 0 {
		o.Config = forecast.DefaultConfig()
	}
	return o
}

// Metrics is one strategy's aggregated out-of-sample score on a segment.
type Metrics struct {
	Strategy   string
	N          int
	CRPS       float64 // mean CRPS (lower better) - the headline
	CRPSSkill  float64 // 1 - CRPS/CRPS_climatology (higher better; >0 beats climatology)
	Pinball    float64 // mean pinball over 10/50/90
	Coverage80 float64 // fraction in the 80% interval (want ~0.80)
	MAEpp      float64 // mean |Mu - u*| in percentage points
	Biaspp     float64 // mean (Mu - u*) in percentage points
}

// Segment names. "overall" pools everything; the rest split the same points by
// session engagement (final u* vs the median) and by forecast horizon (Δt vs
// the median), so a strategy that wins on average but loses on the
// high-engagement / long-horizon cases (the ones near a limit) is visible.
const (
	segOverall   = "overall"
	segEngaged   = "engaged"
	segAbandoned = "abandoned"
	segNear      = "horizon-near"
	segFar       = "horizon-far"
)

var segOrder = []string{segOverall, segEngaged, segAbandoned, segNear, segFar}

// StrategyResult is one strategy's metrics across all segments.
type StrategyResult struct {
	Strategy string
	BySeg    map[string]Metrics
}

func (r StrategyResult) overall() Metrics { return r.BySeg[segOverall] }

type acc struct {
	sumCRPS, sumPin, sumAbs, sumErr float64
	covered, n                      int
}

func (a *acc) add(p Predictive, y float64) {
	a.sumCRPS += crpsGaussian(p.Mu, p.Sigma, y)
	a.sumPin += meanPinball(p, y)
	a.sumAbs += abs(p.Mu - y)
	a.sumErr += p.Mu - y
	if covered80(p, y) {
		a.covered++
	}
	a.n++
}

func (a acc) metrics(strategy string) Metrics {
	if a.n == 0 {
		return Metrics{Strategy: strategy}
	}
	n := float64(a.n)
	return Metrics{
		Strategy: strategy, N: a.n,
		CRPS:       a.sumCRPS / n,
		Pinball:    a.sumPin / n,
		Coverage80: float64(a.covered) / n,
		MAEpp:      100 * a.sumAbs / n,
		Biaspp:     100 * a.sumErr / n,
	}
}

// RunSegmented trains and scores every strategy over the folds, accumulating
// per segment. All scoring is out-of-sample: a strategy only ever sees its
// fold's training sessions when fitting.
func RunSegmented(strats []Strategy, folds []Fold, opt Options) []StrategyResult {
	opt = opt.withDefaults()
	medU, medDt := thresholds(folds, opt)

	// accs[strategy][segment]
	accs := make([]map[string]*acc, len(strats))
	for i := range accs {
		accs[i] = make(map[string]*acc, len(segOrder))
		for _, s := range segOrder {
			accs[i][s] = &acc{}
		}
	}

	for _, fold := range folds {
		states := make([]any, len(strats))
		for i, s := range strats {
			states[i] = s.Train(fold.Train, opt.Config)
		}
		for _, test := range fold.Test {
			engage := segAbandoned
			if test.UFinal >= medU {
				engage = segEngaged
			}
			for _, pt := range samplePoints(test, opt) {
				horizon := segNear
				if test.Reset.Sub(pt.now).Hours() >= medDt {
					horizon = segFar
				}
				for i, s := range strats {
					p, ok := s.Predict(states[i], pt.history, pt.uNow, pt.now, test.Reset, opt.Config)
					if !ok {
						continue
					}
					accs[i][segOverall].add(p, test.UFinal)
					accs[i][engage].add(p, test.UFinal)
					accs[i][horizon].add(p, test.UFinal)
				}
			}
		}
	}

	// climatology CRPS per segment is the skill reference.
	climCRPS := make(map[string]float64)
	for i, s := range strats {
		if s.Name() == "climatology" {
			for _, seg := range segOrder {
				climCRPS[seg] = accs[i][seg].metrics(s.Name()).CRPS
			}
		}
	}

	out := make([]StrategyResult, len(strats))
	for i, s := range strats {
		res := StrategyResult{Strategy: s.Name(), BySeg: make(map[string]Metrics, len(segOrder))}
		for _, seg := range segOrder {
			m := accs[i][seg].metrics(s.Name())
			if c := climCRPS[seg]; c > 0 {
				m.CRPSSkill = 1 - m.CRPS/c
			}
			res.BySeg[seg] = m
		}
		out[i] = res
	}
	return out
}

// Run returns the overall (pooled) metrics per strategy - the flat view used by
// the golden tests. It is RunSegmented projected onto the "overall" segment.
func Run(strats []Strategy, folds []Fold, opt Options) []Metrics {
	res := RunSegmented(strats, folds, opt)
	out := make([]Metrics, len(res))
	for i, r := range res {
		out[i] = r.overall()
	}
	return out
}

// thresholds computes the median final-utilization (engagement split) and the
// median forecast horizon (horizon split) over all test points, so the segment
// boundaries adapt to the dataset (session vs weekly, light vs heavy use).
func thresholds(folds []Fold, opt Options) (medU, medDt float64) {
	var us, dts []float64
	for _, fold := range folds {
		for _, test := range fold.Test {
			us = append(us, test.UFinal)
			for _, pt := range samplePoints(test, opt) {
				dts = append(dts, test.Reset.Sub(pt.now).Hours())
			}
		}
	}
	return median(us), median(dts)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}

type point struct {
	history []forecast.Snapshot
	uNow    float64
	now     time.Time
}

func samplePoints(s forecast.Session, opt Options) []point {
	if len(s.Snapshots) < 3 {
		return nil
	}
	tStart := s.Snapshots[0].Time
	earliest := tStart.Add(opt.Config.TauRecent)
	latest := s.Reset.Add(-opt.MinHorizon)
	if !latest.After(earliest) {
		return nil
	}
	var pts []point
	for k := 0; k < opt.PerSession; k++ {
		frac := (float64(k) + 0.5) / float64(opt.PerSession)
		tf := earliest.Add(time.Duration(frac * float64(latest.Sub(earliest))))
		uNow, ok := interpAt(s.Snapshots, tf)
		if !ok {
			continue
		}
		hist := snapshotsUpTo(s.Snapshots, tf)
		if len(hist) < 1 {
			continue
		}
		pts = append(pts, point{history: hist, uNow: uNow, now: tf})
	}
	return pts
}

func snapshotsUpTo(snaps []forecast.Snapshot, tf time.Time) []forecast.Snapshot {
	out := make([]forecast.Snapshot, 0, len(snaps))
	for _, s := range snaps {
		if !s.Time.After(tf) {
			out = append(out, s)
		}
	}
	return out
}

func interpAt(snaps []forecast.Snapshot, tf time.Time) (float64, bool) {
	if len(snaps) == 0 {
		return 0, false
	}
	if !tf.After(snaps[0].Time) {
		return snaps[0].U, true
	}
	last := snaps[len(snaps)-1]
	if !tf.Before(last.Time) {
		return last.U, true
	}
	for i := 1; i < len(snaps); i++ {
		if !snaps[i].Time.Before(tf) {
			a, b := snaps[i-1], snaps[i]
			total := b.Time.Sub(a.Time).Seconds()
			if total == 0 {
				return a.U, true
			}
			frac := tf.Sub(a.Time).Seconds() / total
			return a.U + frac*(b.U-a.U), true
		}
	}
	return last.U, true
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Report prints the overall table only, sorted by CRPS (best first).
func Report(datasetName, protocol string, ms []Metrics) string {
	sorted := append([]Metrics(nil), ms...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CRPS < sorted[j].CRPS })
	var b strings.Builder
	fmt.Fprintf(&b, "benchmark: %s   protocol: %s\n", datasetName, protocol)
	header(&b, "strategy")
	for _, m := range sorted {
		row(&b, m.Strategy, m)
	}
	fmt.Fprintln(&b, "  (CRPS/pinball: lower better; skill>0 beats climatology; cov80 wants ~80%)")
	return b.String()
}

// ReportSegmented prints each strategy with its per-segment breakdown,
// strategies ordered by overall CRPS.
func ReportSegmented(datasetName, protocol string, rs []StrategyResult) string {
	sorted := append([]StrategyResult(nil), rs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].overall().CRPS < sorted[j].overall().CRPS })
	var b strings.Builder
	fmt.Fprintf(&b, "benchmark: %s   protocol: %s\n", datasetName, protocol)
	header(&b, "strategy / segment")
	for _, r := range sorted {
		for _, seg := range segOrder {
			m := r.BySeg[seg]
			label := "  " + seg
			if seg == segOverall {
				label = r.Strategy
			}
			row(&b, label, m)
		}
	}
	fmt.Fprintln(&b, "  (CRPS/pinball: lower better; skill>0 beats climatology; cov80 wants ~80%)")
	fmt.Fprintln(&b, "  segments: engaged/abandoned split at median final u*; horizon near/far at median Δt")
	return b.String()
}

func header(b *strings.Builder, first string) {
	fmt.Fprintf(b, "  %-20s %7s %8s %9s %8s %8s %8s %6s\n",
		first, "CRPS", "skill", "pinball", "cov80", "MAE pp", "bias pp", "n")
}

func row(b *strings.Builder, label string, m Metrics) {
	fmt.Fprintf(b, "  %-20s %7.4f %+7.1f%% %9.4f %7.1f%% %8.2f %+8.2f %6d\n",
		label, m.CRPS, 100*m.CRPSSkill, m.Pinball, 100*m.Coverage80, m.MAEpp, m.Biaspp, m.N)
}
