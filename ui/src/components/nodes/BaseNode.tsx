import { type ReactNode, useState, useMemo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ModuleCategory, IOPort } from '../../types/workflow.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';

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

function IOPortList({ ports, direction, color }: { ports: IOPort[]; direction: 'in' | 'out'; color: string }) {
  const [expanded, setExpanded] = useState(ports.length <= 2);
  if (ports.length === 0) return null;

  const arrow = direction === 'in' ? '\u2192' : '\u2190';

  return (
    <div style={{ padding: '2px 0' }}>
      {!expanded && ports.length > 2 ? (
        <div
          onClick={(e) => { e.stopPropagation(); setExpanded(true); }}
          style={{ fontSize: 9, color: '#585b70', cursor: 'pointer', padding: '1px 0' }}
        >
          {arrow} {ports.length} {direction === 'in' ? 'inputs' : 'outputs'}
        </div>
      ) : (
        ports.map((port) => (
          <div
            key={port.name}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 3,
              fontSize: 9,
              color: '#585b70',
              padding: '1px 0',
            }}
          >
            <span
              style={{
                width: 5,
                height: 5,
                borderRadius: '50%',
                background: color,
                opacity: 0.6,
                flexShrink: 0,
              }}
            />
            <span style={{ color: '#a6adc8' }}>{port.name}</span>
            <span style={{ color: '#45475a' }}>{port.type}</span>
          </div>
        ))
      )}
    </div>
  );
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
  const connectingFrom = useWorkflowStore((s) => s.connectingFrom);
  const compatibleNodeIds = useWorkflowStore((s) => s.compatibleNodeIds);
  const moduleTypeMap = useModuleSchemaStore((s) => s.moduleTypeMap);
  const info = moduleTypeMap[moduleType];
  const category: ModuleCategory = info?.category ?? 'infrastructure';
  const color = CATEGORY_COLORS[category];
  const isSelected = selectedNodeId === id;
  const ioSig = info?.ioSignature;
  const edges = useWorkflowStore((s) => s.edges);

  // Pipeline chain position: find if this node is a target of a pipeline-flow edge
  const pipelinePosition = useMemo(() => {
    if (!moduleType.startsWith('step.')) return null;
    for (const edge of edges) {
      const edgeData = edge.data as Record<string, unknown> | undefined;
      if (edgeData?.edgeType === 'pipeline-flow' && edge.target === id) {
        return (edgeData.chainOrder as number) ?? null;
      }
    }
    return null;
  }, [moduleType, edges, id]);

  // Connection drag highlighting
  const isDragging = connectingFrom !== null;
  const isSource = connectingFrom?.nodeId === id;
  const isCompatible = isDragging && !isSource && compatibleNodeIds.includes(id);
  const isIncompatible = isDragging && !isSource && !compatibleNodeIds.includes(id);

  const nodeStyle = useMemo(() => {
    const base: React.CSSProperties = {
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
      transition: 'opacity 0.3s ease, box-shadow 0.3s ease, border-color 0.3s ease, filter 0.3s ease',
    };

    if (isSource) {
      base.boxShadow = `0 0 0 2px ${color}60, 0 4px 16px rgba(0,0,0,0.4)`;
    } else if (isCompatible) {
      base.border = `2px solid #22c55e`;
      base.boxShadow = `0 0 0 2px rgba(34, 197, 94, 0.3), 0 0 12px rgba(34, 197, 94, 0.2)`;
    } else if (isIncompatible) {
      base.opacity = 0.35;
      base.filter = 'saturate(0.3)';
    }

    return base;
  }, [isSelected, color, isSource, isCompatible, isIncompatible]);

  const targetHandleStyle = useMemo(() => {
    const base: React.CSSProperties = {
      background: color,
      width: 10,
      height: 10,
      border: '2px solid #1e1e2e',
      transition: 'width 0.2s ease, height 0.2s ease, box-shadow 0.2s ease',
    };
    if (isCompatible && connectingFrom?.handleType === 'source') {
      base.width = 14;
      base.height = 14;
      base.boxShadow = `0 0 8px rgba(34, 197, 94, 0.6)`;
      base.background = '#22c55e';
    }
    return base;
  }, [color, isCompatible, connectingFrom?.handleType]);

  const sourceHandleStyle = useMemo(() => {
    const base: React.CSSProperties = {
      background: color,
      width: 10,
      height: 10,
      border: '2px solid #1e1e2e',
      transition: 'width 0.2s ease, height 0.2s ease, box-shadow 0.2s ease',
    };
    if (isCompatible && connectingFrom?.handleType === 'target') {
      base.width = 14;
      base.height = 14;
      base.boxShadow = `0 0 8px rgba(34, 197, 94, 0.6)`;
      base.background = '#22c55e';
    }
    return base;
  }, [color, isCompatible, connectingFrom?.handleType]);

  return (
    <div
      onClick={() => setSelectedNode(id)}
      className={isCompatible ? 'connection-compatible' : undefined}
      style={nodeStyle}
    >
      {hasInput && (
        <Handle
          type="target"
          position={Position.Top}
          style={targetHandleStyle}
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
        {pipelinePosition !== null && (
          <span
            style={{
              width: 18,
              height: 18,
              borderRadius: '50%',
              background: '#e879f930',
              color: '#e879f9',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 10,
              fontWeight: 700,
              flexShrink: 0,
            }}
            title={`Pipeline step #${pipelinePosition}`}
          >
            {pipelinePosition}
          </span>
        )}
        <span style={{ fontWeight: 600, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {label}
        </span>
      </div>

      <div style={{ padding: '6px 10px' }}>
        {ioSig && ioSig.inputs.length > 0 && (
          <IOPortList ports={ioSig.inputs} direction="in" color={color} />
        )}
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
        {ioSig && ioSig.outputs.length > 0 && (
          <IOPortList ports={ioSig.outputs} direction="out" color={color} />
        )}
      </div>

      {hasOutput && (
        <Handle
          type="source"
          position={Position.Bottom}
          style={sourceHandleStyle}
        />
      )}
    </div>
  );
}
