import type { WorkflowConfig } from '../types/workflow.ts';

const API_BASE = '/api';

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

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`API ${res.status}: ${text}`);
  }
  return res.json();
}

export async function getWorkflowConfig(): Promise<WorkflowConfig> {
  return apiFetch<WorkflowConfig>('/workflow/config');
}

export async function saveWorkflowConfig(config: WorkflowConfig): Promise<void> {
  await apiFetch<void>('/workflow/config', {
    method: 'PUT',
    body: JSON.stringify(config),
  });
}

export async function getModuleTypes(): Promise<ModuleTypeInfo[]> {
  return apiFetch<ModuleTypeInfo[]>('/workflow/modules');
}

export async function validateWorkflow(config: WorkflowConfig): Promise<ValidationResult> {
  return apiFetch<ValidationResult>('/workflow/validate', {
    method: 'POST',
    body: JSON.stringify(config),
  });
}

export async function generateWorkflow(intent: string): Promise<GenerateResponse> {
  return apiFetch<GenerateResponse>('/ai/generate', {
    method: 'POST',
    body: JSON.stringify({ intent }),
  });
}

export async function generateComponent(spec: ComponentSpec): Promise<string> {
  const res = await fetch(`${API_BASE}/ai/component`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(spec),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`API ${res.status}: ${text}`);
  }
  return res.text();
}

export async function suggestWorkflows(useCase: string): Promise<WorkflowSuggestion[]> {
  return apiFetch<WorkflowSuggestion[]>('/ai/suggest', {
    method: 'POST',
    body: JSON.stringify({ useCase }),
  });
}

export async function listDynamicComponents(): Promise<DynamicComponent[]> {
  return apiFetch<DynamicComponent[]>('/dynamic/components');
}

export async function createDynamicComponent(name: string, source: string, language: string): Promise<void> {
  await apiFetch<void>('/dynamic/components', {
    method: 'POST',
    body: JSON.stringify({ name, source, language }),
  });
}

export async function deleteDynamicComponent(name: string): Promise<void> {
  await apiFetch<void>(`/dynamic/components/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
}
