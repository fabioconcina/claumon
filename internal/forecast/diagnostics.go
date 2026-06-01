package forecast

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Diagnostics summarises forecaster calibration over past completed sessions.
// Same replay loop as calibration.go but instead of fitting sigma we score
// the forecast each replay point produced against what actually happened.
type Diagnostics struct {
	ModelVersion string
	NSessions    int
	NReplays     int
	// Forecast at reset
	CoverageF80    float64 // fraction of replays where u_final fell in [F-, F+]
	MAEFpct        float64 // mean absolute error of F (in percentage points)
	BiasFpct       float64 // mean(F - u_final), pct points; +ve = over-predicting
	// Spread sanity check: if mean_e2 / mean_predicted_variance >> 1, the
	// 80% CI is too narrow; if << 1, it's too wide. Calibrated model -> ~1.
	MeanE2         float64 // mean of (u_final - F)^2 across replays
	MeanPredVar    float64 // mean of sigma_F^2 used by the forecast at each replay
	UnderspreadX   float64 // MeanE2 / MeanPredVar; 1.0 means calibrated
	MeanEffRateVar float64 // mean of EffectiveRateVar(tau_post^2, bar_tau^2)
	MeanTauPostSq  float64 // mean of the conjugate tau_post^2 (pre-floor)
	BarTauSq       float64 // calibration's bar tau^2 (the floor value)
	// ETA
	NETAFinite     int     // replays where the forecast emitted a finite median ETA AND threshold was crossed in reality
	MAEEtaMin      float64 // mean abs error of MC median ETA vs actual crossing time, minutes
	BiasEtaMin     float64 // mean(eta_median - actual_crossing), minutes
	CoverageEta80  float64 // fraction with finite ETA CI where actual crossing was in [lower, upper]
	NETACovered    int     // denominator for CoverageEta80
	// Per-horizon coverage of F80 (binned by remaining hours at replay)
	HorizonBins    []HorizonBin
}

// HorizonBin reports per-remaining-horizon F80 coverage so we can see whether
// miscalibration is concentrated at long or short horizons.
type HorizonBin struct {
	HoursLow    float64
	HoursHigh   float64
	N           int
	CoverageF80 float64
}

