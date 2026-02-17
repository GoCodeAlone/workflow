import { useState, useEffect, useCallback } from 'react';
import useFeatureFlagStore from '../../store/featureFlagStore.ts';
import type { FlagDefinition, CreateFlagRequest, FlagType, FlagOverride, TargetingRule } from '../../store/featureFlagStore.ts';
import FlagOverrides from './FlagOverrides.tsx';
import FlagRules from './FlagRules.tsx';

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

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function ToggleSwitch({ enabled, onChange }: { enabled: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      onClick={() => onChange(!enabled)}
      style={{
        width: 36,
        height: 20,
        borderRadius: 10,
        border: 'none',
        cursor: 'pointer',
        background: enabled ? '#a6e3a1' : '#45475a',
        position: 'relative',
        transition: 'background 0.15s',
        flexShrink: 0,
      }}
    >
      <div
        style={{
          width: 14,
          height: 14,
          borderRadius: '50%',
          background: '#1e1e2e',
          position: 'absolute',
          top: 3,
          left: enabled ? 19 : 3,
          transition: 'left 0.15s',
        }}
      />
    </button>
  );
}

function TypeBadge({ type }: { type: FlagType }) {
  const colors: Record<FlagType, string> = {
    boolean: '#a6e3a1',
    string: '#89b4fa',
    number: '#f9e2af',
    json: '#cba6f7',
  };
  return (
    <span
      style={{
        padding: '1px 6px',
        borderRadius: 3,
        fontSize: 10,
        fontWeight: 600,
        background: (colors[type] || '#6c7086') + '22',
        color: colors[type] || '#6c7086',
        textTransform: 'uppercase',
      }}
    >
      {type}
    </span>
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
        style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 16, padding: '0 4px' }}
      >
        x
      </button>
    </div>
  );
}

function FormField({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', fontSize: 12, color: '#a6adc8', marginBottom: 4 }}>
        {label}
        {required && <span style={{ color: '#f38ba8', marginLeft: 2 }}>*</span>}
      </label>
      {children}
    </div>
  );
}

