package main

import (
	"encoding/json"
	"net/http"
)

func (s *server) exportAccount(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT jsonb_build_object('schema_version',1,'exported_at',now(),
 'profile',(SELECT jsonb_build_object('username',username,'display_name',display_name) FROM users WHERE id=$1),
 'preferences',(SELECT to_jsonb(p)-'user_id' FROM user_preferences p WHERE user_id=$1),
 'reviews',COALESCE((SELECT jsonb_agg(to_jsonb(rs)-'user_id') FROM review_states rs WHERE user_id=$1),'[]'),
 'suspended',COALESCE((SELECT jsonb_agg(sense_id) FROM suspended_senses WHERE user_id=$1),'[]'),
 'vocabulary',COALESCE((SELECT jsonb_agg(jsonb_build_object('sense_id',s.id,'word',w.lemma,'phonetic',s.phonetic,'pos',s.part_of_speech,'chinese',s.meaning_zh)) FROM word_senses s JOIN words w ON w.id=s.word_id),'[]'),
 'answers',COALESCE((SELECT jsonb_agg(to_jsonb(a)-'user_id') FROM answer_logs a WHERE user_id=$1),'[]'),
 'questions',COALESCE((SELECT jsonb_agg(to_jsonb(q)) FROM study_questions q JOIN study_sessions ss ON ss.id=q.session_id WHERE ss.user_id=$1),'[]'),
 'study_sessions',COALESCE((SELECT jsonb_agg(to_jsonb(ss)-'user_id') FROM study_sessions ss WHERE ss.user_id=$1),'[]'),
 'conversations',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'title',c.title,'created_at',c.created_at,'messages',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM ai_messages m WHERE m.conversation_id=c.id),'[]'))) FROM ai_conversations c WHERE c.user_id=$1),'[]'),
 'training',COALESCE((SELECT jsonb_agg(CASE WHEN t.kind='listening' AND t.answers IS NULL THEN jsonb_build_object('id',t.id,'kind',t.kind,'title',t.title,'created_at',t.created_at,'status','unanswered') ELSE to_jsonb(t)-'user_id' END) FROM training_sessions t WHERE t.user_id=$1),'[]'))`, userID(r.Context())).Scan(&raw)
	if err != nil {
		s.dbError(w, "account export", err)
		return
	}
	respond(w, 200, raw)
}
