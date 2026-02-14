import { useEffect, useState, useCallback } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import { apiListCompanies, type ApiCompany } from '../../utils/api.ts';
import type { IAMProviderType, IAMRoleMapping } from '../../types/observability.ts';
import UserManagement from '../settings/UserManagement.tsx';

const PROVIDER_TYPE_COLORS: Record<string, string> = {
  aws_iam: '#fab387',
  kubernetes: '#89b4fa',
  oidc: '#cba6f7',
  saml: '#a6e3a1',
  ldap: '#f9e2af',
  custom: '#6c7086',
};

const PROVIDER_TYPES: { value: IAMProviderType; label: string }[] = [
  { value: 'aws_iam', label: 'AWS IAM' },
  { value: 'kubernetes', label: 'Kubernetes' },
  { value: 'oidc', label: 'OIDC' },
  { value: 'saml', label: 'SAML' },
  { value: 'ldap', label: 'LDAP' },
  { value: 'custom', label: 'Custom' },
];

function ProviderTypeBadge({ type }: { type: string }) {
  const color = PROVIDER_TYPE_COLORS[type] || '#6c7086';
  const label = PROVIDER_TYPES.find((p) => p.value === type)?.label ?? type;
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 10,
        fontWeight: 600,
        background: color + '22',
        color,
      }}
    >
      {label}
    </span>
  );
}

interface ProviderFormData {
  provider_type: IAMProviderType;
  name: string;
  config: Record<string, string>;
  enabled: boolean;
}

const CONFIG_FIELDS: Record<IAMProviderType, { key: string; label: string; type?: string }[]> = {
  aws_iam: [
    { key: 'region', label: 'Region' },
    { key: 'account_id', label: 'Account ID' },
    { key: 'role_arn_pattern', label: 'Role ARN Pattern' },
    { key: 'external_id', label: 'External ID' },
  ],
  kubernetes: [
    { key: 'api_server_url', label: 'API Server URL' },
    { key: 'namespace', label: 'Namespace' },
    { key: 'ca_cert', label: 'CA Certificate', type: 'textarea' },
  ],
  oidc: [
    { key: 'issuer_url', label: 'Issuer URL' },
    { key: 'client_id', label: 'Client ID' },
    { key: 'claims_mapping', label: 'Claims Mapping (JSON)', type: 'textarea' },
  ],
  saml: [
    { key: 'metadata_url', label: 'Metadata URL' },
    { key: 'entity_id', label: 'Entity ID' },
  ],
  ldap: [
    { key: 'server_url', label: 'Server URL' },
    { key: 'base_dn', label: 'Base DN' },
    { key: 'bind_dn', label: 'Bind DN' },
    { key: 'search_filter', label: 'Search Filter' },
  ],
  custom: [
    { key: 'config_json', label: 'Configuration (JSON)', type: 'textarea' },
  ],
};

interface MappingFormData {
  external_identifier: string;
  resource_type: string;
  resource_id: string;
  role: string;
}

