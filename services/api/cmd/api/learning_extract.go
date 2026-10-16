package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"fmt"
)

// Three separate tasks keep transcription, reference extraction and matching reviewable.
func (s *server) extractChapterStages(ctx context.Context, qparts, aparts []map[string]any) ([]extractedQuestion, error) {
	run := func(task, contract string, parts []map[string]any, out any) error {
		models, _, err := s.taskModels(ctx, task, false)
		if err != nil {
			return fmt.Errorf("%s：%w", learningDefaults[task].Name, err)
		}
		text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, task)+contract, parts)
		if err != nil {
			return err
		}
		if err = platform.Decode(text, out); err != nil {
			return fmt.Errorf("%s：%w", learningDefaults[task].Name, err)
		}
		return nil
	}
	var qs struct {
		Questions []extractedQuestion `json:"questions"`
	}
	err := run("extract_questions", ` 返回{"questions":[{"source_key":"原题号","kind":"允许题型ID","content":{"text":"题干说明","passage":"共用材料","parts":[{"id":"1","text":"小题","choices":[{"id":"A","text":"选项"}],"points":2}]},"question_pages":[1],"figures":[{"page":1,"bbox":[0.1,0.1,0.8,0.8],"alt":"插图说明"}]}]}。只提取题目，不填写solution；图bbox为归一化[x0,y0,x1,y1]。`, qparts, &qs)
	if err != nil {
		return nil, err
	}
	if len(qs.Questions) < 1 || len(qs.Questions) > 60 {
		return nil, fmt.Errorf("题目提取数量无效")
	}
	type reference struct {
		SourceKey string            `json:"source_key"`
		Solution  platform.Solution `json:"solution"`
		Pages     []int             `json:"answer_pages"`
		Quote     string            `json:"answer_quote"`
	}
	var refs struct {
		Answers []reference `json:"answers"`
	}
	err = run("extract_answers", ` 返回{"answers":[{"source_key":"答案原题号","solution":{"answers":{"1":"A"},"explanation":"参考解析","rubric":"评分要点"},"answer_pages":[1],"answer_quote":"逐字原始答案依据"}]}。选择题答案用选项ID，多选用逗号分隔。禁止虚构缺失答案。`, aparts, &refs)
	if err != nil {
		return nil, err
	}
	if len(refs.Answers) < 1 || len(refs.Answers) > 100 {
		return nil, fmt.Errorf("答案提取数量无效")
	}
	qmap := map[string]int{}
	amap := map[string]reference{}
	for i, q := range qs.Questions {
		if q.SourceKey == "" || len(q.SourceKey) > 100 {
			return nil, fmt.Errorf("题号缺失或过长")
		}
		if _, ok := qmap[q.SourceKey]; ok {
			return nil, fmt.Errorf("题目原题号重复，需核对小节范围")
		}
		qmap[q.SourceKey] = i
	}
	for _, a := range refs.Answers {
		if a.SourceKey == "" {
			return nil, fmt.Errorf("答案题号缺失")
		}
		if _, ok := amap[a.SourceKey]; ok {
			return nil, fmt.Errorf("答案原题号重复，需核对小节范围")
		}
		amap[a.SourceKey] = a
	}
	raw, _ := json.Marshal(map[string]any{"questions": qs.Questions, "answers": refs.Answers})
	var paired struct {
		Matches []struct {
			Question string `json:"question_key"`
			Answer   string `json:"answer_key"`
		} `json:"matches"`
	}
	err = run("match_answers", ` 返回{"matches":[{"question_key":"题目原题号","answer_key":"答案原题号"}]}。每个题目必须有唯一可靠配对，否则不生成该项。`, []map[string]any{textPart(string(raw))}, &paired)
	if err != nil {
		return nil, err
	}
	if len(paired.Matches) != len(qs.Questions) {
		return nil, fmt.Errorf("题目与答案未完整配对；请核对PDF页码和答案缺失情况")
	}
	seenQ, seenA := map[string]bool{}, map[string]bool{}
	for _, m := range paired.Matches {
		qi, ok := qmap[m.Question]
		a, aok := amap[m.Answer]
		if !ok || !aok || seenQ[m.Question] || seenA[m.Answer] {
			return nil, fmt.Errorf("配对包含未知编号或重复引用")
		}
		seenQ[m.Question] = true
		seenA[m.Answer] = true
		qs.Questions[qi].Solution = a.Solution
		qs.Questions[qi].AnswerPages = a.Pages
		qs.Questions[qi].AnswerQuote = a.Quote
		qs.Questions[qi].NeedsReview = true
	}
	return qs.Questions, nil
}
