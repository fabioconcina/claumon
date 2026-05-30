package forecast

import "time"

// Payload is the JSON-friendly per-gauge forecast snapshot. Times are
// emitted as RFC3339 strings (or "" when nil). Percentages are 0-100.
type Payload struct {
	ModelVersion string       `json:"model_version"`
	ProjectedPct float64      `json:"projected_pct"`
	Lower80Pct   float64      `json:"lower_80_pct"`
	Upper80Pct   float64      `json:"upper_80_pct"`
	SigmaPct     float64      `json:"sigma_pct"`
	RatePerHour  float64      `json:"rate_per_hour_pct"`
	UsedOLS      bool         `json:"used_ols"`
	HorizonHours float64      `json:"horizon_hours"`
	ETAs         []ETAPayload `json:"etas,omitempty"`
}

// ETAPayload reports the first-passage forecast for one threshold (in 0-100
// percent). Median/Lower/Upper are RFC3339 timestamps or empty.
type ETAPayload struct {
	ThresholdPct float64 `json:"threshold_pct"`
	Median       string  `json:"median,omitempty"`
	Lower        string  `json:"lower,omitempty"`
	Upper        string  `json:"upper,omitempty"`
	PInf         float64 `json:"p_inf"`
}

// ToPayload converts a Result and its threshold list (in percent) to the
// JSON-friendly payload.
func (r Result) ToPayload(thresholdsPct []float64) Payload {
	p := Payload{
		ModelVersion: ModelVersion,
		ProjectedPct: r.Forecast.F * 100,
		Lower80Pct:   r.Forecast.Lower * 100,
		Upper80Pct:   r.Forecast.Upper * 100,
		SigmaPct:     r.Forecast.SigmaF * 100,
		RatePerHour:  r.Posterior.RHat * 100,
		UsedOLS:      r.Posterior.UsedOLS,
		HorizonHours: r.Forecast.DeltaT,
	}
	for _, thrPct := range thresholdsPct {
		thr := thrPct / 100.0
		eta, ok := r.ETAs[thr]
		if !ok || eta == nil {
			continue
		}
		p.ETAs = append(p.ETAs, etaToPayload(thrPct, eta))
	}
	return p
}

func etaToPayload(thrPct float64, e *ETA) ETAPayload {
	out := ETAPayload{ThresholdPct: thrPct, PInf: e.PInf}
	if e.Median != nil {
		out.Median = e.Median.UTC().Format(time.RFC3339)
	}
	if e.Lower != nil {
		out.Lower = e.Lower.UTC().Format(time.RFC3339)
	}
	if e.Upper != nil {
		out.Upper = e.Upper.UTC().Format(time.RFC3339)
	}
	return out
}

// ObservedPoint is one (t, u) snapshot for the visualization line.
type ObservedPoint struct {
	TimeISO string  `json:"t"`
	U       float64 `json:"u"`
}

// SamplePayload is the wire format for the forecast modal. Everything the
// frontend needs to render the trajectory fog, empirical 10/90 band,
// posterior-mean line, and first-passage histogram comes from one of these.
//
// Trajectories are subsampled before encoding; CrossingsH covers the full K
// sample so the histogram still reflects the unsubsampled MC.
type SamplePayload struct {
	ModelVersion string          `json:"model_version"`
	TStartISO    string          `json:"t_start"`
	TNowISO      string          `json:"t_now"`
	TResetISO    string          `json:"t_reset"`
	UNow         float64         `json:"u_now"`
	F            float64         `json:"f"`
	CILo         float64         `json:"ci_lo"`
	CIHi         float64         `json:"ci_hi"`
	ThresholdPct float64         `json:"threshold_pct"`
	StepHours    float64         `json:"step_hours"`
	Observed     []ObservedPoint `json:"observed"`
	Trajectories [][]float64     `json:"trajectories"`
	CrossingsH   []float64       `json:"crossings_h"`
	PInf         float64         `json:"p_inf"`
	NTraj        int             `json:"n_traj"`
	ETA          ETAPayload      `json:"eta"`
}
