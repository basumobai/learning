package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
)

func (s *server) engineOperations(w http.ResponseWriter, r *http.Request) {
	var queues, storage, usage json.RawMessage
	var outbox, missing int
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT queue,status,count(*) AS count FROM learning_jobs GROUP BY queue,status ORDER BY queue,status)t`).Scan(&queues)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT jsonb_build_object('database_bytes',pg_database_size(current_database()),'blob_bytes',COALESCE(sum(size),0),'blob_count',count(*)) FROM learning_blobs`).Scan(&storage)
	}
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM learning_evidence_outbox WHERE processed_at IS NULL`).Scan(&outbox)
	}
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM learning_attempts a WHERE NOT EXISTS(SELECT 1 FROM learning_question_revisions q WHERE q.question_id=a.question_id AND q.revision=a.question_revision)`).Scan(&missing)
	}
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT task,status,count(*) AS calls,round(avg(extract(epoch FROM finished_at-started_at))*1000) AS mean_elapsed_ms,sum(CASE WHEN jsonb_typeof(usage->'prompt_tokens')='number' AND usage->>'prompt_tokens' ~ '^[0-9]{1,15}$' THEN (usage->>'prompt_tokens')::bigint END) AS reported_input_tokens,sum(CASE WHEN jsonb_typeof(usage->'completion_tokens')='number' AND usage->>'completion_tokens' ~ '^[0-9]{1,15}$' THEN (usage->>'completion_tokens')::bigint END) AS reported_output_tokens FROM ai_invocations WHERE started_at>=now()-interval '7 days' GROUP BY task,status ORDER BY task,status)t`).Scan(&usage)
	}
	if err != nil {
		s.dbError(w, "engine operations", err)
		return
	}
	flags := map[string]bool{}
	for _, f := range []string{"SCORING", "SKILLS", "MISTAKES", "PLANNING", "TUTOR", "QUALITY_GATE", "EXAMS"} {
		flags[f] = v3Enabled(f)
	}
	respond(w, 200, map[string]any{"flags": flags, "queues": queues, "storage": storage, "pending_score_events": outbox, "missing_question_snapshots": missing, "ai_usage": usage, "note": "Token为供应商报告值；非流式调用没有TTFT证据，费用未与供应商账单核对。Redis不是学习数据真值。"})
}
func (s *server) reviewScoreDetail(w http.ResponseWriter, r *http.Request) {
	var uid string
	if s.db.QueryRowContext(r.Context(), `SELECT user_id FROM learning_attempts WHERE id=$1 AND submitted_at IS NOT NULL`, r.PathValue("id")).Scan(&uid) != nil {
		fail(w, 404, "已交卷练习不存在")
		return
	}
	owned := context.WithValue(r.Context(), identityKey{}, identity{ID: uid})
	a, err := s.loadAttempt(owned, r.PathValue("id"))
	if err != nil {
		s.dbError(w, "score review", err)
		return
	}
	respond(w, 200, a)
}
func learningExport(ctx context.Context, db *sql.DB) (json.RawMessage, error) {
	var raw json.RawMessage
	err := db.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'attempts',COALESCE((SELECT jsonb_agg(CASE WHEN a.submitted_at IS NULL THEN jsonb_build_object('id',a.id,'question_id',a.question_id,'question_revision',a.question_revision,'started_at',a.started_at,'deadline',a.deadline,'draft',a.draft_answers) ELSE to_jsonb(a)-'user_id' END ORDER BY a.started_at,a.id) FROM learning_attempts a WHERE a.user_id=$1),'[]'),
 'submitted_question_snapshots',COALESCE((SELECT jsonb_agg(to_jsonb(q)) FROM learning_question_revisions q WHERE EXISTS(SELECT 1 FROM learning_attempts a WHERE a.user_id=$1 AND a.question_id=q.question_id AND a.question_revision=q.revision AND a.submitted_at IS NOT NULL)),'[]'),
 'scores',COALESCE((SELECT jsonb_agg(to_jsonb(e)) FROM learning_score_evidence e JOIN learning_attempts a ON a.id=e.attempt_id WHERE a.user_id=$1),'[]'),
 'skill_evidence',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id') FROM learning_skill_evidence e WHERE e.user_id=$1),'[]'),
 'skill_states',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id') FROM learning_user_skill_states e WHERE e.user_id=$1),'[]'),
 'mistakes',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id'-'digest') FROM learning_mistake_events e WHERE e.user_id=$1),'[]'),
 'review_items',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id') FROM learning_review_items e WHERE e.user_id=$1),'[]'),
 'review_events',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id'-'digest') FROM learning_review_events e WHERE e.user_id=$1),'[]'),
 'review_undos',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id') FROM learning_review_undos e WHERE e.user_id=$1),'[]'),
 'syntax_attempts',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id'-'digest') FROM learning_syntax_attempts e WHERE e.user_id=$1),'[]'),
 'tutor_steps',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id'-'digest'-'model_snapshot') FROM learning_tutor_steps e WHERE e.user_id=$1),'[]'),
 'plans',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id') FROM learning_study_plans e WHERE e.user_id=$1),'[]'),
 'recordings',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'training_id',training_id,'task_index',task_index,'hash',hash,'mime',mime)) FROM learning_recordings WHERE user_id=$1),'[]'),
 'transcriptions',COALESCE((SELECT jsonb_agg(to_jsonb(e)-'user_id'-'digest') FROM learning_transcriptions e WHERE e.user_id=$1),'[]'))`, userID(ctx)).Scan(&raw)
	return raw, err
}
