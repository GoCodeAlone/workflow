import { useEffect, useState } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { ExecutionStep } from '../../types/observability.ts';

const STATUS_COLORS: Record<string, string> = {
  pending: '#6c7086',
  running: '#89b4fa',
  completed: '#a6e3a1',
  failed: '#f38ba8',
  cancelled: '#f9e2af',
  skipped: '#6c7086',
};

function StatusBadge({ status }: { status: string }) {
  const color = STATUS_COLORS[status] || '#6c7086';
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 10px',
        borderRadius: 12,
        fontSize: 11,
        fontWeight: 600,
        background: color + '22',
        color,
      }}
    >
      {status}
    </span>
  );
}

function formatDuration(ms?: number): string {
  if (ms == null) return '-';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function JsonViewer({ data, label }: { data: unknown; label: string }) {
  const [collapsed, setCollapsed] = useState(true);
  if (data == null) return null;

  const json = typeof data === 'string' ? data : JSON.stringify(data, null, 2);

  return (
    <div style={{ marginBottom: 8 }}>
      <button
        onClick={() => setCollapsed(!collapsed)}
        style={{
          background: 'none',
          border: 'none',
          color: '#a6adc8',
          fontSize: 11,
          cursor: 'pointer',
          padding: 0,
          fontWeight: 600,
        }}
      >
        {collapsed ? '\u25B6' : '\u25BC'} {label}
      </button>
      {!collapsed && (
        <pre
          style={{
            background: '#0d1117',
            padding: 8,
            borderRadius: 4,
            overflow: 'auto',
            maxHeight: 200,
            margin: '4px 0 0',
            fontFamily: 'monospace',
            fontSize: 11,
            lineHeight: '16px',
          }}
        >
          {json.split('\n').map((line, i) => {
            // Simple syntax coloring
            const colored = line
              .replace(/"([^"]+)":/g, '<key>"$1"</key>:')
              .replace(/: "([^"]*)"/g, ': <str>"$1"</str>')
              .replace(/: (\d+\.?\d*)/g, ': <num>$1</num>');
            return (
              <span key={i} dangerouslySetInnerHTML={{ __html: colored
                .replace(/<key>/g, '<span style="color:#89b4fa">')
                .replace(/<\/key>/g, '</span>')
                .replace(/<str>/g, '<span style="color:#a6e3a1">')
                .replace(/<\/str>/g, '</span>')
                .replace(/<num>/g, '<span style="color:#fab387">')
                .replace(/<\/num>/g, '</span>')
              }} />
            );
          }).reduce<React.ReactNode[]>((acc, el, i) => {
            if (i > 0) acc.push('\n');
            acc.push(el);
            return acc;
          }, [])}
        </pre>
      )}
    </div>
  );
}

function StepRow({ step }: { step: ExecutionStep }) {
  const [expanded, setExpanded] = useState(false);
  const isFailed = step.status === 'failed';

  return (
    <div
      style={{
        borderLeft: `3px solid ${STATUS_COLORS[step.status] || '#6c7086'}`,
        marginBottom: 8,
        paddingLeft: 12,
      }}
    >
      <div
        onClick={() => setExpanded(!expanded)}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          cursor: 'pointer',
          padding: '6px 0',
        }}
      >
        <span style={{ color: '#6c7086', fontSize: 11, fontFamily: 'monospace', minWidth: 20 }}>
          #{step.sequence_num}
        </span>
        <div
          style={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: STATUS_COLORS[step.status] || '#6c7086',
          }}
        />
        <span style={{ color: '#cdd6f4', fontSize: 13, fontWeight: 500 }}>{step.step_name}</span>
        <span
          style={{
            fontSize: 10,
            padding: '1px 6px',
            borderRadius: 4,
            background: '#45475a',
            color: '#a6adc8',
          }}
        >
          {step.step_type}
        </span>
        <span style={{ flex: 1 }} />
        <span style={{ color: '#a6adc8', fontSize: 11 }}>{formatDuration(step.duration_ms)}</span>
        <StatusBadge status={step.status} />
      </div>
      {isFailed && step.error_message && (
        <div
          style={{
            color: '#f38ba8',
            fontSize: 12,
            padding: '4px 0 4px 28px',
            borderTop: '1px solid #45475a44',
          }}
        >
          {step.error_message}
        </div>
      )}
      {expanded && (
        <div style={{ paddingLeft: 28, paddingBottom: 8 }}>
          <JsonViewer data={step.input_data} label="Input Data" />
          <JsonViewer data={step.output_data} label="Output Data" />
        </div>
      )}
    </div>
  );
}

