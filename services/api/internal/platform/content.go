package platform

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type Type struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Objective bool   `json:"objective"`
}

func Types(subject string) []Type {
	switch subject {
	case "chinese":
		return []Type{{"modern_reading", "现代文阅读", false}, {"classical_reading", "文言文阅读", false}, {"poetry", "古代诗歌阅读", false}, {"memorization", "名篇名句默写", false}, {"language_use", "语言文字运用", false}, {"essay", "作文训练", false}}
	case "english":
		return []Type{{"reading", "阅读理解", true}, {"seven_five", "七选五", true}, {"cloze", "完形填空", true}, {"grammar_fill", "语法填空", false}, {"practical_writing", "应用文写作", false}, {"continuation", "读后续写", false}, {"speaking", "英语听说", false}}
	case "math":
		return []Type{{"single_choice", "单项选择", true}, {"multiple_choice", "多项选择", true}, {"fill", "填空题", false}, {"solution", "解答题", false}}
	case "physics":
		return []Type{{"single_choice", "单项选择", true}, {"multiple_choice", "多项选择", true}, {"experiment", "实验题", false}, {"calculation", "计算题", false}}
	case "chemistry", "biology":
		return []Type{{"single_choice", "选择题", true}, {"non_choice", "非选择题", false}}
	}
	return nil
}
func ValidType(subject, kind string) bool {
	for _, t := range Types(subject) {
		if t.ID == kind {
			return true
		}
	}
	return false
}

type Choice struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	POS   string `json:"pos,omitempty"`
	Group string `json:"similar_group,omitempty"`
}
type Part struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Choices []Choice `json:"choices,omitempty"`
	Points  float64  `json:"points"`
}
type Content struct {
	Text    string `json:"text"`
	Passage string `json:"passage,omitempty"`
	Parts   []Part `json:"parts"`
}
type Solution struct {
	Answers     map[string]string `json:"answers"`
	Explanation string            `json:"explanation"`
	Rubric      string            `json:"rubric"`
}

func ValidateQuestion(subject, kind string, c Content, s Solution) error {
	if !ValidType(subject, kind) || strings.TrimSpace(c.Text) == "" || len([]rune(c.Text+c.Passage)) > 18000 || len(c.Parts) < 1 || len(c.Parts) > 30 || len(s.Answers) != len(c.Parts) || strings.TrimSpace(s.Explanation) == "" {
		return fmt.Errorf("题目、题型、答案或解析不完整")
	}
	total := len([]rune(c.Text + c.Passage))
	solutionLength := len([]rune(s.Explanation + s.Rubric))
	for _, answer := range s.Answers {
		solutionLength += len([]rune(answer))
	}
	if solutionLength > 18000 {
		return fmt.Errorf("参考答案与解析过长")
	}
	seen := map[string]bool{}
	for _, p := range c.Parts {
		total += len([]rune(p.Text))
		for _, choice := range p.Choices {
			total += len([]rune(choice.Text))
		}
		if total > 32000 {
			return fmt.Errorf("题目组过长，请按材料分组")
		}
		if p.ID == "" || len(p.ID) > 40 || seen[p.ID] || p.Points <= 0 || p.Points > 150 || strings.TrimSpace(s.Answers[p.ID]) == "" || len(p.Choices) > 7 {
			return fmt.Errorf("小题编号、分值或答案无效")
		}
		seen[p.ID] = true
		choices := map[string]bool{}
		texts := map[string]bool{}
		for _, v := range p.Choices {
			if v.ID == "" || choices[v.ID] || texts[strings.TrimSpace(v.Text)] || strings.TrimSpace(v.Text) == "" {
				return fmt.Errorf("选项重复或为空")
			}
			choices[v.ID] = true
			texts[strings.TrimSpace(v.Text)] = true
		}
		if len(p.Choices) > 0 {
			for _, a := range strings.Split(s.Answers[p.ID], ",") {
				if !choices[strings.TrimSpace(a)] {
					return fmt.Errorf("参考答案不在选项中")
				}
			}
		}
	}
	return nil
}
func HanCount(text string) int {
	n := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}
