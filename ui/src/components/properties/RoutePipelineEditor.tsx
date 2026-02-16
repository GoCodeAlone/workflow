import { useState, useRef, useCallback } from 'react';
import FieldPicker from './FieldPicker.tsx';

interface PipelineStep {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

interface RoutePipelineEditorProps {
  steps: PipelineStep[];
  onChange: (steps: PipelineStep[]) => void;
}

const STEP_TYPES = [
  { value: 'validate', label: 'Validate' },
  { value: 'transform', label: 'Transform' },
  { value: 'conditional', label: 'Conditional' },
  { value: 'set', label: 'Set Values' },
  { value: 'log', label: 'Log' },
  { value: 'publish', label: 'Publish Event' },
  { value: 'http_call', label: 'HTTP Call' },
  { value: 'delegate', label: 'Delegate' },
  { value: 'db_query', label: 'DB Query' },
  { value: 'db_exec', label: 'DB Exec' },
  { value: 'request_parse', label: 'Request Parse' },
  { value: 'json_response', label: 'JSON Response' },
];

// --- Role classification ---

type StepRole = 'start' | 'middleware' | 'transform' | 'action' | 'end';

const STEP_ROLES: Record<string, StepRole> = {
  'step.request_parse': 'start',
  'step.validate': 'middleware',
  'step.conditional': 'middleware',
  'step.set': 'transform',
  'step.transform': 'transform',
  'step.log': 'middleware',
  'step.db_query': 'action',
  'step.db_exec': 'action',
  'step.http_call': 'action',
  'step.delegate': 'action',
  'step.publish': 'action',
  'step.json_response': 'end',
};

const ROLE_COLORS: Record<StepRole, string> = {
  start: '#a6e3a1',
  middleware: '#89b4fa',
  transform: '#fab387',
  action: '#cba6f7',
  end: '#f38ba8',
};

const ROLE_ICONS: Record<StepRole, string> = {
  start: '\u25B6',
  middleware: '\u25C6',
  transform: '\u27F3',
  action: '\u26A1',
  end: '\u25A0',
};

const ROLE_LABELS: Record<StepRole, string> = {
  start: 'Start',
  middleware: 'Middleware',
  transform: 'Transform',
  action: 'Action',
  end: 'Response',
};

function getStepRole(type: string): StepRole {
  return STEP_ROLES['step.' + type] ?? STEP_ROLES[type] ?? 'action';
}

function getStepPreview(step: PipelineStep): string {
  if (!step.config) return '';
  const t = step.type;
  if (t === 'delegate' || t === 'step.delegate') {
    return step.config.service ? `\u2192 ${step.config.service}` : '';
  }
  if (t === 'db_exec' || t === 'db_query' || t === 'step.db_exec' || t === 'step.db_query') {
    const q = String(step.config.query ?? '');
    return q.length > 35 ? q.slice(0, 35) + '...' : q;
  }
  if (t === 'set' || t === 'step.set') {
    const vals = step.config.values;
    if (vals && typeof vals === 'object' && !Array.isArray(vals)) {
      const keys = Object.keys(vals as Record<string, unknown>);
      return keys.length > 0 ? keys.join(', ') : '';
    }
    return '';
  }
  if (t === 'json_response' || t === 'step.json_response') {
    return step.config.status ? `${step.config.status}` : '';
  }
  if (t === 'validate' || t === 'step.validate') {
    return step.config.schema ? 'schema' : '';
  }
  return '';
}

// Puzzle-piece SVG connector dimensions
const NOTCH_WIDTH = 20;
const NOTCH_HEIGHT = 6;
const CARD_HEIGHT = 38;

/** SVG puzzle-piece notch (indent at top of card) */
function PuzzleNotch({ color, prevColor }: { color: string; prevColor: string }) {
  // Gradient from previous step's color to this step's color
  const gradId = `notch-grad-${color}-${prevColor}`.replace(/#/g, '');
  return (
    <div style={{ display: 'flex', justifyContent: 'center', height: NOTCH_HEIGHT, marginTop: -1, position: 'relative', zIndex: 1 }}>
      <svg width={NOTCH_WIDTH + 8} height={NOTCH_HEIGHT} viewBox={`0 0 ${NOTCH_WIDTH + 8} ${NOTCH_HEIGHT}`} style={{ display: 'block' }}>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={prevColor} stopOpacity={0.6} />
            <stop offset="100%" stopColor={color} stopOpacity={0.6} />
          </linearGradient>
        </defs>
        <path
          d={`M0,0 L${(NOTCH_WIDTH + 8 - NOTCH_WIDTH) / 2},0 L${(NOTCH_WIDTH + 8 - NOTCH_WIDTH) / 2 + 2},${NOTCH_HEIGHT} L${(NOTCH_WIDTH + 8 + NOTCH_WIDTH) / 2 - 2},${NOTCH_HEIGHT} L${(NOTCH_WIDTH + 8 + NOTCH_WIDTH) / 2},0 L${NOTCH_WIDTH + 8},0`}
          fill="none"
          stroke={`url(#${gradId})`}
          strokeWidth={2}
        />
      </svg>
    </div>
  );
}

/** SVG puzzle-piece tab (protrusion at bottom of card) */
function PuzzleTab({ color }: { color: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'center', height: NOTCH_HEIGHT, marginBottom: -1, position: 'relative', zIndex: 2 }}>
      <svg width={NOTCH_WIDTH + 8} height={NOTCH_HEIGHT} viewBox={`0 0 ${NOTCH_WIDTH + 8} ${NOTCH_HEIGHT}`} style={{ display: 'block' }}>
        <path
          d={`M${(NOTCH_WIDTH + 8 - NOTCH_WIDTH) / 2},0 L${(NOTCH_WIDTH + 8 - NOTCH_WIDTH) / 2 + 2},${NOTCH_HEIGHT} L${(NOTCH_WIDTH + 8 + NOTCH_WIDTH) / 2 - 2},${NOTCH_HEIGHT} L${(NOTCH_WIDTH + 8 + NOTCH_WIDTH) / 2},0`}
          fill={color + '30'}
          stroke={color + '60'}
          strokeWidth={1}
        />
      </svg>
    </div>
  );
}

