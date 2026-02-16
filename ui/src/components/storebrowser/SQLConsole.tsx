import { useState, useCallback } from 'react';
import useStoreBrowserStore from '../../store/storeBrowserStore.ts';
import DataGrid from './DataGrid.tsx';

export default function SQLConsole() {
  const {
    sqlQuery,
    sqlResults,
    sqlError,
    sqlHistory,
    sqlLoading,
    setSqlQuery,
    executeSql,
  } = useStoreBrowserStore();

  const [showHistory, setShowHistory] = useState(false);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
        e.preventDefault();
        executeSql();
      }
    },
    [executeSql],
  );

  const handleHistorySelect = useCallback(
    (query: string) => {
      setSqlQuery(query);
      setShowHistory(false);
    },
    [setSqlQuery],
  );

  const rows = sqlResults?.rows || [];
  // Derive columns from row keys as fallback when backend returns null columns.
  const columns = sqlResults?.columns?.length
    ? sqlResults.columns
    : rows.length > 0
      ? Object.keys(rows[0])
      : [];

  return (
    <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
      {/* Main area */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        {/* Query input */}
        <div
          style={{
            padding: 12,
            borderBottom: '1px solid #313244',
            background: '#181825',
            flexShrink: 0,
          }}
        >
          <textarea
            value={sqlQuery}
            onChange={(e) => setSqlQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="SELECT * FROM ..."
            spellCheck={false}
            style={{
              width: '100%',
              minHeight: 80,
              maxHeight: 200,
              background: '#11111b',
              border: '1px solid #313244',
              borderRadius: 4,
              color: '#cdd6f4',
              fontFamily: 'monospace',
              fontSize: 13,
              padding: 10,
              outline: 'none',
              resize: 'vertical',
              boxSizing: 'border-box',
            }}
          />
          <div style={{ display: 'flex', gap: 8, marginTop: 8, alignItems: 'center' }}>
            <button
              onClick={executeSql}
              disabled={sqlLoading || !sqlQuery.trim()}
              style={{
                background: '#89b4fa',
                border: 'none',
                borderRadius: 4,
                color: '#1e1e2e',
                padding: '6px 16px',
                fontSize: 12,
                fontWeight: 600,
                cursor: sqlLoading || !sqlQuery.trim() ? 'not-allowed' : 'pointer',
                opacity: sqlLoading || !sqlQuery.trim() ? 0.5 : 1,
              }}
            >
              {sqlLoading ? 'Executing...' : 'Execute'}
            </button>
            <span style={{ color: '#6c7086', fontSize: 11 }}>Ctrl+Enter to run</span>
            <button
              onClick={() => setShowHistory(!showHistory)}
              style={{
                marginLeft: 'auto',
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 4,
                color: showHistory ? '#89b4fa' : '#a6adc8',
                padding: '4px 10px',
                fontSize: 11,
                cursor: 'pointer',
              }}
            >
              History ({sqlHistory.length})
            </button>
          </div>
        </div>

        {/* Error */}
        {sqlError && (
          <div
            style={{
              padding: '8px 12px',
              background: '#f38ba811',
              borderBottom: '1px solid #f38ba844',
              color: '#f38ba8',
              fontSize: 12,
              fontFamily: 'monospace',
              flexShrink: 0,
            }}
          >
            {sqlError}
          </div>
        )}

        {/* Results */}
        {sqlResults && (
          <DataGrid columns={columns} rows={rows} loading={sqlLoading} />
        )}

        {!sqlResults && !sqlError && (
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
            Enter a SQL query and press Execute.
          </div>
        )}
      </div>

      {/* History sidebar */}
      {showHistory && (
        <div
          style={{
            width: 260,
            borderLeft: '1px solid #313244',
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
              borderBottom: '1px solid #313244',
            }}
          >
            Query History
          </div>
          {sqlHistory.length === 0 && (
            <div style={{ padding: 12, color: '#6c7086', fontSize: 12 }}>No history yet.</div>
          )}
          {[...sqlHistory].reverse().map((q, i) => (
            <div
              key={i}
              onClick={() => handleHistorySelect(q)}
              style={{
                padding: '8px 12px',
                borderBottom: '1px solid #313244',
                cursor: 'pointer',
                fontSize: 11,
                fontFamily: 'monospace',
                color: '#cdd6f4',
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}
              title={q}
            >
              {q}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
