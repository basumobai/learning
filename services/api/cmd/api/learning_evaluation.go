package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *server) evaluationRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/learning/evaluations", s.evaluationList)
	m.HandleFunc("POST /api/v1/admin/learning/evaluations/cases", s.createEvaluationCase)
	m.HandleFunc("POST /api/v1/admin/learning/evaluations", s.queueEvaluation)
	m.HandleFunc("POST /api/v1/admin/learning/evaluations/{id}/review", s.reviewEvaluation)
}
func (s *server) evaluationList(w http.ResponseWriter, r *http.Request) {
	var cases, runs json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM learning_evaluation_cases t`).Scan(&cases)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT r.*,to_jsonb(e) AS result,to_jsonb(v) AS human_review FROM learning_evaluation_runs r LEFT JOIN learning_evaluation_results e ON e.run_id=r.id LEFT JOIN learning_evaluation_reviews v ON v.run_id=r.id ORDER BY r.created_at DESC LIMIT 100)t`).Scan(&runs)
	}
	if err != nil {
		s.dbError(w, "evaluation list", err)
		return
	}
	respond(w, 200, map[string]any{"cases": cases, "runs": runs, "note": "自动术语检查不等于教学正确率。调用耗时是端到端时间；非流式接口没有TTFT证据，供应商费用需另对账。"})
}
func (s *server) createEvaluationCase(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subject  string   `json:"subject_id"`
		Task     string   `json:"task"`
		Input    string   `json:"input"`
		Expected []string `json:"expected_terms"`
		Rights   string   `json:"rights_note"`
	}
	if decodeLimit(r, &in, 16000) != nil || len(platform.Types(in.Subject)) == 0 || len([]rune(in.Input)) < 5 || len([]rune(in.Input)) > 8000 || len(in.Expected) < 1 || len(in.Expected) > 30 || len(strings.TrimSpace(in.Rights)) < 10 {
		fail(w, 400, "评测需要有权输入、核验词项和来源说明")
		return
	}
	if _, ok := learningDefaults[in.Task]; !ok {
		fail(w, 400, "任务不存在")
		return
	}
	for _, term := range in.Expected {
		if strings.TrimSpace(term) == "" || len([]rune(term)) > 300 {
			fail(w, 400, "核验词项无效")
			return
		}
	}
	id := security.Token(18)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO learning_evaluation_cases(id,subject_id,task,input,expected_terms,rights_note,reviewer_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.Subject, in.Task, in.Input, nullableJSON(in.Expected), in.Rights, userID(r.Context()))
	if err != nil {
		s.dbError(w, "save evaluation case", err)
		return
	}
	respond(w, 201, map[string]string{"id": id})
}
func (s *server) queueEvaluation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Case    string `json:"case_id"`
		Model   int64  `json:"model_id"`
		Prompt  string `json:"prompt"`
		Request string `json:"request_id"`
	}
	if decodeLimit(r, &in, 18000) != nil || !requestIDPattern.MatchString(in.Request) || len(strings.TrimSpace(in.Prompt)) < 5 || len([]rune(in.Prompt)) > 12000 {
		fail(w, 400, "评测配置无效")
		return
	}
	model, err := s.learningModel(r.Context(), in.Model)
	if err != nil {
		fail(w, 403, "评测模型不可用")
		return
	}
	var exists bool
	if s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_evaluation_cases WHERE id=$1)`, in.Case).Scan(&exists) != nil || !exists {
		fail(w, 404, "评测样例不存在")
		return
	}
	digest := payloadDigest(in)
	id := security.Token(18)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "evaluation queue", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_evaluation_runs(id,case_id,model_id,model_version,prompt,prompt_hash,request_id,reviewer_id,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(reviewer_id,request_id) DO NOTHING`, id, in.Case, in.Model, model.version, in.Prompt, payloadDigest(in.Prompt), in.Request, userID(r.Context()), digest)
	var prior string
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT id,digest FROM learning_evaluation_runs WHERE reviewer_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &prior)
	}
	if err != nil {
		s.dbError(w, "reserve evaluation", err)
		return
	}
	if prior != digest {
		fail(w, 409, "评测请求冲突")
		return
	}
	job := security.Token(18)
	payload := learningJobPayload{EvaluationID: id}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_jobs(id,owner_id,kind,dedupe_key,payload) VALUES($1,$2,'model_evaluation',$3,$4) ON CONFLICT(owner_id,kind,dedupe_key) DO NOTHING`, job, userID(r.Context()), id, nullableJSON(payload))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "queue evaluation job", err)
		return
	}
	respond(w, 202, map[string]string{"id": id})
}
func (s *server) runEvaluation(ctx context.Context, id string) (any, error) {
	var input, prompt, version string
	var modelID int64
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT c.input,c.expected_terms,r.prompt,r.model_id,r.model_version FROM learning_evaluation_runs r JOIN learning_evaluation_cases c ON c.id=r.case_id WHERE r.id=$1 AND r.reviewer_id=$2`, id, userID(ctx)).Scan(&input, &raw, &prompt, &modelID, &version)
	if err != nil {
		return nil, err
	}
	model, err := s.learningModel(ctx, modelID)
	if err != nil || model.version != version {
		return nil, fmt.Errorf("模型配置已变化，请创建新评测")
	}
	model.budgetTask = "model_evaluation"
	start := time.Now()
	answer, err := s.learningCompletion(ctx, model, prompt, input)
	if err != nil {
		return nil, err
	}
	var expected []string
	if json.Unmarshal(raw, &expected) != nil {
		return nil, fmt.Errorf("invalid evaluation terms")
	}
	checks := map[string]bool{}
	all := true
	for _, term := range expected {
		ok := strings.Contains(strings.ToLower(answer), strings.ToLower(term))
		checks[term] = ok
		all = all && ok
	}
	_, err = s.learningExec(ctx, `INSERT INTO learning_evaluation_results(run_id,answer,automatic_checks,elapsed_ms,execution) VALUES($1,$2,$3,$4,'provider_request') ON CONFLICT DO NOTHING`, id, answer, nullableJSON(map[string]any{"all_expected_terms": all, "terms": checks}), min(time.Since(start).Milliseconds(), 2147483647))
	return map[string]any{"run_id": id, "automatic_checks": checks, "requires_human_review": true}, err
}
func (s *server) reviewEvaluation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if decodeLimit(r, &in, 6000) != nil || (in.Decision != "approved" && in.Decision != "rejected") || len(strings.TrimSpace(in.Note)) < 10 || len([]rune(in.Note)) > 2000 {
		fail(w, 400, "需要明确人工核验结论和说明")
		return
	}
	var automatic bool
	err := s.db.QueryRowContext(r.Context(), `SELECT (automatic_checks->>'all_expected_terms')::boolean FROM learning_evaluation_results WHERE run_id=$1`, r.PathValue("id")).Scan(&automatic)
	if err != nil {
		fail(w, 409, "评测尚未完成")
		return
	}
	if in.Decision == "approved" && !automatic {
		fail(w, 409, "基础核验词项未通过，请先修正模型或样例后重新评测")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_evaluation_reviews(run_id,decision,note,reviewer_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, r.PathValue("id"), in.Decision, in.Note, userID(r.Context()))
	if err != nil {
		s.dbError(w, "evaluation review", err)
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (s *server) requireModelEvaluation(ctx context.Context, task, prompt string, ids []int64) error {
	if !v3Enabled("QUALITY_GATE") {
		return nil
	}
	for _, id := range ids {
		model, err := s.learningModel(ctx, id)
		if err != nil {
			return err
		}
		var approved bool
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_evaluation_runs r JOIN learning_evaluation_cases c ON c.id=r.case_id JOIN learning_evaluation_reviews v ON v.run_id=r.id WHERE r.model_id=$1 AND r.model_version=$2 AND r.prompt_hash=$3 AND c.task=$4 AND v.decision='approved')`, id, model.version, payloadDigest(prompt), task).Scan(&approved)
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("模型%d在当前配置/提示词下缺少该任务人工通过的评测", id)
		}
	}
	return nil
}
