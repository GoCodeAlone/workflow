import { useState, useEffect, useMemo } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import { CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ConfigFieldDef, IOPort, WorkflowEdgeData } from '../../types/workflow.ts';
import ArrayFieldEditor from './ArrayFieldEditor.tsx';
import MapFieldEditor from './MapFieldEditor.tsx';
import MiddlewareChainEditor from './MiddlewareChainEditor.tsx';
import FilePicker from './FilePicker.tsx';

// Resolve inherited value for a field based on incoming edges.
// inheritFrom pattern: "{edgeType}.{sourceField}" where sourceField is "name" (source node label)
// or a config key on the source node.
function resolveInheritedValue(
  field: ConfigFieldDef,
  nodeId: string,
  edges: { source: string; target: string; data?: unknown }[],
  nodes: { id: string; data: { label: string; config: Record<string, unknown> } }[],
): { value: unknown; sourceName: string } | null {
  if (!field.inheritFrom) return null;
  const dotIdx = field.inheritFrom.indexOf('.');
  if (dotIdx < 0) return null;
  const edgeType = field.inheritFrom.slice(0, dotIdx);
  const sourceField = field.inheritFrom.slice(dotIdx + 1);
  if (!edgeType || !sourceField) return null;

  for (const edge of edges) {
    if (edge.target !== nodeId) continue;
    const edgeData = edge.data as WorkflowEdgeData | undefined;
    const type = edgeData?.edgeType ?? 'dependency';
    if (type !== edgeType) continue;

    const sourceNode = nodes.find((n) => n.id === edge.source);
    if (!sourceNode) continue;

    if (sourceField === 'name') {
      return { value: sourceNode.data.label, sourceName: sourceNode.data.label };
    }
    const val = sourceNode.data.config[sourceField];
    if (val !== undefined) {
      return { value: val, sourceName: sourceNode.data.label };
    }
  }
  return null;
}

