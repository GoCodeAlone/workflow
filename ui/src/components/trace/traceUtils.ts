import type { ExecutionStep, ExecutionLog } from '../../types/observability.ts';
import type { TraceStep, LogEntry } from '@gocodealone/workflow-ui/trace';

export function toTraceStep(step: ExecutionStep): TraceStep {
  return {
    stepName: step.step_name,
    stepType: step.step_type,
    status: step.status as TraceStep['status'],
    durationMs: step.duration_ms,
    inputData: step.input_data as Record<string, unknown> | null | undefined,
    outputData: step.output_data as Record<string, unknown> | null | undefined,
    errorMessage: step.error_message,
    sequenceNum: step.sequence_num,
  };
}

export function toLogEntry(log: ExecutionLog): LogEntry {
  const level: LogEntry['level'] = log.level === 'fatal' ? 'error' : log.level as LogEntry['level'];
  return {
    id: String(log.id),
    level,
    message: log.message,
    moduleName: log.module_name,
    fields: log.fields,
    createdAt: log.created_at,
  };
}