function formatValue(value: unknown): string {
  if (value === null || value === undefined) return '-';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

// ---------------------------------------------------------------------------
// Create / Edit Flag Modal
// ---------------------------------------------------------------------------

interface FlagFormData {
  key: string;
  name: string;
  description: string;
  type: FlagType;
  enabled: boolean;
  default_value: string;
  tags: string;
  scope: string;
  overrides: FlagOverride[];
  targeting_rules: TargetingRule[];
}

function emptyFormData(): FlagFormData {
  return {
    key: '',
    name: '',
    description: '',
    type: 'boolean',
    enabled: false,
    default_value: 'false',
    tags: '',
    scope: 'global',
    overrides: [],
    targeting_rules: [],
  };
}

function formDataFromFlag(flag: FlagDefinition): FlagFormData {
  return {
    key: flag.key,
    name: flag.name,
    description: flag.description || '',
    type: flag.type,
    enabled: flag.enabled,
    default_value: formatValue(flag.default_value),
    tags: (flag.tags || []).join(', '),
    scope: flag.scope || 'global',
    overrides: flag.overrides || [],
    targeting_rules: flag.targeting_rules || [],
  };
}

function parseDefaultValue(raw: string, type: FlagType): unknown {
  if (type === 'boolean') return raw === 'true';
  if (type === 'number') return Number(raw) || 0;
  if (type === 'json') {
    try { return JSON.parse(raw); } catch { return raw; }
  }
  return raw;
}

function FlagFormModal({
  initial,
  title,
  isEdit,
  onSave,
  onCancel,
  saving,
}: {
  initial: FlagFormData;
  title: string;
  isEdit: boolean;
  onSave: (data: FlagFormData) => void;
  onCancel: () => void;
  saving: boolean;
}) {
  const [form, setForm] = useState<FlagFormData>(initial);

  const setField = <K extends keyof FlagFormData>(key: K, value: FlagFormData[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

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
          maxWidth: 600,
          maxHeight: '85vh',
          overflowY: 'auto',
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 16, fontWeight: 600 }}>
          {title}
        </h3>

        <FormField label="Key" required>
          <input
            value={form.key}
            onChange={(e) => setField('key', e.target.value)}
            placeholder="feature.my-flag"
            disabled={isEdit}
            style={{ ...inputStyle, opacity: isEdit ? 0.5 : 1 }}
          />
        </FormField>

        <FormField label="Name" required>
          <input
            value={form.name}
            onChange={(e) => setField('name', e.target.value)}
            placeholder="My Feature Flag"
            style={inputStyle}
          />
        </FormField>

        <FormField label="Description">
          <textarea
            value={form.description}
            onChange={(e) => setField('description', e.target.value)}
            placeholder="What does this flag control?"
            rows={2}
            style={{ ...inputStyle, resize: 'vertical' }}
          />
        </FormField>

        <div style={{ display: 'flex', gap: 12 }}>
          <div style={{ flex: 1 }}>
            <FormField label="Type" required>
              <select
                value={form.type}
                onChange={(e) => setField('type', e.target.value as FlagType)}
                disabled={isEdit}
                style={{ ...inputStyle, opacity: isEdit ? 0.5 : 1 }}
              >
                <option value="boolean">Boolean</option>
                <option value="string">String</option>
                <option value="number">Number</option>
                <option value="json">JSON</option>
              </select>
            </FormField>
          </div>
          <div style={{ flex: 1 }}>
            <FormField label="Scope">
              <input
                value={form.scope}
                onChange={(e) => setField('scope', e.target.value)}
                placeholder="global"
                style={inputStyle}
              />
            </FormField>
          </div>
        </div>

        <FormField label="Default Value" required>
          {form.type === 'boolean' ? (
            <select
              value={form.default_value}
              onChange={(e) => setField('default_value', e.target.value)}
              style={inputStyle}
            >
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <input
              value={form.default_value}
              onChange={(e) => setField('default_value', e.target.value)}
              placeholder={form.type === 'number' ? '0' : form.type === 'json' ? '{}' : 'value'}
              style={inputStyle}
            />
          )}
        </FormField>

        <div style={{ display: 'flex', gap: 12 }}>
          <div style={{ flex: 1 }}>
            <FormField label="Tags (comma-separated)">
              <input
                value={form.tags}
                onChange={(e) => setField('tags', e.target.value)}
                placeholder="frontend, experiment"
                style={inputStyle}
              />
            </FormField>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingTop: 16 }}>
            <span style={{ fontSize: 12, color: '#a6adc8' }}>Enabled</span>
            <ToggleSwitch enabled={form.enabled} onChange={(v) => setField('enabled', v)} />
          </div>
        </div>

        {/* Overrides section */}
        <div style={{ borderTop: '1px solid #45475a', paddingTop: 12, marginTop: 4 }}>
          <FlagOverrides
            overrides={form.overrides}
            flagType={form.type}
            onChange={(overrides) => setField('overrides', overrides)}
          />
        </div>

        {/* Targeting rules section */}
        <div style={{ borderTop: '1px solid #45475a', paddingTop: 12, marginTop: 12 }}>
          <FlagRules
            rules={form.targeting_rules}
            flagType={form.type}
            onChange={(rules) => setField('targeting_rules', rules)}
          />
        </div>

        {/* Actions */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 16 }}>
          <button onClick={onCancel} style={cancelBtnStyle} disabled={saving}>Cancel</button>
          <button
            onClick={() => onSave(form)}
            style={primaryBtnStyle}
            disabled={saving || !form.key || !form.name}
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Delete Confirm Dialog
// ---------------------------------------------------------------------------

function DeleteConfirmDialog({
  flagKey,
  onConfirm,
  onCancel,
}: {
  flagKey: string;
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
          Delete Flag
        </h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.5, marginBottom: 16 }}>
          Are you sure you want to delete <strong style={{ color: '#cdd6f4' }}>{flagKey}</strong>?
          This action cannot be undone.
        </p>
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button onClick={onCancel} style={cancelBtnStyle}>Cancel</button>
          <button onClick={onConfirm} style={{ ...primaryBtnStyle, background: '#f38ba8' }}>
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Flag Row
// ---------------------------------------------------------------------------

