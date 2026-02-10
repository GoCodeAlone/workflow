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
import type { Toast } from '../components/toast/ToastContainer.tsx';

export interface WorkflowNodeData extends Record<string, unknown> {
  moduleType: string;
  label: string;
  config: Record<string, unknown>;
}

export type WorkflowNode = Node<WorkflowNodeData>;

interface HistoryEntry {
  nodes: WorkflowNode[];
  edges: Edge[];
}

interface WorkflowStore {
  nodes: WorkflowNode[];
  edges: Edge[];
  selectedNodeId: string | null;
  nodeCounter: number;

  // Toast notifications
  toasts: Toast[];
  addToast: (message: string, type: Toast['type']) => void;
  removeToast: (id: string) => void;

  // Undo/redo
  undoStack: HistoryEntry[];
  redoStack: HistoryEntry[];
  pushHistory: () => void;
  undo: () => void;
  redo: () => void;

  // UI panels
  showAIPanel: boolean;
  showComponentBrowser: boolean;
  toggleAIPanel: () => void;
  toggleComponentBrowser: () => void;

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

let toastIdCounter = 0;

const useWorkflowStore = create<WorkflowStore>((set, get) => ({
  nodes: [],
  edges: [],
  selectedNodeId: null,
  nodeCounter: 0,

  // Toast
  toasts: [],
  addToast: (message, type) => {
    const id = `toast-${++toastIdCounter}`;
    set({ toasts: [...get().toasts, { id, message, type }] });
  },
  removeToast: (id) => {
    set({ toasts: get().toasts.filter((t) => t.id !== id) });
  },

  // Undo/redo
  undoStack: [],
  redoStack: [],
  pushHistory: () => {
    const { nodes, edges, undoStack } = get();
    const entry: HistoryEntry = {
      nodes: JSON.parse(JSON.stringify(nodes)),
      edges: JSON.parse(JSON.stringify(edges)),
    };
    set({
      undoStack: [...undoStack.slice(-49), entry],
      redoStack: [],
    });
  },
  undo: () => {
    const { undoStack, nodes, edges } = get();
    if (undoStack.length === 0) return;
    const prev = undoStack[undoStack.length - 1];
    set({
      undoStack: undoStack.slice(0, -1),
      redoStack: [
        ...get().redoStack,
        { nodes: JSON.parse(JSON.stringify(nodes)), edges: JSON.parse(JSON.stringify(edges)) },
      ],
      nodes: prev.nodes,
      edges: prev.edges,
    });
  },
  redo: () => {
    const { redoStack, nodes, edges } = get();
    if (redoStack.length === 0) return;
    const next = redoStack[redoStack.length - 1];
    set({
      redoStack: redoStack.slice(0, -1),
      undoStack: [
        ...get().undoStack,
        { nodes: JSON.parse(JSON.stringify(nodes)), edges: JSON.parse(JSON.stringify(edges)) },
      ],
      nodes: next.nodes,
      edges: next.edges,
    });
  },

  // UI panels
  showAIPanel: false,
  showComponentBrowser: false,
  toggleAIPanel: () => set({ showAIPanel: !get().showAIPanel }),
  toggleComponentBrowser: () => set({ showComponentBrowser: !get().showComponentBrowser }),

  onNodesChange: (changes) => {
    set({ nodes: applyNodeChanges(changes, get().nodes) });
  },

  onEdgesChange: (changes) => {
    set({ edges: applyEdgeChanges(changes, get().edges) });
  },

  onConnect: (connection) => {
    get().pushHistory();
    set({ edges: rfAddEdge(connection, get().edges) });
  },

  setSelectedNode: (id) => set({ selectedNodeId: id }),

  addNode: (moduleType, position) => {
    const info = MODULE_TYPE_MAP[moduleType];
    if (!info) return;

    get().pushHistory();
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
    get().pushHistory();
    set({
      nodes: get().nodes.filter((n) => n.id !== id),
      edges: get().edges.filter((e) => e.source !== id && e.target !== id),
      selectedNodeId: get().selectedNodeId === id ? null : get().selectedNodeId,
    });
  },

  updateNodeConfig: (id, config) => {
    get().pushHistory();
    set({
      nodes: get().nodes.map((n) =>
        n.id === id ? { ...n, data: { ...n.data, config: { ...n.data.config, ...config } } } : n
      ),
    });
  },

  updateNodeName: (id, name) => {
    get().pushHistory();
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
    get().pushHistory();
    const { nodes, edges } = configToNodes(config);
    set({ nodes, edges, selectedNodeId: null });
  },

  clearCanvas: () => {
    get().pushHistory();
    set({ nodes: [], edges: [], selectedNodeId: null, nodeCounter: 0 });
  },
}));

function nodeComponentType(moduleType: string): string {
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
  return 'infrastructureNode';
}

export default useWorkflowStore;
