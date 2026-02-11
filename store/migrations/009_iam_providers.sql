-- IAM provider configuration and role mappings
CREATE TABLE iam_provider_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL CHECK (provider_type IN ('aws_iam', 'kubernetes', 'oidc', 'saml', 'ldap', 'custom')),
    name TEXT NOT NULL,
    config JSONB NOT NULL,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, name)
);

CREATE TABLE iam_role_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES iam_provider_configs(id) ON DELETE CASCADE,
    external_identifier TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(provider_id, external_identifier, resource_type, resource_id)
);
