import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function HTTPServerNode({ id, data }: NodeProps<WorkflowNode>) {
  const addr = (data.config?.address as string) || ':8080';
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<HttpIcon />}
      preview={addr}
      hasInput={false}
    />
  );
}

function HttpIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <circle cx="8" cy="8" r="7" stroke="#3b82f6" strokeWidth="1.5" />
      <path d="M3 8h10M8 3c-1.5 2-1.5 8 0 10M8 3c1.5 2 1.5 8 0 10" stroke="#3b82f6" strokeWidth="1" />
    </svg>
  );
}
