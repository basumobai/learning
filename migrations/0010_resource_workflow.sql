-- Existing document IDs and published questions remain valid.
ALTER TABLE learning_documents ALTER COLUMN question_pdf DROP NOT NULL;
ALTER TABLE learning_documents ALTER COLUMN answer_pdf DROP NOT NULL;
ALTER TABLE learning_documents ADD COLUMN stage text NOT NULL DEFAULT 'A'
 CHECK(stage IN ('A','B','C','library'));
ALTER TABLE learning_documents ADD COLUMN revision integer NOT NULL DEFAULT 1;
ALTER TABLE learning_documents ADD COLUMN resource_key text UNIQUE;
ALTER TABLE learning_documents ADD COLUMN resource_type text NOT NULL DEFAULT 'other';
ALTER TABLE learning_documents ADD COLUMN textbook_alignment text NOT NULL DEFAULT 'unknown'
 CHECK(textbook_alignment IN ('yes','no','unknown'));
ALTER TABLE learning_documents ADD COLUMN classification_status text NOT NULL DEFAULT 'pending'
 CHECK(classification_status IN ('pending','proposed','needs_confirmation','confirmed'));
ALTER TABLE learning_documents ADD COLUMN classification_revision integer NOT NULL DEFAULT 0;
ALTER TABLE learning_documents ADD COLUMN classification jsonb;
ALTER TABLE learning_documents ADD COLUMN published_run_id text;
ALTER TABLE learning_documents ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE learning_documents d SET stage=CASE
 WHEN EXISTS(SELECT 1 FROM learning_questions q WHERE q.document_id=d.id AND q.status='draft') THEN 'C'
 WHEN EXISTS(SELECT 1 FROM learning_questions q WHERE q.document_id=d.id AND q.status='published') THEN 'library'
 WHEN scope_confirmed THEN 'B' ELSE 'A' END,
 classification_status=CASE WHEN scope_confirmed THEN 'confirmed' ELSE 'pending' END;
CREATE INDEX learning_resources_stage ON learning_documents(stage,created_at DESC,id);
CREATE INDEX learning_resources_type ON learning_documents(resource_type,subject_id);

CREATE TABLE learning_blobs (
 hash text PRIMARY KEY CHECK(hash ~ '^[a-f0-9]{64}$'), data bytea NOT NULL,
 size bigint NOT NULL CHECK(size>0),mime text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(octet_length(data)=size),CHECK(encode(sha256(data),'hex')=hash)
);
CREATE FUNCTION learning_immutable_blob() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'original resource blobs are immutable'; END $$;
CREATE TRIGGER immutable_original BEFORE UPDATE OR DELETE ON learning_blobs
 FOR EACH ROW EXECUTE FUNCTION learning_immutable_blob();
CREATE TABLE learning_resource_files (
 document_id text NOT NULL REFERENCES learning_documents(id),
 blob_hash text NOT NULL REFERENCES learning_blobs(hash),role text NOT NULL
 CHECK(role IN ('primary','answer','attachment')),
 PRIMARY KEY(document_id,role,blob_hash)
);
CREATE UNIQUE INDEX learning_resource_main_file ON learning_resource_files(document_id,role)
 WHERE role IN ('primary','answer');
CREATE TABLE learning_resource_sources (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 document_id text NOT NULL REFERENCES learning_documents(id),kind text NOT NULL,
 source_key text NOT NULL,original_name text NOT NULL,source_version text NOT NULL DEFAULT '',
 metadata jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(kind,source_key,source_version)
);
CREATE INDEX learning_source_resource ON learning_resource_sources(document_id);
CREATE TABLE learning_terms (
 id text PRIMARY KEY,subject_id text REFERENCES learning_subjects(id),
 kind text NOT NULL CHECK(kind IN ('topic','knowledge','tag')),name text NOT NULL,
 parent_id text REFERENCES learning_terms(id),UNIQUE(subject_id,kind,name)
);
CREATE TABLE learning_resource_terms (
 document_id text NOT NULL REFERENCES learning_documents(id),term_id text NOT NULL REFERENCES learning_terms(id),
 source text NOT NULL DEFAULT 'human',PRIMARY KEY(document_id,term_id)
);
CREATE TABLE learning_resource_chapters (
 document_id text NOT NULL REFERENCES learning_documents(id),chapter_id text NOT NULL REFERENCES learning_chapters(id),
 PRIMARY KEY(document_id,chapter_id)
);
CREATE TABLE learning_resource_subjects (
 document_id text NOT NULL REFERENCES learning_documents(id),subject_id text NOT NULL REFERENCES learning_subjects(id),
 PRIMARY KEY(document_id,subject_id)
);
CREATE TABLE learning_classifications (
 id text PRIMARY KEY,document_id text NOT NULL REFERENCES learning_documents(id),
 resource_revision integer NOT NULL,cache_key text NOT NULL,result jsonb NOT NULL,
 source text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(document_id,cache_key)
);

