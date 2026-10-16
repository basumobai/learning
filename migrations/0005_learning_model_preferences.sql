-- User choices contain model IDs only. Credentials remain in administrator providers.
CREATE TABLE IF NOT EXISTS learning_model_preferences (
  user_id text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  selection jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now()
);
