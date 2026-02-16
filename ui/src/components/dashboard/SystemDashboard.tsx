import { useEffect, useCallback, useMemo, useState } from 'react';
import useObservabilityStore from '../../store/observabilityStore.ts';
import useWorkflowStore from '../../store/workflowStore.ts';
import type { WorkflowDashSummary } from '../../types/observability.ts';
import { apiFetchRuntimeInstances, apiStopRuntimeInstance, type RuntimeInstanceResponse, type ApiWorkflowRecord } from '../../utils/api.ts';

const STATUS_COLORS: Record<string, string> = {
  draft: '#6c7086',
  active: '#a6e3a1',
  stopped: '#f9e2af',
  error: '#f38ba8',
};

const EXEC_STATUS_COLORS: Record<string, string> = {
  pending: '#6c7086',
  running: '#89b4fa',
  completed: '#a6e3a1',
  failed: '#f38ba8',
  cancelled: '#f9e2af',
};

function MetricCard({ label, value, color }: { label: string; value: string | number; color: string }) {
  return (
    <div
      style={{
        background: '#313244',
        borderRadius: 8,
        padding: '16px 20px',
        borderLeft: `4px solid ${color}`,
        flex: 1,
        minWidth: 160,
      }}
    >
      <div style={{ fontSize: 28, fontWeight: 700, color: '#cdd6f4' }}>{value}</div>
      <div style={{ fontSize: 12, color: '#a6adc8', marginTop: 4 }}>{label}</div>
    </div>
  );
}

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

