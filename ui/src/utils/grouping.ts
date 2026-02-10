import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import { MODULE_TYPE_MAP, type ModuleCategory } from '../types/workflow.ts';

/**
 * Transform nodes/edges into container view:
 * - Group nodes by their module category
 * - Create parent group nodes for each category
 * - Set parentId on children, position them relative to parent
 * - Collapse inter-group edges into single container-to-container edges
 */
export function computeContainerView(
  nodes: WorkflowNode[],
  edges: Edge[],
): { nodes: WorkflowNode[]; edges: Edge[] } {
  // Group nodes by category
  const groups: Record<string, WorkflowNode[]> = {};
  for (const node of nodes) {
    const info = MODULE_TYPE_MAP[node.data.moduleType];
    const category = info?.category || 'infrastructure';
    if (!groups[category]) groups[category] = [];
    groups[category].push(node);
  }

  const newNodes: WorkflowNode[] = [];
  const nodeToGroup: Record<string, string> = {};
  const GROUP_WIDTH = 320;
  const GROUP_PADDING = 50;
  const NODE_HEIGHT = 80;

  let groupX = 50;
  for (const [category, categoryNodes] of Object.entries(groups)) {
    const groupId = `group-${category}`;
    const groupHeight = GROUP_PADDING + categoryNodes.length * (NODE_HEIGHT + 20) + 20;

    // Create group node
    newNodes.push({
      id: groupId,
      type: 'groupNode',
      position: { x: groupX, y: 50 },
      data: {
        label: category.charAt(0).toUpperCase() + category.slice(1),
        category: category as ModuleCategory,
        childCount: categoryNodes.length,
        collapsed: false,
        moduleType: 'group',
        config: {},
      },
      style: {
        width: GROUP_WIDTH,
        height: groupHeight,
      },
    } as WorkflowNode);

    // Position children relative to parent
    categoryNodes.forEach((node, i) => {
      nodeToGroup[node.id] = groupId;
      newNodes.push({
        ...node,
        position: { x: 20, y: GROUP_PADDING + i * (NODE_HEIGHT + 20) },
        parentId: groupId,
        extent: 'parent' as const,
      } as WorkflowNode);
    });

    groupX += GROUP_WIDTH + 60;
  }

  // Collapse edges: if source and target are in different groups, create group-to-group edge
  const groupEdgeSet = new Set<string>();
  const newEdges: Edge[] = [];

  for (const edge of edges) {
    const srcGroup = nodeToGroup[edge.source];
    const tgtGroup = nodeToGroup[edge.target];

    if (!srcGroup || !tgtGroup) continue;

    if (srcGroup === tgtGroup) {
      // Keep intra-group edges
      newEdges.push(edge);
    } else {
      const key = `${srcGroup}->${tgtGroup}`;
      if (!groupEdgeSet.has(key)) {
        groupEdgeSet.add(key);
        newEdges.push({
          id: `ge-${srcGroup}-${tgtGroup}`,
          source: srcGroup,
          target: tgtGroup,
          style: { stroke: '#585b70', strokeWidth: 3 },
          animated: true,
        });
      }
    }
  }

  return { nodes: newNodes, edges: newEdges };
}

/**
 * Strip group nodes and clear parentId to return to component view.
 */
export function computeComponentView(
  _nodes: WorkflowNode[],
  _edges: Edge[],
  originalNodes: WorkflowNode[],
  originalEdges: Edge[],
): { nodes: WorkflowNode[]; edges: Edge[] } {
  return { nodes: originalNodes, edges: originalEdges };
}
