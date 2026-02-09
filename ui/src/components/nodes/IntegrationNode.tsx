import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function IntegrationNode({ id, data }: NodeProps<WorkflowNode>) {
  const baseURL = (data.config?.baseURL as string) || '';
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<LinkIcon />}
      preview={baseURL || undefined}
    />
  );
}

function LinkIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path
        d="M6.5 9.5l3-3M5 11a2.83 2.83 0 01-1-4l2-2a2.83 2.83 0 014 0M11 5a2.83 2.83 0 011 4l-2 2a2.83 2.83 0 01-4 0"
        stroke="#10b981"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}
