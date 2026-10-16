-- Additive migration: existing ELS accounts, vocabulary and FSRS history remain valid.
CREATE TABLE learning_subjects (id text PRIMARY KEY, name text NOT NULL, position integer NOT NULL);
INSERT INTO learning_subjects VALUES ('chinese','语文',1),('math','数学',2),('english','英语',3),('physics','物理',4),('chemistry','化学',5),('biology','生物',6);
CREATE TABLE learning_chapters (
 id text PRIMARY KEY, subject_id text NOT NULL REFERENCES learning_subjects(id),
 parent_id text REFERENCES learning_chapters(id), title text NOT NULL,
 textbook text NOT NULL DEFAULT '', term text NOT NULL DEFAULT '', position integer NOT NULL DEFAULT 0
);
CREATE TABLE learning_documents (
 id text PRIMARY KEY, owner_id text NOT NULL REFERENCES users(id), title text NOT NULL,
 subject_id text NOT NULL REFERENCES learning_subjects(id), textbook text NOT NULL DEFAULT '', term text NOT NULL DEFAULT '',
 question_pdf bytea NOT NULL, answer_pdf bytea NOT NULL, outline jsonb, outline_scanned_pages integer NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_pages (
 document_id text NOT NULL REFERENCES learning_documents(id), kind text NOT NULL CHECK(kind IN ('question','answer')),
 page integer NOT NULL CHECK(page>0), text_content text NOT NULL, image bytea NOT NULL,
 PRIMARY KEY(document_id,kind,page)
);
CREATE TABLE learning_questions (
 id text PRIMARY KEY, subject_id text NOT NULL REFERENCES learning_subjects(id),
 chapter_id text NOT NULL REFERENCES learning_chapters(id), kind text NOT NULL,
 content jsonb NOT NULL, solution jsonb NOT NULL, provenance jsonb NOT NULL,
 document_id text REFERENCES learning_documents(id), source_key text NOT NULL,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published')),
 revision integer NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(document_id,source_key)
);
CREATE INDEX learning_question_filter ON learning_questions(subject_id,kind,chapter_id,status);
CREATE TABLE learning_assets (
 id text PRIMARY KEY, question_id text NOT NULL REFERENCES learning_questions(id) ON DELETE CASCADE,
 image bytea NOT NULL, alt text NOT NULL, source_page integer NOT NULL, bbox jsonb NOT NULL
);
CREATE TABLE learning_ai_tasks (
 task text PRIMARY KEY, prompt text NOT NULL, backend text NOT NULL DEFAULT 'api' CHECK(backend IN ('api','codex')), codex_model text NOT NULL DEFAULT '', model_ids jsonb NOT NULL DEFAULT '[]',
 lead_model_id bigint REFERENCES ai_models(id), revision integer NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_personal_api (
 user_id text PRIMARY KEY REFERENCES users(id), base_url text NOT NULL, api_key bytea NOT NULL,
 model_id text NOT NULL, reviewer_models jsonb NOT NULL DEFAULT '[]', enabled boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_attempts (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), question_id text NOT NULL REFERENCES learning_questions(id),
 question_revision integer NOT NULL, request_id text NOT NULL, answers jsonb, draft_answers jsonb, draft_version bigint NOT NULL DEFAULT 0,
 started_at timestamptz NOT NULL DEFAULT now(), deadline timestamptz,
 submitted_at timestamptz, feedback jsonb, UNIQUE(user_id,request_id)
);
CREATE INDEX learning_attempt_owner ON learning_attempts(user_id,started_at DESC);
CREATE TABLE learning_jobs (
 id text PRIMARY KEY, owner_id text NOT NULL REFERENCES users(id), kind text NOT NULL,
 dedupe_key text NOT NULL, payload jsonb NOT NULL, result jsonb,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','completed','failed')),
 error text NOT NULL DEFAULT '', generation integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_id,kind,dedupe_key)
);
CREATE INDEX learning_job_queue ON learning_jobs(status,created_at);
CREATE TABLE learning_question_chat (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, attempt_id text NOT NULL REFERENCES learning_attempts(id),
 step_id text NOT NULL, question text NOT NULL, answer text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE learning_word_sets (
 id text PRIMARY KEY, sense_id bigint NOT NULL REFERENCES word_senses(id),
 direction text NOT NULL CHECK(direction IN ('en_zh','zh_en')),
 difficulty text NOT NULL CHECK(difficulty IN ('normal','hard','despair')),
 prompt text NOT NULL, choices jsonb NOT NULL, correct_id text NOT NULL,
 approved boolean NOT NULL DEFAULT false, rationale text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX learning_word_ready ON learning_word_sets(direction,difficulty,approved);
CREATE TABLE learning_word_rounds (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), set_id text NOT NULL REFERENCES learning_word_sets(id),
 request_id text NOT NULL, choice_order jsonb NOT NULL, started_at timestamptz NOT NULL,
 deadline timestamptz NOT NULL, picked text, correct boolean, answered_at timestamptz,
 UNIQUE(user_id,request_id)
);
CREATE UNIQUE INDEX learning_one_active_word_round ON learning_word_rounds(user_id) WHERE answered_at IS NULL;
CREATE TABLE learning_cloud_connections (
 id text PRIMARY KEY, name text NOT NULL, base_url text NOT NULL, username text NOT NULL,
 password bytea NOT NULL, enabled boolean NOT NULL DEFAULT true
);
