-- 001_initial.sql
-- Initial schema for platform state management.

-- platform_resources stores the current state of all managed resources,
-- partitioned by context path.
CREATE TABLE IF NOT EXISTS platform_resources (
    context_path TEXT    NOT NULL,
    name         TEXT    NOT NULL,
    type         TEXT    NOT NULL DEFAULT '',
    provider_type TEXT   NOT NULL DEFAULT '',
    endpoint     TEXT    NOT NULL DEFAULT '',
    connection_str TEXT  NOT NULL DEFAULT '',
    credential_ref TEXT  NOT NULL DEFAULT '',
    properties   TEXT    NOT NULL DEFAULT '{}',
    status       TEXT    NOT NULL DEFAULT 'pending',
    last_synced  TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (context_path, name)
);

-- platform_plans stores execution plans for infrastructure changes.
CREATE TABLE IF NOT EXISTS platform_plans (
    id           TEXT    PRIMARY KEY,
    tier         INTEGER NOT NULL DEFAULT 0,
    context_path TEXT    NOT NULL DEFAULT '',
    actions      TEXT    NOT NULL DEFAULT '[]',
    created_at   TEXT    NOT NULL DEFAULT '',
    approved_at  TEXT,
    approved_by  TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'pending',
    provider     TEXT    NOT NULL DEFAULT '',
    dry_run      INTEGER NOT NULL DEFAULT 0,
    fidelity_reports TEXT NOT NULL DEFAULT '[]'
);

-- platform_dependencies tracks cross-resource and cross-tier dependencies
-- for impact analysis when upstream resources change.
CREATE TABLE IF NOT EXISTS platform_dependencies (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    source_context  TEXT NOT NULL,
    source_resource TEXT NOT NULL,
    target_context  TEXT NOT NULL,
    target_resource TEXT NOT NULL,
    dep_type        TEXT NOT NULL DEFAULT 'hard',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (source_context, source_resource, target_context, target_resource)
);

-- platform_drift_reports stores drift detection results.
CREATE TABLE IF NOT EXISTS platform_drift_reports (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    context_path    TEXT    NOT NULL,
    resource_name   TEXT    NOT NULL,
    resource_type   TEXT    NOT NULL DEFAULT '',
    tier            INTEGER NOT NULL DEFAULT 0,
    drift_type      TEXT    NOT NULL DEFAULT '',
    expected        TEXT    NOT NULL DEFAULT '{}',
    actual          TEXT    NOT NULL DEFAULT '{}',
    diffs           TEXT    NOT NULL DEFAULT '[]',
    detected_at     TEXT    NOT NULL DEFAULT '',
    resolved_at     TEXT,
    resolved_by     TEXT    NOT NULL DEFAULT ''
);

-- platform_locks stores advisory locks for context paths.
CREATE TABLE IF NOT EXISTS platform_locks (
    context_path TEXT    PRIMARY KEY,
    holder       TEXT    NOT NULL DEFAULT '',
    acquired_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    expires_at   TEXT    NOT NULL
);

-- Indexes for common queries.
CREATE INDEX IF NOT EXISTS idx_resources_context ON platform_resources (context_path);
CREATE INDEX IF NOT EXISTS idx_resources_status ON platform_resources (status);
CREATE INDEX IF NOT EXISTS idx_plans_context ON platform_plans (context_path);
CREATE INDEX IF NOT EXISTS idx_plans_status ON platform_plans (status);
CREATE INDEX IF NOT EXISTS idx_deps_source ON platform_dependencies (source_context, source_resource);
CREATE INDEX IF NOT EXISTS idx_deps_target ON platform_dependencies (target_context, target_resource);
CREATE INDEX IF NOT EXISTS idx_drift_context ON platform_drift_reports (context_path);
CREATE INDEX IF NOT EXISTS idx_drift_resource ON platform_drift_reports (context_path, resource_name);
CREATE INDEX IF NOT EXISTS idx_drift_detected ON platform_drift_reports (detected_at);
CREATE INDEX IF NOT EXISTS idx_locks_expires ON platform_locks (expires_at);
