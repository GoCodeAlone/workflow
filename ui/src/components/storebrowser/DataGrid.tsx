import React from 'react';

interface DataGridProps {
  columns: string[];
  rows: Array<Record<string, unknown>>;
  loading?: boolean;
  page?: number;
  total?: number;
  pageSize?: number;
  onPageChange?: (page: number) => void;
  onRowClick?: (row: Record<string, unknown>, index: number) => void;
  selectedRowIndex?: number;
}

const cellStyle: React.CSSProperties = {
  padding: '6px 12px',
  textAlign: 'left',
  whiteSpace: 'nowrap',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  maxWidth: 300,
  borderBottom: '1px solid #313244',
};

export default function DataGrid({
  columns,
  rows,
  loading,
  page,
  total,
  pageSize = 50,
  onPageChange,
  onRowClick,
  selectedRowIndex,
}: DataGridProps) {
  const totalPages = total != null ? Math.max(1, Math.ceil(total / pageSize)) : undefined;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
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
                    ...cellStyle,
                    position: 'sticky',
                    top: 0,
                    background: '#181825',
                    color: '#89b4fa',
                    fontWeight: 600,
                    fontSize: 11,
                    textTransform: 'uppercase',
                    letterSpacing: '0.5px',
                    zIndex: 1,
                  }}
                >
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && rows.length === 0 && (
              <tr>
                <td
                  colSpan={columns.length}
                  style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}
                >
                  Loading...
                </td>
              </tr>
            )}
            {!loading && rows.length === 0 && (
              <tr>
                <td
                  colSpan={columns.length}
                  style={{ padding: 24, textAlign: 'center', color: '#6c7086' }}
                >
                  No data.
                </td>
              </tr>
            )}
            {rows.map((row, i) => (
              <tr
                key={i}
                onClick={() => onRowClick?.(row, i)}
                style={{
                  background: selectedRowIndex === i ? '#313244' : i % 2 === 0 ? '#1e1e2e' : '#181825',
                  cursor: onRowClick ? 'pointer' : undefined,
                }}
              >
                {columns.map((col) => {
                  const val = row[col];
                  const display = val == null ? '' : typeof val === 'object' ? JSON.stringify(val) : String(val);
                  return (
                    <td key={col} style={{ ...cellStyle, color: '#cdd6f4' }} title={display}>
                      {display}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages != null && totalPages > 1 && page != null && onPageChange && (
        <div
          style={{
            padding: '8px 12px',
            borderTop: '1px solid #313244',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            background: '#181825',
            fontSize: 12,
            color: '#a6adc8',
            flexShrink: 0,
          }}
        >
          <span>
            Page {page} of {totalPages} ({total} rows)
          </span>
          <div style={{ display: 'flex', gap: 4 }}>
            <button
              disabled={page <= 1}
              onClick={() => onPageChange(page - 1)}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 4,
                color: page <= 1 ? '#45475a' : '#cdd6f4',
                padding: '3px 10px',
                fontSize: 11,
                cursor: page <= 1 ? 'not-allowed' : 'pointer',
              }}
            >
              Prev
            </button>
            <button
              disabled={page >= totalPages}
              onClick={() => onPageChange(page + 1)}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 4,
                color: page >= totalPages ? '#45475a' : '#cdd6f4',
                padding: '3px 10px',
                fontSize: 11,
                cursor: page >= totalPages ? 'not-allowed' : 'pointer',
              }}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
