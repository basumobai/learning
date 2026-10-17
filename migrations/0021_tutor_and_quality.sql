-- Search only approved non-question publication text; original PDFs stay private.
ALTER TABLE learning_unit_publications ADD COLUMN search_text tsvector GENERATED ALWAYS AS (to_tsvector('simple',COALESCE(body->>'title','')||' '||COALESCE(body->'content'->>'text',''))) STORED;
CREATE INDEX learning_publication_search ON learning_unit_publications USING gin(search_text);
CREATE FUNCTION learning_publication_body_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.unit_id,NEW.revision,NEW.document_id,NEW.run_id,NEW.body,NEW.question_id,NEW.reviewer_id) IS DISTINCT FROM (OLD.unit_id,OLD.revision,OLD.document_id,OLD.run_id,OLD.body,OLD.question_id,OLD.reviewer_id) THEN RAISE EXCEPTION 'published body is immutable'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER immutable_publication_body BEFORE UPDATE ON learning_unit_publications FOR EACH ROW EXECUTE FUNCTION learning_publication_body_guard();
CREATE TABLE learning_citations (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),unit_id text NOT NULL,publication_revision integer NOT NULL,
 document_id text NOT NULL REFERENCES learning_documents(id),document_revision_id text NOT NULL,page integer NOT NULL CHECK(page>0),
 quote text NOT NULL,title text NOT NULL,review_status text NOT NULL CHECK(review_status='approved'),created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(unit_id,publication_revision) REFERENCES learning_unit_publications(unit_id,revision)
);
CREATE TRIGGER immutable_citation BEFORE UPDATE OR DELETE ON learning_citations FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_tutor_states (
 attempt_id text PRIMARY KEY REFERENCES learning_attempts(id),state text NOT NULL CHECK(state IN ('diagnosis','remediation','transfer_check','completed')),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_tutor_steps (
 id text PRIMARY KEY,attempt_id text NOT NULL REFERENCES learning_attempts(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,digest text NOT NULL,mode text NOT NULL,question text NOT NULL,answer text NOT NULL,citation_ids jsonb NOT NULL,
 context_step_ids jsonb NOT NULL,model_snapshot jsonb NOT NULL,prompt_hash text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_tutor_step BEFORE UPDATE OR DELETE ON learning_tutor_steps FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_score_confirmations (
 reviewer_id text NOT NULL REFERENCES users(id),request_id text NOT NULL,digest text NOT NULL,score_evidence_id text NOT NULL REFERENCES learning_score_evidence(id),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(reviewer_id,request_id)
);
CREATE TRIGGER immutable_score_confirmation BEFORE UPDATE OR DELETE ON learning_score_confirmations FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE UNIQUE INDEX learning_single_score_successor ON learning_score_evidence(supersedes_id);
CREATE TABLE learning_evaluation_cases (
 id text PRIMARY KEY,subject_id text NOT NULL REFERENCES learning_subjects(id),task text NOT NULL,input text NOT NULL,
 expected_terms jsonb NOT NULL,rights_note text NOT NULL,reviewer_id text NOT NULL REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_evaluation_case BEFORE UPDATE OR DELETE ON learning_evaluation_cases FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_evaluation_runs (
 id text PRIMARY KEY,case_id text NOT NULL REFERENCES learning_evaluation_cases(id),model_id bigint NOT NULL REFERENCES ai_models(id),
 model_version text NOT NULL,prompt text NOT NULL,prompt_hash text NOT NULL,request_id text NOT NULL,reviewer_id text NOT NULL REFERENCES users(id),
 digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(reviewer_id,request_id)
);
CREATE TABLE learning_evaluation_results (
 run_id text PRIMARY KEY REFERENCES learning_evaluation_runs(id),answer text NOT NULL,automatic_checks jsonb NOT NULL,
 elapsed_ms integer NOT NULL,execution text NOT NULL CHECK(execution='provider_request'),created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_evaluation_result BEFORE UPDATE OR DELETE ON learning_evaluation_results FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_evaluation_reviews (
 run_id text PRIMARY KEY REFERENCES learning_evaluation_runs(id),decision text NOT NULL CHECK(decision IN ('approved','rejected')),
 note text NOT NULL,reviewer_id text NOT NULL REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_evaluation_review BEFORE UPDATE OR DELETE ON learning_evaluation_reviews FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TRIGGER immutable_evaluation_run BEFORE UPDATE OR DELETE ON learning_evaluation_runs FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
