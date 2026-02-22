import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

function routePreview(data: WorkflowNode['data']): string {
  const routes = data.handlerRoutes;
  if (!routes || routes.length === 0) {
    return (data.config?.prefix as string) || (data.config?.path as string) || '/';
  }
  const methods = [...new Set(routes.map((r) => r.method))];
  return `${routes.length} route${routes.length !== 1 ? 's' : ''} (${methods.join(', ')})`;
}

export default function HTTPRouterNode({ id, data }: NodeProps<WorkflowNode>) {
  const preview = routePreview(data);
  return (
    <BaseNode
      id={id}
      label={data.label}
      moduleType={data.moduleType}
      icon={<RouterIcon />}
      preview={preview}
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
