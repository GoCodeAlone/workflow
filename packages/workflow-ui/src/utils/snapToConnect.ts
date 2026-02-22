import type { Edge } from '@xyflow/react';
import type { WorkflowNode } from '../store/workflowStore.ts';
import type { ModuleTypeInfo } from '../types/workflow.ts';
import { isTypeCompatible, canAcceptIncoming, canAcceptOutgoing } from './connectionCompatibility.ts';

export const SNAP_THRESHOLD = 50;  // px proximity to trigger snap
export const PUZZLE_OVERLAP = 8;   // px — matches SVG tab/notch height

export interface SnapCandidate {
  targetNodeId: string;
  snappedPosition: { x: number; y: number };
  sourceNodeId: string;  // the node that acts as edge source (top node)
  targetForEdge: string; // the node that acts as edge target (bottom node)
}

/** Check if a module type has an input handle, derived from schema limits */
function hasInputHandle(moduleType: string, moduleTypeMap: Record<string, ModuleTypeInfo>): boolean {
  const info = moduleTypeMap[moduleType];
  if (info?.maxIncoming === 0) return false;
  if (moduleType.startsWith('trigger.')) return false;
  return true;
}

/** Check if a module type has an output handle, derived from schema limits */
function hasOutputHandle(moduleType: string, moduleTypeMap: Record<string, ModuleTypeInfo>): boolean {
  const info = moduleTypeMap[moduleType];
  if (info?.maxOutgoing === 0) return false;
  return true;
}

/** Check if two nodes have compatible I/O types for connection */
function areTypesCompatible(
  sourceModuleType: string,
  targetModuleType: string,
  moduleTypeMap: Record<string, ModuleTypeInfo>,
): boolean {
  const sourceInfo = moduleTypeMap[sourceModuleType];
  const targetInfo = moduleTypeMap[targetModuleType];

  // Allow connection if either has no IO signature defined
  if (!sourceInfo?.ioSignature || !targetInfo?.ioSignature) return true;

  const outputTypes = sourceInfo.ioSignature.outputs.map((o) => o.type);
  const inputTypes = targetInfo.ioSignature.inputs.map((i) => i.type);

  // Allow if either side has no ports (acts as a passthrough)
  if (outputTypes.length === 0 || inputTypes.length === 0) return true;

  return outputTypes.some((outType) =>
    inputTypes.some((inType) => isTypeCompatible(outType, inType)),
  );
}

/** Get a node's rendered dimensions via DOM measurement, falling back to defaults */
function getNodeDimensions(
  nodeId: string,
  zoom: number,
): { width: number; height: number } {
  const el = document.querySelector(`[data-id="${nodeId}"]`);
  if (el) {
    const rect = el.getBoundingClientRect();
    return {
      width: rect.width / zoom,
      height: rect.height / zoom,
    };
  }
  return { width: 180, height: 80 };
}

/**
 * Find the best snap candidate for a dragged node.
 * Returns the closest compatible node within SNAP_THRESHOLD, or null.
 */
export function findSnapCandidate(
  draggedNodeId: string,
  draggedPos: { x: number; y: number },
  allNodes: WorkflowNode[],
  edges: Edge[],
  moduleTypeMap: Record<string, ModuleTypeInfo>,
  zoom: number,
): SnapCandidate | null {
  const draggedNode = allNodes.find((n) => n.id === draggedNodeId);
  if (!draggedNode?.position) return null;

  const draggedType = draggedNode.data.moduleType;
  const draggedDims = getNodeDimensions(draggedNodeId, zoom);

  // Build set of existing connections involving the dragged node
  const connectedPairs = new Set<string>();
  for (const edge of edges) {
    if (edge.source === draggedNodeId) connectedPairs.add(edge.target);
    if (edge.target === draggedNodeId) connectedPairs.add(edge.source);
  }

  let best: SnapCandidate | null = null;
  let bestDist = Infinity;

  for (const candidate of allNodes) {
    if (candidate.id === draggedNodeId) continue;
    if (connectedPairs.has(candidate.id)) continue;
    if (!candidate.position) continue;

    const candidateType = candidate.data.moduleType;
    const candidateDims = getNodeDimensions(candidate.id, zoom);

    // Case 1: Dragged node snaps BELOW candidate
    // (candidate output tab → dragged node input notch)
    if (hasOutputHandle(candidateType, moduleTypeMap) && hasInputHandle(draggedType, moduleTypeMap)) {
      if (areTypesCompatible(candidateType, draggedType, moduleTypeMap)
        && canAcceptOutgoing(candidate.id, edges, moduleTypeMap, candidateType)
        && canAcceptIncoming(draggedNodeId, edges, moduleTypeMap, draggedType)) {
        // Snap point: dragged node's top-center aligns with candidate's bottom-center
        const candidateBottomCenter = {
          x: candidate.position.x + candidateDims.width / 2,
          y: candidate.position.y + candidateDims.height,
        };
        const draggedTopCenter = {
          x: draggedPos.x + draggedDims.width / 2,
          y: draggedPos.y,
        };

        const dx = candidateBottomCenter.x - draggedTopCenter.x;
        const dy = candidateBottomCenter.y - draggedTopCenter.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        if (dist < SNAP_THRESHOLD && dist < bestDist) {
          bestDist = dist;
          best = {
            targetNodeId: candidate.id,
            snappedPosition: {
              x: candidate.position.x + candidateDims.width / 2 - draggedDims.width / 2,
              y: candidate.position.y + candidateDims.height - PUZZLE_OVERLAP,
            },
            sourceNodeId: candidate.id,
            targetForEdge: draggedNodeId,
          };
        }
      }
    }

    // Case 2: Dragged node snaps ABOVE candidate
    // (dragged node output tab → candidate input notch)
    if (hasOutputHandle(draggedType, moduleTypeMap) && hasInputHandle(candidateType, moduleTypeMap)) {
      if (areTypesCompatible(draggedType, candidateType, moduleTypeMap)
        && canAcceptOutgoing(draggedNodeId, edges, moduleTypeMap, draggedType)
        && canAcceptIncoming(candidate.id, edges, moduleTypeMap, candidateType)) {
        // Snap point: dragged node's bottom-center aligns with candidate's top-center
        const candidateTopCenter = {
          x: candidate.position.x + candidateDims.width / 2,
          y: candidate.position.y,
        };
        const draggedBottomCenter = {
          x: draggedPos.x + draggedDims.width / 2,
          y: draggedPos.y + draggedDims.height,
        };

        const dx = candidateTopCenter.x - draggedBottomCenter.x;
        const dy = candidateTopCenter.y - draggedBottomCenter.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        if (dist < SNAP_THRESHOLD && dist < bestDist) {
          bestDist = dist;
          best = {
            targetNodeId: candidate.id,
            snappedPosition: {
              x: candidate.position.x + candidateDims.width / 2 - draggedDims.width / 2,
              y: candidate.position.y - draggedDims.height + PUZZLE_OVERLAP,
            },
            sourceNodeId: draggedNodeId,
            targetForEdge: candidate.id,
          };
        }
      }
    }
  }

  return best;
}
