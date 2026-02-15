// ---- Workflow Types ----

/** Workflow represents a configured workflow definition. */
export interface Workflow {
  id: string;
  name: string;
  description?: string;
  version: number;
  status: WorkflowStatus;
  config: WorkflowConfig;
  created_at: string;
  updated_at: string;
}

/** WorkflowConfig is the declarative YAML-equivalent configuration. */
export interface WorkflowConfig {
  modules?: ModuleConfig[];
  workflows?: WorkflowDefinition[];
  triggers?: TriggerConfig[];
  [key: string]: unknown;
}

export interface ModuleConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

export interface WorkflowDefinition {
  name: string;
  steps?: StepConfig[];
  [key: string]: unknown;
}

export interface StepConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

export interface TriggerConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

export type WorkflowStatus = "active" | "inactive" | "draft" | "error";

// ---- Execution Types ----

/** Execution represents a running or completed workflow execution. */
export interface Execution {
  id: string;
  workflow_id: string;
  status: ExecutionStatus;
  input: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
  steps: StepExecution[];
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
}

export interface StepExecution {
  name: string;
  status: ExecutionStatus;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
}

export type ExecutionStatus =
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "timeout";

/** ExecutionFilter is used to filter execution listings. */
export interface ExecutionFilter {
  workflow_id?: string;
  status?: ExecutionStatus;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

// ---- SSE Types ----

/** SSEEvent represents a Server-Sent Event from execution streaming. */
export interface SSEEvent {
  id: string;
  event: string;
  data: string;
}

/** Parsed event data from SSE step events. */
export interface StepEventData {
  workflow_type?: string;
  step_name?: string;
  connector?: string;
  action?: string;
  status?: string;
  timestamp?: string;
  duration_ms?: number;
  error?: string;
  data?: Record<string, unknown>;
  results?: Record<string, unknown>;
}

// ---- DLQ Types ----

/** DLQEntry represents a dead-letter queue entry for failed events. */
export interface DLQEntry {
  id: string;
  workflow_id: string;
  execution_id: string;
  error: string;
  payload: Record<string, unknown>;
  retry_count: number;
  max_retries: number;
  created_at: string;
  last_retry_at?: string;
}

/** DLQFilter is used to filter DLQ entry listings. */
export interface DLQFilter {
  workflow_id?: string;
  since?: string;
  limit?: number;
  offset?: number;
}

// ---- Health Types ----

/** HealthStatus represents the overall system health. */
export interface HealthStatus {
  status: "healthy" | "degraded" | "unhealthy";
  checks: Record<string, HealthCheck>;
}

export interface HealthCheck {
  status: string;
  message?: string;
}

// ---- Client Options ----

/** Options for configuring the WorkflowClient. */
export interface ClientOptions {
  /** Base URL of the workflow engine (e.g., "http://localhost:8080"). */
  baseURL: string;
  /** Optional API key for authentication. */
  apiKey?: string;
  /** Optional custom fetch implementation (for testing or Node.js). */
  fetch?: typeof globalThis.fetch;
  /** Request timeout in milliseconds (default: 30000). */
  timeout?: number;
}
