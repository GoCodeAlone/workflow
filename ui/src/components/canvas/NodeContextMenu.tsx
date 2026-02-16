import useWorkflowStore from '../../store/workflowStore.ts';

interface NodeContextMenuProps {
  x: number;
  y: number;
  nodeId: string;
  onClose: () => void;
}

export default function NodeContextMenu({ x, y, nodeId, onClose }: NodeContextMenuProps) {
  const edges = useWorkflowStore((s) => s.edges);
  const removeEdge = useWorkflowStore((s) => s.removeEdge);
  const removeNode = useWorkflowStore((s) => s.removeNode);

  const connectedEdges = edges.filter(
    (e) => e.source === nodeId || e.target === nodeId
  ).filter((e) => (e.data as Record<string, unknown> | undefined)?.edgeType !== 'auto-wire');

  return (
    <div
      className="context-menu"
      style={{ position: 'fixed', left: x, top: y, zIndex: 1000 }}
    >
      {connectedEdges.length > 0 && (
        <button
          className="context-menu-item"
          onClick={() => {
            connectedEdges.forEach((e) => removeEdge(e.id));
            onClose();
          }}
        >
          Disconnect All ({connectedEdges.length})
        </button>
      )}
      <button
        className="context-menu-item context-menu-item-danger"
        onClick={() => {
          removeNode(nodeId);
          onClose();
        }}
      >
        Delete Node
      </button>
      <button className="context-menu-item" onClick={onClose}>
        Cancel
      </button>
    </div>
  );
}
