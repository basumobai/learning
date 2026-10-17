ALTER TABLE learning_exam_rules ADD COLUMN duration_seconds integer CHECK(duration_seconds BETWEEN 30 AND 21600);
ALTER TABLE learning_exam_rules ADD COLUMN total_points numeric CHECK(total_points>0 AND total_points<=1000);
CREATE TABLE learning_exam_sessions (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),rule_id text NOT NULL REFERENCES learning_exam_rules(id),
 year integer NOT NULL,request_id text NOT NULL,digest text NOT NULL,rule_snapshot jsonb NOT NULL,
 started_at timestamptz NOT NULL DEFAULT now(),deadline timestamptz NOT NULL,total_points numeric NOT NULL,
 submitted_at timestamptz,submission_digest text,UNIQUE(user_id,request_id)
);
CREATE UNIQUE INDEX learning_one_open_exam ON learning_exam_sessions(user_id) WHERE submitted_at IS NULL;
CREATE TABLE learning_exam_items (
 exam_id text NOT NULL REFERENCES learning_exam_sessions(id),position integer NOT NULL,attempt_id text NOT NULL UNIQUE REFERENCES learning_attempts(id),
 section_name text NOT NULL,points numeric NOT NULL,PRIMARY KEY(exam_id,position)
);
CREATE TABLE learning_exam_submissions (
 user_id text NOT NULL REFERENCES users(id),request_id text NOT NULL,exam_id text NOT NULL REFERENCES learning_exam_sessions(id),
 digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,request_id)
);
CREATE TRIGGER immutable_exam_submission BEFORE UPDATE OR DELETE ON learning_exam_submissions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TRIGGER immutable_exam_items BEFORE UPDATE OR DELETE ON learning_exam_items FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE FUNCTION learning_exam_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.rule_id,NEW.rule_snapshot,NEW.year,NEW.deadline,NEW.total_points) IS DISTINCT FROM (OLD.rule_id,OLD.rule_snapshot,OLD.year,OLD.deadline,OLD.total_points) OR (OLD.submitted_at IS NOT NULL AND (NEW.submitted_at,NEW.submission_digest) IS DISTINCT FROM (OLD.submitted_at,OLD.submission_digest)) THEN RAISE EXCEPTION 'exam snapshot is immutable'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER immutable_exam_snapshot BEFORE UPDATE ON learning_exam_sessions FOR EACH ROW EXECUTE FUNCTION learning_exam_guard();
ALTER TABLE ai_models DROP CONSTRAINT ai_models_kind_check;
ALTER TABLE ai_models ADD CONSTRAINT ai_models_kind_check CHECK(kind IN ('chat','tts','asr'));
CREATE TABLE learning_recordings (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),training_id text NOT NULL REFERENCES training_sessions(id),
 task_index integer NOT NULL,mime text NOT NULL,audio bytea NOT NULL,hash text NOT NULL,request_id text NOT NULL,digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_recording BEFORE UPDATE OR DELETE ON learning_recordings FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_transcriptions (
 id text PRIMARY KEY,recording_id text NOT NULL REFERENCES learning_recordings(id),user_id text NOT NULL REFERENCES users(id),
 model_id bigint NOT NULL REFERENCES ai_models(id),model_version text NOT NULL,text text NOT NULL,
 request_id text NOT NULL,digest text NOT NULL,elapsed_ms integer NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_transcription BEFORE UPDATE OR DELETE ON learning_transcriptions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
