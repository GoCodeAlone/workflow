import { describe, it, expect } from 'vitest';
import { nodesToConfig, configToNodes, configToYaml, parseYaml } from './serialization.ts';
import type { WorkflowNode } from '../store/workflowStore.ts';
import type { Edge } from '@xyflow/react';
import type { WorkflowConfig } from '../types/workflow.ts';

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

    it('converts edges to dependsOn relationships', () => {
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
        { id: 'e1', source: 'server_1', target: 'router_1' },
      ];

      const config = nodesToConfig(nodes, edges);
      expect(config.modules[1].dependsOn).toEqual(['Server']);
    });

    it('includes workflows and triggers as empty objects', () => {
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
        { id: 'e1', source: 'a', target: 'c' },
        { id: 'e2', source: 'b', target: 'c' },
      ];

      const config = nodesToConfig(nodes, edges);
      expect(config.modules[2].dependsOn).toEqual(['A', 'B']);
    });
  });

  describe('configToNodes', () => {
    it('converts WorkflowConfig to nodes with correct positions', () => {
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

    it('generates grid positions (3 columns)', () => {
      const config: WorkflowConfig = {
        modules: [
          { name: 'A', type: 'http.server' },
          { name: 'B', type: 'http.server' },
          { name: 'C', type: 'http.server' },
          { name: 'D', type: 'http.server' },
        ],
        workflows: {},
        triggers: {},
      };

      const { nodes } = configToNodes(config);

      // First row: columns 0,1,2
      expect(nodes[0].position).toEqual({ x: 50, y: 50 });
      expect(nodes[1].position).toEqual({ x: 350, y: 50 });
      expect(nodes[2].position).toEqual({ x: 650, y: 50 });
      // Second row: column 0
      expect(nodes[3].position).toEqual({ x: 50, y: 250 });
    });

    it('creates edges from dependsOn', () => {
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
        { id: 'e1', source: 'http_server_1', target: 'http_router_2' },
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
  });
});
