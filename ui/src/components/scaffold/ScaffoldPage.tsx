import { useState, useCallback, type CSSProperties } from 'react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface Operation {
  method: string;
  path: string;
  summary?: string;
}

interface ResourceInfo {
  name: string;
  basePath: string;
  operations: Operation[];
  hasCreate: boolean;
  hasDetail: boolean;
  hasDelete: boolean;
  formFieldCount: number;
  isAuth: boolean;
}

interface SpecAnalysis {
  resources: ResourceInfo[];
  hasAuth: boolean;
  title: string;
  version: string;
}

interface ScaffoldOptions {
  title: string;
  theme: 'light' | 'dark';
  includeAuth: boolean;
  basePath: string;
}

// ---------------------------------------------------------------------------
// OpenAPI analysis (client-side)
// ---------------------------------------------------------------------------

const AUTH_PATH_PATTERNS = [
  /^\/auth\b/i,
  /^\/login$/i,
  /^\/logout$/i,
  /^\/register$/i,
  /^\/signup$/i,
  /^\/token$/i,
  /^\/refresh$/i,
];

function isAuthPath(path: string): boolean {
  return AUTH_PATH_PATTERNS.some((re) => re.test(path));
}

function resourceNameFromPath(path: string): string {
  // Strip leading slash and split
  const parts = path.replace(/^\//, '').split('/').filter(Boolean);
  // Find first non-parameter segment as resource name
  for (const part of parts) {
    if (!part.startsWith('{')) {
      return part.replace(/-/g, '_');
    }
  }
  return 'resource';
}

function countFormFields(operation: Record<string, unknown>): number {
  try {
    const rb = (operation as Record<string, unknown>).requestBody as Record<string, unknown> | undefined;
    if (!rb) return 0;
    const content = rb.content as Record<string, unknown> | undefined;
    if (!content) return 0;
    const mediaType = content['application/json'] as Record<string, unknown> | undefined;
    if (!mediaType) return 0;
    const schema = mediaType.schema as Record<string, unknown> | undefined;
    if (!schema) return 0;
    const props = schema.properties as Record<string, unknown> | undefined;
    if (props) return Object.keys(props).length;
    return 0;
  } catch {
    return 0;
  }
}

function analyzeSpec(spec: unknown): SpecAnalysis {
  const s = spec as Record<string, unknown>;
  const info = (s.info as Record<string, unknown>) ?? {};
  const title = (info.title as string) ?? 'My App';
  const version = (info.version as string) ?? '1.0.0';
  const paths = (s.paths as Record<string, Record<string, unknown>>) ?? {};

  const resourceMap = new Map<string, ResourceInfo>();

  for (const [path, pathItem] of Object.entries(paths)) {
    const isAuth = isAuthPath(path);
    const resourceName = resourceNameFromPath(path);

    if (!resourceMap.has(resourceName)) {
      resourceMap.set(resourceName, {
        name: resourceName,
        basePath: '/' + resourceName,
        operations: [],
        hasCreate: false,
        hasDetail: false,
        hasDelete: false,
        formFieldCount: 0,
        isAuth,
      });
    }

    const resource = resourceMap.get(resourceName)!;

    const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options'];
    for (const method of HTTP_METHODS) {
      const op = pathItem[method] as Record<string, unknown> | undefined;
      if (!op) continue;

      resource.operations.push({
        method: method.toUpperCase(),
        path,
        summary: (op.summary as string) ?? undefined,
      });

      if (method === 'post') {
        resource.hasCreate = true;
        const fields = countFormFields(op);
        if (fields > resource.formFieldCount) {
          resource.formFieldCount = fields;
        }
      }
      if (method === 'put' || method === 'patch') {
        const fields = countFormFields(op);
        if (fields > resource.formFieldCount) {
          resource.formFieldCount = fields;
        }
      }
      if (path.includes('{') && method === 'get') {
        resource.hasDetail = true;
      }
      if (method === 'delete') {
        resource.hasDelete = true;
      }
    }
  }

  const resources = Array.from(resourceMap.values());
  const hasAuth = resources.some((r) => r.isAuth);

  return { resources, hasAuth, title, version };
}

// ---------------------------------------------------------------------------
// Style constants (Catppuccin Mocha palette, matching the rest of the admin UI)
// ---------------------------------------------------------------------------

const C = {
  base: '#1e1e2e',
  mantle: '#181825',
  crust: '#11111b',
  surface0: '#313244',
  surface1: '#45475a',
  surface2: '#585b70',
  overlay0: '#6c7086',
  overlay1: '#7f849c',
  text: '#cdd6f4',
  subtext: '#a6adc8',
  blue: '#89b4fa',
  green: '#a6e3a1',
  yellow: '#f9e2af',
  peach: '#fab387',
  red: '#f38ba8',
  mauve: '#cba6f7',
  teal: '#94e2d5',
};

const METHOD_COLORS: Record<string, string> = {
  GET: C.green,
  POST: C.blue,
  PUT: C.peach,
  PATCH: C.yellow,
  DELETE: C.red,
};

// ---------------------------------------------------------------------------
// Small reusable components
// ---------------------------------------------------------------------------

function SectionCard({ children, style }: { children: React.ReactNode; style?: CSSProperties }) {
  return (
    <div
      style={{
        background: C.surface0,
        border: `1px solid ${C.surface1}`,
        borderRadius: 8,
        padding: 20,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <h3
      style={{
        margin: '0 0 14px',
        fontSize: 13,
        fontWeight: 600,
        color: C.subtext,
        textTransform: 'uppercase',
        letterSpacing: '0.05em',
      }}
    >
      {children}
    </h3>
  );
}

function MethodBadge({ method }: { method: string }) {
  const color = METHOD_COLORS[method] ?? C.overlay1;
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 7px',
        borderRadius: 4,
        fontSize: 10,
        fontWeight: 700,
        letterSpacing: '0.03em',
        background: color + '22',
        color,
        border: `1px solid ${color}44`,
        fontFamily: 'monospace',
      }}
    >
      {method}
    </span>
  );
}

