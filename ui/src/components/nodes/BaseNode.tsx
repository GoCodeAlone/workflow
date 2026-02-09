import { type ReactNode } from 'react';
import { Handle, Position } from '@xyflow/react';
import { CATEGORY_COLORS, MODULE_TYPE_MAP } from '../../types/workflow.ts';
import type { ModuleCategory } from '../../types/workflow.ts';
import useWorkflowStore from '../../store/workflowStore.ts';

interface BaseNodeProps {
  id: string;
  label: string;
  moduleType: string;
  icon: ReactNode;
  preview?: string;
  hasInput?: boolean;
  hasOutput?: boolean;
  children?: ReactNode;
}

export default function BaseNode({
  id,
  label,
  moduleType,
  icon,
  preview,
  hasInput = true,
  hasOutput = true,
  children,
}: BaseNodeProps) {
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const info = MODULE_TYPE_MAP[moduleType];
  const category: ModuleCategory = info?.category ?? 'infrastructure';
  const color = CATEGORY_COLORS[category];
  const isSelected = selectedNodeId === id;

  return (
    <div
      onClick={() => setSelectedNode(id)}
      style={{
        background: '#1e1e2e',
        border: `2px solid ${isSelected ? '#fff' : color}`,
        borderRadius: 8,
        padding: 0,
        minWidth: 180,
        fontFamily: 'system-ui, sans-serif',
        fontSize: 12,
        color: '#cdd6f4',
        boxShadow: isSelected
          ? `0 0 0 2px ${color}40, 0 4px 12px rgba(0,0,0,0.4)`
          : '0 2px 8px rgba(0,0,0,0.3)',
        cursor: 'pointer',
      }}
    >
      {hasInput && (
        <Handle
          type="target"
          position={Position.Top}
          style={{ background: color, width: 10, height: 10, border: '2px solid #1e1e2e' }}
        />
      )}

      <div
        style={{
          background: `${color}20`,
          borderBottom: `1px solid ${color}40`,
          padding: '6px 10px',
          borderRadius: '6px 6px 0 0',
          display: 'flex',
          alignItems: 'center',
          gap: 6,
        }}
      >
        <span style={{ fontSize: 16 }}>{icon}</span>
        <span style={{ fontWeight: 600, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {label}
        </span>
      </div>

      <div style={{ padding: '6px 10px' }}>
        <span
          style={{
            background: `${color}30`,
            color,
            padding: '2px 6px',
            borderRadius: 4,
            fontSize: 10,
            fontWeight: 500,
          }}
        >
          {moduleType}
        </span>
        {preview && (
          <div style={{ marginTop: 4, color: '#a6adc8', fontSize: 11 }}>{preview}</div>
        )}
        {children}
      </div>

      {hasOutput && (
        <Handle
          type="source"
          position={Position.Bottom}
          style={{ background: color, width: 10, height: 10, border: '2px solid #1e1e2e' }}
        />
      )}
    </div>
  );
}
