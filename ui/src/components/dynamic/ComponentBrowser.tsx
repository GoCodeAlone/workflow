import { useState, useEffect, useCallback } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import { listDynamicComponents, createDynamicComponent, deleteDynamicComponent } from '../../utils/api.ts';
import type { DynamicComponent } from '../../utils/api.ts';

export default function ComponentBrowser() {
  const [components, setComponents] = useState<DynamicComponent[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedComponent, setSelectedComponent] = useState<DynamicComponent | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newSource, setNewSource] = useState('');
  const [newLanguage, setNewLanguage] = useState('go');
  const [creating, setCreating] = useState(false);

  const addToast = useWorkflowStore((s) => s.addToast);
  const toggleComponentBrowser = useWorkflowStore((s) => s.toggleComponentBrowser);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const list = await listDynamicComponents();
      setComponents(list);
    } catch {
      // Server not available
      setComponents([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleCreate = async () => {
    if (!newName.trim() || !newSource.trim()) return;
    setCreating(true);
    try {
      await createDynamicComponent(newName.trim(), newSource, newLanguage);
      addToast(`Component "${newName}" created`, 'success');
      setShowCreate(false);
      setNewName('');
      setNewSource('');
      refresh();
    } catch (e) {
      addToast(`Create failed: ${(e as Error).message}`, 'error');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (name: string) => {
    try {
      await deleteDynamicComponent(name);
      addToast(`Component "${name}" deleted`, 'success');
      if (selectedComponent?.name === name) setSelectedComponent(null);
      refresh();
    } catch (e) {
      addToast(`Delete failed: ${(e as Error).message}`, 'error');
    }
  };

  const statusColors: Record<string, string> = {
    running: '#10b981',
    stopped: '#585b70',
    error: '#ef4444',
  };

  return (
    <div
      style={{
        width: 360,
        background: '#181825',
        borderLeft: '1px solid #313244',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ fontWeight: 700, fontSize: 14, color: '#cdd6f4' }}>Dynamic Components</span>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            onClick={refresh}
            style={{
              background: 'none',
              border: 'none',
              color: '#585b70',
              cursor: 'pointer',
              fontSize: 12,
            }}
          >
            Refresh
          </button>
          <button
            onClick={toggleComponentBrowser}
            style={{
              background: 'none',
              border: 'none',
              color: '#585b70',
              cursor: 'pointer',
              fontSize: 16,
              padding: '0 4px',
            }}
          >
            x
          </button>
        </div>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
        {/* Create button */}
        <button
          onClick={() => setShowCreate(!showCreate)}
          style={{
            width: '100%',
            padding: '8px 14px',
            background: showCreate ? '#45475a' : '#313244',
            border: '1px solid #45475a',
            borderRadius: 6,
            color: '#cdd6f4',
            fontSize: 12,
            fontWeight: 600,
            cursor: 'pointer',
            marginBottom: 12,
          }}
        >
          {showCreate ? 'Cancel' : '+ Create Component'}
        </button>

        {/* Create form */}
        {showCreate && (
          <div
            style={{
              background: '#1e1e2e',
              border: '1px solid #313244',
              borderRadius: 6,
              padding: 12,
              marginBottom: 16,
            }}
          >
            <label style={{ display: 'block', marginBottom: 10 }}>
              <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 3 }}>Name</span>
              <input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="my-component"
                style={inputStyle}
              />
            </label>
            <label style={{ display: 'block', marginBottom: 10 }}>
              <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 3 }}>Language</span>
              <select
                value={newLanguage}
                onChange={(e) => setNewLanguage(e.target.value)}
                style={inputStyle}
              >
                <option value="go">Go</option>
                <option value="javascript">JavaScript</option>
                <option value="wasm">WebAssembly</option>
              </select>
            </label>
            <label style={{ display: 'block', marginBottom: 10 }}>
              <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 3 }}>Source</span>
              <textarea
                value={newSource}
                onChange={(e) => setNewSource(e.target.value)}
                placeholder="package main..."
                rows={8}
                style={{ ...inputStyle, fontFamily: 'monospace', resize: 'vertical' }}
              />
            </label>
            <button
              onClick={handleCreate}
              disabled={creating || !newName.trim() || !newSource.trim()}
              style={{
                width: '100%',
                padding: '8px 14px',
                background: creating ? '#45475a' : '#89b4fa',
                border: 'none',
                borderRadius: 6,
                color: creating ? '#a6adc8' : '#1e1e2e',
                fontSize: 12,
                fontWeight: 600,
                cursor: creating ? 'default' : 'pointer',
              }}
            >
              {creating ? 'Creating...' : 'Create'}
            </button>
          </div>
        )}

        {/* Component list */}
        {loading && <div style={{ color: '#585b70', fontSize: 12 }}>Loading...</div>}

        {!loading && components.length === 0 && (
          <div style={{ color: '#585b70', fontSize: 12, textAlign: 'center', padding: 24 }}>
            No dynamic components loaded.
            {'\n'}Start the server to manage components.
          </div>
        )}

        {components.map((comp) => (
          <div
            key={comp.name}
            style={{
              background: selectedComponent?.name === comp.name ? '#313244' : '#1e1e2e',
              border: '1px solid #313244',
              borderRadius: 6,
              padding: '10px 12px',
              marginBottom: 8,
              cursor: 'pointer',
            }}
            onClick={() => setSelectedComponent(selectedComponent?.name === comp.name ? null : comp)}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  background: statusColors[comp.status] ?? '#585b70',
                  flexShrink: 0,
                }}
              />
              <span style={{ color: '#cdd6f4', fontSize: 12, fontWeight: 600, flex: 1 }}>
                {comp.name}
              </span>
              <span style={{ color: '#585b70', fontSize: 10 }}>{comp.language}</span>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  handleDelete(comp.name);
                }}
                style={{
                  background: 'none',
                  border: 'none',
                  color: '#f38ba8',
                  cursor: 'pointer',
                  fontSize: 11,
                  padding: '0 4px',
                }}
              >
                Delete
              </button>
            </div>
            <div style={{ color: '#585b70', fontSize: 10, marginTop: 4 }}>
              Status: {comp.status} | Loaded: {comp.loadedAt}
            </div>

            {/* Expanded source view */}
            {selectedComponent?.name === comp.name && comp.source && (
              <div style={{ marginTop: 8 }}>
                <span style={{ color: '#a6adc8', fontSize: 10, display: 'block', marginBottom: 4 }}>
                  Source
                </span>
                <textarea
                  readOnly
                  value={comp.source}
                  rows={10}
                  style={{
                    ...inputStyle,
                    fontFamily: 'monospace',
                    fontSize: 11,
                    resize: 'vertical',
                    cursor: 'text',
                  }}
                />
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 4,
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};
