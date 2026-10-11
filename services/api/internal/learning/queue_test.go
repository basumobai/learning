package learning

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestDueQueuePrioritizesReviewsAndBuriesSiblings(t *testing.T) {
	now := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	due, early := now.Add(-time.Hour), now.Add(time.Hour)
	newLimit := 1
	opts := Options{NewLimit: &newLimit, Limit: 10}
	if err := opts.Validate(); err != nil {
		t.Fatal(err)
	}
	candidates := []Candidate{{SenseID: 1, Kind: "spelling", Due: &due}, {SenseID: 1, Kind: "recognition"}, {SenseID: 2, Kind: "spelling", Due: &early}, {SenseID: 3, Kind: "spelling"}, {SenseID: 4, Kind: "spelling"}}
	random := rand.New(rand.NewPCG(7, 8))
	queue := BuildQueue(candidates, opts, now, random.Shuffle)
	if len(queue) != 2 || queue[0].SenseID != 1 || queue[0].Kind != "spelling" {
		t.Fatalf("unexpected queue: %+v", queue)
	}
	if queue[1].Due != nil || queue[1].SenseID == 2 {
		t.Fatal("non-due card selected")
	}
	*opts.NewLimit = 0
	if got := BuildQueue(candidates, opts, now, random.Shuffle); len(got) != 1 || got[0].SenseID != 1 {
		t.Fatalf("review only: %+v", got)
	}
}

func TestPracticeCanReviewEarlyWithOneAbilityPerSense(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)
	zero := 0
	opts := Options{Mode: "practice", Limit: 5, NewLimit: &zero}
	if err := opts.Validate(); err != nil {
		t.Fatal(err)
	}
	queue := BuildQueue([]Candidate{{SenseID: 1, Kind: "spelling", Due: &future}, {SenseID: 1, Kind: "recognition"}, {SenseID: 2, Kind: "spelling"}}, opts, now, rand.New(rand.NewPCG(1, 2)).Shuffle)
	if len(queue) != 2 || queue[0].SenseID == queue[1].SenseID {
		t.Fatalf("practice siblings: %+v", queue)
	}
}

func TestOptionsValidationAndChoiceUniqueness(t *testing.T) {
	negative := -1
	for _, opts := range []Options{{Mode: "random"}, {Limit: 101}, {NewLimit: &negative}, {Types: []string{"spelling", "spelling"}}, {Types: []string{"context"}}} {
		if opts.Validate() == nil {
			t.Fatalf("accepted: %+v", opts)
		}
	}
	zero := 0
	opts := Options{NewLimit: &zero}
	if opts.Validate() != nil || *opts.NewLimit != 0 {
		t.Fatal("explicit zero must be preserved")
	}
	choices := Choices("适应", []string{"适应", "适应。", "采用", "维持", "必要的"})
	if len(choices) != 4 {
		t.Fatalf("choices: %+v", choices)
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		n := Normalize(choice)
		if seen[n] {
			t.Fatal("duplicate distractor")
		}
		seen[n] = true
	}
	if !seen[Normalize("适应")] {
		t.Fatal("correct answer missing")
	}
}
