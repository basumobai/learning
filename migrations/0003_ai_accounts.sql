CREATE TABLE permission_groups (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL UNIQUE
);
INSERT INTO permission_groups(name) VALUES ('基础学习');
ALTER TABLE users ADD COLUMN username text UNIQUE,
  ADD COLUMN password_hash text,
  ADD COLUMN role text NOT NULL DEFAULT 'learner' CHECK(role IN ('admin','learner')),
  ADD COLUMN group_id bigint NOT NULL DEFAULT 1 REFERENCES permission_groups(id),
  ADD COLUMN disabled boolean NOT NULL DEFAULT false,
  ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
  ADD COLUMN totp_secret bytea,
  ADD COLUMN totp_pending bytea,
  ADD COLUMN totp_pending_expires timestamptz,
  ADD COLUMN totp_last_step bigint NOT NULL DEFAULT -1;
CREATE TABLE auth_sessions (
  token_hash text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX auth_sessions_user_idx ON auth_sessions(user_id);
CREATE TABLE login_attempts (
  key text PRIMARY KEY,
  failures integer NOT NULL DEFAULT 0,
  started_at timestamptz NOT NULL DEFAULT now(),
  blocked_until timestamptz
);
CREATE TABLE account_audit (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_id text REFERENCES users(id),
  action text NOT NULL,
  target text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_providers (
  id text PRIMARY KEY,
  name text NOT NULL,
  base_url text NOT NULL,
  api_key bytea NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_models (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  provider_id text NOT NULL REFERENCES ai_providers(id),
  model_id text NOT NULL,
  name text NOT NULL,
  kind text NOT NULL CHECK(kind IN ('chat','tts')),
  voices jsonb NOT NULL DEFAULT '[]',
  instructions text NOT NULL DEFAULT '',
  json_mode boolean NOT NULL DEFAULT false,
  enabled boolean NOT NULL DEFAULT true,
  UNIQUE(provider_id,model_id,kind)
);
CREATE TABLE group_models (
  group_id bigint NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
  model_id bigint NOT NULL REFERENCES ai_models(id) ON DELETE CASCADE,
  PRIMARY KEY(group_id,model_id)
);
CREATE TABLE user_preferences (
  user_id text PRIMARY KEY REFERENCES users(id),
  daily_new integer NOT NULL DEFAULT 10 CHECK(daily_new BETWEEN 0 AND 100),
  retention double precision NOT NULL DEFAULT 0.9 CHECK(retention BETWEEN 0.8 AND 0.97),
  timezone text NOT NULL DEFAULT 'Asia/Shanghai',
  tts_model_id bigint REFERENCES ai_models(id),
  voice text NOT NULL DEFAULT 'cedar'
);
ALTER TABLE word_senses ADD COLUMN phonetic text NOT NULL DEFAULT '';
ALTER TABLE word_senses ALTER COLUMN example_en SET DEFAULT '';
CREATE UNIQUE INDEX sense_content_unique ON word_senses(word_id,part_of_speech,meaning_zh);
CREATE TABLE vocabulary_imports (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  actor_id text NOT NULL REFERENCES users(id),
  request_id text NOT NULL,
  content_hash text NOT NULL,
  added integer NOT NULL,
  skipped integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(actor_id,request_id)
);
CREATE TABLE suspended_senses (
  user_id text NOT NULL REFERENCES users(id),
  sense_id bigint NOT NULL REFERENCES word_senses(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,sense_id)
);
ALTER TABLE review_states DROP CONSTRAINT review_states_dimension_check;
ALTER TABLE review_states ADD CONSTRAINT review_states_dimension_check CHECK(dimension IN ('recognition','meaning_recall','spelling','listening_spelling','context','pronunciation'));
ALTER TABLE study_questions DROP CONSTRAINT study_questions_question_type_check;
ALTER TABLE study_questions ADD CONSTRAINT study_questions_question_type_check CHECK(question_type IN ('recognition','meaning_recall','spelling','listening_spelling'));
CREATE TABLE ai_conversations (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  title text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversation_owner_idx ON ai_conversations(user_id,updated_at DESC);
CREATE TABLE ai_messages (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  conversation_id text NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
  role text NOT NULL CHECK(role IN ('user','assistant')),
  content text NOT NULL,
  model_id bigint REFERENCES ai_models(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_requests (
  user_id text NOT NULL REFERENCES users(id),
  request_id text NOT NULL,
  fingerprint text NOT NULL,
  conversation_id text REFERENCES ai_conversations(id) ON DELETE CASCADE,
  kind text NOT NULL,
  status text NOT NULL CHECK(status IN ('running','completed','failed')),
  response jsonb,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,request_id)
);
CREATE TABLE training_sessions (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  kind text NOT NULL CHECK(kind IN ('sentence','listening')),
  title text NOT NULL,
  payload jsonb NOT NULL,
  answers jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX training_owner_idx ON training_sessions(user_id,created_at DESC);
CREATE TABLE speech_cache (
  user_id text NOT NULL REFERENCES users(id),
  cache_key text NOT NULL,
  audio bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,cache_key)
);
