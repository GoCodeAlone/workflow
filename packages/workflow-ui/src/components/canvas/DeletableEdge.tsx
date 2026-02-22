import { memo } from 'react';
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react';
import useWorkflowStore from '../../store/workflowStore.ts';

function DeletableEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style,
  markerEnd,
  data,
}: EdgeProps) {
  const removeEdge = useWorkflowStore((s) => s.removeEdge);

  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const isAutoWire = data?.edgeType === 'auto-wire';

  return (
    <>
      <BaseEdge path={edgePath} markerEnd={markerEnd} style={style} />
      {!isAutoWire && (
        <EdgeLabelRenderer>
          <div
            className="edge-delete-button"
            style={{
              position: 'absolute',
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
              pointerEvents: 'all',
            }}
          >
            {data?.label != null && (
              <span className="edge-label">{String(data.label)}</span>
            )}
            <button
              className="edge-delete-btn"
              onClick={(e) => {
                e.stopPropagation();
                removeEdge(id);
              }}
              title="Delete connection"
            >
              &times;
            </button>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export default memo(DeletableEdge);
