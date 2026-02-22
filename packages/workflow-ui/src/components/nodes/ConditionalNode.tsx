import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import { CATEGORY_COLORS } from '../../types/workflow.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';

export default function ConditionalNode({ id, data }: NodeProps<WorkflowNode>) {
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const isSelected = selectedNodeId === id;
  const color = CATEGORY_COLORS.statemachine;
  const expression = (data.config?.expression as string) || '?';

  const moduleTypeMap = useModuleSchemaStore((s) => s.moduleTypeMap);
  const info = moduleTypeMap[data.moduleType];
  const conditionType = data.moduleType === 'conditional.ifelse'
    ? 'ifelse'
    : data.moduleType === 'conditional.switch'
      ? 'switch'
      : 'expression';

  const cases = (data.config?.cases as string[]) ?? [];
  const outputLabels = (data.config?.outputs as string[]) ?? [];

  // Determine output handles
  let outputs: { id: string; label: string; position: Position }[];
  if (conditionType === 'ifelse') {
    outputs = [
      { id: 'true', label: 'true', position: Position.Right },
      { id: 'false', label: 'false', position: Position.Bottom },
    ];
  } else if (conditionType === 'switch') {
    outputs = cases.length > 0
      ? cases.map((c, i) => ({
          id: `case-${i}`,
          label: c,
          position: Position.Bottom,
        }))
      : [{ id: 'default', label: 'default', position: Position.Bottom }];
  } else {
    outputs = outputLabels.length > 0
      ? outputLabels.map((l, i) => ({
          id: `out-${i}`,
          label: l,
          position: Position.Bottom,
        }))
      : [{ id: 'result', label: 'result', position: Position.Bottom }];
  }

  const diamondSize = 100;

  return (
    <div
      onClick={() => setSelectedNode(id)}
      style={{ position: 'relative', width: diamondSize + 40, height: diamondSize + 40, cursor: 'pointer' }}
    >
      {/* Input handle at top */}
      <Handle
        type="target"
        position={Position.Top}
        style={{
          background: color,
          width: 10,
          height: 10,
          border: '2px solid #1e1e2e',
          top: 0,
          left: '50%',
          transform: 'translateX(-50%)',
        }}
      />

      {/* Diamond shape */}
      <div
        style={{
          position: 'absolute',
          top: 20,
          left: 20,
          width: diamondSize,
          height: diamondSize,
          transform: 'rotate(45deg)',
          background: '#1e1e2e',
          border: `2px solid ${isSelected ? '#fff' : color}`,
          boxShadow: isSelected
            ? `0 0 0 2px ${color}40, 0 4px 12px rgba(0,0,0,0.4)`
            : '0 2px 8px rgba(0,0,0,0.3)',
        }}
      />

      {/* Inner content (counter-rotated) */}
      <div
        style={{
          position: 'absolute',
          top: 20,
          left: 20,
          width: diamondSize,
          height: diamondSize,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#cdd6f4',
          fontFamily: 'system-ui, sans-serif',
          fontSize: 10,
          textAlign: 'center',
          pointerEvents: 'none',
          zIndex: 1,
        }}
      >
        <span style={{ fontSize: 14, fontWeight: 600, color, marginBottom: 2 }}>
          {conditionType === 'ifelse' ? 'IF' : conditionType === 'switch' ? 'SW' : 'EX'}
        </span>
        <span
          style={{
            fontSize: 9,
            maxWidth: 60,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            color: '#a6adc8',
          }}
        >
          {expression}
        </span>
      </div>

      {/* Label below diamond */}
      <div
        style={{
          position: 'absolute',
          bottom: -16,
          left: 0,
          width: '100%',
          textAlign: 'center',
          fontSize: 10,
          color: '#a6adc8',
          fontFamily: 'system-ui, sans-serif',
          whiteSpace: 'nowrap',
        }}
      >
        {info?.label ?? data.label}
      </div>

      {/* Output handles */}
      {outputs.map((out, i) => {
        if (conditionType === 'ifelse') {
          // true -> right, false -> bottom
          if (out.id === 'true') {
            return (
              <Handle
                key={out.id}
                id={out.id}
                type="source"
                position={Position.Right}
                style={{
                  background: '#22c55e',
                  width: 8,
                  height: 8,
                  border: '2px solid #1e1e2e',
                  right: 0,
                  top: '50%',
                  transform: 'translateY(-50%)',
                }}
              />
            );
          }
          return (
            <Handle
              key={out.id}
              id={out.id}
              type="source"
              position={Position.Bottom}
              style={{
                background: '#ef4444',
                width: 8,
                height: 8,
                border: '2px solid #1e1e2e',
                bottom: 0,
                left: '50%',
                transform: 'translateX(-50%)',
              }}
            />
          );
        }

        // Switch / expression: spread outputs along bottom
        const total = outputs.length;
        const offset = total > 1 ? (i / (total - 1)) * 80 + 10 : 50;
        return (
          <Handle
            key={out.id}
            id={out.id}
            type="source"
            position={Position.Bottom}
            style={{
              background: color,
              width: 8,
              height: 8,
              border: '2px solid #1e1e2e',
              bottom: 0,
              left: `${offset}%`,
              transform: 'translateX(-50%)',
            }}
          />
        );
      })}
    </div>
  );
}
