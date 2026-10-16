-- Discovery is separate from administrator selection and group authorization.
CREATE TABLE ai_model_catalog (
 provider_id text NOT NULL REFERENCES ai_providers(id), model_id text NOT NULL,
 name text NOT NULL, context_length bigint, input_modalities jsonb,
 output_modalities jsonb, pricing jsonb, price_unit text,
 supported_parameters jsonb, source_url text NOT NULL,
 fetched_at timestamptz NOT NULL DEFAULT now(), available boolean NOT NULL DEFAULT true,
 PRIMARY KEY(provider_id,model_id)
);
ALTER TABLE ai_providers ADD COLUMN model_sync_at timestamptz,
 ADD COLUMN model_sync_error text NOT NULL DEFAULT '';
ALTER TABLE learning_ai_tasks ADD COLUMN routing jsonb NOT NULL DEFAULT
 '{"fallback_enabled":false,"max_attempts":3,"timeout_seconds":90,"max_input_tokens":32000,"max_output_tokens":4000,"max_cost_usd":null}';
CREATE TABLE ai_task_candidates (
 task text NOT NULL REFERENCES learning_ai_tasks(task) ON DELETE CASCADE,
 model_id bigint NOT NULL REFERENCES ai_models(id), priority integer NOT NULL CHECK(priority>=0),
 PRIMARY KEY(task,model_id), UNIQUE(task,priority)
);
INSERT INTO ai_task_candidates(task,model_id,priority)
 SELECT t.task,m.id,(e.ordinality-1)::integer FROM learning_ai_tasks t
 CROSS JOIN LATERAL jsonb_array_elements_text(t.model_ids) WITH ORDINALITY e(value,ordinality)
 JOIN ai_models m ON m.id=e.value::bigint ON CONFLICT DO NOTHING;
CREATE TABLE ai_invocations (
 id text PRIMARY KEY, owner_id text REFERENCES users(id), task text NOT NULL,
 model_id bigint REFERENCES ai_models(id), status text NOT NULL,
 input_token_bound integer NOT NULL, output_limit integer NOT NULL,
 usage jsonb, error_code text NOT NULL DEFAULT '',
 started_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz
);
CREATE INDEX ai_invocations_owner_time ON ai_invocations(owner_id,started_at DESC);
