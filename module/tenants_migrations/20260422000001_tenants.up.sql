-- +goose Up
-- Tenant table with configurable schema support.
-- The real DDL is generated at runtime via TenantSchemaConfig templates.
-- This file is a reference migration for the default schema.

CREATE TABLE IF NOT EXISTS tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT UNIQUE NOT NULL,
    name        TEXT NOT NULL,
    domains     TEXT[] NOT NULL DEFAULT '{}',
    metadata    JSONB NOT NULL DEFAULT '{}',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tenants_slug_idx ON tenants (slug);
CREATE INDEX IF NOT EXISTS tenants_active_idx ON tenants (is_active);

-- Partial index for fast domain lookup.
CREATE INDEX IF NOT EXISTS tenants_domains_idx ON tenants USING GIN (domains);
