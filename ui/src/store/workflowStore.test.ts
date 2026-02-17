import { describe, it, expect, beforeEach } from 'vitest';
import { act } from '@testing-library/react';
import useWorkflowStore from './workflowStore.ts';

function getStore() {
  return useWorkflowStore.getState();
}

function resetStore() {
  useWorkflowStore.setState({
    nodes: [],
    edges: [],
    selectedNodeId: null,
    nodeCounter: 0,
    undoStack: [],
    redoStack: [],
    toasts: [],
    showAIPanel: false,
    showComponentBrowser: false,
  });
}

describe('workflowStore', () => {
  beforeEach(() => {
    resetStore();
  });

  describe('addNode', () => {
    it('creates a node with correct type, config, and position', () => {
      act(() => {
        getStore().addNode('http.server', { x: 100, y: 200 });
      });

      const { nodes, nodeCounter } = getStore();
      expect(nodes).toHaveLength(1);
      expect(nodeCounter).toBe(1);

      const node = nodes[0];
      expect(node.id).toBe('http_server_1');
      expect(node.type).toBe('httpNode');
      expect(node.position).toEqual({ x: 100, y: 200 });
      expect(node.data.moduleType).toBe('http.server');
      expect(node.data.label).toBe('HTTP Server 1');
      expect(node.data.config).toEqual({ address: ':8080' });
    });

    it('increments nodeCounter for each added node', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const { nodes, nodeCounter } = getStore();
      expect(nodes).toHaveLength(2);
      expect(nodeCounter).toBe(2);
      expect(nodes[0].id).toBe('http_server_1');
      expect(nodes[1].id).toBe('http_router_2');
    });

    it('does nothing for unknown module types', () => {
      act(() => {
        getStore().addNode('nonexistent.type', { x: 0, y: 0 });
      });

      expect(getStore().nodes).toHaveLength(0);
      expect(getStore().nodeCounter).toBe(0);
    });

    it('maps middleware types to middlewareNode component', () => {
      act(() => {
        getStore().addNode('http.middleware.auth', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('middlewareNode');
    });

    it('maps messaging types to messagingNode component', () => {
      act(() => {
        getStore().addNode('messaging.broker', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('messagingNode');
    });

    it('maps statemachine types to stateMachineNode component', () => {
      act(() => {
        getStore().addNode('statemachine.engine', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('stateMachineNode');
    });

    it('maps scheduler to schedulerNode component', () => {
      act(() => {
        getStore().addNode('scheduler.modular', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('schedulerNode');
    });

    it('maps notification.slack to integrationNode component', () => {
      act(() => {
        getStore().addNode('notification.slack', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('integrationNode');
    });

    it('maps step types to integrationNode component', () => {
      act(() => {
        getStore().addNode('step.validate', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('integrationNode');
    });

    it('maps fallback types to infrastructureNode component', () => {
      act(() => {
        getStore().addNode('database.modular', { x: 0, y: 0 });
      });

      expect(getStore().nodes[0].type).toBe('infrastructureNode');
    });

    it('pushes history before adding a node', () => {
      expect(getStore().undoStack).toHaveLength(0);

      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      expect(getStore().undoStack).toHaveLength(1);
    });
  });

  describe('removeNode', () => {
    it('removes a node from the nodes array', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const nodeId = getStore().nodes[0].id;

      act(() => {
        getStore().removeNode(nodeId);
      });

      expect(getStore().nodes).toHaveLength(1);
      expect(getStore().nodes[0].data.moduleType).toBe('http.router');
    });

    it('removes connected edges when a node is removed', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const [node1, node2] = getStore().nodes;

      act(() => {
        getStore().onConnect({
          source: node1.id,
          target: node2.id,
          sourceHandle: null,
          targetHandle: null,
        });
      });

      expect(getStore().edges).toHaveLength(1);

      act(() => {
        getStore().removeNode(node1.id);
      });

      expect(getStore().edges).toHaveLength(0);
    });

    it('clears selectedNodeId if the removed node was selected', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const nodeId = getStore().nodes[0].id;

      act(() => {
        getStore().setSelectedNode(nodeId);
      });

      expect(getStore().selectedNodeId).toBe(nodeId);

      act(() => {
        getStore().removeNode(nodeId);
      });

      expect(getStore().selectedNodeId).toBeNull();
    });

    it('does not clear selectedNodeId if a different node was selected', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const [node1, node2] = getStore().nodes;

      act(() => {
        getStore().setSelectedNode(node2.id);
        getStore().removeNode(node1.id);
      });

      expect(getStore().selectedNodeId).toBe(node2.id);
    });
  });

  describe('updateNodeConfig', () => {
    it('updates config fields on the correct node', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const nodeId = getStore().nodes[0].id;

      act(() => {
        getStore().updateNodeConfig(nodeId, { address: ':9090' });
      });

      expect(getStore().nodes[0].data.config.address).toBe(':9090');
    });

    it('merges config without overwriting other fields', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const nodeId = getStore().nodes[0].id;

      act(() => {
        getStore().updateNodeConfig(nodeId, { readTimeout: '60s' });
      });

      expect(getStore().nodes[0].data.config.address).toBe(':8080');
      expect(getStore().nodes[0].data.config.readTimeout).toBe('60s');
    });

    it('does not affect other nodes', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const [node1] = getStore().nodes;

      act(() => {
        getStore().updateNodeConfig(node1.id, { address: ':9090' });
      });

      expect(getStore().nodes[1].data.config).toEqual({});
    });
  });

  describe('updateNodeName', () => {
    it('updates the label of the specified node', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const nodeId = getStore().nodes[0].id;

      act(() => {
        getStore().updateNodeName(nodeId, 'My Server');
      });

      expect(getStore().nodes[0].data.label).toBe('My Server');
    });
  });

  describe('onConnect', () => {
    it('creates an edge between two nodes', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const [node1, node2] = getStore().nodes;

      act(() => {
        getStore().onConnect({
          source: node1.id,
          target: node2.id,
          sourceHandle: null,
          targetHandle: null,
        });
      });

      expect(getStore().edges).toHaveLength(1);
      expect(getStore().edges[0].source).toBe(node1.id);
      expect(getStore().edges[0].target).toBe(node2.id);
    });

    it('pushes history before connecting', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const stackBefore = getStore().undoStack.length;

      act(() => {
        const [n1, n2] = getStore().nodes;
        getStore().onConnect({
          source: n1.id,
          target: n2.id,
          sourceHandle: null,
          targetHandle: null,
        });
      });

      expect(getStore().undoStack.length).toBe(stackBefore + 1);
    });
  });

  describe('setSelectedNode', () => {
    it('sets the selected node id', () => {
      act(() => {
        getStore().setSelectedNode('some-id');
      });

      expect(getStore().selectedNodeId).toBe('some-id');
    });

    it('clears selection when passed null', () => {
      act(() => {
        getStore().setSelectedNode('some-id');
        getStore().setSelectedNode(null);
      });

      expect(getStore().selectedNodeId).toBeNull();
    });
  });

  describe('clearCanvas', () => {
    it('resets nodes, edges, selectedNodeId, and nodeCounter', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
        getStore().setSelectedNode(getStore().nodes[0].id);
      });

      expect(getStore().nodes.length).toBeGreaterThan(0);

      act(() => {
        getStore().clearCanvas();
      });

      expect(getStore().nodes).toHaveLength(0);
      expect(getStore().edges).toHaveLength(0);
      expect(getStore().selectedNodeId).toBeNull();
      expect(getStore().nodeCounter).toBe(0);
    });

    it('pushes history before clearing', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const stackBefore = getStore().undoStack.length;

      act(() => {
        getStore().clearCanvas();
      });

      expect(getStore().undoStack.length).toBe(stackBefore + 1);
    });
  });

  describe('importFromConfig', () => {
    it('loads config into nodes and edges', () => {
      const config = {
        modules: [
          { name: 'Server', type: 'http.server', config: { address: ':3000' } },
          { name: 'Router', type: 'http.router', dependsOn: ['Server'] },
        ],
        workflows: {},
        triggers: {},
      };

      act(() => {
        getStore().importFromConfig(config);
      });

      const { nodes, edges } = getStore();
      expect(nodes).toHaveLength(2);
      expect(edges).toHaveLength(1);
      expect(nodes[0].data.label).toBe('Server');
      expect(nodes[0].data.config.address).toBe(':3000');
      expect(nodes[1].data.label).toBe('Router');
    });

    it('clears selectedNodeId on import', () => {
      act(() => {
        getStore().setSelectedNode('old-id');
        getStore().importFromConfig({ modules: [], workflows: {}, triggers: {} });
      });

      expect(getStore().selectedNodeId).toBeNull();
    });

    it('pushes history before importing', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      const stackBefore = getStore().undoStack.length;

      act(() => {
        getStore().importFromConfig({ modules: [], workflows: {}, triggers: {} });
      });

      expect(getStore().undoStack.length).toBe(stackBefore + 1);
    });
  });

  describe('exportToConfig', () => {
    it('exports current nodes and edges as a WorkflowConfig', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      const [n1, n2] = getStore().nodes;

      act(() => {
        getStore().onConnect({
          source: n1.id,
          target: n2.id,
          sourceHandle: null,
          targetHandle: null,
        });
      });

      const config = getStore().exportToConfig();
      expect(config.modules).toHaveLength(2);
      expect(config.modules[0].type).toBe('http.server');
      expect(config.modules[1].dependsOn).toEqual([config.modules[0].name]);
    });
  });

  describe('undo/redo', () => {
    it('undo restores previous state', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      expect(getStore().nodes).toHaveLength(1);

      act(() => {
        getStore().undo();
      });

      expect(getStore().nodes).toHaveLength(0);
    });

    it('redo restores undone state', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      act(() => {
        getStore().undo();
      });

      expect(getStore().nodes).toHaveLength(0);

      act(() => {
        getStore().redo();
      });

      expect(getStore().nodes).toHaveLength(1);
    });

    it('undo does nothing when stack is empty', () => {
      const before = getStore();

      act(() => {
        getStore().undo();
      });

      expect(getStore().nodes).toEqual(before.nodes);
    });

    it('redo does nothing when stack is empty', () => {
      const before = getStore();

      act(() => {
        getStore().redo();
      });

      expect(getStore().nodes).toEqual(before.nodes);
    });

    it('pushHistory clears the redo stack', () => {
      act(() => {
        getStore().addNode('http.server', { x: 0, y: 0 });
      });

      act(() => {
        getStore().undo();
      });

      expect(getStore().redoStack.length).toBeGreaterThan(0);

      act(() => {
        getStore().addNode('http.router', { x: 100, y: 0 });
      });

      expect(getStore().redoStack).toHaveLength(0);
    });

    it('limits undo stack to 50 entries', () => {
      act(() => {
        for (let i = 0; i < 55; i++) {
          getStore().pushHistory();
        }
      });

      expect(getStore().undoStack.length).toBeLessThanOrEqual(50);
    });
  });

  describe('toasts', () => {
    it('addToast adds a toast notification', () => {
      act(() => {
        getStore().addToast('Test message', 'success');
      });

      expect(getStore().toasts).toHaveLength(1);
      expect(getStore().toasts[0].message).toBe('Test message');
      expect(getStore().toasts[0].type).toBe('success');
    });

    it('removeToast removes a toast by id', () => {
      act(() => {
        getStore().addToast('msg1', 'success');
        getStore().addToast('msg2', 'error');
      });

      const id = getStore().toasts[0].id;

      act(() => {
        getStore().removeToast(id);
      });

      expect(getStore().toasts).toHaveLength(1);
      expect(getStore().toasts[0].message).toBe('msg2');
    });
  });

  describe('UI panels', () => {
    it('toggleAIPanel toggles showAIPanel', () => {
      expect(getStore().showAIPanel).toBe(false);

      act(() => {
        getStore().toggleAIPanel();
      });

      expect(getStore().showAIPanel).toBe(true);

      act(() => {
        getStore().toggleAIPanel();
      });

      expect(getStore().showAIPanel).toBe(false);
    });

    it('toggleComponentBrowser toggles showComponentBrowser', () => {
      expect(getStore().showComponentBrowser).toBe(false);

      act(() => {
        getStore().toggleComponentBrowser();
      });

      expect(getStore().showComponentBrowser).toBe(true);
    });
  });
});
