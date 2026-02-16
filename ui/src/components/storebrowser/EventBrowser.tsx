import { useEffect, useState } from 'react';
import useStoreBrowserStore from '../../store/storeBrowserStore.ts';
import DataGrid from './DataGrid.tsx';

const inputStyle: React.CSSProperties = {
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 4,
  color: '#cdd6f4',
  padding: '4px 8px',
  fontSize: 12,
  outline: 'none',
};

const EVENT_TYPES = ['', 'workflow.started', 'workflow.completed', 'workflow.failed', 'step.started', 'step.completed', 'step.failed'];

export default function EventBrowser() {
  const {
    events,
    eventFilters,
    eventsLoading,
    fetchEvents,
    setEventFilter,
  } = useStoreBrowserStore();

  const [expandedRow, setExpandedRow] = useState<number | null>(null);

  useEffect(() => {
    fetchEvents();
  }, [fetchEvents]);

  const handleApplyFilters = () => {
    fetchEvents();
  };

  const columns = ['id', 'execution_id', 'event_type', 'created_at'];

  const rows = events.map((e) => ({
    id: e.id,
    execution_id: e.execution_id,
    event_type: e.event_type,
    created_at: e.created_at,
    _event_data: e.event_data,
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
        <input
          type="text"
          placeholder="Execution ID"
          value={eventFilters.execution_id || ''}
          onChange={(e) => setEventFilter('execution_id', e.target.value)}
          style={{ ...inputStyle, width: 200 }}
        />
        <select
          value={eventFilters.event_type || ''}
          onChange={(e) => setEventFilter('event_type', e.target.value)}
          style={inputStyle}
        >
          <option value="">All Event Types</option>
          {EVENT_TYPES.filter(Boolean).map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>
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

      {/* Grid */}
      <DataGrid
        columns={columns}
        rows={rows}
        loading={eventsLoading}
        onRowClick={(_row, i) => setExpandedRow(expandedRow === i ? null : i)}
        selectedRowIndex={expandedRow ?? undefined}
      />

      {/* Expanded event data */}
      {expandedRow != null && rows[expandedRow] && (
        <div
          style={{
            borderTop: '1px solid #313244',
            background: '#181825',
            padding: 12,
            maxHeight: 200,
            overflow: 'auto',
            flexShrink: 0,
          }}
        >
          <div
            style={{
              fontSize: 11,
              color: '#6c7086',
              marginBottom: 6,
              fontWeight: 600,
              textTransform: 'uppercase',
            }}
          >
            Event Data
          </div>
          <pre
            style={{
              margin: 0,
              fontFamily: 'monospace',
              fontSize: 12,
              color: '#cdd6f4',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}
          >
            {JSON.stringify(rows[expandedRow]._event_data, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
