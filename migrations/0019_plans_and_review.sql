CREATE TABLE learning_selection_decisions (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),request_id text NOT NULL,digest text NOT NULL,
 policy_version text NOT NULL,input jsonb NOT NULL,candidates jsonb NOT NULL,excluded jsonb NOT NULL,selected jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_selection BEFORE UPDATE OR DELETE ON learning_selection_decisions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_study_plans (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),day date NOT NULL,minutes integer NOT NULL CHECK(minutes BETWEEN 5 AND 240),
 version integer NOT NULL DEFAULT 1,items jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,day)
);
CREATE TABLE learning_plan_events (
 id text PRIMARY KEY,plan_id text NOT NULL REFERENCES learning_study_plans(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,digest text NOT NULL,body jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_plan_event BEFORE UPDATE OR DELETE ON learning_plan_events FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_review_items (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),subject_id text NOT NULL REFERENCES learning_subjects(id),
 source_attempt_id text NOT NULL REFERENCES learning_attempts(id),front text NOT NULL,back text NOT NULL,
 fsrs_card jsonb,due timestamptz NOT NULL DEFAULT now(),version integer NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_review_events (
 id text PRIMARY KEY,item_id text NOT NULL REFERENCES learning_review_items(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,digest text NOT NULL,previous_card jsonb,previous_due timestamptz NOT NULL,
 next_card jsonb NOT NULL,fsrs_log jsonb NOT NULL,rating integer NOT NULL,result text NOT NULL,used_hint boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TABLE learning_review_undos (
 event_id text PRIMARY KEY REFERENCES learning_review_events(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_review_event BEFORE UPDATE OR DELETE ON learning_review_events FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TRIGGER immutable_review_undo BEFORE UPDATE OR DELETE ON learning_review_undos FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
