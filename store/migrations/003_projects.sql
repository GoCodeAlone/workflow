-- 003_projects: Create projects table
CREATE TABLE IF NOT EXISTS projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (company_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_projects_company_id ON projects (company_id);
CREATE INDEX IF NOT EXISTS idx_projects_slug ON projects (company_id, slug);
