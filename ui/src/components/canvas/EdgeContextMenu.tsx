import useWorkflowStore from '../../store/workflowStore.ts';

interface EdgeContextMenuProps {
  x: number;
  y: number;
  edgeId: string;
  onClose: () => void;
}

export default function EdgeContextMenu({ x, y, edgeId, onClose }: EdgeContextMenuProps) {
  const removeEdge = useWorkflowStore((s) => s.removeEdge);

  return (
    <div
      className="context-menu"
      style={{ position: 'fixed', left: x, top: y, zIndex: 1000 }}
    >
      <button
        className="context-menu-item context-menu-item-danger"
        onClick={() => {
          removeEdge(edgeId);
          onClose();
        }}
      >
        Delete Connection
      </button>
      <button className="context-menu-item" onClick={onClose}>
        Cancel
      </button>
    </div>
  );
}
