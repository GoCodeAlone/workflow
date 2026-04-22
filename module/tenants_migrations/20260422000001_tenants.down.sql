-- +goose Down
DROP INDEX IF EXISTS tenants_domains_idx;
DROP INDEX IF EXISTS tenants_active_idx;
DROP INDEX IF EXISTS tenants_slug_idx;
DROP TABLE IF EXISTS tenants;
