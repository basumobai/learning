package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

type examCandidate struct {
	ID       string
	Revision int
	Content  platform.Content
	Rule     string
	Links    sql.NullString
	Kind     string
	Points   int
}
type examSectionSelection struct {
	Name      string
	Questions []examCandidate
}

func cents(v float64) int { return int(math.Round(v * 100)) }

// Bounded subset selection keeps material groups intact while matching actual
// small-question counts and section points. It never invents missing questions.
func chooseExamSection(candidates []examCandidate, count, points int) ([]examCandidate, bool) {
	type target struct{ n, p int }
	states := map[target][]int{{0, 0}: {}}
	for i, c := range candidates {
		n := len(c.Content.Parts)
		if n == 0 {
			continue
		}
		keys := make([]target, 0, len(states))
		for k := range states {
			keys = append(keys, k)
		}
		for _, k := range keys {
			next := target{k.n + n, k.p + c.Points}
			if next.n > count || next.p > points {
				continue
			}
			if _, exists := states[next]; !exists {
				states[next] = append(append([]int{}, states[k]...), i)
			}
			if next.n == count && next.p == points {
				out := []examCandidate{}
				for _, index := range states[next] {
					out = append(out, candidates[index])
				}
				return out, true
			}
		}
		if len(states) > 20000 {
			return nil, false
		}
	}
	return nil, false
}
func completeExamRule(rule examRule) error {
	if !rule.Verified || rule.Region != "广东" || rule.Seconds == nil || rule.Total == nil || *rule.Seconds < 30 || *rule.Total <= 0 {
		return fmt.Errorf("规则缺少已核验的广东年份、时限或总分")
	}
	total := 0
	for _, section := range rule.Sections {
		if section.Count == nil || section.Points == nil || *section.Count < 1 || *section.Count > 100 || *section.Points <= 0 {
			return fmt.Errorf("各板块必须核验小题数与板块总分")
		}
		total += cents(*section.Points)
	}
	if total != cents(*rule.Total) {
		return fmt.Errorf("板块分值与总分不一致")
	}
	return nil
}
func (s *server) examRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/learning/exams/options", s.examOptions)
	m.HandleFunc("GET /api/v1/learning/exams/active", s.activeExam)
	m.HandleFunc("POST /api/v1/learning/exams", s.startExam)
	m.HandleFunc("GET /api/v1/learning/exams/{id}", s.getExam)
	m.HandleFunc("POST /api/v1/learning/exams/{id}/submit", s.submitExam)
	s.recordingRoutes(m)
}
func (s *server) examOptions(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT * FROM learning_exam_rules WHERE verified AND region='广东' AND duration_seconds IS NOT NULL AND total_points IS NOT NULL AND NOT EXISTS(SELECT 1 FROM learning_exam_rules n WHERE n.family=learning_exam_rules.family AND n.revision>learning_exam_rules.revision) ORDER BY year_from DESC,subject_id)t`).Scan(&raw)
	if err != nil {
		s.dbError(w, "exam options", err)
		return
	}
	respond(w, 200, map[string]any{"rules": raw, "label": "平台模拟卷，按已审核规则组卷"})
}
func (s *server) activeExam(w http.ResponseWriter, r *http.Request) {
	var id string
	err := s.db.QueryRowContext(r.Context(), `SELECT id FROM learning_exam_sessions WHERE user_id=$1 AND submitted_at IS NULL`, userID(r.Context())).Scan(&id)
	if err == sql.ErrNoRows {
		respond(w, 200, map[string]any{"exam": nil})
		return
	}
	if err != nil {
		s.dbError(w, "active exam", err)
		return
	}
	respond(w, 200, map[string]any{"exam": map[string]string{"id": id}})
}
func (s *server) startExam(w http.ResponseWriter, r *http.Request) {
	if !v3Enabled("EXAMS") {
		fail(w, 503, "整卷模拟尚未启用")
		return
	}
	var in struct {
		Request string `json:"request_id"`
		Rule    string `json:"rule_id"`
		Year    int    `json:"year"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) || in.Year < 1977 || in.Year > 2100 {
		fail(w, 400, "考试配置无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "start exam", err)
		return
	}
	defer tx.Rollback()
	digest := payloadDigest(in)
	var existing, prior string
	err = tx.QueryRowContext(r.Context(), `SELECT id,digest FROM learning_exam_sessions WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&existing, &prior)
	if err == nil {
		if prior != digest {
			fail(w, 409, "组卷请求冲突")
			return
		}
		tx.Rollback()
		r.SetPathValue("id", existing)
		s.getExam(w, r)
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "exam replay", err)
		return
	}
	var active bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_exam_sessions WHERE user_id=$1 AND submitted_at IS NULL)`, userID(r.Context())).Scan(&active); err != nil {
		s.dbError(w, "exam active", err)
		return
	}
	if active {
		fail(w, 409, "请先恢复并完成正在进行的模拟卷")
		return
	}
	var running bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM ai_requests WHERE user_id=$1 AND status='running' AND updated_at>now()-interval '3 minutes') OR EXISTS(SELECT 1 FROM learning_jobs WHERE owner_id=$1 AND queue='interactive' AND status IN ('queued','running','retry_wait'))`, userID(r.Context())).Scan(&running); err != nil {
		s.dbError(w, "exam pending AI", err)
		return
	}
	if running {
		fail(w, 409, "请等待当前AI请求或讲解任务完成后开始模拟卷")
		return
	}
	var raw []byte
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(r) FROM learning_exam_rules r WHERE id=$1`, in.Rule).Scan(&raw)
	var rule examRule
	if err != nil || json.Unmarshal(raw, &rule) != nil {
		fail(w, 404, "考试规则不存在")
		return
	}
	if err = completeExamRule(rule); err != nil || in.Year < rule.From || in.Year > rule.To {
		fail(w, 409, "年份或规则尚未完整核验，不能宣称对齐组卷")
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT q.id,q.revision,q.kind,q.content,rule.id,links.id FROM learning_questions q
 JOIN LATERAL(SELECT id FROM learning_scoring_rule_revisions WHERE question_id=q.id AND question_revision=q.revision ORDER BY revision DESC LIMIT 1)rule ON true
 LEFT JOIN LATERAL(SELECT id FROM learning_skill_link_revisions WHERE question_id=q.id AND question_revision=q.revision ORDER BY revision DESC LIMIT 1)links ON true
 WHERE q.status='published' AND q.subject_id=$1 ORDER BY q.id LIMIT 300 FOR SHARE OF q`, rule.Subject)
	if err != nil {
		s.dbError(w, "exam candidates", err)
		return
	}
	all := []examCandidate{}
	for rows.Next() {
		var c examCandidate
		var body []byte
		if err = rows.Scan(&c.ID, &c.Revision, &c.Kind, &body, &c.Rule, &c.Links); err != nil {
			break
		}
		if json.Unmarshal(body, &c.Content) != nil {
			err = fmt.Errorf("invalid exam content")
			break
		}
		for _, p := range c.Content.Parts {
			c.Points += cents(p.Points)
		}
		all = append(all, c)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "exam candidate scan", err)
		return
	}
	selections := []examSectionSelection{}
	used := map[string]bool{}
	for _, section := range rule.Sections {
		candidates := []examCandidate{}
		for _, q := range all {
			if used[q.ID] {
				continue
			}
			for _, kind := range section.Kinds {
				if q.Kind == kind {
					candidates = append(candidates, q)
					break
				}
			}
		}
		chosen, ok := chooseExamSection(candidates, *section.Count, cents(*section.Points))
		if !ok {
			fail(w, 409, "题库不足以匹配板块「"+section.Name+"」的已核验小题数与分值；未创建模拟卷")
			return
		}
		selections = append(selections, examSectionSelection{section.Name, chosen})
		for _, q := range chosen {
			used[q.ID] = true
		}
	}
	id := security.Token(18)
	var deadline time.Time
	err = tx.QueryRowContext(r.Context(), `INSERT INTO learning_exam_sessions(id,user_id,rule_id,year,request_id,digest,rule_snapshot,deadline,total_points) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+make_interval(secs=>$8),$9) RETURNING deadline`, id, userID(r.Context()), in.Rule, in.Year, in.Request, digest, string(raw), *rule.Seconds, *rule.Total).Scan(&deadline)
	position := 0
	for _, section := range selections {
		for _, q := range section.Questions {
			if err != nil {
				break
			}
			position++
			attempt := security.Token(18)
			_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_attempts(id,user_id,question_id,question_revision,request_id,deadline,creation_digest,scoring_rule_revision_id,skill_link_revision_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, attempt, userID(r.Context()), q.ID, q.Revision, "exam:"+id+fmt.Sprint(position), deadline, digest, q.Rule, q.Links)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_exam_items(exam_id,position,attempt_id,section_name,points) VALUES($1,$2,$3,$4,$5)`, id, position, attempt, section.Name, float64(q.Points)/100)
			}
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "persist exam snapshot", err)
		return
	}
	r.SetPathValue("id", id)
	s.getExam(w, r)
}
func (s *server) getExam(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(e)-'digest'-'submission_digest'-'user_id' FROM learning_exam_sessions e WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&raw)
	if err != nil {
		fail(w, 404, "模拟卷不存在")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT attempt_id,section_name,position FROM learning_exam_items WHERE exam_id=$1 ORDER BY position`, r.PathValue("id"))
	if err != nil {
		s.dbError(w, "exam items", err)
		return
	}
	type item struct {
		Attempt  learningAttempt `json:"attempt"`
		Section  string          `json:"section"`
		Position int             `json:"position"`
	}
	type key struct {
		ID, Section string
		Position    int
	}
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.ID, &k.Section, &k.Position); err != nil {
			break
		}
		keys = append(keys, k)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "exam item scan", err)
		return
	}
	items := []item{}
	for _, k := range keys {
		a, e := s.loadAttempt(r.Context(), k.ID)
		if e != nil {
			s.dbError(w, "exam snapshot", e)
			return
		}
		items = append(items, item{publicAttempt(a), k.Section, k.Position})
	}
	respond(w, 200, map[string]any{"exam": raw, "items": items, "server_time": time.Now(), "label": "平台模拟卷"})
}
func (s *server) submitExam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string                       `json:"request_id"`
		Answers map[string]map[string]string `json:"answers"`
	}
	if decodeLimit(r, &in, 2*1024*1024) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "整卷作答格式无效")
		return
	}
	if err := s.finalizeExam(r.Context(), r.PathValue("id"), in.Request, payloadDigest(in), in.Answers, false); err != nil {
		if e, ok := err.(examError); ok {
			fail(w, e.code, e.message)
		} else {
			s.dbError(w, "submit exam", err)
		}
		return
	}
	s.getExam(w, r)
}

type examError struct {
	code    int
	message string
}

func (e examError) Error() string { return e.message }
func (s *server) finalizeExam(ctx context.Context, id, key, digest string, answers map[string]map[string]string, expiryOnly bool) error {
	tx, err := s.studyTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expired bool
	var submitted *time.Time
	err = tx.QueryRowContext(ctx, `SELECT deadline<=clock_timestamp(),submitted_at FROM learning_exam_sessions WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, userID(ctx)).Scan(&expired, &submitted)
	if err == sql.ErrNoRows {
		return examError{404, "模拟卷不存在"}
	}
	if err != nil {
		return err
	}
	if expiryOnly && !expired {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO learning_exam_submissions(user_id,request_id,exam_id,digest) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, userID(ctx), key, id, digest)
	if err != nil {
		return err
	}
	var previous, priorID string
	err = tx.QueryRowContext(ctx, `SELECT digest,exam_id FROM learning_exam_submissions WHERE user_id=$1 AND request_id=$2`, userID(ctx), key).Scan(&previous, &priorID)
	if err != nil {
		return err
	}
	if previous != digest || priorID != id {
		return examError{409, "同键整卷答案发生改变"}
	}
	if submitted != nil {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,q.content,q.solution,q.question_id,q.revision,q.kind,q.subject_id,a.scoring_rule_revision_id,a.draft_answers FROM learning_exam_items i JOIN learning_attempts a ON a.id=i.attempt_id JOIN learning_question_revisions q ON q.question_id=a.question_id AND q.revision=a.question_revision WHERE i.exam_id=$1 ORDER BY i.position FOR UPDATE OF a`, id)
	if err != nil {
		return err
	}
	type scoring struct {
		a     learningAttempt
		draft []byte
	}
	work := []scoring{}
	allowed := map[string]bool{}
	for rows.Next() {
		var v scoring
		var c, sol []byte
		err = rows.Scan(&v.a.ID, &c, &sol, &v.a.Question.ID, &v.a.Question.Revision, &v.a.Question.Kind, &v.a.Question.Subject, &v.a.RuleID, &v.draft)
		if err != nil {
			break
		}
		if json.Unmarshal(c, &v.a.Question.Content) != nil || json.Unmarshal(sol, &v.a.Question.Solution) != nil {
			err = fmt.Errorf("invalid exam snapshot")
			break
		}
		allowed[v.a.ID] = true
		work = append(work, v)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	if !expired {
		for aid := range answers {
			if !allowed[aid] {
				return examError{400, "作答包含不属于本卷的题目"}
			}
		}
	}
	for _, v := range work {
		answer := answers[v.a.ID]
		if expired {
			answer = map[string]string{}
			json.Unmarshal(v.draft, &answer)
		}
		if answer == nil {
			answer = map[string]string{}
		}
		if !validateLearningAnswers(v.a.Question.Content, answer) {
			return examError{400, "小题答案无效"}
		}
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT rule_json FROM learning_scoring_rule_revisions WHERE id=$1`, v.a.RuleID).Scan(&raw); err != nil {
			return err
		}
		var rule platform.ScoringRule
		if json.Unmarshal(raw, &rule) != nil {
			return fmt.Errorf("invalid exam rule")
		}
		score, err := rule.Score(v.a.Question.Content, answer)
		if err != nil {
			return err
		}
		feedback := map[string]any{"parts": score.Parts, "score": score.Score, "max_score": score.Max, "needs_ai": score.NeedsReview, "ai_reference": false, "confidence": score.Confidence, "timed_out": expired}
		_, err = tx.ExecContext(ctx, `UPDATE learning_attempts SET answers=$2,draft_answers=$2,feedback=$3,submitted_at=clock_timestamp(),submission_digest=$4 WHERE id=$1`, v.a.ID, nullableJSON(answer), nullableJSON(feedback), payloadDigest(answer))
		if err != nil {
			return err
		}
		if err = writeScoreEvidence(ctx, tx, v.a, answer, feedback); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE learning_exam_sessions SET submitted_at=clock_timestamp(),submission_digest=$3 WHERE id=$1 AND user_id=$2`, id, userID(ctx), digest)
	if err == nil {
		err = tx.Commit()
	}
	return err
}

// While a paper is open, only its questions/drafts/assets, the exam endpoints,
// and account exit are available. Past answer histories cannot bypass the paper.
func (s *server) examAccess(w http.ResponseWriter, r *http.Request) bool {
	var id string
	err := s.db.QueryRowContext(r.Context(), `SELECT id FROM learning_exam_sessions WHERE user_id=$1 AND submitted_at IS NULL`, userID(r.Context())).Scan(&id)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		s.dbError(w, "exam access", err)
		return false
	}
	p := r.URL.Path
	if p == "/api/v1/learning/exams" || p == "/api/v1/auth/me" || p == "/api/v1/auth/logout" || p == "/api/v1/auth/password" || strings.HasPrefix(p, "/api/v1/learning/exams/") {
		return true
	}
	var allowed bool
	if strings.HasPrefix(p, "/api/v1/learning/attempts/") {
		parts := strings.Split(strings.TrimPrefix(p, "/api/v1/learning/attempts/"), "/")
		if len(parts) == 1 && r.Method == "GET" || len(parts) == 2 && parts[1] == "draft" && r.Method == "POST" {
			err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_exam_items WHERE exam_id=$1 AND attempt_id=$2)`, id, parts[0]).Scan(&allowed)
		}
	}
	if strings.HasPrefix(p, "/api/v1/learning/assets/") && r.Method == "GET" {
		err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_exam_items i JOIN learning_attempts a ON a.id=i.attempt_id JOIN learning_revision_assets ra ON ra.question_id=a.question_id AND ra.revision=a.question_revision WHERE i.exam_id=$1 AND ra.asset_id=$2)`, id, strings.TrimPrefix(p, "/api/v1/learning/assets/")).Scan(&allowed)
	}
	if allowed && err == nil {
		return true
	}
	fail(w, 403, "模拟卷正在计时；请先恢复或提交整卷，再查看历史、资料与AI辅导")
	return false
}

func (s *server) expireLearningExam(ctx context.Context) error {
	var id, uid string
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id FROM learning_exam_sessions WHERE submitted_at IS NULL AND deadline<=clock_timestamp() ORDER BY deadline LIMIT 1`).Scan(&id, &uid)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	owned := context.WithValue(ctx, identityKey{}, identity{ID: uid})
	return s.finalizeExam(owned, id, "exam-expiry-"+id, payloadDigest([]string{id, "server_expiry"}), nil, true)
}
