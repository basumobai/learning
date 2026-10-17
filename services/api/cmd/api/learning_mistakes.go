package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"strings"
)

type mistakeType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func mistakeTypes(subject string) []mistakeType {
	common := []mistakeType{{"input_error", "输入失误 / 手滑"}, {"time", "审题与时间"}, {"condition", "条件提取"}}
	var specific []mistakeType
	switch subject {
	case "english":
		specific = []mistakeType{{"word_meaning", "词义 / 熟词生义"}, {"collocation", "词组搭配"}, {"sentence_core", "句子主干"}, {"clause", "从句修饰"}, {"reference", "指代"}, {"contrast", "转折逻辑"}, {"inference", "语篇推断"}, {"distractor", "干扰项"}, {"not_understood", "没听懂"}, {"heard_forgot", "听懂但遗忘"}}
	case "chinese":
		specific = []mistakeType{{"classical_word", "文言实虚词"}, {"sentence_inference", "语句推断"}, {"reading_evidence", "阅读证据与要点"}, {"essay_structure", "作文结构"}, {"essay_intent", "作文立意依据"}, {"memorization", "名句记忆"}}
	case "math":
		specific = []mistakeType{{"concept", "定义与条件"}, {"model", "数学建模"}, {"formula", "公式适用条件"}, {"calculation", "计算"}, {"proof_gap", "推导 / 证明断裂"}, {"graph", "函数与图像"}}
	case "physics":
		specific = []mistakeType{{"concept", "物理概念"}, {"model", "情境建模"}, {"formula", "定律与公式"}, {"unit", "单位 / 数量级"}, {"experiment", "实验原理"}, {"direction", "方向 / 正负号"}, {"graph", "物理图像"}}
	case "chemistry":
		specific = []mistakeType{{"concept", "化学概念"}, {"conservation", "守恒与计量"}, {"reaction", "反应条件"}, {"experiment", "实验与现象"}, {"structure", "结构与性质"}, {"process", "工艺流程"}}
	case "biology":
		specific = []mistakeType{{"concept", "生命概念"}, {"genetics", "遗传推理"}, {"experiment", "实验变量与对照"}, {"graph", "图表与信息"}, {"regulation", "调节机制"}}
	}
	return append(specific, common...)
}
func validMistake(subject, id string) bool {
	for _, t := range mistakeTypes(subject) {
		if t.ID == id {
			return true
		}
	}
	return false
}
func (s *server) mistakeRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/learning/mistakes", s.mistakeNotebook)
	m.HandleFunc("GET /api/v1/learning/attempts/{id}/mistakes", s.attemptMistakes)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/mistakes", s.confirmMistake)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/diagnosis", s.requestDiagnosis)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/retry", s.retryOrTransfer)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/transfer", s.retryOrTransfer)
}
func (s *server) mistakeNotebook(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (
 SELECT a.id,a.question_id,a.question_revision,q.subject_id,q.kind,q.content->>'text' AS title,a.submitted_at,a.feedback->'score' AS score,a.feedback->'max_score' AS max_score,
 (SELECT to_jsonb(e)-'user_id'-'digest' FROM learning_mistake_events e WHERE e.attempt_id=a.id ORDER BY version DESC LIMIT 1) AS latest,
 (SELECT count(*) FROM learning_attempts v WHERE v.user_id=a.user_id AND v.question_id=a.question_id AND v.submitted_at IS NOT NULL) AS repetitions,
 EXISTS(SELECT 1 FROM learning_attempts v JOIN learning_score_evidence e ON e.attempt_id=v.id WHERE v.transfer_from_id=a.id AND e.confidence='verified_rule' AND e.score=e.max_score) AS transfer_verified
 FROM learning_attempts a JOIN learning_question_revisions q ON q.question_id=a.question_id AND q.revision=a.question_revision
 WHERE a.user_id=$1 AND a.submitted_at IS NOT NULL AND ($2='' OR q.subject_id=$2)
 AND ((a.feedback->>'score')::numeric<(a.feedback->>'max_score')::numeric OR EXISTS(SELECT 1 FROM learning_mistake_events e WHERE e.attempt_id=a.id))
 AND ($3='' OR EXISTS(SELECT 1 FROM learning_mistake_events e WHERE e.attempt_id=a.id AND e.type_id=$3 AND e.version=(SELECT max(version) FROM learning_mistake_events v WHERE v.attempt_id=a.id)))
 ORDER BY a.submitted_at DESC,a.id LIMIT 200)t`, userID(r.Context()), r.URL.Query().Get("subject"), r.URL.Query().Get("type")).Scan(&raw)
	if err != nil {
		s.dbError(w, "mistake notebook", err)
		return
	}
	respond(w, 200, map[string]any{"attempts": raw})
}
func (s *server) attemptMistakes(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil || a.Submitted == nil {
		fail(w, 404, "已交卷练习不存在")
		return
	}
	var candidates, events json.RawMessage
	err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM learning_mistake_candidates t WHERE attempt_id=$1`, a.ID).Scan(&candidates)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'digest' ORDER BY version),'[]') FROM learning_mistake_events t WHERE attempt_id=$1 AND user_id=$2`, a.ID, userID(r.Context())).Scan(&events)
	}
	if err != nil {
		s.dbError(w, "mistake history", err)
		return
	}
	respond(w, 200, map[string]any{"candidates": candidates, "events": events, "types": mistakeTypes(a.Question.Subject)})
}
func (s *server) confirmMistake(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request   string `json:"request_id"`
		Version   int    `json:"expected_version"`
		Action    string `json:"action"`
		Type      string `json:"type_id"`
		Candidate string `json:"candidate_id"`
		Note      string `json:"note"`
	}
	if decodeLimit(r, &in, 6000) != nil || !requestIDPattern.MatchString(in.Request) || len([]rune(in.Note)) > 1000 || (in.Action != "confirm" && in.Action != "correct" && in.Action != "unsure" && in.Action != "understood") {
		fail(w, 400, "错因确认无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil || a.Submitted == nil {
		fail(w, 404, "已交卷练习不存在")
		return
	}
	if !validMistake(a.Question.Subject, in.Type) {
		fail(w, 400, "请选择本学科错因")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "mistake transaction", err)
		return
	}
	defer tx.Rollback()
	var lock string
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM learning_attempts WHERE id=$1 AND user_id=$2 FOR UPDATE`, a.ID, userID(r.Context())).Scan(&lock); err != nil {
		s.dbError(w, "mistake lock", err)
		return
	}
	var old, prior string
	err = tx.QueryRowContext(r.Context(), `SELECT digest,attempt_id FROM learning_mistake_events WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&old, &prior)
	if err == nil {
		if old != payloadDigest(in) || prior != a.ID {
			fail(w, 409, "错因请求标识冲突")
			return
		}
		respond(w, 200, map[string]bool{"saved": true})
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "mistake replay", err)
		return
	}
	var version int
	err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(max(version),0) FROM learning_mistake_events WHERE attempt_id=$1`, a.ID).Scan(&version)
	if err != nil {
		s.dbError(w, "mistake version", err)
		return
	}
	if version != in.Version {
		fail(w, 409, "错因已更正，请刷新后重试")
		return
	}
	var candidate any
	if in.Candidate != "" {
		var exists bool
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_mistake_candidates WHERE id=$1 AND attempt_id=$2 AND (type_id=$3 OR $4<>'confirm'))`, in.Candidate, a.ID, in.Type, in.Action).Scan(&exists)
		if err != nil || !exists {
			fail(w, 400, "候选错因不属于本题或类型不符")
			return
		}
		candidate = in.Candidate
	}
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,71))`, userID(r.Context())); err != nil {
		s.dbError(w, "evidence lock", err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_mistake_events(id,attempt_id,user_id,candidate_id,version,action,type_id,note,request_id,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, security.Token(18), a.ID, userID(r.Context()), candidate, version+1, in.Action, in.Type, in.Note, in.Request, payloadDigest(in))
	if err == nil {
		err = rebuildSkillStates(r.Context(), tx, userID(r.Context()))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "confirm mistake", err)
		return
	}
	respond(w, 200, map[string]any{"saved": true, "version": version + 1})
}
func (s *server) requestDiagnosis(w http.ResponseWriter, r *http.Request) {
	if !v3Enabled("MISTAKES") {
		fail(w, 503, "智能错因分析尚未启用，可手动记录错因")
		return
	}
	var in struct {
		Personal  bool                    `json:"personal"`
		Selection *learningModelSelection `json:"model_selection"`
	}
	if decodeLimit(r, &in, 6000) != nil {
		fail(w, 400, "请求无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil || a.Submitted == nil {
		fail(w, 403, "交卷后才能分析错因")
		return
	}
	models, lead, err := s.selectedLearningModels(r.Context(), "mistake_diagnosis", in.Personal, in.Selection)
	if err != nil {
		fail(w, 403, err.Error())
		return
	}
	key := a.ID + ":" + learningModelsKey(models, lead) + ":mistake-v1"
	if !in.Personal {
		in.Selection = learningSelectionSnapshot("mistake_diagnosis", models, lead)
	}
	id, err := s.enqueueLearning(r.Context(), "diagnosis", key, learningJobPayload{AttemptID: a.ID, Personal: in.Personal, ModelSelection: in.Selection, ModelFingerprint: learningModelsKey(models, lead)})
	if err != nil {
		s.dbError(w, "queue diagnosis", err)
		return
	}
	respond(w, 202, map[string]string{"id": id})
}

type mistakeCandidate struct {
	Part          string `json:"part_id"`
	Type          string `json:"type_id"`
	QuestionQuote string `json:"question_quote"`
	AnswerQuote   string `json:"answer_quote"`
	Uncertainty   string `json:"uncertainty"`
}

func validateCandidate(a learningAttempt, c mistakeCandidate) bool {
	valid := false
	for _, p := range a.Question.Content.Parts {
		valid = valid || p.ID == c.Part
	}
	return valid && validMistake(a.Question.Subject, c.Type) && len([]rune(c.QuestionQuote)) >= 2 && len([]rune(c.QuestionQuote)) <= 500 && strings.Contains(sourceText(a.Question.Content), c.QuestionQuote) && (c.AnswerQuote != "" || a.Answers[c.Part] == "") && strings.Contains(a.Answers[c.Part], c.AnswerQuote) && len(c.AnswerQuote) <= 1500 && strings.TrimSpace(c.Uncertainty) != "" && len(c.Uncertainty) <= 1000
}
func (s *server) generateDiagnosis(ctx context.Context, p learningJobPayload) (any, error) {
	a, err := s.loadAttempt(ctx, p.AttemptID)
	if err != nil || a.Submitted == nil {
		return nil, fmt.Errorf("未交卷")
	}
	models, _, err := s.learningPayloadModels(ctx, "mistake_diagnosis", p)
	if err != nil {
		return nil, err
	}
	input := map[string]any{"question": a.Question.Content, "student_answer": a.Answers, "reference": a.Question.Solution, "types": mistakeTypes(a.Question.Subject)}
	text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, "mistake_diagnosis")+` 返回{"candidates":[{"part_id":"小题ID","type_id":"给定类型ID","question_quote":"原题逐字片段","answer_quote":"该小题学生原答逐字片段；空答案用空串","uncertainty":"为什么只是候选"}]}。最多5条，无证据返回空数组。`, stringMustMarshal(input))
	if err != nil {
		return nil, err
	}
	var out struct {
		Candidates []mistakeCandidate `json:"candidates"`
	}
	if err = platform.Decode(text, &out); err != nil || len(out.Candidates) > 5 {
		return nil, fmt.Errorf("候选错因结构无效")
	}
	for _, c := range out.Candidates {
		if !validateCandidate(a, c) {
			return nil, fmt.Errorf("候选依据不能在原题或学生答案中核实")
		}
	}
	if _, _, err = s.learningPayloadModels(ctx, "mistake_diagnosis", p); err != nil {
		return nil, err
	}
	tx, err := s.learningWriteTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lease, ok := ctx.Value(jobLeaseKey{}).(jobLease)
	if !ok {
		return nil, fmt.Errorf("missing worker lease")
	}
	for _, c := range out.Candidates {
		_, err = tx.ExecContext(ctx, `INSERT INTO learning_mistake_candidates(id,attempt_id,job_id,part_id,type_id,question_quote,answer_quote,uncertainty,model_snapshot,prompt_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'mistake-v1') ON CONFLICT DO NOTHING`, security.Token(18), a.ID, lease.ID, c.Part, c.Type, c.QuestionQuote, c.AnswerQuote, c.Uncertainty, nullableJSON(p))
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *server) retryOrTransfer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string `json:"request_id"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "练习请求无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil || a.Submitted == nil {
		fail(w, 404, "已交卷练习不存在")
		return
	}
	transfer := strings.HasSuffix(r.URL.Path, "/transfer")
	digest := payloadDigest([]any{a.ID, transfer})
	var id string
	var old sql.NullString
	err = s.db.QueryRowContext(r.Context(), `SELECT id,creation_digest FROM learning_attempts WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &old)
	if err == nil {
		if !old.Valid || old.String != digest {
			fail(w, 409, "请求标识冲突")
			return
		}
		r.SetPathValue("id", id)
		s.getLearningAttempt(w, r)
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "retry replay", err)
		return
	}
	var qid string
	var rev int
	var rule, links sql.NullString
	if transfer {
		err = s.db.QueryRowContext(r.Context(), `SELECT q.id,q.revision,rule.id,links.id FROM learning_questions q
 JOIN LATERAL(SELECT * FROM learning_scoring_rule_revisions WHERE question_id=q.id AND question_revision=q.revision ORDER BY revision DESC LIMIT 1)rule ON true
 JOIN LATERAL(SELECT * FROM learning_skill_link_revisions WHERE question_id=q.id AND question_revision=q.revision ORDER BY revision DESC LIMIT 1)links ON true
 WHERE q.status='published' AND q.subject_id=$1 AND q.id<>$2 AND NOT EXISTS(SELECT 1 FROM learning_attempts seen WHERE seen.user_id=$3 AND seen.question_id=q.id)
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(links.links) target JOIN learning_skill_link_revisions origin ON origin.id=(SELECT skill_link_revision_id FROM learning_attempts WHERE id=$4) CROSS JOIN jsonb_array_elements(origin.links) source WHERE target->>'skill_id'=source->>'skill_id' AND target->>'role'='primary' AND source->>'role'='primary') ORDER BY q.id LIMIT 1`, a.Question.Subject, a.Question.ID, userID(r.Context()), a.ID).Scan(&qid, &rev, &rule, &links)
	} else {
		qid = a.Question.ID
		rev = a.Question.Revision
		err = s.db.QueryRowContext(r.Context(), `SELECT scoring_rule_revision_id,skill_link_revision_id FROM learning_attempts WHERE id=$1 AND user_id=$2`, a.ID, userID(r.Context())).Scan(&rule, &links)
	}
	if err == sql.ErrNoRows {
		fail(w, 404, "当前没有同知识点的已审核新题；可先原题再练或选择其他范围")
		return
	}
	if err != nil {
		s.dbError(w, "transfer selection", err)
		return
	}
	id = security.Token(18)
	var origin any
	if transfer {
		origin = a.ID
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_attempts(id,user_id,question_id,question_revision,request_id,creation_digest,scoring_rule_revision_id,skill_link_revision_id,transfer_from_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(user_id,request_id) DO NOTHING`, id, userID(r.Context()), qid, rev, in.Request, digest, rule, links, origin)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT id,creation_digest FROM learning_attempts WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &old)
	}
	if err != nil {
		s.dbError(w, "create verification", err)
		return
	}
	if old.String != digest {
		fail(w, 409, "请求标识冲突")
		return
	}
	r.SetPathValue("id", id)
	s.getLearningAttempt(w, r)
}
