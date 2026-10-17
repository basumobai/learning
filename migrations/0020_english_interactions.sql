CREATE TABLE learning_syntax_sessions (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),request_id text NOT NULL,digest text NOT NULL,
 source_text text NOT NULL,source_attempt_id text REFERENCES learning_attempts(id),parent_id text REFERENCES learning_syntax_sessions(id),
 analysis_training_id text REFERENCES training_sessions(id),ai_generated boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TABLE learning_syntax_attempts (
 id text PRIMARY KEY,session_id text NOT NULL REFERENCES learning_syntax_sessions(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,digest text NOT NULL,marks jsonb NOT NULL,translation text NOT NULL,
 skipped boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_syntax_attempt BEFORE UPDATE OR DELETE ON learning_syntax_attempts FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_listening_reflections (
 id text PRIMARY KEY,training_id text NOT NULL REFERENCES training_sessions(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,digest text NOT NULL,reason text NOT NULL CHECK(reason IN ('not_understood','heard_forgot')),
 notes text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_listening_reflection BEFORE UPDATE OR DELETE ON learning_listening_reflections FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
