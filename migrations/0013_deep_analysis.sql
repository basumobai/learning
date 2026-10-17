ALTER TABLE learning_jobs ADD COLUMN ai_token_reserved bigint NOT NULL DEFAULT 0;
ALTER TABLE learning_jobs ADD COLUMN ai_cost_reserved numeric(18,8) NOT NULL DEFAULT 0;
CREATE TABLE learning_analysis_runs (
 id text PRIMARY KEY,document_id text NOT NULL REFERENCES learning_documents(id),
 resource_revision integer NOT NULL,job_id text UNIQUE REFERENCES learning_jobs(id),
 strategy text NOT NULL,config_hash text NOT NULL,status text NOT NULL DEFAULT 'processing',
 total_pages integer NOT NULL DEFAULT 0,processed_pages integer NOT NULL DEFAULT 0,
 metadata jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),completed_at timestamptz
);
CREATE TABLE learning_analysis_steps (
 run_id text NOT NULL REFERENCES learning_analysis_runs(id),step_key text NOT NULL,
 result jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(run_id,step_key)
);
CREATE TABLE learning_content_units (
 id text PRIMARY KEY,run_id text NOT NULL REFERENCES learning_analysis_runs(id),
 document_id text NOT NULL REFERENCES learning_documents(id),unit_key text NOT NULL,
 body jsonb NOT NULL,revision integer NOT NULL DEFAULT 1,
 review_status text NOT NULL DEFAULT 'pending' CHECK(review_status IN ('pending','approved','rejected')),
 question_id text REFERENCES learning_questions(id),created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(run_id,unit_key)
);
CREATE INDEX learning_units_document ON learning_content_units(document_id,run_id);
CREATE TABLE learning_analysis_answers (
 id text PRIMARY KEY,run_id text NOT NULL REFERENCES learning_analysis_runs(id),
 source_key text NOT NULL,body jsonb NOT NULL,UNIQUE(run_id,id)
);
CREATE TABLE learning_resource_reviews (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,document_id text NOT NULL REFERENCES learning_documents(id),
 run_id text REFERENCES learning_analysis_runs(id),unit_id text REFERENCES learning_content_units(id),
 reviewer_id text NOT NULL REFERENCES users(id),action text NOT NULL,revision integer,
 note text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now()
);
