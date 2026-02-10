import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { CATEGORY_COLORS, type ModuleCategory } from '../../types/workflow.ts';

interface GroupNodeData extends Record<string, unknown> {
  label: string;
  category: ModuleCategory;
  childCount: number;
  collapsed: boolean;
}

function GroupNode({ data }: NodeProps) {
  const d = data as GroupNodeData;
  const color = CATEGORY_COLORS[d.category] || '#64748b';

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
