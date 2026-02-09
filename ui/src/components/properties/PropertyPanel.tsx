import useWorkflowStore from '../../store/workflowStore.ts';
import { MODULE_TYPE_MAP, CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ConfigFieldDef } from '../../types/workflow.ts';

export default function PropertyPanel() {
  const nodes = useWorkflowStore((s) => s.nodes);
  const selectedNodeId = useWorkflowStore((s) => s.selectedNodeId);
  const updateNodeConfig = useWorkflowStore((s) => s.updateNodeConfig);
  const updateNodeName = useWorkflowStore((s) => s.updateNodeName);
  const removeNode = useWorkflowStore((s) => s.removeNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);

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

  const info = MODULE_TYPE_MAP[node.data.moduleType];
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
                    style={inputStyle}
                  />
                ) : field.type === 'boolean' ? (
                  <input
                    type="checkbox"
                    checked={Boolean(node.data.config[field.key] ?? field.defaultValue ?? false)}
                    onChange={(e) => handleFieldChange(field.key, e.target.checked)}
                  />
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
                    style={{ ...inputStyle, resize: 'vertical', fontFamily: 'monospace' }}
                  />
                ) : (
                  <input
                    type="text"
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    style={inputStyle}
                  />
                )}
              </label>
            ))}
          </div>
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
