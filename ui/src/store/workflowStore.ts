import { create } from 'zustand';
import { persist } from 'zustand/middleware';
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
import type { WorkflowConfig, WorkflowTab, CrossWorkflowLink } from '../types/workflow.ts';
import { MODULE_TYPE_MAP as STATIC_MODULE_TYPE_MAP } from '../types/workflow.ts';
import useModuleSchemaStore from './moduleSchemaStore.ts';
import { nodesToConfig, configToNodes, nodeComponentType } from '../utils/serialization.ts';
import { layoutNodes } from '../utils/autoLayout.ts';
import { autoGroupOrphanedNodes } from '../utils/grouping.ts';
import { isPipelineFlowConnection } from '../utils/connectionCompatibility.ts';
import type { Toast } from '../components/toast/ToastContainer.tsx';
import type { ApiWorkflowRecord } from '../utils/api.ts';

export interface WorkflowNodeData extends Record<string, unknown> {
  moduleType: string;
  label: string;
  config: Record<string, unknown>;
  handlerRoutes?: Array<{
    method: string;
    path: string;
    middlewares?: string[];
    pipeline?: { steps: Array<{ name: string; type: string; config?: Record<string, unknown> }> };
  }>;
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
  selectedEdgeId: string | null;
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

  // View level
  viewLevel: 'component' | 'container';
  setViewLevel: (level: 'component' | 'container') => void;

  // UI panels
  showAIPanel: boolean;
  showComponentBrowser: boolean;
  toggleAIPanel: () => void;
  toggleComponentBrowser: () => void;

  onNodesChange: OnNodesChange<WorkflowNode>;
  onEdgesChange: OnEdgesChange;
  onConnect: OnConnect;

  setSelectedNode: (id: string | null) => void;
  setSelectedEdge: (id: string | null) => void;
  removeEdge: (id: string) => void;
  addNode: (type: string, position: { x: number; y: number }) => void;
  removeNode: (id: string) => void;
  updateNodeConfig: (id: string, config: Record<string, unknown>) => void;
  updateNodeName: (id: string, name: string) => void;
  updateHandlerRoutes: (nodeId: string, routes: Array<{
    method: string;
    path: string;
    middlewares?: string[];
    pipeline?: { steps: Array<{ name: string; type: string; config?: Record<string, unknown> }> };
  }>) => void;

  // Preserved workflow/trigger/pipeline sections from imported config
  importedWorkflows: Record<string, unknown>;
  importedTriggers: Record<string, unknown>;
  importedPipelines: Record<string, unknown>;

  exportToConfig: () => WorkflowConfig;
  importFromConfig: (config: WorkflowConfig) => void;
  clearCanvas: () => void;

  // Active v1 workflow record (when editing a workflow from the sidebar)
  activeWorkflowRecord: ApiWorkflowRecord | null;
  setActiveWorkflowRecord: (record: ApiWorkflowRecord | null) => void;

  // Tab management
  tabs: WorkflowTab[];
  activeTabId: string;
  crossWorkflowLinks: CrossWorkflowLink[];
  addTab: () => void;
  closeTab: (tabId: string) => void;
  switchTab: (tabId: string) => void;
  renameTab: (tabId: string, name: string) => void;
  duplicateTab: (tabId: string) => void;
  autoGroupOrphans: () => void;
  autoLayout: () => void;
  addCrossWorkflowLink: (link: CrossWorkflowLink) => void;
  removeCrossWorkflowLink: (linkId: string) => void;

  // Validation errors (transient, not persisted)
  validationErrors: Array<{ nodeId?: string; message: string }>;
  setValidationErrors: (errors: Array<{ nodeId?: string; message: string }>) => void;
  clearValidationErrors: () => void;

  // Connection drag state (smart connection UX)
  connectingFrom: {
    nodeId: string;
    handleId: string | null;
    handleType: 'source' | 'target';
    outputTypes: string[];
  } | null;
  compatibleNodeIds: string[];
  connectionPicklist: { x: number; y: number } | null;
  setConnectingFrom: (info: WorkflowStore['connectingFrom']) => void;
  setCompatibleNodeIds: (ids: string[]) => void;
  showConnectionPicklist: (position: { x: number; y: number }) => void;
  hideConnectionPicklist: () => void;

