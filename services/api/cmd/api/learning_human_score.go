package main

import (
	"context"
	"database/sql"
	"example.com/els/api/internal/security"
	"math"
	"net/http"
	"strings"
)

func (s *server) confirmHumanScore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string             `json:"request_id"`
		Parts   map[string]float64 `json:"parts"`
		Note    string             `json:"note"`
	}
	if decodeLimit(r, &in, 12000) != nil || !requestIDPattern.MatchString(in.Request) || len(strings.TrimSpace(in.Note)) < 10 || len([]rune(in.Note)) > 2000 {
		fail(w, 400, "人工确认需要逐题分值及核验说明")
		return
	}
	var uid string
	if err := s.db.QueryRowContext(r.Context(), `SELECT user_id FROM learning_attempts WHERE id=$1 AND submitted_at IS NOT NULL`, r.PathValue("id")).Scan(&uid); err != nil {
		fail(w, 404, "已交卷练习不存在")
		return
	}
	ctx := context.WithValue(r.Context(), identityKey{}, identity{ID: uid})
	a, err := s.loadAttempt(ctx, r.PathValue("id"))
	if err != nil || a.RuleID == "" {
		fail(w, 409, "旧版评分不能无依据改成高可信证据，请先建立审核规则")
		return
	}
	rule, err := s.attemptRule(ctx, a)
	if err != nil {
		s.dbError(w, "human score rule", err)
		return
	}
	base, err := rule.Score(a.Question.Content, a.Answers)
	if err != nil {
		s.dbError(w, "human score base", err)
		return
	}
	if len(in.Parts) != len(base.Parts) {
		fail(w, 400, "每个小题必须出现一次")
		return
	}
	score := 0.0
	for i, p := range base.Parts {
		v, ok := in.Parts[p.ID]
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > p.Max || (!p.NeedsReview && v != p.Points) {
			fail(w, 400, "分值无效；确定性小题必须遵守原审核规则")
			return
		}
		base.Parts[i].Points = v
		base.Parts[i].Correct = v == p.Max
		base.Parts[i].NeedsReview = false
		if p.NeedsReview {
			base.Parts[i].Basis = "human_confirmed"
		}
		score += v
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.dbError(w, "human confirmation", err)
		return
	}
	defer tx.Rollback()
	var lock string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM learning_attempts WHERE id=$1 FOR UPDATE`, a.ID).Scan(&lock); err != nil {
		s.dbError(w, "human score lock", err)
		return
	}
	digest := payloadDigest([]any{a.ID, in})
	var old, id string
	err = tx.QueryRowContext(ctx, `SELECT digest,score_evidence_id FROM learning_score_confirmations WHERE reviewer_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&old, &id)
	if err == nil {
		if old != digest {
			fail(w, 409, "人工评分请求冲突")
			return
		}
		respond(w, 200, map[string]string{"id": id})
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "human score replay", err)
		return
	}
	var supersedes string
	err = tx.QueryRowContext(ctx, `SELECT e.id FROM learning_score_evidence e WHERE e.attempt_id=$1 AND NOT EXISTS(SELECT 1 FROM learning_score_evidence n WHERE n.supersedes_id=e.id) ORDER BY created_at DESC,id DESC LIMIT 1`, a.ID).Scan(&supersedes)
	if err != nil {
		fail(w, 409, "缺少原评分证据，不能确认")
		return
	}
	id = security.Token(18)
	_, err = tx.ExecContext(ctx, `INSERT INTO learning_score_evidence(id,attempt_id,question_revision,rule_revision_id,raw_answer_digest,score,max_score,parts,status,confidence,engine_version,scoring_run_id,supersedes_id,reviewer_id,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'human_confirmed','human_confirmed','scoring-v1',$9,$10,$11,$12)`, id, a.ID, a.Question.Revision, a.RuleID, payloadDigest(a.Answers), score, base.Max, nullableJSON(base.Parts), "human:"+in.Request, supersedes, userID(r.Context()), in.Note)
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO learning_evidence_outbox(score_evidence_id) VALUES($1)`, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO learning_score_confirmations(reviewer_id,request_id,digest,score_evidence_id) VALUES($1,$2,$3,$4)`, userID(r.Context()), in.Request, digest, id)
	}
	if err == nil {
		err = audit(tx, r, "confirm_score", a.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "persist human score", err)
		return
	}
	respond(w, 201, map[string]string{"id": id})
}
