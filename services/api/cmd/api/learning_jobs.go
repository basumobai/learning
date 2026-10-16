package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

func (s *server) enqueueLearning(ctx context.Context, kind, key string, payload any) (string, error) {
	raw, _ := json.Marshal(payload)
	id := security.Token(18)
	err := s.db.QueryRowContext(ctx, `INSERT INTO learning_jobs(id,owner_id,kind,dedupe_key,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner_id,kind,dedupe_key) DO UPDATE SET dedupe_key=EXCLUDED.dedupe_key RETURNING id`, id, userID(ctx), kind, key, string(raw)).Scan(&id)
	return id, err
}
func (s *server) learningJob(w http.ResponseWriter, r *http.Request) {
	var kind, status, message string
	var payload, result []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT kind,status,error,payload,result FROM learning_jobs WHERE id=$1 AND owner_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&kind, &status, &message, &payload, &result)
	if err != nil {
		fail(w, 404, "任务不存在")
		return
	}
	if kind == "explanation" {
		var p struct {
			AttemptID string `json:"attempt_id"`
		}
		_ = json.Unmarshal(payload, &p)
		a, e := s.loadAttempt(r.Context(), p.AttemptID)
		if e != nil {
			fail(w, 404, "练习版本已更新")
			return
		}
		if a.Submitted == nil {
			result = nil
		}

	}
	if kind == "outline" || kind == "identify" || kind == "extract" || kind == "ocr" || kind == "word_options" {
		if account(r.Context()).Role != "admin" {
			fail(w, 403, "需要管理员权限")
			return
		}
	}
	respond(w, 200, map[string]any{"id": r.PathValue("id"), "kind": kind, "status": status, "error": message, "result": json.RawMessage(result)})
}
func (s *server) retryLearningJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ModelSelection *learningModelSelection `json:"model_selection"`
		Personal       *bool                   `json:"personal"`
	}
	if decodeLimit(r, &in, 6000) != nil {
		fail(w, 400, "请求格式无效")
		return
	}
	var raw []byte
	var kind string
	if err := s.db.QueryRowContext(r.Context(), `SELECT payload,kind FROM learning_jobs WHERE id=$1 AND owner_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&raw, &kind); err != nil {
		fail(w, 404, "任务不存在")
		return
	}
	if kind == "explanation" && (in.ModelSelection != nil || in.Personal != nil) {
		fail(w, 400, "更换讲解模型请重新请求动态讲解")
		return
	}
	if kind == "grade" || kind == "essay" {
		var p learningJobPayload
		_ = json.Unmarshal(raw, &p)
		if in.ModelSelection != nil {
			if err := s.validateLearningSelection(r.Context(), in.ModelSelection); err != nil {
				fail(w, 403, err.Error())
				return
			}
			p.ModelSelection = in.ModelSelection
		}
		if in.Personal != nil {
			p.Personal = *in.Personal
		}
		task := "grading"
		if kind == "essay" {
			task = "essay_reviewer"
		}
		models, lead, err := s.selectedLearningModels(r.Context(), task, p.Personal, p.ModelSelection)
		if err != nil {
			fail(w, 403, err.Error())
			return
		}
		p.ModelFingerprint = learningModelsKey(models, lead)
		if !p.Personal {
			p.ModelSelection = learningSelectionSnapshot(task, models, lead)
		}
		raw, _ = json.Marshal(p)
	}

	result, err := s.db.ExecContext(r.Context(), `UPDATE learning_jobs SET status='queued',error='',payload=$3,updated_at=now(),retry_at=now(),lease_until=NULL,lease_owner=NULL,pause_requested=false,cancel_requested=false WHERE id=$1 AND owner_id=$2 AND status='failed'`, r.PathValue("id"), userID(r.Context()), string(raw))
	if err != nil {
		s.dbError(w, "retry job", err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 409, "任务不存在或无需重试")
		return
	}
	respond(w, 202, map[string]bool{"queued": true})
}

type learningJobPayload struct {
	PromptRevision   int                     `json:"prompt_revision"`
	ModelSelection   *learningModelSelection `json:"model_selection"`
	ModelFingerprint string                  `json:"model_fingerprint"`
	Reason           string                  `json:"reason"`
	AttemptID        string                  `json:"attempt_id"`
	Personal         bool                    `json:"personal"`
	DocumentID       string                  `json:"document_id"`
	Chapters         []chapterSelection      `json:"chapters"`
	SenseID          int64                   `json:"sense_id"`
	Direction        string                  `json:"direction"`
	Difficulty       string                  `json:"difficulty"`
}

func (s *server) learningPayloadModels(ctx context.Context, task string, p learningJobPayload) ([]aiModel, aiModel, error) {
	models, lead, err := s.selectedLearningModels(ctx, task, p.Personal, p.ModelSelection)
	if err == nil && p.ModelFingerprint != "" && p.ModelFingerprint != learningModelsKey(models, lead) {
		return nil, aiModel{}, fmt.Errorf("模型配置已更新，请重新选择后重试")
	}
	return models, lead, err
}

const timelineContract = `返回结构 {"version":1,"text":"完整原文","spans":[{"id":"s1","start":0,"end":3,"role":"主语","tone":"yellow"}],"steps":[{"id":"step1","title":"找主干","explanation":"解释","span_ids":["s1"],"asset_ids":[]}],"summary":"总结"}。text 必须逐字等于输入source。start/end按Unicode码点计数，左闭右开。tone仅yellow/green/blue/pink/purple。所有span_ids与asset_ids必须存在。数学标注请包含完整的LaTeX公式及其分隔符，不要把一个公式拆到多个标注中。不返回HTML、JS或CSS。`

func sourceText(c platform.Content) string {
	if c.Passage != "" {
		return c.Passage
	}
	return c.Text
}
func (s *server) generateExplanation(ctx context.Context, p learningJobPayload) (any, error) {
	if p.PromptRevision > 0 {
		var current int
		if err := s.db.QueryRowContext(ctx, `SELECT revision FROM learning_ai_tasks WHERE task='explanation'`).Scan(&current); err != nil || current != p.PromptRevision {
			return nil, fmt.Errorf("讲解配置已更新，请重新生成")
		}
	}
	a, err := s.loadAttempt(ctx, p.AttemptID)
	if err != nil {
		return nil, err
	}
	if a.Submitted == nil || (p.Reason != "self_report" && !learningExplanationNeeded(a)) {
		return nil, fmt.Errorf("讲解仅在交卷后答错或主动求助时生成")
	}
	models, _, err := s.learningPayloadModels(ctx, "explanation", p)
	if err != nil {
		return nil, err
	}
	input := map[string]any{"question": a.Question.Content, "reference": a.Question.Solution, "source": sourceText(a.Question.Content), "assets": a.Question.Assets, "subject": a.Question.Subject, "student_answer": a.Answers, "feedback": json.RawMessage(a.Feedback)}
	raw, _ := json.Marshal(input)
	parts := []map[string]any{textPart(string(raw))}
	for _, asset := range a.Question.Assets {
		var image []byte
		if e := s.db.QueryRowContext(ctx, `SELECT image FROM learning_assets WHERE id=$1 AND question_id=$2`, asset["id"], a.Question.ID).Scan(&image); e != nil {
			return nil, e
		}
		parts = append(parts, textPart("题目插图ID："+asset["id"].(string)), imagePart(image))
	}
	text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, "explanation")+timelineContract, parts)
	if err != nil {
		return nil, err
	}
	var timeline platform.Timeline
	if err = platform.Decode(text, &timeline); err != nil {
		return nil, err
	}
	assets := map[string]bool{}
	for _, v := range a.Question.Assets {
		assets[v["id"].(string)] = true
	}
	if err = timeline.Validate(sourceText(a.Question.Content), assets); err != nil {
		return nil, err
	}
	if _, _, err = s.learningPayloadModels(ctx, "explanation", p); err != nil {
		return nil, err
	}
	if p.PromptRevision > 0 {
		var current int
		if err := s.db.QueryRowContext(ctx, `SELECT revision FROM learning_ai_tasks WHERE task='explanation'`).Scan(&current); err != nil || current != p.PromptRevision {
			return nil, fmt.Errorf("讲解配置已更新，请重新生成")
		}
	}
	return timeline, nil
}

type gradePart struct {
	ID      string  `json:"id"`
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
	Mistake string  `json:"mistake"`
}
type gradeResult struct {
	Parts    []gradePart `json:"parts"`
	Feedback string      `json:"feedback"`
}
type essayReview struct {
	Score       float64  `json:"score"`
	Reason      string   `json:"reason"`
	Evidence    []string `json:"evidence"`
	Suggestions []string `json:"suggestions"`
}
type essayFinal struct {
	Score        float64  `json:"score"`
	Reason       string   `json:"reason"`
	Disagreement string   `json:"disagreement"`
	Suggestions  []string `json:"suggestions"`
}

func (s *server) gradeLearning(ctx context.Context, p learningJobPayload, kind string) (any, error) {
	a, err := s.loadAttempt(ctx, p.AttemptID)
	if err != nil || a.Submitted == nil {
		return nil, fmt.Errorf("未交卷")
	}
	input := map[string]any{"question": a.Question.Content, "reference": a.Question.Solution, "student_answer": a.Answers}
	raw, _ := json.Marshal(input)
	feedback := feedbackFor(a, a.Answers)
	if kind == "essay" {
		models, lead, err := s.learningPayloadModels(ctx, "essay_reviewer", p)
		if err != nil || len(models) < 2 {
			return nil, fmt.Errorf("作文至少需要两个评审模型和一个主审")
		}
		reviews := make([]essayReview, len(models))
		errs := make([]error, len(models))
		var wg sync.WaitGroup
		for i, m := range models {
			wg.Add(1)
			go func(i int, m aiModel) {
				defer wg.Done()
				text, e := s.learningCompletion(ctx, m, s.taskPrompt(ctx, "essay_reviewer")+` 返回{"score":0,"reason":"分维度理由","evidence":["学生原句"],"suggestions":["改法"]}。score在0至60之间。`, string(raw))
				if e == nil {
					e = platform.Decode(text, &reviews[i])
					if e == nil && (reviews[i].Score < 0 || reviews[i].Score > 60 || reviews[i].Reason == "" || len(reviews[i].Evidence) == 0 || len(reviews[i].Suggestions) == 0) {
						e = fmt.Errorf("评审内容无效")
					}
					if e == nil {
						for _, v := range reviews[i].Evidence {
							found := false
							for _, body := range a.Answers {
								if v != "" && strings.Contains(body, v) {
									found = true
								}
							}
							if !found {
								e = fmt.Errorf("原文证据不匹配")
							}
						}
					}
				}
				errs[i] = e
			}(i, m)
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return nil, e
			}
		}
		// Reviews can take minutes: recheck grants before starting the separate lead call.
		_, currentLead, e := s.learningPayloadModels(ctx, "essay_reviewer", p)
		if e != nil {
			return nil, e
		}
		lead = currentLead
		input["independent_reviews"] = reviews
		leadRaw, _ := json.Marshal(input)
		text, err := s.learningCompletion(ctx, lead, s.taskPrompt(ctx, "essay_lead")+` 返回{"score":0,"reason":"评判理由","disagreement":"如何处理分歧","suggestions":["改进措施"]}。score为0至60。`, string(leadRaw))
		if err != nil {
			return nil, err
		}
		var final essayFinal
		if err = platform.Decode(text, &final); err != nil {
			return nil, err
		}
		if final.Score < 0 || final.Score > 60 || final.Reason == "" || final.Disagreement == "" || len(final.Suggestions) == 0 {
			return nil, fmt.Errorf("主审无效")
		}
		if _, _, err = s.learningPayloadModels(ctx, "essay_reviewer", p); err != nil {
			return nil, err
		}
		feedback["score"] = final.Score
		feedback["max_score"] = 60
		feedback["independent_reviews"] = reviews
		feedback["essay_review"] = final
	} else {
		models, _, err := s.learningPayloadModels(ctx, "grading", p)
		if err != nil {
			return nil, err
		}
		text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, "grading")+` 返回{"parts":[{"id":"小题编号","score":0,"reason":"得分依据","mistake":"错因或无"}],"feedback":"整体建议"}。每一小题必须出现一次，分数不得超过原题分值。`, string(raw))
		if err != nil {
			return nil, err
		}
		var result gradeResult
		if err = platform.Decode(text, &result); err != nil {
			return nil, err
		}
		if len(result.Parts) != len(a.Question.Content.Parts) || result.Feedback == "" {
			return nil, fmt.Errorf("评分不完整")
		}
		seen := map[string]bool{}
		score := 0.0
		for _, v := range result.Parts {
			matched := false
			for _, part := range a.Question.Content.Parts {
				if v.ID == part.ID && v.Score >= 0 && v.Score <= part.Points && v.Reason != "" && !seen[v.ID] {
					matched = true
					seen[v.ID] = true
					score += v.Score
				}
			}
			if !matched {
				return nil, fmt.Errorf("评分小题或分值无效")
			}
		}
		feedback["score"] = score
		feedback["ai_grading"] = result
		if _, _, err = s.learningPayloadModels(ctx, "grading", p); err != nil {
			return nil, err
		}
	}
	feedback["needs_ai"] = false
	feedback["ai_reference"] = true
	var existing map[string]any
	_ = json.Unmarshal(a.Feedback, &existing)
	for _, k := range []string{"han_count", "training_rule", "timed_out"} {
		if v, ok := existing[k]; ok {
			feedback[k] = v
		}
	}
	fr, _ := json.Marshal(feedback)
	_, err = s.learningExec(ctx, `UPDATE learning_attempts SET feedback=$3 WHERE id=$1 AND user_id=$2 AND question_revision=$4`, a.ID, userID(ctx), string(fr), a.Question.Revision)
	return feedback, err
}
