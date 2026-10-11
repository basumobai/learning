package learning

import (
	"testing"
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
)

func TestFSRSAgreementAndFailedRecall(t *testing.T) {
	now := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	original := fsrs.NewCard(now)
	for rating := 1; rating <= 4; rating++ {
		got, err := Schedule(&original, now, "correct", false, rating)
		want, wantErr := fsrs.NewFSRS(fsrs.DefaultParam()).Next(original, now, fsrs.Rating(rating))
		if err != nil || wantErr != nil || got != want {
			t.Fatalf("rating %d: %v %v; got %+v want %+v", rating, err, wantErr, got, want)
		}
	}
	for _, result := range []string{"wrong", "pending"} {
		if _, err := Schedule(nil, now, result, false, 3); err == nil {
			t.Fatalf("%s cannot be scheduled as successful recall", result)
		}
	}
	if _, err := Schedule(nil, now, "correct", true, 3); err == nil {
		t.Fatal("hinted recall cannot be Good")
	}
	for _, rating := range []int{0, 5} {
		if _, err := Schedule(nil, now, "correct", false, rating); err == nil {
			t.Fatal("invalid rating accepted")
		}
	}
	again, err := Schedule(nil, now, "wrong", false, 1)
	if err != nil || !again.Card.Due.After(now) || again.Card.Due.After(now.Add(time.Hour)) {
		t.Fatalf("Again: %+v %v", again, err)
	}
}

func TestMemoryGrowsAcrossReviewsAndDimensionsRemainIndependent(t *testing.T) {
	now := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	first, err := Schedule(nil, now, "correct", false, 4)
	if err != nil {
		t.Fatal(err)
	}
	next, err := Schedule(&first.Card, first.Card.Due, "correct", false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if next.Card.Reps != 2 || next.Card.Stability <= first.Card.Stability {
		t.Fatalf("memory did not grow: %+v", next.Card)
	}
	other, err := Schedule(nil, now, "wrong", false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if other.Card.Reps != 1 || other.Card.Stability >= next.Card.Stability {
		t.Fatal("independent new ability inherited learned memory")
	}
	preview, err := Preview(&first.Card, first.Card.Due)
	if err != nil || len(preview) != 4 {
		t.Fatalf("preview: %v %v", preview, err)
	}
	if !preview[3].Equal(next.Card.Due) {
		t.Fatal("preview and applied schedule differ")
	}
}
