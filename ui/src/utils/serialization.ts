import yaml from 'js-yaml';
import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import type { ModuleConfig, WorkflowConfig } from '../types/workflow.ts';
import { MODULE_TYPE_MAP } from '../types/workflow.ts';

export function nodesToConfig(nodes: WorkflowNode[], edges: Edge[]): WorkflowConfig {
  const dependencyMap: Record<string, string[]> = {};
  for (const edge of edges) {
    if (!dependencyMap[edge.target]) {
      dependencyMap[edge.target] = [];
    }
    const sourceNode = nodes.find((n) => n.id === edge.source);
    if (sourceNode) {
      dependencyMap[edge.target].push(sourceNode.data.label);
    }
  }

  const modules: ModuleConfig[] = nodes.map((node) => {
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

  const workflows: Record<string, unknown> = {};
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

  const COLS = 3;
  const X_SPACING = 300;
  const Y_SPACING = 200;

  config.modules.forEach((mod, i) => {
    const id = `${mod.type.replace(/\./g, '_')}_${i + 1}`;
    nameToId[mod.name] = id;

    const col = i % COLS;
    const row = Math.floor(i / COLS);

    const info = MODULE_TYPE_MAP[mod.type];

    nodes.push({
      id,
      type: nodeComponentType(mod.type),
      position: { x: col * X_SPACING + 50, y: row * Y_SPACING + 50 },
      data: {
        moduleType: mod.type,
        label: mod.name,
        config: mod.config ?? (info ? { ...info.defaultConfig } : {}),
      },
    });
  });

  config.modules.forEach((mod) => {
    if (mod.dependsOn) {
      const targetId = nameToId[mod.name];
      for (const dep of mod.dependsOn) {
        const sourceId = nameToId[dep];
        if (sourceId && targetId) {
          edges.push({
            id: `e-${sourceId}-${targetId}`,
            source: sourceId,
            target: targetId,
          });
        }
      }
    }
  });

  return { nodes, edges };
}

function nodeComponentType(moduleType: string): string {
  if (moduleType.startsWith('http.middleware.')) return 'middlewareNode';
  if (moduleType.startsWith('http.')) return 'httpNode';
  if (moduleType === 'api.handler') return 'httpNode';
  if (moduleType.startsWith('messaging.')) return 'messagingNode';
  if (moduleType.startsWith('statemachine.') || moduleType.startsWith('state.')) return 'stateMachineNode';
  if (moduleType === 'scheduler.modular') return 'schedulerNode';
  if (moduleType === 'eventlogger.modular' || moduleType === 'eventbus.modular') return 'eventNode';
  if (moduleType === 'httpclient.modular') return 'integrationNode';
  if (moduleType === 'chimux.router') return 'httpNode';
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
