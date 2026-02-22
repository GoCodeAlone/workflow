import { memo, useCallback } from 'react';
import { Handle, Position, type NodeProps, useReactFlow } from '@xyflow/react';
import { CATEGORY_COLORS, type ModuleCategory } from '../../types/workflow.ts';

interface GroupNodeData extends Record<string, unknown> {
  label: string;
  category: ModuleCategory;
  childCount: number;
  collapsed: boolean;
}

function GroupNode({ id, data }: NodeProps) {
  const d = data as GroupNodeData;
  const color = CATEGORY_COLORS[d.category] || '#64748b';
  const { setNodes } = useReactFlow();

  const toggleCollapse = useCallback(() => {
    setNodes((nds) =>
      nds.map((n) => {
        if (n.id === id) {
          const collapsed = !d.collapsed;
          return {
            ...n,
            data: { ...n.data, collapsed },
            style: collapsed
              ? { ...n.style, height: 40, overflow: 'hidden' as const }
              : { ...n.style, height: undefined, overflow: undefined },
          };
        }
        // Hide/show children
        if (n.parentId === id) {
          return {
            ...n,
            hidden: !d.collapsed,
          };
        }
        return n;
      }),
    );
  }, [id, d.collapsed, setNodes]);

  if (d.collapsed) {
    return (
      <div style={{
        background: `${color}20`,
        border: `2px solid ${color}60`,
        borderRadius: 8,
        minWidth: 200,
        height: 40,
        padding: 0,
      }}>
        <div style={{
          background: `${color}30`,
          padding: '6px 12px',
          borderRadius: '6px 6px 6px 6px',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          height: '100%',
        }}>
          <button
            onClick={toggleCollapse}
            style={{
              background: 'none',
              border: 'none',
              color,
              cursor: 'pointer',
              fontSize: 12,
              padding: 0,
              lineHeight: 1,
            }}
          >
            &#9654;
          </button>
          <span style={{ color, fontWeight: 700, fontSize: 13 }}>{d.label}</span>
          <span style={{
            background: color,
            color: '#1e1e2e',
            borderRadius: 10,
            padding: '1px 8px',
            fontSize: 11,
            fontWeight: 700,
          }}>
            {d.childCount} modules
          </span>
        </div>
        <Handle type="target" position={Position.Left} style={{ background: color }} />
        <Handle type="source" position={Position.Right} style={{ background: color }} />
      </div>
    );
  }

  return (
    <div style={{
      background: `${color}15`,
      border: `2px dashed ${color}60`,
      borderRadius: 12,
      minWidth: 200,
      minHeight: 100,
      padding: 0,
    }}>
      <div style={{
        background: `${color}30`,
        padding: '6px 12px',
        borderRadius: '10px 10px 0 0',
        display: 'flex',
        alignItems: 'center',
        gap: 8,
      }}>
        <button
          onClick={toggleCollapse}
          style={{
            background: 'none',
            border: 'none',
            color,
            cursor: 'pointer',
            fontSize: 12,
            padding: 0,
            lineHeight: 1,
          }}
        >
          &#9660;
        </button>
        <span style={{ color, fontWeight: 700, fontSize: 13 }}>{d.label}</span>
        <span style={{
          background: color,
          color: '#1e1e2e',
          borderRadius: 10,
          padding: '1px 8px',
          fontSize: 11,
          fontWeight: 700,
        }}>
          {d.childCount}
        </span>
      </div>
      <Handle type="target" position={Position.Left} style={{ background: color }} />
      <Handle type="source" position={Position.Right} style={{ background: color }} />
    </div>
  );
}

export default memo(GroupNode);
