package learning

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

type Options struct {
	Mode     string   `json:"mode"`
	Types    []string `json:"types"`
	Limit    int      `json:"limit"`
	NewLimit *int     `json:"new_limit"`
}

func (o *Options) Validate() error {
	if o.Mode == "" {
		o.Mode = "scheduled"
	}
	if o.Mode != "scheduled" && o.Mode != "practice" {
		return fmt.Errorf("invalid mode")
	}
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 {
		return fmt.Errorf("limit must be 1..100")
	}
	if o.NewLimit == nil {
		n := 10
		o.NewLimit = &n
	}
	if *o.NewLimit < 0 || *o.NewLimit > 50 {
		return fmt.Errorf("new_limit must be 0..50")
	}
	if len(o.Types) == 0 {
		o.Types = []string{"recognition", "meaning_recall", "spelling"}
	}
	seen := make(map[string]bool)
	for _, kind := range o.Types {
		if (kind != "recognition" && kind != "meaning_recall" && kind != "spelling") || seen[kind] {
			return fmt.Errorf("invalid types")
		}
		seen[kind] = true
	}
	return nil
}

type Candidate struct {
	SenseID int64
	Kind    string
	Lemma   string
	Meaning string
	POS     string
	Due     *time.Time
}

// BuildQueue shuffles before choosing one ability per word sense. Reviews
// precede unseen abilities; non-due cards are never scheduled early. Keeping
// siblings out of a session avoids showing a spelling answer beforehand.
func BuildQueue(candidates []Candidate, options Options, now time.Time, shuffle func(int, func(int, int))) []Candidate {
	shuffled := append([]Candidate(nil), candidates...)
	shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	if options.Mode == "scheduled" {
		sort.SliceStable(shuffled, func(i, j int) bool {
			if shuffled[i].Due == nil {
				return false
			}
			if shuffled[j].Due == nil {
				return true
			}
			return shuffled[i].Due.Before(*shuffled[j].Due)
		})
	}
	selected := []Candidate{}
	seen := make(map[int64]bool)
	newCount := 0
	for _, item := range shuffled {
		if len(selected) >= options.Limit {
			break
		}
		if seen[item.SenseID] {
			continue
		}
		if options.Mode == "scheduled" {
			if item.Due != nil && item.Due.After(now) {
				continue
			}
			if item.Due == nil {
				if newCount >= *options.NewLimit {
					continue
				}
				newCount++
			}
		}
		selected = append(selected, item)
		seen[item.SenseID] = true
	}
	return selected
}

func Choices(meaning string, pool []string) []string {
	choices := []string{meaning}
	seen := map[string]bool{Normalize(meaning): true}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	for _, option := range pool {
		if !seen[Normalize(option)] {
			choices = append(choices, option)
			seen[Normalize(option)] = true
		}
		if len(choices) == 4 {
			break
		}
	}
	rand.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
	return choices
}
