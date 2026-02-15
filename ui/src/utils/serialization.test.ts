import { describe, it, expect } from 'vitest';
import { nodesToConfig, configToNodes, configToYaml, parseYaml, extractWorkflowEdges } from './serialization.ts';
import type { WorkflowNode } from '../store/workflowStore.ts';
import type { Edge } from '@xyflow/react';
import type { WorkflowConfig, WorkflowEdgeData } from '../types/workflow.ts';

describe('serialization', () => {
  describe('nodesToConfig', () => {
    it('converts nodes to modules in WorkflowConfig', () => {
      const nodes: WorkflowNode[] = [
        {
          id: 'http_server_1',
          type: 'httpNode',
          position: { x: 50, y: 50 },
          data: {
            moduleType: 'http.server',
            label: 'My Server',
            config: { address: ':8080' },
          },
        },
      ];

      const config = nodesToConfig(nodes, []);
      expect(config.modules).toHaveLength(1);
      expect(config.modules[0].name).toBe('My Server');
      expect(config.modules[0].type).toBe('http.server');
      expect(config.modules[0].config).toEqual({ address: ':8080' });
    });

    it('omits config when it is empty', () => {
      const nodes: WorkflowNode[] = [
        {
          id: 'http_router_1',
          type: 'httpNode',
          position: { x: 0, y: 0 },
          data: {
            moduleType: 'http.router',
            label: 'Router',
            config: {},
          },
        },
      ];

      const config = nodesToConfig(nodes, []);
      expect(config.modules[0].config).toBeUndefined();
    });

    it('converts dependency edges to dependsOn relationships', () => {
      const nodes: WorkflowNode[] = [
        {
          id: 'server_1',
          type: 'httpNode',
          position: { x: 0, y: 0 },
          data: { moduleType: 'http.server', label: 'Server', config: {} },
        },
        {
          id: 'router_1',
          type: 'httpNode',
          position: { x: 100, y: 0 },
          data: { moduleType: 'http.router', label: 'Router', config: {} },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'server_1', target: 'router_1', data: { edgeType: 'dependency' } },
      ];

      const config = nodesToConfig(nodes, edges);
      expect(config.modules[1].dependsOn).toEqual(['Server']);
    });

    it('includes workflows and triggers as empty objects when no typed edges', () => {
      const config = nodesToConfig([], []);
      expect(config.workflows).toEqual({});
      expect(config.triggers).toEqual({});
    });

    it('handles multiple dependencies', () => {
      const nodes: WorkflowNode[] = [
        {
          id: 'a', type: 'httpNode', position: { x: 0, y: 0 },
          data: { moduleType: 'http.server', label: 'A', config: {} },
        },
        {
          id: 'b', type: 'httpNode', position: { x: 100, y: 0 },
          data: { moduleType: 'http.server', label: 'B', config: {} },
        },
        {
          id: 'c', type: 'httpNode', position: { x: 200, y: 0 },
          data: { moduleType: 'http.router', label: 'C', config: {} },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'a', target: 'c', data: { edgeType: 'dependency' } },
        { id: 'e2', source: 'b', target: 'c', data: { edgeType: 'dependency' } },
      ];

      const config = nodesToConfig(nodes, edges);
      expect(config.modules[2].dependsOn).toEqual(['A', 'B']);
    });

    it('reconstructs http workflows section from http-route edges', () => {
      const nodes: WorkflowNode[] = [
        { id: 'srv', type: 'httpNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.server', label: 'my-server', config: {} } },
        { id: 'rtr', type: 'httpRouterNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.router', label: 'my-router', config: {} } },
        { id: 'hnd', type: 'httpRouterNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.handler', label: 'my-handler', config: {} } },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'srv', target: 'rtr', data: { edgeType: 'http-route', label: 'http' } as WorkflowEdgeData },
        { id: 'e2', source: 'rtr', target: 'hnd', data: { edgeType: 'http-route', label: 'POST /api/orders' } as WorkflowEdgeData },
      ];

      const config = nodesToConfig(nodes, edges);
      const http = config.workflows.http as { server: string; router: string; routes: Array<{ method: string; path: string; handler: string }> };
      expect(http.server).toBe('my-server');
      expect(http.router).toBe('my-router');
      expect(http.routes).toHaveLength(1);
      expect(http.routes[0]).toEqual({ method: 'POST', path: '/api/orders', handler: 'my-handler' });
    });

    it('reconstructs messaging workflows section from messaging edges', () => {
      const nodes: WorkflowNode[] = [
        { id: 'brk', type: 'messagingNode', position: { x: 0, y: 0 }, data: { moduleType: 'messaging.broker', label: 'my-broker', config: {} } },
        { id: 'hnd', type: 'messagingNode', position: { x: 0, y: 0 }, data: { moduleType: 'messaging.handler', label: 'my-handler', config: {} } },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'brk', target: 'hnd', data: { edgeType: 'messaging-subscription', label: 'topic: order.completed' } as WorkflowEdgeData },
      ];

      const config = nodesToConfig(nodes, edges);
      const messaging = config.workflows.messaging as { broker: string; subscriptions: Array<{ topic: string; handler: string }> };
      expect(messaging.broker).toBe('my-broker');
      expect(messaging.subscriptions).toHaveLength(1);
      expect(messaging.subscriptions[0]).toEqual({ topic: 'order.completed', handler: 'my-handler' });
    });
  });

  describe('extractWorkflowEdges', () => {
    it('extracts HTTP workflow edges', () => {
      const nameToId: Record<string, string> = {
        'my-server': 'srv',
        'my-router': 'rtr',
        'my-handler': 'hnd',
      };
      const workflows = {
        http: {
          server: 'my-server',
          router: 'my-router',
          routes: [
            { method: 'POST', path: '/api/orders', handler: 'my-handler' },
          ],
        },
      };

      const edges = extractWorkflowEdges(workflows, nameToId);
      expect(edges).toHaveLength(2);

      // server -> router
      expect(edges[0].source).toBe('srv');
      expect(edges[0].target).toBe('rtr');
      expect((edges[0].data as WorkflowEdgeData).edgeType).toBe('http-route');

      // router -> handler
      expect(edges[1].source).toBe('rtr');
      expect(edges[1].target).toBe('hnd');
      expect((edges[1].data as WorkflowEdgeData).edgeType).toBe('http-route');
      expect((edges[1].data as WorkflowEdgeData).label).toBe('POST /api/orders');
    });

    it('extracts messaging workflow edges', () => {
      const nameToId: Record<string, string> = {
        'my-broker': 'brk',
        'my-handler': 'hnd',
      };
      const workflows = {
        messaging: {
          broker: 'my-broker',
          subscriptions: [
            { topic: 'order.completed', handler: 'my-handler' },
          ],
        },
      };

      const edges = extractWorkflowEdges(workflows, nameToId);
      expect(edges).toHaveLength(1);
      expect(edges[0].source).toBe('brk');
      expect(edges[0].target).toBe('hnd');
      expect((edges[0].data as WorkflowEdgeData).edgeType).toBe('messaging-subscription');
      expect((edges[0].data as WorkflowEdgeData).label).toBe('topic: order.completed');
    });

    it('extracts statemachine workflow edges for matching modules', () => {
      const nameToId: Record<string, string> = {
        'my-engine': 'eng',
        'order-processing': 'proc',
      };
      const workflows = {
        statemachine: {
          engine: 'my-engine',
          definitions: [
            { name: 'order-processing', initialState: 'received' },
          ],
        },
      };

      const edges = extractWorkflowEdges(workflows, nameToId);
      expect(edges).toHaveLength(1);
      expect(edges[0].source).toBe('eng');
      expect(edges[0].target).toBe('proc');
      expect((edges[0].data as WorkflowEdgeData).edgeType).toBe('statemachine');
    });

    it('extracts event workflow edges', () => {
      const nameToId: Record<string, string> = {
        'my-processor': 'proc',
        'my-handler': 'hnd',
        'my-adapter': 'adp',
      };
      const workflows = {
        event: {
          processor: 'my-processor',
          handlers: ['my-handler'],
          adapters: ['my-adapter'],
        },
      };

      const edges = extractWorkflowEdges(workflows, nameToId);
      expect(edges).toHaveLength(2);
      expect(edges[0].source).toBe('proc');
      expect(edges[0].target).toBe('hnd');
      expect((edges[0].data as WorkflowEdgeData).edgeType).toBe('event');
      expect(edges[1].source).toBe('proc');
      expect(edges[1].target).toBe('adp');
    });

    it('returns empty array for empty workflows', () => {
      const edges = extractWorkflowEdges({}, {});
      expect(edges).toEqual([]);
    });
  });

  describe('configToNodes', () => {
    it('converts WorkflowConfig to nodes', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'Server', type: 'http.server', config: { address: ':8080' } },
          { name: 'Router', type: 'http.router' },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes, edges } = configToNodes(config);
      expect(nodes).toHaveLength(2);
      expect(edges).toHaveLength(0);

      expect(nodes[0].data.label).toBe('Server');
      expect(nodes[0].data.moduleType).toBe('http.server');
      expect(nodes[0].data.config.address).toBe(':8080');
      expect(nodes[0].type).toBe('httpNode');

      expect(nodes[1].data.label).toBe('Router');
      expect(nodes[1].data.moduleType).toBe('http.router');
    });

    it('uses dagre layout (dependent node placed to the right)', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'A', type: 'http.server' },
          { name: 'B', type: 'http.router', dependsOn: ['A'] },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);

      // A should be to the left of B (LR ranking)
      expect(nodes[0].position.x).toBeLessThan(nodes[1].position.x);
    });

    it('places independent nodes in the same column', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'A', type: 'http.server' },
          { name: 'B', type: 'http.server' },
          { name: 'C', type: 'http.server' },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);

      // All independent, so all in same column (same x)
      expect(nodes[0].position.x).toBe(nodes[1].position.x);
      expect(nodes[1].position.x).toBe(nodes[2].position.x);

      // Spaced vertically — each should be below the previous
      expect(nodes[0].position.y).toBeLessThan(nodes[1].position.y);
      expect(nodes[1].position.y).toBeLessThan(nodes[2].position.y);
    });

    it('creates dependency edges from dependsOn', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'Server', type: 'http.server' },
          { name: 'Router', type: 'http.router', dependsOn: ['Server'] },
        ],
        workflows: {},
        triggers: {},
      };

      const { edges } = configToNodes(config);
      expect(edges).toHaveLength(1);
      expect(edges[0].source).toContain('http_server');
      expect(edges[0].target).toContain('http_router');
      expect((edges[0].data as WorkflowEdgeData).edgeType).toBe('dependency');
    });

    it('creates workflow edges from workflows section', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'my-server', type: 'http.server' },
          { name: 'my-router', type: 'http.router' },
          { name: 'my-handler', type: 'http.handler' },
        ],
        workflows: {
          http: {
            server: 'my-server',
            router: 'my-router',
            routes: [
              { method: 'GET', path: '/api', handler: 'my-handler' },
            ],
          },
        },
        triggers: {},
      };

      const { edges } = configToNodes(config);

      const httpEdges = edges.filter((e) => (e.data as WorkflowEdgeData)?.edgeType === 'http-route');
      expect(httpEdges.length).toBe(2); // server->router and router->handler
    });

    it('deduplicates workflow edges that overlap with dependency edges', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'my-server', type: 'http.server' },
          { name: 'my-router', type: 'http.router', dependsOn: ['my-server'] },
        ],
        workflows: {
          http: {
            server: 'my-server',
            router: 'my-router',
          },
        },
        triggers: {},
      };

      const { edges } = configToNodes(config);
      // server->router appears both in dependsOn and http workflow
      // Should only appear once (the dependency edge)
      const serverToRouter = edges.filter((e) =>
        e.source.includes('http_server') && e.target.includes('http_router'),
      );
      expect(serverToRouter).toHaveLength(1);
    });

    it('uses defaultConfig when module config is missing', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'Server', type: 'http.server' },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);
      expect(nodes[0].data.config).toEqual({ address: ':8080' });
    });

    it('maps component types correctly', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'MW', type: 'http.middleware.auth' },
          { name: 'Broker', type: 'messaging.broker' },
          { name: 'SM', type: 'statemachine.engine' },
          { name: 'Sched', type: 'scheduler.modular' },
          { name: 'EvtBus', type: 'eventbus.modular' },
          { name: 'Client', type: 'httpclient.modular' },
          { name: 'DB', type: 'database.modular' },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);
      expect(nodes[0].type).toBe('middlewareNode');
      expect(nodes[1].type).toBe('messagingNode');
      expect(nodes[2].type).toBe('stateMachineNode');
      expect(nodes[3].type).toBe('schedulerNode');
      expect(nodes[4].type).toBe('eventNode');
      expect(nodes[5].type).toBe('integrationNode');
      expect(nodes[6].type).toBe('infrastructureNode');
    });
  });

  describe('dagre layout', () => {
    it('produces correct column ordering for a linear chain', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'A', type: 'http.server' },
          { name: 'B', type: 'http.router', dependsOn: ['A'] },
          { name: 'C', type: 'http.handler', dependsOn: ['B'] },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);

      // A -> B -> C: each column further right
      expect(nodes[0].position.x).toBeLessThan(nodes[1].position.x);
      expect(nodes[1].position.x).toBeLessThan(nodes[2].position.x);
    });

    it('handles diamond dependency pattern', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'Root', type: 'http.server' },
          { name: 'Left', type: 'http.router', dependsOn: ['Root'] },
          { name: 'Right', type: 'http.handler', dependsOn: ['Root'] },
          { name: 'Join', type: 'messaging.broker', dependsOn: ['Left', 'Right'] },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);

      // Root is leftmost
      expect(nodes[0].position.x).toBeLessThan(nodes[1].position.x);
      expect(nodes[0].position.x).toBeLessThan(nodes[2].position.x);
      // Left and Right are in the same column (same x)
      expect(nodes[1].position.x).toBe(nodes[2].position.x);
      // Join is to the right of Left and Right
      expect(nodes[3].position.x).toBeGreaterThan(nodes[1].position.x);
    });
  });

  describe('YAML round-trip', () => {
    it('configToYaml produces valid YAML string', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'Server', type: 'http.server', config: { address: ':8080' } },
        ],
        workflows: {},
        triggers: {},
      };

      const yaml = configToYaml(config);
      expect(typeof yaml).toBe('string');
      expect(yaml).toContain('modules');
      expect(yaml).toContain('http.server');
      expect(yaml).toContain(':8080');
    });

    it('parseYaml parses YAML back to WorkflowConfig', () => {
      const yamlText = `
modules:
  - name: Server
    type: http.server
    config:
      address: ":3000"
workflows: {}
triggers: {}
`;
      const config = parseYaml(yamlText);
      expect(config.modules).toHaveLength(1);
      expect(config.modules[0].name).toBe('Server');
      expect(config.modules[0].type).toBe('http.server');
      expect(config.modules[0].config?.address).toBe(':3000');
    });

    it('round-trips config through YAML', () => {
      const original: WorkflowConfig = {
        modules: [
          { name: 'Server', type: 'http.server', config: { address: ':8080' } },
          { name: 'Router', type: 'http.router', dependsOn: ['Server'] },
        ],
        workflows: {},
        triggers: {},
      };

      const yaml = configToYaml(original);
      const restored = parseYaml(yaml);

      expect(restored.modules).toHaveLength(2);
      expect(restored.modules[0].name).toBe('Server');
      expect(restored.modules[0].config?.address).toBe(':8080');
      expect(restored.modules[1].dependsOn).toEqual(['Server']);
    });

    it('parseYaml handles missing fields gracefully', () => {
      const config = parseYaml('modules: []');
      expect(config.modules).toEqual([]);
      expect(config.workflows).toEqual({});
      expect(config.triggers).toEqual({});
    });

    it('round-trips workflow edges through YAML', () => {
      const original: WorkflowConfig = {
        modules: [
          { name: 'my-server', type: 'http.server' },
          { name: 'my-router', type: 'http.router' },
          { name: 'my-handler', type: 'http.handler' },
          { name: 'my-broker', type: 'messaging.broker' },
          { name: 'my-notifier', type: 'messaging.handler' },
        ],
        workflows: {
          http: {
            server: 'my-server',
            router: 'my-router',
            routes: [
              { method: 'POST', path: '/api/orders', handler: 'my-handler' },
            ],
          },
          messaging: {
            broker: 'my-broker',
            subscriptions: [
              { topic: 'order.completed', handler: 'my-notifier' },
            ],
          },
        },
        triggers: {},
      };

      const yaml = configToYaml(original);
      const restored = parseYaml(yaml);

      // Verify workflows section is preserved through YAML serialization
      const http = restored.workflows.http as Record<string, unknown>;
      expect(http.server).toBe('my-server');
      expect(http.router).toBe('my-router');
      const routes = http.routes as Array<Record<string, string>>;
      expect(routes).toHaveLength(1);
      expect(routes[0].handler).toBe('my-handler');

      const messaging = restored.workflows.messaging as Record<string, unknown>;
      expect(messaging.broker).toBe('my-broker');
      const subs = messaging.subscriptions as Array<Record<string, string>>;
      expect(subs).toHaveLength(1);
      expect(subs[0].topic).toBe('order.completed');
    });
  });

  describe('round-trip: nodes -> config -> nodes', () => {
    it('preserves essential data through conversion', () => {
      const originalNodes: WorkflowNode[] = [
        {
          id: 'http_server_1',
          type: 'httpNode',
          position: { x: 50, y: 50 },
          data: {
            moduleType: 'http.server',
            label: 'My Server',
            config: { address: ':8080' },
          },
        },
        {
          id: 'http_router_2',
          type: 'httpNode',
          position: { x: 350, y: 50 },
          data: {
            moduleType: 'http.router',
            label: 'My Router',
            config: {},
          },
        },
      ];
      const originalEdges: Edge[] = [
        { id: 'e1', source: 'http_server_1', target: 'http_router_2', data: { edgeType: 'dependency' } },
      ];

      const config = nodesToConfig(originalNodes, originalEdges);
      const { nodes, edges } = configToNodes(config);

      // Labels preserved
      expect(nodes[0].data.label).toBe('My Server');
      expect(nodes[1].data.label).toBe('My Router');

      // Module types preserved
      expect(nodes[0].data.moduleType).toBe('http.server');
      expect(nodes[1].data.moduleType).toBe('http.router');

      // Config preserved
      expect(nodes[0].data.config.address).toBe(':8080');

      // Dependencies preserved
      expect(edges).toHaveLength(1);
    });

    it('preserves workflow edges through round-trip', () => {
      const originalNodes: WorkflowNode[] = [
        { id: 'srv', type: 'httpNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.server', label: 'my-server', config: {} } },
        { id: 'rtr', type: 'httpRouterNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.router', label: 'my-router', config: {} } },
        { id: 'hnd', type: 'httpRouterNode', position: { x: 0, y: 0 }, data: { moduleType: 'http.handler', label: 'my-handler', config: {} } },
        { id: 'brk', type: 'messagingNode', position: { x: 0, y: 0 }, data: { moduleType: 'messaging.broker', label: 'my-broker', config: {} } },
        { id: 'not', type: 'messagingNode', position: { x: 0, y: 0 }, data: { moduleType: 'messaging.handler', label: 'my-notifier', config: {} } },
      ];
      const originalEdges: Edge[] = [
        { id: 'e1', source: 'srv', target: 'rtr', data: { edgeType: 'http-route', label: 'http' } as WorkflowEdgeData },
        { id: 'e2', source: 'rtr', target: 'hnd', data: { edgeType: 'http-route', label: 'POST /api/orders' } as WorkflowEdgeData },
        { id: 'e3', source: 'brk', target: 'not', data: { edgeType: 'messaging-subscription', label: 'topic: order.completed' } as WorkflowEdgeData },
      ];

      const config = nodesToConfig(originalNodes, originalEdges);
      const { edges } = configToNodes(config);

      // HTTP edges preserved
      const httpEdges = edges.filter((e) => (e.data as WorkflowEdgeData)?.edgeType === 'http-route');
      expect(httpEdges.length).toBe(2);

      // Messaging edges preserved
      const msgEdges = edges.filter((e) => (e.data as WorkflowEdgeData)?.edgeType === 'messaging-subscription');
      expect(msgEdges.length).toBe(1);
    });
  });
});
