import { useState, useEffect, useCallback } from 'react';
import useEnvironmentStore from '../../store/environmentStore.ts';
import type { Environment } from '../../store/environmentStore.ts';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const STATUS_COLORS: Record<string, string> = {
  active: '#a6e3a1',
  provisioning: '#f9e2af',
  error: '#f38ba8',
  decommissioned: '#6c7086',
};

const PROVIDERS = ['aws', 'gcp', 'azure', 'digitalocean'] as const;
type Provider = (typeof PROVIDERS)[number];

const PROVIDER_LABELS: Record<Provider, string> = {
  aws: 'AWS',
  gcp: 'Google Cloud',
  azure: 'Azure',
  digitalocean: 'DigitalOcean',
};

interface ProviderFieldDef {
  key: string;
  label: string;
  sensitive: boolean;
  target: 'config' | 'secrets';
  placeholder?: string;
}

const PROVIDER_FIELDS: Record<Provider, ProviderFieldDef[]> = {
  aws: [
    { key: 'region', label: 'Region', sensitive: false, target: 'config', placeholder: 'us-east-1' },
    { key: 'access_key_id', label: 'Access Key ID', sensitive: true, target: 'secrets' },
    { key: 'secret_access_key', label: 'Secret Access Key', sensitive: true, target: 'secrets' },
  ],
  gcp: [
    { key: 'project_id', label: 'Project ID', sensitive: false, target: 'config', placeholder: 'my-project-123' },
    { key: 'region', label: 'Region', sensitive: false, target: 'config', placeholder: 'us-central1' },
    { key: 'credentials_json', label: 'Credentials JSON', sensitive: true, target: 'secrets' },
  ],
  azure: [
    { key: 'subscription_id', label: 'Subscription ID', sensitive: false, target: 'config' },
    { key: 'tenant_id', label: 'Tenant ID', sensitive: false, target: 'config' },
    { key: 'client_id', label: 'Client ID', sensitive: false, target: 'config' },
    { key: 'client_secret', label: 'Client Secret', sensitive: true, target: 'secrets' },
  ],
  digitalocean: [
    { key: 'region', label: 'Region', sensitive: false, target: 'config', placeholder: 'nyc3' },
    { key: 'api_token', label: 'API Token', sensitive: true, target: 'secrets' },
  ],
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDate(iso: string): string {
  if (!iso) return '-';
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StatusBadge({ status }: { status: string }) {
  const color = STATUS_COLORS[status] || '#6c7086';
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 11,
        fontWeight: 600,
        background: color + '22',
        color,
        textTransform: 'capitalize',
      }}
    >
      <span
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: color,
          display: 'inline-block',
        }}
      />
      {status}
    </span>
  );
}

function Spinner() {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: '#a6adc8',
        fontSize: 14,
        padding: 40,
      }}
    >
      Loading environments...
    </div>
  );
}

