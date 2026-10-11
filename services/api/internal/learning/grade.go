package learning

import "strings"

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
	if normalized == "" {
		return "wrong"
	}
	switch kind {
	case "spelling", "listening_spelling":
		if strings.EqualFold(strings.TrimSpace(answer), strings.TrimSpace(lemma)) {
			return "correct"
		}
		return "wrong"
	case "recognition":
		if normalized == Normalize(meaning) {
			return "correct"
		}
		return "wrong"
	case "meaning_recall":
		if normalized == Normalize(meaning) {
			return "correct"
		}
		for _, alias := range aliases {
			if normalized == Normalize(alias) {
				return "correct"
			}
		}
		return "pending"
	}
	return "pending"
}
