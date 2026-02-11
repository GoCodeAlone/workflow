import yaml from 'js-yaml';
import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import type {
  ModuleConfig,
  WorkflowConfig,
  WorkflowEdgeData,
  WorkflowEdgeType,
  HTTPWorkflowConfig,
  MessagingWorkflowConfig,
  StateMachineWorkflowConfig,
  EventWorkflowConfig,
  WorkflowTab,
} from '../types/workflow.ts';
import { MODULE_TYPE_MAP } from '../types/workflow.ts';

function makeEdge(
  sourceId: string,
  targetId: string,
  edgeType: WorkflowEdgeType,
  label?: string,
): Edge {
  const id = `e-${edgeType}-${sourceId}-${targetId}`;
  const data: WorkflowEdgeData = { edgeType, label };
  const edge: Edge = { id, source: sourceId, target: targetId, data };
  if (label) {
    edge.label = label;
    edge.labelBgStyle = { fill: '#1e1e2e', fillOpacity: 0.9 };
  }
  return edge;
}

export function extractWorkflowEdges(
  workflows: Record<string, unknown>,
  nameToId: Record<string, string>,
): Edge[] {
  const edges: Edge[] = [];

  // HTTP workflow edges
  const http = workflows.http as HTTPWorkflowConfig | undefined;
  if (http) {
    const serverId = nameToId[http.server];
    const routerId = nameToId[http.router];
    if (serverId && routerId) {
      edges.push(makeEdge(serverId, routerId, 'http-route', 'http'));
    }
    if (http.routes) {
      for (const route of http.routes) {
        const handlerId = nameToId[route.handler];
        if (routerId && handlerId) {
          edges.push(makeEdge(routerId, handlerId, 'http-route', `${route.method} ${route.path}`));
        }
        if (route.middlewares) {
          for (const mw of route.middlewares) {
            const mwId = nameToId[mw];
            if (mwId && handlerId) {
              edges.push(makeEdge(mwId, handlerId, 'http-route', 'middleware'));
            }
          }
        }
      }
    }
  }

  // Messaging workflow edges
  const messaging = workflows.messaging as MessagingWorkflowConfig | undefined;
  if (messaging) {
    const brokerId = nameToId[messaging.broker];
    if (messaging.subscriptions) {
      for (const sub of messaging.subscriptions) {
        const handlerId = nameToId[sub.handler];
        if (brokerId && handlerId) {
          edges.push(makeEdge(brokerId, handlerId, 'messaging-subscription', `topic: ${sub.topic}`));
        }
      }
    }
  }

  // State machine workflow edges
  const sm = workflows.statemachine as StateMachineWorkflowConfig | undefined;
  if (sm) {
    const engineId = nameToId[sm.engine];
    if (sm.definitions && engineId) {
      for (const def of sm.definitions) {
        // Link engine to any referenced modules by definition name
        const defModId = nameToId[def.name];
        if (defModId) {
          edges.push(makeEdge(engineId, defModId, 'statemachine', def.name));
        }
      }
    }
  }

  // Event workflow edges
  const evt = workflows.event as EventWorkflowConfig | undefined;
  if (evt) {
    const processorId = nameToId[evt.processor];
    if (processorId) {
      if (evt.handlers) {
        for (const h of evt.handlers) {
          const hId = nameToId[h];
          if (hId) {
            edges.push(makeEdge(processorId, hId, 'event', 'handler'));
          }
        }
      }
      if (evt.adapters) {
        for (const a of evt.adapters) {
          const aId = nameToId[a];
          if (aId) {
            edges.push(makeEdge(processorId, aId, 'event', 'adapter'));
          }
        }
      }
    }
  }

  return edges;
}

