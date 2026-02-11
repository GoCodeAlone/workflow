-- 001_companies: Create companies table
CREATE TABLE IF NOT EXISTS companies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    owner_id    UUID NOT NULL,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_companies_slug ON companies (slug);
CREATE INDEX IF NOT EXISTS idx_companies_owner_id ON companies (owner_id);
