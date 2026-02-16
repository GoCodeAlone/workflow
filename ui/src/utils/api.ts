import type { WorkflowConfig } from '../types/workflow.ts';
import useAuthStore from '../store/authStore.ts';

/** If we get a 401, the token is invalid — force logout to show login screen. */
function handleUnauthorized(status: number, body: string): void {
  if (status === 401 || status === 403) {
    const msg = body.toLowerCase();
    if (msg.includes('user not found') || msg.includes('unauthorized') || msg.includes('invalid') || msg.includes('expired') || status === 401) {
      useAuthStore.getState().logout();
    }
  }
}

export interface ValidationResult {
  valid: boolean;
  errors: string[];
  warnings: string[];
}

export interface GenerateResponse {
  config: WorkflowConfig;
  explanation: string;
}

export interface ComponentSpec {
  name: string;
  description: string;
  language: string;
}

export interface WorkflowSuggestion {
  title: string;
  description: string;
  intent: string;
}

export interface DynamicComponent {
  name: string;
  source: string;
  status: 'running' | 'stopped' | 'error';
  language: string;
  loadedAt: string;
}

export interface ModuleTypeInfo {
  type: string;
  label: string;
  category: string;
  description: string;
}

// --- Unified API client (all routes under /api/v1) ---

const V1_BASE = '/api/v1';

function getAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('auth_token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

async function v1Fetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${V1_BASE}${path}`, {
    ...options,
    headers: { ...getAuthHeaders(), ...(options?.headers as Record<string, string>) },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    handleUnauthorized(res.status, text);
    throw new Error(`API ${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

// --- Engine Management ---

export async function getWorkflowConfig(): Promise<WorkflowConfig> {
  return v1Fetch<WorkflowConfig>('/admin/engine/config');
}

export async function saveWorkflowConfig(config: WorkflowConfig): Promise<void> {
  await v1Fetch<void>('/admin/engine/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  });
}

export async function getModuleTypes(): Promise<ModuleTypeInfo[]> {
  return v1Fetch<ModuleTypeInfo[]>('/admin/engine/modules');
}

export async function validateWorkflow(config: WorkflowConfig): Promise<ValidationResult> {
  return v1Fetch<ValidationResult>('/admin/engine/validate', {
    method: 'POST',
    body: JSON.stringify(config),
  });
}

// --- AI ---

export async function generateWorkflow(intent: string): Promise<GenerateResponse> {
  return v1Fetch<GenerateResponse>('/admin/ai/generate', {
    method: 'POST',
    body: JSON.stringify({ intent }),
  });
}

export async function generateComponent(spec: ComponentSpec): Promise<string> {
  const res = await fetch(`${V1_BASE}/admin/ai/component`, {
    method: 'POST',
    headers: getAuthHeaders(),
    body: JSON.stringify(spec),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    handleUnauthorized(res.status, text);
    throw new Error(`API ${res.status}: ${text}`);
  }
  return res.text();
}

export async function suggestWorkflows(useCase: string): Promise<WorkflowSuggestion[]> {
  return v1Fetch<WorkflowSuggestion[]>('/admin/ai/suggest', {
    method: 'POST',
    body: JSON.stringify({ useCase }),
  });
}

// --- Dynamic Components ---

export async function listDynamicComponents(): Promise<DynamicComponent[]> {
  return v1Fetch<DynamicComponent[]>('/admin/components');
}

export async function createDynamicComponent(name: string, source: string, language: string): Promise<void> {
  await v1Fetch<void>('/admin/components', {
    method: 'POST',
    body: JSON.stringify({ id: name, source, language }),
  });
}

export async function deleteDynamicComponent(name: string): Promise<void> {
  await v1Fetch<void>(`/admin/components/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
}

// --- Auth ---

export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

export interface ApiUser {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export function apiLogin(email: string, password: string): Promise<TokenResponse> {
  return v1Fetch<TokenResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  });
}

export function apiRegister(email: string, password: string, displayName: string): Promise<TokenResponse> {
  return v1Fetch<TokenResponse>('/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password, display_name: displayName }),
  });
}

export function apiRefreshToken(refreshToken: string): Promise<TokenResponse> {
  return v1Fetch<TokenResponse>('/auth/refresh', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
  });
}

export function apiLogout(): Promise<void> {
  return v1Fetch<void>('/auth/logout', { method: 'POST' });
}

export function apiGetMe(): Promise<ApiUser> {
  return v1Fetch<ApiUser>('/auth/me');
}

export function apiUpdateMe(data: { display_name?: string; avatar_url?: string }): Promise<ApiUser> {
  return v1Fetch<ApiUser>('/auth/me', {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

// --- User Management (admin) ---

export interface AdminUser {
  id: string;
  email: string;
  name: string;
  role: string;
  createdAt: string;
}

export function apiListUsers(): Promise<AdminUser[]> {
  return v1Fetch<AdminUser[]>('/auth/users');
}

export function apiCreateUser(email: string, password: string, name: string, role: string): Promise<AdminUser> {
  return v1Fetch<AdminUser>('/auth/users', {
    method: 'POST',
    body: JSON.stringify({ email, password, name, role }),
  });
}

export function apiDeleteUser(id: string): Promise<void> {
  return v1Fetch<void>(`/auth/users/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export function apiUpdateUserRole(id: string, role: string): Promise<AdminUser> {
  return v1Fetch<AdminUser>(`/auth/users/${encodeURIComponent(id)}/role`, {
    method: 'PUT',
    body: JSON.stringify({ role }),
  });
}

// --- Companies ---

export interface ApiCompany {
  id: string;
  name: string;
  slug: string;
  owner_id: string;
  parent_id?: string;
  is_system?: boolean;
  metadata?: unknown;
  created_at: string;
  updated_at: string;
}

export function apiListCompanies(): Promise<ApiCompany[]> {
  return v1Fetch<ApiCompany[]>('/admin/companies');
}

export function apiCreateCompany(name: string, slug?: string): Promise<ApiCompany> {
  return v1Fetch<ApiCompany>('/admin/companies', {
    method: 'POST',
    body: JSON.stringify({ name, slug }),
  });
}

export function apiGetCompany(id: string): Promise<ApiCompany> {
  return v1Fetch<ApiCompany>(`/admin/companies/${encodeURIComponent(id)}`);
}

// --- Organizations ---

export function apiListOrgs(companyId: string): Promise<ApiCompany[]> {
  return v1Fetch<ApiCompany[]>(`/admin/companies/${encodeURIComponent(companyId)}/organizations`);
}

export function apiCreateOrg(companyId: string, name: string, slug?: string): Promise<ApiCompany> {
  return v1Fetch<ApiCompany>(`/admin/companies/${encodeURIComponent(companyId)}/organizations`, {
    method: 'POST',
    body: JSON.stringify({ name, slug }),
  });
}

// --- Projects ---

export interface ApiProject {
  id: string;
  company_id: string;
  name: string;
  slug: string;
  description?: string;
  is_system?: boolean;
  metadata?: unknown;
  created_at: string;
  updated_at: string;
}

export function apiListProjects(orgId: string): Promise<ApiProject[]> {
  return v1Fetch<ApiProject[]>(`/admin/organizations/${encodeURIComponent(orgId)}/projects`);
}

export function apiCreateProject(orgId: string, name: string, slug?: string): Promise<ApiProject> {
  return v1Fetch<ApiProject>(`/admin/organizations/${encodeURIComponent(orgId)}/projects`, {
    method: 'POST',
    body: JSON.stringify({ name, slug }),
  });
}

// --- Workflows ---

export interface ApiWorkflowRecord {
  id: string;
  project_id: string;
  name: string;
  slug: string;
  description?: string;
  config_yaml: string;
  version: number;
  status: 'draft' | 'active' | 'stopped' | 'error';
  is_system?: boolean;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
}

export function apiListWorkflows(projectId?: string): Promise<ApiWorkflowRecord[]> {
  if (projectId) {
    return v1Fetch<ApiWorkflowRecord[]>(`/admin/projects/${encodeURIComponent(projectId)}/workflows`);
  }
  return v1Fetch<ApiWorkflowRecord[]>('/admin/workflows');
}

export function apiCreateWorkflow(
  projectId: string,
  data: { name: string; slug?: string; description?: string; config_yaml?: string },
): Promise<ApiWorkflowRecord> {
  return v1Fetch<ApiWorkflowRecord>(`/admin/projects/${encodeURIComponent(projectId)}/workflows`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function apiGetWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return v1Fetch<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}`);
}

export function apiUpdateWorkflow(
  id: string,
  data: { name?: string; description?: string; config_yaml?: string },
): Promise<ApiWorkflowRecord> {
  return v1Fetch<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export function apiDeleteWorkflow(id: string): Promise<void> {
  return v1Fetch<void>(`/admin/workflows/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export function apiDeployWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return v1Fetch<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}/deploy`, { method: 'POST' });
}

export function apiStopWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return v1Fetch<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}/stop`, { method: 'POST' });
}

export function apiGetWorkflowStatus(id: string): Promise<{ id: string; status: string; version: number }> {
  return v1Fetch<{ id: string; status: string; version: number }>(`/admin/workflows/${encodeURIComponent(id)}/status`);
}

export interface ApiWorkflowVersion {
  id: string;
  workflow_id: string;
  version: number;
  config_yaml: string;
  created_by: string;
  created_at: string;
}

export function apiListVersions(id: string): Promise<ApiWorkflowVersion[]> {
  return v1Fetch<ApiWorkflowVersion[]>(`/admin/workflows/${encodeURIComponent(id)}/versions`);
}

export interface ApiMembership {
  id: string;
  user_id: string;
  company_id: string;
  project_id?: string;
  role: string;
  created_at: string;
  updated_at: string;
}

export function apiListPermissions(id: string): Promise<ApiMembership[]> {
  return v1Fetch<ApiMembership[]>(`/admin/workflows/${encodeURIComponent(id)}/permissions`);
}

export function apiShareWorkflow(id: string, userId: string, role: string): Promise<ApiMembership> {
  return v1Fetch<ApiMembership>(`/admin/workflows/${encodeURIComponent(id)}/permissions`, {
    method: 'POST',
    body: JSON.stringify({ user_id: userId, role }),
  });
}

// --- Dashboard ---

import type {
  SystemDashboard,
  WorkflowDashboardResponse,
  WorkflowExecution,
  ExecutionStep,
  ExecutionLog,
  ExecutionFilter,
  LogFilter,
  IAMProviderConfig,
  IAMRoleMapping,
  AuditEntry,
  AuditFilter,
} from '../types/observability.ts';

export function apiFetchDashboard(): Promise<SystemDashboard> {
  return v1Fetch<SystemDashboard>('/admin/dashboard');
}

export function apiFetchWorkflowDashboard(workflowId: string): Promise<WorkflowDashboardResponse> {
  return v1Fetch<WorkflowDashboardResponse>(`/admin/workflows/${encodeURIComponent(workflowId)}/dashboard`);
}

// --- Executions ---

export function apiFetchExecutions(workflowId: string, filter?: ExecutionFilter): Promise<WorkflowExecution[]> {
  const params = new URLSearchParams();
  if (filter?.status) params.set('status', filter.status);
  if (filter?.since) params.set('since', filter.since);
  if (filter?.until) params.set('until', filter.until);
  const qs = params.toString();
  return v1Fetch<WorkflowExecution[]>(
    `/admin/workflows/${encodeURIComponent(workflowId)}/executions${qs ? '?' + qs : ''}`,
  );
}

export function apiFetchExecutionDetail(executionId: string): Promise<WorkflowExecution> {
  return v1Fetch<WorkflowExecution>(`/admin/executions/${encodeURIComponent(executionId)}`);
}

export function apiFetchExecutionSteps(executionId: string): Promise<ExecutionStep[]> {
  return v1Fetch<ExecutionStep[]>(`/admin/executions/${encodeURIComponent(executionId)}/steps`);
}

export function apiTriggerExecution(workflowId: string, triggerData?: unknown): Promise<WorkflowExecution> {
  return v1Fetch<WorkflowExecution>(`/admin/workflows/${encodeURIComponent(workflowId)}/trigger`, {
    method: 'POST',
    body: JSON.stringify({ trigger_data: triggerData ?? {} }),
  });
}

export function apiCancelExecution(executionId: string): Promise<WorkflowExecution> {
  return v1Fetch<WorkflowExecution>(`/admin/executions/${encodeURIComponent(executionId)}/cancel`, {
    method: 'POST',
  });
}

// --- Logs ---

export function apiFetchLogs(workflowId: string, filter?: LogFilter): Promise<ExecutionLog[]> {
  const params = new URLSearchParams();
  if (filter?.level) params.set('level', filter.level);
  if (filter?.executionId) params.set('execution_id', filter.executionId);
  if (filter?.module) params.set('module', filter.module);
  if (filter?.since) params.set('since', filter.since);
  const qs = params.toString();
  return v1Fetch<ExecutionLog[]>(
    `/admin/workflows/${encodeURIComponent(workflowId)}/logs${qs ? '?' + qs : ''}`,
  );
}

export function createLogStream(workflowId: string, token: string): EventSource {
  const url = `${V1_BASE}/admin/workflows/${encodeURIComponent(workflowId)}/logs/stream?token=${encodeURIComponent(token)}`;
  return new EventSource(url);
}

// --- Events ---

export function apiFetchEvents(workflowId: string): Promise<WorkflowExecution[]> {
  return v1Fetch<WorkflowExecution[]>(`/admin/workflows/${encodeURIComponent(workflowId)}/events`);
}

export function createEventStream(workflowId: string, token: string): EventSource {
  const url = `${V1_BASE}/admin/workflows/${encodeURIComponent(workflowId)}/events/stream?token=${encodeURIComponent(token)}`;
  return new EventSource(url);
}

// --- Audit ---

export function apiFetchAuditLog(filter?: AuditFilter): Promise<AuditEntry[]> {
  const params = new URLSearchParams();
  if (filter?.action) params.set('action', filter.action);
  if (filter?.resourceType) params.set('resource_type', filter.resourceType);
  if (filter?.since) params.set('since', filter.since);
  if (filter?.until) params.set('until', filter.until);
  const qs = params.toString();
  return v1Fetch<AuditEntry[]>(`/admin/audit${qs ? '?' + qs : ''}`);
}

// --- IAM ---

export function apiFetchIAMProviders(companyId: string): Promise<IAMProviderConfig[]> {
  return v1Fetch<IAMProviderConfig[]>(`/admin/iam/providers/${encodeURIComponent(companyId)}`);
}

export function apiCreateIAMProvider(companyId: string, data: Partial<IAMProviderConfig>): Promise<IAMProviderConfig> {
  return v1Fetch<IAMProviderConfig>('/admin/iam/providers', {
    method: 'POST',
    body: JSON.stringify({ ...data, company_id: companyId }),
  });
}

export function apiUpdateIAMProvider(providerId: string, data: Partial<IAMProviderConfig>): Promise<IAMProviderConfig> {
  return v1Fetch<IAMProviderConfig>(`/admin/iam/providers/${encodeURIComponent(providerId)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export function apiDeleteIAMProvider(providerId: string): Promise<void> {
  return v1Fetch<void>(`/admin/iam/providers/${encodeURIComponent(providerId)}`, { method: 'DELETE' });
}

export function apiTestIAMProvider(providerId: string): Promise<{ success: boolean; message: string }> {
  return v1Fetch<{ success: boolean; message: string }>(`/admin/iam/providers/${encodeURIComponent(providerId)}/test`, {
    method: 'POST',
  });
}

export function apiFetchIAMRoleMappings(providerId: string): Promise<IAMRoleMapping[]> {
  return v1Fetch<IAMRoleMapping[]>(`/admin/iam/providers/${encodeURIComponent(providerId)}/mappings`);
}

export function apiCreateIAMRoleMapping(providerId: string, data: Partial<IAMRoleMapping>): Promise<IAMRoleMapping> {
  return v1Fetch<IAMRoleMapping>(`/admin/iam/providers/${encodeURIComponent(providerId)}/mappings`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function apiDeleteIAMRoleMapping(mappingId: string): Promise<void> {
  return v1Fetch<void>(`/admin/iam/mappings/${encodeURIComponent(mappingId)}`, { method: 'DELETE' });
}

// --- Runtime Instances ---

export interface RuntimeInstanceResponse {
  id: string;
  name: string;
  config_path: string;
  work_dir: string;
  status: string;
  started_at: string;
  error?: string;
}

export function apiFetchRuntimeInstances(): Promise<{ instances: RuntimeInstanceResponse[]; total: number }> {
  return v1Fetch<{ instances: RuntimeInstanceResponse[]; total: number }>('/admin/runtime/instances');
}

export function apiStopRuntimeInstance(instanceId: string): Promise<{ status: string }> {
  return v1Fetch<{ status: string }>(`/admin/runtime/instances/${encodeURIComponent(instanceId)}/stop`, {
    method: 'POST',
  });
}