function WorkflowCard({
  summary,
  onClick,
}: {
  summary: WorkflowDashSummary;
  onClick: () => void;
}) {
  const totalExecs = Object.values(summary.executions).reduce((a, b) => a + b, 0);
  const errorCount = (summary.log_counts?.error ?? 0) + (summary.log_counts?.fatal ?? 0);

  return (
    <div
      onClick={onClick}
      style={{
        background: '#313244',
        border: '1px solid #45475a',
        borderRadius: 8,
        padding: 16,
        cursor: 'pointer',
        transition: 'border-color 0.15s',
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#89b4fa')}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = '#45475a')}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <span style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{summary.workflow_name}</span>
        <StatusBadge status={summary.status} />
      </div>
      <div style={{ fontSize: 12, color: '#a6adc8' }}>
        {totalExecs} executions
        {errorCount > 0 && (
          <span style={{ color: '#f38ba8', marginLeft: 8 }}>{errorCount} errors</span>
        )}
      </div>
    </div>
  );
}

export default function SystemDashboard() {
  const { systemDashboard, dashboardLoading, fetchSystemDashboard, setActiveView, setSelectedWorkflowId } =
    useObservabilityStore();
  const setActiveWorkflowRecord = useWorkflowStore((s) => s.setActiveWorkflowRecord);
  const renameTab = useWorkflowStore((s) => s.renameTab);
  const activeTabId = useWorkflowStore((s) => s.activeTabId);

  const [runtimeInstances, setRuntimeInstances] = useState<RuntimeInstanceResponse[]>([]);

  useEffect(() => {
    fetchSystemDashboard();
    apiFetchRuntimeInstances()
      .then((r) => setRuntimeInstances(r.instances ?? []))
      .catch(() => {});
    const interval = setInterval(() => {
      fetchSystemDashboard();
      apiFetchRuntimeInstances()
        .then((r) => setRuntimeInstances(r.instances ?? []))
        .catch(() => {});
    }, 10000);
    return () => clearInterval(interval);
  }, [fetchSystemDashboard]);

  const summaries = useMemo(() => systemDashboard?.workflow_summaries ?? [], [systemDashboard]);

  const handleWorkflowClick = useCallback(
    (wfId: string) => {
      const wf = summaries.find((s) => s.workflow_id === wfId);
      const workflowName = wf?.workflow_name ?? wfId;
      setSelectedWorkflowId(wfId);
      renameTab(activeTabId, workflowName);
      setActiveWorkflowRecord({ id: wfId, name: workflowName } as Pick<ApiWorkflowRecord, 'id' | 'name'> as ApiWorkflowRecord);
      setActiveView('executions');
    },
    [setActiveView, setSelectedWorkflowId, summaries, renameTab, activeTabId, setActiveWorkflowRecord],
  );

  const totalActive = summaries.filter((s) => s.status === 'active').length;
  const totalExecs = summaries.reduce(
    (acc, s) => acc + Object.values(s.executions).reduce((a, b) => a + b, 0),
    0,
  );
  const totalErrors = summaries.reduce(
    (acc, s) => acc + (s.executions?.failed ?? 0),
    0,
  );
  const errorRate = totalExecs > 0 ? ((totalErrors / totalExecs) * 100).toFixed(1) : '0';

  // Collect recent failed executions across all workflows for error summary
  const errorSummary = summaries
    .filter((s) => (s.executions?.failed ?? 0) > 0)
    .map((s) => ({ name: s.workflow_name, count: s.executions.failed ?? 0, id: s.workflow_id }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5);

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <h2 style={{ color: '#cdd6f4', margin: '0 0 20px', fontSize: 20, fontWeight: 600 }}>System Dashboard</h2>

      {dashboardLoading && !systemDashboard && (
        <div style={{ color: '#6c7086', padding: 40, textAlign: 'center' }}>Loading...</div>
      )}

      {/* Metrics row */}
      <div style={{ display: 'flex', gap: 16, marginBottom: 24, flexWrap: 'wrap' }}>
        <MetricCard label="Total Workflows" value={systemDashboard?.total_workflows ?? 0} color="#89b4fa" />
        <MetricCard label="Active Workflows" value={totalActive} color="#a6e3a1" />
        <MetricCard label="Total Executions" value={totalExecs} color="#fab387" />
        <MetricCard label="Error Rate" value={`${errorRate}%`} color="#f38ba8" />
      </div>

      {/* Workflow grid */}
      <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 12 }}>Workflows</h3>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
          gap: 12,
          marginBottom: 24,
        }}
      >
        {summaries.map((s) => (
          <WorkflowCard key={s.workflow_id} summary={s} onClick={() => handleWorkflowClick(s.workflow_id)} />
        ))}
        {summaries.length === 0 && !dashboardLoading && (
          <div style={{ color: '#6c7086', fontSize: 13, gridColumn: '1 / -1' }}>No workflows found.</div>
        )}
      </div>

      {/* Runtime Instances */}
      {runtimeInstances.length > 0 && (
        <>
          <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 12 }}>
            Runtime Instances ({runtimeInstances.length})
          </h3>
          <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden', marginBottom: 24 }}>
            {runtimeInstances.map((inst, i) => (
              <div
                key={inst.id}
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  padding: '10px 16px',
                  borderBottom: i < runtimeInstances.length - 1 ? '1px solid #45475a' : 'none',
                }}
              >
                <div>
                  <span style={{ color: '#cdd6f4', fontSize: 13, fontWeight: 600 }}>{inst.name}</span>
                  <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>{inst.config_path}</div>
                  {inst.error && (
                    <div style={{ fontSize: 11, color: '#f38ba8', marginTop: 2 }}>{inst.error}</div>
                  )}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <StatusBadge status={inst.status} />
                  {inst.status === 'running' && (
                    <button
                      onClick={() =>
                        apiStopRuntimeInstance(inst.id).then(() =>
                          apiFetchRuntimeInstances()
                            .then((r) => setRuntimeInstances(r.instances ?? []))
                            .catch(() => {}),
                        )
                      }
                      style={{
                        background: '#f38ba822',
                        color: '#f38ba8',
                        border: '1px solid #f38ba844',
                        borderRadius: 4,
                        padding: '2px 8px',
                        fontSize: 11,
                        cursor: 'pointer',
                      }}
                    >
                      Stop
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {/* Error Summary */}
      {errorSummary.length > 0 && (
        <>
          <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 12 }}>Top Errors</h3>
          <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
            {errorSummary.map((e, i) => (
              <div
                key={e.id}
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  padding: '10px 16px',
                  borderBottom: i < errorSummary.length - 1 ? '1px solid #45475a' : 'none',
                  cursor: 'pointer',
                }}
                onClick={() => handleWorkflowClick(e.id)}
              >
                <span style={{ color: '#cdd6f4', fontSize: 13 }}>{e.name}</span>
                <span
                  style={{
                    color: '#f38ba8',
                    fontSize: 13,
                    fontWeight: 600,
                    background: '#f38ba822',
                    padding: '1px 8px',
                    borderRadius: 8,
                  }}
                >
                  {e.count} failed
                </span>
              </div>
            ))}
          </div>
        </>
      )}

      {/* Recent execution status breakdown */}
      {summaries.length > 0 && (
        <>
          <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, margin: '24px 0 12px' }}>
            Execution Status Breakdown
          </h3>
          <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '2fr repeat(5, 1fr)',
                padding: '10px 16px',
                background: '#181825',
                fontSize: 11,
                color: '#a6adc8',
                fontWeight: 600,
              }}
            >
              <span>Workflow</span>
              <span>Pending</span>
              <span>Running</span>
              <span>Completed</span>
              <span>Failed</span>
              <span>Cancelled</span>
            </div>
            {summaries.map((s, i) => (
              <div
                key={s.workflow_id}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '2fr repeat(5, 1fr)',
                  padding: '8px 16px',
                  borderBottom: i < summaries.length - 1 ? '1px solid #45475a' : 'none',
                  fontSize: 13,
                  background: i % 2 === 0 ? 'transparent' : '#181825',
                }}
              >
                <span style={{ color: '#cdd6f4' }}>{s.workflow_name}</span>
                {(['pending', 'running', 'completed', 'failed', 'cancelled'] as const).map((st) => (
                  <span key={st} style={{ color: EXEC_STATUS_COLORS[st] }}>
                    {s.executions[st] ?? 0}
                  </span>
                ))}
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