function ErrorBanner({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <div
      style={{
        background: '#f38ba822',
        border: '1px solid #f38ba844',
        borderRadius: 6,
        padding: '10px 16px',
        marginBottom: 16,
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        fontSize: 13,
        color: '#f38ba8',
      }}
    >
      <span>{message}</span>
      <button
        onClick={onDismiss}
        style={{
          background: 'none',
          border: 'none',
          color: '#f38ba8',
          cursor: 'pointer',
          fontSize: 16,
          padding: '0 4px',
        }}
      >
        x
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Environment Form (create / edit)
// ---------------------------------------------------------------------------

interface EnvFormData {
  name: string;
  workflow_id: string;
  provider: Provider;
  region: string;
  config: Record<string, string>;
  secrets: Record<string, string>;
}

function emptyFormData(): EnvFormData {
  return { name: '', workflow_id: '', provider: 'aws', region: '', config: {}, secrets: {} };
}

function formDataFromEnv(env: Environment): EnvFormData {
  const provider = (PROVIDERS.includes(env.provider as Provider) ? env.provider : 'aws') as Provider;
  const config: Record<string, string> = {};
  const secrets: Record<string, string> = {};
  for (const [k, v] of Object.entries(env.config ?? {})) config[k] = String(v);
  for (const [k, v] of Object.entries(env.secrets ?? {})) secrets[k] = String(v);
  return {
    name: env.name,
    workflow_id: env.workflow_id,
    provider,
    region: env.region,
    config,
    secrets,
  };
}

function EnvironmentFormModal({
  initial,
  title,
  onSave,
  onCancel,
  saving,
}: {
  initial: EnvFormData;
  title: string;
  onSave: (data: EnvFormData) => void;
  onCancel: () => void;
  saving: boolean;
}) {
  const [form, setForm] = useState<EnvFormData>(initial);

  const fields = PROVIDER_FIELDS[form.provider] || [];

  const setField = (key: string, value: string) => setForm((f) => ({ ...f, [key]: value }));

  const setProviderField = (field: ProviderFieldDef, value: string) => {
    setForm((f) => {
      if (field.target === 'secrets') {
        return { ...f, secrets: { ...f.secrets, [field.key]: value } };
      }
      return { ...f, config: { ...f.config, [field.key]: value } };
    });
  };

  const getProviderFieldValue = (field: ProviderFieldDef): string => {
    if (field.target === 'secrets') return form.secrets[field.key] || '';
    return form.config[field.key] || '';
  };

  return (
    <div
      onClick={onCancel}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          padding: 24,
          width: '90%',
          maxWidth: 520,
          maxHeight: '80vh',
          overflowY: 'auto',
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 16, fontWeight: 600 }}>
          {title}
        </h3>

        {/* Name */}
        <FormField label="Name" required>
          <input
            value={form.name}
            onChange={(e) => setField('name', e.target.value)}
            placeholder="production-us-east"
            style={inputStyle}
          />
        </FormField>

        {/* Workflow ID */}
        <FormField label="Workflow ID" required>
          <input
            value={form.workflow_id}
            onChange={(e) => setField('workflow_id', e.target.value)}
            placeholder="workflow-uuid"
            style={inputStyle}
          />
        </FormField>

        {/* Provider */}
        <FormField label="Provider" required>
          <select
            value={form.provider}
            onChange={(e) => setField('provider', e.target.value)}
            style={inputStyle}
          >
            {PROVIDERS.map((p) => (
              <option key={p} value={p}>{PROVIDER_LABELS[p]}</option>
            ))}
          </select>
        </FormField>

        {/* Region (top-level) */}
        <FormField label="Region">
          <input
            value={form.region}
            onChange={(e) => setField('region', e.target.value)}
            placeholder="us-east-1"
            style={inputStyle}
          />
        </FormField>

        {/* Provider-specific fields */}
        {fields.length > 0 && (
          <div style={{ marginTop: 12, padding: '12px 0', borderTop: '1px solid #45475a' }}>
            <div style={{ fontSize: 12, color: '#a6adc8', fontWeight: 600, marginBottom: 8 }}>
              {PROVIDER_LABELS[form.provider]} Configuration
            </div>
            {fields.map((field) => (
              <FormField key={field.key} label={field.label} sensitive={field.sensitive}>
                <input
                  value={getProviderFieldValue(field)}
                  onChange={(e) => setProviderField(field, e.target.value)}
                  placeholder={field.placeholder}
                  type={field.sensitive ? 'password' : 'text'}
                  style={inputStyle}
                />
              </FormField>
            ))}
          </div>
        )}

        {/* Actions */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 16 }}>
          <button onClick={onCancel} style={cancelBtnStyle} disabled={saving}>
            Cancel
          </button>
          <button
            onClick={() => onSave(form)}
            style={primaryBtnStyle}
            disabled={saving || !form.name || !form.workflow_id}
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}

