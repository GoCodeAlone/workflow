import { useState, useEffect } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import { CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ConfigFieldDef, IOPort } from '../../types/workflow.ts';

export default function PropertyPanel() {
  const nodes = useWorkflowStore((s) => s.nodes);
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const updateNodeConfig = useWorkflowStore((s) => s.updateNodeConfig);
  const updateNodeName = useWorkflowStore((s) => s.updateNodeName);
  const removeNode = useWorkflowStore((s) => s.removeNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);

  const moduleTypeMap = useModuleSchemaStore((s) => s.moduleTypeMap);
  const fetchSchemas = useModuleSchemaStore((s) => s.fetchSchemas);
  const schemasLoaded = useModuleSchemaStore((s) => s.loaded);

  useEffect(() => {
    if (!schemasLoaded) fetchSchemas();
  }, [schemasLoaded, fetchSchemas]);

  const node = nodes.find((n) => n.id === selectedNodeId);
  if (!node) {
    return (
      <div
        style={{
          width: 280,
          background: '#181825',
          borderLeft: '1px solid #313244',
          padding: 16,
          color: '#585b70',
          fontSize: 13,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        Select a node to edit its properties
      </div>
    );
  }

  const info = moduleTypeMap[node.data.moduleType];
  const color = info ? CATEGORY_COLORS[info.category] : '#64748b';
  const fields: ConfigFieldDef[] = info?.configFields ?? [];

  const handleFieldChange = (key: string, value: unknown) => {
    updateNodeConfig(node.id, { [key]: value });
  };

  return (
    <div
      style={{
        width: 280,
        background: '#181825',
        borderLeft: '1px solid #313244',
        overflowY: 'auto',
        height: '100%',
        fontSize: 12,
        color: '#cdd6f4',
      }}
    >
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ fontWeight: 700, fontSize: 14 }}>Properties</span>
        <button
          onClick={() => setSelectedNode(null)}
          style={{
            background: 'none',
            border: 'none',
            color: '#585b70',
            cursor: 'pointer',
            fontSize: 16,
            padding: '0 4px',
          }}
        >
          x
        </button>
      </div>

      <div style={{ padding: 16 }}>
        {/* Name */}
        <label style={{ display: 'block', marginBottom: 12 }}>
          <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 4 }}>Name</span>
          <input
            value={node.data.label}
            onChange={(e) => updateNodeName(node.id, e.target.value)}
            style={inputStyle}
          />
        </label>

        {/* Type badge */}
        <div style={{ marginBottom: 16 }}>
          <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 4 }}>Type</span>
          <span
            style={{
              background: `${color}20`,
              color,
              padding: '3px 8px',
              borderRadius: 4,
              fontSize: 11,
              fontWeight: 500,
            }}
          >
            {node.data.moduleType}
          </span>
        </div>

        {/* Config fields */}
        {fields.length > 0 && (
          <div style={{ marginBottom: 16 }}>
            <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 8, fontWeight: 600 }}>
              Configuration
            </span>
            {fields.map((field) => (
              <label key={field.key} style={{ display: 'block', marginBottom: 10 }}>
                <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 3 }}>
                  {field.label}
                  {field.required && <span style={{ color: '#f38ba8', marginLeft: 2 }}>*</span>}
                </span>
                {field.type === 'select' ? (
                  <select
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    style={inputStyle}
                  >
                    <option value="">--</option>
                    {field.options?.map((opt) => (
                      <option key={opt} value={opt}>
                        {opt}
                      </option>
                    ))}
                  </select>
                ) : field.type === 'number' ? (
                  <input
                    type="number"
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, Number(e.target.value))}
                    placeholder={field.placeholder}
                    style={inputStyle}
                  />
                ) : field.type === 'boolean' ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <input
                      type="checkbox"
                      checked={Boolean(node.data.config[field.key] ?? field.defaultValue ?? false)}
                      onChange={(e) => handleFieldChange(field.key, e.target.checked)}
                    />
                    {field.description && (
                      <span style={{ color: '#585b70', fontSize: 10 }}>{field.description}</span>
                    )}
                  </div>
                ) : field.type === 'json' ? (
                  <textarea
                    value={
                      typeof node.data.config[field.key] === 'string'
                        ? (node.data.config[field.key] as string)
                        : JSON.stringify(node.data.config[field.key] ?? '', null, 2)
                    }
                    onChange={(e) => {
                      try {
                        handleFieldChange(field.key, JSON.parse(e.target.value));
                      } catch {
                        handleFieldChange(field.key, e.target.value);
                      }
                    }}
                    rows={4}
                    placeholder={field.placeholder}
                    style={{ ...inputStyle, resize: 'vertical', fontFamily: 'monospace' }}
                  />
                ) : (
                  <input
                    type="text"
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    placeholder={field.placeholder}
                    style={inputStyle}
                  />
                )}
                {field.description && field.type !== 'boolean' && (
                  <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginTop: 2 }}>{field.description}</span>
                )}
              </label>
            ))}
          </div>
        )}

        {/* I/O Signature */}
        {info?.ioSignature && (info.ioSignature.inputs.length > 0 || info.ioSignature.outputs.length > 0) && (
          <div style={{ marginBottom: 16 }}>
            <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 8, fontWeight: 600 }}>
              I/O Ports
            </span>
            {info.ioSignature.inputs.length > 0 && (
              <div style={{ marginBottom: 6 }}>
                <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginBottom: 2 }}>Inputs</span>
                {info.ioSignature.inputs.map((port: IOPort) => (
                  <div key={port.name} style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: 11, padding: '1px 0' }}>
                    <span style={{ width: 6, height: 6, borderRadius: '50%', background: color, opacity: 0.6 }} />
                    <span style={{ color: '#cdd6f4' }}>{port.name}</span>
                    <span style={{ color: '#585b70' }}>{port.type}</span>
                  </div>
                ))}
              </div>
            )}
            {info.ioSignature.outputs.length > 0 && (
              <div>
                <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginBottom: 2 }}>Outputs</span>
                {info.ioSignature.outputs.map((port: IOPort) => (
                  <div key={port.name} style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: 11, padding: '1px 0' }}>
                    <span style={{ width: 6, height: 6, borderRadius: '50%', background: color, opacity: 0.6 }} />
                    <span style={{ color: '#cdd6f4' }}>{port.name}</span>
                    <span style={{ color: '#585b70' }}>{port.type}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Conditional-specific UI */}
        {node.data.moduleType === 'conditional.switch' && (
          <ConditionalCasesEditor
            cases={(node.data.config?.cases as string[]) ?? []}
            onChange={(cases) => updateNodeConfig(node.id, { cases })}
          />
        )}
        {node.data.moduleType === 'conditional.expression' && (
          <ConditionalOutputsEditor
            outputs={(node.data.config?.outputs as string[]) ?? []}
            onChange={(outputs) => updateNodeConfig(node.id, { outputs })}
          />
        )}

        {/* Delete */}
        <button
          onClick={() => {
            removeNode(node.id);
          }}
          style={{
            width: '100%',
            padding: '8px 12px',
            background: '#45475a',
            border: '1px solid #585b70',
            borderRadius: 6,
            color: '#f38ba8',
            cursor: 'pointer',
            fontSize: 12,
            fontWeight: 500,
          }}
        >
          Delete Node
        </button>
      </div>
    </div>
  );
}

