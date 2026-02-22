import { apiGet, apiPost, apiPut, apiDelete, getApiConfig } from '@gocodealone/workflow-ui/api';
import type { WorkflowConfig } from '../types/workflow.ts';
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
  WorkflowEventEntry,
} from '../types/observability.ts';

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

// --- Engine Management ---

export async function getWorkflowConfig(): Promise<WorkflowConfig> {
  return apiGet<WorkflowConfig>('/admin/engine/config');
}

export async function saveWorkflowConfig(config: WorkflowConfig): Promise<void> {
  await apiPut<void>('/admin/engine/config', config);
}

export async function getModuleTypes(): Promise<ModuleTypeInfo[]> {
  return apiGet<ModuleTypeInfo[]>('/admin/engine/modules');
}

export async function validateWorkflow(config: WorkflowConfig): Promise<ValidationResult> {
  return apiPost<ValidationResult>('/admin/engine/validate', config);
}

// --- AI ---

export async function generateWorkflow(intent: string): Promise<GenerateResponse> {
  return apiPost<GenerateResponse>('/admin/ai/generate', { intent });
}

export async function generateComponent(spec: ComponentSpec): Promise<string> {
  const { baseUrl } = getApiConfig();
  const token = localStorage.getItem('auth_token');
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${baseUrl}/admin/ai/component`, {
    method: 'POST',
    headers,
    body: JSON.stringify(spec),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`API ${res.status}: ${text}`);
  }
  return res.text();
}

export async function suggestWorkflows(useCase: string): Promise<WorkflowSuggestion[]> {
  return apiPost<WorkflowSuggestion[]>('/admin/ai/suggest', { useCase });
}

// --- Dynamic Components ---

export async function listDynamicComponents(): Promise<DynamicComponent[]> {
  return apiGet<DynamicComponent[]>('/admin/components');
}

export async function createDynamicComponent(name: string, source: string, language: string): Promise<void> {
  await apiPost<void>('/admin/components', { id: name, source, language });
}

export async function deleteDynamicComponent(name: string): Promise<void> {
  await apiDelete<void>(`/admin/components/${encodeURIComponent(name)}`);
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
  return apiPost<TokenResponse>('/auth/login', { email, password });
}

export function apiRegister(email: string, password: string, displayName: string): Promise<TokenResponse> {
  return apiPost<TokenResponse>('/auth/register', { email, password, display_name: displayName });
}

export function apiRefreshToken(refreshToken: string): Promise<TokenResponse> {
  return apiPost<TokenResponse>('/auth/refresh', { refresh_token: refreshToken });
}

export function apiLogout(): Promise<void> {
  return apiPost<void>('/auth/logout');
}

export function apiGetMe(): Promise<ApiUser> {
  return apiGet<ApiUser>('/auth/me');
}

export function apiUpdateMe(data: { display_name?: string; avatar_url?: string }): Promise<ApiUser> {
  return apiPut<ApiUser>('/auth/me', data);
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
  return apiGet<AdminUser[]>('/auth/users');
}

export function apiCreateUser(email: string, password: string, name: string, role: string): Promise<AdminUser> {
  return apiPost<AdminUser>('/auth/users', { email, password, name, role });
}

export function apiDeleteUser(id: string): Promise<void> {
  return apiDelete<void>(`/auth/users/${encodeURIComponent(id)}`);
}

export function apiUpdateUserRole(id: string, role: string): Promise<AdminUser> {
  return apiPut<AdminUser>(`/auth/users/${encodeURIComponent(id)}/role`, { role });
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
  return apiGet<ApiCompany[]>('/admin/companies');
}

export function apiCreateCompany(name: string, slug?: string): Promise<ApiCompany> {
  return apiPost<ApiCompany>('/admin/companies', { name, slug });
}

export function apiGetCompany(id: string): Promise<ApiCompany> {
  return apiGet<ApiCompany>(`/admin/companies/${encodeURIComponent(id)}`);
}

// --- Organizations ---

export function apiListOrgs(companyId: string): Promise<ApiCompany[]> {
  return apiGet<ApiCompany[]>(`/admin/companies/${encodeURIComponent(companyId)}/organizations`);
}

export function apiCreateOrg(companyId: string, name: string, slug?: string): Promise<ApiCompany> {
  return apiPost<ApiCompany>(`/admin/companies/${encodeURIComponent(companyId)}/organizations`, { name, slug });
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
  return apiGet<ApiProject[]>(`/admin/organizations/${encodeURIComponent(orgId)}/projects`);
}

export function apiListAllProjects(): Promise<ApiProject[]> {
  return apiGet<ApiProject[]>('/admin/projects');
}

export function apiCreateProject(orgId: string, name: string, slug?: string): Promise<ApiProject> {
  return apiPost<ApiProject>(`/admin/organizations/${encodeURIComponent(orgId)}/projects`, { name, slug });
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
    return apiGet<ApiWorkflowRecord[]>(`/admin/projects/${encodeURIComponent(projectId)}/workflows`);
  }
  return apiGet<ApiWorkflowRecord[]>('/admin/workflows');
}

export function apiCreateWorkflow(
  projectId: string,
  data: { name: string; slug?: string; description?: string; config_yaml?: string },
): Promise<ApiWorkflowRecord> {
  return apiPost<ApiWorkflowRecord>(`/admin/projects/${encodeURIComponent(projectId)}/workflows`, data);
}

export function apiGetWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return apiGet<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}`);
}

