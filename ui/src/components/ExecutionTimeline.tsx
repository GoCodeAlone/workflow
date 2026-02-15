import { useState, useMemo } from 'react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type StepStatus = 'completed' | 'failed' | 'skipped' | 'pending';

export interface TimelineStep {
  name: string;
  status: StepStatus;
  startTime: string;
  endTime: string;
  input?: unknown;
  output?: unknown;
}

export interface ExecutionTimelineProps {
  executionId: string;
  steps: TimelineStep[];
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const STATUS_COLORS: Record<StepStatus, string> = {
  completed: '#a6e3a1',
  failed: '#f38ba8',
  skipped: '#f9e2af',
  pending: '#6c7086',
};

const STATUS_LABELS: Record<StepStatus, string> = {
  completed: 'Completed',
  failed: 'Failed',
  skipped: 'Skipped',
  pending: 'Not yet executed',
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getDurationMs(start: string, end: string): number {
  return Math.max(0, new Date(end).getTime() - new Date(start).getTime());
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const min = Math.floor(ms / 60000);
  const sec = ((ms % 60000) / 1000).toFixed(0);
  return `${min}m ${sec}s`;
}

function formatJson(data: unknown): string {
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return String(data);
  }
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function Tooltip({
  children,
  text,
}: {
  children: React.ReactNode;
  text: string;
}) {
  const [show, setShow] = useState(false);
  const [pos, setPos] = useState({ x: 0, y: 0 });

  return (
    <div
      style={{ position: 'relative', display: 'inline-flex', flex: 'none' }}
      onMouseEnter={(e) => {
        const rect = e.currentTarget.getBoundingClientRect();
        setPos({ x: rect.left + rect.width / 2, y: rect.top });
        setShow(true);
      }}
      onMouseLeave={() => setShow(false)}
    >
      {children}
      {show && (
        <div
          style={{
            position: 'fixed',
            left: pos.x,
            top: pos.y - 8,
            transform: 'translate(-50%, -100%)',
            background: '#181825',
            border: '1px solid #45475a',
            borderRadius: 6,
            padding: '6px 10px',
            fontSize: 11,
            color: '#cdd6f4',
            whiteSpace: 'pre-line',
            zIndex: 2000,
            pointerEvents: 'none',
            maxWidth: 280,
            boxShadow: '0 4px 12px rgba(0,0,0,0.4)',
          }}
        >
          {text}
        </div>
      )}
    </div>
  );
}

function StepDetailPanel({ step, durationMs }: { step: TimelineStep; durationMs: number }) {
  const [showInput, setShowInput] = useState(false);
  const [showOutput, setShowOutput] = useState(false);

  return (
    <div
      style={{
        background: '#181825',
        border: '1px solid #313244',
        borderRadius: 6,
        padding: 12,
        marginTop: 6,
        fontSize: 12,
      }}
    >
      <div style={{ display: 'flex', gap: 16, marginBottom: 8, flexWrap: 'wrap' }}>
        <div>
          <span style={{ color: '#6c7086' }}>Status: </span>
          <span style={{ color: STATUS_COLORS[step.status], fontWeight: 600 }}>{STATUS_LABELS[step.status]}</span>
        </div>
        <div>
          <span style={{ color: '#6c7086' }}>Duration: </span>
          <span style={{ color: '#cdd6f4' }}>{formatDuration(durationMs)}</span>
        </div>
        <div>
          <span style={{ color: '#6c7086' }}>Start: </span>
          <span style={{ color: '#a6adc8' }}>{new Date(step.startTime).toLocaleTimeString()}</span>
        </div>
        <div>
          <span style={{ color: '#6c7086' }}>End: </span>
          <span style={{ color: '#a6adc8' }}>{new Date(step.endTime).toLocaleTimeString()}</span>
        </div>
      </div>

      {/* Input */}
      {step.input !== undefined && (
        <div style={{ marginBottom: 6 }}>
          <button
            onClick={() => setShowInput(!showInput)}
            style={{
              background: 'none',
              border: 'none',
              color: '#89b4fa',
              fontSize: 12,
              cursor: 'pointer',
              padding: 0,
              fontWeight: 500,
            }}
          >
            {showInput ? 'Hide' : 'Show'} Input
          </button>
          {showInput && (
            <pre
              style={{
                background: '#1e1e2e',
                border: '1px solid #313244',
                borderRadius: 4,
                padding: 8,
                marginTop: 4,
                fontSize: 11,
                color: '#a6e3a1',
                overflow: 'auto',
                maxHeight: 200,
              }}
            >
              {formatJson(step.input)}
            </pre>
          )}
        </div>
      )}

      {/* Output */}
      {step.output !== undefined && (
        <div>
          <button
            onClick={() => setShowOutput(!showOutput)}
            style={{
              background: 'none',
              border: 'none',
              color: '#89b4fa',
              fontSize: 12,
              cursor: 'pointer',
              padding: 0,
              fontWeight: 500,
            }}
          >
            {showOutput ? 'Hide' : 'Show'} Output
          </button>
          {showOutput && (
            <pre
              style={{
                background: '#1e1e2e',
                border: '1px solid #313244',
                borderRadius: 4,
                padding: 8,
                marginTop: 4,
                fontSize: 11,
                color: '#fab387',
                overflow: 'auto',
                maxHeight: 200,
              }}
            >
              {formatJson(step.output)}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function ExecutionTimeline({ executionId, steps }: ExecutionTimelineProps) {
  const [expandedStep, setExpandedStep] = useState<string | null>(null);

  const { maxDuration, stepDurations } = useMemo(() => {
    const durations = steps.map((s) => getDurationMs(s.startTime, s.endTime));
    const max = Math.max(...durations, 1);
    return { maxDuration: max, stepDurations: durations };
  }, [steps]);

  if (steps.length === 0) {
    return (
      <div
        style={{
          background: '#313244',
          borderRadius: 8,
          padding: 20,
          textAlign: 'center',
          color: '#6c7086',
          fontSize: 13,
        }}
      >
        No execution steps to display.
      </div>
    );
  }

  return (
    <div
      style={{
        background: '#313244',
        borderRadius: 8,
        padding: 16,
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <h3 style={{ color: '#cdd6f4', margin: 0, fontSize: 14, fontWeight: 600 }}>
          Execution Timeline
        </h3>
        <span style={{ fontSize: 11, color: '#6c7086' }}>ID: {executionId}</span>
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', gap: 16, marginBottom: 12, flexWrap: 'wrap' }}>
        {(Object.keys(STATUS_COLORS) as StepStatus[]).map((status) => (
          <span key={status} style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 11, color: '#a6adc8' }}>
            <span
              style={{
                width: 10,
                height: 10,
                borderRadius: 2,
                background: STATUS_COLORS[status],
                display: 'inline-block',
              }}
            />
            {STATUS_LABELS[status]}
          </span>
        ))}
      </div>

      {/* Timeline bars */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        {steps.map((step, idx) => {
          const duration = stepDurations[idx];
          const widthPct = Math.max(2, (duration / maxDuration) * 100);
          const isExpanded = expandedStep === step.name;

          return (
            <div key={step.name}>
              <div
                onClick={() => setExpandedStep(isExpanded ? null : step.name)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  cursor: 'pointer',
                  padding: '4px 0',
                }}
              >
                {/* Step name */}
                <div style={{ width: 140, minWidth: 140, fontSize: 12, color: '#cdd6f4', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {step.name}
                </div>

                {/* Bar */}
                <div style={{ flex: 1, position: 'relative', height: 22 }}>
                  <div
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      height: '100%',
                      background: '#1e1e2e',
                      borderRadius: 4,
                    }}
                  />
                  <Tooltip
                    text={`${step.name}\nStatus: ${STATUS_LABELS[step.status]}\nDuration: ${formatDuration(duration)}`}
                  >
                    <div
                      style={{
                        position: 'relative',
                        height: 22,
                        width: `${widthPct}%`,
                        minWidth: 20,
                        background: STATUS_COLORS[step.status],
                        borderRadius: 4,
                        opacity: 0.85,
                        transition: 'opacity 0.15s',
                      }}
                      onMouseEnter={(e) => (e.currentTarget.style.opacity = '1')}
                      onMouseLeave={(e) => (e.currentTarget.style.opacity = '0.85')}
                    />
                  </Tooltip>
                </div>

                {/* Duration label */}
                <div style={{ width: 70, minWidth: 70, fontSize: 11, color: '#6c7086', textAlign: 'right' }}>
                  {formatDuration(duration)}
                </div>
              </div>

              {/* Expanded detail */}
              {isExpanded && <StepDetailPanel step={step} durationMs={duration} />}
            </div>
          );
        })}
      </div>
    </div>
  );
}
