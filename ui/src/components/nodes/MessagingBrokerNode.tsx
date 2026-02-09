import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function MessagingBrokerNode({ id, data }: NodeProps<WorkflowNode>) {
  const provider = (data.config?.provider as string) || '';
  const topic = (data.config?.topic as string) || '';
  const preview = topic ? `${provider} / ${topic}` : provider;
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<MsgIcon />}
      preview={preview || undefined}
    />
  );
}

function MsgIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <rect x="1" y="3" width="14" height="10" rx="2" stroke="#8b5cf6" strokeWidth="1.5" />
      <path d="M1 5l7 4 7-4" stroke="#8b5cf6" strokeWidth="1.2" />
    </svg>
  );
}
