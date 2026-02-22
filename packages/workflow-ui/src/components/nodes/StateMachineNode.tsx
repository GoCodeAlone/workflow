import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function StateMachineNode({ id, data }: NodeProps<WorkflowNode>) {
  const initial = (data.config?.initialState as string) || '';
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<SMIcon />}
      preview={initial ? `initial: ${initial}` : undefined}
    />
  );
}

function SMIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <circle cx="4" cy="8" r="3" stroke="#f59e0b" strokeWidth="1.5" />
      <circle cx="12" cy="8" r="3" stroke="#f59e0b" strokeWidth="1.5" />
      <path d="M7 8h2" stroke="#f59e0b" strokeWidth="1.5" markerEnd="url(#arr)" />
    </svg>
  );
}
