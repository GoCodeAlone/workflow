import { useState, useMemo, useCallback } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';

interface MiddlewareChainEditorProps {
  nodeId: string;
  middlewareChain: string[];
  onChange: (chain: string[]) => void;
}

export default function MiddlewareChainEditor({
  nodeId,
  middlewareChain,
  onChange,
}: MiddlewareChainEditorProps) {
  const nodes = useWorkflowStore((s) => s.nodes);
  const [dragIdx, setDragIdx] = useState<number | null>(null);

  // Find all middleware nodes on the canvas that are not already in the chain
  const availableMiddleware = useMemo(() => {
    const chainSet = new Set(middlewareChain);
    return nodes
      .filter(
        (n) =>
          n.data.moduleType.startsWith('http.middleware.') &&
          n.id !== nodeId &&
          !chainSet.has(n.data.label),
      )
      .map((n) => ({ id: n.id, label: n.data.label }));
  }, [nodes, nodeId, middlewareChain]);

  const handleRemove = useCallback(
    (idx: number) => {
      onChange(middlewareChain.filter((_, i) => i !== idx));
    },
    [middlewareChain, onChange],
  );

  const handleAdd = useCallback(
    (name: string) => {
      onChange([...middlewareChain, name]);
    },
    [middlewareChain, onChange],
  );

  const handleDragStart = (idx: number) => {
    setDragIdx(idx);
  };

  const handleDragOver = (e: React.DragEvent, idx: number) => {
    e.preventDefault();
    if (dragIdx === null || dragIdx === idx) return;
    const updated = [...middlewareChain];
    const [moved] = updated.splice(dragIdx, 1);
    updated.splice(idx, 0, moved);
    setDragIdx(idx);
    onChange(updated);
  };

  const handleDragEnd = () => {
    setDragIdx(null);
  };

  const handleMoveUp = (idx: number) => {
    if (idx === 0) return;
    const updated = [...middlewareChain];
    [updated[idx - 1], updated[idx]] = [updated[idx], updated[idx - 1]];
    onChange(updated);
  };

  const handleMoveDown = (idx: number) => {
    if (idx >= middlewareChain.length - 1) return;
    const updated = [...middlewareChain];
    [updated[idx], updated[idx + 1]] = [updated[idx + 1], updated[idx]];
    onChange(updated);
  };

  return (
    <div style={{ marginBottom: 16 }}>
      <span
        style={{
          color: '#a6adc8',
          fontSize: 11,
          display: 'block',
          marginBottom: 6,
          fontWeight: 600,
        }}
      >
        Middleware Chain
      </span>

      {middlewareChain.length === 0 && (
        <div style={{ color: '#585b70', fontSize: 11, marginBottom: 6 }}>
          No middleware attached. Add middleware below.
        </div>
      )}

      {middlewareChain.map((mw, idx) => (
        <div
          key={`${mw}-${idx}`}
          draggable
          onDragStart={() => handleDragStart(idx)}
          onDragOver={(e) => handleDragOver(e, idx)}
          onDragEnd={handleDragEnd}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 4,
            padding: '4px 6px',
            marginBottom: 2,
            background: dragIdx === idx ? '#313244' : '#1e1e2e',
            borderRadius: 4,
            border: '1px solid #313244',
            cursor: 'grab',
            fontSize: 11,
          }}
        >
          <span
            style={{
              width: 18,
              height: 18,
              borderRadius: '50%',
              background: '#fab387',
              color: '#1e1e2e',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 10,
              fontWeight: 700,
              flexShrink: 0,
            }}
          >
            {idx + 1}
          </span>
          <span style={{ flex: 1, color: '#cdd6f4', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {mw}
          </span>
          <button
            onClick={() => handleMoveUp(idx)}
            disabled={idx === 0}
            style={arrowBtnStyle}
            title="Move up"
          >
            &#9650;
          </button>
          <button
            onClick={() => handleMoveDown(idx)}
            disabled={idx >= middlewareChain.length - 1}
            style={arrowBtnStyle}
            title="Move down"
          >
            &#9660;
          </button>
          <button
            onClick={() => handleRemove(idx)}
            style={{
              background: 'none',
              border: 'none',
              color: '#f38ba8',
              cursor: 'pointer',
              fontSize: 11,
              padding: '0 2px',
            }}
            title="Remove"
          >
            x
          </button>
        </div>
      ))}

      {availableMiddleware.length > 0 && (
        <div style={{ marginTop: 6 }}>
          <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginBottom: 4 }}>
            Available middleware:
          </span>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {availableMiddleware.map((mw) => (
              <button
                key={mw.id}
                onClick={() => handleAdd(mw.label)}
                style={{
                  padding: '2px 8px',
                  background: '#313244',
                  border: '1px solid #45475a',
                  borderRadius: 4,
                  color: '#fab387',
                  cursor: 'pointer',
                  fontSize: 10,
                  fontWeight: 500,
                }}
                title={`Add ${mw.label} to chain`}
              >
                + {mw.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

const arrowBtnStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#6c7086',
  cursor: 'pointer',
  fontSize: 8,
  padding: '0 2px',
  lineHeight: 1,
};
