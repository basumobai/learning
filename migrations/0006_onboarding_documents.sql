-- Additive only: retain accounts, encrypted provider keys and all learning data.
CREATE TABLE account_policy (
  singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
  registration_open boolean NOT NULL DEFAULT true,
  default_group_id bigint NOT NULL DEFAULT 1 REFERENCES permission_groups(id)
);
INSERT INTO account_policy(singleton) VALUES(true);
CREATE TABLE registration_attempts (
  key text PRIMARY KEY,
  attempts integer NOT NULL,
  started_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE learning_pages ADD COLUMN text_source text NOT NULL DEFAULT 'native';
ALTER TABLE learning_pages ADD COLUMN ocr_notes jsonb NOT NULL DEFAULT '[]';
ALTER TABLE learning_documents ADD COLUMN source_fingerprint text UNIQUE;
UPDATE learning_pages SET text_source='pending' WHERE length(trim(text_content))<24;
ALTER TABLE training_sessions ADD COLUMN speaking_feedback jsonb;
