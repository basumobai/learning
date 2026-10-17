package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

func (s *server) learningRoutes(m *http.ServeMux) {
	s.scoringRoutes(m)
	s.skillRoutes(m)
	s.mistakeRoutes(m)
	s.planningRoutes(m)
	s.englishInteractionRoutes(m)
	s.tutorRoutes(m)
	m.HandleFunc("GET /api/v1/learning/catalog", s.learningCatalog)
	m.HandleFunc("POST /api/v1/learning/attempts", s.startLearningAttempt)
	m.HandleFunc("GET /api/v1/learning/attempts/{id}", s.getLearningAttempt)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/draft", s.saveLearningDraft)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/submit", s.submitLearningAttempt)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/explanation", s.requestExplanation)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/chat", s.questionChat)
	m.HandleFunc("GET /api/v1/learning/assets/{id}", s.learningAsset)
	m.HandleFunc("GET /api/v1/learning/jobs/{id}", s.learningJob)
	m.HandleFunc("POST /api/v1/learning/jobs/{id}/retry", s.retryLearningJob)
	m.HandleFunc("GET /api/v1/learning/model-options", s.learningModelOptions)
	m.HandleFunc("POST /api/v1/learning/model-preferences", s.saveLearningModelPreferences)
	m.HandleFunc("GET /api/v1/learning/ai-settings", s.learningAISettings)
	m.HandleFunc("POST /api/v1/learning/ai-settings", s.saveLearningAPI)
	m.HandleFunc("POST /api/v1/learning/words/next", s.nextWordRound)
	m.HandleFunc("POST /api/v1/learning/words/{id}/answer", s.answerWordRound)
	m.HandleFunc("POST /api/v1/learning/text-import", s.learningTextImport)
	m.HandleFunc("GET /api/v1/admin/learning/tasks", s.learningTasks)
	m.HandleFunc("POST /api/v1/admin/learning/tasks", s.saveLearningTask)
	m.HandleFunc("GET /api/v1/admin/learning/documents", s.learningDocuments)
	m.HandleFunc("GET /api/v1/admin/learning/local-files", s.localLearningFiles)
	m.HandleFunc("POST /api/v1/admin/learning/local-import", s.importLocalPDF)
	m.HandleFunc("GET /api/v1/admin/learning/documents/{id}/ocr", s.learningOCRPages)
	m.HandleFunc("POST /api/v1/admin/learning/documents/{id}/ocr", s.queueLearningOCR)
	m.HandleFunc("POST /api/v1/admin/learning/documents", s.uploadLearningDocument)
	m.HandleFunc("POST /api/v1/admin/learning/documents/{id}/extract", s.extractLearningDocument)
	m.HandleFunc("POST /api/v1/admin/learning/documents/{id}/scope", s.confirmDocumentScope)
	m.HandleFunc("GET /api/v1/admin/learning/documents/{id}/pages/{kind}/{page}", s.learningPage)
	m.HandleFunc("GET /api/v1/admin/learning/questions", s.adminLearningQuestions)
	m.HandleFunc("POST /api/v1/admin/learning/questions/{id}", s.saveLearningQuestion)
	m.HandleFunc("POST /api/v1/admin/learning/word-sets", s.createWordSets)
	m.HandleFunc("GET /api/v1/admin/learning/word-sets", s.adminWordSets)
	m.HandleFunc("POST /api/v1/admin/learning/word-sets/{id}/approve", s.approveWordSet)
	m.HandleFunc("GET /api/v1/admin/learning/cloud", s.learningCloud)
	m.HandleFunc("POST /api/v1/admin/learning/cloud", s.saveLearningCloud)
	m.HandleFunc("POST /api/v1/admin/learning/cloud/import", s.importCloudPDF)
	m.HandleFunc("POST /api/v1/admin/learning/cloud/list", s.listLearningCloud)
}
func (s *server) learningCatalog(w http.ResponseWriter, r *http.Request) {
	subjects := []map[string]any{}
	for _, id := range []string{"chinese", "math", "english", "physics", "chemistry", "biology"} {
		subjects = append(subjects, map[string]any{"id": id, "types": platform.Types(id), "exam": s.examForYear(r.Context(), id, examYearQuery(r)), "curriculum": platform.Curriculum(id)})
	}
	var chapters json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT c.*, (SELECT count(*) FROM learning_questions q WHERE q.chapter_id=c.id AND q.status='published') AS question_count FROM learning_chapters c ORDER BY subject_id,position,title) t`).Scan(&chapters)
	if err != nil {
		s.dbError(w, "catalog", err)
		return
	}
	counts := map[string]map[string]int{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT subject_id,kind,count(*) FROM learning_questions WHERE status='published' GROUP BY subject_id,kind`)
	if err != nil {
		s.dbError(w, "catalog distribution", err)
		return
	}
	for rows.Next() {
		var subject, kind string
		var count int
		if err = rows.Scan(&subject, &kind, &count); err != nil {
			rows.Close()
			s.dbError(w, "catalog distribution", err)
			return
		}
		if counts[subject] == nil {
			counts[subject] = map[string]int{}
		}
		counts[subject][kind] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.dbError(w, "catalog distribution", err)
		return
	}
	for _, subject := range subjects {
		c := counts[subject["id"].(string)]
		if c == nil {
			c = map[string]int{}
		}
		subject["published_counts"] = c
	}
	respond(w, 200, map[string]any{"region": "广东", "subjects": subjects, "chapters": chapters, "training_rules": map[string]any{"essay_seconds": 1800, "essay_min_han": 851, "reading_seconds": 1800}})
}

