import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import { MODULE_TYPE_MAP as STATIC_MODULE_TYPE_MAP, CATEGORY_COLORS, type ModuleCategory } from '../types/workflow.ts';
import useModuleSchemaStore from '../store/moduleSchemaStore.ts';

function getModuleTypeMap() {
  const store = useModuleSchemaStore.getState();
  return store.loaded ? store.moduleTypeMap : STATIC_MODULE_TYPE_MAP;
}

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
    const info = getModuleTypeMap()[node.data.moduleType];
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

/**
 * Auto-group orphaned nodes into category-based containers.
 * Orphans = nodes with 0 incoming AND 0 outgoing edges (excluding group nodes).
 * Groups orphans by category and creates GroupNode parents for groups of 2+.
 */
export function autoGroupOrphanedNodes(
  nodes: WorkflowNode[],
  edges: Edge[],
): { nodes: WorkflowNode[]; edges: Edge[] } {
  // Find connected node IDs
  const connectedNodes = new Set<string>();
  for (const edge of edges) {
    connectedNodes.add(edge.source);
    connectedNodes.add(edge.target);
  }

  // Find orphan nodes (no edges, not a group node, no existing parent)
  const orphans = nodes.filter(
    (n) =>
      !connectedNodes.has(n.id) &&
      n.type !== 'groupNode' &&
      !n.parentId,
  );

  if (orphans.length === 0) {
    return { nodes, edges };
  }

  // Category name mappings for group labels
  const categoryLabels: Partial<Record<ModuleCategory, string>> = {
    observability: 'Monitoring',
    infrastructure: 'Infrastructure',
    events: 'Events',
  };

  // Group orphans by category
  const orphansByCategory: Record<string, WorkflowNode[]> = {};
  for (const orphan of orphans) {
    const info = getModuleTypeMap()[orphan.data.moduleType];
    const category = info?.category || 'infrastructure';
    if (!orphansByCategory[category]) orphansByCategory[category] = [];
    orphansByCategory[category].push(orphan);
  }

  const orphanIds = new Set(orphans.map((n) => n.id));
  // Keep all non-orphan nodes, plus orphans that won't be grouped (single orphans)
  const resultNodes: WorkflowNode[] = nodes.filter((n) => !orphanIds.has(n.id));

  const GROUP_WIDTH = 320;
  const GROUP_PADDING = 50;
  const NODE_HEIGHT = 80;

  // Find rightmost x position to place groups after existing content
  let maxX = 0;
  for (const n of nodes) {
    if (n.position.x > maxX) maxX = n.position.x;
  }
  let groupX = maxX + 400;

  for (const [category, catOrphans] of Object.entries(orphansByCategory)) {
    if (catOrphans.length < 2) {
      // Single orphan: keep as-is, no grouping
      resultNodes.push(...catOrphans);
      continue;
    }

    const groupId = `autogroup-${category}`;
    const groupHeight = GROUP_PADDING + catOrphans.length * (NODE_HEIGHT + 20) + 20;
    const color = CATEGORY_COLORS[category as ModuleCategory] || '#64748b';

    // Create group node
    resultNodes.push({
      id: groupId,
      type: 'groupNode',
      position: { x: groupX, y: 50 },
      data: {
        label: categoryLabels[category as ModuleCategory] || (category.charAt(0).toUpperCase() + category.slice(1)),
        category: category as ModuleCategory,
        childCount: catOrphans.length,
        collapsed: false,
        moduleType: 'group',
        config: {},
      },
      style: {
        width: GROUP_WIDTH,
        height: groupHeight,
        background: `${color}10`,
      },
    } as WorkflowNode);

    // Add children with parentId set
    catOrphans.forEach((orphan, i) => {
      resultNodes.push({
        ...orphan,
        position: { x: 20, y: GROUP_PADDING + i * (NODE_HEIGHT + 20) },
        parentId: groupId,
        extent: 'parent' as const,
      } as WorkflowNode);
    });

    groupX += GROUP_WIDTH + 60;
  }

  return { nodes: resultNodes, edges };
}
