package learning

import (
	"fmt"
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
)

// Schedule uses the upstream FSRS-6 scheduler without changing its equations.
// Correctness and hint use constrain the rating; elapsed answer time is logged
// for diagnostics, not passed off as an FSRS input.
func Schedule(card *fsrs.Card, now time.Time, result string, usedHint bool, rating int) (fsrs.SchedulingInfo, error) {
	if rating < 1 || rating > 4 {
		return fsrs.SchedulingInfo{}, fmt.Errorf("invalid rating")
	}
	if result != "correct" && result != "wrong" {
		return fsrs.SchedulingInfo{}, fmt.Errorf("unresolved answer")
	}
	if (result == "wrong" || usedHint) && rating != int(fsrs.Again) {
		return fsrs.SchedulingInfo{}, fmt.Errorf("failed recall requires Again")
	}
	current := fsrs.NewCard(now)
	if card != nil {
		current = *card
	}
	return fsrs.NewFSRS(fsrs.DefaultParam()).Next(current, now, fsrs.Rating(rating))
}

func Preview(card *fsrs.Card, now time.Time) (map[int]time.Time, error) {
	current := fsrs.NewCard(now)
	if card != nil {
		current = *card
	}
	records, err := fsrs.NewFSRS(fsrs.DefaultParam()).Repeat(current, now)
	if err != nil {
		return nil, err
	}
	due := make(map[int]time.Time, 4)
	for rating, record := range records {
		due[int(rating)] = record.Card.Due
	}
	return due, nil
}