export default function PropertyPanel() {
  const nodes = useWorkflowStore((s) => s.nodes);
  const edges = useWorkflowStore((s) => s.edges);
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

  const info = node ? moduleTypeMap[node.data.moduleType] : undefined;
  const fields: ConfigFieldDef[] = useMemo(() => info?.configFields ?? [], [info]);

  // Compute inherited values for fields with inheritFrom
  const inheritedValues = useMemo(() => {
    const result: Record<string, { value: unknown; sourceName: string }> = {};
    if (!node) return result;
    for (const field of fields) {
      if (!field.inheritFrom) continue;
      const resolved = resolveInheritedValue(field, node.id, edges, nodes);
      if (resolved) {
        result[field.key] = resolved;
      }
    }
    return result;
  }, [node, fields, edges, nodes]);

  // Track which inherited fields have been overridden by the user
  const [overriddenFields, setOverriddenFields] = useState<Set<string>>(new Set());

  const handleFieldChange = (key: string, value: unknown) => {
    // Mark field as overridden if it has inheritance
    if (inheritedValues[key]) {
      setOverriddenFields((prev) => new Set(prev).add(key));
    }
    if (node) {
      updateNodeConfig(node.id, { [key]: value });
    }
  };

  if (!node) {
    return (
      <div
        style={{
          width: '100%',
          background: '#181825',
          padding: 16,
          color: '#585b70',
          fontSize: 13,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          boxSizing: 'border-box',
        }}
      >
        Select a node to edit its properties
      </div>
    );
  }

  const color = info ? CATEGORY_COLORS[info.category] : '#64748b';

  return (
    <div
      style={{
        width: '100%',
        background: '#181825',
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
            {fields.map((field) => {
              const inherited = inheritedValues[field.key];
              const isOverridden = overriddenFields.has(field.key);
              const useInherited = inherited && !isOverridden && !node.data.config[field.key];

              // Auto-fill inherited value if not overridden and field is empty
              if (useInherited && inherited) {
                const currentVal = node.data.config[field.key];
                if (currentVal === undefined || currentVal === '' || currentVal === null) {
                  // Will be auto-filled on display, actual config update happens on first render
                  // We show the inherited value but don't force-write to config to avoid loops
                }
              }

              return (
              <label key={field.key} style={{ display: 'block', marginBottom: 10 }}>
                <span style={{ color: '#a6adc8', fontSize: 11, display: 'flex', alignItems: 'center', marginBottom: 3, gap: 4 }}>
                  <span>
                    {field.label}
                    {field.required && <span style={{ color: '#f38ba8', marginLeft: 2 }}>*</span>}
                  </span>
                  {inherited && !isOverridden && (
                    <span
                      style={{ color: '#a6e3a1', fontSize: 9, cursor: 'pointer' }}
                      title={`Click to override inherited value from ${inherited.sourceName}`}
                      onClick={() => setOverriddenFields((prev) => new Set(prev).add(field.key))}
                    >
                      inherited from {inherited.sourceName}
                    </span>
                  )}
                  {inherited && isOverridden && (
                    <span
                      style={{ color: '#fab387', fontSize: 9, cursor: 'pointer' }}
                      title="Click to restore inherited value"
                      onClick={() => {
                        setOverriddenFields((prev) => {
                          const next = new Set(prev);
                          next.delete(field.key);
                          return next;
                        });
                        updateNodeConfig(node.id, { [field.key]: undefined });
                      }}
                    >
                      overridden
                    </span>
                  )}
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
                ) : field.type === 'array' ? (
                  <ArrayFieldEditor
                    label={field.label}
                    value={(node.data.config[field.key] as unknown[]) ?? (field.defaultValue as unknown[]) ?? []}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    itemType={field.arrayItemType}
                    placeholder={field.placeholder}
                  />
                ) : field.type === 'map' ? (
                  <MapFieldEditor
                    label={field.label}
                    value={(node.data.config[field.key] as Record<string, unknown>) ?? (field.defaultValue as Record<string, unknown>) ?? {}}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    valueType={field.mapValueType}
                    placeholder={field.placeholder}
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
                    placeholder={field.placeholder}
                    style={{ ...inputStyle, resize: 'vertical', fontFamily: 'monospace' }}
                  />
                ) : field.type === 'filepath' ? (
                  <FilePicker
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    placeholder={field.placeholder}
                    description={field.description}
                  />
                ) : field.sensitive ? (
                  <SensitiveFieldInput
                    value={String(node.data.config[field.key] ?? field.defaultValue ?? '')}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    placeholder={field.placeholder}
                  />
                ) : (
                  <input
                    type="text"
                    value={String(node.data.config[field.key] ?? (useInherited ? inherited?.value : field.defaultValue) ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    placeholder={field.placeholder}
                    style={useInherited ? { ...inputStyle, fontStyle: 'italic', color: '#a6e3a1', opacity: 0.8 } : inputStyle}
                  />
                )}
                {field.description && field.type !== 'boolean' && (
                  <span style={{ color: '#585b70', fontSize: 10, display: 'block', marginTop: 2 }}>{field.description}</span>
                )}
              </label>
              );
            })}
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

        {/* Handler Routes */}
        {node.data.handlerRoutes && node.data.handlerRoutes.length > 0 && (
          <HandlerRoutesSection routes={node.data.handlerRoutes as Array<{ method: string; path: string; middlewares?: string[] }>} color={color} />
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

        {/* Middleware chain editor for router nodes */}
        {(node.data.moduleType === 'http.router' || node.data.moduleType === 'chimux.router') && (
          <MiddlewareChainEditor
            nodeId={node.id}
            middlewareChain={(node.data.config?.middlewareChain as string[]) ?? []}
            onChange={(chain) => updateNodeConfig(node.id, { middlewareChain: chain })}
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

function SensitiveFieldInput({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (val: string) => void;
  placeholder?: string;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <div style={{ position: 'relative' }}>
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        style={{ ...inputStyle, paddingRight: 30 }}
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        style={{
          position: 'absolute',
          right: 4,
          top: '50%',
          transform: 'translateY(-50%)',
          background: 'none',
          border: 'none',
          color: '#585b70',
          cursor: 'pointer',
          fontSize: 11,
          padding: '2px 4px',
        }}
        title={visible ? 'Hide value' : 'Show value'}
      >
        {visible ? 'hide' : 'show'}
      </button>
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

const HTTP_METHOD_COLORS: Record<string, string> = {
  GET: '#a6e3a1',     // green
  POST: '#89b4fa',    // blue
  PUT: '#fab387',     // peach/orange
  DELETE: '#f38ba8',  // red
  PATCH: '#cba6f7',   // mauve
  OPTIONS: '#585b70', // overlay
  HEAD: '#585b70',    // overlay
};

function HandlerRoutesSection({
  routes,
  color,
}: {
  routes: Array<{ method: string; path: string; middlewares?: string[] }>;
  color: string;
}) {
  const [expanded, setExpanded] = useState(true);

  return (
    <div style={{ marginBottom: 16 }}>
      <div
        onClick={() => setExpanded((v) => !v)}
        style={{
          color: '#a6adc8',
          fontSize: 11,
          display: 'flex',
          alignItems: 'center',
          marginBottom: 8,
          fontWeight: 600,
          cursor: 'pointer',
          userSelect: 'none',
          gap: 4,
        }}
      >
        <span style={{ fontSize: 9, transition: 'transform 0.15s', transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>
          &#9654;
        </span>
        <span>Routes</span>
        <span
          style={{
            background: `${color}30`,
            color,
            padding: '1px 6px',
            borderRadius: 8,
            fontSize: 10,
            fontWeight: 500,
            marginLeft: 4,
          }}
        >
          {routes.length}
        </span>
      </div>
      {expanded && (
        <div
          style={{
            background: '#11111b',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: '6px 0',
            maxHeight: 300,
            overflowY: 'auto',
          }}
        >
          {routes.map((route, i) => {
            const methodColor = HTTP_METHOD_COLORS[route.method] ?? '#cdd6f4';
            return (
              <div
                key={`${route.method}-${route.path}-${i}`}
                style={{
                  display: 'flex',
                  alignItems: 'baseline',
                  gap: 8,
                  padding: '3px 10px',
                  fontSize: 11,
                  borderBottom: i < routes.length - 1 ? '1px solid #1e1e2e' : undefined,
                }}
              >
                <span
                  style={{
                    color: methodColor,
                    fontWeight: 700,
                    fontSize: 10,
                    fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, monospace',
                    minWidth: 48,
                    textAlign: 'right',
                  }}
                >
                  {route.method}
                </span>
                <span
                  style={{
                    color: '#cdd6f4',
                    fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, monospace',
                    fontSize: 11,
                    wordBreak: 'break-all',
                  }}
                >
                  {route.path}
                </span>
                {route.middlewares && route.middlewares.length > 0 && (
                  <span
                    style={{
                      color: '#585b70',
                      fontSize: 9,
                      marginLeft: 'auto',
                      flexShrink: 0,
                    }}
                    title={route.middlewares.join(', ')}
                  >
                    +{route.middlewares.length} mw
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}
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