function Btn({
  children,
  onClick,
  disabled,
  variant = 'secondary',
  style,
}: {
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  variant?: 'primary' | 'secondary' | 'danger';
  style?: CSSProperties;
}) {
  const variantStyles: Record<string, CSSProperties> = {
    primary: {
      background: C.blue + '22',
      color: C.blue,
      border: `1px solid ${C.blue}55`,
    },
    secondary: {
      background: 'transparent',
      color: C.subtext,
      border: `1px solid ${C.surface1}`,
    },
    danger: {
      background: C.red + '22',
      color: C.red,
      border: `1px solid ${C.red}44`,
    },
  };

  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: '7px 16px',
        borderRadius: 6,
        fontSize: 13,
        fontWeight: 500,
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        transition: 'opacity 0.15s',
        fontFamily: 'system-ui, -apple-system, sans-serif',
        ...variantStyles[variant],
        ...style,
      }}
    >
      {children}
    </button>
  );
}

function Input({
  label,
  value,
  onChange,
  placeholder,
  style,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  style?: CSSProperties;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 5, ...style }}>
      <span style={{ fontSize: 12, color: C.subtext, fontWeight: 500 }}>{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        style={{
          background: C.mantle,
          border: `1px solid ${C.surface1}`,
          borderRadius: 6,
          padding: '7px 10px',
          color: C.text,
          fontSize: 13,
          fontFamily: 'system-ui, -apple-system, sans-serif',
          outline: 'none',
        }}
      />
    </label>
  );
}

// ---------------------------------------------------------------------------
// File tree preview
// ---------------------------------------------------------------------------

interface TreeNode {
  name: string;
  children?: TreeNode[];
  note?: string;
}