ALTER TABLE learning_jobs DROP CONSTRAINT learning_jobs_status_check;
ALTER TABLE learning_jobs ADD CONSTRAINT learning_jobs_status_check CHECK(status IN
 ('queued','running','retry_wait','pause_requested','paused','cancel_requested','cancelled','completed','failed'));
ALTER TABLE learning_jobs ADD COLUMN queue text NOT NULL DEFAULT 'interactive';
ALTER TABLE learning_jobs ADD COLUMN resource_id text REFERENCES learning_documents(id);
ALTER TABLE learning_jobs ADD COLUMN resource_revision integer;
ALTER TABLE learning_jobs ADD COLUMN lease_owner text;
ALTER TABLE learning_jobs ADD COLUMN lease_until timestamptz;
ALTER TABLE learning_jobs ADD COLUMN heartbeat_at timestamptz;
ALTER TABLE learning_jobs ADD COLUMN attempts integer NOT NULL DEFAULT 0;
ALTER TABLE learning_jobs ADD COLUMN max_attempts integer NOT NULL DEFAULT 3 CHECK(max_attempts BETWEEN 1 AND 8);
ALTER TABLE learning_jobs ADD COLUMN retry_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE learning_jobs ADD COLUMN pause_requested boolean NOT NULL DEFAULT false;
ALTER TABLE learning_jobs ADD COLUMN cancel_requested boolean NOT NULL DEFAULT false;
ALTER TABLE learning_jobs ADD COLUMN checkpoint jsonb NOT NULL DEFAULT '{}';
ALTER TABLE learning_jobs ADD COLUMN progress jsonb NOT NULL DEFAULT '{}';
ALTER TABLE learning_jobs ADD COLUMN control_revision integer NOT NULL DEFAULT 1;
UPDATE learning_jobs j SET resource_id=d.id,resource_revision=d.revision
 FROM learning_documents d WHERE j.payload->>'document_id'=d.id;
UPDATE learning_jobs SET queue='resource' WHERE kind IN ('identify','outline','ocr','extract','word_options');
CREATE UNIQUE INDEX learning_resource_job_dedupe ON learning_jobs(resource_id,kind,dedupe_key)
 WHERE resource_id IS NOT NULL AND kind LIKE 'resource_%';
CREATE INDEX learning_jobs_due ON learning_jobs(queue,status,retry_at,created_at);
CREATE TABLE learning_job_signals (
 job_id text PRIMARY KEY REFERENCES learning_jobs(id),generation integer NOT NULL,
 delivered_at timestamptz,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION learning_job_enqueue_signal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('identify','outline','ocr','extract','word_options') OR NEW.kind LIKE 'resource_%' OR NEW.kind LIKE 'share_%' THEN
  NEW.queue := CASE WHEN NEW.kind LIKE 'share_%' THEN 'download' ELSE 'resource' END;
 END IF;
 IF NEW.resource_id IS NULL AND NEW.payload->>'document_id' IS NOT NULL THEN
  SELECT id,revision INTO NEW.resource_id,NEW.resource_revision FROM learning_documents WHERE id=NEW.payload->>'document_id';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER learning_job_defaults BEFORE INSERT ON learning_jobs FOR EACH ROW EXECUTE FUNCTION learning_job_enqueue_signal();
CREATE FUNCTION learning_job_publish_signal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('queued','retry_wait') THEN
  INSERT INTO learning_job_signals(job_id,generation) VALUES(NEW.id,NEW.generation)
   ON CONFLICT(job_id) DO UPDATE SET generation=EXCLUDED.generation,delivered_at=NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER learning_job_outbox AFTER INSERT OR UPDATE OF status ON learning_jobs
 FOR EACH ROW EXECUTE FUNCTION learning_job_publish_signal();
INSERT INTO learning_job_signals(job_id,generation) SELECT id,generation FROM learning_jobs WHERE status IN ('queued','retry_wait');
CREATE TABLE learning_worker_heartbeats (id text PRIMARY KEY,updated_at timestamptz NOT NULL DEFAULT now());
