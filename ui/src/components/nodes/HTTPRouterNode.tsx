import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function HTTPRouterNode({ id, data }: NodeProps<WorkflowNode>) {
  const prefix = (data.config?.prefix as string) || '/';
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<RouterIcon />}
      preview={prefix}
    />
  );
}

function RouterIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path d="M8 2v4M8 6L3 12M8 6l5 6" stroke="#3b82f6" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="3" cy="13" r="1.5" fill="#3b82f6" />
      <circle cx="8" cy="2" r="1.5" fill="#3b82f6" />
      <circle cx="13" cy="13" r="1.5" fill="#3b82f6" />
    </svg>
  );
}
