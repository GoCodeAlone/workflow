-- 005_memberships: Create memberships table and cross_workflow_links table
CREATE TABLE IF NOT EXISTS memberships (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_id  UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    project_id  UUID REFERENCES projects(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A user can only have one role per company (when project_id is NULL)
    -- or per project (when project_id is set).
    UNIQUE NULLS NOT DISTINCT (user_id, company_id, project_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_user_id ON memberships (user_id);
CREATE INDEX IF NOT EXISTS idx_memberships_company_id ON memberships (company_id);
CREATE INDEX IF NOT EXISTS idx_memberships_project_id ON memberships (project_id);

CREATE TABLE IF NOT EXISTS cross_workflow_links (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_workflow_id  UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    target_workflow_id  UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    link_type           TEXT NOT NULL,
    config              JSONB,
    created_by          UUID NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (source_workflow_id, target_workflow_id, link_type)
);

CREATE INDEX IF NOT EXISTS idx_cwl_source ON cross_workflow_links (source_workflow_id);
CREATE INDEX IF NOT EXISTS idx_cwl_target ON cross_workflow_links (target_workflow_id);
