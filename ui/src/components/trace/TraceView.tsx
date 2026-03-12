import { useEffect, useState, useCallback } from 'react';
import type { Node as RFNode, Edge } from '@xyflow/react';
import {
  TraceCanvas,
  ExecutionWaterfall,
  ExecutionLogViewer,
  StepDetailPanel,
} from '@gocodealone/workflow-ui/trace';
import type { TraceStep, TraceData, LogEntry } from '@gocodealone/workflow-ui/trace';
import { toTraceStep, toLogEntry } from './traceUtils.ts';
import useObservabilityStore from '../../store/observabilityStore.ts';
import { useWorkflowStore } from '@gocodealone/workflow-editor/stores';
import { apiGetExecutionLogs, apiGetWorkflow, type ApiWorkflowRecord } from '../../utils/api.ts';
import { configToNodes, parseYaml } from '@gocodealone/workflow-editor/utils';

const STATUS_COLORS: Record<string, string> = {
  pending: '#6c7086',
  running: '#89b4fa',
  completed: '#a6e3a1',
  failed: '#f38ba8',
  cancelled: '#f9e2af',
};

export default function TraceView({ executionId }: { executionId: string }) {
  const {
    selectedExecution,
    executionSteps,
    fetchExecutionDetail,
    fetchExecutionSteps,
    setSelectedTraceExecutionId,
    selectedWorkflowId,
  } = useObservabilityStore();

  const activeWorkflowRecord = useWorkflowStore((s) => s.activeWorkflowRecord) as ApiWorkflowRecord | null;

  const [selectedStep, setSelectedStep] = useState<TraceStep | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [nodes, setNodes] = useState<RFNode[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);

  // Fetch execution data and logs on mount
  useEffect(() => {
    fetchExecutionDetail(executionId);
    fetchExecutionSteps(executionId);
    apiGetExecutionLogs(executionId)
      .then((raw) => setLogs(raw.filter((log) => log.level !== 'event').map(toLogEntry)))
      .catch(() => {});
  }, [executionId, fetchExecutionDetail, fetchExecutionSteps]);

  // Build canvas nodes/edges from workflow config
  useEffect(() => {
    const loadConfig = async () => {
      let configYaml = activeWorkflowRecord?.config_yaml;
      if (!configYaml && selectedWorkflowId) {
        try {
          const wf = await apiGetWorkflow(selectedWorkflowId);
          configYaml = wf.config_yaml;
        } catch {
          return;
        }
      }
      if (!configYaml) return;
      try {
        const config = parseYaml(configYaml);
        const { nodes: n, edges: e } = configToNodes(config);
        setNodes(n as RFNode[]);
        setEdges(e);
      } catch {
        // leave canvas empty
      }
    };
    loadConfig();
  }, [activeWorkflowRecord, selectedWorkflowId]);

  const handleBack = useCallback(() => {
    setSelectedTraceExecutionId(null);
  }, [setSelectedTraceExecutionId]);

  const handleStepClick = useCallback(
    (step: TraceStep) => {
      setSelectedStep((prev: TraceStep | null) => (prev?.stepName === step.stepName ? null : step));
    },
    [],
  );

  const handleWaterfallStepClick = useCallback(
    (stepName: string) => {
      const step = executionSteps.map(toTraceStep).find((s) => s.stepName === stepName);
      if (step) setSelectedStep((prev: TraceStep | null) => (prev?.stepName === stepName ? null : step));
    },
    [executionSteps],
  );

  const traceSteps = executionSteps.map(toTraceStep);
  const metadata = selectedExecution?.metadata as Record<string, unknown> | null | undefined;
  const configHash = (metadata?.config_version as string) ?? '';
  const status = selectedExecution?.status ?? 'pending';
  const statusColor = STATUS_COLORS[status] ?? '#6c7086';

  const traceData: TraceData = {
    executionId,
    pipeline: selectedExecution?.trigger_type ?? '',
    status,
    steps: traceSteps,
    configHash,
    startedAt: selectedExecution?.started_at ?? '',
    completedAt: selectedExecution?.completed_at,
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: '#1e1e2e' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: '10px 16px',
          borderBottom: '1px solid #313244',
          flexShrink: 0,
        }}
      >
        <button
          onClick={handleBack}
          style={{
            background: 'none',
            border: 'none',
            color: '#89b4fa',
            cursor: 'pointer',
            fontSize: 13,
            padding: '2px 4px',
            flexShrink: 0,
          }}
        >
          ← Back
        </button>
        <span style={{ color: '#a6adc8', fontSize: 11 }}>Trace</span>
        <span
          style={{
            color: '#cdd6f4',
            fontSize: 12,
            fontFamily: 'monospace',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            maxWidth: 280,
          }}
        >
          {executionId}
        </span>
        <span
          style={{
            display: 'inline-block',
            padding: '2px 10px',
            borderRadius: 12,
            fontSize: 11,
            fontWeight: 600,
            background: statusColor + '22',
            color: statusColor,
            flexShrink: 0,
          }}
        >
          {status}
        </span>
        {configHash && (
          <span
            style={{
              color: '#6c7086',
              fontSize: 10,
              fontFamily: 'monospace',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              maxWidth: 200,
            }}
            title={configHash}
          >
            config: {configHash}
          </span>
        )}
      </div>

      {/* Main content area */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Left: canvas + waterfall + logs */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {/* TraceCanvas */}
          <div style={{ flex: '0 0 280px', overflow: 'hidden', borderBottom: '1px solid #313244' }}>
            {nodes.length > 0 ? (
              <TraceCanvas
                nodes={nodes}
                edges={edges}
                traceData={traceData}
                onStepClick={handleStepClick}
              />
            ) : (
              <div
                style={{
                  height: '100%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  color: '#45475a',
                  fontSize: 12,
                }}
              >
                No workflow config loaded
              </div>
            )}
          </div>

          {/* Waterfall + logs (scrollable) */}
          <div style={{ flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
            {traceSteps.length > 0 && (
              <div style={{ borderBottom: '1px solid #313244', flexShrink: 0 }}>
                <ExecutionWaterfall
                  steps={traceSteps}
                  onStepClick={handleWaterfallStepClick}
                />
              </div>
            )}
            <div style={{ flex: 1, minHeight: 200 }}>
              <ExecutionLogViewer logs={logs} onStepClick={handleWaterfallStepClick} />
            </div>
          </div>
        </div>

        {/* Right: StepDetailPanel */}
        {selectedStep && (
          <StepDetailPanel
            step={selectedStep}
            onClose={() => setSelectedStep(null)}
            style={{ width: 360, flexShrink: 0, borderLeft: '1px solid #313244' }}
          />
        )}
      </div>
    </div>
  );
}
