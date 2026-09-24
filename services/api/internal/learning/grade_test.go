package learning

import (
	"testing"
	"time"
)

func TestGrade(t *testing.T) {
	cases := []struct{ kind, answer, want string }{
		{"spelling", " Necessary ", "correct"},
		{"spelling", "neccessary", "wrong"},
		{"recognition", "必要的", "correct"},
		{"recognition", "后果", "wrong"},
		{"meaning_recall", "必需的。", "correct"},
		{"meaning_recall", "很重要", "pending"},
		{"meaning_recall", "  ", "wrong"},
	}
	for _, c := range cases {
		got := Grade(c.kind, c.answer, "necessary", "必要的", []string{"必要的", "必需的"})
		if got != c.want { t.Errorf("%s %q: got %s want %s", c.kind, c.answer, got, c.want) }
	}
}

func TestNextReview(t *testing.T) {
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	if got := NextReview(now, true, false, 0); !got.Equal(now.Add(24*time.Hour)) { t.Fatal(got) }
	if got := NextReview(now, true, true, 4); !got.Equal(now.Add(10*time.Minute)) { t.Fatal(got) }
	if got := NextReview(now, false, false, 4); !got.Equal(now.Add(10*time.Minute)) { t.Fatal(got) }
}
