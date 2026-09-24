package learning

import (
	"strings"
	"time"
)

// Normalize removes only harmless input differences. It intentionally does not
// guess semantic equivalence; uncertain Chinese answers remain pending.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '　', '。', '.', '！', '!', '；', ';':
			return -1
		default:
			return r
		}
	}, s)
	return s
}

func Grade(kind, answer, lemma, meaning string, aliases []string) string {
	normalized := Normalize(answer)
	if normalized == "" { return "wrong" }
	switch kind {
	case "spelling":
		if normalized == Normalize(lemma) { return "correct" }
		return "wrong"
	case "recognition":
		if normalized == Normalize(meaning) { return "correct" }
		return "wrong"
	case "meaning_recall":
		for _, alias := range aliases {
			if normalized == Normalize(alias) { return "correct" }
		}
		return "pending"
	}
	return "pending"
}

// Temporary first-phase schedule, deliberately isolated for FSRS replacement.
func NextReview(now time.Time, correct, usedHint bool, previousAttempts int) time.Time {
	if !correct || usedHint { return now.Add(10 * time.Minute) }
	if previousAttempts == 0 { return now.Add(24 * time.Hour) }
	return now.Add(72 * time.Hour)
}
