import { useCallback, useEffect, useMemo, type DragEvent } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  BackgroundVariant,
  useReactFlow,
  type Connection,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { nodeTypes } from '../nodes/index.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import { saveWorkflowConfig } from '../../utils/api.ts';
import type { WorkflowEdgeData } from '../../types/workflow.ts';
import { computeContainerView } from '../../utils/grouping.ts';

export default function WorkflowCanvas() {
  const nodes = useWorkflowStore((s) => s.nodes);
  const edges = useWorkflowStore((s) => s.edges);
  const onNodesChange = useWorkflowStore((s) => s.onNodesChange);
  const onEdgesChange = useWorkflowStore((s) => s.onEdgesChange);
  const onConnect = useWorkflowStore((s) => s.onConnect);
  const addNode = useWorkflowStore((s) => s.addNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const removeNode = useWorkflowStore((s) => s.removeNode);
  const undo = useWorkflowStore((s) => s.undo);
  const redo = useWorkflowStore((s) => s.redo);
  const exportToConfig = useWorkflowStore((s) => s.exportToConfig);
  const addToast = useWorkflowStore((s) => s.addToast);
  const viewLevel = useWorkflowStore((s) => s.viewLevel);

  const { screenToFlowPosition } = useReactFlow();

  const styledEdges: Edge[] = useMemo(() => {
    const edgeStyles: Record<string, { stroke: string; strokeDasharray?: string }> = {
      'dependency': { stroke: '#585b70', strokeDasharray: '5,5' },
      'http-route': { stroke: '#3b82f6' },
      'messaging-subscription': { stroke: '#8b5cf6' },
      'statemachine': { stroke: '#f59e0b' },
      'event': { stroke: '#ef4444' },
      'conditional': { stroke: '#22c55e' },
    };
    return edges.map((edge) => {
      const edgeData = edge.data as WorkflowEdgeData | undefined;
      const edgeType = edgeData?.edgeType;
      if (!edgeType) return edge;
      const style = edgeStyles[edgeType];
      if (!style) return edge;
      return {
        ...edge,
        style: { ...edge.style, stroke: style.stroke, strokeWidth: 2, strokeDasharray: style.strokeDasharray },
        labelStyle: { fill: style.stroke, fontWeight: 600, fontSize: 11 },
        labelBgStyle: { fill: '#1e1e2e', fillOpacity: 0.9 },
      };
    });
  }, [edges]);

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

  const handlePaneClick = useCallback(() => {
    setSelectedNode(null);
  }, [setSelectedNode]);

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT';

      if ((e.key === 'Delete' || e.key === 'Backspace') && !isInput && selectedNodeId) {
        e.preventDefault();
        removeNode(selectedNodeId);
      }

      if (e.key === 'Escape') {
        setSelectedNode(null);
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
          .catch((err) => addToast(`Save failed: ${err.message}`, 'error'));
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedNodeId, removeNode, setSelectedNode, undo, redo, exportToConfig, addToast]);

  return (
    <div style={{ flex: 1, height: '100%' }} onDragOver={handleDragOver} onDrop={handleDrop}>
      <ReactFlow
        nodes={displayNodes}
        edges={displayEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={handleConnect}
        onPaneClick={handlePaneClick}
        nodeTypes={nodeTypes}
        fitView
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{
          type: 'smoothstep',
          animated: true,
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
          style={{ background: '#181825', border: '1px solid #313244', borderRadius: 6 }}
        />
      </ReactFlow>
    </div>
  );
}
