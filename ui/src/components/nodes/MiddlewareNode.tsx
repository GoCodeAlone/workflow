import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function MiddlewareNode({ id, data }: NodeProps<WorkflowNode>) {
  const authType = (data.config?.type as string) || '';
  const level = (data.config?.level as string) || '';
  const rps = data.config?.rps as number | undefined;
  let preview: string | undefined;
  if (authType) preview = `auth: ${authType}`;
  else if (level) preview = `level: ${level}`;
  else if (rps) preview = `${rps} req/s`;

  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<ShieldIcon />}
      preview={preview}
    />
  );
}

function ShieldIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path
        d="M8 1.5L2.5 4v4c0 3.5 2.5 5.5 5.5 6.5 3-1 5.5-3 5.5-6.5V4L8 1.5z"
        stroke="#06b6d4"
        strokeWidth="1.5"
        fill="none"
      />
    </svg>
  );
}