  // Snap-to-connect state
  snapTargetId: string | null;
  setSnapTargetId: (id: string | null) => void;
}

let toastIdCounter = 0;
let tabCounter = 1;

function makeDefaultTab(id: string, name: string): WorkflowTab {
  return {
    id,
    name,
    nodes: [],
    edges: [],
    undoStack: [],
    redoStack: [],
    dirty: false,
  };
}

const useWorkflowStore = create<WorkflowStore>()(
  persist(
  (set, get) => ({
  nodes: [],
  edges: [],
  selectedNodeId: null,
  selectedEdgeId: null,
  nodeCounter: 0,

  // Preserved workflow/trigger sections from imported config
  importedWorkflows: {},
  importedTriggers: {},
  importedPipelines: {},

  // Active v1 workflow record
  activeWorkflowRecord: null,
  setActiveWorkflowRecord: (record) => set({ activeWorkflowRecord: record }),

  // Tab management
  tabs: [makeDefaultTab('default', 'Workflow 1')],
  activeTabId: 'default',
  crossWorkflowLinks: [],

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

  // View level
  viewLevel: 'component',
  setViewLevel: (level) => set({ viewLevel: level }),

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
    const { source, target } = connection;
    const nodes = get().nodes;
    const sourceNode = nodes.find((n) => n.id === source);
    const targetNode = nodes.find((n) => n.id === target);

    // Auto-detect pipeline-flow edge type
    if (sourceNode && targetNode && isPipelineFlowConnection(sourceNode.data.moduleType, targetNode.data.moduleType)) {
      // Count existing pipeline-flow edges from the same source to determine chain order
      const existingPipelineEdges = get().edges.filter(
        (e) => (e.data as Record<string, unknown> | undefined)?.edgeType === 'pipeline-flow'
      );
      const chainOrder = existingPipelineEdges.length + 1;
      const edgeId = `e-pipeline-flow-${source}-${target}`;
      const edge: Edge = {
        id: edgeId,
        source: source!,
        target: target!,
        sourceHandle: connection.sourceHandle ?? undefined,
        targetHandle: connection.targetHandle ?? undefined,
        data: { edgeType: 'pipeline-flow' as const, chainOrder },
      };
      set({ edges: [...get().edges, edge] });
    } else {
      set({ edges: rfAddEdge(connection, get().edges) });
    }
  },

  setSelectedNode: (id) => set({ selectedNodeId: id, selectedEdgeId: null }),

  setSelectedEdge: (id) => set({ selectedEdgeId: id, selectedNodeId: null }),

  removeEdge: (id) => {
    get().pushHistory();
    set({
      edges: get().edges.filter((e) => e.id !== id),
      selectedEdgeId: get().selectedEdgeId === id ? null : get().selectedEdgeId,
    });
  },

  addNode: (moduleType, position) => {
    const schemaMap = useModuleSchemaStore.getState().moduleTypeMap;
    const info = schemaMap[moduleType] ?? STATIC_MODULE_TYPE_MAP[moduleType];
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

    const currentNodes = get().nodes;
    set({
      nodes: [...currentNodes, newNode],
      nodeCounter: counter,
    });

    // Auto-associate middleware with routers
    if (moduleType.startsWith('http.middleware.')) {
      const routers = currentNodes.filter(
        (n) => n.data.moduleType === 'http.router',
      );
      if (routers.length === 1) {
        // Single router — auto-add to its middleware chain
        const router = routers[0];
        const chain = (router.data.config?.middlewareChain as string[]) ?? [];
        set({
          nodes: get().nodes.map((n) =>
            n.id === router.id
              ? { ...n, data: { ...n.data, config: { ...n.data.config, middlewareChain: [...chain, newNode.data.label] } } }
              : n,
          ),
        });
      } else if (routers.length > 1) {
        // Multiple routers — find nearest by position
        let nearest = routers[0];
        let minDist = Infinity;
        for (const r of routers) {
          const dx = r.position.x - position.x;
          const dy = r.position.y - position.y;
          const dist = dx * dx + dy * dy;
          if (dist < minDist) {
            minDist = dist;
            nearest = r;
          }
        }
        const chain = (nearest.data.config?.middlewareChain as string[]) ?? [];
        set({
          nodes: get().nodes.map((n) =>
            n.id === nearest.id
              ? { ...n, data: { ...n.data, config: { ...n.data.config, middlewareChain: [...chain, newNode.data.label] } } }
              : n,
          ),
        });
        get().addToast(
          `Auto-associated "${newNode.data.label}" with nearest router "${nearest.data.label}"`,
          'info',
        );
      }
    }
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

  updateHandlerRoutes: (nodeId, routes) => {
    get().pushHistory();
    set({
      nodes: get().nodes.map((n) =>
        n.id === nodeId ? { ...n, data: { ...n.data, handlerRoutes: routes } } : n
      ),
    });
  },

  exportToConfig: () => {
    const { nodes, edges, importedWorkflows, importedTriggers, importedPipelines } = get();
    const config = nodesToConfig(nodes, edges);
    // Merge back preserved workflow/trigger/pipeline sections from the imported config.
    // nodesToConfig reconstructs what it can from edges; the imported sections
    // fill in anything that couldn't be reconstructed (routes, subscriptions, etc.)
    if (Object.keys(config.workflows).length === 0 && Object.keys(importedWorkflows).length > 0) {
      config.workflows = importedWorkflows;
    }
    if (Object.keys(config.triggers).length === 0 && Object.keys(importedTriggers).length > 0) {
      config.triggers = importedTriggers;
    }
    if (Object.keys(importedPipelines).length > 0) {
      config.pipelines = importedPipelines;
    }
    return config;
  },

  importFromConfig: (config) => {
    get().pushHistory();
    const { nodes, edges } = configToNodes(config);
    set({
      nodes,
      edges,
      selectedNodeId: null,
      importedWorkflows: config.workflows ?? {},
      importedTriggers: config.triggers ?? {},
      importedPipelines: config.pipelines ?? {},
    });
  },

  clearCanvas: () => {
    get().pushHistory();
    set({ nodes: [], edges: [], selectedNodeId: null, selectedEdgeId: null, nodeCounter: 0, importedWorkflows: {}, importedTriggers: {}, importedPipelines: {} });
  },

  // Validation errors (transient, not persisted)
  validationErrors: [],
  setValidationErrors: (errors) => set({ validationErrors: errors }),
  clearValidationErrors: () => set({ validationErrors: [] }),

  // Connection drag state (smart connection UX)
  connectingFrom: null,
  compatibleNodeIds: [],
  connectionPicklist: null,
  setConnectingFrom: (info) => set({ connectingFrom: info }),
  setCompatibleNodeIds: (ids) => set({ compatibleNodeIds: ids }),
  showConnectionPicklist: (position) => set({ connectionPicklist: position }),
  hideConnectionPicklist: () => set({ connectionPicklist: null, connectingFrom: null, compatibleNodeIds: [] }),

  // Snap-to-connect state
  snapTargetId: null,
  setSnapTargetId: (id) => set({ snapTargetId: id }),

  // Tab actions
  addTab: () => {
    const { tabs, nodes, edges, undoStack, redoStack, activeTabId } = get();
    tabCounter++;
    const newTabId = `tab-${Date.now()}`;
    const newTab = makeDefaultTab(newTabId, `Workflow ${tabCounter}`);

    // Save current state into active tab
    const updatedTabs = tabs.map((t) =>
      t.id === activeTabId
        ? {
            ...t,
            nodes: structuredClone(nodes),
            edges: structuredClone(edges),
            undoStack: structuredClone(undoStack),
            redoStack: structuredClone(redoStack),
          }
        : t,
    );

    set({
      tabs: [...updatedTabs, newTab],
      activeTabId: newTabId,
      nodes: [],
      edges: [],
      undoStack: [],
      redoStack: [],
      selectedNodeId: null,
      selectedEdgeId: null,
      nodeCounter: 0,
    });
  },

  closeTab: (tabId) => {
    const { tabs, activeTabId } = get();
    if (tabs.length <= 1) return;

    const idx = tabs.findIndex((t) => t.id === tabId);
    const newTabs = tabs.filter((t) => t.id !== tabId);

    if (tabId === activeTabId) {
      // Switch to adjacent tab
      const nextTab = newTabs[Math.min(idx, newTabs.length - 1)];
      set({
        tabs: newTabs,
        activeTabId: nextTab.id,
        nodes: nextTab.nodes as WorkflowNode[],
        edges: nextTab.edges,
        undoStack: nextTab.undoStack as HistoryEntry[],
        redoStack: nextTab.redoStack as HistoryEntry[],
        selectedNodeId: null,
        selectedEdgeId: null,
      });
    } else {
      set({ tabs: newTabs });
    }
  },

  switchTab: (tabId) => {
    const { activeTabId, tabs, nodes, edges, undoStack, redoStack } = get();
    if (tabId === activeTabId) return;

    // Save current state into current tab
    const updatedTabs = tabs.map((t) =>
      t.id === activeTabId
        ? {
            ...t,
            nodes: structuredClone(nodes),
            edges: structuredClone(edges),
            undoStack: structuredClone(undoStack),
            redoStack: structuredClone(redoStack),
          }
        : t,
    );

    const newTab = updatedTabs.find((t) => t.id === tabId);
    if (!newTab) return;

    set({
      tabs: updatedTabs,
      activeTabId: tabId,
      nodes: newTab.nodes as WorkflowNode[],
      edges: newTab.edges,
      undoStack: newTab.undoStack as HistoryEntry[],
      redoStack: newTab.redoStack as HistoryEntry[],
      selectedNodeId: null,
      selectedEdgeId: null,
    });
  },

  renameTab: (tabId, name) => {
    set({
      tabs: get().tabs.map((t) => (t.id === tabId ? { ...t, name } : t)),
    });
  },

  duplicateTab: (tabId) => {
    const { tabs, nodes, edges, undoStack, redoStack, activeTabId } = get();

    // Save current state first
    const updatedTabs = tabs.map((t) =>
      t.id === activeTabId
        ? {
            ...t,
            nodes: structuredClone(nodes),
            edges: structuredClone(edges),
            undoStack: structuredClone(undoStack),
            redoStack: structuredClone(redoStack),
          }
        : t,
    );

    const sourceTab = updatedTabs.find((t) => t.id === tabId);
    if (!sourceTab) return;

    tabCounter++;
    const newTabId = `tab-${Date.now()}`;
    const newTab: WorkflowTab = {
      ...structuredClone(sourceTab),
      id: newTabId,
      name: `Copy of ${sourceTab.name}`,
    };

    set({
      tabs: [...updatedTabs, newTab],
      activeTabId: newTabId,
      nodes: newTab.nodes as WorkflowNode[],
      edges: newTab.edges,
      undoStack: newTab.undoStack as HistoryEntry[],
      redoStack: newTab.redoStack as HistoryEntry[],
      selectedNodeId: null,
    });
  },

  autoGroupOrphans: () => {
    const { nodes, edges } = get();
    get().pushHistory();
    const result = autoGroupOrphanedNodes(nodes, edges);
    set({ nodes: result.nodes as WorkflowNode[], edges: result.edges });
  },

  autoLayout: () => {
    const { nodes, edges } = get();
    if (nodes.length === 0) return;
    get().pushHistory();
    const laid = layoutNodes(nodes, edges);
    set({ nodes: laid });
  },

  addCrossWorkflowLink: (link) => {
    set({ crossWorkflowLinks: [...get().crossWorkflowLinks, link] });
  },

  removeCrossWorkflowLink: (linkId) => {
    set({
      crossWorkflowLinks: get().crossWorkflowLinks.filter((l) => l.id !== linkId),
    });
  },
}),
  {
    name: 'workflow-store',
    partialize: (state) => ({
      nodes: state.nodes,
      edges: state.edges,
      nodeCounter: state.nodeCounter,
      importedWorkflows: state.importedWorkflows,
      importedTriggers: state.importedTriggers,
      importedPipelines: state.importedPipelines,
      activeWorkflowRecord: state.activeWorkflowRecord,
      tabs: state.tabs,
      activeTabId: state.activeTabId,
    }),
  },
));

export default useWorkflowStore;