export function apiUpdateWorkflow(
  id: string,
  data: { name?: string; description?: string; config_yaml?: string },
): Promise<ApiWorkflowRecord> {
  return apiPut<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}`, data);
}

export function apiDeleteWorkflow(id: string): Promise<void> {
  return apiDelete<void>(`/admin/workflows/${encodeURIComponent(id)}`);
}

export function apiDeployWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return apiPost<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}/deploy`);
}

export function apiStopWorkflow(id: string): Promise<ApiWorkflowRecord> {
  return apiPost<ApiWorkflowRecord>(`/admin/workflows/${encodeURIComponent(id)}/stop`);
}

export function apiLoadWorkflowFromPath(
  projectId: string,
  path: string,
): Promise<ApiWorkflowRecord> {
  return apiPost<ApiWorkflowRecord>('/admin/workflows/load-from-path', { project_id: projectId, path });
}

export function apiGetWorkflowStatus(id: string): Promise<{ id: string; status: string; version: number }> {
  return apiGet<{ id: string; status: string; version: number }>(`/admin/workflows/${encodeURIComponent(id)}/status`);
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
  return apiGet<ApiWorkflowVersion[]>(`/admin/workflows/${encodeURIComponent(id)}/versions`);
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
  return apiGet<ApiMembership[]>(`/admin/workflows/${encodeURIComponent(id)}/permissions`);
}

export function apiShareWorkflow(id: string, userId: string, role: string): Promise<ApiMembership> {
  return apiPost<ApiMembership>(`/admin/workflows/${encodeURIComponent(id)}/permissions`, { user_id: userId, role });
}

// --- Dashboard ---

export function apiFetchDashboard(): Promise<SystemDashboard> {
  return apiGet<SystemDashboard>('/admin/dashboard');
}

export function apiFetchWorkflowDashboard(workflowId: string): Promise<WorkflowDashboardResponse> {
  return apiGet<WorkflowDashboardResponse>(`/admin/workflows/${encodeURIComponent(workflowId)}/dashboard`);
}

// --- Executions ---

export function apiFetchExecutions(workflowId: string, filter?: ExecutionFilter): Promise<WorkflowExecution[]> {
  const params = new URLSearchParams();
  if (filter?.status) params.set('status', filter.status);
  if (filter?.since) params.set('since', filter.since);
  if (filter?.until) params.set('until', filter.until);
  const qs = params.toString();
  return apiGet<WorkflowExecution[]>(
    `/admin/workflows/${encodeURIComponent(workflowId)}/executions${qs ? '?' + qs : ''}`,
  );
}

export function apiFetchExecutionDetail(executionId: string): Promise<WorkflowExecution> {
  return apiGet<WorkflowExecution>(`/admin/executions/${encodeURIComponent(executionId)}`);
}

export function apiFetchExecutionSteps(executionId: string): Promise<ExecutionStep[]> {
  return apiGet<ExecutionStep[]>(`/admin/executions/${encodeURIComponent(executionId)}/steps`);
}