function FileTree({ node, depth = 0 }: { node: TreeNode; depth?: number }) {
  const isDir = !!node.children;
  const indent = depth * 16;

  return (
    <div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          paddingLeft: indent,
          paddingTop: 2,
          paddingBottom: 2,
          fontSize: 12,
          fontFamily: 'monospace',
          color: isDir ? C.blue : C.text,
        }}
      >
        {isDir ? (
          <span style={{ color: C.overlay0 }}>
            {depth === 0 ? '' : '├── '}
          </span>
        ) : (
          <span style={{ color: C.overlay0 }}>{'├── '}</span>
        )}
        <span>{node.name}</span>
        {node.note && (
          <span style={{ color: C.overlay0, fontSize: 11, fontStyle: 'italic' }}>
            {' '}# {node.note}
          </span>
        )}
      </div>
      {node.children?.map((child, i) => (
        <FileTree key={i} node={child} depth={depth + 1} />
      ))}
    </div>
  );
}

function buildFileTree(analysis: SpecAnalysis, options: ScaffoldOptions): TreeNode {
  const nonAuthResources = analysis.resources.filter((r) => !r.isAuth);
  const includeAuth = options.includeAuth;

  const pages: TreeNode[] = [
    { name: 'DashboardPage.tsx' },
  ];

  if (includeAuth) {
    pages.push({ name: 'LoginPage.tsx', note: 'auth' });
    pages.push({ name: 'RegisterPage.tsx', note: 'auth' });
  }

  for (const resource of nonAuthResources) {
    const pageName = resource.name.charAt(0).toUpperCase() + resource.name.slice(1) + 'Page.tsx';
    pages.push({ name: pageName });
  }

  const components: TreeNode[] = [
    { name: 'Layout.tsx' },
    { name: 'DataTable.tsx' },
    { name: 'FormField.tsx' },
  ];

  const srcChildren: TreeNode[] = [
    { name: 'main.tsx' },
    { name: 'index.css' },
    { name: 'App.tsx' },
    { name: 'api.ts' },
  ];

  if (includeAuth) {
    srcChildren.push({ name: 'auth.tsx', note: 'auth' });
  }

  srcChildren.push({ name: 'components/', children: components });
  srcChildren.push({ name: 'pages/', children: pages });

  return {
    name: 'ui/',
    children: [
      { name: 'package.json' },
      { name: 'tsconfig.json' },
      { name: 'vite.config.ts' },
      { name: 'index.html' },
      { name: 'src/', children: srcChildren },
    ],
  };
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function ScaffoldPage() {
  const [specText, setSpecText] = useState('');
  const [analysis, setAnalysis] = useState<SpecAnalysis | null>(null);
  const [specLoaded, setSpecLoaded] = useState(false);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [fetchLoading, setFetchLoading] = useState(false);
  const [downloadLoading, setDownloadLoading] = useState(false);
  const [downloadMessage, setDownloadMessage] = useState<string | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);

  const [options, setOptions] = useState<ScaffoldOptions>({
    title: '',
    theme: 'dark',
    includeAuth: false,
    basePath: '/api',
  });

  // ---------------------------------------------------------------------------
  // Fetch spec from server
  // ---------------------------------------------------------------------------

  const handleFetch = useCallback(async () => {
    setFetchError(null);
    setFetchLoading(true);
    setSpecLoaded(false);
    setAnalysis(null);
    setParseError(null);

    const token = localStorage.getItem('auth_token');
    const headers: Record<string, string> = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;

    // Try JSON first, then YAML
    const urls = ['/api/openapi.json', '/api/openapi.yaml', '/openapi.json', '/openapi.yaml'];
    let fetched = false;

    for (const url of urls) {
      try {
        const res = await fetch(url, { headers });
        if (res.ok) {
          const text = await res.text();
          setSpecText(text);
          fetched = true;
          break;
        }
      } catch {
        // try next
      }
    }

    setFetchLoading(false);

    if (!fetched) {
      setFetchError('Could not fetch OpenAPI spec. Try pasting it manually below.');
      return;
    }

    setSpecLoaded(true);
  }, []);

  // ---------------------------------------------------------------------------
  // Analyze
  // ---------------------------------------------------------------------------

  const handleAnalyze = useCallback(() => {
    setParseError(null);
    setAnalysis(null);

    if (!specText.trim()) {
      setParseError('Paste or fetch an OpenAPI spec first.');
      return;
    }

    try {
      let parsed: unknown;
      const trimmed = specText.trim();

      if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
        parsed = JSON.parse(trimmed);
      } else {
        // Very lightweight YAML→JSON shim for simple OpenAPI specs.
        // For full YAML support the user should paste pre-converted JSON.
        // We attempt JSON.parse anyway in case the spec is actually JSON with
        // leading whitespace/BOM; fall back to a helpful message.
        try {
          parsed = JSON.parse(trimmed);
        } catch {
          throw new Error(
            'YAML parsing is not supported in the browser. Please paste the spec as JSON (use: curl … | python3 -c "import sys,json,yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout, indent=2)").',
          );
        }
      }

      const result = analyzeSpec(parsed);
      setAnalysis(result);
      setOptions((prev) => ({
        ...prev,
        title: prev.title || result.title,
        includeAuth: result.hasAuth,
      }));
    } catch (e) {
      setParseError(e instanceof Error ? e.message : 'Failed to parse spec');
    }
  }, [specText]);

  // ---------------------------------------------------------------------------
  // Download scaffold
  // ---------------------------------------------------------------------------

  const handleDownload = useCallback(async () => {
    if (!analysis) return;

    setDownloadLoading(true);
    setDownloadMessage(null);

    let specJson: unknown;
    try {
      specJson = JSON.parse(specText.trim());
    } catch {
      setDownloadMessage('Could not parse spec as JSON for upload.');
      setDownloadLoading(false);
      return;
    }

    const token = localStorage.getItem('auth_token');
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (token) headers['Authorization'] = `Bearer ${token}`;

    try {
      const res = await fetch('/api/v1/admin/scaffold/generate', {
        method: 'POST',
        headers,
        body: JSON.stringify({ spec: specJson, options }),
      });

      if (res.status === 404) {
        setDownloadMessage(
          'Backend scaffold endpoint not available. Use the CLI instead: wfctl ui scaffold --spec openapi.json --out ./ui',
        );
        setDownloadLoading(false);
        return;
      }

      if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(`Server error ${res.status}: ${text}`);
      }

      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'ui-scaffold.zip';
      a.click();
      URL.revokeObjectURL(url);
      setDownloadMessage('Download started!');
    } catch (e) {
      setDownloadMessage(e instanceof Error ? e.message : 'Download failed');
    } finally {
      setDownloadLoading(false);
    }
  }, [analysis, specText, options]);

  // ---------------------------------------------------------------------------
  // Derived data
  // ---------------------------------------------------------------------------

  const totalOps = analysis
    ? analysis.resources.reduce((sum, r) => sum + r.operations.length, 0)
    : 0;
  const totalFields = analysis
    ? analysis.resources.reduce((sum, r) => sum + r.formFieldCount, 0)
    : 0;

  const fileTree = analysis ? buildFileTree(analysis, options) : null;

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  const containerStyle: CSSProperties = {
    flex: 1,
    background: C.base,
    overflow: 'auto',
    padding: 28,
    fontFamily: 'system-ui, -apple-system, sans-serif',
  };

  const twoColStyle: CSSProperties = {
    display: 'grid',
    gridTemplateColumns: '1fr 1fr',
    gap: 16,
  };

  return (
    <div style={containerStyle}>
      {/* Header */}
      <div style={{ marginBottom: 28 }}>
        <h2 style={{ color: C.text, margin: '0 0 6px', fontSize: 22, fontWeight: 700 }}>
          UI Scaffold Generator
        </h2>
        <p style={{ color: C.subtext, margin: 0, fontSize: 14 }}>
          Generate a React + TypeScript UI from your API's OpenAPI specification
        </p>
      </div>

      {/* Source Section */}
      <SectionCard style={{ marginBottom: 16 }}>
        <SectionTitle>Source</SectionTitle>

        <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 14 }}>
          <Btn variant="primary" onClick={handleFetch} disabled={fetchLoading}>
            {fetchLoading ? 'Fetching…' : 'Fetch from current app'}
          </Btn>
          <span style={{ color: C.overlay0, fontSize: 12 }}>
            Tries /api/openapi.json and /api/openapi.yaml
          </span>
          {specLoaded && (
            <span
              style={{
                marginLeft: 'auto',
                fontSize: 12,
                color: C.green,
                background: C.green + '22',
                border: `1px solid ${C.green}44`,
                borderRadius: 6,
                padding: '3px 10px',
                fontWeight: 600,
              }}
            >
              Spec loaded
            </span>
          )}
        </div>

        {fetchError && (
          <div
            style={{
              color: C.red,
              fontSize: 12,
              background: C.red + '15',
              border: `1px solid ${C.red}33`,
              borderRadius: 6,
              padding: '8px 12px',
              marginBottom: 12,
            }}
          >
            {fetchError}
          </div>
        )}

        <div style={{ marginBottom: 8 }}>
          <span style={{ fontSize: 12, color: C.subtext, fontWeight: 500 }}>
            Or paste an OpenAPI spec (JSON)
          </span>
        </div>
        <textarea
          value={specText}
          onChange={(e) => {
            setSpecText(e.target.value);
            setSpecLoaded(!!e.target.value.trim());
            setAnalysis(null);
            setParseError(null);
          }}
          placeholder={'{\n  "openapi": "3.0.0",\n  "info": { "title": "My API", "version": "1.0.0" },\n  "paths": { ... }\n}'}
          spellCheck={false}
          style={{
            width: '100%',
            minHeight: 160,
            background: C.mantle,
            border: `1px solid ${C.surface1}`,
            borderRadius: 6,
            color: C.text,
            fontSize: 12,
            fontFamily: 'monospace',
            padding: '10px 12px',
            resize: 'vertical',
            outline: 'none',
            boxSizing: 'border-box',
          }}
        />

        <div style={{ display: 'flex', gap: 10, marginTop: 12 }}>
          <Btn
            variant="primary"
            onClick={handleAnalyze}
            disabled={!specText.trim()}
          >
            Analyze Spec
          </Btn>
          {specText.trim() && (
            <Btn
              variant="secondary"
              onClick={() => {
                setSpecText('');
                setSpecLoaded(false);
                setAnalysis(null);
                setParseError(null);
                setFetchError(null);
              }}
            >
              Clear
            </Btn>
          )}
        </div>

        {parseError && (
          <div
            style={{
              color: C.red,
              fontSize: 12,
              background: C.red + '15',
              border: `1px solid ${C.red}33`,
              borderRadius: 6,
              padding: '8px 12px',
              marginTop: 10,
            }}
          >
            {parseError}
          </div>
        )}
      </SectionCard>

      {/* Analysis Panel */}
      {analysis && (
        <SectionCard style={{ marginBottom: 16 }}>
          <SectionTitle>Analysis</SectionTitle>

          {/* Stats row */}
          <div style={{ display: 'flex', gap: 12, marginBottom: 18, flexWrap: 'wrap' }}>
            {[
              { label: 'Title', value: analysis.title, color: C.blue },
              { label: 'Version', value: analysis.version, color: C.mauve },
              { label: 'Resources', value: analysis.resources.length, color: C.teal },
              { label: 'Operations', value: totalOps, color: C.peach },
              { label: 'Form Fields', value: totalFields, color: C.yellow },
            ].map((stat) => (
              <div
                key={stat.label}
                style={{
                  background: C.mantle,
                  border: `1px solid ${C.surface1}`,
                  borderLeft: `3px solid ${stat.color}`,
                  borderRadius: 6,
                  padding: '10px 14px',
                  minWidth: 110,
                }}
              >
                <div style={{ fontSize: 18, fontWeight: 700, color: C.text }}>{stat.value}</div>
                <div style={{ fontSize: 11, color: C.overlay0, marginTop: 3 }}>{stat.label}</div>
              </div>
            ))}
            {analysis.hasAuth && (
              <div
                style={{
                  background: C.green + '15',
                  border: `1px solid ${C.green}44`,
                  borderRadius: 6,
                  padding: '10px 14px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                }}
              >
                <span style={{ fontSize: 14, color: C.green }}>Auth detected</span>
              </div>
            )}
          </div>

          {/* Resource table */}
          <div style={{ overflowX: 'auto' }}>
            <table
              style={{
                width: '100%',
                borderCollapse: 'collapse',
                fontSize: 13,
              }}
            >
              <thead>
                <tr
                  style={{
                    background: C.mantle,
                    borderBottom: `1px solid ${C.surface1}`,
                  }}
                >
                  {['Resource', 'Operations', 'Form Fields', 'Auth'].map((h) => (
                    <th
                      key={h}
                      style={{
                        padding: '8px 12px',
                        textAlign: 'left',
                        color: C.subtext,
                        fontWeight: 600,
                        fontSize: 11,
                        textTransform: 'uppercase',
                        letterSpacing: '0.04em',
                      }}
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {analysis.resources.map((resource, i) => (
                  <tr
                    key={resource.name}
                    style={{
                      background: i % 2 === 0 ? 'transparent' : C.mantle + '80',
                      borderBottom: `1px solid ${C.surface1}44`,
                    }}
                  >
                    <td style={{ padding: '8px 12px', color: C.text, fontFamily: 'monospace', fontWeight: 600 }}>
                      {resource.name}
                    </td>
                    <td style={{ padding: '8px 12px' }}>
                      <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                        {Array.from(new Set(resource.operations.map((o) => o.method))).map((m) => (
                          <MethodBadge key={m} method={m} />
                        ))}
                      </div>
                    </td>
                    <td style={{ padding: '8px 12px', color: resource.formFieldCount > 0 ? C.text : C.overlay0 }}>
                      {resource.formFieldCount > 0 ? resource.formFieldCount : '—'}
                    </td>
                    <td style={{ padding: '8px 12px' }}>
                      {resource.isAuth ? (
                        <span
                          style={{
                            fontSize: 11,
                            color: C.green,
                            background: C.green + '22',
                            border: `1px solid ${C.green}44`,
                            borderRadius: 4,
                            padding: '2px 7px',
                            fontWeight: 600,
                          }}
                        >
                          yes
                        </span>
                      ) : (
                        <span style={{ color: C.overlay0, fontSize: 12 }}>—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </SectionCard>
      )}

      {/* Config + File Tree side by side (when analysis is available) */}
      {analysis && (
        <div style={twoColStyle}>
          {/* Configuration */}
          <SectionCard>
            <SectionTitle>Configuration</SectionTitle>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              <Input
                label="App Title"
                value={options.title}
                onChange={(v) => setOptions((p) => ({ ...p, title: v }))}
                placeholder="My App"
              />

              <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                <span style={{ fontSize: 12, color: C.subtext, fontWeight: 500 }}>Theme</span>
                <div style={{ display: 'flex', gap: 8 }}>
                  {(['light', 'dark'] as const).map((t) => (
                    <button
                      key={t}
                      onClick={() => setOptions((p) => ({ ...p, theme: t }))}
                      style={{
                        flex: 1,
                        padding: '7px 0',
                        borderRadius: 6,
                        fontSize: 13,
                        fontWeight: options.theme === t ? 600 : 400,
                        cursor: 'pointer',
                        background: options.theme === t ? C.blue + '22' : 'transparent',
                        color: options.theme === t ? C.blue : C.subtext,
                        border: `1px solid ${options.theme === t ? C.blue + '55' : C.surface1}`,
                        transition: 'all 0.15s',
                        fontFamily: 'system-ui, -apple-system, sans-serif',
                      }}
                    >
                      {t.charAt(0).toUpperCase() + t.slice(1)}
                    </button>
                  ))}
                </div>
              </label>

              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  cursor: 'pointer',
                  padding: '8px 12px',
                  background: C.mantle,
                  borderRadius: 6,
                  border: `1px solid ${C.surface1}`,
                }}
              >
                <input
                  type="checkbox"
                  checked={options.includeAuth}
                  onChange={(e) => setOptions((p) => ({ ...p, includeAuth: e.target.checked }))}
                  style={{ accentColor: C.blue, width: 15, height: 15 }}
                />
                <div>
                  <div style={{ fontSize: 13, color: C.text, fontWeight: 500 }}>Include auth pages</div>
                  <div style={{ fontSize: 11, color: C.overlay0, marginTop: 2 }}>
                    {analysis.hasAuth ? 'Auto-detected from spec' : 'No auth endpoints found in spec'}
                  </div>
                </div>
              </label>

              <Input
                label="API Base Path"
                value={options.basePath}
                onChange={(v) => setOptions((p) => ({ ...p, basePath: v }))}
                placeholder="/api"
              />
            </div>
          </SectionCard>

          {/* File Tree Preview */}
          <SectionCard>
            <SectionTitle>File Tree Preview</SectionTitle>
            <div
              style={{
                background: C.crust,
                border: `1px solid ${C.surface1}`,
                borderRadius: 6,
                padding: '14px 12px',
                overflow: 'auto',
                maxHeight: 380,
              }}
            >
              {fileTree && <FileTree node={fileTree} />}
            </div>
            <div style={{ marginTop: 10, fontSize: 11, color: C.overlay0 }}>
              {analysis.resources.filter((r) => !r.isAuth).length} resource pages
              {options.includeAuth ? ' + 2 auth pages' : ''}
              {' + shared components'}
            </div>
          </SectionCard>
        </div>
      )}

      {/* Action Buttons */}
      {analysis && (
        <SectionCard style={{ marginTop: 16 }}>
          <SectionTitle>Generate</SectionTitle>
          <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
            <Btn
              variant="primary"
              onClick={handleDownload}
              disabled={downloadLoading}
              style={{ padding: '9px 22px', fontWeight: 600 }}
            >
              {downloadLoading ? 'Generating…' : 'Download Scaffold'}
            </Btn>
            <span style={{ color: C.overlay0, fontSize: 12 }}>
              Downloads a .zip containing the full React + TypeScript UI
            </span>
          </div>

          {downloadMessage && (
            <div
              style={{
                marginTop: 12,
                padding: '10px 14px',
                borderRadius: 6,
                fontSize: 13,
                background: downloadMessage.startsWith('Download started')
                  ? C.green + '15'
                  : downloadMessage.includes('wfctl')
                  ? C.yellow + '15'
                  : C.red + '15',
                color: downloadMessage.startsWith('Download started')
                  ? C.green
                  : downloadMessage.includes('wfctl')
                  ? C.yellow
                  : C.red,
                border: `1px solid ${
                  downloadMessage.startsWith('Download started')
                    ? C.green + '44'
                    : downloadMessage.includes('wfctl')
                    ? C.yellow + '44'
                    : C.red + '33'
                }`,
              }}
            >
              {downloadMessage}
            </div>
          )}

          <div
            style={{
              marginTop: 14,
              padding: '10px 14px',
              background: C.mantle,
              borderRadius: 6,
              border: `1px solid ${C.surface1}`,
              fontSize: 12,
              color: C.overlay1,
            }}
          >
            <span style={{ fontWeight: 600, color: C.subtext }}>CLI alternative:</span>
            <code
              style={{
                display: 'block',
                marginTop: 6,
                fontFamily: 'monospace',
                color: C.teal,
                fontSize: 12,
              }}
            >
              wfctl ui scaffold --spec openapi.json --out ./ui --title "{options.title || 'My App'}"
              {options.includeAuth ? ' --auth' : ''}
              {options.theme === 'light' ? ' --theme light' : ''}
            </code>
          </div>
        </SectionCard>
      )}

      {/* Empty state when no spec loaded yet */}
      {!analysis && !specText.trim() && (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: '48px 0',
            color: C.overlay0,
            gap: 10,
          }}
        >
          <div style={{ fontSize: 40 }}>⚙</div>
          <div style={{ fontSize: 15, fontWeight: 500, color: C.subtext }}>
            No spec loaded yet
          </div>
          <div style={{ fontSize: 13, maxWidth: 380, textAlign: 'center', lineHeight: 1.6 }}>
            Fetch the OpenAPI spec from your running app, or paste it manually above, then click
            "Analyze Spec" to see the resource breakdown.
          </div>
        </div>
      )}
    </div>
  );
}
