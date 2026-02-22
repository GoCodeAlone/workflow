import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

export default function SchedulerNode({ id, data }: NodeProps<WorkflowNode>) {
  const interval = (data.config?.interval as string) || '';
  const cron = (data.config?.cron as string) || '';
  const preview = cron ? `cron: ${cron}` : interval ? `every ${interval}` : undefined;
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<ClockIcon />}
      preview={preview}
    />
  );
}

function ClockIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <circle cx="8" cy="8" r="6.5" stroke="#6366f1" strokeWidth="1.5" />
      <path d="M8 4v4l3 2" stroke="#6366f1" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}
