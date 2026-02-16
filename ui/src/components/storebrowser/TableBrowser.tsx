import { useEffect } from 'react';
import useStoreBrowserStore from '../../store/storeBrowserStore.ts';
import DataGrid from './DataGrid.tsx';

export default function TableBrowser() {
  const {
    tables,
    selectedTable,
    tableSchema,
    tableRows,
    tablePage,
    tableTotal,
    tableLoading,
    fetchTables,
    selectTable,
    fetchTableRows,
  } = useStoreBrowserStore();

  useEffect(() => {
    fetchTables();
  }, [fetchTables]);

  const columns = tableSchema.length > 0
    ? tableSchema.map((c) => c.name)
    : tableRows.length > 0
      ? Object.keys(tableRows[0])
      : [];

  return (
    <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
      {/* Sidebar */}
      <div
        style={{
          width: 200,
          borderRight: '1px solid #313244',
          background: '#181825',
          overflow: 'auto',
          flexShrink: 0,
        }}
      >
        <div
          style={{
            padding: '10px 12px',
            fontSize: 11,
            color: '#6c7086',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
            fontWeight: 600,
          }}
        >
          Tables ({tables.length})
        </div>
        {tables.map((t) => (
          <div
            key={t}
            onClick={() => selectTable(t)}
            style={{
              padding: '6px 12px',
              fontSize: 12,
              color: selectedTable === t ? '#89b4fa' : '#cdd6f4',
              background: selectedTable === t ? '#313244' : 'transparent',
              cursor: 'pointer',
              borderLeft: selectedTable === t ? '2px solid #89b4fa' : '2px solid transparent',
              fontFamily: 'monospace',
            }}
          >
            {t}
          </div>
        ))}
        {tables.length === 0 && !tableLoading && (
          <div style={{ padding: '12px', color: '#6c7086', fontSize: 12 }}>No tables found.</div>
        )}
      </div>

      {/* Main content */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        {!selectedTable && (
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#6c7086',
              fontSize: 13,
            }}
          >
            Select a table to browse its contents.
          </div>
        )}

        {selectedTable && (
          <>
            {/* Schema info */}
            {tableSchema.length > 0 && (
              <div
                style={{
                  padding: '8px 12px',
                  borderBottom: '1px solid #313244',
                  background: '#181825',
                  display: 'flex',
                  gap: 12,
                  flexWrap: 'wrap',
                  fontSize: 11,
                }}
              >
                {tableSchema.map((col) => (
                  <span key={col.name} style={{ color: '#a6adc8' }}>
                    <span style={{ color: '#cdd6f4', fontWeight: 600, fontFamily: 'monospace' }}>
                      {col.name}
                    </span>
                    <span style={{ color: '#6c7086', marginLeft: 4 }}>{col.type}</span>
                    {col.pk && (
                      <span
                        style={{
                          marginLeft: 4,
                          color: '#f9e2af',
                          fontSize: 9,
                          fontWeight: 600,
                        }}
                      >
                        PK
                      </span>
                    )}
                    {col.notnull && (
                      <span
                        style={{
                          marginLeft: 4,
                          color: '#f38ba8',
                          fontSize: 9,
                          fontWeight: 600,
                        }}
                      >
                        NOT NULL
                      </span>
                    )}
                  </span>
                ))}
              </div>
            )}

            <DataGrid
              columns={columns}
              rows={tableRows}
              loading={tableLoading}
              page={tablePage}
              total={tableTotal}
              pageSize={50}
              onPageChange={(p) => fetchTableRows(p)}
            />
          </>
        )}
      </div>
    </div>
  );
}
