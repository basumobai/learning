-- Preserve Phase 0 logs and due dates. FSRS cards are initialized lazily at
-- their first confirmed review; historical counters are not FSRS memory data.
ALTER TABLE review_states ADD COLUMN fsrs_card jsonb;
ALTER TABLE study_sessions ADD COLUMN mode text NOT NULL DEFAULT 'scheduled'
  CHECK (mode IN ('scheduled','practice'));
ALTER TABLE answer_logs ADD COLUMN initial_result text;
UPDATE answer_logs SET initial_result=result;
ALTER TABLE answer_logs ALTER COLUMN initial_result SET NOT NULL;
ALTER TABLE answer_logs ADD COLUMN rating integer CHECK (rating BETWEEN 1 AND 4);
ALTER TABLE answer_logs ADD COLUMN reviewed_at timestamptz;
ALTER TABLE answer_logs ADD COLUMN schedule_applied boolean NOT NULL DEFAULT false;
ALTER TABLE answer_logs ADD COLUMN previous_review jsonb;
ALTER TABLE answer_logs ADD COLUMN fsrs_log jsonb;
ALTER TABLE answer_logs ADD COLUMN next_review_at timestamptz;
ALTER TABLE answer_logs ADD COLUMN review_request_id text;
ALTER TABLE answer_logs ADD COLUMN resolved_result text;
ALTER TABLE answer_logs ADD COLUMN undone_at timestamptz;
-- Existing submissions were already finalized by the old implementation.
UPDATE answer_logs SET rating=CASE WHEN result='correct' AND NOT used_hint THEN 3 ELSE 1 END,
  reviewed_at=created_at, schedule_applied=true WHERE result<>'pending';
ALTER TABLE answer_logs DROP CONSTRAINT answer_logs_question_id_key;
CREATE UNIQUE INDEX answer_active_question_idx ON answer_logs(question_id) WHERE undone_at IS NULL;
CREATE UNIQUE INDEX answer_review_request_idx ON answer_logs(user_id,review_request_id);
CREATE INDEX answer_reviewed_idx ON answer_logs(user_id,reviewed_at DESC) WHERE undone_at IS NULL;
CREATE TABLE undo_requests (
  user_id text NOT NULL REFERENCES users(id),
  request_id text NOT NULL,
  session_id bigint NOT NULL REFERENCES study_sessions(id),
  question_id bigint NOT NULL REFERENCES study_questions(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,request_id)
);