function topologicalLayout(
  nodes: WorkflowNode[],
  edges: Edge[],
): void {
  const X_SPACING = 300;
  const Y_SPACING = 150;
  const X_OFFSET = 50;
  const Y_OFFSET = 50;

  const nodeIds = new Set(nodes.map((n) => n.id));
  const inDegree: Record<string, number> = {};
  const adj: Record<string, string[]> = {};

  for (const n of nodes) {
    inDegree[n.id] = 0;
    adj[n.id] = [];
  }

  for (const e of edges) {
    if (nodeIds.has(e.source) && nodeIds.has(e.target)) {
      inDegree[e.target] = (inDegree[e.target] ?? 0) + 1;
      adj[e.source].push(e.target);
    }
  }

  // BFS by layers
  const layers: string[][] = [];
  let queue = nodes.filter((n) => inDegree[n.id] === 0).map((n) => n.id);

  const visited = new Set<string>();
  while (queue.length > 0) {
    layers.push([...queue]);
    const next: string[] = [];
    for (const id of queue) {
      visited.add(id);
      for (const child of adj[id]) {
        inDegree[child]--;
        if (inDegree[child] === 0 && !visited.has(child)) {
          next.push(child);
        }
      }
    }
    queue = next;
  }

  // Any nodes not placed (cycles) go in a final layer
  const remaining = nodes.filter((n) => !visited.has(n.id)).map((n) => n.id);
  if (remaining.length > 0) {
    layers.push(remaining);
  }

  // Assign positions: each layer is a column (left to right)
  const posMap: Record<string, { x: number; y: number }> = {};
  for (let col = 0; col < layers.length; col++) {
    const layer = layers[col];
    for (let row = 0; row < layer.length; row++) {
      posMap[layer[row]] = {
        x: col * X_SPACING + X_OFFSET,
        y: row * Y_SPACING + Y_OFFSET,
      };
    }
  }

  for (const node of nodes) {
    if (posMap[node.id]) {
      node.position = posMap[node.id];
    }
  }
}

export function nodesToConfig(nodes: WorkflowNode[], edges: Edge[]): WorkflowConfig {
  // Filter out synthesized conditional nodes
  const realNodes = nodes.filter((n) => !n.data.synthesized);

  const dependencyEdges: Edge[] = [];
  const httpRouteEdges: Edge[] = [];
  const messagingEdges: Edge[] = [];

  for (const edge of edges) {
    const edgeData = edge.data as WorkflowEdgeData | undefined;
    const edgeType = edgeData?.edgeType ?? 'dependency';
    switch (edgeType) {
      case 'http-route':
        httpRouteEdges.push(edge);
        break;
      case 'messaging-subscription':
        messagingEdges.push(edge);
        break;
      case 'conditional':
        // Skip conditional edges in export
        break;
      default:
        dependencyEdges.push(edge);
        break;
    }
  }

  // Build dependsOn from dependency edges
  const dependencyMap: Record<string, string[]> = {};
  for (const edge of dependencyEdges) {
    if (!dependencyMap[edge.target]) {
      dependencyMap[edge.target] = [];
    }
    const sourceNode = realNodes.find((n) => n.id === edge.source);
    if (sourceNode) {
      dependencyMap[edge.target].push(sourceNode.data.label);
    }
  }

  const modules: ModuleConfig[] = realNodes.map((node) => {
    const mod: ModuleConfig = {
      name: node.data.label,
      type: node.data.moduleType,
    };

    if (node.data.config && Object.keys(node.data.config).length > 0) {
      mod.config = { ...node.data.config };
    }

    const deps = dependencyMap[node.id];
    if (deps && deps.length > 0) {
      mod.dependsOn = deps;
    }

    return mod;
  });

  // Reconstruct workflows section from typed edges
  const workflows: Record<string, unknown> = {};

  // Reconstruct HTTP workflows
  if (httpRouteEdges.length > 0) {
    const idToName: Record<string, string> = {};
    for (const n of nodes) idToName[n.id] = n.data.label;

    // Find server->router edge (label "http")
    const serverRouterEdge = httpRouteEdges.find(
      (e) => (e.data as WorkflowEdgeData)?.label === 'http',
    );
    const routerRouteEdges = httpRouteEdges.filter(
      (e) => (e.data as WorkflowEdgeData)?.label !== 'http' && (e.data as WorkflowEdgeData)?.label !== 'middleware',
    );

    if (serverRouterEdge) {
      const httpConfig: Record<string, unknown> = {
        server: idToName[serverRouterEdge.source],
        router: idToName[serverRouterEdge.target],
      };

      if (routerRouteEdges.length > 0) {
        httpConfig.routes = routerRouteEdges.map((e) => {
          const label = (e.data as WorkflowEdgeData)?.label ?? 'GET /';
          const parts = label.split(' ', 2);
          return {
            method: parts[0],
            path: parts[1] ?? '/',
            handler: idToName[e.target],
          };
        });
      }

      workflows.http = httpConfig;
    }
  }

  // Reconstruct messaging workflows
  if (messagingEdges.length > 0) {
    const idToName: Record<string, string> = {};
    for (const n of nodes) idToName[n.id] = n.data.label;

    // All messaging edges share the same broker (source)
    const brokerId = messagingEdges[0].source;
    const msgConfig: Record<string, unknown> = {
      broker: idToName[brokerId],
      subscriptions: messagingEdges.map((e) => {
        const label = (e.data as WorkflowEdgeData)?.label ?? '';
        const topic = label.startsWith('topic: ') ? label.slice(7) : label;
        return {
          topic,
          handler: idToName[e.target],
        };
      }),
    };
    workflows.messaging = msgConfig;
  }

  const triggers: Record<string, unknown> = {};

  return { modules, workflows, triggers };
}

