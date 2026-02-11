-- 006_sessions: Create sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT NOT NULL,
    ip_address  TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    metadata    JSONB,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions (token) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);
