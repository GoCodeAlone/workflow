import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function EventProcessorNode({ id, data }: NodeProps<WorkflowNode>) {
  const output = (data.config?.output as string) || '';
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<BoltIcon />}
      preview={output ? `output: ${output}` : undefined}
    />
  );
}

function BoltIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path d="M9 1L3 9h4l-1 6 6-8H8l1-6z" fill="#ef4444" opacity="0.8" />
    </svg>
  );
}
