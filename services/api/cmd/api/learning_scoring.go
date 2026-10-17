package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

func v3Enabled(feature string) bool { return os.Getenv("LEARNING_V3_"+feature) == "true" }
func payloadDigest(v any) string    { b, _ := json.Marshal(v); return security.Digest(string(b)) }
func canonicalIDs(ids []string) []string {
	m := map[string]bool{}
	for _, s := range ids {
		m[s] = true
	}
	out := []string{}
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (s *server) scoringRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/learning/questions/{id}/scoring-rules", s.scoringRules)
	m.HandleFunc("POST /api/v1/admin/learning/questions/{id}/scoring-rules", s.approveScoringRule)
}
func (s *server) scoringRules(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY question_revision DESC,revision DESC),'[]') FROM learning_scoring_rule_revisions t WHERE question_id=$1`, r.PathValue("id")).Scan(&raw)
	if err != nil {
		s.dbError(w, "scoring rules", err)
		return
	}
	respond(w, 200, map[string]any{"rules": raw})
}
func (s *server) approveScoringRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QuestionRevision int                  `json:"question_revision"`
		Rule             platform.ScoringRule `json:"rule"`
		Source           string               `json:"source_revision"`
	}
	if decodeLimit(r, &in, 64000) != nil || in.QuestionRevision < 1 || len(strings.TrimSpace(in.Source)) < 3 || len(in.Source) > 500 {
		fail(w, 400, "需要题目版本、逐题规则和核验依据")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "rule transaction", err)
		return
	}
	defer tx.Rollback()
	var raw, solutionRaw []byte
	var current int
	err = tx.QueryRowContext(r.Context(), `SELECT content,revision,solution FROM learning_questions WHERE id=$1 AND status='published' FOR UPDATE`, r.PathValue("id")).Scan(&raw, &current, &solutionRaw)
	if err != nil {
		fail(w, 404, "已发布题目不存在")
		return
	}
	if current != in.QuestionRevision {
		fail(w, 409, "题目已修订，请刷新后核验规则")
		return
	}
	var c platform.Content
	if json.Unmarshal(raw, &c) != nil {
		fail(w, 409, "题目无效")
		return
	}
	if err = in.Rule.Validate(c); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var solution platform.Solution
	if json.Unmarshal(solutionRaw, &solution) != nil {
		fail(w, 409, "原题参考答案无效")
		return
	}
	for _, part := range in.Rule.Parts {
		if part.Mode == "manual" {
			continue
		}
		probe := platform.ScoringRule{Parts: []platform.ScoringPart{part}}
		var content platform.Content
		for _, p := range c.Parts {
			if p.ID == part.ID {
				content = platform.Content{Parts: []platform.Part{p}}
			}
		}
		scored, e := probe.Score(content, map[string]string{part.ID: solution.Answers[part.ID]})
		if e != nil || len(scored.Parts) != 1 || !scored.Parts[0].Correct {
			fail(w, 400, "新规则与原题参考答案矛盾；请先修订题目答案，再审核评分规则")
			return
		}
	}
	total := 0.0
	for _, p := range c.Parts {
		total += p.Points
	}
	id := security.Token(18)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_scoring_rule_revisions(id,question_id,question_revision,revision,rule_json,max_points,engine_version,review_status,reviewer_id,source_revision) SELECT $1,$2,$3,COALESCE(max(revision),0)+1,$4,$5,$6,'approved',$7,$8 FROM learning_scoring_rule_revisions WHERE question_id=$2 AND question_revision=$3`, id, r.PathValue("id"), current, nullableJSON(in.Rule), total, platform.ScoringVersion, userID(r.Context()), in.Source)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "approve scoring rule", err)
		return
	}
	respond(w, 201, map[string]string{"id": id})
}
func (s *server) attemptRule(ctx context.Context, a learningAttempt) (*platform.ScoringRule, error) {
	if a.RuleID == "" {
		return nil, nil
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT rule_json FROM learning_scoring_rule_revisions WHERE id=$1 AND question_id=$2 AND question_revision=$3`, a.RuleID, a.Question.ID, a.Question.Revision).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var rule platform.ScoringRule
	err = json.Unmarshal(raw, &rule)
	return &rule, err
}
func writeScoreEvidence(ctx context.Context, tx *sql.Tx, a learningAttempt, answers map[string]string, feedback map[string]any) error {
	confidence, status, engine := "legacy", "legacy", "legacy-v1"
	if a.RuleID != "" {
		confidence = "verified_rule"
		status = "deterministic"
		engine = platform.ScoringVersion
		if feedback["needs_ai"] == true {
			confidence = "unresolved"
			status = "needs_review"
		}
	}
	id := security.Token(18)
	var rule any
	if a.RuleID != "" {
		rule = a.RuleID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO learning_score_evidence(id,attempt_id,question_revision,rule_revision_id,raw_answer_digest,score,max_score,parts,status,confidence,engine_version,scoring_run_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'initial') ON CONFLICT(attempt_id,scoring_run_id) DO NOTHING`, id, a.ID, a.Question.Revision, rule, payloadDigest(answers), feedback["score"], feedback["max_score"], nullableJSON(feedback["parts"]), status, confidence, engine)
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO learning_evidence_outbox(score_evidence_id) SELECT id FROM learning_score_evidence WHERE attempt_id=$1 AND scoring_run_id='initial' ON CONFLICT DO NOTHING`, a.ID)
	}
	return err
}

// Bind the key before checking submitted_at; the same key with any changed payload
// conflicts even after a timeout selected a different, server-authoritative draft.
func bindSubmission(ctx context.Context, tx *sql.Tx, uid, id, key, digest string) error {
	if key == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO learning_submission_requests(user_id,client_request_id,attempt_id,digest) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, uid, key, id, digest)
	if err != nil {
		return err
	}
	var attempt, old string
	err = tx.QueryRowContext(ctx, `SELECT attempt_id,digest FROM learning_submission_requests WHERE user_id=$1 AND client_request_id=$2`, uid, key).Scan(&attempt, &old)
	if err == nil && (attempt != id || old != digest) {
		return errSubmissionConflict
	}
	return err
}

var errSubmissionConflict = errors.New("同一请求标识的内容发生改变，请刷新核对原交卷结果")
