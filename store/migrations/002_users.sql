-- 002_users: Create users table
CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL DEFAULT '',
    display_name    TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    oauth_provider  TEXT NOT NULL DEFAULT '',
    oauth_id        TEXT NOT NULL DEFAULT '',
    active          BOOLEAN NOT NULL DEFAULT true,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oauth ON users (oauth_provider, oauth_id) WHERE oauth_provider != '' AND oauth_id != '';
