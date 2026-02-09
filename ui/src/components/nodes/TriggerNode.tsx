import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function TriggerNode({ id, data }: NodeProps<WorkflowNode>) {
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<TriggerIcon />}
      hasInput={false}
    />
  );
}

function TriggerIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <polygon points="8,1 15,8 8,15 1,8" stroke="#ef4444" strokeWidth="1.5" fill="none" />
    </svg>
  );
}
