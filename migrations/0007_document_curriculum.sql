-- Unknown PDF metadata is staged for confirmation instead of guessed into a subject.
ALTER TABLE learning_documents ALTER COLUMN subject_id DROP NOT NULL;
ALTER TABLE learning_documents ADD COLUMN recognition jsonb;
ALTER TABLE learning_documents ADD COLUMN scope_confirmed boolean NOT NULL DEFAULT true;
ALTER TABLE learning_documents ADD COLUMN edition text NOT NULL DEFAULT '';
ALTER TABLE learning_documents ADD COLUMN volume text NOT NULL DEFAULT '';
ALTER TABLE learning_chapters ADD COLUMN edition text NOT NULL DEFAULT '';
ALTER TABLE learning_chapters ADD COLUMN volume text NOT NULL DEFAULT '';
ALTER TABLE learning_chapters ADD COLUMN curriculum_chapter text NOT NULL DEFAULT '';
CREATE INDEX learning_chapter_curriculum ON learning_chapters(subject_id,edition,volume);
