CREATE TABLE IF NOT EXISTS config_documents (
    key         TEXT PRIMARY KEY,
    data        BYTEA NOT NULL,
    hash        TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT NOT NULL DEFAULT 'system'
);

CREATE INDEX IF NOT EXISTS idx_config_documents_updated_at ON config_documents(updated_at);

CREATE TABLE IF NOT EXISTS config_document_history (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT NOT NULL,
    data        BYTEA NOT NULL,
    hash        TEXT NOT NULL,
    version     INTEGER NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    changed_by  TEXT NOT NULL DEFAULT 'system'
);

CREATE INDEX IF NOT EXISTS idx_config_history_key ON config_document_history(key, version);
