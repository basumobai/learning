CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id text PRIMARY KEY,
  display_name text NOT NULL
);
CREATE TABLE words (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  lemma text NOT NULL UNIQUE
);
CREATE TABLE word_senses (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  word_id bigint NOT NULL REFERENCES words(id),
  part_of_speech text NOT NULL,
  meaning_zh text NOT NULL,
  example_en text NOT NULL
);
CREATE TABLE sense_aliases (
  sense_id bigint NOT NULL REFERENCES word_senses(id),
  normalized_zh text NOT NULL,
  PRIMARY KEY(sense_id, normalized_zh)
);
CREATE TABLE review_states (
  user_id text NOT NULL REFERENCES users(id),
  sense_id bigint NOT NULL REFERENCES word_senses(id),
  dimension text NOT NULL CHECK (dimension IN ('recognition','meaning_recall','spelling','context','pronunciation')),
  attempts integer NOT NULL DEFAULT 0,
  correct_count integer NOT NULL DEFAULT 0,
  next_review_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,sense_id,dimension)
);
CREATE TABLE study_sessions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE study_questions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id bigint NOT NULL REFERENCES study_sessions(id),
  sense_id bigint NOT NULL REFERENCES word_senses(id),
  question_type text NOT NULL CHECK (question_type IN ('recognition','meaning_recall','spelling')),
  prompt text NOT NULL,
  hint_text text NOT NULL DEFAULT '',
  choices jsonb,
  position integer NOT NULL,
  UNIQUE(session_id,position)
);
CREATE TABLE answer_logs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  session_id bigint NOT NULL REFERENCES study_sessions(id),
  question_id bigint NOT NULL UNIQUE REFERENCES study_questions(id),
  request_id text NOT NULL,
  raw_answer text NOT NULL,
  result text NOT NULL CHECK (result IN ('correct','wrong','pending')),
  response_time_ms integer NOT NULL,
  used_hint boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,request_id)
);
CREATE INDEX review_due_idx ON review_states(user_id,next_review_at);
CREATE INDEX answer_user_created_idx ON answer_logs(user_id,created_at DESC);

INSERT INTO users(id,display_name) VALUES ('demo','体验用户');
INSERT INTO words(lemma) VALUES ('adapt'),('adopt'),('maintain'),('consequence'),('necessary');
INSERT INTO word_senses(word_id,part_of_speech,meaning_zh,example_en) VALUES
  (1,'v.','适应','Plants adapt to their surroundings.'),
  (2,'v.','采用','The class will adopt a new plan.'),
  (3,'v.','维持','We maintain a steady pace.'),
  (4,'n.','后果','Every choice has a consequence.'),
  (5,'adj.','必要的','Rest is necessary for learning.');
INSERT INTO sense_aliases(sense_id,normalized_zh) VALUES
  (1,'适应'),(2,'采用'),(2,'采纳'),(3,'维持'),(3,'保持'),(4,'后果'),(4,'结果'),(5,'必要的'),(5,'必需的');
