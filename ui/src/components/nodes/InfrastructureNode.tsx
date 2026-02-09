import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function InfrastructureNode({ id, data }: NodeProps<WorkflowNode>) {
  const driver = (data.config?.driver as string) || '';
  const provider = (data.config?.provider as string) || '';
  const preview = driver || provider || undefined;
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<ServerIcon />}
      preview={preview}
    />
  );
}

function ServerIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <rect x="2" y="2" width="12" height="4" rx="1" stroke="#64748b" strokeWidth="1.5" />
      <rect x="2" y="10" width="12" height="4" rx="1" stroke="#64748b" strokeWidth="1.5" />
      <circle cx="5" cy="4" r="1" fill="#64748b" />
      <circle cx="5" cy="12" r="1" fill="#64748b" />
      <path d="M8 6v4" stroke="#64748b" strokeWidth="1" strokeDasharray="2 1" />
    </svg>
  );
}
