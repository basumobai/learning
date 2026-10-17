CREATE TABLE learning_mistake_candidates (
 id text PRIMARY KEY,attempt_id text NOT NULL REFERENCES learning_attempts(id),job_id text NOT NULL REFERENCES learning_jobs(id),
 part_id text NOT NULL,type_id text NOT NULL,question_quote text NOT NULL,answer_quote text NOT NULL,
 uncertainty text NOT NULL,model_snapshot jsonb NOT NULL,prompt_version text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(job_id,part_id,type_id)
);
CREATE TRIGGER immutable_mistake_candidate BEFORE UPDATE OR DELETE ON learning_mistake_candidates FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_mistake_events (
 id text PRIMARY KEY,attempt_id text NOT NULL REFERENCES learning_attempts(id),user_id text NOT NULL REFERENCES users(id),
 candidate_id text REFERENCES learning_mistake_candidates(id),version integer NOT NULL,
 action text NOT NULL CHECK(action IN ('confirm','correct','unsure','understood')),
 type_id text NOT NULL,note text NOT NULL,request_id text NOT NULL,digest text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(attempt_id,version),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_mistake_event BEFORE UPDATE OR DELETE ON learning_mistake_events FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE INDEX learning_mistake_owner ON learning_mistake_events(user_id,attempt_id,version DESC);
