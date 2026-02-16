import { type DragEvent, type MouseEvent, useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { useReactFlow } from '@xyflow/react';
import { CATEGORIES, CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ModuleCategory } from '../../types/workflow.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';

export default function NodePalette() {
  const [expanded, setExpanded] = useState<Record<string, boolean>>(
    Object.fromEntries(CATEGORIES.map((c) => [c.key, false]))
  );
  const [search, setSearch] = useState('');

  const addNode = useWorkflowStore((s) => s.addNode);
  const moduleTypes = useModuleSchemaStore((s) => s.moduleTypes);
  const fetchSchemas = useModuleSchemaStore((s) => s.fetchSchemas);
  const schemasLoaded = useModuleSchemaStore((s) => s.loaded);

  useEffect(() => {
    if (!schemasLoaded) fetchSchemas();
  }, [schemasLoaded, fetchSchemas]);

  const toggle = (key: string) => {
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const onDragStart = (event: DragEvent, moduleType: string) => {
    event.dataTransfer.setData('application/workflow-module-type', moduleType);
    event.dataTransfer.effectAllowed = 'move';
  };

  const { getViewport } = useReactFlow();

  // Debounce guard: prevent rapid double-fire from double-click adding two nodes
  const lastAddTime = useRef(0);
  const addCountRef = useRef(0);
  const addNodeOnce = useCallback((moduleType: string) => {
    const now = Date.now();
    if (now - lastAddTime.current < 400) return; // ignore rapid re-fires
    lastAddTime.current = now;

    // Place new node in the center of the current viewport with a slight
    // offset so consecutive adds don't stack exactly on top of each other.
    const viewport = getViewport();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    // Convert viewport center from screen coords to flow coords:
    // flow_x = (screen_x - viewport.x) / viewport.zoom
    const centerFlowX = (viewportWidth / 2 - viewport.x) / viewport.zoom;
    const centerFlowY = (viewportHeight / 2 - viewport.y) / viewport.zoom;

    // Apply a small spiral-like offset so consecutive nodes don't overlap
    const seq = addCountRef.current++;
    const col = seq % 4;
    const row = Math.floor(seq / 4) % 4;
    const offsetX = (col - 1.5) * 60;
    const offsetY = (row - 1.5) * 50;

    const position = {
      x: Math.round(centerFlowX + offsetX),
      y: Math.round(centerFlowY + offsetY),
    };

    addNode(moduleType, position);
  }, [addNode, getViewport]);

  const onDoubleClick = (e: MouseEvent, moduleType: string) => {
    e.stopPropagation();
    e.preventDefault();
    addNodeOnce(moduleType);
  };

  const searchLower = search.toLowerCase();

  const grouped = useMemo(() => {
    return CATEGORIES.map((cat) => ({
      ...cat,
      types: moduleTypes.filter((t) => {
        if (t.category !== cat.key) return false;
        if (!searchLower) return true;
        return (
          t.label.toLowerCase().includes(searchLower) ||
          t.type.toLowerCase().includes(searchLower)
        );
      }),
    }));
  }, [searchLower, moduleTypes]);

  // Stop all clicks/events on the palette from propagating to sibling components
  const stopPropagation = (e: MouseEvent) => {
    e.stopPropagation();
  };

  return (
    <div
      onClick={stopPropagation}
      onMouseDown={stopPropagation}
      onDoubleClick={stopPropagation}
      style={{
        width: '100%',
        background: '#181825',
        overflowY: 'auto',
        height: '100%',
        padding: '8px 0',
        position: 'relative',
        zIndex: 15,
        pointerEvents: 'auto',
      }}
    >
      <div
        style={{
          padding: '8px 16px',
          fontWeight: 700,
          fontSize: 14,
          color: '#cdd6f4',
          borderBottom: '1px solid #313244',
          marginBottom: 4,
        }}
      >
        Modules
      </div>
      <div style={{ padding: '4px 12px 8px' }}>
        <input
          type="text"
          placeholder="Filter modules..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{
            width: '100%',
            padding: '6px 8px',
            fontSize: 12,
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 4,
            color: '#cdd6f4',
            outline: 'none',
            boxSizing: 'border-box',
          }}
        />
      </div>
      {grouped.map((cat) => (
        <div key={cat.key}>
          {cat.types.length > 0 && (
            <>
              <div
                onClick={(e) => { e.stopPropagation(); toggle(cat.key); }}
                onMouseDown={(e) => e.stopPropagation()}
                style={{
                  padding: '6px 16px',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  color: CATEGORY_COLORS[cat.key as ModuleCategory],
                  fontSize: 12,
                  fontWeight: 600,
                  userSelect: 'none',
                }}
              >
                <span style={{ transform: expanded[cat.key] ? 'rotate(90deg)' : 'none', transition: 'transform 0.15s' }}>
                  &#9654;
                </span>
                {cat.label}
                <span style={{ marginLeft: 'auto', color: '#585b70', fontSize: 11 }}>{cat.types.length}</span>
              </div>
              {expanded[cat.key] &&
                cat.types.map((t) => (
                  <div
                    key={t.type}
                    draggable
                    onDragStart={(e) => onDragStart(e, t.type)}
                    onDoubleClick={(e) => onDoubleClick(e, t.type)}
                    onClick={(e) => e.stopPropagation()}
                    onMouseDown={(e) => e.stopPropagation()}
                    title="Drag to canvas or double-click to add"
                    style={{
                      padding: '5px 16px 5px 28px',
                      cursor: 'grab',
                      fontSize: 12,
                      color: '#bac2de',
                      display: 'flex',
                      alignItems: 'center',
                      gap: 6,
                      transition: 'background 0.1s',
                    }}
                    onMouseOver={(e) => (e.currentTarget.style.background = '#313244')}
                    onMouseOut={(e) => (e.currentTarget.style.background = 'transparent')}
                  >
                    {t.type.startsWith('conditional.') ? (
                      <span
                        style={{
                          width: 8,
                          height: 8,
                          transform: 'rotate(45deg)',
                          background: CATEGORY_COLORS[cat.key as ModuleCategory],
                          flexShrink: 0,
                        }}
                      />
                    ) : (
                      <span
                        style={{
                          width: 8,
                          height: 8,
                          borderRadius: '50%',
                          background: CATEGORY_COLORS[cat.key as ModuleCategory],
                          flexShrink: 0,
                        }}
                      />
                    )}
                    {t.label}
                  </div>
                ))}
            </>
          )}
        </div>
      ))}
    </div>
  );
}
