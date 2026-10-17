-- Expand only. Existing attempts remain legacy; no fabricated rule backfill.
CREATE TABLE learning_scoring_rule_revisions (
 id text PRIMARY KEY,question_id text NOT NULL,question_revision integer NOT NULL,
 revision integer NOT NULL,rule_json jsonb NOT NULL,max_points numeric NOT NULL CHECK(max_points>0),
 engine_version text NOT NULL,review_status text NOT NULL CHECK(review_status='approved'),
 reviewer_id text NOT NULL REFERENCES users(id),source_revision text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(question_id,question_revision) REFERENCES learning_question_revisions(question_id,revision),
 UNIQUE(question_id,question_revision,revision)
);
CREATE TRIGGER immutable_scoring_rule BEFORE UPDATE OR DELETE ON learning_scoring_rule_revisions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
ALTER TABLE learning_attempts ADD COLUMN scoring_rule_revision_id text REFERENCES learning_scoring_rule_revisions(id);
ALTER TABLE learning_attempts ADD COLUMN creation_digest text;
ALTER TABLE learning_attempts ADD COLUMN submission_digest text;
CREATE TABLE learning_submission_requests (
 user_id text NOT NULL REFERENCES users(id),client_request_id text NOT NULL,attempt_id text NOT NULL REFERENCES learning_attempts(id),
 digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,client_request_id)
);
CREATE TABLE learning_score_evidence (
 id text PRIMARY KEY,attempt_id text NOT NULL REFERENCES learning_attempts(id),question_revision integer NOT NULL,
 rule_revision_id text REFERENCES learning_scoring_rule_revisions(id),raw_answer_digest text NOT NULL,
 score numeric NOT NULL CHECK(score>=0),max_score numeric NOT NULL CHECK(max_score>=score),parts jsonb NOT NULL,
 status text NOT NULL CHECK(status IN ('deterministic','needs_review','human_confirmed','legacy')),
 confidence text NOT NULL CHECK(confidence IN ('verified_rule','unresolved','human_confirmed','legacy')),
 engine_version text NOT NULL,scoring_run_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id text REFERENCES learning_score_evidence(id),reviewer_id text REFERENCES users(id),note text NOT NULL DEFAULT '',
 UNIQUE(attempt_id,scoring_run_id)
);
CREATE TRIGGER immutable_score_evidence BEFORE UPDATE OR DELETE ON learning_score_evidence FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TRIGGER immutable_submission_request BEFORE UPDATE OR DELETE ON learning_submission_requests FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_evidence_outbox (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,score_evidence_id text NOT NULL UNIQUE REFERENCES learning_score_evidence(id),
 created_at timestamptz NOT NULL DEFAULT now(),processed_at timestamptz
);
CREATE TABLE learning_ai_score_suggestions (
 job_id text PRIMARY KEY REFERENCES learning_jobs(id),attempt_id text NOT NULL REFERENCES learning_attempts(id),
 result jsonb NOT NULL,model_snapshot jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_ai_score_suggestion BEFORE UPDATE OR DELETE ON learning_ai_score_suggestions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE FUNCTION learning_submission_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.submitted_at IS NOT NULL AND (NEW.answers,NEW.submitted_at,NEW.submission_digest,NEW.question_id,NEW.question_revision,NEW.scoring_rule_revision_id) IS DISTINCT FROM (OLD.answers,OLD.submitted_at,OLD.submission_digest,OLD.question_id,OLD.question_revision,OLD.scoring_rule_revision_id) THEN
 RAISE EXCEPTION 'submitted facts are immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_submitted_facts BEFORE UPDATE ON learning_attempts FOR EACH ROW EXECUTE FUNCTION learning_submission_guard();