// Score replays the forecaster across past sessions and computes calibration
// metrics. forecastsPerSession (default 6) and minHorizon (default 30 min)
// match CalibrateSigmaSession. thresholdPct is the threshold whose ETA we
// score (default 100 if zero).
func Score(sessions []Session, prior Prior, cal Calibration, cfg Config, forecastsPerSession int, minHorizon time.Duration, thresholdPct float64) Diagnostics {
	cfg = cfg.withDefaults()
	if forecastsPerSession <= 0 {
		forecastsPerSession = 6
	}
	if minHorizon <= 0 {
		minHorizon = 30 * time.Minute
	}
	if thresholdPct <= 0 {
		thresholdPct = 100
	}
	threshold := thresholdPct / 100

	d := Diagnostics{ModelVersion: ModelVersion, BarTauSq: cal.BarTauSq}
	var (
		coveredF       int
		sumAbsErr      float64
		sumErr         float64
		sumE2          float64
		sumPredVar     float64
		sumEffRateVar  float64
		sumTauPostSq   float64
		etaErrors      []float64 // minutes
		etaSignedErr   []float64
		etaCovered     int
		etaCoverable   int
	)

	// 4 horizon bins, log-ish
	bins := []HorizonBin{
		{HoursLow: 0, HoursHigh: 0.5},
		{HoursLow: 0.5, HoursHigh: 1.5},
		{HoursLow: 1.5, HoursHigh: 3.5},
		{HoursLow: 3.5, HoursHigh: math.Inf(1)},
	}
	binCovered := make([]int, len(bins))

	for _, s := range sessions {
		if len(s.Snapshots) < 3 || s.DurationHours <= 0 {
			continue
		}
		d.NSessions++

		tStart := s.Snapshots[0].Time
		earliest := tStart.Add(cfg.TauRecent)
		latest := s.Reset.Add(-minHorizon)
		if !latest.After(earliest) {
			continue
		}

		// Did this session actually cross the threshold? If so, when?
		actualCrossH, crossed := firstCrossing(s.Snapshots, threshold, tStart)

		for k := 0; k < forecastsPerSession; k++ {
			frac := (float64(k) + 0.5) / float64(forecastsPerSession)
			tf := earliest.Add(time.Duration(frac * float64(latest.Sub(earliest))))

			recent := filterRecent(s.Snapshots, tf, cfg.TauRecent)
			if len(recent) < 3 {
				continue
			}
			uAtTf, ok := interpAt(s.Snapshots, tf)
			if !ok {
				continue
			}
			post := EstimatePosterior(recent, prior)
			deltaH := s.Reset.Sub(tf).Hours()
			if deltaH <= 0 {
				continue
			}

			rateVar := EffectiveRateVar(post.TauPostSq, cal.BarTauSq)
			fc := ProjectForecast(uAtTf, post.RHat, rateVar, cal.SigmaSessionSq, deltaH)

			// F coverage + error
			d.NReplays++
			errPct := (fc.F - s.UFinal) * 100
			sumErr += errPct
			sumAbsErr += math.Abs(errPct)
			// Track e^2 (in fraction units) and the model's variance for the
			// underspread ratio. SigmaF is the unclipped sqrt of the sum of
			// rate-uncertainty and path-noise terms; squaring it gives the
			// model variance the CI was built from.
			eFrac := fc.F - s.UFinal
			sumE2 += eFrac * eFrac
			sumPredVar += fc.SigmaF * fc.SigmaF
			sumEffRateVar += rateVar
			sumTauPostSq += post.TauPostSq
			covered := s.UFinal >= fc.Lower-1e-9 && s.UFinal <= fc.Upper+1e-9
			if covered {
				coveredF++
			}
			for i, b := range bins {
				if deltaH >= b.HoursLow && deltaH < b.HoursHigh {
					bins[i].N++
					if covered {
						binCovered[i]++
					}
					break
				}
			}

			// ETA scoring: only when the threshold was actually crossed AFTER tf
			if !crossed {
				continue
			}
			tCrossH := tStart.Add(time.Duration(actualCrossH * float64(time.Hour)))
			if !tCrossH.After(tf) {
				continue // already crossed at this replay point — no ETA to score
			}
			eta := EstimateETA(tf, s.Reset, uAtTf, post, cal, threshold, cfg)
			if eta == nil || eta.Median == nil {
				continue
			}
			d.NETAFinite++
			errMin := eta.Median.Sub(tCrossH).Minutes()
			etaSignedErr = append(etaSignedErr, errMin)
			etaErrors = append(etaErrors, math.Abs(errMin))
			if eta.Lower != nil && eta.Upper != nil {
				etaCoverable++
				if !tCrossH.Before(*eta.Lower) && !tCrossH.After(*eta.Upper) {
					etaCovered++
				}
			}
		}
	}

	if d.NReplays > 0 {
		n := float64(d.NReplays)
		d.CoverageF80 = float64(coveredF) / n
		d.MAEFpct = sumAbsErr / n
		d.BiasFpct = sumErr / n
		d.MeanE2 = sumE2 / n
		d.MeanPredVar = sumPredVar / n
		if d.MeanPredVar > 0 {
			d.UnderspreadX = d.MeanE2 / d.MeanPredVar
		}
		d.MeanEffRateVar = sumEffRateVar / n
		d.MeanTauPostSq = sumTauPostSq / n
	}
	if d.NETAFinite > 0 {
		d.MAEEtaMin = mean(etaErrors)
		d.BiasEtaMin = mean(etaSignedErr)
	}
	if etaCoverable > 0 {
		d.CoverageEta80 = float64(etaCovered) / float64(etaCoverable)
		d.NETACovered = etaCoverable
	}

	for i := range bins {
		if bins[i].N > 0 {
			bins[i].CoverageF80 = float64(binCovered[i]) / float64(bins[i].N)
		}
	}
	d.HorizonBins = bins
	return d
}

