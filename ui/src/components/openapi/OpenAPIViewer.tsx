import { useState, useEffect, useCallback } from 'react';

interface OpenAPISpec {
  openapi: string;
  info: {
    title: string;
    version: string;
    description?: string;
  };
  servers?: Array<{ url: string; description?: string }>;
  paths: Record<string, Record<string, PathOperation>>;
}

interface PathOperation {
  summary?: string;
  operationId?: string;
  tags?: string[];
  parameters?: Array<{
    name: string;
    in: string;
    required?: boolean;
    schema?: { type: string };
  }>;
  requestBody?: {
    required?: boolean;
    content?: Record<string, unknown>;
  };
  responses?: Record<string, { description: string }>;
}

const METHOD_COLORS: Record<string, string> = {
  get: '#22c55e',
  post: '#3b82f6',
  put: '#f59e0b',
  delete: '#ef4444',
  patch: '#8b5cf6',
  options: '#6b7280',
};

export default function OpenAPIViewer() {
  const [spec, setSpec] = useState<OpenAPISpec | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());
  const [filterTag, setFilterTag] = useState<string>('');

  const fetchSpec = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/openapi.json');
      if (!res.ok) {
        throw new Error(`Failed to fetch spec: ${res.status} ${res.statusText}`);
      }
      const data = await res.json();
      setSpec(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load OpenAPI spec');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSpec();
  }, [fetchSpec]);

  const togglePath = (key: string) => {
    setExpandedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  const expandAll = () => {
    if (!spec) return;
    const all = new Set<string>();
    for (const [path, methods] of Object.entries(spec.paths)) {
      for (const method of Object.keys(methods)) {
        all.add(`${method}:${path}`);
      }
    }
    setExpandedPaths(all);
  };

  const collapseAll = () => setExpandedPaths(new Set());

  // Collect all tags
  const allTags = new Set<string>();
  if (spec) {
    for (const methods of Object.values(spec.paths)) {
      for (const op of Object.values(methods) as PathOperation[]) {
        if (op.tags) {
          op.tags.forEach((t) => allTags.add(t));
        }
      }
    }
  }

  if (loading) {
    return (
      <div style={{ padding: 24, color: '#94a3b8' }}>
        Loading OpenAPI specification...
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 24 }}>
        <div style={{ color: '#ef4444', marginBottom: 12 }}>{error}</div>
        <button
          onClick={fetchSpec}
          style={{
            padding: '6px 16px',
            background: '#334155',
            color: '#e2e8f0',
            border: '1px solid #475569',
            borderRadius: 4,
            cursor: 'pointer',
          }}
        >
          Retry
        </button>
      </div>
    );
  }

  if (!spec) return null;

  const sortedPaths = Object.entries(spec.paths).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div style={{ padding: 16, color: '#e2e8f0', maxHeight: '100%', overflow: 'auto' }}>
      {/* Header */}
      <div style={{ marginBottom: 16 }}>
        <h2 style={{ margin: 0, fontSize: 18, fontWeight: 600 }}>
          {spec.info.title}
          <span style={{ color: '#94a3b8', fontWeight: 400, marginLeft: 8, fontSize: 14 }}>
            v{spec.info.version}
          </span>
        </h2>
        {spec.info.description && (
          <p style={{ margin: '4px 0 0', color: '#94a3b8', fontSize: 13 }}>
            {spec.info.description}
          </p>
        )}
        <div style={{ fontSize: 12, color: '#64748b', marginTop: 4 }}>
          OpenAPI {spec.openapi}
        </div>
      </div>

      {/* Servers */}
      {spec.servers && spec.servers.length > 0 && (
        <div style={{ marginBottom: 12, fontSize: 13 }}>
          <strong>Servers:</strong>{' '}
          {spec.servers.map((s, i) => (
            <span key={i} style={{ color: '#94a3b8' }}>
              {s.url}
              {i < spec.servers!.length - 1 ? ', ' : ''}
            </span>
          ))}
        </div>
      )}

      {/* Controls */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <button
          onClick={expandAll}
          style={{
            padding: '4px 10px',
            background: '#1e293b',
            color: '#94a3b8',
            border: '1px solid #334155',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 12,
          }}
        >
          Expand All
        </button>
        <button
          onClick={collapseAll}
          style={{
            padding: '4px 10px',
            background: '#1e293b',
            color: '#94a3b8',
            border: '1px solid #334155',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 12,
          }}
        >
          Collapse All
        </button>
        {allTags.size > 0 && (
          <select
            value={filterTag}
            onChange={(e) => setFilterTag(e.target.value)}
            style={{
              padding: '4px 8px',
              background: '#1e293b',
              color: '#e2e8f0',
              border: '1px solid #334155',
              borderRadius: 4,
              fontSize: 12,
            }}
          >
            <option value="">All tags</option>
            {Array.from(allTags)
              .sort()
              .map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
          </select>
        )}
        <button
          onClick={fetchSpec}
          style={{
            padding: '4px 10px',
            background: '#1e293b',
            color: '#94a3b8',
            border: '1px solid #334155',
            borderRadius: 4,
            cursor: 'pointer',
            fontSize: 12,
            marginLeft: 'auto',
          }}
        >
          Refresh
        </button>
      </div>

      {/* Paths */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        {sortedPaths.map(([path, methods]) =>
          Object.entries(methods)
            .filter(([, op]) => {
              if (!filterTag) return true;
              const typedOp = op as PathOperation;
              return typedOp.tags?.includes(filterTag);
            })
            .map(([method, op]) => {
              const typedOp = op as PathOperation;
              const key = `${method}:${path}`;
              const isExpanded = expandedPaths.has(key);
              const color = METHOD_COLORS[method] || '#6b7280';

              return (
                <div
                  key={key}
                  style={{
                    background: '#1e293b',
                    borderRadius: 4,
                    border: '1px solid #334155',
                    overflow: 'hidden',
                  }}
                >
                  <div
                    onClick={() => togglePath(key)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                      padding: '6px 10px',
                      cursor: 'pointer',
                      userSelect: 'none',
                    }}
                  >
                    <span
                      style={{
                        background: color,
                        color: '#fff',
                        padding: '1px 6px',
                        borderRadius: 3,
                        fontSize: 11,
                        fontWeight: 700,
                        textTransform: 'uppercase',
                        minWidth: 50,
                        textAlign: 'center',
                      }}
                    >
                      {method}
                    </span>
                    <span style={{ fontFamily: 'monospace', fontSize: 13 }}>{path}</span>
                    {typedOp.summary && (
                      <span style={{ color: '#94a3b8', fontSize: 12, marginLeft: 'auto' }}>
                        {typedOp.summary}
                      </span>
                    )}
                  </div>

                  {isExpanded && (
                    <div
                      style={{
                        padding: '8px 10px',
                        borderTop: '1px solid #334155',
                        fontSize: 12,
                        color: '#94a3b8',
                      }}
                    >
                      {typedOp.operationId && (
                        <div>
                          <strong>Operation ID:</strong> {typedOp.operationId}
                        </div>
                      )}
                      {typedOp.tags && typedOp.tags.length > 0 && (
                        <div>
                          <strong>Tags:</strong> {typedOp.tags.join(', ')}
                        </div>
                      )}
                      {typedOp.parameters && typedOp.parameters.length > 0 && (
                        <div style={{ marginTop: 6 }}>
                          <strong>Parameters:</strong>
                          <ul style={{ margin: '4px 0', paddingLeft: 20 }}>
                            {typedOp.parameters.map((p, i) => (
                              <li key={i}>
                                <code>{p.name}</code> ({p.in})
                                {p.required && <span style={{ color: '#ef4444' }}> *</span>}
                                {p.schema && (
                                  <span style={{ color: '#64748b' }}> - {p.schema.type}</span>
                                )}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                      {typedOp.requestBody && (
                        <div style={{ marginTop: 4 }}>
                          <strong>Request Body:</strong>{' '}
                          {typedOp.requestBody.required ? 'required' : 'optional'}
                        </div>
                      )}
                      {typedOp.responses && (
                        <div style={{ marginTop: 4 }}>
                          <strong>Responses:</strong>
                          {Object.entries(typedOp.responses).map(([code, resp]) => (
                            <span key={code} style={{ marginLeft: 8 }}>
                              <code>{code}</code>: {resp.description}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })
        )}
      </div>

      {sortedPaths.length === 0 && (
        <div style={{ color: '#64748b', padding: 24, textAlign: 'center' }}>
          No paths found in the OpenAPI specification.
        </div>
      )}
    </div>
  );
}