export function configToNodes(config: WorkflowConfig): {
  nodes: WorkflowNode[];
  edges: Edge[];
} {
  const nodes: WorkflowNode[] = [];
  const edges: Edge[] = [];
  const nameToId: Record<string, string> = {};

  config.modules.forEach((mod, i) => {
    const id = `${mod.type.replace(/\./g, '_')}_${i + 1}`;
    nameToId[mod.name] = id;

    const info = MODULE_TYPE_MAP[mod.type];

    nodes.push({
      id,
      type: nodeComponentType(mod.type),
      position: { x: 0, y: 0 }, // will be set by topological layout
      data: {
        moduleType: mod.type,
        label: mod.name,
        config: mod.config ?? (info ? { ...info.defaultConfig } : {}),
      },
    });
  });

  // Dependency edges (labeled with source module name)
  config.modules.forEach((mod) => {
    if (mod.dependsOn) {
      const targetId = nameToId[mod.name];
      for (const dep of mod.dependsOn) {
        const sourceId = nameToId[dep];
        if (sourceId && targetId) {
          edges.push(makeEdge(sourceId, targetId, 'dependency', dep));
        }
      }
    }
  });

  // Workflow edges
  const workflowEdges = extractWorkflowEdges(config.workflows, nameToId);
  // Deduplicate: don't add workflow edge if an identical source-target already exists
  const existingPairs = new Set(edges.map((e) => `${e.source}->${e.target}`));
  for (const we of workflowEdges) {
    const key = `${we.source}->${we.target}`;
    if (!existingPairs.has(key)) {
      edges.push(we);
      existingPairs.add(key);
    }
  }

  // Apply topological layout
  topologicalLayout(nodes, edges);

  return { nodes, edges };
}

export function nodeComponentType(moduleType: string): string {
  if (moduleType.startsWith('conditional.')) return 'conditionalNode';
  if (moduleType.startsWith('http.middleware.')) return 'middlewareNode';
  if (moduleType === 'http.server') return 'httpNode';
  if (moduleType.startsWith('http.')) return 'httpRouterNode';
  if (moduleType === 'api.handler') return 'httpRouterNode';
  if (moduleType.startsWith('messaging.')) return 'messagingNode';
  if (moduleType.startsWith('statemachine.') || moduleType.startsWith('state.')) return 'stateMachineNode';
  if (moduleType === 'scheduler.modular') return 'schedulerNode';
  if (moduleType === 'eventlogger.modular' || moduleType === 'eventbus.modular') return 'eventNode';
  if (moduleType === 'httpclient.modular') return 'integrationNode';
  if (moduleType === 'chimux.router') return 'httpRouterNode';
  if (moduleType === 'notification.slack' || moduleType === 'storage.s3') return 'integrationNode';
  if (moduleType === 'observability.otel') return 'infrastructureNode';
  return 'infrastructureNode';
}

