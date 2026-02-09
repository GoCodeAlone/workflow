import { create } from 'zustand';
import {
  type Node,
  type Edge,
  type OnNodesChange,
  type OnEdgesChange,
  type OnConnect,
  applyNodeChanges,
  applyEdgeChanges,
  addEdge as rfAddEdge,
} from '@xyflow/react';
import type { WorkflowConfig } from '../types/workflow.ts';
import { MODULE_TYPE_MAP } from '../types/workflow.ts';
import { nodesToConfig, configToNodes } from '../utils/serialization.ts';

export interface WorkflowNodeData extends Record<string, unknown> {
  moduleType: string;
  label: string;
  config: Record<string, unknown>;
}

export type WorkflowNode = Node<WorkflowNodeData>;

interface WorkflowStore {
  nodes: WorkflowNode[];
  edges: Edge[];
  selectedNodeId: string | null;
  nodeCounter: number;

  onNodesChange: OnNodesChange<WorkflowNode>;
  onEdgesChange: OnEdgesChange;
  onConnect: OnConnect;

  setSelectedNode: (id: string | null) => void;
  addNode: (type: string, position: { x: number; y: number }) => void;
  removeNode: (id: string) => void;
  updateNodeConfig: (id: string, config: Record<string, unknown>) => void;
  updateNodeName: (id: string, name: string) => void;

  exportToConfig: () => WorkflowConfig;
  importFromConfig: (config: WorkflowConfig) => void;
  clearCanvas: () => void;
}

const useWorkflowStore = create<WorkflowStore>((set, get) => ({
  nodes: [],
  edges: [],
  selectedNodeId: null,
  nodeCounter: 0,

  onNodesChange: (changes) => {
    set({ nodes: applyNodeChanges(changes, get().nodes) });
  },

  onEdgesChange: (changes) => {
    set({ edges: applyEdgeChanges(changes, get().edges) });
  },

  onConnect: (connection) => {
    set({ edges: rfAddEdge(connection, get().edges) });
  },

  setSelectedNode: (id) => set({ selectedNodeId: id }),

  addNode: (moduleType, position) => {
    const info = MODULE_TYPE_MAP[moduleType];
    if (!info) return;

    const counter = get().nodeCounter + 1;
    const id = `${moduleType.replace(/\./g, '_')}_${counter}`;
    const newNode: WorkflowNode = {
      id,
      type: nodeComponentType(moduleType),
      position,
      data: {
        moduleType,
        label: `${info.label} ${counter}`,
        config: { ...info.defaultConfig },
      },
    };

    set({
      nodes: [...get().nodes, newNode],
      nodeCounter: counter,
    });
  },

  removeNode: (id) => {
    set({
      nodes: get().nodes.filter((n) => n.id !== id),
      edges: get().edges.filter((e) => e.source !== id && e.target !== id),
      selectedNodeId: get().selectedNodeId === id ? null : get().selectedNodeId,
    });
  },

  updateNodeConfig: (id, config) => {
    set({
      nodes: get().nodes.map((n) =>
        n.id === id ? { ...n, data: { ...n.data, config: { ...n.data.config, ...config } } } : n
      ),
    });
  },

  updateNodeName: (id, name) => {
    set({
      nodes: get().nodes.map((n) =>
        n.id === id ? { ...n, data: { ...n.data, label: name } } : n
      ),
    });
  },

  exportToConfig: () => {
    const { nodes, edges } = get();
    return nodesToConfig(nodes, edges);
  },

  importFromConfig: (config) => {
    const { nodes, edges } = configToNodes(config);
    set({ nodes, edges, selectedNodeId: null });
  },

  clearCanvas: () => {
    set({ nodes: [], edges: [], selectedNodeId: null, nodeCounter: 0 });
  },
}));

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

export default useWorkflowStore;
