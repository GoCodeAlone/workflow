import { useState, useRef } from 'react';
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

export default function RoutePipelineEditor({ steps, onChange }: RoutePipelineEditorProps) {
  const [expanded, setExpanded] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editIdx, setEditIdx] = useState<number | null>(null);
  const [stepName, setStepName] = useState('');
  const [stepType, setStepType] = useState('validate');
  const [stepConfig, setStepConfig] = useState('');
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

  const handleMoveUp = (idx: number) => {
    if (idx === 0) return;
    const next = [...steps];
    [next[idx - 1], next[idx]] = [next[idx], next[idx - 1]];
    onChange(next);
  };

  const handleMoveDown = (idx: number) => {
    if (idx >= steps.length - 1) return;
    const next = [...steps];
    [next[idx], next[idx + 1]] = [next[idx + 1], next[idx]];
    onChange(next);
  };

  const startEdit = (idx: number) => {
    const s = steps[idx];
    setEditIdx(idx);
    setStepName(s.name);
    // Strip 'step.' prefix so the value matches STEP_TYPES dropdown values
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
            padding: 6,
          }}
        >
          {/* Step cards */}
          {steps.map((step, i) => {
            const role = getStepRole(step.type);
            const color = ROLE_COLORS[role];
            const icon = ROLE_ICONS[role];
            const roleLabel = ROLE_LABELS[role];
            const preview = getStepPreview(step);
            const isFirst = i === 0;
            const isLast = i === steps.length - 1;

            if (editIdx === i) {
              return (
                <div key={`edit-${i}`}>
                  {!isFirst && (
                    <div style={{ display: 'flex', justifyContent: 'center' }}>
                      <div style={{ width: 2, height: 6, background: color + '60' }} />
                    </div>
                  )}
                  <div
                    style={{
                      background: '#1e1e2e',
                      borderLeft: `3px solid ${color}`,
                      borderRadius: isFirst && isLast ? 6 : isFirst ? '6px 6px 0 0' : isLast ? '0 0 6px 6px' : 0,
                      padding: 6,
                    }}
                  >
                    {renderForm(true)}
                  </div>
                </div>
              );
            }

            return (
              <div key={`${step.name}-${i}`}>
                {/* Connector line between cards */}
                {!isFirst && (
                  <div style={{ display: 'flex', justifyContent: 'center' }}>
                    <div
                      style={{
                        width: 2,
                        height: 6,
                        background: color + '60',
                      }}
                    />
                  </div>
                )}
                {/* Step card */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '4px 6px',
                    background: '#1e1e2e',
                    borderLeft: `3px solid ${color}`,
                    borderRadius: isFirst && isLast
                      ? 6
                      : isFirst
                        ? '6px 6px 0 0'
                        : isLast
                          ? '0 0 6px 6px'
                          : 0,
                    marginTop: isFirst ? 0 : -1,
                    position: 'relative',
                    transition: 'background 0.15s',
                  }}
                  onMouseEnter={(e) => { e.currentTarget.style.background = '#242438'; }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = '#1e1e2e'; }}
                >
                  {/* Role icon + indicator */}
                  <div
                    style={{
                      width: 18,
                      height: 18,
                      borderRadius: 4,
                      background: color + '20',
                      color: color,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 9,
                      flexShrink: 0,
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
                      onClick={() => handleMoveUp(i)}
                      disabled={i === 0}
                      style={{
                        ...iconBtnStyle,
                        opacity: i === 0 ? 0.25 : 0.5,
                      }}
                      title="Move up"
                    >
                      &#9650;
                    </button>
                    <button
                      onClick={() => handleMoveDown(i)}
                      disabled={i >= steps.length - 1}
                      style={{
                        ...iconBtnStyle,
                        opacity: i >= steps.length - 1 ? 0.25 : 0.5,
                      }}
                      title="Move down"
                    >
                      &#9660;
                    </button>
                    <button
                      onClick={() => startEdit(i)}
                      style={{ ...iconBtnStyle, opacity: 0.5 }}
                      title="Edit step"
                    >
                      &#9998;
                    </button>
                    <button
                      onClick={() => handleDelete(i)}
                      style={{ ...iconBtnStyle, color: '#f38ba8', opacity: 0.6 }}
                      title="Delete step"
                    >
                      &#10005;
                    </button>
                  </div>
                </div>
              </div>
            );
          })}

          {adding && (
            <div
              style={{
                padding: 6,
                borderTop: steps.length > 0 ? '1px solid #313244' : undefined,
                marginTop: steps.length > 0 ? 6 : 0,
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
    // Compute preceding steps for the field picker
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
