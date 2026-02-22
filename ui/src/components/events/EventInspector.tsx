import { useEffect, useState, useCallback } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { WorkflowEventEntry } from '../../types/observability.ts';

const LEVEL_COLORS: Record<string, string> = {
  event: '#89b4fa',
  info: '#a6e3a1',
  warn: '#f9e2af',
  error: '#f38ba8',
  fatal: '#cba6f7',
  debug: '#6c7086',
};

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString();
}

function EventCard({
  event,
  isExpanded,
  onToggle,
}: {
  event: WorkflowEventEntry;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const color = LEVEL_COLORS[event.level] || '#6c7086';
  const idStr = String(event.id).slice(0, 8);

  let parsedFields: Record<string, unknown> | null = null;
  if (event.fields) {
    try {
      parsedFields = JSON.parse(event.fields);
    } catch {
      // ignore malformed fields
    }
  }

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
          {event.level.toUpperCase()}
        </span>
        <span style={{ color: '#cdd6f4', fontSize: 12, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {event.message}
        </span>
        {event.module_name && (
          <span style={{ color: '#89b4fa', fontSize: 11, fontFamily: 'monospace' }}>
            [{event.module_name}]
          </span>
        )}
        <span style={{ color: '#6c7086', fontSize: 11, fontFamily: 'monospace' }}>
          #{idStr}
        </span>
        <span style={{ color: '#6c7086', fontSize: 11 }}>{formatTime(event.created_at)}</span>
        <span style={{ color: '#6c7086', fontSize: 12 }}>{isExpanded ? '\u25B2' : '\u25BC'}</span>
      </div>
      {isExpanded && (
        <div style={{ borderTop: '1px solid #45475a', padding: 12, background: '#181825' }}>
          {event.execution_id && (
            <div style={{ marginBottom: 8 }}>
              <div style={{ color: '#a6adc8', fontSize: 11, marginBottom: 4 }}>Execution ID</div>
              <code style={{ color: '#89b4fa', fontSize: 11 }}>{event.execution_id}</code>
            </div>
          )}
          {parsedFields && Object.keys(parsedFields).length > 0 && (
            <div style={{ marginBottom: 8 }}>
              <div style={{ color: '#a6adc8', fontSize: 11, marginBottom: 4 }}>Fields</div>
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
                {JSON.stringify(parsedFields, null, 2)}
              </pre>
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

  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [messageFilter, setMessageFilter] = useState('');

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

  const filteredEvents = events.filter((e) => {
    if (messageFilter && !e.message.toLowerCase().includes(messageFilter.toLowerCase())) return false;
    return true;
  });

  // Level summary counts
  const levelCounts: Record<string, number> = {};
  for (const e of events) {
    levelCounts[e.level] = (levelCounts[e.level] || 0) + 1;
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

        {/* Message filter */}
        <input
          type="text"
          value={messageFilter}
          onChange={(e) => setMessageFilter(e.target.value)}
          placeholder="Filter events..."
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            padding: '4px 8px',
            fontSize: 12,
            outline: 'none',
            width: 180,
          }}
        />

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
          {/* Level summary cards */}
          <div style={{ display: 'flex', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
            {Object.entries(levelCounts).map(([level, count]) => (
              <div
                key={level}
                style={{
                  background: '#313244',
                  borderRadius: 8,
                  padding: '8px 16px',
                  borderLeft: `3px solid ${LEVEL_COLORS[level] || '#6c7086'}`,
                }}
              >
                <div style={{ fontSize: 18, fontWeight: 700, color: '#cdd6f4' }}>{count}</div>
                <div style={{ fontSize: 10, color: '#a6adc8', textTransform: 'capitalize' }}>{level}</div>
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
