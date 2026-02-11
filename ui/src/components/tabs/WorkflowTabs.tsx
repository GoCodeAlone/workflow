import { useState, useRef, useCallback, useEffect } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';

export default function WorkflowTabs() {
  const tabs = useWorkflowStore((s) => s.tabs);
  const activeTabId = useWorkflowStore((s) => s.activeTabId);
  const addTab = useWorkflowStore((s) => s.addTab);
  const closeTab = useWorkflowStore((s) => s.closeTab);
  const switchTab = useWorkflowStore((s) => s.switchTab);
  const renameTab = useWorkflowStore((s) => s.renameTab);
  const duplicateTab = useWorkflowStore((s) => s.duplicateTab);

  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState('');
  const [contextMenu, setContextMenu] = useState<{ tabId: string; x: number; y: number } | null>(null);
  const editInputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Focus input when editing starts
  useEffect(() => {
    if (editingTabId && editInputRef.current) {
      editInputRef.current.focus();
      editInputRef.current.select();
    }
  }, [editingTabId]);

  // Close context menu on outside click
  useEffect(() => {
    if (!contextMenu) return;
    const handler = () => setContextMenu(null);
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [contextMenu]);

  const startEditing = useCallback((tabId: string, name: string) => {
    setEditingTabId(tabId);
    setEditingName(name);
  }, []);

  const finishEditing = useCallback(() => {
    if (editingTabId && editingName.trim()) {
      renameTab(editingTabId, editingName.trim());
    }
    setEditingTabId(null);
  }, [editingTabId, editingName, renameTab]);

  const handleContextMenu = useCallback((e: React.MouseEvent, tabId: string) => {
    e.preventDefault();
    setContextMenu({ tabId, x: e.clientX, y: e.clientY });
  }, []);

  const scrollLeft = useCallback(() => {
    scrollRef.current?.scrollBy({ left: -150, behavior: 'smooth' });
  }, []);

  const scrollRight = useCallback(() => {
    scrollRef.current?.scrollBy({ left: 150, behavior: 'smooth' });
  }, []);

  return (
    <div style={{
      height: 32,
      background: '#181825',
      borderBottom: '1px solid #313244',
      display: 'flex',
      alignItems: 'stretch',
      position: 'relative',
      userSelect: 'none',
    }}>
      {/* Scroll left arrow */}
      <button
        onClick={scrollLeft}
        style={{
          background: 'none',
          border: 'none',
          color: '#585b70',
          cursor: 'pointer',
          padding: '0 4px',
          fontSize: 12,
          flexShrink: 0,
        }}
      >
        &#9664;
      </button>

      {/* Scrollable tab area */}
      <div
        ref={scrollRef}
        style={{
          display: 'flex',
          alignItems: 'stretch',
          overflow: 'hidden',
          flex: 1,
        }}
      >
        {tabs.map((tab) => {
          const isActive = tab.id === activeTabId;
          return (
            <div
              key={tab.id}
              onClick={() => switchTab(tab.id)}
              onDoubleClick={() => startEditing(tab.id, tab.name)}
              onContextMenu={(e) => handleContextMenu(e, tab.id)}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                padding: '0 12px',
                cursor: 'pointer',
                background: isActive ? '#1e1e2e' : 'transparent',
                borderBottom: isActive ? '2px solid #89b4fa' : '2px solid transparent',
                flexShrink: 0,
                minWidth: 0,
              }}
            >
              {editingTabId === tab.id ? (
                <input
                  ref={editInputRef}
                  value={editingName}
                  onChange={(e) => setEditingName(e.target.value)}
                  onBlur={finishEditing}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') finishEditing();
                    if (e.key === 'Escape') setEditingTabId(null);
                  }}
                  style={{
                    background: '#313244',
                    border: '1px solid #89b4fa',
                    borderRadius: 3,
                    color: '#cdd6f4',
                    fontSize: 11,
                    padding: '1px 4px',
                    width: 100,
                    outline: 'none',
                  }}
                />
              ) : (
                <span style={{
                  color: isActive ? '#cdd6f4' : '#6c7086',
                  fontSize: 11,
                  fontWeight: isActive ? 600 : 400,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  maxWidth: 120,
                }}>
                  {tab.name}
                  {tab.dirty ? ' *' : ''}
                </span>
              )}
              {tabs.length > 1 && (
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    closeTab(tab.id);
                  }}
                  style={{
                    background: 'none',
                    border: 'none',
                    color: '#585b70',
                    cursor: 'pointer',
                    fontSize: 12,
                    padding: '0 2px',
                    lineHeight: 1,
                    opacity: 0.6,
                  }}
                  onMouseEnter={(e) => { (e.target as HTMLElement).style.opacity = '1'; (e.target as HTMLElement).style.color = '#f38ba8'; }}
                  onMouseLeave={(e) => { (e.target as HTMLElement).style.opacity = '0.6'; (e.target as HTMLElement).style.color = '#585b70'; }}
                >
                  x
                </button>
              )}
            </div>
          );
        })}
      </div>

      {/* Scroll right arrow */}
      <button
        onClick={scrollRight}
        style={{
          background: 'none',
          border: 'none',
          color: '#585b70',
          cursor: 'pointer',
          padding: '0 4px',
          fontSize: 12,
          flexShrink: 0,
        }}
      >
        &#9654;
      </button>

      {/* Add tab button */}
      <button
        onClick={addTab}
        style={{
          background: 'none',
          border: 'none',
          borderLeft: '1px solid #313244',
          color: '#6c7086',
          cursor: 'pointer',
          padding: '0 10px',
          fontSize: 14,
          fontWeight: 700,
          flexShrink: 0,
        }}
        onMouseEnter={(e) => { (e.target as HTMLElement).style.color = '#89b4fa'; }}
        onMouseLeave={(e) => { (e.target as HTMLElement).style.color = '#6c7086'; }}
      >
        +
      </button>

      {/* Context menu */}
      {contextMenu && (
        <div style={{
          position: 'fixed',
          left: contextMenu.x,
          top: contextMenu.y,
          background: '#313244',
          border: '1px solid #45475a',
          borderRadius: 6,
          padding: '4px 0',
          zIndex: 1000,
          boxShadow: '0 4px 12px rgba(0,0,0,0.5)',
        }}>
          <ContextMenuItem
            label="Rename"
            onClick={() => {
              const tab = tabs.find((t) => t.id === contextMenu.tabId);
              if (tab) startEditing(tab.id, tab.name);
              setContextMenu(null);
            }}
          />
          <ContextMenuItem
            label="Duplicate"
            onClick={() => {
              duplicateTab(contextMenu.tabId);
              setContextMenu(null);
            }}
          />
          {tabs.length > 1 && (
            <ContextMenuItem
              label="Close"
              onClick={() => {
                closeTab(contextMenu.tabId);
                setContextMenu(null);
              }}
              danger
            />
          )}
        </div>
      )}
    </div>
  );
}

function ContextMenuItem({ label, onClick, danger }: { label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button
      onClick={onClick}
      style={{
        display: 'block',
        width: '100%',
        background: 'none',
        border: 'none',
        color: danger ? '#f38ba8' : '#cdd6f4',
        padding: '6px 16px',
        fontSize: 12,
        textAlign: 'left',
        cursor: 'pointer',
      }}
      onMouseEnter={(e) => { (e.target as HTMLElement).style.background = '#45475a'; }}
      onMouseLeave={(e) => { (e.target as HTMLElement).style.background = 'none'; }}
    >
      {label}
    </button>
  );
}