function FormField({
  label,
  required,
  sensitive,
  children,
}: {
  label: string;
  required?: boolean;
  sensitive?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', fontSize: 12, color: '#a6adc8', marginBottom: 4 }}>
        {label}
        {required && <span style={{ color: '#f38ba8', marginLeft: 2 }}>*</span>}
        {sensitive && <span style={{ color: '#f9e2af', marginLeft: 6, fontSize: 10 }}>sensitive</span>}
      </label>
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Delete Confirm Dialog
// ---------------------------------------------------------------------------

function DeleteConfirmDialog({
  envName,
  onConfirm,
  onCancel,
}: {
  envName: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      onClick={onCancel}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          padding: 24,
          width: '90%',
          maxWidth: 400,
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 12px', fontSize: 16, fontWeight: 600 }}>
          Delete Environment
        </h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.5, marginBottom: 16 }}>
          Are you sure you want to delete <strong style={{ color: '#cdd6f4' }}>{envName}</strong>?
          This action cannot be undone.
        </p>
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button onClick={onCancel} style={cancelBtnStyle}>Cancel</button>
          <button
            onClick={onConfirm}
            style={{ ...primaryBtnStyle, background: '#f38ba8' }}
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Environment Card
// ---------------------------------------------------------------------------

function EnvironmentCard({
  env,
  onEdit,
  onDelete,
  onTest,
}: {
  env: Environment;
  onEdit: (env: Environment) => void;
  onDelete: (env: Environment) => void;
  onTest: (env: Environment) => void;
}) {
  return (
    <div
      style={{
        background: '#313244',
        borderRadius: 8,
        border: '1px solid #45475a',
        padding: 16,
        transition: 'border-color 0.15s',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#89b4fa')}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = '#45475a')}
    >
      {/* Header row */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
        <div>
          <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 15 }}>{env.name}</div>
          <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>
            {PROVIDER_LABELS[env.provider as Provider] || env.provider}
            {env.region ? ` / ${env.region}` : ''}
          </div>
        </div>
        <StatusBadge status={env.status} />
      </div>

      {/* Details */}
      <div style={{ fontSize: 12, color: '#a6adc8', marginBottom: 12, display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <span>
          <span style={{ color: '#6c7086' }}>Workflow: </span>
          <span style={{ color: '#89b4fa', fontFamily: 'monospace', fontSize: 11 }}>{env.workflow_id}</span>
        </span>
        <span>
          <span style={{ color: '#6c7086' }}>Created: </span>{formatDate(env.created_at)}
        </span>
        <span>
          <span style={{ color: '#6c7086' }}>Updated: </span>{formatDate(env.updated_at)}
        </span>
      </div>

      {/* Actions */}
      <div style={{ display: 'flex', gap: 8 }}>
        <button onClick={() => onTest(env)} style={smallBtnStyle}>
          Test Connection
        </button>
        <button onClick={() => onEdit(env)} style={smallBtnStyle}>
          Edit
        </button>
        <button
          onClick={() => onDelete(env)}
          style={{ ...smallBtnStyle, color: '#f38ba8', borderColor: '#f38ba844' }}
        >
          Delete
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Environments() {
  const environments = useEnvironmentStore((s) => s.environments);
  const loading = useEnvironmentStore((s) => s.loading);
  const error = useEnvironmentStore((s) => s.error);
  const fetchEnvironments = useEnvironmentStore((s) => s.fetchEnvironments);
  const createEnvironment = useEnvironmentStore((s) => s.createEnvironment);
  const updateEnvironment = useEnvironmentStore((s) => s.updateEnvironment);
  const deleteEnvironment = useEnvironmentStore((s) => s.deleteEnvironment);
  const testConnection = useEnvironmentStore((s) => s.testConnection);

  const [showCreateForm, setShowCreateForm] = useState(false);
  const [editingEnv, setEditingEnv] = useState<Environment | null>(null);
  const [deletingEnv, setDeletingEnv] = useState<Environment | null>(null);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<{ envId: string; success: boolean; message: string } | null>(null);
  const [filterProvider, setFilterProvider] = useState('');
  const [filterStatus, setFilterStatus] = useState('');

  useEffect(() => {
    fetchEnvironments();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleCreate = useCallback(async (data: EnvFormData) => {
    setSaving(true);
    try {
      await createEnvironment({
        name: data.name,
        workflow_id: data.workflow_id,
        provider: data.provider,
        region: data.region,
        config: data.config,
        secrets: data.secrets,
      });
      setShowCreateForm(false);
    } catch {
      // error is set in the store
    } finally {
      setSaving(false);
    }
  }, [createEnvironment]);

  const handleUpdate = useCallback(async (data: EnvFormData) => {
    if (!editingEnv) return;
    setSaving(true);
    try {
      await updateEnvironment(editingEnv.id, {
        name: data.name,
        workflow_id: data.workflow_id,
        provider: data.provider,
        region: data.region,
        config: data.config,
        secrets: data.secrets,
      });
      setEditingEnv(null);
    } catch {
      // error is set in the store
    } finally {
      setSaving(false);
    }
  }, [editingEnv, updateEnvironment]);

  const handleDelete = useCallback(async () => {
    if (!deletingEnv) return;
    try {
      await deleteEnvironment(deletingEnv.id);
      setDeletingEnv(null);
    } catch {
      // error is set in the store
    }
  }, [deletingEnv, deleteEnvironment]);

  const handleTest = useCallback(async (env: Environment) => {
    setTestResult(null);
    try {
      const result = await testConnection(env.id);
      setTestResult({ envId: env.id, success: result.success, message: result.message });
    } catch (e) {
      setTestResult({ envId: env.id, success: false, message: e instanceof Error ? e.message : 'Test failed' });
    }
  }, [testConnection]);

  const handleRefresh = useCallback(() => {
    const filter: Record<string, string> = {};
    if (filterProvider) filter.provider = filterProvider;
    if (filterStatus) filter.status = filterStatus;
    fetchEnvironments(Object.keys(filter).length > 0 ? filter : undefined);
  }, [fetchEnvironments, filterProvider, filterStatus]);

  // Apply client-side filter for immediate feedback
  const filtered = (Array.isArray(environments) ? environments : []).filter((env) => {
    if (filterProvider && env.provider !== filterProvider) return false;
    if (filterStatus && env.status !== filterStatus) return false;
    return true;
  });

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
        <div>
          <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>
            Environments
          </h2>
          <p style={{ color: '#6c7086', fontSize: 13, margin: 0 }}>
            Manage deployment targets and cloud provider configurations.
          </p>
        </div>
        <button
          onClick={() => setShowCreateForm(true)}
          style={primaryBtnStyle}
        >
          + Create Environment
        </button>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => useEnvironmentStore.setState({ error: null })} />}

      {testResult && (
        <div
          style={{
            background: testResult.success ? '#a6e3a122' : '#f38ba822',
            border: `1px solid ${testResult.success ? '#a6e3a144' : '#f38ba844'}`,
            borderRadius: 6,
            padding: '10px 16px',
            marginBottom: 16,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            fontSize: 13,
            color: testResult.success ? '#a6e3a1' : '#f38ba8',
          }}
        >
          <span>Connection test: {testResult.message}</span>
          <button
            onClick={() => setTestResult(null)}
            style={{
              background: 'none',
              border: 'none',
              color: testResult.success ? '#a6e3a1' : '#f38ba8',
              cursor: 'pointer',
              fontSize: 16,
              padding: '0 4px',
            }}
          >
            x
          </button>
        </div>
      )}

      {/* Filters */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
        <select
          value={filterProvider}
          onChange={(e) => setFilterProvider(e.target.value)}
          style={{ ...inputStyle, width: 160, marginBottom: 0 }}
        >
          <option value="">All Providers</option>
          {PROVIDERS.map((p) => (
            <option key={p} value={p}>{PROVIDER_LABELS[p]}</option>
          ))}
        </select>
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          style={{ ...inputStyle, width: 160, marginBottom: 0 }}
        >
          <option value="">All Statuses</option>
          <option value="active">Active</option>
          <option value="provisioning">Provisioning</option>
          <option value="error">Error</option>
          <option value="decommissioned">Decommissioned</option>
        </select>
        <button onClick={handleRefresh} style={smallBtnStyle}>
          Refresh
        </button>
        <span style={{ fontSize: 12, color: '#6c7086', marginLeft: 8 }}>
          {filtered.length} environment{filtered.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Content */}
      {loading && environments.length === 0 ? (
        <Spinner />
      ) : filtered.length === 0 ? (
        <div
          style={{
            color: '#6c7086',
            fontSize: 14,
            textAlign: 'center',
            padding: 40,
            background: '#313244',
            borderRadius: 8,
          }}
        >
          {environments.length === 0
            ? 'No environments yet. Create one to get started.'
            : 'No environments match the current filters.'}
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(420px, 1fr))', gap: 16 }}>
          {filtered.map((env) => (
            <EnvironmentCard
              key={env.id}
              env={env}
              onEdit={setEditingEnv}
              onDelete={setDeletingEnv}
              onTest={handleTest}
            />
          ))}
        </div>
      )}

      {/* Create dialog */}
      {showCreateForm && (
        <EnvironmentFormModal
          initial={emptyFormData()}
          title="Create Environment"
          onSave={handleCreate}
          onCancel={() => setShowCreateForm(false)}
          saving={saving}
        />
      )}

      {/* Edit dialog */}
      {editingEnv && (
        <EnvironmentFormModal
          initial={formDataFromEnv(editingEnv)}
          title={`Edit: ${editingEnv.name}`}
          onSave={handleUpdate}
          onCancel={() => setEditingEnv(null)}
          saving={saving}
        />
      )}

      {/* Delete confirm dialog */}
      {deletingEnv && (
        <DeleteConfirmDialog
          envName={deletingEnv.name}
          onConfirm={handleDelete}
          onCancel={() => setDeletingEnv(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared styles
// ---------------------------------------------------------------------------

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 10px',
  borderRadius: 6,
  border: '1px solid #45475a',
  background: '#313244',
  color: '#cdd6f4',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

const primaryBtnStyle: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 6,
  border: 'none',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
  background: '#89b4fa',
  color: '#1e1e2e',
};

const cancelBtnStyle: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 6,
  border: '1px solid #45475a',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
  background: 'transparent',
  color: '#a6adc8',
};

const smallBtnStyle: React.CSSProperties = {
  padding: '4px 12px',
  borderRadius: 4,
  border: '1px solid #45475a',
  fontSize: 11,
  fontWeight: 600,
  cursor: 'pointer',
  background: 'transparent',
  color: '#89b4fa',
};
