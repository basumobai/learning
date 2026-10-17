package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
)

const trackerVersion = "skills-v1"

type skillLink struct {
	Skill string `json:"skill_id"`
	Part  string `json:"part_id"`
	Role  string `json:"role"`
}

func (s *server) skillRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/learning/skills", s.learningSkills)
	m.HandleFunc("GET /api/v1/learning/skills/{id}/evidence", s.skillEvidence)
	m.HandleFunc("POST /api/v1/admin/learning/questions/{id}/skill-links", s.approveSkillLinks)
	m.HandleFunc("GET /api/v1/admin/learning/questions/{id}/skill-links", s.skillLinks)
	m.HandleFunc("POST /api/v1/admin/learning/skill-prerequisites", s.skillPrerequisite)
	m.HandleFunc("POST /api/v1/learning/attempts/{id}/hint", s.safeHint)
}
func (s *server) learningSkills(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT k.id,k.name,k.subject_id,k.parent_id,COALESCE(to_jsonb(st)-'user_id'-'skill_id','{}'::jsonb) AS evidence_state FROM learning_terms k LEFT JOIN learning_user_skill_states st ON st.skill_id=k.id AND st.user_id=$1 WHERE k.kind='knowledge' AND ($2='' OR k.subject_id=$2) ORDER BY k.subject_id,k.name LIMIT 3000)t`, userID(r.Context()), r.URL.Query().Get("subject")).Scan(&raw)
	if err != nil {
		s.dbError(w, "skill catalog", err)
		return
	}
	respond(w, 200, map[string]any{"skills": raw, "note": "状态来自可追溯作答证据；证据不足时不计算掌握概率"})
}
func (s *server) skillEvidence(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT * FROM learning_skill_evidence WHERE user_id=$1 AND skill_id=$2 ORDER BY created_at DESC,id LIMIT 200)t`, userID(r.Context()), r.PathValue("id")).Scan(&raw)
	if err != nil {
		s.dbError(w, "skill evidence", err)
		return
	}
	respond(w, 200, map[string]any{"evidence": raw})
}
func (s *server) skillLinks(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY question_revision DESC,revision DESC),'[]') FROM learning_skill_link_revisions t WHERE question_id=$1`, r.PathValue("id")).Scan(&raw)
	if err != nil {
		s.dbError(w, "skill links", err)
		return
	}
	respond(w, 200, map[string]any{"revisions": raw})
}
func (s *server) approveSkillLinks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int         `json:"question_revision"`
		Links    []skillLink `json:"links"`
	}
	if decodeLimit(r, &in, 32000) != nil || len(in.Links) == 0 || len(in.Links) > 100 {
		fail(w, 400, "知识映射无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "link transaction", err)
		return
	}
	defer tx.Rollback()
	var subject string
	var revision int
	var raw []byte
	err = tx.QueryRowContext(r.Context(), `SELECT subject_id,revision,content FROM learning_questions WHERE id=$1 AND status='published' FOR UPDATE`, r.PathValue("id")).Scan(&subject, &revision, &raw)
	if err != nil {
		fail(w, 404, "题目不存在")
		return
	}
	if revision != in.Revision {
		fail(w, 409, "题目已修订")
		return
	}
	var c platform.Content
	json.Unmarshal(raw, &c)
	seen := map[string]bool{}
	for _, l := range in.Links {
		valid := false
		for _, p := range c.Parts {
			valid = valid || p.ID == l.Part
		}
		key := l.Skill + ":" + l.Part
		if !valid || seen[key] || (l.Role != "primary" && l.Role != "auxiliary" && l.Role != "prerequisite" && l.Role != "application") {
			fail(w, 400, "小题、角色或重复映射无效")
			return
		}
		seen[key] = true
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_terms WHERE id=$1 AND subject_id=$2 AND kind='knowledge')`, l.Skill, subject).Scan(&valid) != nil || !valid {
			fail(w, 400, "知识点学科不匹配")
			return
		}
	}
	id := security.Token(18)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_skill_link_revisions(id,question_id,question_revision,revision,links,reviewer_id) SELECT $1,$2,$3,COALESCE(max(revision),0)+1,$4,$5 FROM learning_skill_link_revisions WHERE question_id=$2 AND question_revision=$3`, id, r.PathValue("id"), revision, nullableJSON(in.Links), userID(r.Context()))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "approve knowledge mapping", err)
		return
	}
	respond(w, 201, map[string]string{"id": id})
}
func (s *server) skillPrerequisite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Skill        string `json:"skill_id"`
		Prerequisite string `json:"prerequisite_id"`
	}
	if decode(r, &in) != nil || in.Skill == in.Prerequisite {
		fail(w, 400, "前置关系无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "prerequisite", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(19283749)`); err != nil {
		s.dbError(w, "prerequisite lock", err)
		return
	}
	var valid, cycle bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_terms a JOIN learning_terms b ON b.id=$2 AND b.subject_id=a.subject_id WHERE a.id=$1 AND a.kind='knowledge' AND b.kind='knowledge')`, in.Skill, in.Prerequisite).Scan(&valid)
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `WITH RECURSIVE p(id) AS (VALUES($1::text) UNION SELECT r.prerequisite_id FROM learning_skill_prerequisites r JOIN p ON r.skill_id=p.id) SELECT EXISTS(SELECT 1 FROM p WHERE id=$2)`, in.Prerequisite, in.Skill).Scan(&cycle)
	}
	if err != nil {
		s.dbError(w, "prerequisite validation", err)
		return
	}
	if !valid || cycle {
		fail(w, 400, "前置知识学科不匹配或构成循环")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_skill_prerequisites(skill_id,prerequisite_id,reviewer_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, in.Skill, in.Prerequisite, userID(r.Context()))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save prerequisite", err)
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (s *server) safeHint(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string `json:"request_id"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "提示请求无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "hint", err)
		return
	}
	defer tx.Rollback()
	var submitted *string
	var expired bool
	err = tx.QueryRowContext(r.Context(), `SELECT submitted_at::text,deadline IS NOT NULL AND deadline<=clock_timestamp() FROM learning_attempts WHERE id=$1 AND user_id=$2 FOR UPDATE`, r.PathValue("id"), userID(r.Context())).Scan(&submitted, &expired)
	if err != nil {
		fail(w, 404, "练习不存在")
		return
	}
	if submitted != nil || expired {
		fail(w, 409, "练习已结束")
		return
	}
	var id string
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_hint_events(id,attempt_id,user_id,request_id) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,request_id) DO NOTHING`, security.Token(18), r.PathValue("id"), userID(r.Context()), in.Request)
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT attempt_id FROM learning_hint_events WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id)
	}
	if err == nil && id != r.PathValue("id") {
		fail(w, 409, "提示请求标识冲突")
		return
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save hint", err)
		return
	}
	respond(w, 200, map[string]string{"hint": "先圈出题目明确给出的条件，再写出要解决的问题；将条件与所学定义逐项对应。可以先完成有把握的小题。", "source": "platform_strategy", "effect": "本次作答会记为使用提示"})
}

// The outbox is a replayable DB worklist, not a second task queue. The existing
// worker drains it; submission may drain its own event in the same transaction.
func applySkillEvidence(ctx context.Context, tx *sql.Tx, scoreID string) error {
	var a learningAttempt
	var uid, linkID, confidence string
	var linksRaw, partsRaw []byte
	var assisted, repeated, transfer bool
	err := tx.QueryRowContext(ctx, `SELECT a.id,a.user_id,a.question_id,a.question_revision,COALESCE(a.skill_link_revision_id,''),e.parts,e.confidence,
 EXISTS(SELECT 1 FROM learning_hint_events h WHERE h.attempt_id=a.id),
 EXISTS(SELECT 1 FROM learning_attempts prior WHERE prior.user_id=a.user_id AND prior.question_id=a.question_id AND prior.id<>a.id AND prior.submitted_at IS NOT NULL AND (prior.submitted_at,prior.id)<(a.submitted_at,a.id)),a.transfer_from_id IS NOT NULL
 FROM learning_score_evidence e JOIN learning_attempts a ON a.id=e.attempt_id WHERE e.id=$1`, scoreID).Scan(&a.ID, &uid, &a.Question.ID, &a.Question.Revision, &linkID, &partsRaw, &confidence, &assisted, &repeated, &transfer)
	if err != nil {
		return err
	}
	if linkID == "" || (confidence != "verified_rule" && confidence != "human_confirmed") {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,71))`, uid); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT links FROM learning_skill_link_revisions WHERE id=$1`, linkID).Scan(&linksRaw); err != nil {
		return err
	}
	var links []skillLink
	var parts []platform.PartScore
	if json.Unmarshal(linksRaw, &links) != nil || json.Unmarshal(partsRaw, &parts) != nil {
		return fmt.Errorf("invalid evidence JSON")
	}
	kind := "independent"
	if transfer {
		kind = "transfer"
	}
	if repeated {
		kind = "repeat"
	}
	if assisted {
		kind = "assisted"
	}
	for _, l := range links {
		for _, p := range parts {
			if l.Part != p.ID || p.NeedsReview {
				continue
			}
			n := 0
			for _, other := range links {
				if other.Part == p.ID && (other.Role == "primary" || other.Role == "application") {
					n++
				}
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO learning_skill_evidence(id,user_id,score_evidence_id,attempt_id,question_id,question_revision,link_revision_id,skill_id,part_id,role,evidence_kind,correct,ambiguous,points,max_points,confidence,tracker_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT DO NOTHING`, security.Token(18), uid, scoreID, a.ID, a.Question.ID, a.Question.Revision, linkID, l.Skill, l.Part, l.Role, kind, p.Correct, n != 1, p.Points, p.Max, confidence, trackerVersion)
			if err != nil {
				return err
			}
		}
	}
	return rebuildSkillStates(ctx, tx, uid)
}
func rebuildSkillStates(ctx context.Context, tx *sql.Tx, uid string) error {
	_, err := tx.ExecContext(ctx, `UPDATE learning_user_skill_states SET independent_count=0,independent_correct=0,repeat_count=0,assisted_count=0,transfer_count=0,transfer_correct=0,state='limited',recomputed_at=now() WHERE user_id=$1`, uid)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO learning_user_skill_states(user_id,skill_id,independent_count,independent_correct,repeat_count,assisted_count,transfer_count,transfer_correct,state,tracker_version,last_evidence_at)
 SELECT user_id,skill_id,count(DISTINCT question_id) FILTER(WHERE evidence_kind='independent' AND NOT ambiguous AND role IN ('primary','application')),
 count(DISTINCT question_id) FILTER(WHERE evidence_kind='independent' AND correct AND NOT ambiguous AND role IN ('primary','application')),
 count(*) FILTER(WHERE evidence_kind='repeat'),count(*) FILTER(WHERE evidence_kind='assisted'),
 count(DISTINCT question_id) FILTER(WHERE evidence_kind='transfer' AND NOT ambiguous AND role IN ('primary','application')),
 count(DISTINCT question_id) FILTER(WHERE evidence_kind='transfer' AND correct AND NOT ambiguous AND role IN ('primary','application')),
 'limited',$2,max(created_at) FROM learning_skill_evidence se WHERE user_id=$1 AND NOT EXISTS(
 SELECT 1 FROM learning_mistake_events me WHERE me.attempt_id=se.attempt_id AND me.type_id='input_error' AND me.action IN ('confirm','correct') AND me.version=(SELECT max(version) FROM learning_mistake_events WHERE attempt_id=se.attempt_id AND action<>'understood')) GROUP BY user_id,skill_id
 ON CONFLICT(user_id,skill_id) DO UPDATE SET independent_count=EXCLUDED.independent_count,independent_correct=EXCLUDED.independent_correct,repeat_count=EXCLUDED.repeat_count,assisted_count=EXCLUDED.assisted_count,transfer_count=EXCLUDED.transfer_count,transfer_correct=EXCLUDED.transfer_correct,tracker_version=EXCLUDED.tracker_version,last_evidence_at=EXCLUDED.last_evidence_at,recomputed_at=now()`, uid, trackerVersion)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE learning_user_skill_states SET state=CASE WHEN independent_count+transfer_count<3 THEN 'limited' WHEN independent_correct>=4 AND transfer_correct>=2 THEN 'basic' WHEN independent_correct+transfer_correct=0 THEN 'learning' ELSE 'consolidating' END WHERE user_id=$1`, uid)
	}
	return err
}
func (s *server) processEvidenceOutbox(ctx context.Context) error {
	if !v3Enabled("SKILLS") {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var score string
	err = tx.QueryRowContext(ctx, `SELECT id,score_evidence_id FROM learning_evidence_outbox WHERE processed_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &score)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err = applySkillEvidence(ctx, tx, score); err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE learning_evidence_outbox SET processed_at=now() WHERE id=$1`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