function AddProviderModal({
  onClose,
  onSave,
}: {
  onClose: () => void;
  onSave: (data: ProviderFormData) => void;
}) {
  const [form, setForm] = useState<ProviderFormData>({
    provider_type: 'aws_iam',
    name: '',
    config: {},
    enabled: true,
  });

  const fields = CONFIG_FIELDS[form.provider_type] || [];

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSave(form);
  };

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          padding: 24,
          width: 480,
          maxHeight: '80vh',
          overflow: 'auto',
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 16 }}>Add IAM Provider</h3>
        <form onSubmit={handleSubmit}>
          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
              Provider Type
            </label>
            <select
              value={form.provider_type}
              onChange={(e) =>
                setForm({ ...form, provider_type: e.target.value as IAMProviderType, config: {} })
              }
              style={{
                width: '100%',
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 12px',
                fontSize: 13,
                outline: 'none',
              }}
            >
              {PROVIDER_TYPES.map((t) => (
                <option key={t.value} value={t.value}>{t.label}</option>
              ))}
            </select>
          </div>

          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>Name</label>
            <input
              type="text"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
              style={{
                width: '100%',
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 12px',
                fontSize: 13,
                outline: 'none',
                boxSizing: 'border-box',
              }}
            />
          </div>

          {fields.map((field) => (
            <div key={field.key} style={{ marginBottom: 12 }}>
              <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>
                {field.label}
              </label>
              {field.type === 'textarea' ? (
                <textarea
                  value={form.config[field.key] || ''}
                  onChange={(e) =>
                    setForm({ ...form, config: { ...form.config, [field.key]: e.target.value } })
                  }
                  rows={4}
                  style={{
                    width: '100%',
                    background: '#313244',
                    border: '1px solid #45475a',
                    borderRadius: 6,
                    color: '#cdd6f4',
                    padding: '8px 12px',
                    fontSize: 12,
                    fontFamily: 'monospace',
                    outline: 'none',
                    resize: 'vertical',
                    boxSizing: 'border-box',
                  }}
                />
              ) : (
                <input
                  type="text"
                  value={form.config[field.key] || ''}
                  onChange={(e) =>
                    setForm({ ...form, config: { ...form.config, [field.key]: e.target.value } })
                  }
                  style={{
                    width: '100%',
                    background: '#313244',
                    border: '1px solid #45475a',
                    borderRadius: 6,
                    color: '#cdd6f4',
                    padding: '8px 12px',
                    fontSize: 13,
                    outline: 'none',
                    boxSizing: 'border-box',
                  }}
                />
              )}
            </div>
          ))}

          <div style={{ display: 'flex', gap: 8, marginTop: 20 }}>
            <button
              type="submit"
              style={{
                background: '#89b4fa',
                border: 'none',
                borderRadius: 6,
                color: '#1e1e2e',
                padding: '8px 20px',
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Create
            </button>
            <button
              type="button"
              onClick={onClose}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 20px',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function MappingTable({
  mappings,
  onDelete,
  onAdd,
}: {
  providerId?: string;
  mappings: IAMRoleMapping[];
  onDelete: (id: string) => void;
  onAdd: (data: MappingFormData) => void;
}) {
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<MappingFormData>({
    external_identifier: '',
    resource_type: 'company',
    resource_id: '',
    role: 'viewer',
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onAdd(form);
    setForm({ external_identifier: '', resource_type: 'company', resource_id: '', role: 'viewer' });
    setShowForm(false);
  };

  return (
    <div style={{ marginTop: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <span style={{ color: '#a6adc8', fontSize: 12, fontWeight: 600 }}>Role Mappings</span>
        <button
          onClick={() => setShowForm(!showForm)}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#89b4fa',
            fontSize: 11,
            padding: '2px 8px',
            cursor: 'pointer',
          }}
        >
          + Add
        </button>
      </div>

      {showForm && (
        <form
          onSubmit={handleSubmit}
          style={{
            display: 'flex',
            gap: 6,
            marginBottom: 8,
            flexWrap: 'wrap',
            alignItems: 'flex-end',
          }}
        >
          <input
            type="text"
            placeholder="External ID"
            value={form.external_identifier}
            onChange={(e) => setForm({ ...form, external_identifier: e.target.value })}
            required
            style={{
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 4,
              color: '#cdd6f4',
              padding: '4px 8px',
              fontSize: 11,
              outline: 'none',
              width: 140,
            }}
          />
          <select
            value={form.resource_type}
            onChange={(e) => setForm({ ...form, resource_type: e.target.value })}
            style={{
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 4,
              color: '#cdd6f4',
              padding: '4px 8px',
              fontSize: 11,
              outline: 'none',
            }}
          >
            <option value="company">Company</option>
            <option value="project">Project</option>
            <option value="workflow">Workflow</option>
          </select>
          <input
            type="text"
            placeholder="Resource ID"
            value={form.resource_id}
            onChange={(e) => setForm({ ...form, resource_id: e.target.value })}
            required
            style={{
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 4,
              color: '#cdd6f4',
              padding: '4px 8px',
              fontSize: 11,
              outline: 'none',
              width: 140,
              fontFamily: 'monospace',
            }}
          />
          <select
            value={form.role}
            onChange={(e) => setForm({ ...form, role: e.target.value })}
            style={{
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 4,
              color: '#cdd6f4',
              padding: '4px 8px',
              fontSize: 11,
              outline: 'none',
            }}
          >
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
            <option value="admin">Admin</option>
            <option value="owner">Owner</option>
          </select>
          <button
            type="submit"
            style={{
              background: '#89b4fa',
              border: 'none',
              borderRadius: 4,
              color: '#1e1e2e',
              padding: '4px 12px',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Add
          </button>
        </form>
      )}

      {mappings.length > 0 && (
        <div style={{ background: '#181825', borderRadius: 6, overflow: 'hidden' }}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 80px 1fr 70px 40px',
              padding: '6px 10px',
              fontSize: 10,
              color: '#6c7086',
              fontWeight: 600,
            }}
          >
            <span>External ID</span>
            <span>Type</span>
            <span>Resource</span>
            <span>Role</span>
            <span></span>
          </div>
          {mappings.map((m) => (
            <div
              key={m.id}
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 80px 1fr 70px 40px',
                padding: '5px 10px',
                fontSize: 11,
                borderTop: '1px solid #31324444',
                alignItems: 'center',
              }}
            >
              <span style={{ color: '#cdd6f4', fontFamily: 'monospace', fontSize: 10 }}>
                {m.external_identifier}
              </span>
              <span style={{ color: '#a6adc8' }}>{m.resource_type}</span>
              <span style={{ color: '#89b4fa', fontFamily: 'monospace', fontSize: 10 }}>
                {m.resource_id.slice(0, 8)}...
              </span>
              <span style={{ color: '#a6adc8' }}>{m.role}</span>
              <button
                onClick={() => onDelete(m.id)}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#f38ba8',
                  cursor: 'pointer',
                  fontSize: 11,
                  padding: 0,
                }}
              >
                X
              </button>
            </div>
          ))}
        </div>
      )}

      {mappings.length === 0 && !showForm && (
        <div style={{ color: '#6c7086', fontSize: 11, padding: '4px 0' }}>No mappings configured.</div>
      )}
    </div>
  );
}