func NormalizeAnswer(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}
func ChoiceAnswer(s string) string {
	set := map[string]bool{}
	for _, a := range strings.Split(s, ",") {
		set[strings.TrimSpace(a)] = true
	}
	result := []string{}
	for r := 'A'; r <= 'G'; r++ {
		if set[string(r)] {
			result = append(result, string(r))
			delete(set, string(r))
		}
	}
	if len(set) > 0 {
		return "INVALID"
	}
	return strings.Join(result, ",")
}

type Span struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Role  string `json:"role"`
	Tone  string `json:"tone"`
}
type Step struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	SpanIDs     []string `json:"span_ids"`
	AssetIDs    []string `json:"asset_ids"`
}
type Timeline struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
	Spans   []Span `json:"spans"`
	Steps   []Step `json:"steps"`
	Summary string `json:"summary"`
}

func (t Timeline) Validate(source string, assets map[string]bool) error {
	if t.Version != 1 || t.Text != source || len(t.Spans) > 80 || len(t.Steps) < 1 || len(t.Steps) > 20 || strings.TrimSpace(t.Summary) == "" {
		return fmt.Errorf("动态讲解结构无效")
	}
	ids := map[string]bool{}
	tones := map[string]bool{"yellow": true, "green": true, "blue": true, "pink": true, "purple": true}
	for _, p := range t.Spans {
		if p.ID == "" || ids[p.ID] || p.Start < 0 || p.End <= p.Start || p.End > len([]rune(t.Text)) || p.Role == "" || !tones[p.Tone] {
			return fmt.Errorf("讲解标注范围无效")
		}
		ids[p.ID] = true
	}
	steps := map[string]bool{}
	for _, p := range t.Steps {
		if p.ID == "" || steps[p.ID] || p.Title == "" || p.Explanation == "" {
			return fmt.Errorf("讲解步骤无效")
		}
		steps[p.ID] = true
		for _, id := range p.SpanIDs {
			if !ids[id] {
				return fmt.Errorf("未知标注")
			}
		}
		for _, id := range p.AssetIDs {
			if !assets[id] {
				return fmt.Errorf("未知插图")
			}
		}
	}
	return nil
}
func (t Timeline) Step(id string) (Step, bool) {
	for _, v := range t.Steps {
		if v.ID == id {
			return v, true
		}
	}
	return Step{}, false
}
func Decode(raw string, out any) error {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) < 3 {
			return fmt.Errorf("invalid JSON")
		}
		raw = strings.Join(lines[1:len(lines)-1], "\n")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("extra or invalid JSON")
	}
	return nil
}
func WordRules(difficulty string) (int, error) {
	switch difficulty {
	case "normal":
		return 6000, nil
	case "hard", "despair":
		return 3000, nil
	}
	return 0, fmt.Errorf("难度无效")
}
func ValidateWord(difficulty, correct string, choices []Choice) error {
	if _, err := WordRules(difficulty); err != nil {
		return err
	}
	if len(choices) != 4 {
		return fmt.Errorf("必须有四个选项")
	}
	ids, texts, groups := map[string]bool{}, map[string]bool{}, map[string]int{}
	cg := ""
	for _, c := range choices {
		key := strings.ToLower(strings.TrimSpace(c.Text))
		if c.ID == "" || ids[c.ID] || key == "" || texts[key] {
			return fmt.Errorf("选项为空或重复")
		}
		ids[c.ID] = true
		texts[key] = true
		if c.Group != "" {
			groups[c.Group]++
		}
		if c.ID == correct {
			cg = c.Group
		}
		if difficulty == "despair" && strings.TrimSpace(c.POS) == "" {
			return fmt.Errorf("绝望模式每个选项必须带词性")
		}
	}
	if !ids[correct] {
		return fmt.Errorf("正确选项不存在")
	}
	if difficulty == "hard" {
		if cg == "" || groups[cg] != 2 || len(groups) != 3 {
			return fmt.Errorf("困难模式必须有一对包含正确答案的近似选项，另两个明显不同")
		}
	}
	if difficulty == "despair" && (cg == "" || groups[cg] != 4) {
		return fmt.Errorf("绝望模式四个选项必须属于同一近似组")
	}
	if difficulty == "normal" {
		for _, n := range groups {
			if n > 1 {
				return fmt.Errorf("普通模式选项应有明显差异")
			}
		}
	}
	return nil
}
