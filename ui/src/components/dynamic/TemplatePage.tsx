import { useEffect, useReducer } from 'react';
import type { UIPageDef } from '../../store/pluginStore.ts';

// ---------------------------------------------------------------------------
// Shared types
// ---------------------------------------------------------------------------

interface Column {
  key: string;
  label: string;
  sortable?: boolean;
}

interface TemplatePageProps {
  page: UIPageDef;
}

// ---------------------------------------------------------------------------
// DataTable template — renders JSON array data as a sortable table
// ---------------------------------------------------------------------------

function DataTable({ data, columns }: { data: Record<string, unknown>[]; columns: Column[] }) {
  const [sortKey, setSortKey] = useState<string | null>(null);
  const [sortAsc, setSortAsc] = useState(true);

  const handleSort = useCallback(
    (key: string) => {
      if (sortKey === key) {
        setSortAsc(!sortAsc);
      } else {
        setSortKey(key);
        setSortAsc(true);
      }
    },
    [sortKey, sortAsc],
  );

  const sorted = [...data].sort((a, b) => {
    if (!sortKey) return 0;
    const av = String(a[sortKey] ?? '');
    const bv = String(b[sortKey] ?? '');
    return sortAsc ? av.localeCompare(bv) : bv.localeCompare(av);
  });

  return (
    <div style={{ overflowX: 'auto' }}>
      <table
        style={{
          width: '100%',
          borderCollapse: 'collapse',
          fontSize: 13,
          color: '#cdd6f4',
        }}
      >
        <thead>
          <tr>
            {columns.map((col) => (
              <th
                key={col.key}
                onClick={col.sortable !== false ? () => handleSort(col.key) : undefined}
                style={{
                  textAlign: 'left',
                  padding: '8px 12px',
                  borderBottom: '1px solid #45475a',
                  cursor: col.sortable !== false ? 'pointer' : 'default',
                  userSelect: 'none',
                  fontWeight: 600,
                  color: '#89b4fa',
                }}
              >
                {col.label}
                {sortKey === col.key && (sortAsc ? ' \u25B2' : ' \u25BC')}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((row, i) => (
            <tr
              key={i}
              style={{ background: i % 2 === 0 ? 'transparent' : 'rgba(69,71,90,0.2)' }}
            >
              {columns.map((col) => (
                <td key={col.key} style={{ padding: '6px 12px', borderBottom: '1px solid #313244' }}>
                  {String(row[col.key] ?? '')}
                </td>
              ))}
            </tr>
          ))}
          {sorted.length === 0 && (
            <tr>
              <td
                colSpan={columns.length}
                style={{ padding: 16, textAlign: 'center', color: '#585b70' }}
              >
                No data available
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

// ---------------------------------------------------------------------------
// ChartDashboard template — renders simple metric cards from JSON data
// ---------------------------------------------------------------------------

function ChartDashboard({ data }: { data: Record<string, unknown> }) {
  const entries = Object.entries(data);
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16 }}>
      {entries.map(([key, value]) => (
        <div
          key={key}
          style={{
            background: '#1e1e2e',
            border: '1px solid #313244',
            borderRadius: 8,
            padding: '16px 24px',
            minWidth: 160,
            flex: '1 1 200px',
          }}
        >
          <div style={{ fontSize: 11, color: '#585b70', textTransform: 'uppercase', marginBottom: 4 }}>
            {key.replace(/_/g, ' ')}
          </div>
          <div style={{ fontSize: 24, fontWeight: 700, color: '#cdd6f4' }}>
            {typeof value === 'number' ? value.toLocaleString() : String(value ?? '-')}
          </div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// DetailView template — renders key/value pairs
// ---------------------------------------------------------------------------

function DetailView({ data }: { data: Record<string, unknown> }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {Object.entries(data).map(([key, value]) => (
        <div key={key} style={{ display: 'flex', gap: 12, fontSize: 13 }}>
          <span style={{ color: '#89b4fa', fontWeight: 600, minWidth: 140 }}>
            {key.replace(/_/g, ' ')}
          </span>
          <span style={{ color: '#cdd6f4' }}>
            {typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value ?? '-')}
          </span>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// TemplatePage — fetches data from apiEndpoint and renders using template
// ---------------------------------------------------------------------------

export default function TemplatePage({ page }: TemplatePageProps) {
  type FetchState = { data: unknown; loading: boolean; error: string | null };
  type FetchAction =
    | { type: 'start' }
    | { type: 'success'; data: unknown }
    | { type: 'error'; error: string };

  const [state, dispatch] = useReducer(
    (prev: FetchState, action: FetchAction): FetchState => {
      switch (action.type) {
        case 'start': return { ...prev, loading: true, error: null };
        case 'success': return { data: action.data, loading: false, error: null };
        case 'error': return { ...prev, loading: false, error: action.error };
      }
    },
    { data: null, loading: false, error: null },
  );
  const { data, loading, error } = state;

  useEffect(() => {
    if (!page.apiEndpoint) return;
    dispatch({ type: 'start' });

    const token = localStorage.getItem('auth_token');
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;

    fetch(page.apiEndpoint, { headers })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((d) => dispatch({ type: 'success', data: d }))
      .catch((e) => dispatch({ type: 'error', error: e instanceof Error ? e.message : 'Failed to fetch data' }));
  }, [page.apiEndpoint]);

  if (loading) {
    return (
      <div style={{ padding: 24, color: '#585b70' }}>Loading...</div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 24, color: '#f38ba8' }}>
        Error loading data: {error}
      </div>
    );
  }

  if (!page.apiEndpoint) {
    return (
      <div style={{ padding: 24, color: '#585b70' }}>
        No data source configured for this page.
      </div>
    );
  }

  if (!data) return null;

  // Render based on template type
  const template = page.template || 'detail-view';

  return (
    <div style={{ padding: 24 }}>
      <h2 style={{ color: '#cdd6f4', fontSize: 18, fontWeight: 600, marginBottom: 16 }}>
        {page.icon} {page.label}
      </h2>

      {template === 'data-table' && Array.isArray(data) && data.length > 0 && (
        <DataTable
          data={data as Record<string, unknown>[]}
          columns={Object.keys(data[0] as Record<string, unknown>).map((key) => ({
            key,
            label: key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
            sortable: true,
          }))}
        />
      )}

      {template === 'chart-dashboard' && typeof data === 'object' && !Array.isArray(data) && (
        <ChartDashboard data={data as Record<string, unknown>} />
      )}

      {template === 'detail-view' && typeof data === 'object' && !Array.isArray(data) && (
        <DetailView data={data as Record<string, unknown>} />
      )}

      {template === 'data-table' && Array.isArray(data) && data.length === 0 && (
        <div style={{ color: '#585b70', padding: 16 }}>No records found.</div>
      )}
    </div>
  );
}