export default function RoutePipelineEditor({ steps, onChange }: RoutePipelineEditorProps) {
  const [expanded, setExpanded] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editIdx, setEditIdx] = useState<number | null>(null);
  const [stepName, setStepName] = useState('');
  const [stepType, setStepType] = useState('validate');
  const [stepConfig, setStepConfig] = useState('');
  const [dragIdx, setDragIdx] = useState<number | null>(null);
  const [dragOverIdx, setDragOverIdx] = useState<number | null>(null);
  const configTextareaRef = useRef<HTMLTextAreaElement>(null);

  const insertAtCursor = (text: string) => {
    const ta = configTextareaRef.current;
    if (ta) {
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      const newVal = stepConfig.slice(0, start) + text + stepConfig.slice(end);
      setStepConfig(newVal);
      requestAnimationFrame(() => {
        ta.selectionStart = ta.selectionEnd = start + text.length;
        ta.focus();
      });
    } else {
      setStepConfig(stepConfig + text);
    }
  };

  const handleAdd = () => {
    if (!stepName.trim()) return;
    let config: Record<string, unknown> | undefined;
    if (stepConfig.trim()) {
      try {
        config = JSON.parse(stepConfig);
      } catch {
        config = undefined;
      }
    }
    onChange([...steps, { name: stepName.trim(), type: stepType, config }]);
    resetForm();
  };

  const handleEditSave = () => {
    if (editIdx === null || !stepName.trim()) return;
    let config: Record<string, unknown> | undefined;
    if (stepConfig.trim()) {
      try {
        config = JSON.parse(stepConfig);
      } catch {
        config = undefined;
      }
    }
    const updated = steps.map((s, i) =>
      i === editIdx ? { name: stepName.trim(), type: stepType, config } : s
    );
    onChange(updated);
    resetForm();
  };

  const handleDelete = (idx: number) => {
    onChange(steps.filter((_, i) => i !== idx));
  };

  const startEdit = (idx: number) => {
    const s = steps[idx];
    setEditIdx(idx);
    setStepName(s.name);
    const normalizedType = s.type.startsWith('step.') ? s.type.slice(5) : s.type;
    setStepType(normalizedType);
    setStepConfig(s.config ? JSON.stringify(s.config, null, 2) : '');
    setAdding(false);
  };

  const resetForm = () => {
    setAdding(false);
    setEditIdx(null);
    setStepName('');
    setStepType('validate');
    setStepConfig('');
  };

  const stepTypeLabel = (type: string) => {
    const normalized = type.startsWith('step.') ? type.slice(5) : type;
    return STEP_TYPES.find((t) => t.value === normalized)?.label ?? type;
  };

  // --- Drag and drop handlers ---
  const handleDragStart = useCallback((e: React.DragEvent, idx: number) => {
    setDragIdx(idx);
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', String(idx));
    // Make the drag image semi-transparent
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '0.5';
    }
  }, []);

  const handleDragEnd = useCallback((e: React.DragEvent) => {
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '1';
    }
    setDragIdx(null);
    setDragOverIdx(null);
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent, idx: number) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    setDragOverIdx(idx);
  }, []);

  const handleDrop = useCallback((e: React.DragEvent, dropIdx: number) => {
    e.preventDefault();
    const fromIdx = dragIdx;
    if (fromIdx === null || fromIdx === dropIdx) {
      setDragIdx(null);
      setDragOverIdx(null);
      return;
    }
    const next = [...steps];
    const [moved] = next.splice(fromIdx, 1);
    next.splice(dropIdx, 0, moved);
    onChange(next);
    setDragIdx(null);
    setDragOverIdx(null);
  }, [dragIdx, steps, onChange]);

  return (
    <div style={{ marginTop: 4 }}>
      {/* Header row */}
      <div
        onClick={() => setExpanded((v) => !v)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          cursor: 'pointer',
          fontSize: 10,
          color: '#585b70',
          userSelect: 'none',
        }}
      >
        <span
          style={{
            fontSize: 8,
            transition: 'transform 0.15s',
            transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
          }}
        >
          &#9654;
        </span>
        <span>Pipeline</span>
        {steps.length > 0 && (
          <span
            style={{
              background: '#e879f920',
              color: '#e879f9',
              padding: '0 5px',
              borderRadius: 6,
              fontSize: 9,
            }}
          >
            {steps.length} step{steps.length !== 1 ? 's' : ''}
          </span>
        )}
        <button
          onClick={(e) => {
            e.stopPropagation();
            setAdding(true);
            setEditIdx(null);
            setExpanded(true);
          }}
          style={{
            marginLeft: 'auto',
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 3,
            color: '#e879f9',
            cursor: 'pointer',
            fontSize: 9,
            padding: '0 4px',
          }}
          title="Add pipeline step"
        >
          +
        </button>
      </div>

      {expanded && (
        <div
          style={{
            marginTop: 4,
            background: '#181825',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: '6px 6px 6px 10px',
            position: 'relative',
          }}
        >
          {/* Vertical flow line on left */}
          {steps.length > 1 && (
            <div
              style={{
                position: 'absolute',
                left: 12,
                top: CARD_HEIGHT / 2 + 6,
                bottom: adding ? 60 : CARD_HEIGHT / 2 + 6,
                width: 2,
                background: 'linear-gradient(to bottom, #a6e3a140, #f38ba840)',
                borderRadius: 1,
                zIndex: 0,
              }}
            />
          )}

          {/* Step cards with puzzle connectors */}
          {steps.map((step, i) => {
            const role = getStepRole(step.type);
            const color = ROLE_COLORS[role];
            const icon = ROLE_ICONS[role];
            const roleLabel = ROLE_LABELS[role];
            const preview = getStepPreview(step);
            const isFirst = i === 0;
            const isLast = i === steps.length - 1;
            const prevRole = i > 0 ? getStepRole(steps[i - 1].type) : role;
            const prevColor = i > 0 ? ROLE_COLORS[prevRole] : color;
            const isDragging = dragIdx === i;
            const isDragOver = dragOverIdx === i && dragIdx !== i;

            if (editIdx === i) {
              return (
                <div key={`edit-${i}`} style={{ position: 'relative', zIndex: 1 }}>
                  {!isFirst && <PuzzleNotch color={color} prevColor={prevColor} />}
                  <div
                    style={{
                      background: '#1e1e2e',
                      borderLeft: `3px solid ${color}`,
                      borderRadius: 6,
                      padding: 6,
                      marginLeft: 12,
                    }}
                  >
                    {renderForm(true)}
                  </div>
                  {!isLast && <PuzzleTab color={color} />}
                </div>
              );
            }

            return (
              <div
                key={`${step.name}-${i}`}
                style={{ position: 'relative', zIndex: 1 }}
                draggable
                onDragStart={(e) => handleDragStart(e, i)}
                onDragEnd={handleDragEnd}
                onDragOver={(e) => handleDragOver(e, i)}
                onDrop={(e) => handleDrop(e, i)}
              >
                {/* Puzzle connector between cards */}
                {!isFirst && <PuzzleNotch color={color} prevColor={prevColor} />}

                {/* Step card */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '4px 6px',
                    minHeight: CARD_HEIGHT,
                    background: isDragOver
                      ? '#313244'
                      : isDragging
                        ? '#1e1e2e80'
                        : '#1e1e2e',
                    borderLeft: `3px solid ${color}`,
                    borderTop: isFirst ? `1px solid ${color}40` : undefined,
                    borderBottom: isLast ? `1px solid ${color}40` : undefined,
                    borderRight: `1px solid ${color}20`,
                    borderRadius: isFirst && isLast
                      ? 6
                      : isFirst
                        ? '6px 6px 2px 2px'
                        : isLast
                          ? '2px 2px 6px 6px'
                          : '2px',
                    marginLeft: 12,
                    position: 'relative',
                    transition: 'background 0.15s, box-shadow 0.15s',
                    boxShadow: isDragOver
                      ? `0 0 0 1px ${color}60, 0 2px 8px ${color}20`
                      : isDragging
                        ? '0 4px 12px rgba(0,0,0,0.4)'
                        : 'none',
                    cursor: 'grab',
                  }}
                  onMouseEnter={(e) => {
                    if (!isDragging) e.currentTarget.style.background = '#242438';
                  }}
                  onMouseLeave={(e) => {
                    if (!isDragging) e.currentTarget.style.background = '#1e1e2e';
                  }}
                >
                  {/* Flow dot on the vertical line */}
                  <div
                    style={{
                      position: 'absolute',
                      left: -16,
                      top: '50%',
                      transform: 'translateY(-50%)',
                      width: 8,
                      height: 8,
                      borderRadius: '50%',
                      background: color,
                      border: '2px solid #181825',
                      zIndex: 2,
                    }}
                  />

                  {/* Drag handle */}
                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 1,
                      cursor: 'grab',
                      opacity: 0.3,
                      flexShrink: 0,
                      padding: '0 2px',
                    }}
                    title="Drag to reorder"
                  >
                    <div style={{ width: 6, height: 1.5, background: '#585b70', borderRadius: 1 }} />
                    <div style={{ width: 6, height: 1.5, background: '#585b70', borderRadius: 1 }} />
                    <div style={{ width: 6, height: 1.5, background: '#585b70', borderRadius: 1 }} />
                  </div>

                  {/* Role icon + indicator */}
                  <div
                    style={{
                      width: 20,
                      height: 20,
                      borderRadius: 5,
                      background: color + '20',
                      color: color,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 10,
                      flexShrink: 0,
                      border: `1px solid ${color}30`,
                    }}
                    title={roleLabel}
                  >
                    {icon}
                  </div>

                  {/* Name + type label */}
                  <div style={{ flex: 1, minWidth: 0, overflow: 'hidden' }}>
                    <div
                      style={{
                        color: '#cdd6f4',
                        fontWeight: 600,
                        fontSize: 11,
                        lineHeight: '14px',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                    >
                      {step.name}
                    </div>
                    <div
                      style={{
                        color: color,
                        fontSize: 9,
                        lineHeight: '11px',
                        opacity: 0.7,
                      }}
                    >
                      {stepTypeLabel(step.type)}
                    </div>
                  </div>

                  {/* Config preview */}
                  {preview && (
                    <span
                      style={{
                        color: '#585b70',
                        fontSize: 9,
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        maxWidth: 80,
                        flexShrink: 1,
                      }}
                      title={preview}
                    >
                      {preview}
                    </span>
                  )}

                  {/* Action buttons */}
                  <div style={{ display: 'flex', alignItems: 'center', gap: 2, flexShrink: 0 }}>
                    <button
                      onClick={(e) => { e.stopPropagation(); startEdit(i); }}
                      style={{ ...iconBtnStyle, opacity: 0.5 }}
                      title="Edit step"
                    >
                      &#9998;
                    </button>
                    <button
                      onClick={(e) => { e.stopPropagation(); handleDelete(i); }}
                      style={{ ...iconBtnStyle, color: '#f38ba8', opacity: 0.6 }}
                      title="Delete step"
                    >
                      &#10005;
                    </button>
                  </div>
                </div>

                {/* Puzzle tab at bottom */}
                {!isLast && <PuzzleTab color={color} />}
              </div>
            );
          })}

          {adding && (
            <div
              style={{
                padding: 6,
                borderTop: steps.length > 0 ? '1px solid #313244' : undefined,
                marginTop: steps.length > 0 ? 6 : 0,
                marginLeft: 12,
              }}
            >
              {renderForm(false)}
            </div>
          )}

          {steps.length === 0 && !adding && (
            <div style={{ padding: '6px 4px', color: '#585b70', fontSize: 10, textAlign: 'center' }}>
              No pipeline steps
            </div>
          )}
        </div>
      )}
    </div>
  );

  function renderForm(isEdit: boolean) {
    const stepIdx = isEdit && editIdx !== null ? editIdx : steps.length;
    const preceding = steps.slice(0, stepIdx).map((s) => ({
      name: s.name,
      type: s.type,
      config: s.config,
    }));

    return (
      <>
        <div style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
          <select
            value={stepType}
            onChange={(e) => setStepType(e.target.value)}
            style={{ ...formInputStyle, width: 100, flexShrink: 0 }}
          >
            {STEP_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
          <input
            value={stepName}
            onChange={(e) => setStepName(e.target.value)}
            placeholder="Step name..."
            style={{ ...formInputStyle, flex: 1 }}
            autoFocus
            onKeyDown={(e) => {
              if (e.key === 'Enter') { if (isEdit) { handleEditSave(); } else { handleAdd(); } }
              if (e.key === 'Escape') resetForm();
            }}
          />
        </div>
        <div style={{ position: 'relative' }}>
          {preceding.length > 0 && (
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 3 }}>
              <FieldPicker
                precedingSteps={preceding}
                onSelect={(expr) => insertAtCursor(expr)}
              />
            </div>
          )}
          <textarea
            ref={configTextareaRef}
            value={stepConfig}
            onChange={(e) => setStepConfig(e.target.value)}
            placeholder='{"key": "value"}'
            rows={3}
            style={{
              ...formInputStyle,
              width: '100%',
              resize: 'vertical',
              fontFamily: 'monospace',
              fontSize: 10,
              marginBottom: 4,
            }}
          />
        </div>
        <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
          <button onClick={resetForm} style={cancelBtnStyle}>
            Cancel
          </button>
          <button onClick={isEdit ? handleEditSave : handleAdd} style={saveBtnStyle}>
            {isEdit ? 'Save' : 'Add'}
          </button>
        </div>
      </>
    );
  }
}

const formInputStyle: React.CSSProperties = {
  padding: '3px 5px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 3,
  color: '#cdd6f4',
  fontSize: 10,
  outline: 'none',
  boxSizing: 'border-box',
};

const iconBtnStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#585b70',
  cursor: 'pointer',
  fontSize: 8,
  padding: '0 1px',
  lineHeight: 1,
};

const cancelBtnStyle: React.CSSProperties = {
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 3,
  color: '#a6adc8',
  cursor: 'pointer',
  fontSize: 9,
  padding: '2px 8px',
};

const saveBtnStyle: React.CSSProperties = {
  background: '#e879f930',
  border: '1px solid #e879f950',
  borderRadius: 3,
  color: '#e879f9',
  cursor: 'pointer',
  fontSize: 9,
  padding: '2px 8px',
};
