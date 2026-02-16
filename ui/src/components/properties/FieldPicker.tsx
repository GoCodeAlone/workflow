import { useState, useEffect, useRef } from 'react';

interface PrecedingStep {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

interface FieldPickerProps {
  /** Steps preceding the current one in the pipeline */
  precedingSteps: PrecedingStep[];
  /** Callback when user selects a field — inserts template expression */
  onSelect: (expression: string) => void;
}

interface OutputField {
  name: string;
  type: 'string' | 'number' | 'boolean' | 'object' | 'array' | 'map' | 'any';
  description?: string;
  children?: OutputField[];
}

const STEP_OUTPUT_SCHEMAS: Record<string, OutputField[]> = {
  'step.request_parse': [
    { name: 'path_params', type: 'map', description: 'URL path parameters' },
    { name: 'query_params', type: 'map', description: 'URL query parameters' },
    { name: 'body', type: 'object', description: 'Request body (parsed JSON)' },
    { name: 'headers', type: 'map', description: 'Request headers' },
    { name: 'method', type: 'string', description: 'HTTP method' },
  ],
  'step.set': [],
  'step.db_query': [
    { name: 'row', type: 'object', description: 'Single result (mode: single)' },
    { name: 'rows', type: 'array', description: 'All results (mode: list)' },
    { name: 'found', type: 'boolean', description: 'Whether any rows matched' },
  ],
  'step.db_exec': [
    { name: 'rows_affected', type: 'number', description: 'Number of rows affected' },
    { name: 'last_insert_id', type: 'number', description: 'Auto-increment ID of inserted row' },
  ],
  'step.json_response': [],
  'step.conditional': [
    { name: 'matched_route', type: 'string', description: 'Which route was matched' },
  ],
  'step.http_call': [
    { name: 'status', type: 'number', description: 'HTTP status code' },
    { name: 'body', type: 'object', description: 'Response body' },
    { name: 'headers', type: 'map', description: 'Response headers' },
  ],
  'step.validate': [
    { name: 'valid', type: 'boolean', description: 'Whether validation passed' },
  ],
  'step.log': [],
  'step.publish': [],
  'step.delegate': [
    { name: 'result', type: 'any', description: 'Delegate response' },
  ],
  'step.transform': [
    { name: 'result', type: 'any', description: 'Transform output' },
  ],
};

const TYPE_COLORS: Record<string, string> = {
  string: '#a6e3a1',
  number: '#fab387',
  boolean: '#89b4fa',
  object: '#cba6f7',
  array: '#f9e2af',
  map: '#f38ba8',
  any: '#585b70',
};

function normalizeType(type: string): string {
  if (type.startsWith('step.')) return type;
  return 'step.' + type;
}

function getFieldsForStep(step: PrecedingStep): OutputField[] {
  const normalized = normalizeType(step.type);

  // For step.set, dynamically derive from config.values
  if ((normalized === 'step.set') && step.config?.values) {
    const vals = step.config.values as Record<string, unknown>;
    return Object.keys(vals).map((k) => ({
      name: k,
      type: 'any' as const,
      description: `Set value: ${String(vals[k]).slice(0, 30)}`,
    }));
  }

  return STEP_OUTPUT_SCHEMAS[normalized] ?? [];
}

function needsIndexSyntax(name: string): boolean {
  return !/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(name);
}

function generateExpression(stepName: string, field: OutputField): string {
  if (field.type === 'map') {
    return `{{index .steps "${stepName}" "${field.name}" "?"}}`;
  }
  if (needsIndexSyntax(stepName) || needsIndexSyntax(field.name)) {
    return `{{index .steps "${stepName}" "${field.name}"}}`;
  }
  return `{{ .steps.${stepName}.${field.name} }}`;
}

export default function FieldPicker({ precedingSteps, onSelect }: FieldPickerProps) {
  const [open, setOpen] = useState(false);
  const [expandedSteps, setExpandedSteps] = useState<Set<number>>(new Set());
  const popoverRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  // Close on click outside or Escape
  useEffect(() => {
    if (!open) return;

    const handleClick = (e: MouseEvent) => {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(e.target as Node) &&
        buttonRef.current &&
        !buttonRef.current.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };

    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };

    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKey);
    };
  }, [open]);

  const toggleStep = (idx: number) => {
    setExpandedSteps((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  };

  const handleFieldClick = (stepName: string, field: OutputField) => {
    onSelect(generateExpression(stepName, field));
    setOpen(false);
  };

  const handleBuiltinClick = (expression: string) => {
    onSelect(expression);
    setOpen(false);
  };

  // No preceding steps and no built-ins worth showing? Still show built-ins.
  const hasContent = precedingSteps.length > 0 || true; // always show built-ins

  if (!hasContent) return null;

  return (
    <div style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        ref={buttonRef}
        onClick={() => setOpen((v) => !v)}
        style={{
          background: open ? '#313244' : 'transparent',
          border: '1px solid #45475a',
          borderRadius: 4,
          color: open ? '#cba6f7' : '#585b70',
          cursor: 'pointer',
          fontSize: 11,
          padding: '2px 6px',
          fontFamily: 'ui-monospace, "Cascadia Code", "Source Code Pro", Menlo, monospace',
          lineHeight: '16px',
          transition: 'color 0.15s, background 0.15s',
        }}
        title="Insert field reference"
      >
        {'{ }'}
      </button>

      {open && (
        <div
          ref={popoverRef}
          style={{
            position: 'absolute',
            top: '100%',
            right: 0,
            marginTop: 4,
            width: 280,
            maxHeight: 300,
            overflowY: 'auto',
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 6,
            zIndex: 1000,
            boxShadow: '0 4px 12px rgba(0,0,0,0.4)',
          }}
        >
          {/* Header */}
          <div
            style={{
              padding: '6px 10px',
              borderBottom: '1px solid #313244',
              color: '#a6adc8',
              fontSize: 10,
              fontWeight: 600,
            }}
          >
            Insert Field Reference
          </div>

          {/* Preceding steps */}
          {precedingSteps.map((step, i) => {
            const fields = getFieldsForStep(step);
            const isExpanded = expandedSteps.has(i);
            if (fields.length === 0) return null;

            return (
              <div key={`${step.name}-${i}`}>
                <div
                  onClick={() => toggleStep(i)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '5px 10px',
                    cursor: 'pointer',
                    borderBottom: '1px solid #181825',
                    background: isExpanded ? '#181825' : 'transparent',
                  }}
                  onMouseEnter={(e) => { e.currentTarget.style.background = '#181825'; }}
                  onMouseLeave={(e) => { if (!isExpanded) e.currentTarget.style.background = 'transparent'; }}
                >
                  <span
                    style={{
                      fontSize: 7,
                      color: '#585b70',
                      transition: 'transform 0.15s',
                      transform: isExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                    }}
                  >
                    &#9654;
                  </span>
                  <span
                    style={{
                      background: '#cba6f720',
                      color: '#cba6f7',
                      fontSize: 9,
                      fontWeight: 700,
                      padding: '1px 5px',
                      borderRadius: 4,
                      flexShrink: 0,
                    }}
                  >
                    {i + 1}
                  </span>
                  <span
                    style={{
                      color: '#cdd6f4',
                      fontSize: 11,
                      fontWeight: 500,
                      flex: 1,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {step.name}
                  </span>
                  <span style={{ color: '#585b70', fontSize: 9 }}>
                    {normalizeType(step.type).replace('step.', '')}
                  </span>
                </div>

                {isExpanded && (
                  <div style={{ padding: '2px 0' }}>
                    {fields.map((field) => (
                      <div
                        key={field.name}
                        onClick={() => handleFieldClick(step.name, field)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 6,
                          padding: '4px 10px 4px 32px',
                          cursor: 'pointer',
                          fontSize: 11,
                        }}
                        onMouseEnter={(e) => { e.currentTarget.style.background = '#313244'; }}
                        onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
                      >
                        <span style={{ color: '#cdd6f4', fontWeight: 600, fontFamily: 'monospace', fontSize: 10 }}>
                          {field.name}
                        </span>
                        <span
                          style={{
                            background: (TYPE_COLORS[field.type] ?? '#585b70') + '20',
                            color: TYPE_COLORS[field.type] ?? '#585b70',
                            fontSize: 8,
                            padding: '0 4px',
                            borderRadius: 3,
                            fontWeight: 600,
                            flexShrink: 0,
                          }}
                        >
                          {field.type}
                        </span>
                        {field.description && (
                          <span
                            style={{
                              color: '#585b70',
                              fontSize: 9,
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                              flex: 1,
                            }}
                          >
                            {field.description}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}

          {/* Built-in scope */}
          <div
            style={{
              borderTop: precedingSteps.length > 0 ? '1px solid #313244' : undefined,
            }}
          >
            <div
              style={{
                padding: '5px 10px',
                color: '#585b70',
                fontSize: 9,
                fontWeight: 600,
                textTransform: 'uppercase',
                letterSpacing: 0.5,
              }}
            >
              Built-in
            </div>
            <div
              onClick={() => handleBuiltinClick('{{ uuidv4 }}')}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                padding: '4px 10px 4px 20px',
                cursor: 'pointer',
                fontSize: 11,
              }}
              onMouseEnter={(e) => { e.currentTarget.style.background = '#313244'; }}
              onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
            >
              <span style={{ color: '#cdd6f4', fontWeight: 600, fontFamily: 'monospace', fontSize: 10 }}>
                uuidv4
              </span>
              <span
                style={{
                  background: '#a6e3a120',
                  color: '#a6e3a1',
                  fontSize: 8,
                  padding: '0 4px',
                  borderRadius: 3,
                  fontWeight: 600,
                }}
              >
                string
              </span>
              <span style={{ color: '#585b70', fontSize: 9 }}>Generate UUID</span>
            </div>
            <div
              onClick={() => handleBuiltinClick('{{ now }}')}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                padding: '4px 10px 4px 20px',
                cursor: 'pointer',
                fontSize: 11,
                marginBottom: 4,
              }}
              onMouseEnter={(e) => { e.currentTarget.style.background = '#313244'; }}
              onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
            >
              <span style={{ color: '#cdd6f4', fontWeight: 600, fontFamily: 'monospace', fontSize: 10 }}>
                now
              </span>
              <span
                style={{
                  background: '#a6e3a120',
                  color: '#a6e3a1',
                  fontSize: 8,
                  padding: '0 4px',
                  borderRadius: 3,
                  fontWeight: 600,
                }}
              >
                string
              </span>
              <span style={{ color: '#585b70', fontSize: 9 }}>Current timestamp</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
