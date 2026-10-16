CREATE TABLE learning_processing_config (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),revision integer NOT NULL DEFAULT 1,
 config jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO learning_processing_config(config) VALUES('{"sample_pages":6,"sample_chars":10000,"ocr_pages":2,"ocr_min_chars":60,"light_seconds":90,"light_tokens":24000,"confidence":0.85,"max_cost_usd":null,"deep_pages":800,"deep_tokens":400000,"deep_seconds":1800,"worker_concurrency":2,"retries":3}');
ALTER TABLE learning_documents ADD COLUMN exam_region text NOT NULL DEFAULT '';
ALTER TABLE learning_documents ADD COLUMN exam_year integer;
ALTER TABLE learning_documents ADD COLUMN difficulty text NOT NULL DEFAULT 'unknown';
INSERT INTO learning_terms(id,subject_id,kind,name) VALUES
 ('en-relative','english','knowledge','定语从句'),('en-nonfinite','english','knowledge','非谓语动词'),
 ('en-subjunctive','english','knowledge','虚拟语气'),('en-adverbial','english','knowledge','状语从句'),
 ('en-tense','english','knowledge','时态语态'),('en-reading','english','knowledge','阅读理解'),
 ('en-vocabulary','english','knowledge','词汇'),('physics-induction','physics','knowledge','电磁感应')
 ON CONFLICT DO NOTHING;
