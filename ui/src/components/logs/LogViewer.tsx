import { useEffect, useRef, useState, useCallback } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { LogLevel } from '../../types/observability.ts';

const LEVEL_COLORS: Record<string, string> = {
  debug: '#6c7086',
  info: '#89b4fa',
  warn: '#f9e2af',
  error: '#f38ba8',
  fatal: '#f38ba8',
};

const LEVELS: LogLevel[] = ['debug', 'info', 'warn', 'error', 'fatal'];

function formatLogTime(iso: string): string {
  const d = new Date(iso);
  return d.toISOString().replace('T', ' ').slice(0, 23);
}

export default function LogViewer() {
  const {
    logEntries,
    logStreaming,
    logFilter,
    selectedWorkflowId,
    fetchLogs,
    setLogFilter,
    startLogStream,
    stopLogStream,
  } = useObservabilityStore();

  const [search, setSearch] = useState('');
  const [levelFilter, setLevelFilter] = useState<string>('');
  const [autoScroll, setAutoScroll] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);

  // Load initial logs
  useEffect(() => {
    if (selectedWorkflowId) {
      fetchLogs(selectedWorkflowId, logFilter);
    }
  }, [selectedWorkflowId, logFilter, fetchLogs]);

  // Auto-scroll
  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logEntries, autoScroll]);

  const handleScroll = useCallback(() => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    const atBottom = scrollHeight - scrollTop - clientHeight < 40;
    setAutoScroll(atBottom);
  }, []);

  const handleToggleStream = useCallback(() => {
    if (logStreaming) {
      stopLogStream();
    } else if (selectedWorkflowId) {
      startLogStream(selectedWorkflowId);
    }
  }, [logStreaming, selectedWorkflowId, startLogStream, stopLogStream]);

  const handleLevelFilter = useCallback(
    (level: string) => {
      const newLevel = levelFilter === level ? '' : level;
      setLevelFilter(newLevel);
      setLogFilter({ ...logFilter, level: newLevel || undefined });
    },
    [levelFilter, logFilter, setLogFilter],
  );

  const filteredLogs = logEntries.filter((log) => {
    if (search) {
      const lower = search.toLowerCase();
      if (
        !log.message.toLowerCase().includes(lower) &&
        !(log.module_name || '').toLowerCase().includes(lower)
      ) {
        return false;
      }
    }
    return true;
  });

  const handleExport = useCallback(() => {
    const data = JSON.stringify(filteredLogs, null, 2);
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `logs-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }, [filteredLogs]);

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
        <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 16, fontWeight: 600, marginRight: 12 }}>Logs</h2>

        {/* Level filters */}
        {LEVELS.map((level) => (
          <button
            key={level}
            onClick={() => handleLevelFilter(level)}
            style={{
              background: levelFilter === level ? LEVEL_COLORS[level] + '33' : '#313244',
              color: levelFilter === level ? LEVEL_COLORS[level] : '#a6adc8',
              border: `1px solid ${levelFilter === level ? LEVEL_COLORS[level] : '#45475a'}`,
              borderRadius: 4,
              padding: '3px 8px',
              fontSize: 11,
              cursor: 'pointer',
              fontWeight: levelFilter === level ? 600 : 400,
              textTransform: 'uppercase',
            }}
          >
            {level}
          </button>
        ))}

        {/* Search */}
        <input
          type="text"
          placeholder="Search logs..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#cdd6f4',
            padding: '4px 8px',
            fontSize: 12,
            outline: 'none',
            width: 180,
            marginLeft: 'auto',
          }}
        />

        {/* Stream toggle */}
        <button
          onClick={handleToggleStream}
          style={{
            background: logStreaming ? '#a6e3a122' : '#313244',
            color: logStreaming ? '#a6e3a1' : '#a6adc8',
            border: `1px solid ${logStreaming ? '#a6e3a1' : '#45475a'}`,
            borderRadius: 4,
            padding: '4px 10px',
            fontSize: 11,
            cursor: 'pointer',
            fontWeight: 600,
          }}
        >
          {logStreaming ? 'Streaming' : 'Stream'}
        </button>

        {/* Export */}
        <button
          onClick={handleExport}
          style={{
            background: '#313244',
            border: '1px solid #45475a',
            borderRadius: 4,
            color: '#a6adc8',
            padding: '4px 10px',
            fontSize: 11,
            cursor: 'pointer',
          }}
        >
          Export
        </button>
      </div>

      {!selectedWorkflowId && (
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#6c7086' }}>
          Select a workflow from the Dashboard to view logs.
        </div>
      )}

      {/* Log display */}
      {selectedWorkflowId && (
        <div
          ref={containerRef}
          onScroll={handleScroll}
          style={{
            flex: 1,
            overflow: 'auto',
            background: '#0d1117',
            fontFamily: 'monospace',
            fontSize: 12,
            lineHeight: '20px',
            padding: '8px 0',
          }}
        >
          {filteredLogs.map((log) => {
            const color = LEVEL_COLORS[log.level] || '#cdd6f4';
            const isFatal = log.level === 'fatal';
            return (
              <div
                key={log.id}
                style={{
                  padding: '1px 12px',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  fontWeight: isFatal ? 700 : 400,
                }}
              >
                <span style={{ color: '#6c7086' }}>[{formatLogTime(log.created_at)}]</span>{' '}
                <span style={{ color, fontWeight: 600 }}>[{log.level.toUpperCase().padEnd(5)}]</span>{' '}
                {log.module_name && <span style={{ color: '#fab387' }}>[{log.module_name}]</span>}{' '}
                <span style={{ color: '#cdd6f4' }}>{log.message}</span>
              </div>
            );
          })}
          {filteredLogs.length === 0 && (
            <div style={{ padding: 20, color: '#6c7086', textAlign: 'center' }}>
              {logEntries.length === 0 ? 'No log entries.' : 'No logs match the current filter.'}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
