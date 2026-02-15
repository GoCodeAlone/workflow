import { useState } from 'react';

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
];

export default function RoutePipelineEditor({ steps, onChange }: RoutePipelineEditorProps) {
  const [expanded, setExpanded] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editIdx, setEditIdx] = useState<number | null>(null);
  const [stepName, setStepName] = useState('');
  const [stepType, setStepType] = useState('validate');
  const [stepConfig, setStepConfig] = useState('');

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
    setStepType(s.type);
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

  const stepTypeLabel = (type: string) =>
    STEP_TYPES.find((t) => t.value === type)?.label ?? type;

  return (
    <div style={{ marginTop: 4 }}>
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
            borderRadius: 4,
            padding: 4,
          }}
        >
          {steps.map((step, i) => {
            if (editIdx === i) {
              return (
                <div key={`edit-${i}`} style={{ padding: 4, borderBottom: '1px solid #1e1e2e' }}>
                  {renderForm(true)}
                </div>
              );
            }
            return (
              <div
                key={`${step.name}-${i}`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 4,
                  padding: '3px 4px',
                  fontSize: 10,
                  borderBottom: i < steps.length - 1 ? '1px solid #1e1e2e' : undefined,
                }}
              >
                <span
                  style={{
                    width: 14,
                    height: 14,
                    borderRadius: '50%',
                    background: '#e879f930',
                    color: '#e879f9',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontSize: 8,
                    fontWeight: 700,
                    flexShrink: 0,
                  }}
                >
                  {i + 1}
                </span>
                <span style={{ color: '#a6adc8', fontWeight: 600, fontSize: 10 }}>
                  {stepTypeLabel(step.type)}
                </span>
                <span style={{ color: '#585b70', fontSize: 9, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {step.name}
                </span>
                <button
                  onClick={() => handleMoveUp(i)}
                  disabled={i === 0}
                  style={arrowBtnStyle}
                  title="Move up"
                >
                  &#9650;
                </button>
                <button
                  onClick={() => handleMoveDown(i)}
                  disabled={i >= steps.length - 1}
                  style={arrowBtnStyle}
                  title="Move down"
                >
                  &#9660;
                </button>
                <button
                  onClick={() => startEdit(i)}
                  style={{ background: 'none', border: 'none', color: '#585b70', cursor: 'pointer', fontSize: 9, padding: 0 }}
                  title="Edit step"
                >
                  &#9998;
                </button>
                <button
                  onClick={() => handleDelete(i)}
                  style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 9, padding: 0 }}
                  title="Delete step"
                >
                  x
                </button>
              </div>
            );
          })}

          {adding && <div style={{ padding: 4, borderTop: steps.length > 0 ? '1px solid #313244' : undefined }}>{renderForm(false)}</div>}

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
    return (
      <>
        <div style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
          <select
            value={stepType}
            onChange={(e) => setStepType(e.target.value)}
            style={{ ...formInputStyle, width: 90, flexShrink: 0 }}
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
        <textarea
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

const arrowBtnStyle: React.CSSProperties = {
  background: 'none',
  border: 'none',
  color: '#585b70',
  cursor: 'pointer',
  fontSize: 8,
  padding: 0,
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