// firstCrossing returns the hours-since-tStart at which the snapshot series
// first reaches threshold (linear interpolation between adjacent snapshots).
func firstCrossing(snaps []Snapshot, threshold float64, tStart time.Time) (float64, bool) {
	for i := 0; i < len(snaps); i++ {
		if snaps[i].U >= threshold {
			if i == 0 {
				return 0, true
			}
			a, b := snaps[i-1], snaps[i]
			if b.U == a.U {
				return b.Time.Sub(tStart).Hours(), true
			}
			frac := (threshold - a.U) / (b.U - a.U)
			tCross := a.Time.Add(time.Duration(frac * float64(b.Time.Sub(a.Time))))
			return tCross.Sub(tStart).Hours(), true
		}
	}
	return 0, false
}

// Report formats the diagnostics for terminal display.
func (d Diagnostics) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Forecast diagnostics (model %s)\n", d.ModelVersion)
	fmt.Fprintf(&b, "  sessions replayed:   %d\n", d.NSessions)
	fmt.Fprintf(&b, "  forecast points:     %d\n", d.NReplays)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "F at reset:")
	fmt.Fprintf(&b, "  80%% CI coverage:    %s (want ~80%%)\n", pct(d.CoverageF80))
	fmt.Fprintf(&b, "  MAE:                 %.2f pp\n", d.MAEFpct)
	fmt.Fprintf(&b, "  bias (F - u*):       %+.2f pp\n", d.BiasFpct)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Spread sanity (mean across replays):")
	fmt.Fprintf(&b, "  mean e^2:            %.3e\n", d.MeanE2)
	fmt.Fprintf(&b, "  mean predicted var:  %.3e\n", d.MeanPredVar)
	fmt.Fprintf(&b, "  underspread ratio:   %.2fx (1.0 = calibrated; >1 under-spread)\n", d.UnderspreadX)
	fmt.Fprintf(&b, "  mean tau_post^2:     %.3e (conjugate, pre-floor)\n", d.MeanTauPostSq)
	fmt.Fprintf(&b, "  bar tau^2 floor:     %.3e (from calibration b_hat)\n", d.BarTauSq)
	fmt.Fprintf(&b, "  mean effective rateVar: %.3e (max of the two)\n", d.MeanEffRateVar)
	if len(d.HorizonBins) > 0 {
		fmt.Fprintln(&b, "  per-horizon coverage:")
		for _, h := range d.HorizonBins {
			if h.N == 0 {
				continue
			}
			hi := fmt.Sprintf("%.1fh", h.HoursHigh)
			if math.IsInf(h.HoursHigh, 1) {
				hi = "inf"
			}
			fmt.Fprintf(&b, "    [%.1f, %s)  n=%4d  cov=%s\n", h.HoursLow, hi, h.N, pct(h.CoverageF80))
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "ETA (MC median vs actual crossing):")
	if d.NETAFinite == 0 {
		fmt.Fprintln(&b, "  no scorable ETA points (no sessions crossed threshold post-replay)")
	} else {
		fmt.Fprintf(&b, "  scorable replays:    %d\n", d.NETAFinite)
		fmt.Fprintf(&b, "  MAE:                 %.1f min\n", d.MAEEtaMin)
		fmt.Fprintf(&b, "  bias (eta - actual): %+.1f min (positive = late)\n", d.BiasEtaMin)
		if d.NETACovered > 0 {
			fmt.Fprintf(&b, "  80%% CI coverage:    %s (n=%d with finite bounds)\n", pct(d.CoverageEta80), d.NETACovered)
		}
	}
	return b.String()
}

func pct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }
