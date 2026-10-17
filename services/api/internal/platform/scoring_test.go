package platform

import "testing"

func TestReviewedScoring(t *testing.T) {
	c := Content{Text: "fixture", Parts: []Part{{ID: "m", Points: 5, Choices: []Choice{{ID: "A"}, {ID: "B"}, {ID: "C"}}}, {ID: "f", Points: 2}, {ID: "n", Points: 2}, {ID: "s", Points: 4}}}
	r := ScoringRule{[]ScoringPart{{ID: "m", Mode: "choice", Accepted: []string{"A,C"}, SubsetPoints: 2}, {ID: "f", Mode: "exact", Accepted: []string{"color", "colour"}}, {ID: "n", Mode: "rational", Accepted: []string{"1/2"}}, {ID: "s", Mode: "manual"}}}
	for _, test := range []struct {
		answer string
		score  float64
	}{{"A", 6}, {"C,A", 9}, {"A,B", 4}, {"A,A", 4}, {"Z", 4}, {"", 4}} {
		out, err := r.Score(c, map[string]string{"m": test.answer, "f": " Colour ", "n": "0.5"})
		if err != nil || out.Score != test.score || out.Max != 13 || !out.NeedsReview || out.Confidence != "unresolved" {
			t.Fatal(test, out, err)
		}
	}
	r.Parts[0].SubsetPoints = 6
	if r.Validate(c) == nil {
		t.Fatal("excess partial accepted")
	}
	if _, ok := rational("1/0"); ok {
		t.Fatal("division by zero")
	}
	if _, ok := rational("__import__('os')"); ok {
		t.Fatal("expression executed")
	}
}