function ConditionalCasesEditor({ cases, onChange }: { cases: string[]; onChange: (c: string[]) => void }) {
  const [newCase, setNewCase] = useState('');
  return (
    <div style={{ marginBottom: 16 }}>
      <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 6, fontWeight: 600 }}>
        Switch Cases
      </span>
      {cases.map((c, i) => (
        <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
          <span style={{ color: '#cdd6f4', fontSize: 11, flex: 1 }}>{c}</span>
          <button
            onClick={() => onChange(cases.filter((_, j) => j !== i))}
            style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 11, padding: '0 4px' }}
          >
            x
          </button>
        </div>
      ))}
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          value={newCase}
          onChange={(e) => setNewCase(e.target.value)}
          placeholder="Add case..."
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && newCase.trim()) {
              onChange([...cases, newCase.trim()]);
              setNewCase('');
            }
          }}
        />
        <button
          onClick={() => {
            if (newCase.trim()) {
              onChange([...cases, newCase.trim()]);
              setNewCase('');
            }
          }}
          style={{ background: '#313244', border: '1px solid #45475a', borderRadius: 4, color: '#cdd6f4', cursor: 'pointer', fontSize: 11, padding: '4px 8px' }}
        >
          +
        </button>
      </div>
    </div>
  );
}

function ConditionalOutputsEditor({ outputs, onChange }: { outputs: string[]; onChange: (o: string[]) => void }) {
  const [newOutput, setNewOutput] = useState('');
  return (
    <div style={{ marginBottom: 16 }}>
      <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 6, fontWeight: 600 }}>
        Output Labels
      </span>
      {outputs.map((o, i) => (
        <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4, alignItems: 'center' }}>
          <span style={{ color: '#cdd6f4', fontSize: 11, flex: 1 }}>{o}</span>
          <button
            onClick={() => onChange(outputs.filter((_, j) => j !== i))}
            style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 11, padding: '0 4px' }}
          >
            x
          </button>
        </div>
      ))}
      <div style={{ display: 'flex', gap: 4 }}>
        <input
          value={newOutput}
          onChange={(e) => setNewOutput(e.target.value)}
          placeholder="Add output..."
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && newOutput.trim()) {
              onChange([...outputs, newOutput.trim()]);
              setNewOutput('');
            }
          }}
        />
        <button
          onClick={() => {
            if (newOutput.trim()) {
              onChange([...outputs, newOutput.trim()]);
              setNewOutput('');
            }
          }}
          style={{ background: '#313244', border: '1px solid #45475a', borderRadius: 4, color: '#cdd6f4', cursor: 'pointer', fontSize: 11, padding: '4px 8px' }}
        >
          +
        </button>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '6px 8px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 4,
  color: '#cdd6f4',
  fontSize: 12,
  outline: 'none',
  boxSizing: 'border-box',
};
