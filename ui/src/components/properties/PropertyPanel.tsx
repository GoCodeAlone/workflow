import { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import useModuleSchemaStore from '../../store/moduleSchemaStore.ts';
import { CATEGORY_COLORS } from '../../types/workflow.ts';
import type { ConfigFieldDef, IOPort, WorkflowEdgeData } from '../../types/workflow.ts';
import ArrayFieldEditor from './ArrayFieldEditor.tsx';
import MapFieldEditor from './MapFieldEditor.tsx';
import MiddlewareChainEditor from './MiddlewareChainEditor.tsx';
import FilePicker from './FilePicker.tsx';
import SqlEditor from './SqlEditor.tsx';
import DelegateServicePicker from './DelegateServicePicker.tsx';
import RoutePipelineEditor from './RoutePipelineEditor.tsx';
import FieldPicker from './FieldPicker.tsx';

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

  // Compute preceding steps for pipeline step nodes (for FieldPicker)
  const precedingSteps = useMemo(() => {
    if (!node || !node.data.moduleType.startsWith('step.')) return [];
    const pipelineEdges = edges.filter(
      (e) => (e.data as Record<string, unknown> | undefined)?.edgeType === 'pipeline-flow',
    );
    if (pipelineEdges.length === 0) return [];

    // Build reverse adjacency: target -> source
    const prevStep = new Map<string, string>();
    for (const e of pipelineEdges) {
      prevStep.set(e.target, e.source);
    }

    // Walk backwards from current node
    const result: Array<{ name: string; type: string; config?: Record<string, unknown> }> = [];
    let currentId: string | undefined = prevStep.get(node.id);
    const visited = new Set<string>();
    while (currentId && !visited.has(currentId)) {
      visited.add(currentId);
      const n = nodes.find((nd) => nd.id === currentId);
      if (n && n.data.moduleType.startsWith('step.')) {
        result.unshift({
          name: n.data.label,
          type: n.data.moduleType,
          config: n.data.config as Record<string, unknown> | undefined,
        });
      }
      currentId = prevStep.get(currentId);
    }
    return result;
  }, [node, edges, nodes]);

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

  // Track refs to input elements for cursor-position insertion
  const fieldInputRefs = useRef<Record<string, HTMLInputElement | HTMLTextAreaElement | null>>({});

  const handleFieldChange = (key: string, value: unknown) => {
    // Mark field as overridden if it has inheritance
    if (inheritedValues[key]) {
      setOverriddenFields((prev) => new Set(prev).add(key));
    }
    if (node) {
      updateNodeConfig(node.id, { [key]: value });
    }
  };

  const insertAtCursor = useCallback((fieldKey: string, expr: string, currentValue: string) => {
    const el = fieldInputRefs.current[fieldKey];
    if (el) {
      const start = el.selectionStart ?? currentValue.length;
      const end = el.selectionEnd ?? start;
      const newVal = currentValue.slice(0, start) + expr + currentValue.slice(end);
      handleFieldChange(fieldKey, newVal);
      requestAnimationFrame(() => {
        el.focus();
        const pos = start + expr.length;
        el.selectionStart = el.selectionEnd = pos;
      });
    } else {
      handleFieldChange(fieldKey, currentValue + expr);
    }
  }, [handleFieldChange]); // eslint-disable-line react-hooks/exhaustive-deps

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
              const hasExplicitValue = node.data.config[field.key] !== undefined
                && node.data.config[field.key] !== ''
                && node.data.config[field.key] !== null;
              const useInherited = inherited && !isOverridden && !hasExplicitValue;

              // Resolve the effective value: explicit config > inherited > default
              const effectiveValue = hasExplicitValue
                ? node.data.config[field.key]
                : (useInherited ? inherited?.value : node.data.config[field.key] ?? field.defaultValue);

              // Style for inherited fields
              const inheritedInputStyle = useInherited
                ? { ...inputStyle, fontStyle: 'italic' as const, color: '#a6e3a1', opacity: 0.8 }
                : inputStyle;

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
                    value={String(effectiveValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    style={inheritedInputStyle}
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
                    value={String(effectiveValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, Number(e.target.value))}
                    placeholder={field.placeholder}
                    style={inheritedInputStyle}
                  />
                ) : field.type === 'boolean' ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <input
                      type="checkbox"
                      checked={Boolean(effectiveValue ?? false)}
                      onChange={(e) => handleFieldChange(field.key, e.target.checked)}
                    />
                    {field.description && (
                      <span style={{ color: '#585b70', fontSize: 10 }}>{field.description}</span>
                    )}
                  </div>
                ) : field.type === 'array' ? (
                  <ArrayFieldEditor
                    label={field.label}
                    value={(effectiveValue as unknown[]) ?? []}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    itemType={field.arrayItemType}
                    placeholder={field.placeholder}
                  />
                ) : field.type === 'map' ? (
                  <MapFieldEditor
                    label={field.label}
                    value={(effectiveValue as Record<string, unknown>) ?? {}}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    valueType={field.mapValueType}
                    placeholder={field.placeholder}
                  />
                ) : field.type === 'json' ? (
                  <textarea
                    value={
                      typeof effectiveValue === 'string'
                        ? effectiveValue
                        : JSON.stringify(effectiveValue ?? '', null, 2)
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
                    style={{ ...inheritedInputStyle, resize: 'vertical', fontFamily: 'monospace' }}
                  />
                ) : field.type === 'filepath' ? (
                  <FilePicker
                    value={String(effectiveValue ?? '')}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    placeholder={field.placeholder}
                    description={field.description}
                  />
                ) : field.type === 'sql' ? (
                  <div>
                    {precedingSteps.length > 0 && (
                      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 4 }}>
                        <FieldPicker
                          precedingSteps={precedingSteps}
                          onSelect={(expr) => insertAtCursor(field.key, expr, String(effectiveValue ?? ''))}
                        />
                      </div>
                    )}
                    <SqlEditor
                      value={String(effectiveValue ?? '')}
                      onChange={(val) => handleFieldChange(field.key, val)}
                      placeholder={field.placeholder}
                    />
                  </div>
                ) : field.inheritFrom === 'dependency.name' ? (
                  <DelegateServicePicker
                    value={String(effectiveValue ?? '')}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    placeholder={field.placeholder}
                    nodes={nodes}
                    currentNodeId={node.id}
                  />
                ) : field.sensitive ? (
                  <SensitiveFieldInput
                    value={String(effectiveValue ?? '')}
                    onChange={(val) => handleFieldChange(field.key, val)}
                    placeholder={field.placeholder}
                  />
                ) : precedingSteps.length > 0 && node.data.moduleType.startsWith('step.') ? (
                  <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
                    <input
                      ref={(el) => { fieldInputRefs.current[field.key] = el; }}
                      type="text"
                      value={String(effectiveValue ?? '')}
                      onChange={(e) => handleFieldChange(field.key, e.target.value)}
                      placeholder={field.placeholder}
                      style={{ ...inheritedInputStyle, flex: 1 }}
                    />
                    <FieldPicker
                      precedingSteps={precedingSteps}
                      onSelect={(expr) => insertAtCursor(field.key, expr, String(effectiveValue ?? ''))}
                    />
                  </div>
                ) : (
                  <input
                    type="text"
                    value={String(effectiveValue ?? '')}
                    onChange={(e) => handleFieldChange(field.key, e.target.value)}
                    placeholder={field.placeholder}
                    style={inheritedInputStyle}
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
        {(node.data.handlerRoutes || node.data.moduleType === 'api.query' || node.data.moduleType === 'api.command') && (
          <HandlerRoutesEditor
            routes={(node.data.handlerRoutes ?? []) as Array<{ method: string; path: string; middlewares?: string[]; pipeline?: { steps: Array<{ name: string; type: string; config?: Record<string, unknown> }> } }>}
            nodeId={node.id}
            color={color}
            delegate={String((node.data.config as Record<string, unknown>)?.delegate ?? '')}
          />
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

function HandlerRoutesEditor({
  routes,
  nodeId,
  color,
  delegate,
}: {
  routes: Array<{
    method: string;
    path: string;
    middlewares?: string[];
    pipeline?: { steps: Array<{ name: string; type: string; config?: Record<string, unknown> }> };
  }>;
  nodeId: string;
  color: string;
  delegate?: string;
}) {
  const [expanded, setExpanded] = useState(true);
  const [adding, setAdding] = useState(false);
  const [editIdx, setEditIdx] = useState<number | null>(null);
  const [newMethod, setNewMethod] = useState('GET');
  const [newPath, setNewPath] = useState('');
  const [newMiddlewares, setNewMiddlewares] = useState('');
  const updateHandlerRoutes = useWorkflowStore((s) => s.updateHandlerRoutes);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const nodes = useWorkflowStore((s) => s.nodes);
  const edges = useWorkflowStore((s) => s.edges);

  // Available middleware nodes on canvas
  const availableMiddleware = nodes
    .filter((n) => n.data.moduleType.startsWith('http.middleware.'))
    .map((n) => n.data.label);

  // Detect pipeline-flow chain from this handler node
  const pipelineChain = useMemo(() => {
    const chain: Array<{ id: string; label: string; type: string }> = [];
    const pipelineEdges = edges.filter(
      (e) => (e.data as Record<string, unknown> | undefined)?.edgeType === 'pipeline-flow',
    );
    if (pipelineEdges.length === 0) return chain;

    // Find the first step connected from this handler
    const firstEdge = pipelineEdges.find((e) => e.source === nodeId);
    if (!firstEdge) return chain;

    // Build adjacency for step-to-step
    const nextStep = new Map<string, string>();
    for (const e of pipelineEdges) {
      if (e.source !== nodeId) {
        nextStep.set(e.source, e.target);
      }
    }

    // Walk the chain
    let currentId: string | undefined = firstEdge.target;
    const visited = new Set<string>();
    while (currentId && !visited.has(currentId)) {
      visited.add(currentId);
      const node = nodes.find((n) => n.id === currentId);
      if (node && node.data.moduleType.startsWith('step.')) {
        chain.push({ id: node.id, label: node.data.label, type: node.data.moduleType });
      }
      currentId = nextStep.get(currentId);
    }

    return chain;
  }, [nodeId, edges, nodes]);

  const handleDelete = (idx: number) => {
    if (!window.confirm('Delete this route?')) return;
    updateHandlerRoutes(nodeId, routes.filter((_, i) => i !== idx));
  };

  const handleAdd = () => {
    if (!newPath.trim()) return;
    const mws = newMiddlewares.trim()
      ? newMiddlewares.split(',').map((s) => s.trim()).filter(Boolean)
      : undefined;
    const route: { method: string; path: string; middlewares?: string[] } = {
      method: newMethod,
      path: newPath.trim(),
    };
    if (mws && mws.length > 0) route.middlewares = mws;
    updateHandlerRoutes(nodeId, [...routes, route]);
    setNewMethod('GET');
    setNewPath('');
    setNewMiddlewares('');
    setAdding(false);
  };

  const handleEditSave = () => {
    if (editIdx === null || !newPath.trim()) return;
    const mws = newMiddlewares.trim()
      ? newMiddlewares.split(',').map((s) => s.trim()).filter(Boolean)
      : undefined;
    const updated = routes.map((r, i) => {
      if (i !== editIdx) return r;
      const route: typeof routes[number] = {
        method: newMethod,
        path: newPath.trim(),
      };
      if (mws && mws.length > 0) route.middlewares = mws;
      // Preserve existing pipeline steps when editing route method/path/middleware
      if (r.pipeline && r.pipeline.steps.length > 0) route.pipeline = r.pipeline;
      return route;
    });
    updateHandlerRoutes(nodeId, updated);
    setEditIdx(null);
    setNewMethod('GET');
    setNewPath('');
    setNewMiddlewares('');
  };

  const startEdit = (idx: number) => {
    const r = routes[idx];
    setEditIdx(idx);
    setNewMethod(r.method);
    setNewPath(r.path);
    setNewMiddlewares(r.middlewares?.join(', ') ?? '');
    setAdding(false);
  };

  const cancelEdit = () => {
    setEditIdx(null);
    setAdding(false);
    setNewMethod('GET');
    setNewPath('');
    setNewMiddlewares('');
  };

  const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'OPTIONS', 'HEAD'];

  return (
    <div style={{ marginBottom: 16 }}>
      <div
        style={{
          color: '#a6adc8',
          fontSize: 11,
          display: 'flex',
          alignItems: 'center',
          marginBottom: 8,
          fontWeight: 600,
          gap: 4,
        }}
      >
        <span
          onClick={() => setExpanded((v) => !v)}
          style={{ fontSize: 9, cursor: 'pointer', transition: 'transform 0.15s', transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}
        >
          &#9654;
        </span>
        <span onClick={() => setExpanded((v) => !v)} style={{ cursor: 'pointer', userSelect: 'none' }}>Routes</span>
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
        <button
          onClick={() => { setAdding(true); setEditIdx(null); setNewMethod('GET'); setNewPath(''); setNewMiddlewares(''); }}
          style={{
            marginLeft: 'auto',
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#a6e3a1',
            cursor: 'pointer',
            fontSize: 10,
            padding: '1px 6px',
            fontWeight: 600,
          }}
          title="Add route"
        >
          +
        </button>
      </div>
      {expanded && (
        <div
          style={{
            background: '#11111b',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: '6px 0',
            maxHeight: 400,
            overflowY: 'auto',
          }}
        >
          {routes.map((route, i) => {
            const methodColor = HTTP_METHOD_COLORS[route.method] ?? '#cdd6f4';
            if (editIdx === i) {
              return (
                <div key={`edit-${i}`} style={{ padding: '6px 10px', borderBottom: '1px solid #1e1e2e' }}>
                  <div style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                    <select
                      value={newMethod}
                      onChange={(e) => setNewMethod(e.target.value)}
                      style={{ ...routeInputStyle, width: 70, flexShrink: 0 }}
                    >
                      {methods.map((m) => <option key={m} value={m}>{m}</option>)}
                    </select>
                    <input
                      value={newPath}
                      onChange={(e) => setNewPath(e.target.value)}
                      placeholder="/api/..."
                      style={{ ...routeInputStyle, flex: 1 }}
                    />
                  </div>
                  <input
                    value={newMiddlewares}
                    onChange={(e) => setNewMiddlewares(e.target.value)}
                    placeholder={`Middleware (comma-sep)${availableMiddleware.length > 0 ? ': ' + availableMiddleware.join(', ') : ''}`}
                    style={{ ...routeInputStyle, width: '100%', marginBottom: 4, fontSize: 10 }}
                  />
                  <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
                    <button onClick={cancelEdit} style={routeCancelBtnStyle}>Cancel</button>
                    <button onClick={handleEditSave} style={routeSaveBtnStyle}>Save</button>
                  </div>
                </div>
              );
            }
            return (
              <div
                key={`${route.method}-${route.path}-${i}`}
                style={{ borderBottom: i < routes.length - 1 ? '1px solid #1e1e2e' : undefined }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'baseline',
                    gap: 8,
                    padding: '3px 10px',
                    fontSize: 11,
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
                      flexShrink: 0,
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
                      flex: 1,
                    }}
                  >
                    {route.path}
                  </span>
                  {route.middlewares && route.middlewares.length > 0 && (
                    <span
                      style={{ color: '#585b70', fontSize: 9, flexShrink: 0 }}
                      title={route.middlewares.join(', ')}
                    >
                      +{route.middlewares.length} mw
                    </span>
                  )}
                  {route.pipeline && route.pipeline.steps.length > 0 && (
                    <span
                      style={{ color: '#e879f9', fontSize: 9, flexShrink: 0 }}
                      title={route.pipeline.steps.map((s) => s.name).join(' \u2192 ')}
                    >
                      {route.pipeline.steps.length} step{route.pipeline.steps.length !== 1 ? 's' : ''}
                    </span>
                  )}
                  <button
                    onClick={() => startEdit(i)}
                    style={{ background: 'none', border: 'none', color: '#585b70', cursor: 'pointer', fontSize: 10, padding: '0 2px', flexShrink: 0 }}
                    title="Edit route"
                  >
                    &#9998;
                  </button>
                  <button
                    onClick={() => handleDelete(i)}
                    style={{ background: 'none', border: 'none', color: '#f38ba8', cursor: 'pointer', fontSize: 10, padding: '0 2px', flexShrink: 0 }}
                    title="Delete route"
                  >
                    x
                  </button>
                </div>
                <div style={{ padding: '0 10px 4px' }}>
                  {pipelineChain.length > 0 ? (
                    <div style={{ marginTop: 4 }}>
                      <div style={{ fontSize: 10, color: '#585b70', marginBottom: 2 }}>
                        Pipeline (canvas)
                      </div>
                      <div style={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        alignItems: 'center',
                        gap: 2,
                        fontSize: 10,
                      }}>
                        {pipelineChain.map((step, si) => (
                          <span key={step.id} style={{ display: 'inline-flex', alignItems: 'center', gap: 2 }}>
                            {si > 0 && <span style={{ color: '#585b70' }}>{'\u2192'}</span>}
                            <span
                              onClick={() => setSelectedNode(step.id)}
                              style={{
                                color: '#e879f9',
                                cursor: 'pointer',
                                padding: '1px 4px',
                                borderRadius: 3,
                                background: '#e879f910',
                              }}
                              title={`Click to select ${step.type} node`}
                            >
                              {step.label}
                            </span>
                          </span>
                        ))}
                      </div>
                    </div>
                  ) : (route.pipeline?.steps?.length ?? 0) > 0 ? (
                    <RoutePipelineEditor
                      steps={route.pipeline?.steps ?? []}
                      onChange={(steps) => {
                        const updated = routes.map((r, idx) =>
                          idx === i ? { ...r, pipeline: steps.length > 0 ? { steps } : undefined } : r
                        );
                        updateHandlerRoutes(nodeId, updated);
                      }}
                    />
                  ) : delegate ? (
                    <div style={{ fontSize: 10, color: '#585b70', padding: '2px 0', display: 'flex', alignItems: 'center', gap: 4 }}>
                      <span style={{ color: '#f9e2af', fontSize: 9, padding: '1px 4px', borderRadius: 3, background: '#f9e2af15' }}>
                        delegate
                      </span>
                      <span style={{ color: '#a6adc8' }}>{delegate}</span>
                    </div>
                  ) : (
                    <RoutePipelineEditor
                      steps={[]}
                      onChange={(steps) => {
                        const updated = routes.map((r, idx) =>
                          idx === i ? { ...r, pipeline: steps.length > 0 ? { steps } : undefined } : r
                        );
                        updateHandlerRoutes(nodeId, updated);
                      }}
                    />
                  )}
                </div>
              </div>
            );
          })}

          {adding && (
            <div style={{ padding: '6px 10px', borderTop: routes.length > 0 ? '1px solid #313244' : undefined }}>
              <div style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                <select
                  value={newMethod}
                  onChange={(e) => setNewMethod(e.target.value)}
                  style={{ ...routeInputStyle, width: 70, flexShrink: 0 }}
                >
                  {methods.map((m) => <option key={m} value={m}>{m}</option>)}
                </select>
                <input
                  value={newPath}
                  onChange={(e) => setNewPath(e.target.value)}
                  placeholder="/api/..."
                  style={{ ...routeInputStyle, flex: 1 }}
                  onKeyDown={(e) => { if (e.key === 'Enter') handleAdd(); if (e.key === 'Escape') cancelEdit(); }}
                  autoFocus
                />
              </div>
              <input
                value={newMiddlewares}
                onChange={(e) => setNewMiddlewares(e.target.value)}
                placeholder={`Middleware (comma-sep)${availableMiddleware.length > 0 ? ': ' + availableMiddleware.join(', ') : ''}`}
                style={{ ...routeInputStyle, width: '100%', marginBottom: 4, fontSize: 10 }}
                onKeyDown={(e) => { if (e.key === 'Enter') handleAdd(); if (e.key === 'Escape') cancelEdit(); }}
              />
              <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
                <button onClick={cancelEdit} style={routeCancelBtnStyle}>Cancel</button>
                <button onClick={handleAdd} style={routeSaveBtnStyle}>Add</button>
              </div>
            </div>
          )}

          {routes.length === 0 && !adding && (
            <div style={{ padding: '8px 10px', color: '#585b70', fontSize: 11, textAlign: 'center' }}>
              No routes. Click + to add one.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

const routeInputStyle: React.CSSProperties = {
  padding: '4px 6px',
  background: '#1e1e2e',
  border: '1px solid #313244',
  borderRadius: 4,
  color: '#cdd6f4',
  fontSize: 11,
  outline: 'none',
  boxSizing: 'border-box',
  fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, monospace',
};

const routeCancelBtnStyle: React.CSSProperties = {
  background: 'none',
  border: '1px solid #45475a',
  borderRadius: 4,
  color: '#585b70',
  cursor: 'pointer',
  fontSize: 10,
  padding: '2px 8px',
};

const routeSaveBtnStyle: React.CSSProperties = {
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 4,
  color: '#a6e3a1',
  cursor: 'pointer',
  fontSize: 10,
  padding: '2px 8px',
  fontWeight: 600,
};

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
