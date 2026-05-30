package bench

import (
	"sort"

	"github.com/fabioconcina/claumon/internal/forecast"
)

// Fold is one train/test split: fit a strategy on Train, score it on Test.
type Fold struct {
	Train []forecast.Session
	Test  []forecast.Session
}

// LOSO yields one fold per session: train on all the others, test on the one
// held out. Gold standard for small samples - every session is scored fully
// out-of-sample, and the fit sees the largest possible training set each time.
func LOSO(sessions []forecast.Session) []Fold {
	folds := make([]Fold, 0, len(sessions))
	for i := range sessions {
		train := make([]forecast.Session, 0, len(sessions)-1)
		train = append(train, sessions[:i]...)
		train = append(train, sessions[i+1:]...)
		folds = append(folds, Fold{Train: train, Test: sessions[i : i+1]})
	}
	return folds
}

// TemporalSplit fits on the earliest trainFrac of sessions (by reset time) and
// tests on the rest. Mimics deployment and catches drift, since the model only
// ever sees the past - unlike LOSO, which lets future sessions inform the fit.
func TemporalSplit(sessions []forecast.Session, trainFrac float64) Fold {
	sorted := append([]forecast.Session(nil), sessions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Reset.Before(sorted[j].Reset) })
	cut := int(trainFrac * float64(len(sorted)))
	if cut < 1 {
		cut = 1
	}
	if cut > len(sorted)-1 {
		cut = len(sorted) - 1
	}
	return Fold{Train: sorted[:cut], Test: sorted[cut:]}
}