type learningQuestion struct {
	ID         string            `json:"id"`
	Subject    string            `json:"subject_id"`
	Chapter    string            `json:"chapter_id"`
	Kind       string            `json:"kind"`
	Content    platform.Content  `json:"content"`
	Solution   platform.Solution `json:"solution"`
	Revision   int               `json:"revision"`
	Assets     []map[string]any  `json:"assets"`
	Provenance json.RawMessage   `json:"provenance"`
}
type learningAttempt struct {
	RuleID            string            `json:"scoring_rule_revision_id,omitempty"`
	Scores            json.RawMessage   `json:"score_evidence,omitempty"`
	DraftVersion      int64             `json:"draft_version"`
	Jobs              json.RawMessage   `json:"jobs"`
	ID                string            `json:"id"`
	Question          learningQuestion  `json:"question"`
	Started           time.Time         `json:"started_at"`
	Deadline          *time.Time        `json:"deadline"`
	Submitted         *time.Time        `json:"submitted_at"`
	Answers           map[string]string `json:"answers"`
	Draft             map[string]string `json:"draft"`
	Feedback          json.RawMessage   `json:"feedback"`
	ServerTime        time.Time         `json:"server_time"`
	ExplanationNeeded bool              `json:"explanation_needed"`
}

func (s *server) loadAttempt(ctx context.Context, id string) (learningAttempt, error) {
	a := learningAttempt{ID: id, ServerTime: time.Now()}
	var c, sol, answers, draft, feedback []byte
	err := s.db.QueryRowContext(ctx, `SELECT q.question_id,q.subject_id,COALESCE(q.chapter_id,''),q.kind,q.content,q.solution,q.revision,q.provenance,a.started_at,a.deadline,a.submitted_at,a.answers,a.draft_answers,a.feedback,a.draft_version,COALESCE(a.scoring_rule_revision_id,'') FROM learning_attempts a JOIN learning_question_revisions q ON q.question_id=a.question_id AND q.revision=a.question_revision WHERE a.id=$1 AND a.user_id=$2`, id, userID(ctx)).Scan(&a.Question.ID, &a.Question.Subject, &a.Question.Chapter, &a.Question.Kind, &c, &sol, &a.Question.Revision, &a.Question.Provenance, &a.Started, &a.Deadline, &a.Submitted, &answers, &draft, &feedback, &a.DraftVersion, &a.RuleID)
	if err != nil {
		return a, err
	}
	if json.Unmarshal(c, &a.Question.Content) != nil || json.Unmarshal(sol, &a.Question.Solution) != nil {
		return a, fmt.Errorf("question JSON invalid")
	}
	_ = json.Unmarshal(answers, &a.Answers)
	_ = json.Unmarshal(draft, &a.Draft)
	if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'kind',kind,'status',status,'error',error) ORDER BY created_at,id),'[]') FROM learning_jobs WHERE owner_id=$1 AND payload->>'attempt_id'=$2 AND kind IN ('grade','essay','explanation')`, userID(ctx), id).Scan(&a.Jobs); err != nil {
		return a, err
	}
	if a.Submitted != nil {
		if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY created_at,id),'[]') FROM learning_score_evidence e WHERE attempt_id=$1`, id).Scan(&a.Scores); err != nil {
			return a, err
		}
	}
	a.Feedback = feedback
	a.ExplanationNeeded = learningExplanationNeeded(a)
	a.Question.Assets = []map[string]any{}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.alt FROM learning_revision_assets r JOIN learning_assets a ON a.id=r.asset_id WHERE r.question_id=$1 AND r.revision=$2 ORDER BY a.id`, a.Question.ID, a.Question.Revision)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, alt string
		if err = rows.Scan(&id, &alt); err != nil {
			return a, err
		}
		a.Question.Assets = append(a.Question.Assets, map[string]any{"id": id, "alt": alt})
	}
	return a, rows.Err()
}
func publicAttempt(a learningAttempt) learningAttempt {
	if a.Submitted == nil {
		a.Question.Solution = platform.Solution{}
		a.Question.Provenance = nil
	}
	return a
}
func (s *server) startLearningAttempt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subject    string   `json:"subject_id"`
		Kind       string   `json:"kind"`
		Chapters   []string `json:"chapter_ids"`
		Textbook   string   `json:"textbook"`
		Term       string   `json:"term"`
		Edition    string   `json:"edition"`
		Volume     string   `json:"volume"`
		RequestID  string   `json:"request_id"`
		QuestionID string   `json:"question_id"`
	}
	if decodeLimit(r, &in, 16000) != nil || !platform.ValidType(in.Subject, in.Kind) || len(in.Chapters) > 100 || !requestIDPattern.MatchString(in.RequestID) {
		fail(w, 400, "请选择科目、题型与章节范围")
		return
	}
	if in.Subject == "english" && in.Edition != "" && in.Edition != platform.EnglishEdition {
		fail(w, 400, "英语教材固定北师大版2019")
		return
	}
	in.Chapters = canonicalIDs(in.Chapters)
	fingerprint := payloadDigest(struct {
		Subject, Kind, Textbook, Term, Edition, Volume string
		Chapters                                       []string
	}{in.Subject, in.Kind, in.Textbook, in.Term, in.Edition, in.Volume, in.Chapters})
	if in.QuestionID != "" {
		fingerprint = payloadDigest([]string{fingerprint, in.QuestionID})
	}
	var existing string
	var previous sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT id,creation_digest FROM learning_attempts WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.RequestID).Scan(&existing, &previous)
	if err == nil {
		if previous.Valid && previous.String != fingerprint {
			fail(w, 409, "同一请求标识的选题范围发生改变")
			return
		}
		r.SetPathValue("id", existing)
		s.getLearningAttempt(w, r)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "attempt replay", err)
		return
	}
	if in.Chapters == nil {
		in.Chapters = []string{}
	}
	var qid string
	var rev int
	err = s.db.QueryRowContext(r.Context(), `SELECT q.id,q.revision FROM learning_questions q WHERE q.status='published' AND q.subject_id=$1 AND q.kind=$2 AND ($9='' OR q.id=$9) AND (
 (cardinality($3::text[])=0 AND $4='' AND $5='' AND $7='' AND $8='') OR EXISTS(
 SELECT 1 FROM learning_chapters c WHERE (c.id=q.chapter_id OR EXISTS(SELECT 1 FROM learning_question_chapters qc WHERE qc.question_id=q.id AND qc.chapter_id=c.id))
 AND (cardinality($3::text[])=0 OR c.id=ANY($3::text[])) AND ($4='' OR c.textbook=$4) AND ($5='' OR c.term=$5) AND ($7='' OR c.edition=$7) AND ($8='' OR c.volume=$8)))
 ORDER BY (SELECT count(*) FROM learning_attempts a WHERE a.user_id=$6 AND a.question_id=q.id),random() LIMIT 1`, in.Subject, in.Kind, in.Chapters, in.Textbook, in.Term, userID(r.Context()), in.Edition, in.Volume, in.QuestionID).Scan(&qid, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "这个范围还没有已审核题目，请管理员导入并发布")
		return
	}
	if err != nil {
		s.dbError(w, "select question", err)
		return
	}
	id := security.Token(18)
	var seconds int
	if in.Subject == "chinese" && (in.Kind == "essay" || in.Kind == "poetry" || strings.Contains(in.Kind, "reading")) {
		seconds = 1800
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_attempts(id,user_id,question_id,question_revision,request_id,deadline,creation_digest,scoring_rule_revision_id,skill_link_revision_id) VALUES($1,$2,$3,$4,$5,CASE WHEN $6::int>0 THEN now()+make_interval(secs=>$6) END,$7,(SELECT id FROM learning_scoring_rule_revisions WHERE question_id=$3 AND question_revision=$4 AND $8 ORDER BY revision DESC LIMIT 1),(SELECT id FROM learning_skill_link_revisions WHERE question_id=$3 AND question_revision=$4 ORDER BY revision DESC LIMIT 1)) ON CONFLICT(user_id,request_id) DO NOTHING`, id, userID(r.Context()), qid, rev, in.RequestID, seconds, fingerprint, v3Enabled("SCORING"))
	if err != nil {
		s.dbError(w, "start attempt", err)
		return
	}
	if err = s.db.QueryRowContext(r.Context(), `SELECT id,creation_digest FROM learning_attempts WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.RequestID).Scan(&id, &previous); err != nil {
		s.dbError(w, "create replay", err)
		return
	}
	if previous.Valid && previous.String != fingerprint {
		fail(w, 409, "同一请求标识的选题范围发生改变")
		return
	}
	r.SetPathValue("id", id)
	s.getLearningAttempt(w, r)
}
func (s *server) getLearningAttempt(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "练习不存在或题目版本已更新，请重新选题")
		return
	}
	if err != nil {
		s.dbError(w, "get attempt", err)
		return
	}
	respond(w, 200, publicAttempt(a))
}
func validateLearningAnswers(c platform.Content, a map[string]string) bool {
	if len(a) > len(c.Parts) {
		return false
	}
	valid := map[string]bool{}
	for _, p := range c.Parts {
		valid[p.ID] = true
	}
	for id, v := range a {
		if !valid[id] || len([]rune(v)) > 12000 {
			return false
		}
	}
	return true
}
func (s *server) saveLearningDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64             `json:"version"`
		Answers map[string]string `json:"answers"`
	}
	if decodeLimit(r, &in, 100000) != nil || in.Version < 1 {
		fail(w, 400, "作答格式无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, 404, "练习不存在")
		return
	}
	if !validateLearningAnswers(a.Question.Content, in.Answers) {
		fail(w, 400, "作答内容无效")
		return
	}
	raw, _ := json.Marshal(in.Answers)
	result, err := s.db.ExecContext(r.Context(), `UPDATE learning_attempts SET draft_answers=$3,draft_version=$4 WHERE id=$1 AND user_id=$2 AND draft_version<$4 AND submitted_at IS NULL AND (deadline IS NULL OR deadline>clock_timestamp())`, a.ID, userID(r.Context()), string(raw), in.Version)
	if err != nil {
		s.dbError(w, "save draft", err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var version int64
		var old []byte
		var closed bool
		if s.db.QueryRowContext(r.Context(), `SELECT draft_version,draft_answers,submitted_at IS NOT NULL OR (deadline IS NOT NULL AND deadline<=clock_timestamp()) FROM learning_attempts WHERE id=$1 AND user_id=$2`, a.ID, userID(r.Context())).Scan(&version, &old, &closed) == nil && !closed && version == in.Version {
			var prior map[string]string
			_ = json.Unmarshal(old, &prior)
			canonical, _ := json.Marshal(prior)
			if string(canonical) == string(raw) {
				respond(w, 200, map[string]bool{"saved": true})
				return
			}
		}
		fail(w, 409, "草稿版本已更新、已交卷或训练时间已结束")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func feedbackFor(a learningAttempt, answers map[string]string) map[string]any {
	parts := []map[string]any{}
	score, total := 0.0, 0.0
	subjective := false
	for _, p := range a.Question.Content.Parts {
		expected := a.Question.Solution.Answers[p.ID]
		correct := platform.NormalizeAnswer(answers[p.ID]) == platform.NormalizeAnswer(expected)
		if len(p.Choices) > 0 {
			correct = platform.ChoiceAnswer(answers[p.ID]) == platform.ChoiceAnswer(expected)
		}
		objective := len(p.Choices) > 0 || a.Question.Kind == "grammar_fill" || a.Question.Kind == "fill" || a.Question.Kind == "memorization"
		points := 0.0
		if correct && objective {
			points = p.Points
			score += points
		}
		total += p.Points
		if !objective {
			subjective = true
		}
		parts = append(parts, map[string]any{"id": p.ID, "correct": correct, "points": points, "max_points": p.Points, "needs_ai": !objective, "expected": expected})
	}
	return map[string]any{"parts": parts, "score": score, "max_score": total, "needs_ai": subjective, "ai_reference": false}
}
func (s *server) submitLearningAttempt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientRequestID string                  `json:"client_request_id"`
		Answers         map[string]string       `json:"answers"`
		Personal        bool                    `json:"personal"`
		ModelSelection  *learningModelSelection `json:"model_selection"`
	}
	if decodeLimit(r, &in, 120000) != nil || (in.ClientRequestID != "" && !requestIDPattern.MatchString(in.ClientRequestID)) {
		fail(w, 400, "作答格式无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, 404, "练习不存在或版本已更新")
		return
	}
	if !validateLearningAnswers(a.Question.Content, in.Answers) {
		fail(w, 400, "作答内容无效")
		return
	}
	// Model problems must not discard or delay a student's submitted answer.
	// The grading worker validates group grants before any provider call.
	// Deadline uses server-stored draft; late clients cannot replace a timed answer.
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "submit transaction", err)
		return
	}
	defer tx.Rollback()
	var old []byte
	var expired bool
	var submitted *time.Time
	err = tx.QueryRowContext(r.Context(), `SELECT draft_answers,deadline IS NOT NULL AND deadline<=clock_timestamp(),submitted_at FROM learning_attempts WHERE id=$1 AND user_id=$2 FOR UPDATE`, a.ID, userID(r.Context())).Scan(&old, &expired, &submitted)
	if err != nil {
		s.dbError(w, "lock attempt", err)
		return
	}
	if err = bindSubmission(r.Context(), tx, userID(r.Context()), a.ID, in.ClientRequestID, payloadDigest(in)); err != nil {
		if errors.Is(err, errSubmissionConflict) {
			fail(w, 409, err.Error())
		} else {
			s.dbError(w, "submission request", err)
		}
		return
	}
	if submitted != nil {
		if err = tx.Commit(); err != nil {
			s.dbError(w, "submission replay", err)
			return
		}
		s.getLearningAttempt(w, r)
		return
	}
	if expired {
		in.Answers = map[string]string{}
		_ = json.Unmarshal(old, &in.Answers)
	}
	feedback := feedbackFor(a, in.Answers)
	rule, err := s.attemptRule(r.Context(), a)
	if err != nil {
		s.dbError(w, "load scoring rule", err)
		return
	}
	if rule != nil {
		result, e := rule.Score(a.Question.Content, in.Answers)
		if e != nil {
			s.dbError(w, "score approved rule", e)
			return
		}
		feedback = map[string]any{"parts": result.Parts, "score": result.Score, "max_score": result.Max, "needs_ai": result.NeedsReview, "ai_reference": false, "confidence": result.Confidence, "engine_version": result.Engine}
	} else {
		feedback["confidence"] = "legacy"
	}
	if a.Question.Kind == "essay" && a.Question.Subject == "chinese" {
		n := 0
		for _, v := range in.Answers {
			n += platform.HanCount(v)
		}
		feedback["han_count"] = n
		if n < 851 && !expired {
			fail(w, 400, fmt.Sprintf("有效汉字需超过850个，目前%d个（标点、数字和字母不计）", n))
			return
		}
		if n < 851 {
			feedback["needs_ai"] = false
			feedback["incomplete"] = true
		}
		feedback["training_rule"] = "30分钟、有效汉字超过850个"
	}
	feedback["timed_out"] = expired
	raw, _ := json.Marshal(in.Answers)
	fr, _ := json.Marshal(feedback)
	_, err = tx.ExecContext(r.Context(), `UPDATE learning_attempts SET answers=$3,draft_answers=$3,feedback=$4,submitted_at=clock_timestamp(),submission_digest=$5 WHERE id=$1 AND user_id=$2`, a.ID, userID(r.Context()), string(raw), string(fr), payloadDigest(in.Answers))
	if err == nil && v3Enabled("SCORING") {
		err = writeScoreEvidence(r.Context(), tx, a, in.Answers, feedback)
	}
	if err == nil && feedback["needs_ai"] == true {
		kind := "grade"
		if a.Question.Subject == "chinese" && a.Question.Kind == "essay" {
			kind = "essay"
		}
		p := learningJobPayload{AttemptID: a.ID, Personal: in.Personal, ModelSelection: in.ModelSelection}
		task := "grading"
		if kind == "essay" {
			task = "essay_reviewer"
		}
		if models, lead, e := s.selectedLearningModels(r.Context(), task, in.Personal, in.ModelSelection); e == nil {
			p.ModelFingerprint = learningModelsKey(models, lead)
			if !in.Personal {
				p.ModelSelection = learningSelectionSnapshot(task, models, lead)
			}
		}
		payload, _ := json.Marshal(p)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_jobs(id,owner_id,kind,dedupe_key,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner_id,kind,dedupe_key) DO NOTHING`, security.Token(18), userID(r.Context()), kind, a.ID, string(payload))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "submit attempt", err)
		return
	}
	s.getLearningAttempt(w, r)
}
func (s *server) requestExplanation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Personal       bool                    `json:"personal"`
		Reason         string                  `json:"reason"`
		ModelSelection *learningModelSelection `json:"model_selection"`
	}
	if decodeLimit(r, &in, 6000) != nil {
		fail(w, 400, "请求格式无效")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, 404, "练习不存在")
		return
	}
	if a.Submitted == nil {
		fail(w, 403, "先完成并提交作答，再按需生成动态讲解")
		return
	}
	if in.Reason == "" {
		in.Reason = "incorrect"
	}
	if in.Reason != "incorrect" && in.Reason != "self_report" {
		fail(w, 400, "讲解触发方式无效")
		return
	}
	if in.Reason == "incorrect" && !learningExplanationNeeded(a) {
		fail(w, 409, "这道题尚未判错；没有理解可主动选择动态讲解")
		return
	}
	models, lead, err := s.selectedLearningModels(r.Context(), "explanation", in.Personal, in.ModelSelection)
	if err != nil {
		fail(w, 403, "动态讲解模型不可用："+err.Error())
		return
	}
	var revision int
	_ = s.db.QueryRowContext(r.Context(), `SELECT revision FROM learning_ai_tasks WHERE task='explanation'`).Scan(&revision)
	key := fmt.Sprintf("%s:%d:%d:%s", a.ID, a.Question.Revision, revision, learningModelsKey(models, lead))
	// Snapshot the resolved group model so later admin routing edits cannot switch queued work.
	if !in.Personal {
		in.ModelSelection = learningSelectionSnapshot("explanation", models, lead)
	}
	id, err := s.enqueueLearning(r.Context(), "explanation", key, learningJobPayload{AttemptID: a.ID, Personal: in.Personal, PromptRevision: revision, Reason: in.Reason, ModelSelection: in.ModelSelection, ModelFingerprint: learningModelsKey(models, lead)})
	if err != nil {
		s.dbError(w, "queue explanation", err)
		return
	}
	respond(w, 202, map[string]string{"id": id})
}
func (s *server) questionChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		JobID          string                  `json:"job_id"`
		StepID         string                  `json:"step_id"`
		Content        string                  `json:"content"`
		RequestID      string                  `json:"request_id"`
		Personal       bool                    `json:"personal"`
		ModelSelection *learningModelSelection `json:"model_selection"`
	}
	if decodeLimit(r, &in, 16000) != nil || strings.TrimSpace(in.Content) == "" || len([]rune(in.Content)) > 2000 || !requestIDPattern.MatchString(in.RequestID) {
		fail(w, 400, "请输入当前步骤的问题，最多2000字")
		return
	}
	a, err := s.loadAttempt(r.Context(), r.PathValue("id"))
	if err != nil || a.Submitted == nil {
		fail(w, 403, "交卷后可追问讲解")
		return
	}
	var raw []byte
	var sourceAttempt string
	err = s.db.QueryRowContext(r.Context(), `SELECT result,payload->>'attempt_id' FROM learning_jobs WHERE id=$1 AND owner_id=$2 AND kind='explanation' AND status='completed'`, in.JobID, userID(r.Context())).Scan(&raw, &sourceAttempt)
	if err != nil {
		fail(w, 404, "讲解不存在")
		return
	}
	src, err := s.loadAttempt(r.Context(), sourceAttempt)
	if err != nil || src.Question.ID != a.Question.ID || src.Question.Revision != a.Question.Revision {
		fail(w, 403, "讲解与题目不匹配")
		return
	}
	var timeline platform.Timeline
	if json.Unmarshal(raw, &timeline) != nil {
		fail(w, 409, "讲解数据无效")
		return
	}
	step, ok := timeline.Step(in.StepID)
	if !ok {
		fail(w, 400, "请选择有效讲解步骤")
		return
	}
	models, _, err := s.selectedLearningModels(r.Context(), "question_tutor", in.Personal, in.ModelSelection)
	if err != nil {
		fail(w, 403, err.Error())
		return
	}
	if s.aiReplay(w, r, in.RequestID, "question_tutor:"+a.ID+":"+in.StepID, "", in) {
		return
	}
	complete := false
	defer func() {
		if !complete {
			s.failAI(r.Context(), in.RequestID)
		}
	}()
	// Only this question, current step and the last two exchanges for that step.
	contextData := map[string]any{"question": a.Question.Content, "reference": a.Question.Solution, "student_answer": a.Answers, "current_step": step, "question_from_student": in.Content}
	history := []map[string]string{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT question,answer FROM learning_question_chat WHERE attempt_id=$1 AND step_id=$2 ORDER BY id DESC LIMIT 2`, a.ID, in.StepID)
	if err != nil {
		s.dbError(w, "step history", err)
		return
	}
	for rows.Next() {
		var q, v string
		if rows.Scan(&q, &v) != nil {
			continue
		}
		history = append(history, map[string]string{"question": q, "answer": v})
	}
	rows.Close()
	contextData["recent_exchanges_newest_first"] = history
	cr, _ := json.Marshal(contextData)
	text, err := s.completion(r.Context(), models[0], []chatMessage{{"system", s.taskPrompt(r.Context(), "question_tutor")}, {"user", string(cr)}}, false)
	if err != nil {
		providerFailure(w, err)
		return
	}
	if len([]rune(text)) > 8000 {
		fail(w, 502, "模型回答过长，请重试")
		return
	}
	if _, _, err = s.selectedLearningModels(r.Context(), "question_tutor", in.Personal, in.ModelSelection); err != nil {
		fail(w, 403, "模型授权已变更")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "question chat", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_question_chat(attempt_id,step_id,question,answer) VALUES($1,$2,$3,$4)`, a.ID, in.StepID, in.Content, text)
	result := map[string]string{"answer": text}
	if err == nil {
		err = finishAI(r.Context(), tx, in.RequestID, result)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save question chat", err)
		return
	}
	complete = true
	respond(w, 200, result)
}
func (s *server) learningAsset(w http.ResponseWriter, r *http.Request) {
	var image []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT a.image FROM learning_assets a WHERE a.id=$1 AND ($2='admin' OR EXISTS(SELECT 1 FROM learning_revision_assets ra JOIN learning_attempts t ON t.question_id=ra.question_id AND t.question_revision=ra.revision WHERE ra.asset_id=a.id AND t.user_id=$3))`, r.PathValue("id"), account(r.Context()).Role, userID(r.Context())).Scan(&image)
	if err != nil {
		fail(w, 404, "插图不存在")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(image)
}