export function configToYaml(config: WorkflowConfig): string {
  return yaml.dump(config, { lineWidth: -1, noRefs: true, sortKeys: false });
}

export function parseYaml(text: string): WorkflowConfig {
  const parsed = yaml.load(text) as Record<string, unknown>;
  return {
    modules: (parsed.modules ?? []) as ModuleConfig[],
    workflows: (parsed.workflows ?? {}) as Record<string, unknown>,
    triggers: (parsed.triggers ?? {}) as Record<string, unknown>,
  };
}

// Extract conditional branch points from state machine workflow definitions
export function extractStateMachineBranches(
  workflows: Record<string, unknown>,
  nameToId: Record<string, string>,
): { nodes: WorkflowNode[]; edges: Edge[] } {
  const newNodes: WorkflowNode[] = [];
  const newEdges: Edge[] = [];

  const sm = workflows.statemachine as StateMachineWorkflowConfig | undefined;
  if (!sm?.definitions) return { nodes: newNodes, edges: newEdges };

  for (const def of sm.definitions) {
    const states = def.states as Record<string, { transitions?: Record<string, string> }> | undefined;
    if (!states) continue;

    for (const [stateName, stateConfig] of Object.entries(states)) {
      const transitions = stateConfig?.transitions;
      if (!transitions || Object.keys(transitions).length <= 1) continue;

      // Multiple outgoing transitions = branch point
      const branchId = `synth_branch_${stateName}_${Date.now()}`;
      const sourceId = nameToId[stateName];
      if (!sourceId) continue;

      const branchNode: WorkflowNode = {
        id: branchId,
        type: 'conditionalNode',
        position: { x: 0, y: 0 },
        data: {
          moduleType: 'conditional.switch',
          label: `${stateName} branch`,
          config: {
            expression: stateName,
            cases: Object.keys(transitions),
          },
          synthesized: true,
        },
      };

      newNodes.push(branchNode);
      newEdges.push(makeEdge(sourceId, branchId, 'statemachine', `branch: ${stateName}`));

      for (const [eventName, targetState] of Object.entries(transitions)) {
        const targetId = nameToId[targetState];
        if (targetId) {
          newEdges.push(makeEdge(branchId, targetId, 'conditional', `transition: ${eventName}`));
        }
      }
    }
  }

  return { nodes: newNodes, edges: newEdges };
}

// Multi-workflow export: all tabs as a single YAML with `workflows` top-level array
export function nodesToMultiConfig(tabs: WorkflowTab[]): string {
  const multiConfig = {
    workflows: tabs.map((tab) => {
      const config = nodesToConfig(
        tab.nodes as WorkflowNode[],
        tab.edges,
      );
      return {
        name: tab.name,
        ...config,
      };
    }),
  };
  return yaml.dump(multiConfig, { lineWidth: -1, noRefs: true, sortKeys: false });
}

// Multi-workflow import: parse YAML with `workflows` array into tabs
interface MultiWorkflowEntry {
  name?: string;
  modules?: ModuleConfig[];
  workflows?: Record<string, unknown>;
  triggers?: Record<string, unknown>;
}

export function multiConfigToTabs(yamlContent: string): WorkflowTab[] {
  const parsed = yaml.load(yamlContent) as { workflows?: MultiWorkflowEntry[] };
  const entries = parsed?.workflows ?? [];

  return entries.map((entry, i) => {
    const config: WorkflowConfig = {
      modules: (entry.modules ?? []) as ModuleConfig[],
      workflows: (entry.workflows ?? {}) as Record<string, unknown>,
      triggers: (entry.triggers ?? {}) as Record<string, unknown>,
    };
    const { nodes, edges } = configToNodes(config);
    return {
      id: `imported-${i}-${Date.now()}`,
      name: entry.name || `Workflow ${i + 1}`,
      nodes,
      edges,
      undoStack: [],
      redoStack: [],
      dirty: false,
    };
  });
}
