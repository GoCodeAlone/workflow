import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  BackgroundVariant,
  useReactFlow,
  applyNodeChanges,
  type Connection,
  type Edge,
  type Node as RFNode,
  type OnConnectStart,
  type OnConnectEnd,
  type IsValidConnection,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { nodeTypes } from '../nodes/index.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import useUILayoutStore from '../../store/uiLayoutStore.ts';
import { saveWorkflowConfig } from '../../utils/api.ts';
import type { WorkflowEdgeData } from '../../types/workflow.ts';
import { computeContainerView } from '../../utils/grouping.ts';
import { isTypeCompatible, getOutputTypes, getInputTypes, getCompatibleNodes, canAcceptIncoming, canAcceptOutgoing } from '../../utils/connectionCompatibility.ts';
import { findSnapCandidate } from '../../utils/snapToConnect.ts';
import ConnectionPicklist from './ConnectionPicklist.tsx';
import DeletableEdge from './DeletableEdge.tsx';
import EdgeContextMenu from './EdgeContextMenu.tsx';
import NodeContextMenu from './NodeContextMenu.tsx';

const edgeTypes = { deletable: DeletableEdge };

interface ContextMenuState {
  type: 'edge' | 'node';
  x: number;
  y: number;
  id: string;
}

export default function WorkflowCanvas() {
  const nodes = useWorkflowStore((s) => s.nodes);
  const edges = useWorkflowStore((s) => s.edges);
  const onNodesChange = useWorkflowStore((s) => s.onNodesChange);
  const onEdgesChange = useWorkflowStore((s) => s.onEdgesChange);
  const onConnect = useWorkflowStore((s) => s.onConnect);
  const addNode = useWorkflowStore((s) => s.addNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const selectedEdgeId = useWorkflowStore((s) => s.selectedEdgeId);
  const setSelectedEdge = useWorkflowStore((s) => s.setSelectedEdge);
  const removeEdge = useWorkflowStore((s) => s.removeEdge);
  const removeNode = useWorkflowStore((s) => s.removeNode);
  const undo = useWorkflowStore((s) => s.undo);
  const redo = useWorkflowStore((s) => s.redo);
  const exportToConfig = useWorkflowStore((s) => s.exportToConfig);
  const addToast = useWorkflowStore((s) => s.addToast);
  const viewLevel = useWorkflowStore((s) => s.viewLevel);
  const setConnectingFrom = useWorkflowStore((s) => s.setConnectingFrom);
  const setCompatibleNodeIds = useWorkflowStore((s) => s.setCompatibleNodeIds);
  const showConnectionPicklist = useWorkflowStore((s) => s.showConnectionPicklist);
  const hideConnectionPicklist = useWorkflowStore((s) => s.hideConnectionPicklist);
  const connectionPicklist = useWorkflowStore((s) => s.connectionPicklist);
  const connectingFrom = useWorkflowStore((s) => s.connectingFrom);
  const setSnapTargetId = useWorkflowStore((s) => s.setSnapTargetId);
  const pushHistory = useWorkflowStore((s) => s.pushHistory);

  const moduleTypeMap = useModuleSchemaStore((s) => s.moduleTypeMap);

  const propertyPanelCollapsed = useUILayoutStore((s) => s.propertyPanelCollapsed);
  const setPropertyPanelCollapsed = useUILayoutStore((s) => s.setPropertyPanelCollapsed);

  const { screenToFlowPosition, getViewport } = useReactFlow();
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);

  const styledEdges: Edge[] = useMemo(() => {
    // Clean solid lines with opacity/thickness differentiation instead of dashes
    const edgeStyles: Record<string, { stroke: string; strokeWidth: number; opacity?: number }> = {
      'dependency':              { stroke: '#585b70', strokeWidth: 1.5, opacity: 0.4 },
      'http-route':              { stroke: '#3b82f6', strokeWidth: 2.5 },
      'messaging-subscription':  { stroke: '#8b5cf6', strokeWidth: 2.5 },
      'statemachine':            { stroke: '#f59e0b', strokeWidth: 2.5 },
      'event':                   { stroke: '#ef4444', strokeWidth: 2 },
      'conditional':             { stroke: '#22c55e', strokeWidth: 2 },
      'auto-wire':               { stroke: '#45475a', strokeWidth: 1, opacity: 0.3 },
      'middleware-chain':        { stroke: '#fab387', strokeWidth: 2.5 },
      'pipeline-flow':           { stroke: '#e879f9', strokeWidth: 3 },
    };
    return edges.map((edge) => {
      const edgeData = edge.data as WorkflowEdgeData | undefined;
      const edgeType = edgeData?.edgeType;
      const isAutoWire = edgeType === 'auto-wire';
      const isSelected = edge.id === selectedEdgeId && !isAutoWire;

      if (!edgeType) {
        // Untyped edge — apply selected styling if applicable
        if (isSelected) {
          const baseStroke = (edge.style?.stroke as string) || '#585b70';
          return {
            ...edge,
            type: 'deletable',
            style: {
              ...edge.style,
              strokeWidth: ((edge.style?.strokeWidth as number) || 2) + 1.5,
              opacity: 1,
              filter: `drop-shadow(0 0 4px ${baseStroke})`,
            },
          };
        }
        return { ...edge, type: 'deletable' };
      }
      const style = edgeStyles[edgeType];
      if (!style) return edge;
      const isMiddlewareChain = edgeType === 'middleware-chain';
      const isPipelineFlow = edgeType === 'pipeline-flow';

      // For middleware-chain and pipeline-flow edges, show chain order as a step number label
      const chainOrder = (isMiddlewareChain || isPipelineFlow) ? edgeData?.chainOrder : undefined;

      // Selected edge: increase strokeWidth, add glow, full opacity
      const edgeStrokeWidth = isSelected ? style.strokeWidth + 1.5 : style.strokeWidth;
      const edgeOpacity = isSelected ? 1 : (style.opacity ?? 1);
      const edgeFilter = isSelected ? `drop-shadow(0 0 4px ${style.stroke})` : undefined;

      return {
        ...edge,
        ...(!isAutoWire ? { type: 'deletable' as const } : {}),
        ...(chainOrder !== undefined
          ? { label: `#${chainOrder}` }
          : {}),
        style: {
          ...edge.style,
          stroke: style.stroke,
          strokeWidth: edgeStrokeWidth,
          opacity: edgeOpacity,
          filter: edgeFilter,
        },
        labelStyle: (isMiddlewareChain || isPipelineFlow)
          ? { fill: style.stroke, fontWeight: 700, fontSize: 14 }
          : { fill: style.stroke, fontWeight: 600, fontSize: 11 },
        labelBgStyle: (isMiddlewareChain || isPipelineFlow)
          ? { fill: '#1e1e2e', fillOpacity: 0.95, rx: 10, ry: 10 }
          : { fill: '#1e1e2e', fillOpacity: 0.9 },
        labelBgPadding: (isMiddlewareChain || isPipelineFlow) ? [4, 4] as [number, number] : undefined,
        ...(isAutoWire ? { deletable: false, selectable: false, animated: false } : {}),
      };
    });
  }, [edges, selectedEdgeId]);

  const { nodes: displayNodes, edges: displayEdges } = useMemo(() => {
    if (viewLevel === 'container' && nodes.length > 0) {
      return computeContainerView(nodes, styledEdges);
    }
    return { nodes, edges: styledEdges };
  }, [viewLevel, nodes, styledEdges]);

  const handleDragOver = useCallback((event: DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const handleDrop = useCallback(
    (event: DragEvent) => {
      event.preventDefault();
      const moduleType = event.dataTransfer.getData('application/workflow-module-type');
      if (!moduleType) return;

      const position = screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      position.x = Math.max(position.x, 20);
      position.y = Math.max(position.y, 20);

      addNode(moduleType, position);
    },
    [addNode, screenToFlowPosition]
  );

  const handleConnect = useCallback(
    (connection: Connection) => {
      onConnect(connection);
    },
    [onConnect]
  );

  const handleNodeDoubleClick = useCallback(
    (_event: React.MouseEvent, node: { id: string }) => {
      setSelectedNode(node.id);
      if (propertyPanelCollapsed) {
        setPropertyPanelCollapsed(false);
      }
    },
    [setSelectedNode, propertyPanelCollapsed, setPropertyPanelCollapsed]
  );

  const handleEdgeClick = useCallback((_event: React.MouseEvent, edge: Edge) => {
    const edgeData = edge.data as WorkflowEdgeData | undefined;
    if (edgeData?.edgeType === 'auto-wire') return; // auto-wire not selectable
    setSelectedEdge(edge.id);
  }, [setSelectedEdge]);

  const handlePaneClick = useCallback(() => {
    setSelectedNode(null);
    setSelectedEdge(null);
    setContextMenu(null);
    // Close picklist on pane click if not clicking within it
    if (connectionPicklist) {
      hideConnectionPicklist();
    }
  }, [setSelectedNode, setSelectedEdge, connectionPicklist, hideConnectionPicklist]);

  const handleEdgeContextMenu = useCallback(
    (event: React.MouseEvent, edge: Edge) => {
      event.preventDefault();
      const edgeData = edge.data as WorkflowEdgeData | undefined;
      if (edgeData?.edgeType === 'auto-wire') return;
      setContextMenu({ type: 'edge', x: event.clientX, y: event.clientY, id: edge.id });
    },
    []
  );

  const handleNodeContextMenu = useCallback(
    (event: React.MouseEvent, node: RFNode) => {
      event.preventDefault();
      setContextMenu({ type: 'node', x: event.clientX, y: event.clientY, id: node.id });
    },
    []
  );

  const closeContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

  // onConnectStart: identify source node's types and highlight compatible targets
  const handleConnectStart: OnConnectStart = useCallback(
    (_event, params) => {
      const { nodeId, handleType } = params;
      if (!nodeId) return;

      const sourceNode = nodes.find((n) => n.id === nodeId);
      if (!sourceNode) return;

      const info = moduleTypeMap[sourceNode.data.moduleType];
      if (!info?.ioSignature) return;

      // Determine the relevant types based on handle direction
      const relevantTypes = handleType === 'source'
        ? getOutputTypes(info)
        : getInputTypes(info);

      if (relevantTypes.length === 0) return;

      setConnectingFrom({
        nodeId,
        handleId: params.handleId ?? null,
        handleType: handleType ?? 'source',
        outputTypes: relevantTypes,
      });

      const compatible = getCompatibleNodes(
        nodeId,
        relevantTypes,
        handleType ?? 'source',
        nodes,
        moduleTypeMap,
      );
      setCompatibleNodeIds(compatible);
    },
    [nodes, moduleTypeMap, setConnectingFrom, setCompatibleNodeIds]
  );

  // onConnectEnd: if dropped on empty canvas, show picklist
  const handleConnectEnd: OnConnectEnd = useCallback(
    (event) => {
      const currentConnecting = useWorkflowStore.getState().connectingFrom;
      if (!currentConnecting) return;

      // Check if the connection was completed (landed on a valid target)
      const targetElement = (event as MouseEvent).target as HTMLElement;
      const isHandle = targetElement?.closest('.react-flow__handle');

      if (!isHandle) {
        // Dropped on empty canvas - show picklist
        const clientX = (event as MouseEvent).clientX;
        const clientY = (event as MouseEvent).clientY;

        // Position relative to the wrapper
        const rect = wrapperRef.current?.getBoundingClientRect();
        if (rect) {
          showConnectionPicklist({
            x: clientX - rect.left,
            y: clientY - rect.top,
          });
        }
        // Keep connectingFrom state alive for the picklist
        return;
      }

      // Connection completed normally - clear state
      setConnectingFrom(null);
      setCompatibleNodeIds([]);
    },
    [setConnectingFrom, setCompatibleNodeIds, showConnectionPicklist]
  );

  // isValidConnection: check type compatibility
  const isValidConnection: IsValidConnection = useCallback(
    (connection: Edge | Connection) => {
      const { source, target } = connection;
      if (!source || !target) return false;

      // Prevent self-connections
      if (source === target) return false;

      // Prevent duplicate edges
      const existingEdges = useWorkflowStore.getState().edges;
      const hasDuplicate = existingEdges.some(
        (e) => e.source === source && e.target === target,
      );
      if (hasDuplicate) return false;

      // Check I/O type compatibility
      const sourceNode = nodes.find((n) => n.id === source);
      const targetNode = nodes.find((n) => n.id === target);
      if (!sourceNode || !targetNode) return false;

      const sourceInfo = moduleTypeMap[sourceNode.data.moduleType];
      const targetInfo = moduleTypeMap[targetNode.data.moduleType];
      if (!sourceInfo?.ioSignature || !targetInfo?.ioSignature) return true; // Allow if no signature defined

      const outputTypes = sourceInfo.ioSignature.outputs.map((o) => o.type);
      const inputTypes = targetInfo.ioSignature.inputs.map((i) => i.type);

      if (outputTypes.length === 0 || inputTypes.length === 0) return true;

      const typesMatch = outputTypes.some((outType) =>
        inputTypes.some((inType) => isTypeCompatible(outType, inType)),
      );
      if (!typesMatch) return false;

      // Enforce connection limits
      if (!canAcceptOutgoing(source, existingEdges, moduleTypeMap, sourceNode.data.moduleType)) return false;
      if (!canAcceptIncoming(target, existingEdges, moduleTypeMap, targetNode.data.moduleType)) return false;

      return true;
    },
    [nodes, moduleTypeMap]
  );

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT';

      if ((e.key === 'Delete' || e.key === 'Backspace') && !isInput) {
        if (selectedNodeId) {
          e.preventDefault();
          removeNode(selectedNodeId);
        } else if (selectedEdgeId) {
          e.preventDefault();
          removeEdge(selectedEdgeId);
        }
      }

      if (e.key === 'Escape') {
        setSelectedNode(null);
        setSelectedEdge(null);
        setContextMenu(null);
        hideConnectionPicklist();
      }

      if (e.key === 'z' && (e.ctrlKey || e.metaKey) && !e.shiftKey) {
        e.preventDefault();
        undo();
      }

      if ((e.key === 'y' && (e.ctrlKey || e.metaKey)) || (e.key === 'z' && (e.ctrlKey || e.metaKey) && e.shiftKey)) {
        e.preventDefault();
        redo();
      }

      if (e.key === 's' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        const config = exportToConfig();
        saveWorkflowConfig(config)
          .then(() => addToast('Workflow saved to server', 'success'))
          .catch((err: Error) => addToast(`Save failed: ${err.message}`, 'error'));
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedNodeId, selectedEdgeId, removeNode, removeEdge, setSelectedNode, setSelectedEdge, undo, redo, exportToConfig, addToast, hideConnectionPicklist]);

  // Snap-to-connect: detect proximity during drag
  // Uses getState() for fresh nodes/edges to avoid stale closures during rapid drag events
  const handleNodeDrag = useCallback(
    (_event: React.MouseEvent, node: RFNode) => {
      const { zoom } = getViewport();
      const state = useWorkflowStore.getState();
      const schemas = useModuleSchemaStore.getState().moduleTypeMap;
      const candidate = findSnapCandidate(
        node.id,
        node.position,
        state.nodes,
        state.edges,
        schemas,
        zoom,
      );
      setSnapTargetId(candidate ? candidate.targetNodeId : null);
    },
    [getViewport, setSnapTargetId]
  );

  // Snap-to-connect: finalize on drag stop
  // Uses getState() for fresh state; onConnect already calls pushHistory internally
  const handleNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: RFNode) => {
      const { zoom } = getViewport();
      const state = useWorkflowStore.getState();
      const schemas = useModuleSchemaStore.getState().moduleTypeMap;
      const candidate = findSnapCandidate(
        node.id,
        node.position,
        state.nodes,
        state.edges,
        schemas,
        zoom,
      );

      if (candidate) {
        // Push history BEFORE position change so undo captures pre-snap state
        pushHistory();

        // Move dragged node to snapped position
        const positionChange = {
          id: node.id,
          type: 'position' as const,
          position: candidate.snappedPosition,
        };
        const freshNodes = useWorkflowStore.getState().nodes;
        const updatedNodes = applyNodeChanges([positionChange], freshNodes);
        useWorkflowStore.setState({ nodes: updatedNodes });

        // Auto-create edge (source = top node, target = bottom node)
        // onConnect also calls pushHistory internally, so undo requires 2 steps
        onConnect({
          source: candidate.sourceNodeId,
          target: candidate.targetForEdge,
          sourceHandle: null,
          targetHandle: null,
        });
      }

      setSnapTargetId(null);
    },
    [getViewport, pushHistory, onConnect, setSnapTargetId]
  );

  // Handle picklist node creation
  const handlePicklistSelect = useCallback(
    (moduleType: string) => {
      const cf = useWorkflowStore.getState().connectingFrom;
      const picklistPos = useWorkflowStore.getState().connectionPicklist;
      if (!cf || !picklistPos) return;

      // Convert screen-relative position to flow position
      const rect = wrapperRef.current?.getBoundingClientRect();
      if (!rect) return;

      const flowPos = screenToFlowPosition({
        x: picklistPos.x + rect.left,
        y: picklistPos.y + rect.top,
      });
      flowPos.x = Math.max(flowPos.x, 20);
      flowPos.y = Math.max(flowPos.y, 20);

      // Add the node
      addNode(moduleType, flowPos);

      // Get the newly added node (last node in store)
      const latestNodes = useWorkflowStore.getState().nodes;
      const newNode = latestNodes[latestNodes.length - 1];
      if (!newNode) return;

      // Create edge from source to new node (or new node to source depending on handle type)
      if (cf.handleType === 'source') {
        onConnect({ source: cf.nodeId, target: newNode.id, sourceHandle: cf.handleId, targetHandle: null });
      } else {
        onConnect({ source: newNode.id, target: cf.nodeId, sourceHandle: null, targetHandle: cf.handleId });
      }

      hideConnectionPicklist();
    },
    [addNode, onConnect, screenToFlowPosition, hideConnectionPicklist]
  );

  return (
    <div
      ref={wrapperRef}
      style={{ flex: 1, height: '100%', position: 'relative' }}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <ReactFlow
        nodes={displayNodes}
        edges={displayEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={handleConnect}
        onConnectStart={handleConnectStart}
        onConnectEnd={handleConnectEnd}
        isValidConnection={isValidConnection}
        onNodeDrag={handleNodeDrag}
        onNodeDragStop={handleNodeDragStop}
        onEdgeClick={handleEdgeClick}
        onEdgeContextMenu={handleEdgeContextMenu}
        onNodeContextMenu={handleNodeContextMenu}
        onPaneClick={handlePaneClick}
        onNodeDoubleClick={handleNodeDoubleClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{
          type: 'deletable',
          animated: false,
          style: { stroke: '#585b70', strokeWidth: 2 },
        }}
        style={{ background: '#1e1e2e' }}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="#313244" />
        <Controls
          style={{ background: '#181825', border: '1px solid #313244', borderRadius: 6 }}
        />
        <MiniMap
          nodeColor={() => '#45475a'}
          maskColor="rgba(0,0,0,0.5)"
          style={{ background: '#181825', border: '1px solid #313244', borderRadius: 6, zIndex: 4 }}
          pannable
          zoomable
        />
      </ReactFlow>
      {connectionPicklist && connectingFrom && (
        <ConnectionPicklist
          position={connectionPicklist}
          connectingFrom={connectingFrom}
          onSelect={handlePicklistSelect}
          onClose={hideConnectionPicklist}
        />
      )}
      {contextMenu?.type === 'edge' && (
        <EdgeContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          edgeId={contextMenu.id}
          onClose={closeContextMenu}
        />
      )}
      {contextMenu?.type === 'node' && (
        <NodeContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          nodeId={contextMenu.id}
          onClose={closeContextMenu}
        />
      )}
    </div>
  );
}