function FlagRow({
  flag,
  onToggle,
  onEdit,
  onDelete,
}: {
  flag: FlagDefinition;
  onToggle: (key: string, enabled: boolean) => void;
  onEdit: (flag: FlagDefinition) => void;
  onDelete: (flag: FlagDefinition) => void;
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '12px 16px',
        background: '#313244',
        borderRadius: 8,
        border: '1px solid #45475a',
        transition: 'border-color 0.15s',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#89b4fa')}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = '#45475a')}
    >
      <ToggleSwitch enabled={flag.enabled} onChange={(v) => onToggle(flag.key, v)} />

      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 2 }}>
          <span style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{flag.name}</span>
          <TypeBadge type={flag.type} />
          {flag.scope && flag.scope !== 'global' && (
            <span style={{ fontSize: 10, color: '#6c7086', padding: '1px 5px', background: '#45475a', borderRadius: 3 }}>
              {flag.scope}
            </span>
          )}
        </div>
        <div style={{ fontSize: 11, color: '#6c7086', fontFamily: 'monospace' }}>{flag.key}</div>
        {flag.description && (
          <div style={{ fontSize: 12, color: '#a6adc8', marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {flag.description}
          </div>
        )}
        {flag.tags && flag.tags.length > 0 && (
          <div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
            {flag.tags.map((tag) => (
              <span
                key={tag}
                style={{
                  padding: '1px 5px',
                  borderRadius: 3,
                  fontSize: 10,
                  background: '#89b4fa22',
                  color: '#89b4fa',
                }}
              >
                {tag}
              </span>
            ))}
          </div>
        )}
      </div>

      <div style={{ textAlign: 'right', flexShrink: 0, minWidth: 80 }}>
        <div style={{ fontSize: 11, color: '#6c7086' }}>Value</div>
        <div style={{ fontSize: 13, color: '#f9e2af', fontFamily: 'monospace' }}>
          {formatValue(flag.default_value)}
        </div>
      </div>

      <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
        <button onClick={() => onEdit(flag)} style={smallBtnStyle}>Edit</button>
        <button
          onClick={() => onDelete(flag)}
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

export default function FlagManager() {
  const flags = useFeatureFlagStore((s) => s.flags);
  const loading = useFeatureFlagStore((s) => s.loading);
  const error = useFeatureFlagStore((s) => s.error);
  const sseConnected = useFeatureFlagStore((s) => s.sseConnected);
  const fetchFlags = useFeatureFlagStore((s) => s.fetchFlags);
  const createFlag = useFeatureFlagStore((s) => s.createFlag);
  const updateFlag = useFeatureFlagStore((s) => s.updateFlag);
  const deleteFlag = useFeatureFlagStore((s) => s.deleteFlag);
  const connectSSE = useFeatureFlagStore((s) => s.connectSSE);
  const disconnectSSE = useFeatureFlagStore((s) => s.disconnectSSE);

  const [showCreateForm, setShowCreateForm] = useState(false);
  const [editingFlag, setEditingFlag] = useState<FlagDefinition | null>(null);
  const [deletingFlag, setDeletingFlag] = useState<FlagDefinition | null>(null);
  const [saving, setSaving] = useState(false);
  const [search, setSearch] = useState('');
  const [filterTag, setFilterTag] = useState('');
  const [filterScope, setFilterScope] = useState('');

  useEffect(() => {
    fetchFlags();
    connectSSE();
    return () => disconnectSSE();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleCreate = useCallback(async (data: FlagFormData) => {
    setSaving(true);
    try {
      const req: CreateFlagRequest = {
        key: data.key,
        name: data.name,
        description: data.description || undefined,
        type: data.type,
        enabled: data.enabled,
        default_value: parseDefaultValue(data.default_value, data.type),
        tags: data.tags ? data.tags.split(',').map((t) => t.trim()).filter(Boolean) : undefined,
        scope: data.scope || undefined,
      };
      await createFlag(req);
      // If overrides or rules were set, update them
      if (data.overrides.length > 0 || data.targeting_rules.length > 0) {
        await updateFlag(data.key, {
          overrides: data.overrides,
          targeting_rules: data.targeting_rules,
        });
      }
      setShowCreateForm(false);
    } catch {
      // error in store
    } finally {
      setSaving(false);
    }
  }, [createFlag, updateFlag]);

  const handleUpdate = useCallback(async (data: FlagFormData) => {
    if (!editingFlag) return;
    setSaving(true);
    try {
      await updateFlag(editingFlag.key, {
        name: data.name,
        description: data.description,
        enabled: data.enabled,
        default_value: parseDefaultValue(data.default_value, data.type),
        tags: data.tags ? data.tags.split(',').map((t) => t.trim()).filter(Boolean) : [],
        scope: data.scope,
        overrides: data.overrides,
        targeting_rules: data.targeting_rules,
      });
      setEditingFlag(null);
    } catch {
      // error in store
    } finally {
      setSaving(false);
    }
  }, [editingFlag, updateFlag]);

  const handleDelete = useCallback(async () => {
    if (!deletingFlag) return;
    try {
      await deleteFlag(deletingFlag.key);
      setDeletingFlag(null);
    } catch {
      // error in store
    }
  }, [deletingFlag, deleteFlag]);

  const handleToggle = useCallback(async (key: string, enabled: boolean) => {
    try {
      await updateFlag(key, { enabled });
    } catch {
      // error in store
    }
  }, [updateFlag]);

  // Collect unique tags and scopes for filter dropdowns
  const allTags = Array.from(new Set(flags.flatMap((f) => f.tags || []))).sort();
  const allScopes = Array.from(new Set(flags.map((f) => f.scope || 'global').filter(Boolean))).sort();

  // Filter
  const filtered = flags.filter((flag) => {
    if (search) {
      const q = search.toLowerCase();
      const matchName = flag.name.toLowerCase().includes(q);
      const matchKey = flag.key.toLowerCase().includes(q);
      const matchDesc = (flag.description || '').toLowerCase().includes(q);
      if (!matchName && !matchKey && !matchDesc) return false;
    }
    if (filterTag && !(flag.tags || []).includes(filterTag)) return false;
    if (filterScope && (flag.scope || 'global') !== filterScope) return false;
    return true;
  });

  return (
    <div style={{ flex: 1, background: '#1e1e2e', overflow: 'auto', padding: 24 }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
        <div>
          <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>
            Feature Flags
          </h2>
          <p style={{ color: '#6c7086', fontSize: 13, margin: 0 }}>
            Manage feature flags, targeting rules, and overrides.
            {sseConnected && (
              <span style={{ marginLeft: 8, fontSize: 11, color: '#a6e3a1' }}>
                (live)
              </span>
            )}
          </p>
        </div>
        <button onClick={() => setShowCreateForm(true)} style={primaryBtnStyle}>
          + Create Flag
        </button>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => useFeatureFlagStore.setState({ error: null })} />}

      {/* Filters */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search flags..."
          style={{ ...inputStyle, width: 240, marginBottom: 0 }}
        />
        {allTags.length > 0 && (
          <select
            value={filterTag}
            onChange={(e) => setFilterTag(e.target.value)}
            style={{ ...inputStyle, width: 140, marginBottom: 0 }}
          >
            <option value="">All Tags</option>
            {allTags.map((tag) => (
              <option key={tag} value={tag}>{tag}</option>
            ))}
          </select>
        )}
        {allScopes.length > 1 && (
          <select
            value={filterScope}
            onChange={(e) => setFilterScope(e.target.value)}
            style={{ ...inputStyle, width: 140, marginBottom: 0 }}
          >
            <option value="">All Scopes</option>
            {allScopes.map((scope) => (
              <option key={scope} value={scope}>{scope}</option>
            ))}
          </select>
        )}
        <button onClick={() => fetchFlags()} style={smallBtnStyle}>Refresh</button>
        <span style={{ fontSize: 12, color: '#6c7086', marginLeft: 8 }}>
          {filtered.length} flag{filtered.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Content */}
      {loading && flags.length === 0 ? (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#a6adc8', fontSize: 14, padding: 40 }}>
          Loading flags...
        </div>
      ) : filtered.length === 0 ? (
        <div style={{ color: '#6c7086', fontSize: 14, textAlign: 'center', padding: 40, background: '#313244', borderRadius: 8 }}>
          {flags.length === 0
            ? 'No feature flags yet. Create one to get started.'
            : 'No flags match the current filters.'}
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {filtered.map((flag) => (
            <FlagRow
              key={flag.key}
              flag={flag}
              onToggle={handleToggle}
              onEdit={setEditingFlag}
              onDelete={setDeletingFlag}
            />
          ))}
        </div>
      )}

      {/* Create dialog */}
      {showCreateForm && (
        <FlagFormModal
          initial={emptyFormData()}
          title="Create Feature Flag"
          isEdit={false}
          onSave={handleCreate}
          onCancel={() => setShowCreateForm(false)}
          saving={saving}
        />
      )}

      {/* Edit dialog */}
      {editingFlag && (
        <FlagFormModal
          initial={formDataFromFlag(editingFlag)}
          title={`Edit: ${editingFlag.name}`}
          isEdit={true}
          onSave={handleUpdate}
          onCancel={() => setEditingFlag(null)}
          saving={saving}
        />
      )}

      {/* Delete confirm */}
      {deletingFlag && (
        <DeleteConfirmDialog
          flagKey={deletingFlag.key}
          onConfirm={handleDelete}
          onCancel={() => setDeletingFlag(null)}
        />
      )}
    </div>
  );
}
