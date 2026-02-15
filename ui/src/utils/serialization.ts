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
import { MODULE_TYPE_MAP as STATIC_MODULE_TYPE_MAP } from '../types/workflow.ts';
import useModuleSchemaStore from '../store/moduleSchemaStore.ts';
import { layoutNodes } from './autoLayout.ts';

function getModuleTypeMap() {
  const store = useModuleSchemaStore.getState();
  return store.loaded ? store.moduleTypeMap : STATIC_MODULE_TYPE_MAP;
}

function makeEdge(
  sourceId: string,
  targetId: string,
  edgeType: WorkflowEdgeType,
  label?: string,
  sourceHandle?: string,
): Edge {
  const id = `e-${edgeType}-${sourceId}-${targetId}${sourceHandle ? `-${sourceHandle}` : ''}`;
  const data: WorkflowEdgeData = { edgeType, label };
  const edge: Edge = { id, source: sourceId, target: targetId, data };
  if (sourceHandle) {
    edge.sourceHandle = sourceHandle;
  }
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
        if (route.middlewares && route.middlewares.length > 0 && routerId) {
          // Build ordered middleware chain: router -> mw1 -> mw2 -> ... -> handler
          const mwIds = route.middlewares
            .map((mw) => nameToId[mw])
            .filter((id): id is string => !!id);

          if (mwIds.length > 0) {
            // Router to first middleware
            edges.push(makeEdge(routerId, mwIds[0], 'middleware-chain', `${route.method} ${route.path} [1]`));
            // Chain middlewares together
            for (let i = 0; i < mwIds.length - 1; i++) {
              edges.push(makeEdge(mwIds[i], mwIds[i + 1], 'middleware-chain', `[${i + 2}]`));
            }
            // Last middleware to handler
            if (handlerId) {
              edges.push(makeEdge(mwIds[mwIds.length - 1], handlerId, 'middleware-chain', `[${mwIds.length + 1}] handler`));
            }
          }
        } else if (routerId && handlerId) {
          // No middleware — direct route edge
          edges.push(makeEdge(routerId, handlerId, 'http-route', `${route.method} ${route.path}`));
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

export function nodesToConfig(nodes: WorkflowNode[], edges: Edge[]): WorkflowConfig {
  // Filter out synthesized conditional nodes
  const realNodes = nodes.filter((n) => !n.data.synthesized);

  const dependencyEdges: Edge[] = [];
  const httpRouteEdges: Edge[] = [];
  const messagingEdges: Edge[] = [];
  const conditionalEdges: Edge[] = [];
  const middlewareChainEdges: Edge[] = [];

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
        conditionalEdges.push(edge);
        break;
      case 'middleware-chain':
        middlewareChainEdges.push(edge);
        break;
      case 'auto-wire':
        // Auto-wire edges are computed, not serialized
        break;
      default:
        dependencyEdges.push(edge);
        break;
    }
  }

  // Build branches map from conditional edges (sourceId -> { handleId: targetName })
  const branchesMap: Record<string, Record<string, string>> = {};
  const idToName: Record<string, string> = {};
  for (const n of realNodes) idToName[n.id] = n.data.label;
  for (const edge of conditionalEdges) {
    const sourceNode = realNodes.find((n) => n.id === edge.source);
    if (!sourceNode || sourceNode.data.synthesized) continue;
    if (!branchesMap[edge.source]) branchesMap[edge.source] = {};
    const handleId = edge.sourceHandle ?? (edge.data as WorkflowEdgeData)?.label ?? 'default';
    branchesMap[edge.source][handleId] = idToName[edge.target] ?? edge.target;
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

    const branches = branchesMap[node.id];
    if (branches && Object.keys(branches).length > 0) {
      mod.branches = branches;
    }

    // Persist canvas position so layout survives save/load
    mod.ui_position = {
      x: Math.round(node.position.x),
      y: Math.round(node.position.y),
    };

    return mod;
  });

  // Reconstruct workflows section from typed edges
  const workflows: Record<string, unknown> = {};

  // Reconstruct HTTP workflows
  if (httpRouteEdges.length > 0 || middlewareChainEdges.length > 0) {
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

      // Reconstruct routes from both http-route and middleware-chain edges
      const routes: Array<{ method: string; path: string; handler: string; middlewares?: string[] }> = [];

      // Direct routes (no middleware)
      for (const e of routerRouteEdges) {
        const label = (e.data as WorkflowEdgeData)?.label ?? 'GET /';
        const parts = label.split(' ', 2);
        routes.push({
          method: parts[0],
          path: parts[1] ?? '/',
          handler: idToName[e.target],
        });
      }

      // Reconstruct middleware chain routes: walk chain edges from router
      // Group chain edges by their starting route label
      if (middlewareChainEdges.length > 0) {
        // Find chain starts: edges from the router node
        const routerId = serverRouterEdge.target;
        const chainStarts = middlewareChainEdges.filter((e) => e.source === routerId);

        for (const startEdge of chainStarts) {
          const label = (startEdge.data as WorkflowEdgeData)?.label ?? '';
          // Extract method/path from label like "GET /api [1]"
          const routeMatch = label.match(/^(\S+)\s+(\S+)/);
          const method = routeMatch?.[1] ?? 'GET';
          const path = routeMatch?.[2] ?? '/';

          // Walk the chain to collect ordered middleware names
          const middlewares: string[] = [];
          let currentId = startEdge.target;
          const visited = new Set<string>();

          while (currentId && !visited.has(currentId)) {
            visited.add(currentId);
            const nodeName = idToName[currentId];
            const nodeObj = nodes.find((n) => n.id === currentId);
            const isMiddleware = nodeObj?.data.moduleType?.startsWith('http.middleware.');

            if (isMiddleware && nodeName) {
              middlewares.push(nodeName);
            }

            // Find next edge in chain from currentId
            const nextEdge = middlewareChainEdges.find(
              (e) => e.source === currentId && e.id !== startEdge.id,
            );
            if (nextEdge) {
              // Check if the target is the handler (last in chain)
              const targetNode = nodes.find((n) => n.id === nextEdge.target);
              const targetIsMiddleware = targetNode?.data.moduleType?.startsWith('http.middleware.');
              if (!targetIsMiddleware && targetNode) {
                // This is the handler
                routes.push({
                  method,
                  path,
                  handler: idToName[nextEdge.target],
                  ...(middlewares.length > 0 ? { middlewares } : {}),
                });
                break;
              }
              currentId = nextEdge.target;
            } else {
              // End of chain without explicit handler
              if (middlewares.length > 0) {
                routes.push({ method, path, handler: '', middlewares });
              }
              break;
            }
          }
        }
      }

      if (routes.length > 0) {
        httpConfig.routes = routes;
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

  let hasPositions = false;
  config.modules.forEach((mod, i) => {
    const id = `${mod.type.replace(/\./g, '_')}_${i + 1}`;
    nameToId[mod.name] = id;

    const info = getModuleTypeMap()[mod.type];
    const savedPos = mod.ui_position;
    if (savedPos) hasPositions = true;

    nodes.push({
      id,
      type: nodeComponentType(mod.type),
      position: savedPos ? { x: savedPos.x, y: savedPos.y } : { x: 0, y: 0 },
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

  // Conditional branch edges (from output handles to target modules)
  config.modules.forEach((mod) => {
    if (mod.branches) {
      const sourceId = nameToId[mod.name];
      if (!sourceId) return;
      for (const [handleId, targetName] of Object.entries(mod.branches)) {
        const targetId = nameToId[targetName];
        if (targetId) {
          edges.push(makeEdge(sourceId, targetId, 'conditional', handleId, handleId));
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

  // Auto-wire edges: observability modules auto-wire to the first router
  const autoWireTypes = new Set(['health.checker', 'metrics.collector', 'log.collector']);
  const routerTypes = new Set(['http.router', 'chimux.router']);
  const firstRouter = config.modules.find((m) => routerTypes.has(m.type));
  if (firstRouter) {
    const routerId = nameToId[firstRouter.name];
    if (routerId) {
      for (const mod of config.modules) {
        if (autoWireTypes.has(mod.type)) {
          const modId = nameToId[mod.name];
          if (modId) {
            const key = `${modId}->${routerId}`;
            if (!existingPairs.has(key)) {
              edges.push(makeEdge(modId, routerId, 'auto-wire', 'auto-wired'));
              existingPairs.add(key);
            }
          }
        }
      }
    }
  }

  // Apply dagre layout only when no saved positions exist
  if (!hasPositions) {
    const laid = layoutNodes(nodes, edges);
    for (let i = 0; i < nodes.length; i++) {
      nodes[i].position = laid[i].position;
    }
  }

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
