import { type DragEvent, useState } from 'react';
import { CATEGORIES, MODULE_TYPES, CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ModuleCategory } from '../../types/workflow.ts';

export default function NodePalette() {
  const [expanded, setExpanded] = useState<Record<string, boolean>>(
    Object.fromEntries(CATEGORIES.map((c) => [c.key, true]))
  );

  const toggle = (key: string) => {
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const onDragStart = (event: DragEvent, moduleType: string) => {
    event.dataTransfer.setData('application/workflow-module-type', moduleType);
    event.dataTransfer.effectAllowed = 'move';
  };

  const grouped = CATEGORIES.map((cat) => ({
    ...cat,
    types: MODULE_TYPES.filter((t) => t.category === cat.key),
  }));

  return (
    <div
      style={{
        width: 240,
        background: '#181825',
        borderRight: '1px solid #313244',
        overflowY: 'auto',
        height: '100%',
        padding: '8px 0',
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
      {grouped.map((cat) => (
        <div key={cat.key}>
          <div
            onClick={() => toggle(cat.key)}
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
        </div>
      ))}
    </div>
  );
}
