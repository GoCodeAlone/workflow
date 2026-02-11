import { useEffect, useState, useCallback } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { WorkflowExecution } from '../../types/observability.ts';

const STATUS_COLORS: Record<string, string> = {
  pending: '#6c7086',
  running: '#89b4fa',
  completed: '#a6e3a1',
  failed: '#f38ba8',
  cancelled: '#f9e2af',
};

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString();
}

function EventCard({
  event,
  isExpanded,
  onToggle,
}: {
  event: WorkflowExecution;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const color = STATUS_COLORS[event.status] || '#6c7086';

  return (
    <div
      style={{
        background: '#313244',
        border: '1px solid #45475a',
        borderRadius: 8,
        marginBottom: 8,
        overflow: 'hidden',
      }}
    >
      <div
        onClick={onToggle}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '10px 14px',
          cursor: 'pointer',
        }}
      >
        <span
          style={{
            display: 'inline-block',
            padding: '2px 8px',
            borderRadius: 4,
            fontSize: 10,
            fontWeight: 600,
            background: color + '22',
            color,
          }}
        >
          {event.status}
        </span>
        <span style={{ color: '#a6adc8', fontSize: 12 }}>{event.trigger_type}</span>
        <span style={{ color: '#6c7086', fontSize: 11, fontFamily: 'monospace' }}>
          {event.id.slice(0, 8)}
        </span>
        <span style={{ flex: 1 }} />
        <span style={{ color: '#6c7086', fontSize: 11 }}>{formatTime(event.started_at)}</span>
        <span style={{ color: '#6c7086', fontSize: 12 }}>{isExpanded ? '\u25B2' : '\u25BC'}</span>
      </div>
      {isExpanded && (
        <div style={{ borderTop: '1px solid #45475a', padding: 12, background: '#181825' }}>
          {event.trigger_data != null && (
            <div style={{ marginBottom: 8 }}>
              <div style={{ color: '#a6adc8', fontSize: 11, marginBottom: 4 }}>Trigger Data</div>
              <pre
                style={{
                  background: '#0d1117',
                  padding: 8,
                  borderRadius: 4,
                  color: '#a6e3a1',
                  fontSize: 11,
                  fontFamily: 'monospace',
                  overflow: 'auto',
                  maxHeight: 200,
                  margin: 0,
                }}
              >
                {JSON.stringify(event.trigger_data, null, 2)}
              </pre>
            </div>
          )}
          {event.output_data != null && (
            <div style={{ marginBottom: 8 }}>
              <div style={{ color: '#a6adc8', fontSize: 11, marginBottom: 4 }}>Output Data</div>
              <pre
                style={{
                  background: '#0d1117',
                  padding: 8,
                  borderRadius: 4,
                  color: '#a6e3a1',
                  fontSize: 11,
                  fontFamily: 'monospace',
                  overflow: 'auto',
                  maxHeight: 200,
                  margin: 0,
                }}
              >
                {JSON.stringify(event.output_data, null, 2)}
              </pre>
            </div>
          )}
          {event.error_message && (
            <div>
              <div style={{ color: '#f38ba8', fontSize: 11, marginBottom: 4 }}>Error</div>
              <div style={{ color: '#f38ba8', fontSize: 12 }}>{event.error_message}</div>
              {event.error_stack && (
                <pre
                  style={{
                    background: '#0d1117',
                    padding: 8,
                    borderRadius: 4,
                    color: '#f38ba8',
                    fontSize: 10,
                    fontFamily: 'monospace',
                    overflow: 'auto',
                    maxHeight: 120,
                    margin: '4px 0 0',
                  }}
                >
                  {event.error_stack}
                </pre>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function EventInspector() {
  const {
    events,
    eventStreaming,
    selectedWorkflowId,
    fetchEvents,
    startEventStream,
    stopEventStream,
  } = useObservabilityStore();

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [typeFilter, setTypeFilter] = useState('');

  useEffect(() => {
    if (selectedWorkflowId) {
      fetchEvents(selectedWorkflowId);
    }
  }, [selectedWorkflowId, fetchEvents]);

  const handleToggleStream = useCallback(() => {
    if (eventStreaming) {
      stopEventStream();
    } else if (selectedWorkflowId) {
      startEventStream(selectedWorkflowId);
    }
  }, [eventStreaming, selectedWorkflowId, startEventStream, stopEventStream]);

  // Derive unique trigger types for filter
  const triggerTypes = [...new Set(events.map((e) => e.trigger_type))];

  const filteredEvents = events.filter((e) => {
    if (typeFilter && e.trigger_type !== typeFilter) return false;
    return true;
  });

  // Status summary
  const statusCounts: Record<string, number> = {};
  for (const e of events) {
    statusCounts[e.status] = (statusCounts[e.status] || 0) + 1;
  }

  return (
    <div style={{ flex: 1, background: '#1e1e2e', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* Header */}
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          flexWrap: 'wrap',
        }}
      >
        <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 16, fontWeight: 600, marginRight: 12 }}>Events</h2>

        {/* Type filter */}
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            padding: '4px 8px',
            fontSize: 12,
            outline: 'none',
          }}
        >
          <option value="">All Types</option>
          {triggerTypes.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>

        <span style={{ flex: 1 }} />

        {/* Stream toggle */}
        <button
          onClick={handleToggleStream}
          style={{
            background: eventStreaming ? '#a6e3a122' : '#313244',
            color: eventStreaming ? '#a6e3a1' : '#a6adc8',
            border: `1px solid ${eventStreaming ? '#a6e3a1' : '#45475a'}`,
            borderRadius: 4,
            padding: '4px 10px',
            fontSize: 11,
            cursor: 'pointer',
            fontWeight: 600,
          }}
        >
          {eventStreaming ? 'Live' : 'Stream'}
        </button>
      </div>

      {!selectedWorkflowId && (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#6c7086' }}>
          Select a workflow from the Dashboard to view events.
        </div>
      )}

      {selectedWorkflowId && (
        <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
          {/* Status summary cards */}
          <div style={{ display: 'flex', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
            {Object.entries(statusCounts).map(([status, count]) => (
              <div
                key={status}
                style={{
                  background: '#313244',
                  borderRadius: 8,
                  padding: '8px 16px',
                  borderLeft: `3px solid ${STATUS_COLORS[status] || '#6c7086'}`,
                }}
              >
                <div style={{ fontSize: 18, fontWeight: 700, color: '#cdd6f4' }}>{count}</div>
                <div style={{ fontSize: 10, color: '#a6adc8', textTransform: 'capitalize' }}>{status}</div>
              </div>
            ))}
            <div
              style={{
                background: '#313244',
                borderRadius: 8,
                padding: '8px 16px',
                borderLeft: '3px solid #fab387',
              }}
            >
              <div style={{ fontSize: 18, fontWeight: 700, color: '#cdd6f4' }}>{events.length}</div>
              <div style={{ fontSize: 10, color: '#a6adc8' }}>Total</div>
            </div>
          </div>

          {/* Event list */}
          {filteredEvents.map((event) => (
            <EventCard
              key={event.id}
              event={event}
              isExpanded={expandedId === event.id}
              onToggle={() => setExpandedId(expandedId === event.id ? null : event.id)}
            />
          ))}
          {filteredEvents.length === 0 && (
            <div style={{ color: '#6c7086', textAlign: 'center', padding: 40 }}>No events found.</div>
          )}
        </div>
      )}
    </div>
  );
}
