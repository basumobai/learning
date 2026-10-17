SELECT 'users',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM users t
UNION ALL
SELECT 'learning_question_revisions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_question_revisions t
UNION ALL
SELECT 'learning_revision_assets',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_revision_assets t
UNION ALL
SELECT 'learning_attempts',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_attempts t
UNION ALL
SELECT 'answer_logs',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM answer_logs t
UNION ALL
SELECT 'review_states',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM review_states t
UNION ALL
SELECT 'learning_word_rounds',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_word_rounds t
UNION ALL
SELECT 'learning_scoring_rule_revisions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_scoring_rule_revisions t
UNION ALL
SELECT 'learning_score_evidence',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_score_evidence t
UNION ALL
SELECT 'learning_submission_requests',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_submission_requests t
UNION ALL
SELECT 'learning_evidence_outbox',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_evidence_outbox t
UNION ALL
SELECT 'learning_skill_link_revisions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_skill_link_revisions t
UNION ALL
SELECT 'learning_skill_evidence',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_skill_evidence t
UNION ALL
SELECT 'learning_user_skill_states',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_user_skill_states t
UNION ALL
SELECT 'learning_mistake_candidates',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_mistake_candidates t
UNION ALL
SELECT 'learning_mistake_events',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_mistake_events t
UNION ALL
SELECT 'learning_selection_decisions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_selection_decisions t
UNION ALL
SELECT 'learning_study_plans',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_study_plans t
UNION ALL
SELECT 'learning_review_items',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_review_items t
UNION ALL
SELECT 'learning_review_events',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_review_events t
UNION ALL
SELECT 'learning_review_undos',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_review_undos t
UNION ALL
SELECT 'learning_syntax_sessions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_syntax_sessions t
UNION ALL
SELECT 'learning_syntax_attempts',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_syntax_attempts t
UNION ALL
SELECT 'learning_citations',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_citations t
UNION ALL
SELECT 'learning_tutor_steps',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_tutor_steps t
UNION ALL
SELECT 'learning_evaluation_cases',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_evaluation_cases t
UNION ALL
SELECT 'learning_evaluation_results',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_evaluation_results t
UNION ALL
SELECT 'learning_exam_sessions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_exam_sessions t
UNION ALL
SELECT 'learning_exam_items',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_exam_items t
UNION ALL
SELECT 'learning_transcriptions',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM learning_transcriptions t
UNION ALL
SELECT 'learning_documents',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM (SELECT id,owner_id,title,subject_id,revision,stage,published_run_id,md5(question_pdf) AS q,md5(answer_pdf) AS a FROM learning_documents) t
UNION ALL
SELECT 'learning_blobs',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM (SELECT hash,size,mime,encode(sha256(data),'hex') AS verified_hash FROM learning_blobs) t
UNION ALL
SELECT 'learning_assets',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM (SELECT id,question_id,alt,source_page,bbox,encode(sha256(image),'hex') AS image_hash FROM learning_assets) t
UNION ALL
SELECT 'learning_recordings',count(*)::text,COALESCE(sum(hashtextextended(to_jsonb(t)::text,0)::numeric),0)::text FROM (SELECT id,user_id,training_id,task_index,mime,hash,encode(sha256(audio),'hex') AS audio_hash FROM learning_recordings) t
ORDER BY 1;
