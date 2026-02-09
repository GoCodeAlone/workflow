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
  return toYaml(config, 0);
}

function toYaml(value: unknown, indent: number): string {
  const prefix = '  '.repeat(indent);

  if (value === null || value === undefined) return 'null';
  if (typeof value === 'string') return value.includes(':') || value.includes('#') ? `"${value}"` : value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);

  if (Array.isArray(value)) {
    if (value.length === 0) return '[]';
    return value.map((item) => {
      if (typeof item === 'object' && item !== null) {
        const entries = Object.entries(item as Record<string, unknown>);
        const firstEntry = entries[0];
        const rest = entries.slice(1);
        let result = `${prefix}- ${firstEntry[0]}: ${toYaml(firstEntry[1], indent + 2)}`;
        for (const [k, v] of rest) {
          result += `\n${prefix}  ${k}: ${toYaml(v, indent + 2)}`;
        }
        return result;
      }
      return `${prefix}- ${toYaml(item, indent + 1)}`;
    }).join('\n');
  }

  if (typeof value === 'object') {
    const obj = value as Record<string, unknown>;
    const entries = Object.entries(obj);
    if (entries.length === 0) return '{}';
    return entries
      .map(([k, v]) => {
        if (typeof v === 'object' && v !== null && !Array.isArray(v) && Object.keys(v as object).length > 0) {
          return `${prefix}${k}:\n${toYaml(v, indent + 1)}`;
        }
        if (Array.isArray(v) && v.length > 0) {
          return `${prefix}${k}:\n${toYaml(v, indent + 1)}`;
        }
        return `${prefix}${k}: ${toYaml(v, indent + 1)}`;
      })
      .join('\n');
  }

  return String(value);
}

export function parseYaml(text: string): WorkflowConfig {
  // Simple YAML parser for workflow configs - handles basic cases
  // For production use, a full YAML parser library would be recommended
  const lines = text.split('\n');
  const result: Record<string, unknown> = {};
  const modules: ModuleConfig[] = [];
  let currentModule: Partial<ModuleConfig> | null = null;
  let inModules = false;
  let inConfig = false;
  let configObj: Record<string, unknown> = {};
  let inDependsOn = false;
  const deps: string[] = [];

  for (const rawLine of lines) {
    const line = rawLine.trimEnd();
    if (!line || line.startsWith('#')) continue;

    if (line === 'modules:') {
      inModules = true;
      continue;
    }
    if (line === 'workflows:' || line === 'triggers:') {
      if (currentModule) {
        if (Object.keys(configObj).length > 0) currentModule.config = { ...configObj };
        if (deps.length > 0) currentModule.dependsOn = [...deps];
        modules.push(currentModule as ModuleConfig);
      }
      inModules = false;
      inConfig = false;
      inDependsOn = false;
      currentModule = null;
      continue;
    }

    if (inModules) {
      const itemMatch = line.match(/^\s*-\s+(\w+):\s*(.*)/);
      if (itemMatch) {
        if (currentModule) {
          if (Object.keys(configObj).length > 0) currentModule.config = { ...configObj };
          if (deps.length > 0) currentModule.dependsOn = [...deps];
          modules.push(currentModule as ModuleConfig);
        }
        currentModule = {};
        configObj = {};
        deps.length = 0;
        inConfig = false;
        inDependsOn = false;
        currentModule[itemMatch[1] as keyof ModuleConfig] = itemMatch[2] as never;
        continue;
      }

      if (currentModule) {
        const kvMatch = line.match(/^\s+(\w+):\s*(.*)/);
        if (kvMatch) {
          const [, key, value] = kvMatch;
          if (key === 'config') {
            inConfig = true;
            inDependsOn = false;
            continue;
          }
          if (key === 'dependsOn') {
            inDependsOn = true;
            inConfig = false;
            continue;
          }
          if (inConfig) {
            configObj[key] = parseValue(value);
          } else if (!inDependsOn) {
            inConfig = false;
            inDependsOn = false;
            currentModule[key as keyof ModuleConfig] = value as never;
          }
        }
        const depMatch = line.match(/^\s+-\s+(.*)/);
        if (depMatch && inDependsOn) {
          deps.push(depMatch[1].trim());
        }
      }
    }
  }

  if (currentModule) {
    if (Object.keys(configObj).length > 0) currentModule.config = { ...configObj };
    if (deps.length > 0) currentModule.dependsOn = [...deps];
    modules.push(currentModule as ModuleConfig);
  }

  result.modules = modules;
  result.workflows = {};
  result.triggers = {};

  return result as unknown as WorkflowConfig;
}

function parseValue(s: string): unknown {
  s = s.trim();
  if (s === 'true') return true;
  if (s === 'false') return false;
  if (s === 'null') return null;
  const num = Number(s);
  if (!isNaN(num) && s !== '') return num;
  if (s.startsWith('"') && s.endsWith('"')) return s.slice(1, -1);
  return s;
}