export function apiTriggerExecution(workflowId: string, triggerData?: unknown): Promise<WorkflowExecution> {
  return apiPost<WorkflowExecution>(`/admin/workflows/${encodeURIComponent(workflowId)}/trigger`, { trigger_data: triggerData ?? {} });
}

export function apiCancelExecution(executionId: string): Promise<WorkflowExecution> {
  return apiPost<WorkflowExecution>(`/admin/executions/${encodeURIComponent(executionId)}/cancel`);
}

// --- Logs ---

export function apiFetchLogs(workflowId: string, filter?: LogFilter): Promise<ExecutionLog[]> {
  const params = new URLSearchParams();
  if (filter?.level) params.set('level', filter.level);
  if (filter?.executionId) params.set('execution_id', filter.executionId);
  if (filter?.module) params.set('module', filter.module);
  if (filter?.since) params.set('since', filter.since);
  const qs = params.toString();
  return apiGet<ExecutionLog[]>(
    `/admin/workflows/${encodeURIComponent(workflowId)}/logs${qs ? '?' + qs : ''}`,
  );
}

export function createLogStream(workflowId: string, token: string): EventSource {
  const { baseUrl } = getApiConfig();
  const url = `${baseUrl}/admin/workflows/${encodeURIComponent(workflowId)}/logs/stream?token=${encodeURIComponent(token)}`;
  return new EventSource(url);
}

// --- Events ---

export function apiFetchEvents(workflowId: string): Promise<WorkflowEventEntry[]> {
  return apiGet<WorkflowEventEntry[]>(`/admin/workflows/${encodeURIComponent(workflowId)}/events`);
}

export function createEventStream(workflowId: string, token: string): EventSource {
  const { baseUrl } = getApiConfig();
  const url = `${baseUrl}/admin/workflows/${encodeURIComponent(workflowId)}/events/stream?token=${encodeURIComponent(token)}`;
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
  return apiGet<AuditEntry[]>(`/admin/audit${qs ? '?' + qs : ''}`);
}

// --- IAM ---

export function apiFetchIAMProviders(companyId: string): Promise<IAMProviderConfig[]> {
  return apiGet<IAMProviderConfig[]>(`/admin/iam/providers/${encodeURIComponent(companyId)}`);
}

export function apiCreateIAMProvider(companyId: string, data: Partial<IAMProviderConfig>): Promise<IAMProviderConfig> {
  return apiPost<IAMProviderConfig>('/admin/iam/providers', { ...data, company_id: companyId });
}

export function apiUpdateIAMProvider(providerId: string, data: Partial<IAMProviderConfig>): Promise<IAMProviderConfig> {
  return apiPut<IAMProviderConfig>(`/admin/iam/providers/${encodeURIComponent(providerId)}`, data);
}

export function apiDeleteIAMProvider(providerId: string): Promise<void> {
  return apiDelete<void>(`/admin/iam/providers/${encodeURIComponent(providerId)}`);
}

export function apiTestIAMProvider(providerId: string): Promise<{ success: boolean; message: string }> {
  return apiPost<{ success: boolean; message: string }>(`/admin/iam/providers/${encodeURIComponent(providerId)}/test`);
}

export function apiFetchIAMRoleMappings(providerId: string): Promise<IAMRoleMapping[]> {
  return apiGet<IAMRoleMapping[]>(`/admin/iam/providers/${encodeURIComponent(providerId)}/mappings`);
}

export function apiCreateIAMRoleMapping(providerId: string, data: Partial<IAMRoleMapping>): Promise<IAMRoleMapping> {
  return apiPost<IAMRoleMapping>(`/admin/iam/providers/${encodeURIComponent(providerId)}/mappings`, data);
}

export function apiDeleteIAMRoleMapping(mappingId: string): Promise<void> {
  return apiDelete<void>(`/admin/iam/mappings/${encodeURIComponent(mappingId)}`);
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
  return apiGet<{ instances: RuntimeInstanceResponse[]; total: number }>('/admin/runtime/instances');
}

export function apiStopRuntimeInstance(instanceId: string): Promise<{ status: string }> {
  return apiPost<{ status: string }>(`/admin/runtime/instances/${encodeURIComponent(instanceId)}/stop`);
}
