import { useEffect } from 'react';
import useStoreBrowserStore from '../../store/storeBrowserStore.ts';

const inputStyle: React.CSSProperties = {
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 4,
  color: '#cdd6f4',
  padding: '4px 8px',
  fontSize: 12,
  outline: 'none',
};

const STATUS_COLORS: Record<string, string> = {
  pending: '#f9e2af',
  retrying: '#89b4fa',
  resolved: '#a6e3a1',
  discarded: '#6c7086',
};

const DLQ_STATUSES = ['', 'pending', 'retrying', 'resolved', 'discarded'];

export default function DLQBrowser() {
  const {
    dlqEntries,
    dlqFilters,
    dlqLoading,
    fetchDLQ,
    setDLQFilter,
    retryDLQ,
    discardDLQ,
  } = useStoreBrowserStore();

  useEffect(() => {
    fetchDLQ();
  }, [fetchDLQ]);

  const handleApplyFilters = () => {
    fetchDLQ();
  };

  const columns = ['id', 'pipeline_name', 'step_name', 'error_message', 'status', 'retry_count', 'created_at', 'actions'];

  const rows = dlqEntries.map((e) => ({
    id: e.id,
    pipeline_name: e.pipeline_name,
    step_name: e.step_name,
    error_message: e.error_message,
    status: e.status,
    retry_count: e.retry_count,
    created_at: e.created_at,
    actions: e.status,
    _raw: e,
  }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
      {/* Filter bar */}
      <div
        style={{
          padding: '8px 12px',
          borderBottom: '1px solid #313244',
          background: '#181825',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          flexShrink: 0,
        }}
      >
        <span style={{ color: '#6c7086', fontSize: 11, fontWeight: 600 }}>Filters:</span>
        <select
          value={dlqFilters.status || ''}
          onChange={(e) => setDLQFilter('status', e.target.value)}
          style={inputStyle}
        >
          <option value="">All Statuses</option>
          {DLQ_STATUSES.filter(Boolean).map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <input
          type="text"
          placeholder="Pipeline name"
          value={dlqFilters.pipeline || ''}
          onChange={(e) => setDLQFilter('pipeline', e.target.value)}
          style={{ ...inputStyle, width: 180 }}
        />
        <button
          onClick={handleApplyFilters}
          style={{
            background: '#89b4fa',
            border: 'none',
            borderRadius: 4,
            color: '#1e1e2e',
            padding: '5px 12px',
            fontSize: 11,
            fontWeight: 600,
            cursor: 'pointer',
          }}
        >
          Apply
        </button>
      </div>

      {/* Custom table to support status badges and action buttons */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        <table
          style={{
            width: '100%',
            borderCollapse: 'collapse',
            fontSize: 12,
            fontFamily: 'monospace',
          }}
        >
          <thead>
            <tr>
              {columns.map((col) => (
                <th
                  key={col}
                  style={{
                    padding: '6px 12px',
                    textAlign: 'left',
                    position: 'sticky',
                    top: 0,
                    background: '#181825',
                    color: '#89b4fa',
                    fontWeight: 600,
                    fontSize: 11,
                    textTransform: 'uppercase',
                    letterSpacing: '0.5px',
                    borderBottom: '1px solid #313244',
                    zIndex: 1,
                  }}
                >
                  {col === 'actions' ? '' : col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {dlqLoading && rows.length === 0 && (
              <tr>
                <td colSpan={columns.length} style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}>
                  Loading...
                </td>
              </tr>
            )}
            {!dlqLoading && rows.length === 0 && (
              <tr>
                <td colSpan={columns.length} style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}>
                  No DLQ entries.
                </td>
              </tr>
            )}
            {rows.map((row, i) => (
              <tr key={row.id} style={{ background: i % 2 === 0 ? '#1e1e2e' : '#181825' }}>
                {columns.map((col) => {
                  if (col === 'status') {
                    const statusColor = STATUS_COLORS[row.status] || '#a6adc8';
                    return (
                      <td
                        key={col}
                        style={{
                          padding: '6px 12px',
                          borderBottom: '1px solid #313244',
                        }}
                      >
                        <span
                          style={{
                            display: 'inline-block',
                            padding: '2px 8px',
                            borderRadius: 10,
                            fontSize: 10,
                            fontWeight: 600,
                            color: statusColor,
                            background: statusColor + '22',
                            border: `1px solid ${statusColor}44`,
                          }}
                        >
                          {row.status}
                        </span>
                      </td>
                    );
                  }
                  if (col === 'actions') {
                    const isPending = row.status === 'pending';
                    return (
                      <td
                        key={col}
                        style={{
                          padding: '6px 12px',
                          borderBottom: '1px solid #313244',
                        }}
                      >
                        {isPending && (
                          <div style={{ display: 'flex', gap: 4 }}>
                            <button
                              onClick={() => retryDLQ(row.id)}
                              style={{
                                background: '#89b4fa22',
                                border: '1px solid #89b4fa44',
                                borderRadius: 4,
                                color: '#89b4fa',
                                padding: '2px 8px',
                                fontSize: 10,
                                fontWeight: 600,
                                cursor: 'pointer',
                              }}
                            >
                              Retry
                            </button>
                            <button
                              onClick={() => discardDLQ(row.id)}
                              style={{
                                background: '#f38ba822',
                                border: '1px solid #f38ba844',
                                borderRadius: 4,
                                color: '#f38ba8',
                                padding: '2px 8px',
                                fontSize: 10,
                                fontWeight: 600,
                                cursor: 'pointer',
                              }}
                            >
                              Discard
                            </button>
                          </div>
                        )}
                      </td>
                    );
                  }
                  const val = row[col as keyof typeof row];
                  const display = val == null ? '' : String(val);
                  return (
                    <td
                      key={col}
                      style={{
                        padding: '6px 12px',
                        color: '#cdd6f4',
                        borderBottom: '1px solid #313244',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        maxWidth: 250,
                      }}
                      title={display}
                    >
                      {display}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
