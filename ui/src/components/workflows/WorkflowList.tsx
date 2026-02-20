import { useState, useEffect } from 'react';
import {
  apiListWorkflows,
  apiCreateWorkflow,
  apiDeleteWorkflow,
  apiDeployWorkflow,
  apiStopWorkflow,
  apiLoadWorkflowFromPath,
  apiListAllProjects,
  type ApiWorkflowRecord,
} from '../../utils/api.ts';

interface WorkflowListProps {
  projectId?: string;
  projectName?: string;
  onOpenWorkflow: (wf: ApiWorkflowRecord) => void;
}

const STATUS_COLORS: Record<string, { bg: string; text: string }> = {
  draft: { bg: '#585b70', text: '#cdd6f4' },
  active: { bg: 'rgba(166, 227, 161, 0.2)', text: '#a6e3a1' },
  stopped: { bg: 'rgba(249, 226, 175, 0.2)', text: '#f9e2af' },
  error: { bg: 'rgba(243, 139, 168, 0.2)', text: '#f38ba8' },
  deploying: { bg: 'rgba(137, 180, 250, 0.2)', text: '#89b4fa' },
  stopping: { bg: 'rgba(249, 226, 175, 0.2)', text: '#f9e2af' },
};

export default function WorkflowList({ projectId, projectName, onOpenWorkflow }: WorkflowListProps) {
  const [workflows, setWorkflows] = useState<ApiWorkflowRecord[]>([]);
  const [projectMap, setProjectMap] = useState<Map<string, string>>(new Map());
  const [filter, setFilter] = useState('');
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [loading, setLoading] = useState(true);
  const [actionInProgress, setActionInProgress] = useState<Record<string, string>>({}); // id -> "deploying"|"stopping"
  const [actionError, setActionError] = useState<Record<string, string>>({}); // id -> error message

  // Fetch workflows — all or filtered by project
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
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

  // Fetch all projects for the name lookup map (only when showing all workflows)
  useEffect(() => {
    if (projectId) return; // filtered by project — we already have projectName
    let cancelled = false;
    apiListAllProjects()
      .then((projects) => {
        if (!cancelled) {
          const map = new Map<string, string>();
          for (const p of (projects || [])) {
            map.set(p.id, p.name);
          }
          setProjectMap(map);
        }
      })
      .catch(() => {/* ignore */});
    return () => { cancelled = true; };
  }, [projectId]);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    if (!projectId) {
      setActionError((prev) => ({ ...prev, _global: 'Select a project from the sidebar before creating a workflow.' }));
      setCreating(false);
      setNewName('');
      return;
    }
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
    setActionInProgress((prev) => ({ ...prev, [id]: 'deploying' }));
    setActionError((prev) => { const next = { ...prev }; delete next[id]; return next; });
    try {
      const updated = await apiDeployWorkflow(id);
      setWorkflows((prev) => prev.map((w) => (w.id === id ? updated : w)));
    } catch (e) {
      setActionError((prev) => ({ ...prev, [id]: (e as Error).message }));
    } finally {
      setActionInProgress((prev) => { const next = { ...prev }; delete next[id]; return next; });
    }
  };

  const handleStop = async (id: string) => {
    setActionInProgress((prev) => ({ ...prev, [id]: 'stopping' }));
    setActionError((prev) => { const next = { ...prev }; delete next[id]; return next; });
    try {
      const updated = await apiStopWorkflow(id);
      setWorkflows((prev) => prev.map((w) => (w.id === id ? updated : w)));
    } catch (e) {
      setActionError((prev) => ({ ...prev, [id]: (e as Error).message }));
    } finally {
      setActionInProgress((prev) => { const next = { ...prev }; delete next[id]; return next; });
    }
  };

  const handleLoadFromPath = async () => {
    if (!projectId) {
      setActionError((prev) => ({ ...prev, _global: 'Select a project from the sidebar before loading from server path.' }));
      return;
    }
    const serverPath = window.prompt('Enter server-local path to a workflow YAML file or directory:');
    if (!serverPath) return;
    try {
      const wf = await apiLoadWorkflowFromPath(projectId, serverPath);
      setWorkflows((prev) => [wf, ...prev]);
    } catch (e) {
      setActionError((prev) => ({ ...prev, _global: (e as Error).message }));
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

  const showProjectColumn = !projectId;
  const headerTitle = projectName || 'All Workflows';

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
          {headerTitle}
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
          onClick={handleLoadFromPath}
          style={{
            padding: '6px 14px',
            background: '#45475a',
            border: 'none',
            borderRadius: 6,
            color: '#cdd6f4',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
          }}
        >
          From Server Path
        </button>
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

        {actionError._global && (
          <div style={{ padding: '8px 12px', marginBottom: 8, background: 'rgba(243, 139, 168, 0.15)', borderRadius: 6, color: '#f38ba8', fontSize: 12 }}>
            {actionError._global}
            <button onClick={() => setActionError((prev) => { const next = { ...prev }; delete next._global; return next; })} style={{ ...iconBtnStyle, color: '#f38ba8', marginLeft: 8 }}>&times;</button>
          </div>
        )}

        {filtered.map((wf) => {
          const inProgress = actionInProgress[wf.id];
          const error = actionError[wf.id];
          const displayStatus = inProgress || wf.status;
          const statusStyle = STATUS_COLORS[displayStatus] || STATUS_COLORS.draft;
          const resolvedProjectName = showProjectColumn
            ? (projectMap.get(wf.project_id) || 'Unknown')
            : null;
          return (
            <div key={wf.id}>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  padding: '10px 12px',
                  borderRadius: error ? '8px 8px 0 0' : 8,
                  marginBottom: error ? 0 : 4,
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
                    {!!wf.is_system && <span title="System workflow">{'\u{1F6E1}'}</span>}
                    {wf.name}
                    {!!wf.is_system && (
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
                    {!!wf.is_system && ' \u00B7 Admin Configuration'}
                  </div>
                </div>

                {/* Project name column (only in "All Workflows" view) */}
                {showProjectColumn && resolvedProjectName && (
                  <span
                    style={{
                      fontSize: 11,
                      color: '#a6adc8',
                      maxWidth: 140,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      flexShrink: 0,
                    }}
                    title={resolvedProjectName}
                  >
                    {resolvedProjectName}
                  </span>
                )}

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
                  {inProgress ? `${inProgress}...` : wf.status}
                </span>

                {/* Actions */}
                <div style={{ display: 'flex', gap: 4 }}>
                  {(wf.status === 'draft' || wf.status === 'stopped' || wf.status === 'error') && !inProgress && (
                    <button
                      onClick={() => handleDeploy(wf.id)}
                      style={iconBtnStyle}
                      title="Deploy"
                    >
                      &#9654;
                    </button>
                  )}
                  {wf.status === 'active' && !inProgress && (
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
              {error && (
                <div
                  style={{
                    padding: '6px 12px',
                    background: 'rgba(243, 139, 168, 0.1)',
                    borderRadius: '0 0 8px 8px',
                    border: '1px solid #313244',
                    borderTop: 'none',
                    marginBottom: 4,
                    fontSize: 11,
                    color: '#f38ba8',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                  }}
                >
                  <span style={{ flex: 1 }}>{error}</span>
                  <button onClick={() => setActionError((prev) => { const next = { ...prev }; delete next[wf.id]; return next; })} style={{ ...iconBtnStyle, color: '#f38ba8', fontSize: 12, padding: 2 }}>&times;</button>
                </div>
              )}
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
