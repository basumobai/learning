package platform

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
)

const ScoringVersion = "scoring-v1"

// Rules contain reviewed, finite comparisons only. No expression is executed.
type ScoringRule struct {
	Parts []ScoringPart `json:"parts"`
}
type ScoringPart struct {
	ID            string   `json:"id"`
	Mode          string   `json:"mode"` // choice, exact, rational, manual
	Accepted      []string `json:"accepted,omitempty"`
	SubsetPoints  float64  `json:"subset_points,omitempty"`
	PerCorrect    float64  `json:"per_correct,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty"`
}
type PartScore struct {
	ID          string  `json:"id"`
	Correct     bool    `json:"correct"`
	Points      float64 `json:"points"`
	Max         float64 `json:"max_points"`
	NeedsReview bool    `json:"needs_ai"`
	Basis       string  `json:"basis"`
}
type ScoreResult struct {
	Parts       []PartScore `json:"parts"`
	Score       float64     `json:"score"`
	Max         float64     `json:"max_score"`
	NeedsReview bool        `json:"needs_ai"`
	Confidence  string      `json:"confidence"`
	Engine      string      `json:"engine_version"`
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func choiceSet(s string) ([]string, bool) {
	if strings.TrimSpace(s) == "" {
		return nil, true
	}
	seen := map[string]bool{}
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return nil, false
		}
		seen[v] = true
	}
	out := []string{}
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, true
}
func rational(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 120 || s == "" {
		return nil, false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789+-./", r) {
			return nil, false
		}
	}
	return new(big.Rat).SetString(s)
}
func (rule ScoringRule) Validate(c Content) error {
	if len(rule.Parts) != len(c.Parts) {
		return fmt.Errorf("评分规则必须逐一覆盖小题")
	}
	seen := map[string]bool{}
	for _, r := range rule.Parts {
		var p *Part
		for i := range c.Parts {
			if c.Parts[i].ID == r.ID {
				p = &c.Parts[i]
			}
		}
		if p == nil || seen[r.ID] || !finite(p.Points) || p.Points <= 0 || !finite(r.SubsetPoints) || !finite(r.PerCorrect) || r.SubsetPoints < 0 || r.PerCorrect < 0 || r.SubsetPoints > p.Points || r.PerCorrect > p.Points || (r.PerCorrect > 0 && r.SubsetPoints > 0) {
			return fmt.Errorf("小题、分值或少选政策无效")
		}
		seen[r.ID] = true
		if len(r.Accepted) > 30 {
			return fmt.Errorf("等价答案过多")
		}
		if r.Mode == "manual" {
			if len(r.Accepted) > 0 || r.PerCorrect != 0 || r.SubsetPoints != 0 {
				return fmt.Errorf("人工评分不可包含自动规则")
			}
			continue
		}
		if len(r.Accepted) == 0 {
			return fmt.Errorf("自动评分需要审核答案")
		}
		switch r.Mode {
		case "choice":
			if len(p.Choices) == 0 || len(r.Accepted) != 1 {
				return fmt.Errorf("选择题规则无效")
			}
			set, ok := choiceSet(r.Accepted[0])
			if !ok || len(set) == 0 {
				return fmt.Errorf("正确选项无效")
			}
			for _, v := range set {
				found := false
				for _, ch := range p.Choices {
					found = found || v == ch.ID
				}
				if !found {
					return fmt.Errorf("答案不在选项内")
				}
			}
			if len(set) < 2 && (r.SubsetPoints > 0 || r.PerCorrect > 0) {
				return fmt.Errorf("单选不使用少选政策")
			}
		case "exact", "rational":
			if len(p.Choices) > 0 || r.SubsetPoints != 0 || r.PerCorrect != 0 {
				return fmt.Errorf("填空规则无效")
			}
		default:
			return fmt.Errorf("不支持的评分方式")
		}
		for _, v := range r.Accepted {
			if strings.TrimSpace(v) == "" || len([]rune(v)) > 2000 {
				return fmt.Errorf("等价答案无效")
			}
			if r.Mode == "rational" {
				if _, ok := rational(v); !ok {
					return fmt.Errorf("仅支持有理数或分数，不执行表达式")
				}
			}
		}
	}
	return nil
}
func (rule ScoringRule) Score(c Content, answers map[string]string) (ScoreResult, error) {
	result := ScoreResult{Parts: []PartScore{}, Confidence: "verified_rule", Engine: ScoringVersion}
	if err := rule.Validate(c); err != nil {
		return result, err
	}
	for _, p := range c.Parts {
		var r ScoringPart
		for _, v := range rule.Parts {
			if v.ID == p.ID {
				r = v
			}
		}
		out := PartScore{ID: p.ID, Max: p.Points, Basis: "approved_rule"}
		answer := answers[p.ID]
		switch r.Mode {
		case "manual":
			out.NeedsReview = true
			out.Basis = "pending_human_review"
			result.NeedsReview = true
		case "choice":
			expected, _ := choiceSet(r.Accepted[0])
			selected, valid := choiceSet(answer)
			correct := map[string]bool{}
			for _, v := range expected {
				correct[v] = true
			}
			subset := valid && len(selected) > 0
			for _, v := range selected {
				subset = subset && correct[v]
			}
			if subset && len(selected) == len(expected) {
				out.Points = p.Points
				out.Correct = true
			} else if subset {
				out.Points = math.Min(p.Points, math.Max(r.SubsetPoints, r.PerCorrect*float64(len(selected))))
			}
		case "exact":
			normalize := func(v string) string {
				if r.CaseSensitive {
					return strings.Join(strings.Fields(v), " ")
				}
				return NormalizeAnswer(v)
			}
			for _, v := range r.Accepted {
				if normalize(answer) == normalize(v) {
					out.Correct = true
				}
			}
		case "rational":
			if a, ok := rational(answer); ok {
				for _, v := range r.Accepted {
					b, _ := rational(v)
					if a.Cmp(b) == 0 {
						out.Correct = true
					}
				}
			}
		}
		if out.Correct {
			out.Points = p.Points
		}
		result.Parts = append(result.Parts, out)
		result.Score += out.Points
		result.Max += p.Points
	}
	if result.NeedsReview {
		result.Confidence = "unresolved"
	}
	return result, nil
}
