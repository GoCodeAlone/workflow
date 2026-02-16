import { useEffect, useState, useCallback, useMemo } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import type { WorkflowExecution, ExecutionStep } from '../../types/observability.ts';

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

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString();
}

function truncateId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) + '...' : id;
}

function StepTimeline({ steps }: { steps: ExecutionStep[] }) {
  const [expandedStep, setExpandedStep] = useState<string | null>(null);
  const sorted = [...steps].sort((a, b) => a.sequence_num - b.sequence_num);
  const maxDur = Math.max(...sorted.map((s) => s.duration_ms ?? 0), 1);

  return (
    <div style={{ padding: '12px 16px 12px 32px', background: '#181825', borderTop: '1px solid #45475a' }}>
      {sorted.map((step) => {
        const barWidth = step.duration_ms ? Math.max((step.duration_ms / maxDur) * 100, 4) : 4;
        const isFailed = step.status === 'failed';
        const isExpanded = expandedStep === step.id;

        return (
          <div key={step.id} style={{ marginBottom: 8 }}>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                cursor: 'pointer',
                padding: '4px 0',
              }}
              onClick={() => setExpandedStep(isExpanded ? null : step.id)}
            >
              <div
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  background: STATUS_COLORS[step.status] || '#6c7086',
                  flexShrink: 0,
                }}
              />
              <span style={{ color: '#cdd6f4', fontSize: 12, fontWeight: 500, minWidth: 120 }}>
                {step.step_name}
              </span>
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
              <div
                style={{
                  flex: 1,
                  height: 6,
                  background: '#45475a',
                  borderRadius: 3,
                  overflow: 'hidden',
                }}
              >
                <div
                  style={{
                    width: `${barWidth}%`,
                    height: '100%',
                    background: isFailed ? '#f38ba8' : '#89b4fa',
                    borderRadius: 3,
                  }}
                />
              </div>
              <span style={{ color: '#a6adc8', fontSize: 11, minWidth: 50, textAlign: 'right' }}>
                {formatDuration(step.duration_ms)}
              </span>
            </div>
            {isFailed && step.error_message && (
              <div style={{ color: '#f38ba8', fontSize: 11, marginLeft: 24, marginTop: 2 }}>
                {step.error_message}
              </div>
            )}
            {isExpanded && (
              <div style={{ marginLeft: 24, marginTop: 4, fontSize: 11 }}>
                {step.input_data != null && (
                  <div style={{ marginBottom: 4 }}>
                    <span style={{ color: '#a6adc8' }}>Input: </span>
                    <pre
                      style={{
                        background: '#0d1117',
                        padding: 8,
                        borderRadius: 4,
                        color: '#a6e3a1',
                        overflow: 'auto',
                        maxHeight: 120,
                        margin: '2px 0',
                        fontFamily: 'monospace',
                        fontSize: 11,
                      }}
                    >
                      {JSON.stringify(step.input_data, null, 2)}
                    </pre>
                  </div>
                )}
                {step.output_data != null && (
                  <div>
                    <span style={{ color: '#a6adc8' }}>Output: </span>
                    <pre
                      style={{
                        background: '#0d1117',
                        padding: 8,
                        borderRadius: 4,
                        color: '#a6e3a1',
                        overflow: 'auto',
                        maxHeight: 120,
                        margin: '2px 0',
                        fontFamily: 'monospace',
                        fontSize: 11,
                      }}
                    >
                      {JSON.stringify(step.output_data, null, 2)}
                    </pre>
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
      {sorted.length === 0 && <div style={{ color: '#6c7086', fontSize: 12 }}>No steps recorded.</div>}
    </div>
  );
}

export default function WorkflowDashboard() {
  const {
    selectedWorkflowId,
    executions,
    executionSteps,
    executionFilter,
    fetchExecutions,
    fetchExecutionSteps,
    setExecutionFilter,
    triggerExecution,
    cancelExecution,
    setActiveView,
    setSelectedWorkflowId,
    workflowDashboard,
    fetchWorkflowDashboard,
  } = useObservabilityStore();

  const [expandedExecId, setExpandedExecId] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<string>('');

  useEffect(() => {
    if (selectedWorkflowId) {
      fetchExecutions(selectedWorkflowId, executionFilter);
      fetchWorkflowDashboard(selectedWorkflowId);
    }
  }, [selectedWorkflowId, executionFilter, fetchExecutions, fetchWorkflowDashboard]);

  useEffect(() => {
    if (!selectedWorkflowId) return;
    const interval = setInterval(() => {
      fetchExecutions(selectedWorkflowId, executionFilter);
    }, 10000);
    return () => clearInterval(interval);
  }, [selectedWorkflowId, executionFilter, fetchExecutions]);

  const handleExpandExec = useCallback(
    (exec: WorkflowExecution) => {
      if (expandedExecId === exec.id) {
        setExpandedExecId(null);
        return;
      }
      setExpandedExecId(exec.id);
      fetchExecutionSteps(exec.id);
    },
    [expandedExecId, fetchExecutionSteps],
  );

  const handleStatusFilter = useCallback(
    (s: string) => {
      setStatusFilter(s);
      setExecutionFilter({ ...executionFilter, status: s || undefined });
    },
    [executionFilter, setExecutionFilter],
  );

  const handleBackToDashboard = useCallback(() => {
    setSelectedWorkflowId(null);
    setActiveView('dashboard');
  }, [setActiveView, setSelectedWorkflowId]);

  const wfName = workflowDashboard?.workflow?.name ?? 'Workflow';
  const wfStatus = workflowDashboard?.workflow?.status ?? '';

  // Compute execution counts: prefer dashboard data, fall back to counting local executions
  const dashCounts = workflowDashboard?.execution_counts ?? {};
  const dashTotal = Object.values(dashCounts).reduce((a, b) => a + b, 0);

  const localCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const exec of executions) {
      counts[exec.status] = (counts[exec.status] ?? 0) + 1;
    }
    return counts;
  }, [executions]);
  const localTotal = executions.length;

  const totalExecs = dashTotal > 0 ? dashTotal : localTotal;
  const completedCount = dashTotal > 0
    ? (dashCounts['completed'] ?? 0)
    : (localCounts['completed'] ?? 0);
  const successRate = totalExecs > 0 ? ((completedCount / totalExecs) * 100).toFixed(0) : '-';

  return (
    <div style={{ flex: 1, background: '#1e1e2e', overflow: 'auto', padding: 24 }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
        <button
          onClick={handleBackToDashboard}
          style={{
            background: 'none',
            border: 'none',
            color: '#89b4fa',
            cursor: 'pointer',
            fontSize: 13,
            padding: '2px 4px',
          }}
        >
          &larr; Dashboard
        </button>
        <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 20, fontWeight: 600 }}>{wfName}</h2>
        {wfStatus && <StatusBadge status={wfStatus} />}
        <div style={{ flex: 1 }} />
        {selectedWorkflowId && (
          <button
            onClick={() => triggerExecution(selectedWorkflowId)}
            style={{
              background: '#89b4fa',
              border: 'none',
              borderRadius: 6,
              color: '#1e1e2e',
              padding: '6px 16px',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Trigger Execution
          </button>
        )}
      </div>

      {/* Stats row */}
      <div style={{ display: 'flex', gap: 16, marginBottom: 20, flexWrap: 'wrap' }}>
        <div
          style={{
            background: '#313244',
            borderRadius: 8,
            padding: '12px 20px',
            borderLeft: '4px solid #a6e3a1',
            minWidth: 120,
          }}
        >
          <div style={{ fontSize: 24, fontWeight: 700, color: '#cdd6f4' }}>{successRate}%</div>
          <div style={{ fontSize: 11, color: '#a6adc8' }}>Success Rate</div>
        </div>
        <div
          style={{
            background: '#313244',
            borderRadius: 8,
            padding: '12px 20px',
            borderLeft: '4px solid #89b4fa',
            minWidth: 120,
          }}
        >
          <div style={{ fontSize: 24, fontWeight: 700, color: '#cdd6f4' }}>{totalExecs}</div>
          <div style={{ fontSize: 11, color: '#a6adc8' }}>Total Executions</div>
        </div>
      </div>

      {/* Filter bar */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
        <span style={{ color: '#a6adc8', fontSize: 12 }}>Filter:</span>
        {['', 'pending', 'running', 'completed', 'failed', 'cancelled'].map((s) => (
          <button
            key={s}
            onClick={() => handleStatusFilter(s)}
            style={{
              background: statusFilter === s ? '#89b4fa' : '#313244',
              color: statusFilter === s ? '#1e1e2e' : '#cdd6f4',
              border: '1px solid #45475a',
              borderRadius: 4,
              padding: '3px 10px',
              fontSize: 11,
              cursor: 'pointer',
              fontWeight: statusFilter === s ? 600 : 400,
            }}
          >
            {s || 'All'}
          </button>
        ))}
      </div>

      {/* Execution table */}
      <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '100px 100px 100px 90px 140px 60px',
            padding: '10px 16px',
            background: '#181825',
            fontSize: 11,
            color: '#a6adc8',
            fontWeight: 600,
          }}
        >
          <span>ID</span>
          <span>Trigger</span>
          <span>Status</span>
          <span>Duration</span>
          <span>Started</span>
          <span></span>
        </div>
        {executions.map((exec, i) => (
          <div key={exec.id}>
            <div
              onClick={() => handleExpandExec(exec)}
              style={{
                display: 'grid',
                gridTemplateColumns: '100px 100px 100px 90px 140px 60px',
                padding: '8px 16px',
                borderBottom: '1px solid #45475a',
                fontSize: 12,
                background: i % 2 === 0 ? 'transparent' : '#181825',
                cursor: 'pointer',
                alignItems: 'center',
              }}
            >
              <span style={{ color: '#89b4fa', fontFamily: 'monospace', fontSize: 11 }}>
                {truncateId(exec.id)}
              </span>
              <span style={{ color: '#a6adc8' }}>{exec.trigger_type}</span>
              <span><StatusBadge status={exec.status} /></span>
              <span style={{ color: '#a6adc8' }}>{formatDuration(exec.duration_ms)}</span>
              <span style={{ color: '#a6adc8', fontSize: 11 }}>{formatTime(exec.started_at)}</span>
              <span>
                {(exec.status === 'pending' || exec.status === 'running') && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      cancelExecution(exec.id);
                    }}
                    style={{
                      background: '#f38ba822',
                      border: '1px solid #f38ba8',
                      borderRadius: 4,
                      color: '#f38ba8',
                      fontSize: 10,
                      padding: '2px 6px',
                      cursor: 'pointer',
                    }}
                  >
                    Cancel
                  </button>
                )}
              </span>
            </div>
            {expandedExecId === exec.id && <StepTimeline steps={executionSteps} />}
          </div>
        ))}
        {executions.length === 0 && (
          <div style={{ padding: 20, color: '#6c7086', fontSize: 13, textAlign: 'center' }}>
            No executions found.
          </div>
        )}
      </div>
    </div>
  );
}
