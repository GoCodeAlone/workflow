package main

// ob / cb are Go template actions that output literal {{ and }} in the generated files.
// All JSX double-brace patterns like style={{ ... }} must use these to avoid
// being mis-parsed as Go template directives.
//
// Usage inside a template string: {{ob}} ... {{cb}}
// e.g.  style={{ob}} color: 'red' {{cb}}

// scaffoldTemplates maps template names to their raw Go text/template source.
var scaffoldTemplates = map[string]string{
	"package.json": `{
  "name": "{{.Title | lower | replace " " "-"}}",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.28.0"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.7.2",
    "vite": "^6.0.5"
  }
}
`,

	"tsconfig.json": `{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["src"]
}
`,

	"vite.config.ts": `import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
`,

	"index.html": `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>{{.Title}}</title>
    <style>
      *, *::before, *::after { box-sizing: border-box; }
      body { margin: 0; font-family: system-ui, -apple-system, sans-serif; }
    </style>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`,

	"main.tsx": `import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import './index.css';
{{- if .HasAuth}}
import { AuthProvider } from './auth';
{{- end}}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
{{- if .HasAuth}}
      <AuthProvider>
        <App />
      </AuthProvider>
{{- else}}
      <App />
{{- end}}
    </BrowserRouter>
  </StrictMode>
);
`,

	"App.tsx": `import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import DashboardPage from './pages/DashboardPage';
{{- if .HasAuth}}
import LoginPage from './pages/LoginPage';
import RegisterPage from './pages/RegisterPage';
import { useAuth } from './auth';
{{- end}}
{{- range .Resources}}
import {{.Name}}Page from './pages/{{.Name}}Page';
{{- end}}
{{- if .HasAuth}}
function PrivateRoute({ children }: { children: React.ReactNode }) {
  const { token } = useAuth();
  return token ? <>{children}</> : <Navigate to="/login" replace />;
}
{{- end}}

export default function App() {
  return (
    <Routes>
{{- if .HasAuth}}
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route path="/" element={
        <PrivateRoute>
          <Layout />
        </PrivateRoute>
      }>
{{- else}}
      <Route path="/" element={<Layout />}>
{{- end}}
        <Route index element={<DashboardPage />} />
{{- range .Resources}}
        <Route path="{{.NameLower}}" element={<{{.Name}}Page />} />
{{- end}}
      </Route>
    </Routes>
  );
}
`,

	// api.ts is a static file — no template directives in the fixed parts.
	// The dynamic part is the list of exported functions.
	"api.ts": `const API_BASE = '';

async function apiCall(method: string, path: string, body?: unknown): Promise<unknown> {
  const token = localStorage.getItem('token');
  const res = await fetch(API_BASE + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: ` + "`" + `Bearer ${token}` + "`" + ` } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) {
    localStorage.removeItem('token');
    window.location.href = '/login';
    throw new Error('Unauthorized');
  }
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(` + "`" + `HTTP ${res.status}: ${text}` + "`" + `);
  }
  const ct = res.headers.get('content-type') ?? '';
  if (ct.includes('application/json')) {
    return res.json();
  }
  return res.text();
}

// Generated API functions
{{range .Operations}}
export const {{.FuncName}} = ({{tsTupleArgs .Method .PathParams .HasBody}}) =>
  apiCall('{{.Method}}', {{jsPath .Path}}{{if or .HasBody (eq .Method "POST") (eq .Method "PUT") (eq .Method "PATCH")}}, data{{end}});
{{end}}
`,

	// auth.tsx has no template directives — it is emitted verbatim.
	// The value prop uses {{ }} for JSX object literal; escape with ob/cb funcs.
	"auth.tsx": `import { createContext, useContext, useState, useCallback, ReactNode } from 'react';

interface AuthContextValue {
  token: string | null;
  login: (token: string) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() =>
    localStorage.getItem('token')
  );

  const login = useCallback((t: string) => {
    localStorage.setItem('token', t);
    setToken(t);
  }, []);

  const logout = useCallback(() => {
    localStorage.removeItem('token');
    setToken(null);
  }, []);

  const value: AuthContextValue = { token, login, logout };
  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider');
  return ctx;
}
`,

	"Layout.tsx": `import { Outlet, NavLink } from 'react-router-dom';
{{- if .HasAuth}}
import { useAuth } from '../auth';
{{- end}}
import type { CSSProperties } from 'react';

const navStyle: CSSProperties = {
  width: 220,
  minHeight: '100vh',
  background: '#1a1a2e',
  color: '#eee',
  padding: '1rem',
  display: 'flex',
  flexDirection: 'column',
  gap: '0.5rem',
};

const linkStyle: CSSProperties = {
  color: '#ccc',
  textDecoration: 'none',
  padding: '0.4rem 0.6rem',
  borderRadius: 4,
};

const mainStyle: CSSProperties = {
  flex: 1,
  padding: '2rem',
};

const titleStyle: CSSProperties = {
  fontSize: '1.1rem',
  fontWeight: 700,
  marginBottom: '1rem',
  color: '#fff',
};

const wrapStyle: CSSProperties = {
  display: 'flex',
};

export default function Layout() {
{{- if .HasAuth}}
  const { logout } = useAuth();
{{- end}}
  return (
    <div style={wrapStyle}>
      <nav style={navStyle}>
        <div style={titleStyle}>{{.Title}}</div>
        <NavLink to="/" end style={linkStyle}>Dashboard</NavLink>
{{- range .Resources}}
        <NavLink to="/{{.NameLower}}" style={linkStyle}>{{.Name}}</NavLink>
{{- end}}
{{- if .HasAuth}}
        <div style={logoutWrapStyle}>
          <button onClick={logout} style={logoutBtnStyle}>Logout</button>
        </div>
{{- end}}
      </nav>
      <main style={mainStyle}>
        <Outlet />
      </main>
    </div>
  );
}
{{- if .HasAuth}}

const logoutWrapStyle: CSSProperties = { marginTop: 'auto' };
const logoutBtnStyle: CSSProperties = {
  background: 'none',
  border: '1px solid #555',
  color: '#ccc',
  padding: '0.4rem 0.8rem',
  borderRadius: 4,
  cursor: 'pointer',
};
{{- end}}
`,

	"DataTable.tsx": `import type { CSSProperties } from 'react';

interface Column<T> {
  key: keyof T;
  label: string;
}

interface DataTableProps<T extends Record<string, unknown>> {
  columns: Column<T>[];
  rows: T[];
  onSelect?: (row: T) => void;
}

const tableStyle: CSSProperties = { width: '100%', borderCollapse: 'collapse' };
const thStyle: CSSProperties = {
  textAlign: 'left',
  padding: '0.5rem 1rem',
  background: '#f5f5f5',
  borderBottom: '2px solid #ddd',
  fontWeight: 600,
};
const tdStyle: CSSProperties = {
  padding: '0.5rem 1rem',
  borderBottom: '1px solid #eee',
};
const emptyStyle: CSSProperties = {
  padding: '0.5rem 1rem',
  borderBottom: '1px solid #eee',
  textAlign: 'center',
  color: '#999',
};

export default function DataTable<T extends Record<string, unknown>>({
  columns,
  rows,
  onSelect,
}: DataTableProps<T>) {
  return (
    <table style={tableStyle}>
      <thead>
        <tr>
          {columns.map((col) => (
            <th key={String(col.key)} style={thStyle}>{col.label}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr
            key={i}
            onClick={() => onSelect?.(row)}
            style={onSelect ? { cursor: 'pointer' } : undefined}
          >
            {columns.map((col) => (
              <td key={String(col.key)} style={tdStyle}>
                {String(row[col.key] ?? '')}
              </td>
            ))}
          </tr>
        ))}
        {rows.length === 0 && (
          <tr>
            <td colSpan={columns.length} style={emptyStyle}>
              No data
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}
`,

	"FormField.tsx": `import type { CSSProperties } from 'react';

interface FormFieldProps {
  name: string;
  label: string;
  type?: string;
  value: string;
  onChange: (name: string, value: string) => void;
  required?: boolean;
  options?: string[];
}

const wrapStyle: CSSProperties = { marginBottom: '1rem' };
const labelStyle: CSSProperties = {
  display: 'block',
  marginBottom: '0.25rem',
  fontWeight: 500,
  fontSize: '0.9rem',
};
const inputStyle: CSSProperties = {
  width: '100%',
  padding: '0.5rem 0.75rem',
  border: '1px solid #ccc',
  borderRadius: 4,
  fontSize: '1rem',
};
const reqStyle: CSSProperties = { color: 'red' };

export default function FormField({
  name,
  label,
  type = 'text',
  value,
  onChange,
  required = false,
  options = [],
}: FormFieldProps) {
  return (
    <div style={wrapStyle}>
      <label htmlFor={name} style={labelStyle}>
        {label}{required && <span style={reqStyle}> *</span>}
      </label>
      {type === 'select' ? (
        <select
          id={name}
          name={name}
          value={value}
          required={required}
          onChange={(e) => onChange(name, e.target.value)}
          style={inputStyle}
        >
          <option value="">Select...</option>
          {options.map((o) => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
      ) : (
        <input
          id={name}
          name={name}
          type={type}
          value={value}
          required={required}
          onChange={(e) => onChange(name, e.target.value)}
          style={inputStyle}
        />
      )}
    </div>
  );
}
`,

	"DashboardPage.tsx": `import type { CSSProperties } from 'react';

const headStyle: CSSProperties = { marginTop: 0 };
const cardGridStyle: CSSProperties = {
  display: 'flex',
  gap: '1rem',
  flexWrap: 'wrap',
  marginTop: '1.5rem',
};
const cardStyle: CSSProperties = {
  display: 'block',
  padding: '1rem 1.5rem',
  border: '1px solid #ddd',
  borderRadius: 8,
  textDecoration: 'none',
  color: 'inherit',
  background: '#fafafa',
};
const cardTitleStyle: CSSProperties = { fontWeight: 700, fontSize: '1.1rem' };

export default function DashboardPage() {
  return (
    <div>
      <h1 style={headStyle}>{{.Title}}</h1>
      <p>Welcome to the dashboard. Use the navigation to explore the API resources.</p>
{{- if .Resources}}
      <div style={cardGridStyle}>
{{- range .Resources}}
        <a href="/{{.NameLower}}" style={cardStyle}>
          <div style={cardTitleStyle}>{{.Name}}</div>
        </a>
{{- end}}
      </div>
{{- end}}
    </div>
  );
}
`,

	"LoginPage.tsx": `import { useState, FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '../auth';
import type { CSSProperties } from 'react';

const pageStyle: CSSProperties = {
  minHeight: '100vh',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: '#f5f5f5',
};
const cardStyle: CSSProperties = {
  background: '#fff',
  padding: '2rem',
  borderRadius: 8,
  width: 360,
  boxShadow: '0 2px 8px rgba(0,0,0,0.1)',
};
const headStyle: CSSProperties = { marginTop: 0, fontSize: '1.5rem' };
const errStyle: CSSProperties = { color: 'red', marginBottom: '1rem', fontSize: '0.9rem' };
const fieldStyle: CSSProperties = { marginBottom: '1rem' };
const fieldStyleLast: CSSProperties = { marginBottom: '1.5rem' };
const labelStyle: CSSProperties = { display: 'block', marginBottom: '0.25rem', fontWeight: 500 };
const inputStyle: CSSProperties = {
  width: '100%',
  padding: '0.5rem 0.75rem',
  border: '1px solid #ccc',
  borderRadius: 4,
  fontSize: '1rem',
};
const btnStyle: CSSProperties = {
  width: '100%',
  padding: '0.6rem',
  background: '#1a1a2e',
  color: '#fff',
  border: 'none',
  borderRadius: 4,
  fontSize: '1rem',
  cursor: 'pointer',
};
const footerStyle: CSSProperties = { marginTop: '1rem', textAlign: 'center', fontSize: '0.9rem' };

export default function LoginPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const tok = localStorage.getItem('token');
      const res = await fetch('{{.LoginPath}}', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(tok ? { Authorization: ` + "`" + `Bearer ${tok}` + "`" + ` } : {}),
        },
        body: JSON.stringify({ email, password }),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => 'Login failed');
        throw new Error(text);
      }
      const data = await res.json() as Record<string, unknown>;
      const token = (data.token ?? data.access_token ?? data.jwt) as string | undefined;
      if (!token) throw new Error('No token in response');
      login(token);
      navigate('/');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={pageStyle}>
      <div style={cardStyle}>
        <h1 style={headStyle}>Sign In</h1>
        {error && <div style={errStyle}>{error}</div>}
        <form onSubmit={handleSubmit}>
          <div style={fieldStyle}>
            <label htmlFor="email" style={labelStyle}>Email</label>
            <input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required style={inputStyle} />
          </div>
          <div style={fieldStyleLast}>
            <label htmlFor="password" style={labelStyle}>Password</label>
            <input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required style={inputStyle} />
          </div>
          <button type="submit" disabled={loading} style={btnStyle}>
            {loading ? 'Signing in...' : 'Sign In'}
          </button>
        </form>
        <p style={footerStyle}>
          Don't have an account? <Link to="/register">Register</Link>
        </p>
      </div>
    </div>
  );
}
`,

	"RegisterPage.tsx": `import { useState, FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '../auth';
import type { CSSProperties } from 'react';

const pageStyle: CSSProperties = {
  minHeight: '100vh',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: '#f5f5f5',
};
const cardStyle: CSSProperties = {
  background: '#fff',
  padding: '2rem',
  borderRadius: 8,
  width: 360,
  boxShadow: '0 2px 8px rgba(0,0,0,0.1)',
};
const headStyle: CSSProperties = { marginTop: 0, fontSize: '1.5rem' };
const errStyle: CSSProperties = { color: 'red', marginBottom: '1rem', fontSize: '0.9rem' };
const fieldStyle: CSSProperties = { marginBottom: '1rem' };
const fieldStyleLast: CSSProperties = { marginBottom: '1.5rem' };
const labelStyle: CSSProperties = { display: 'block', marginBottom: '0.25rem', fontWeight: 500 };
const inputStyle: CSSProperties = {
  width: '100%',
  padding: '0.5rem 0.75rem',
  border: '1px solid #ccc',
  borderRadius: 4,
  fontSize: '1rem',
};
const btnStyle: CSSProperties = {
  width: '100%',
  padding: '0.6rem',
  background: '#1a1a2e',
  color: '#fff',
  border: 'none',
  borderRadius: 4,
  fontSize: '1rem',
  cursor: 'pointer',
};
const footerStyle: CSSProperties = { marginTop: '1rem', textAlign: 'center', fontSize: '0.9rem' };

export default function RegisterPage() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const res = await fetch('{{.RegisterPath}}', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => 'Registration failed');
        throw new Error(text);
      }
      const data = await res.json() as Record<string, unknown>;
      const token = (data.token ?? data.access_token ?? data.jwt) as string | undefined;
      if (token) {
        login(token);
        navigate('/');
      } else {
        navigate('/login');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Registration failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={pageStyle}>
      <div style={cardStyle}>
        <h1 style={headStyle}>Create Account</h1>
        {error && <div style={errStyle}>{error}</div>}
        <form onSubmit={handleSubmit}>
          <div style={fieldStyle}>
            <label htmlFor="email" style={labelStyle}>Email</label>
            <input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required style={inputStyle} />
          </div>
          <div style={fieldStyleLast}>
            <label htmlFor="password" style={labelStyle}>Password</label>
            <input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required style={inputStyle} />
          </div>
          <button type="submit" disabled={loading} style={btnStyle}>
            {loading ? 'Creating account...' : 'Create Account'}
          </button>
        </form>
        <p style={footerStyle}>
          Already have an account? <Link to="/login">Sign In</Link>
        </p>
      </div>
    </div>
  );
}
`,

	"index.css": `/* Generated by wfctl ui scaffold */
*,
*::before,
*::after {
  box-sizing: border-box;
}

body {
  margin: 0;
  font-family: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

code {
  font-family: source-code-pro, Menlo, Monaco, Consolas, 'Courier New', monospace;
}
`,

	"ResourcePage.tsx": `{{- if .CreateOp}}import { useState, useEffect, FormEvent } from 'react';
{{- else}}import { useState, useEffect } from 'react';
{{- end}}
import type { CSSProperties } from 'react';
import DataTable from '../components/DataTable';
{{- if .FormFields}}
import FormField from '../components/FormField';
{{- end}}

type Item = Record<string, unknown>;

const headerStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' };
const h1Style: CSSProperties = { margin: 0 };
const errStyle: CSSProperties = { color: 'red', marginBottom: '1rem' };
const loadingStyle: CSSProperties = { color: '#999' };
const detailBoxStyle: CSSProperties = { marginTop: '1.5rem', background: '#f9f9f9', padding: '1.5rem', borderRadius: 8, border: '1px solid #eee' };
const detailHeadStyle: CSSProperties = { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' };
const detailTitleStyle: CSSProperties = { margin: 0, fontSize: '1.1rem' };
const detailBtnsStyle: CSSProperties = { display: 'flex', gap: '0.5rem' };
const deleteBtnStyle: CSSProperties = { padding: '0.4rem 0.8rem', background: '#dc3545', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' };
const closeBtnStyle: CSSProperties = { padding: '0.4rem 0.8rem', background: '#6c757d', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' };
const preStyle: CSSProperties = { margin: 0, fontSize: '0.85rem', overflow: 'auto' };

export default function {{.Name}}Page() {
  const [items, setItems] = useState<Item[]>([]);
  const [selected, setSelected] = useState<Item | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
{{- if .CreateOp}}
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<Record<string, string>>({
{{- range .FormFields}}
    {{.Name}}: '',
{{- end}}
  });
{{- end}}
{{if .ListOp}}
  useEffect(() => {
    loadItems();
  }, []);

  async function loadItems() {
    setLoading(true);
    try {
      const tok = localStorage.getItem('token');
      const res = await fetch('{{.ListOp.Path}}', {
        headers: tok ? { Authorization: ` + "`" + `Bearer ${tok}` + "`" + ` } : {},
      });
      if (!res.ok) throw new Error(` + "`" + `HTTP ${res.status}` + "`" + `);
      const data = await res.json() as unknown;
      setItems(Array.isArray(data) ? data as Item[] : (data as { items?: Item[], data?: Item[] })?.items ?? (data as { items?: Item[], data?: Item[] })?.data ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }
{{end}}
{{if .CreateOp}}
  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    setError('');
    try {
      const tok = localStorage.getItem('token');
      const res = await fetch('{{.CreateOp.Path}}', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(tok ? { Authorization: ` + "`" + `Bearer ${tok}` + "`" + ` } : {}),
        },
        body: JSON.stringify(form),
      });
      if (!res.ok) throw new Error(` + "`" + `HTTP ${res.status}` + "`" + `);
      setShowForm(false);
      setForm({
{{- range .FormFields}}
        {{.Name}}: '',
{{- end}}
      });
{{- if .ListOp}}
      await loadItems();
{{- end}}
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create');
    }
  }
{{end}}
{{if .DeleteOp}}
  async function handleDelete(item: Item) {
    if (!window.confirm('Delete this item?')) return;
    const idKeys = ['id', 'ID', '_id', 'uuid', 'UUID'];
    const id = idKeys.map((k) => item[k]).find((v) => v != null);
    if (!id) return;
    try {
      const tok = localStorage.getItem('token');
      const res = await fetch('{{.DeleteOp.Path}}'.replace(/\{[^}]+\}/, String(id)), {
        method: 'DELETE',
        headers: tok ? { Authorization: ` + "`" + `Bearer ${tok}` + "`" + ` } : {},
      });
      if (!res.ok) throw new Error(` + "`" + `HTTP ${res.status}` + "`" + `);
{{- if .ListOp}}
      await loadItems();
{{- end}}
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete');
    }
  }
{{end}}
  const columns = items.length > 0
    ? Object.keys(items[0]).slice(0, 5).map((k) => ({ key: k as keyof Item, label: k }))
    : [];

  return (
    <div>
      <div style={headerStyle}>
        <h1 style={h1Style}>{{.Name}}</h1>
{{- if .CreateOp}}
        <button onClick={() => setShowForm(!showForm)} style={{ob}} padding: '0.5rem 1rem', background: '#1a1a2e', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' {{cb}}>
          {showForm ? 'Cancel' : '+ New'}
        </button>
{{- end}}
      </div>
      {error && <div style={errStyle}>{error}</div>}
{{- if .CreateOp}}
      {showForm && (
        <div style={{ob}} background: '#f9f9f9', padding: '1.5rem', borderRadius: 8, marginBottom: '1.5rem', border: '1px solid #eee' {{cb}}>
          <h2 style={{ob}} marginTop: 0, fontSize: '1.1rem' {{cb}}>New {{.Name}}</h2>
          <form onSubmit={handleCreate}>
{{- range .FormFields}}
            <FormField
              name="{{.Name}}"
              label="{{.Label}}"
              type="{{.Type}}"
              value={form.{{.Name}}}
              onChange={(name, value) => setForm((f) => ({ ...f, [name]: value }))}
              required={ {{- .Required -}} }
{{- if .Options}}
              options={[{{range .Options}}'{{.}}', {{end}}]}
{{- end}}
            />
{{- end}}
            <button type="submit" style={{ob}} padding: '0.5rem 1.5rem', background: '#1a1a2e', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' {{cb}}>Create</button>
          </form>
        </div>
      )}
{{- end}}
      {loading ? (
        <div style={loadingStyle}>Loading...</div>
      ) : (
        <DataTable columns={columns} rows={items} onSelect={(row) => setSelected(row)} />
      )}
      {selected && (
        <div style={detailBoxStyle}>
          <div style={detailHeadStyle}>
            <h2 style={detailTitleStyle}>Detail</h2>
            <div style={detailBtnsStyle}>
{{- if .DeleteOp}}
              <button onClick={() => handleDelete(selected)} style={deleteBtnStyle}>Delete</button>
{{- end}}
              <button onClick={() => setSelected(null)} style={closeBtnStyle}>Close</button>
            </div>
          </div>
          <pre style={preStyle}>{JSON.stringify(selected, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}
`,
}
