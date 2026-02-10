import { describe, it, expect } from 'vitest';
import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import { computeContainerView, computeComponentView } from './grouping.ts';

function makeNode(id: string, moduleType: string): WorkflowNode {
  return {
    id,
    type: 'infrastructureNode',
    position: { x: 0, y: 0 },
    data: {
      moduleType,
      label: id,
      config: {},
    },
  };
}

function makeEdge(source: string, target: string): Edge {
  return { id: `e-${source}-${target}`, source, target };
}

describe('computeContainerView', () => {
  it('returns empty output for empty input', () => {
    const result = computeContainerView([], []);
    expect(result.nodes).toHaveLength(0);
    expect(result.edges).toHaveLength(0);
  });

  it('creates group nodes for each category', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('broker1', 'messaging.broker'),
    ];
    const result = computeContainerView(nodes, []);

    // Should have 2 group nodes + 2 child nodes = 4
    expect(result.nodes).toHaveLength(4);

    const groupNodes = result.nodes.filter((n) => n.type === 'groupNode');
    expect(groupNodes).toHaveLength(2);

    const groupCategories = groupNodes.map((n) => n.data.category);
    expect(groupCategories).toContain('http');
    expect(groupCategories).toContain('messaging');
  });

  it('sets parentId on children to their group', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('router1', 'http.router'),
    ];
    const result = computeContainerView(nodes, []);

    const children = result.nodes.filter((n) => n.type !== 'groupNode');
    expect(children).toHaveLength(2);
    for (const child of children) {
      expect(child.parentId).toBe('group-http');
    }
  });

  it('collapses inter-group edges to group-to-group edges', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('broker1', 'messaging.broker'),
    ];
    const edges: Edge[] = [makeEdge('server1', 'broker1')];

    const result = computeContainerView(nodes, edges);

    // Should have one group-to-group edge
    expect(result.edges).toHaveLength(1);
    expect(result.edges[0].source).toBe('group-http');
    expect(result.edges[0].target).toBe('group-messaging');
  });

  it('preserves intra-group edges', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('router1', 'http.router'),
    ];
    const edges: Edge[] = [makeEdge('server1', 'router1')];

    const result = computeContainerView(nodes, edges);

    // Intra-group edge should be preserved as-is
    expect(result.edges).toHaveLength(1);
    expect(result.edges[0].source).toBe('server1');
    expect(result.edges[0].target).toBe('router1');
  });

  it('deduplicates inter-group edges', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('router1', 'http.router'),
      makeNode('broker1', 'messaging.broker'),
      makeNode('handler1', 'messaging.handler'),
    ];
    const edges: Edge[] = [
      makeEdge('server1', 'broker1'),
      makeEdge('router1', 'handler1'),
    ];

    const result = computeContainerView(nodes, edges);

    // Both edges cross http->messaging, should collapse to one
    const interGroupEdges = result.edges.filter((e) => e.source.startsWith('group-'));
    expect(interGroupEdges).toHaveLength(1);
    expect(interGroupEdges[0].source).toBe('group-http');
    expect(interGroupEdges[0].target).toBe('group-messaging');
  });

  it('sets correct childCount on group nodes', () => {
    const nodes: WorkflowNode[] = [
      makeNode('server1', 'http.server'),
      makeNode('router1', 'http.router'),
      makeNode('handler1', 'http.handler'),
      makeNode('broker1', 'messaging.broker'),
    ];
    const result = computeContainerView(nodes, []);

    const httpGroup = result.nodes.find((n) => n.id === 'group-http');
    const msgGroup = result.nodes.find((n) => n.id === 'group-messaging');

    expect(httpGroup?.data.childCount).toBe(3);
    expect(msgGroup?.data.childCount).toBe(1);
  });

  it('assigns unknown module types to infrastructure category', () => {
    const nodes: WorkflowNode[] = [makeNode('custom1', 'some.unknown.type')];
    const result = computeContainerView(nodes, []);

    const groupNodes = result.nodes.filter((n) => n.type === 'groupNode');
    expect(groupNodes).toHaveLength(1);
    expect(groupNodes[0].data.category).toBe('infrastructure');
  });
});

describe('computeComponentView', () => {
  it('returns the original nodes and edges', () => {
    const origNodes: WorkflowNode[] = [makeNode('a', 'http.server')];
    const origEdges: Edge[] = [makeEdge('a', 'b')];

    const result = computeComponentView([], [], origNodes, origEdges);
    expect(result.nodes).toBe(origNodes);
    expect(result.edges).toBe(origEdges);
  });
});