export default function ExecutionDetail() {
  const { selectedExecution, executionSteps, fetchExecutionDetail, fetchExecutionSteps } =
    useObservabilityStore();

  const [activeTab, setActiveTab] = useState<'steps' | 'input' | 'output'>('steps');
  const [execIdInput, setExecIdInput] = useState('');

  const handleLoad = () => {
    if (execIdInput.trim()) {
      fetchExecutionDetail(execIdInput.trim());
      fetchExecutionSteps(execIdInput.trim());
    }
  };

  useEffect(() => {
    if (selectedExecution) {
      fetchExecutionSteps(selectedExecution.id);
    }
  }, [selectedExecution, fetchExecutionSteps]);

  const sortedSteps = [...executionSteps].sort((a, b) => a.sequence_num - b.sequence_num);

  return (
    <div style={{ flex: 1, background: '#1e1e2e', overflow: 'auto', padding: 24 }}>
      <h2 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 18, fontWeight: 600 }}>Execution Detail</h2>

      {!selectedExecution && (
        <div style={{ marginBottom: 16 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <input
              type="text"
              placeholder="Enter execution ID..."
              value={execIdInput}
              onChange={(e) => setExecIdInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleLoad()}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 12px',
                fontSize: 13,
                outline: 'none',
                width: 300,
                fontFamily: 'monospace',
              }}
            />
            <button
              onClick={handleLoad}
              style={{
                background: '#89b4fa',
                border: 'none',
                borderRadius: 6,
                color: '#1e1e2e',
                padding: '8px 16px',
                fontSize: 13,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Load
            </button>
          </div>
          <div style={{ color: '#6c7086', fontSize: 12, marginTop: 8 }}>
            Or click an execution from the Executions view.
          </div>
        </div>
      )}

      {selectedExecution && (
        <>
          {/* Header info */}
          <div
            style={{
              background: '#313244',
              borderRadius: 8,
              padding: 16,
              marginBottom: 16,
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
              gap: 12,
            }}
          >
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Execution ID</div>
              <div style={{ color: '#cdd6f4', fontSize: 13, fontFamily: 'monospace' }}>
                {selectedExecution.id}
              </div>
            </div>
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Workflow ID</div>
              <div style={{ color: '#cdd6f4', fontSize: 13, fontFamily: 'monospace' }}>
                {selectedExecution.workflow_id}
              </div>
            </div>
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Trigger</div>
              <div style={{ color: '#cdd6f4', fontSize: 13 }}>{selectedExecution.trigger_type}</div>
            </div>
            {selectedExecution.triggered_by && (
              <div>
                <div style={{ color: '#a6adc8', fontSize: 11 }}>Triggered By</div>
                <div style={{ color: '#cdd6f4', fontSize: 13 }}>{selectedExecution.triggered_by}</div>
              </div>
            )}
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Status</div>
              <StatusBadge status={selectedExecution.status} />
            </div>
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Duration</div>
              <div style={{ color: '#cdd6f4', fontSize: 13 }}>
                {formatDuration(selectedExecution.duration_ms)}
              </div>
            </div>
            <div>
              <div style={{ color: '#a6adc8', fontSize: 11 }}>Started</div>
              <div style={{ color: '#cdd6f4', fontSize: 12 }}>
                {new Date(selectedExecution.started_at).toLocaleString()}
              </div>
            </div>
          </div>

          {selectedExecution.error_message && (
            <div
              style={{
                background: '#f38ba822',
                border: '1px solid #f38ba8',
                borderRadius: 8,
                padding: 12,
                marginBottom: 16,
              }}
            >
              <div style={{ color: '#f38ba8', fontWeight: 600, fontSize: 12, marginBottom: 4 }}>Error</div>
              <div style={{ color: '#f38ba8', fontSize: 13 }}>{selectedExecution.error_message}</div>
              {selectedExecution.error_stack && (
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
                    margin: '8px 0 0',
                  }}
                >
                  {selectedExecution.error_stack}
                </pre>
              )}
            </div>
          )}

          {/* Tabs */}
          <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid #313244', marginBottom: 16 }}>
            {(['steps', 'input', 'output'] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                style={{
                  background: 'none',
                  border: 'none',
                  borderBottom: activeTab === tab ? '2px solid #89b4fa' : '2px solid transparent',
                  color: activeTab === tab ? '#89b4fa' : '#a6adc8',
                  padding: '8px 16px',
                  fontSize: 13,
                  cursor: 'pointer',
                  fontWeight: activeTab === tab ? 600 : 400,
                  textTransform: 'capitalize',
                }}
              >
                {tab === 'steps' ? `Steps (${sortedSteps.length})` : tab}
              </button>
            ))}
          </div>

          {/* Tab content */}
          {activeTab === 'steps' && (
            <div>
              {sortedSteps.map((step) => (
                <StepRow key={step.id} step={step} />
              ))}
              {sortedSteps.length === 0 && (
                <div style={{ color: '#6c7086', textAlign: 'center', padding: 20 }}>No steps recorded.</div>
              )}
            </div>
          )}

          {activeTab === 'input' && (
            <div>
              <JsonViewer data={selectedExecution.trigger_data} label="Trigger Data" />
              {!selectedExecution.trigger_data && (
                <div style={{ color: '#6c7086', fontSize: 13 }}>No trigger data.</div>
              )}
            </div>
          )}

          {activeTab === 'output' && (
            <div>
              <JsonViewer data={selectedExecution.output_data} label="Output Data" />
              {!selectedExecution.output_data && (
                <div style={{ color: '#6c7086', fontSize: 13 }}>No output data.</div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
