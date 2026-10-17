CREATE TABLE learning_skill_prerequisites (
 skill_id text NOT NULL REFERENCES learning_terms(id),prerequisite_id text NOT NULL REFERENCES learning_terms(id),
 reviewer_id text NOT NULL REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(skill_id<>prerequisite_id),PRIMARY KEY(skill_id,prerequisite_id)
);
CREATE TABLE learning_skill_link_revisions (
 id text PRIMARY KEY,question_id text NOT NULL,question_revision integer NOT NULL,revision integer NOT NULL,
 links jsonb NOT NULL,reviewer_id text NOT NULL REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(question_id,question_revision) REFERENCES learning_question_revisions(question_id,revision),UNIQUE(question_id,question_revision,revision)
);
CREATE TRIGGER immutable_skill_links BEFORE UPDATE OR DELETE ON learning_skill_link_revisions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
ALTER TABLE learning_attempts ADD COLUMN skill_link_revision_id text REFERENCES learning_skill_link_revisions(id);
ALTER TABLE learning_attempts ADD COLUMN transfer_from_id text REFERENCES learning_attempts(id);
CREATE TABLE learning_hint_events (
 id text PRIMARY KEY,attempt_id text NOT NULL REFERENCES learning_attempts(id),user_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,request_id)
);
CREATE TRIGGER immutable_hint_event BEFORE UPDATE OR DELETE ON learning_hint_events FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_skill_evidence (
 id text PRIMARY KEY,user_id text NOT NULL REFERENCES users(id),score_evidence_id text NOT NULL REFERENCES learning_score_evidence(id),
 attempt_id text NOT NULL REFERENCES learning_attempts(id),question_id text NOT NULL,question_revision integer NOT NULL,
 link_revision_id text NOT NULL REFERENCES learning_skill_link_revisions(id),skill_id text NOT NULL REFERENCES learning_terms(id),
 part_id text NOT NULL,role text NOT NULL CHECK(role IN ('primary','auxiliary','prerequisite','application')),
 evidence_kind text NOT NULL CHECK(evidence_kind IN ('independent','repeat','assisted','transfer')),
 correct boolean NOT NULL,ambiguous boolean NOT NULL,points numeric NOT NULL,max_points numeric NOT NULL,
 confidence text NOT NULL,tracker_version text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(score_evidence_id,skill_id,part_id,tracker_version),
 FOREIGN KEY(question_id,question_revision) REFERENCES learning_question_revisions(question_id,revision)
);
CREATE TRIGGER immutable_skill_evidence BEFORE UPDATE OR DELETE ON learning_skill_evidence FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE INDEX learning_skill_evidence_owner ON learning_skill_evidence(user_id,skill_id,created_at);
CREATE TABLE learning_user_skill_states (
 user_id text NOT NULL REFERENCES users(id),skill_id text NOT NULL REFERENCES learning_terms(id),
 independent_count integer NOT NULL,independent_correct integer NOT NULL,repeat_count integer NOT NULL,
 assisted_count integer NOT NULL,transfer_count integer NOT NULL,transfer_correct integer NOT NULL,
 state text NOT NULL,tracker_version text NOT NULL,last_evidence_at timestamptz,recomputed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,skill_id)
);
