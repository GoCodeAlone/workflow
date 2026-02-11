import { useState, useEffect, useCallback } from 'react';
import {
  apiUpdateWorkflow,
  apiDeleteWorkflow,
  apiListVersions,
  apiListPermissions,
  apiShareWorkflow,
  type ApiWorkflowRecord,
  type ApiMembership,
} from '../../utils/api.ts';

interface WorkflowSettingsProps {
  workflow: ApiWorkflowRecord;
  onClose: () => void;
  onDeleted: () => void;
  onUpdated: (wf: ApiWorkflowRecord) => void;
}

export default function WorkflowSettings({ workflow, onClose, onDeleted, onUpdated }: WorkflowSettingsProps) {
  const [name, setName] = useState(workflow.name);
  const [description, setDescription] = useState(workflow.description || '');
  const [saving, setSaving] = useState(false);

  const [versions, setVersions] = useState<ApiWorkflowRecord[]>([]);
  const [permissions, setPermissions] = useState<ApiMembership[]>([]);
  const [shareUserId, setShareUserId] = useState('');
  const [shareRole, setShareRole] = useState('viewer');

  const [activeTab, setActiveTab] = useState<'general' | 'sharing' | 'versions' | 'danger'>('general');

  const loadVersions = useCallback(async () => {
    try {
      const v = await apiListVersions(workflow.id);
      setVersions(v || []);
    } catch {
      // ignore
    }
  }, [workflow.id]);

  const loadPermissions = useCallback(async () => {
    try {
      const p = await apiListPermissions(workflow.id);
      setPermissions(p || []);
    } catch {
      // ignore
    }
  }, [workflow.id]);

  useEffect(() => {
    loadVersions();
    loadPermissions();
  }, [loadVersions, loadPermissions]);

  const handleSave = async () => {
    setSaving(true);
    try {
      const updated = await apiUpdateWorkflow(workflow.id, { name, description });
      onUpdated(updated);
    } catch {
      // ignore
    }
    setSaving(false);
  };

  const handleShare = async () => {
    if (!shareUserId.trim()) return;
    try {
      await apiShareWorkflow(workflow.id, shareUserId, shareRole);
      await loadPermissions();
      setShareUserId('');
    } catch {
      // ignore
    }
  };

  const handleDelete = async () => {
    if (!confirm('Are you sure you want to delete this workflow? This cannot be undone.')) return;
    try {
      await apiDeleteWorkflow(workflow.id);
      onDeleted();
    } catch {
      // ignore
    }
  };

  const tabs = [
    { key: 'general' as const, label: 'General' },
    { key: 'sharing' as const, label: 'Sharing' },
    { key: 'versions' as const, label: 'Versions' },
    { key: 'danger' as const, label: 'Danger Zone' },
  ];

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
        style={{
          width: 560,
          maxHeight: '80vh',
          background: '#181825',
          borderRadius: 12,
          border: '1px solid #313244',
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid #313244',
            display: 'flex',
            alignItems: 'center',
          }}
        >
          <h3 style={{ margin: 0, color: '#cdd6f4', fontSize: 16, flex: 1 }}>Workflow Settings</h3>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: '#a6adc8',
              fontSize: 18,
              cursor: 'pointer',
            }}
          >
            &#10005;
          </button>
        </div>

        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid #313244', padding: '0 20px' }}>
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setActiveTab(t.key)}
              style={{
                padding: '8px 14px',
                background: 'none',
                border: 'none',
                borderBottom: activeTab === t.key ? '2px solid #89b4fa' : '2px solid transparent',
                color: activeTab === t.key ? '#89b4fa' : '#a6adc8',
                fontSize: 13,
                fontWeight: 500,
                cursor: 'pointer',
              }}
            >
              {t.label}
            </button>
          ))}
        </div>

        {/* Content */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '16px 20px' }}>
          {activeTab === 'general' && (
            <div>
              <label style={labelStyle}>Name</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                style={inputStyle}
              />

              <label style={{ ...labelStyle, marginTop: 12 }}>Description</label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={3}
                style={{ ...inputStyle, resize: 'vertical' }}
              />

              <button
                onClick={handleSave}
                disabled={saving}
                style={{
                  marginTop: 16,
                  padding: '8px 20px',
                  background: saving ? '#585b70' : '#89b4fa',
                  border: 'none',
                  borderRadius: 6,
                  color: '#1e1e2e',
                  fontSize: 13,
                  fontWeight: 600,
                  cursor: saving ? 'not-allowed' : 'pointer',
                }}
              >
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            </div>
          )}

          {activeTab === 'sharing' && (
            <div>
              <div style={{ marginBottom: 16 }}>
                <label style={labelStyle}>Share with user</label>
                <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
                  <input
                    value={shareUserId}
                    onChange={(e) => setShareUserId(e.target.value)}
                    placeholder="User ID..."
                    style={{ ...inputStyle, flex: 1 }}
                  />
                  <select
                    value={shareRole}
                    onChange={(e) => setShareRole(e.target.value)}
                    style={{
                      ...inputStyle,
                      width: 100,
                    }}
                  >
                    <option value="viewer">Viewer</option>
                    <option value="editor">Editor</option>
                    <option value="admin">Admin</option>
                    <option value="owner">Owner</option>
                  </select>
                  <button onClick={handleShare} style={btnStyle}>
                    Share
                  </button>
                </div>
              </div>

              <label style={labelStyle}>Current permissions</label>
              {permissions.length === 0 && (
                <div style={{ color: '#6c7086', fontSize: 12, marginTop: 8 }}>
                  No permissions configured.
                </div>
              )}
              {permissions.map((p) => (
                <div
                  key={p.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    padding: '6px 0',
                    borderBottom: '1px solid #313244',
                    fontSize: 13,
                    gap: 8,
                  }}
                >
                  <span style={{ flex: 1, color: '#cdd6f4' }}>{p.user_id}</span>
                  <span
                    style={{
                      padding: '2px 8px',
                      borderRadius: 8,
                      background: '#313244',
                      color: '#89b4fa',
                      fontSize: 11,
                      fontWeight: 600,
                    }}
                  >
                    {p.role}
                  </span>
                </div>
              ))}
            </div>
          )}

          {activeTab === 'versions' && (
            <div>
              {versions.length === 0 && (
                <div style={{ color: '#6c7086', fontSize: 12 }}>No version history available.</div>
              )}
              {versions.map((v) => (
                <div
                  key={`${v.id}-v${v.version}`}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    padding: '8px 0',
                    borderBottom: '1px solid #313244',
                    gap: 12,
                    fontSize: 13,
                  }}
                >
                  <span style={{ fontWeight: 600, color: '#89b4fa', minWidth: 30 }}>v{v.version}</span>
                  <span style={{ flex: 1, color: '#a6adc8' }}>
                    {new Date(v.updated_at).toLocaleString()}
                  </span>
                  <span style={{ color: '#6c7086', fontSize: 11 }}>{v.status}</span>
                </div>
              ))}
            </div>
          )}

          {activeTab === 'danger' && (
            <div>
              <div
                style={{
                  border: '1px solid #f38ba8',
                  borderRadius: 8,
                  padding: 16,
                  background: 'rgba(243, 139, 168, 0.05)',
                }}
              >
                <h4 style={{ margin: '0 0 8px', color: '#f38ba8', fontSize: 14 }}>
                  Delete Workflow
                </h4>
                <p style={{ margin: '0 0 12px', color: '#a6adc8', fontSize: 13 }}>
                  Once deleted, this workflow cannot be recovered.
                  All versions and permissions will be permanently removed.
                </p>
                <button
                  onClick={handleDelete}
                  style={{
                    padding: '8px 20px',
                    background: '#f38ba8',
                    border: 'none',
                    borderRadius: 6,
                    color: '#1e1e2e',
                    fontSize: 13,
                    fontWeight: 600,
                    cursor: 'pointer',
                  }}
                >
                  Delete This Workflow
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

const labelStyle: React.CSSProperties = {
  display: 'block',
  color: '#a6adc8',
  fontSize: 12,
  fontWeight: 500,
  marginBottom: 4,
};

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 10px',
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 6,
  color: '#cdd6f4',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

const btnStyle: React.CSSProperties = {
  padding: '8px 14px',
  background: '#89b4fa',
  border: 'none',
  borderRadius: 6,
  color: '#1e1e2e',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
  whiteSpace: 'nowrap',
};