export default function IAMSettings() {
  const {
    iamProviders,
    iamMappings,
    fetchIAMProviders,
    createIAMProvider,
    deleteIAMProvider,
    testIAMProvider,
    fetchIAMRoleMappings,
    createIAMRoleMapping,
    deleteIAMRoleMapping,
  } = useObservabilityStore();

  const [companies, setCompanies] = useState<ApiCompany[]>([]);
  const [selectedCompanyId, setSelectedCompanyId] = useState<string>('');
  const [showAddModal, setShowAddModal] = useState(false);
  const [testResults, setTestResults] = useState<Record<string, { success: boolean; message: string }>>({});
  const [expandedProvider, setExpandedProvider] = useState<string | null>(null);

  useEffect(() => {
    apiListCompanies()
      .then((c) => {
        setCompanies(c);
        if (c.length > 0 && !selectedCompanyId) {
          setSelectedCompanyId(c[0].id);
        }
      })
      .catch(() => {});
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (selectedCompanyId) {
      fetchIAMProviders(selectedCompanyId);
    }
  }, [selectedCompanyId, fetchIAMProviders]);

  const handleExpandProvider = useCallback(
    (id: string) => {
      if (expandedProvider === id) {
        setExpandedProvider(null);
        return;
      }
      setExpandedProvider(id);
      fetchIAMRoleMappings(id);
    },
    [expandedProvider, fetchIAMRoleMappings],
  );

  const handleCreateProvider = useCallback(
    async (data: ProviderFormData) => {
      try {
        await createIAMProvider(selectedCompanyId, {
          provider_type: data.provider_type,
          name: data.name,
          config: data.config,
          enabled: data.enabled,
        });
        setShowAddModal(false);
      } catch {
        // ignore
      }
    },
    [selectedCompanyId, createIAMProvider],
  );

  const handleTest = useCallback(
    async (providerId: string) => {
      try {
        const result = await testIAMProvider(providerId);
        setTestResults((prev) => ({ ...prev, [providerId]: result }));
      } catch (err) {
        setTestResults((prev) => ({
          ...prev,
          [providerId]: { success: false, message: String(err) },
        }));
      }
    },
    [testIAMProvider],
  );

  const handleDeleteProvider = useCallback(
    async (providerId: string) => {
      try {
        await deleteIAMProvider(providerId);
        if (selectedCompanyId) {
          fetchIAMProviders(selectedCompanyId);
        }
      } catch {
        // ignore
      }
    },
    [deleteIAMProvider, fetchIAMProviders, selectedCompanyId],
  );

  const handleAddMapping = useCallback(
    async (providerId: string, data: MappingFormData) => {
      try {
        await createIAMRoleMapping(providerId, {
          external_identifier: data.external_identifier,
          resource_type: data.resource_type,
          resource_id: data.resource_id,
          role: data.role,
        });
      } catch {
        // ignore
      }
    },
    [createIAMRoleMapping],
  );

  const handleDeleteMapping = useCallback(
    async (mappingId: string, providerId: string) => {
      try {
        await deleteIAMRoleMapping(mappingId, providerId);
      } catch {
        // ignore
      }
    },
    [deleteIAMRoleMapping],
  );

  return (
    <div style={{ flex: 1, background: '#1e1e2e', overflow: 'auto', padding: 24 }}>
      {/* User Management Section */}
      <div style={{ marginBottom: 32 }}>
        <UserManagement />
      </div>

      <div style={{ height: 1, background: '#45475a', marginBottom: 24 }} />

      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
        <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 18, fontWeight: 600 }}>IAM Settings</h2>

        {companies.length > 1 && (
          <select
            value={selectedCompanyId}
            onChange={(e) => setSelectedCompanyId(e.target.value)}
            style={{
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 6,
              color: '#cdd6f4',
              padding: '6px 10px',
              fontSize: 12,
              outline: 'none',
            }}
          >
            {companies.map((c) => (
              <option key={c.id} value={c.id}>{c.name}</option>
            ))}
          </select>
        )}

        <span style={{ flex: 1 }} />

        <button
          onClick={() => setShowAddModal(true)}
          style={{
            background: '#89b4fa',
            border: 'none',
            borderRadius: 6,
            color: '#1e1e2e',
            padding: '8px 16px',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
          }}
        >
          Add Provider
        </button>
      </div>

      {/* Providers table */}
      <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '2fr 100px 80px 120px',
            padding: '10px 16px',
            background: '#181825',
            fontSize: 11,
            color: '#a6adc8',
            fontWeight: 600,
          }}
        >
          <span>Name</span>
          <span>Type</span>
          <span>Status</span>
          <span>Actions</span>
        </div>
        {iamProviders.map((provider, i) => {
          const testResult = testResults[provider.id];
          const isExpanded = expandedProvider === provider.id;

          return (
            <div key={provider.id}>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: '2fr 100px 80px 120px',
                  padding: '10px 16px',
                  borderBottom: '1px solid #45475a',
                  fontSize: 13,
                  background: i % 2 === 0 ? 'transparent' : '#181825',
                  alignItems: 'center',
                  cursor: 'pointer',
                }}
                onClick={() => handleExpandProvider(provider.id)}
              >
                <span style={{ color: '#cdd6f4' }}>{provider.name}</span>
                <span><ProviderTypeBadge type={provider.provider_type} /></span>
                <span>
                  <span
                    style={{
                      display: 'inline-block',
                      width: 8,
                      height: 8,
                      borderRadius: '50%',
                      background: provider.enabled ? '#a6e3a1' : '#6c7086',
                      marginRight: 6,
                    }}
                  />
                  <span style={{ color: '#a6adc8', fontSize: 11 }}>
                    {provider.enabled ? 'On' : 'Off'}
                  </span>
                </span>
                <span style={{ display: 'flex', gap: 6 }}>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleTest(provider.id);
                    }}
                    style={{
                      background: '#313244',
                      border: '1px solid #45475a',
                      borderRadius: 4,
                      color: '#89b4fa',
                      fontSize: 10,
                      padding: '2px 8px',
                      cursor: 'pointer',
                    }}
                  >
                    Test
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDeleteProvider(provider.id);
                    }}
                    style={{
                      background: '#f38ba822',
                      border: '1px solid #f38ba8',
                      borderRadius: 4,
                      color: '#f38ba8',
                      fontSize: 10,
                      padding: '2px 8px',
                      cursor: 'pointer',
                    }}
                  >
                    Delete
                  </button>
                </span>
              </div>

              {/* Test result */}
              {testResult && (
                <div
                  style={{
                    padding: '6px 16px',
                    background: testResult.success ? '#a6e3a111' : '#f38ba811',
                    borderBottom: '1px solid #45475a',
                    fontSize: 11,
                    color: testResult.success ? '#a6e3a1' : '#f38ba8',
                  }}
                >
                  {testResult.success ? 'Connection successful' : `Failed: ${testResult.message}`}
                </div>
              )}

              {/* Expanded: mappings */}
              {isExpanded && (
                <div
                  style={{
                    padding: '8px 16px 12px',
                    borderBottom: '1px solid #45475a',
                    background: '#181825',
                  }}
                >
                  <MappingTable
                    providerId={provider.id}
                    mappings={iamMappings[provider.id] ?? []}
                    onDelete={(mappingId) => handleDeleteMapping(mappingId, provider.id)}
                    onAdd={(data) => handleAddMapping(provider.id, data)}
                  />
                </div>
              )}
            </div>
          );
        })}
        {iamProviders.length === 0 && (
          <div style={{ padding: 20, color: '#6c7086', fontSize: 13, textAlign: 'center' }}>
            No IAM providers configured.
          </div>
        )}
      </div>

      {showAddModal && (
        <AddProviderModal onClose={() => setShowAddModal(false)} onSave={handleCreateProvider} />
      )}
    </div>
  );
}
