package learning

import (
	"testing"
)

func TestGrade(t *testing.T) {
	cases := []struct{ kind, answer, want string }{
		{"spelling", " Necessary ", "correct"},
		{"spelling", "neccessary", "wrong"},
		{"spelling", "n e c e s s a r y", "wrong"},
		{"meaning_recall", "必要的", "correct"},
		{"recognition", "必要的", "correct"},
		{"recognition", "后果", "wrong"},
		{"meaning_recall", "必需的。", "correct"},
		{"meaning_recall", "很重要", "pending"},
		{"meaning_recall", "  ", "wrong"},
	}
	for _, c := range cases {
		got := Grade(c.kind, c.answer, "necessary", "必要的", []string{"必要的", "必需的"})
		if got != c.want {
			t.Errorf("%s %q: got %s want %s", c.kind, c.answer, got, c.want)
		}
	}
}
