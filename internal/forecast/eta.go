package forecast

import (
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"time"
)

// EstimateETA implements §6: Monte Carlo simulation of the first-passage time
// to threshold under (drift + Brownian) dynamics with Gaussian rate
// uncertainty.
//
// Returns nil when the threshold is unreachable in [now, reset] under the
// model (p_infty >= 0.5). When 0.1 <= p_infty < 0.5 the returned ETA has a
// finite median and lower bound but nil upper bound (open-ended).
//
// The RNG seed is deterministic in the inputs so tests reproduce.
func EstimateETA(now, reset time.Time, uNow float64, post Posterior, cal Calibration, threshold float64, cfg Config) *ETA {
	if threshold <= uNow {
		// §8: threshold already crossed. Use distinct values per field so a
		// caller mutating one (unusual but possible) can't alias-mutate the
		// others.
		med, lo, up := now, now, now
		return &ETA{Median: &med, Lower: &lo, Upper: &up}
	}
	horizon := reset.Sub(now)
	if horizon <= 0 {
		return nil
	}

	step := cfg.MCStep
	if step <= 0 {
		step = 5 * time.Minute
	}
	// Cap step at the full horizon so very short windows still run at least one step.
	if step > horizon {
		step = horizon
	}
	nSteps := int(math.Ceil(horizon.Seconds() / step.Seconds()))
	if nSteps < 1 {
		nSteps = 1
	}
	dt := horizon.Seconds() / float64(nSteps) / 3600.0 // hours per step
	sigmaStep := math.Sqrt(cal.SigmaSessionSq * dt)
	tauPost := math.Sqrt(math.Max(post.TauPostSq, 0))

	rng := rand.New(rand.NewSource(seedFrom(now, reset, uNow, post, cal, threshold)))

	K := cfg.MCTraj
	finite := make([]float64, 0, K)
	infCount := 0

	for k := 0; k < K; k++ {
		rk := post.RHat + tauPost*rng.NormFloat64()

		u := uNow
		var hitHours float64 = math.Inf(1)
		for j := 1; j <= nSteps; j++ {
			uPrev := u
			noise := 0.0
			if sigmaStep > 0 {
				noise = sigmaStep * rng.NormFloat64()
			}
			u = uPrev + rk*dt + noise
			if u >= threshold {
				// Linear interpolation between j-1 and j.
				frac := 0.0
				if u != uPrev {
					frac = (threshold - uPrev) / (u - uPrev)
				}
				hitHours = (float64(j-1) + frac) * dt
				break
			}
		}

		if math.IsInf(hitHours, 1) {
			infCount++
		} else {
			finite = append(finite, hitHours)
		}
	}

	pInf := float64(infCount) / float64(K)
	if pInf >= 0.5 {
		return &ETA{PInf: pInf}
	}

	sort.Float64s(finite)
	medianHours := percentile(finite, 0.5, len(finite)+infCount)
	medTime := now.Add(time.Duration(medianHours * float64(time.Hour)))

	var lower, upper *time.Time
	if pInf < 0.1 {
		lh := percentile(finite, 0.1, len(finite))
		uh := percentile(finite, 0.9, len(finite))
		lt := now.Add(time.Duration(lh * float64(time.Hour)))
		ut := now.Add(time.Duration(uh * float64(time.Hour)))
		lower, upper = &lt, &ut
	} else {
		lh := percentile(finite, 0.1, len(finite))
		lt := now.Add(time.Duration(lh * float64(time.Hour)))
		lower = &lt
		upper = nil
	}

	return &ETA{
		Median: &medTime,
		Lower:  lower,
		Upper:  upper,
		PInf:   pInf,
	}
}

// percentile returns the p-quantile of the sorted slice xs, treating sample
// size as totalN (so callers can pass the *full* sample including the +Inf
// tail by passing totalN > len(xs)). The quantile is by linear interpolation
// on rank; when the requested rank falls into the +Inf region we return
// math.Inf(1).
func percentile(sorted []float64, p float64, totalN int) float64 {
	if totalN == 0 {
		return math.NaN()
	}
	if len(sorted) == 0 {
		return math.Inf(1)
	}
	// Rank in 1..totalN.
	rank := p * float64(totalN-1)
	finiteCount := len(sorted)
	if rank >= float64(finiteCount) {
		return math.Inf(1)
	}
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if hi >= finiteCount {
		hi = finiteCount - 1
	}
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func seedFrom(now, reset time.Time, uNow float64, post Posterior, cal Calibration, thr float64) int64 {
	h := fnv.New64a()
	var buf [8]byte
	putI64(&buf, now.UnixNano())
	h.Write(buf[:])
	putI64(&buf, reset.UnixNano())
	h.Write(buf[:])
	putU64(&buf, math.Float64bits(uNow))
	h.Write(buf[:])
	putU64(&buf, math.Float64bits(post.RHat))
	h.Write(buf[:])
	putU64(&buf, math.Float64bits(post.TauPostSq))
	h.Write(buf[:])
	putU64(&buf, math.Float64bits(cal.SigmaSessionSq))
	h.Write(buf[:])
	putU64(&buf, math.Float64bits(thr))
	h.Write(buf[:])
	return int64(h.Sum64())
}

func putI64(buf *[8]byte, v int64) {
	for i := 0; i < 8; i++ {
		buf[i] = byte(v >> (8 * i))
	}
}

func putU64(buf *[8]byte, v uint64) {
	for i := 0; i < 8; i++ {
		buf[i] = byte(v >> (8 * i))
	}
}
