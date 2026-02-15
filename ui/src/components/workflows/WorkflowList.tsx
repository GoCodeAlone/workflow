import { useState, useEffect } from 'react';
import {
  apiListWorkflows,
  apiCreateWorkflow,
  apiDeleteWorkflow,
  apiDeployWorkflow,
  apiStopWorkflow,
  type ApiWorkflowRecord,
} from '../../utils/api.ts';

interface WorkflowListProps {
  projectId: string;
  projectName: string;
  onOpenWorkflow: (wf: ApiWorkflowRecord) => void;
}

const STATUS_COLORS: Record<string, { bg: string; text: string }> = {
  draft: { bg: '#585b70', text: '#cdd6f4' },
  active: { bg: 'rgba(166, 227, 161, 0.2)', text: '#a6e3a1' },
  stopped: { bg: 'rgba(249, 226, 175, 0.2)', text: '#f9e2af' },
  error: { bg: 'rgba(243, 139, 168, 0.2)', text: '#f38ba8' },
};

export default function WorkflowList({ projectId, projectName, onOpenWorkflow }: WorkflowListProps) {
  const [workflows, setWorkflows] = useState<ApiWorkflowRecord[]>([]);
  const [filter, setFilter] = useState('');
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    apiListWorkflows(projectId)
      .then((wfs) => {
        if (!cancelled) setWorkflows(wfs || []);
      })
      .catch(() => {/* ignore */})
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [projectId]);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    try {
      const wf = await apiCreateWorkflow(projectId, { name: newName });
      setWorkflows((prev) => [wf, ...prev]);
      setCreating(false);
      setNewName('');
    } catch {
      // ignore
    }
  };

  const handleDeploy = async (id: string) => {
    try {
      const updated = await apiDeployWorkflow(id);
      setWorkflows((prev) => prev.map((w) => (w.id === id ? updated : w)));
    } catch {
      // ignore
    }
  };

  const handleStop = async (id: string) => {
    try {
      const updated = await apiStopWorkflow(id);
      setWorkflows((prev) => prev.map((w) => (w.id === id ? updated : w)));
    } catch {
      // ignore
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this workflow? This action cannot be undone.')) return;
    try {
      await apiDeleteWorkflow(id);
      setWorkflows((prev) => prev.filter((w) => w.id !== id));
    } catch {
      // ignore
    }
  };

  const filtered = workflows.filter((w) =>
    w.name.toLowerCase().includes(filter.toLowerCase()),
  );

  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        background: '#1e1e2e',
        color: '#cdd6f4',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '16px 24px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600, flex: 1 }}>
          {projectName}
        </h2>
        <input
          type="text"
          placeholder="Filter workflows..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{
            padding: '6px 10px',
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 6,
            color: '#cdd6f4',
            fontSize: 13,
            outline: 'none',
            width: 200,
          }}
        />
        <button
          onClick={() => setCreating(true)}
          style={{
            padding: '6px 14px',
            background: '#89b4fa',
            border: 'none',
            borderRadius: 6,
            color: '#1e1e2e',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
          }}
        >
          New Workflow
        </button>
      </div>

      {/* Create form */}
      {creating && (
        <div
          style={{
            padding: '12px 24px',
            borderBottom: '1px solid #313244',
            display: 'flex',
            gap: 8,
            alignItems: 'center',
          }}
        >
          <input
            autoFocus
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleCreate();
              if (e.key === 'Escape') { setCreating(false); setNewName(''); }
            }}
            placeholder="Workflow name..."
            style={{
              flex: 1,
              padding: '6px 10px',
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 6,
              color: '#cdd6f4',
              fontSize: 13,
              outline: 'none',
            }}
          />
          <button onClick={handleCreate} style={actionBtnStyle('#89b4fa')}>Create</button>
          <button onClick={() => { setCreating(false); setNewName(''); }} style={actionBtnStyle('#45475a')}>
            Cancel
          </button>
        </div>
      )}

      {/* List */}
      <div style={{ flex: 1, overflowY: 'auto', padding: '8px 24px' }}>
        {loading && (
          <div style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}>Loading...</div>
        )}

        {!loading && filtered.length === 0 && (
          <div style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}>
            {workflows.length === 0 ? 'No workflows yet. Create one to get started.' : 'No matching workflows.'}
          </div>
        )}

        {filtered.map((wf) => {
          const statusStyle = STATUS_COLORS[wf.status] || STATUS_COLORS.draft;
          return (
            <div
              key={wf.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '10px 12px',
                borderRadius: 8,
                marginBottom: 4,
                background: '#181825',
                border: '1px solid #313244',
                gap: 12,
              }}
            >
              {/* Name */}
              <div
                style={{ flex: 1, cursor: 'pointer', minWidth: 0 }}
                onClick={() => onOpenWorkflow(wf)}
              >
                <div
                  style={{
                    fontWeight: 500,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    color: wf.is_system ? '#f9e2af' : '#89b4fa',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                  }}
                >
                  {wf.is_system && <span title="System workflow">{'\u{1F6E1}'}</span>}
                  {wf.name}
                  {wf.is_system && (
                    <span style={{
                      fontSize: 10,
                      padding: '1px 5px',
                      borderRadius: 8,
                      background: 'rgba(249, 226, 175, 0.15)',
                      color: '#f9e2af',
                      fontWeight: 600,
                    }}>
                      v{wf.version}
                    </span>
                  )}
                </div>
                <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>
                  {!wf.is_system && <>v{wf.version} &middot; </>}
                  {new Date(wf.updated_at).toLocaleDateString()}
                  {wf.is_system && ' \u00B7 Admin Configuration'}
                </div>
              </div>

              {/* Status badge */}
              <span
                style={{
                  padding: '3px 8px',
                  borderRadius: 10,
                  fontSize: 11,
                  fontWeight: 600,
                  background: statusStyle.bg,
                  color: statusStyle.text,
                  whiteSpace: 'nowrap',
                }}
              >
                {wf.status}
              </span>

              {/* Actions */}
              <div style={{ display: 'flex', gap: 4 }}>
                {(wf.status === 'draft' || wf.status === 'stopped') && (
                  <button
                    onClick={() => handleDeploy(wf.id)}
                    style={iconBtnStyle}
                    title="Deploy"
                  >
                    &#9654;
                  </button>
                )}
                {wf.status === 'active' && (
                  <button
                    onClick={() => handleStop(wf.id)}
                    style={iconBtnStyle}
                    title="Stop"
                  >
                    &#9632;
                  </button>
                )}
                <button
                  onClick={() => onOpenWorkflow(wf)}
                  style={iconBtnStyle}
                  title="Edit"
                >
                  &#9998;
                </button>
                {!wf.is_system && (
                  <button
                    onClick={() => handleDelete(wf.id)}
                    style={{ ...iconBtnStyle, color: '#f38ba8' }}
                    title="Delete"
                  >
                    &#10005;
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

const iconBtnStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#a6adc8',
  cursor: 'pointer',
  fontSize: 14,
  padding: '4px 6px',
  borderRadius: 4,
};

function actionBtnStyle(bg: string): React.CSSProperties {
  return {
    padding: '6px 12px',
    background: bg,
    border: 'none',
    borderRadius: 6,
    color: bg === '#45475a' ? '#cdd6f4' : '#1e1e2e',
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  };
}
