import { useMemo } from 'react';
import type { NodeProps } from '@xyflow/react';
import type { WorkflowNode } from '../../store/workflowStore.ts';
import type { WorkflowEdgeData } from '../../types/workflow.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import BaseNode from './BaseNode.tsx';

/**
 * Compute this middleware's position in its chain by walking backwards
 * from the current node to the chain origin (the router), then counting
 * total middleware nodes in the chain.
 */
function useChainPosition(nodeId: string): { position: number; total: number } | null {
  const edges = useWorkflowStore((s) => s.edges);
  const nodes = useWorkflowStore((s) => s.nodes);

  return useMemo(() => {
    // Find all middleware-chain edges
    const chainEdges = edges.filter(
      (e) => (e.data as WorkflowEdgeData | undefined)?.edgeType === 'middleware-chain',
    );
    if (chainEdges.length === 0) return null;

    // Check if this node is part of any middleware chain
    const isTarget = chainEdges.some((e) => e.target === nodeId);
    const isSource = chainEdges.some((e) => e.source === nodeId);
    if (!isTarget && !isSource) return null;

    // Walk backward from this node to find the chain start (router)
    const backwardChain: string[] = [nodeId];
    let current = nodeId;
    const visited = new Set<string>();
    visited.add(current);

    while (true) {
      const incoming = chainEdges.find((e) => e.target === current && !visited.has(e.source));
      if (!incoming) break;
      visited.add(incoming.source);
      backwardChain.unshift(incoming.source);
      current = incoming.source;
    }

    // Walk forward from this node to find the chain end (handler)
    current = nodeId;
    const forwardChain: string[] = [];
    const visitedFwd = new Set<string>();
    visitedFwd.add(current);

    while (true) {
      const outgoing = chainEdges.find((e) => e.source === current && !visitedFwd.has(e.target));
      if (!outgoing) break;
      visitedFwd.add(outgoing.target);
      forwardChain.push(outgoing.target);
      current = outgoing.target;
    }

    // Full chain: backwardChain + forwardChain
    const fullChain = [...backwardChain, ...forwardChain];

    // Filter to only middleware nodes (exclude routers, handlers, etc.)
    const middlewareNodes = fullChain.filter((nid) => {
      const node = nodes.find((n) => n.id === nid);
      return node?.data.moduleType?.startsWith('http.middleware.');
    });

    if (middlewareNodes.length === 0) return null;

    const myIndex = middlewareNodes.indexOf(nodeId);
    if (myIndex === -1) return null;

    return { position: myIndex + 1, total: middlewareNodes.length };
  }, [edges, nodes, nodeId]);
}

export default function MiddlewareNode({ id, data }: NodeProps<WorkflowNode>) {
  const authType = (data.config?.type as string) || '';
  const level = (data.config?.level as string) || '';
  const rps = data.config?.rps as number | undefined;
  let preview: string | undefined;
  if (authType) preview = `auth: ${authType}`;
  else if (level) preview = `level: ${level}`;
  else if (rps) preview = `${rps} req/s`;

  const chainPos = useChainPosition(id);

  return (
    <div style={{ position: 'relative' }}>
      {chainPos && (
        <div
          style={{
            position: 'absolute',
            top: -8,
            right: -8,
            width: 24,
            height: 24,
            borderRadius: '50%',
            background: '#fab387',
            color: '#1e1e2e',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 10,
            fontWeight: 700,
            zIndex: 10,
            border: '2px solid #1e1e2e',
            boxShadow: '0 1px 4px rgba(0,0,0,0.4)',
            lineHeight: 1,
          }}
          title={`Middleware ${chainPos.position} of ${chainPos.total} in chain`}
        >
          {chainPos.position}/{chainPos.total}
        </div>
      )}
      <BaseNode
        id={id}
        label={data.label}
        moduleType={data.moduleType}
        icon={<ShieldIcon />}
        preview={preview}
      />
    </div>
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
