ALTER TABLE learning_questions ALTER COLUMN chapter_id DROP NOT NULL;
CREATE TABLE learning_question_revisions (
 question_id text NOT NULL REFERENCES learning_questions(id),revision integer NOT NULL,
 subject_id text NOT NULL,chapter_id text,kind text NOT NULL,content jsonb NOT NULL,solution jsonb NOT NULL,
 provenance jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(question_id,revision)
);
INSERT INTO learning_question_revisions(question_id,revision,subject_id,chapter_id,kind,content,solution,provenance)
 SELECT id,revision,subject_id,chapter_id,kind,content,solution,provenance FROM learning_questions;
CREATE TABLE learning_revision_assets (
 question_id text NOT NULL,revision integer NOT NULL,asset_id text NOT NULL REFERENCES learning_assets(id),
 PRIMARY KEY(question_id,revision,asset_id),FOREIGN KEY(question_id,revision) REFERENCES learning_question_revisions(question_id,revision)
);
INSERT INTO learning_revision_assets SELECT a.question_id,q.revision,a.id FROM learning_assets a JOIN learning_questions q ON q.id=a.question_id;
CREATE FUNCTION learning_question_revision_before() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.subject_id,NEW.chapter_id,NEW.kind,NEW.content,NEW.solution,NEW.provenance) IS DISTINCT FROM (OLD.subject_id,OLD.chapter_id,OLD.kind,OLD.content,OLD.solution,OLD.provenance) AND NEW.revision<=OLD.revision THEN
  NEW.revision := OLD.revision+1;
 END IF;
 IF NEW.revision<OLD.revision OR NEW.revision>OLD.revision+1 THEN RAISE EXCEPTION 'invalid question revision'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER learning_question_version_guard BEFORE UPDATE ON learning_questions FOR EACH ROW EXECUTE FUNCTION learning_question_revision_before();
CREATE FUNCTION learning_question_revision_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO learning_question_revisions(question_id,revision,subject_id,chapter_id,kind,content,solution,provenance)
 VALUES(NEW.id,NEW.revision,NEW.subject_id,NEW.chapter_id,NEW.kind,NEW.content,NEW.solution,NEW.provenance) ON CONFLICT DO NOTHING;
 IF TG_OP='UPDATE' AND NEW.revision<>OLD.revision THEN
  INSERT INTO learning_revision_assets(question_id,revision,asset_id) SELECT NEW.id,NEW.revision,asset_id FROM learning_revision_assets WHERE question_id=OLD.id AND revision=OLD.revision ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER learning_question_snapshot AFTER INSERT OR UPDATE ON learning_questions FOR EACH ROW EXECUTE FUNCTION learning_question_revision_after();
CREATE FUNCTION learning_asset_revision_after() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO learning_revision_assets(question_id,revision,asset_id) SELECT NEW.question_id,revision,NEW.id FROM learning_questions WHERE id=NEW.question_id ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER learning_asset_snapshot AFTER INSERT ON learning_assets FOR EACH ROW EXECUTE FUNCTION learning_asset_revision_after();
CREATE TRIGGER immutable_question_snapshot BEFORE UPDATE OR DELETE ON learning_question_revisions FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TRIGGER immutable_question_asset BEFORE UPDATE OR DELETE ON learning_assets FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_question_terms (
 question_id text NOT NULL REFERENCES learning_questions(id),term_id text NOT NULL REFERENCES learning_terms(id),PRIMARY KEY(question_id,term_id)
);
CREATE TABLE learning_question_chapters (
 question_id text NOT NULL REFERENCES learning_questions(id),chapter_id text NOT NULL REFERENCES learning_chapters(id),PRIMARY KEY(question_id,chapter_id)
);
ALTER TABLE learning_terms ADD COLUMN revision integer NOT NULL DEFAULT 1;
ALTER TABLE learning_chapters ADD COLUMN revision integer NOT NULL DEFAULT 1;
CREATE TABLE learning_exam_rules (
 id text PRIMARY KEY,family text NOT NULL,revision integer NOT NULL,subject_id text NOT NULL REFERENCES learning_subjects(id),
 region text NOT NULL,category text NOT NULL,paper text NOT NULL,year_from integer NOT NULL,year_to integer NOT NULL,
 sections jsonb NOT NULL,sources jsonb NOT NULL,notes text NOT NULL,verified boolean NOT NULL DEFAULT false,
 reviewer_id text REFERENCES users(id),created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(year_from>=1977 AND year_to>=year_from AND year_to<=2100),UNIQUE(family,revision)
);
CREATE TRIGGER immutable_exam_rule BEFORE UPDATE OR DELETE ON learning_exam_rules FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_unit_publications (
 unit_id text NOT NULL REFERENCES learning_content_units(id),revision integer NOT NULL,
 document_id text NOT NULL REFERENCES learning_documents(id),run_id text NOT NULL REFERENCES learning_analysis_runs(id),
 body jsonb NOT NULL,question_id text REFERENCES learning_questions(id),active boolean NOT NULL DEFAULT true,
 reviewer_id text NOT NULL REFERENCES users(id),published_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(unit_id,revision)
);
CREATE TABLE learning_publication_assets (
 id text PRIMARY KEY,unit_id text NOT NULL,revision integer NOT NULL,blob_hash text NOT NULL REFERENCES learning_blobs(hash),
 alt text NOT NULL,source_page integer NOT NULL,bbox jsonb NOT NULL,
 FOREIGN KEY(unit_id,revision) REFERENCES learning_unit_publications(unit_id,revision)
);
