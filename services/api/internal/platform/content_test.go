package platform

import "testing"

func TestTimelineUnicodeAndReferences(t *testing.T) {
	v := Timeline{Version: 1, Text: "我😀 read.", Spans: []Span{{"s", 1, 2, "符号", "yellow"}}, Steps: []Step{{"one", "观察", "从这个字符开始", []string{"s"}, nil}}, Summary: "总结"}
	if err := v.Validate(v.Text, nil); err != nil {
		t.Fatal(err)
	}
	v.Spans[0].End = 100
	if v.Validate(v.Text, nil) == nil {
		t.Fatal("out-of-range span")
	}
	v.Spans[0].End = 2
	v.Steps[0].AssetIDs = []string{"other-image"}
	if v.Validate(v.Text, nil) == nil {
		t.Fatal("unknown image")
	}
}
func TestWordDifficultyContract(t *testing.T) {
	c := []Choice{{"A", "适应", "v.", "pair"}, {"B", "适合", "v.", "pair"}, {"C", "后果", "n.", "other1"}, {"D", "必要的", "adj.", "other2"}}
	if ValidateWord("hard", "A", c) != nil {
		t.Fatal("valid hard rejected")
	}
	if ValidateWord("hard", "C", c) == nil {
		t.Fatal("correct answer outside pair")
	}
	if ValidateWord("normal", "A", c) == nil {
		t.Fatal("normal similar pair")
	}
	for i := range c {
		c[i].Group = "same"
	}
	if ValidateWord("despair", "A", c) != nil {
		t.Fatal("valid despair rejected")
	}
	c[3].POS = ""
	if ValidateWord("despair", "A", c) == nil {
		t.Fatal("missing POS")
	}
	if n, _ := WordRules("normal"); n != 6000 {
		t.Fatal(n)
	}
	if n, _ := WordRules("hard"); n != 3000 {
		t.Fatal(n)
	}
}
func TestWritingAndQuestionValidation(t *testing.T) {
	if HanCount("中文，123 abc 😀汉字！") != 4 {
		t.Fatal("wrong Han count")
	}
	c := Content{Text: "选择答案", Parts: []Part{{ID: "1", Points: 2, Choices: []Choice{{ID: "A", Text: "一"}, {ID: "B", Text: "二"}}}}}
	s := Solution{Answers: map[string]string{"1": "A"}, Explanation: "依据"}
	if ValidateQuestion("biology", "single_choice", c, s) != nil {
		t.Fatal("valid rejected")
	}
	s.Answers["1"] = "C"
	if ValidateQuestion("biology", "single_choice", c, s) == nil {
		t.Fatal("unknown answer")
	}
	if ValidType("english", "grammar_choice") {
		t.Fatal("unsupported Guangdong type")
	}
}
