// Types matching the backend store models (store/models.go)

export type ExecutionStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
export type StepStatus = 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';
export type IAMProviderType = 'aws_iam' | 'kubernetes' | 'oidc' | 'saml' | 'ldap' | 'custom';

export interface WorkflowExecution {
  id: string;
  workflow_id: string;
  trigger_type: string;
  trigger_data?: unknown;
  status: ExecutionStatus;
  output_data?: unknown;
  error_message?: string;
  error_stack?: string;
  started_at: string;
  completed_at?: string;
  duration_ms?: number;
  metadata?: unknown;
}

export interface ExecutionStep {
  id: string;
  execution_id: string;
  step_name: string;
  step_type: string;
  input_data?: unknown;
  output_data?: unknown;
  status: StepStatus;
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  duration_ms?: number;
  sequence_num: number;
  metadata?: unknown;
}

export interface ExecutionLog {
  id: number;
  workflow_id: string;
  execution_id?: string;
  level: LogLevel;
  message: string;
  module_name?: string;
  fields?: Record<string, unknown>;
  created_at: string;
}

export interface AuditEntry {
  id: number;
  user_id?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  details?: Record<string, unknown>;
  ip_address?: string;
  user_agent?: string;
  created_at: string;
}

export interface IAMProviderConfig {
  id: string;
  company_id: string;
  provider_type: IAMProviderType;
  name: string;
  config: Record<string, unknown>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface IAMRoleMapping {
  id: string;
  provider_id: string;
  external_identifier: string;
  resource_type: string;
  resource_id: string;
  role: string;
  created_at: string;
}

// Dashboard API response (matches api/dashboard_handler.go)
export interface WorkflowDashSummary {
  workflow_id: string;
  workflow_name: string;
  status: string;
  executions: Record<string, number>;
  log_counts: Record<string, number>;
}

export interface SystemDashboard {
  total_workflows: number;
  workflow_summaries: WorkflowDashSummary[];
}

export interface WorkflowDashboardResponse {
  workflow: {
    id: string;
    name: string;
    status: string;
    [key: string]: unknown;
  };
  execution_counts: Record<string, number>;
  log_counts: Record<string, number>;
  recent_executions: WorkflowExecution[];
}

export interface ExecutionFilter {
  status?: string;
  since?: string;
  until?: string;
}

export interface LogFilter {
  workflowId?: string;
  executionId?: string;
  level?: string;
  module?: string;
  since?: string;
}

export interface AuditFilter {
  userId?: string;
  action?: string;
  resourceType?: string;
  since?: string;
  until?: string;
}

export type ActiveView = 'editor' | 'dashboard' | 'executions' | 'logs' | 'events' | 'settings' | 'marketplace' | 'templates' | 'environments';
