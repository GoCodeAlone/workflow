-- 004_workflows: Create workflows and workflow_versions tables
CREATE TABLE IF NOT EXISTS workflows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config_yaml TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'stopped', 'error')),
    created_by  UUID NOT NULL REFERENCES users(id),
    updated_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (project_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_workflows_project_id ON workflows (project_id);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows (status);

CREATE TABLE IF NOT EXISTS workflow_versions (
    workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    config_yaml TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'stopped', 'error')),
    updated_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (workflow_id, version)
);
