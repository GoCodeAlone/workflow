// Types
export type {
  ModuleConfig,
  WorkflowConfig,
  HTTPWorkflowConfig,
  MessagingWorkflowConfig,
  StateMachineWorkflowConfig,
  EventWorkflowConfig,
  WorkflowEdgeData,
  WorkflowTab,
  CrossWorkflowLink,
  ModuleTypeInfo,
  IOPort,
  IOSignature,
  ConfigFieldDef,
  ModuleCategory,
} from './types/workflow.ts';
export { MODULE_TYPE_MAP, MODULE_TYPES, CATEGORIES, CATEGORY_COLORS } from './types/workflow.ts';

export type {
  ExecutionStatus,
  StepStatus,
  LogLevel,
  IAMProviderType,
  WorkflowExecution,
  ExecutionStep,
  ExecutionLog,
  AuditEntry,
  IAMProviderConfig,
  IAMRoleMapping,
  WorkflowDashSummary,
  SystemDashboard as SystemDashboardData,
  WorkflowDashboardResponse,
  ExecutionFilter,
  LogFilter,
  AuditFilter,
  ActiveView,
} from './types/observability.ts';

// API client
export type {
  ValidationResult,
  GenerateResponse,
  ComponentSpec,
  WorkflowSuggestion,
  DynamicComponent,
  TokenResponse,
  ApiUser,
  AdminUser,
  ApiCompany,
  ApiProject,
  ApiWorkflowRecord,
  ApiWorkflowVersion,
  ApiMembership,
  RuntimeInstanceResponse,
} from './utils/api.ts';
export {
  getWorkflowConfig,
  saveWorkflowConfig,
  getModuleTypes,
  validateWorkflow,
  generateWorkflow,
  generateComponent,
  suggestWorkflows,
  listDynamicComponents,
  createDynamicComponent,
  deleteDynamicComponent,
  apiLogin,
  apiRegister,
  apiRefreshToken,
  apiLogout,
  apiGetMe,
  apiUpdateMe,
  apiListUsers,
  apiCreateUser,
  apiDeleteUser,
  apiUpdateUserRole,
  apiListCompanies,
  apiCreateCompany,
  apiGetCompany,
  apiListOrgs,
  apiCreateOrg,
  apiListProjects,
  apiListAllProjects,
  apiCreateProject,
  apiListWorkflows,
  apiCreateWorkflow,
  apiGetWorkflow,
  apiUpdateWorkflow,
  apiDeleteWorkflow,
  apiDeployWorkflow,
  apiStopWorkflow,
  apiLoadWorkflowFromPath,
  apiGetWorkflowStatus,
  apiListVersions,
  apiListPermissions,
  apiShareWorkflow,
  apiFetchDashboard,
  apiFetchWorkflowDashboard,
  apiFetchExecutions,
  apiFetchExecutionDetail,
  apiFetchExecutionSteps,
  apiTriggerExecution,
  apiCancelExecution,
  apiFetchLogs,
  createLogStream,
  apiFetchEvents,
  createEventStream,
  apiFetchAuditLog,
  apiFetchIAMProviders,
  apiCreateIAMProvider,
  apiUpdateIAMProvider,
  apiDeleteIAMProvider,
  apiTestIAMProvider,
  apiFetchIAMRoleMappings,
  apiCreateIAMRoleMapping,
  apiDeleteIAMRoleMapping,
  apiFetchRuntimeInstances,
  apiStopRuntimeInstance,
} from './utils/api.ts';

// Auth store
export type { User } from './store/authStore.ts';
export { default as useAuthStore } from './store/authStore.ts';

// Workflow store
export type { WorkflowNodeData, WorkflowNode } from './store/workflowStore.ts';
export { default as useWorkflowStore } from './store/workflowStore.ts';

// Module schema store
export { default as useModuleSchemaStore } from './store/moduleSchemaStore.ts';

// Observability store
export { default as useObservabilityStore } from './store/observabilityStore.ts';

// UI layout store
export { default as useUILayoutStore } from './store/uiLayoutStore.ts';

// Utility functions
export { layoutNodes } from './utils/autoLayout.ts';
export {
  isTypeCompatible,
  getOutputTypes,
  getInputTypes,
  getCompatibleNodes,
  canAcceptIncoming,
  canAcceptOutgoing,
  getCompatibleModuleTypes,
  getCompatibleSourceModuleTypes,
  countIncoming,
  countOutgoing,
  isPipelineFlowConnection,
} from './utils/connectionCompatibility.ts';
export { computeContainerView, autoGroupOrphanedNodes } from './utils/grouping.ts';
export { nodesToConfig, configToNodes, nodeComponentType } from './utils/serialization.ts';
export { findSnapCandidate } from './utils/snapToConnect.ts';

// Auth components
export { default as LoginPage } from './components/auth/LoginPage.tsx';
export { default as OAuthButton } from './components/auth/OAuthButton.tsx';
export { default as SetupWizard } from './components/auth/SetupWizard.tsx';

// Layout components
export { default as CollapsiblePanel } from './components/layout/CollapsiblePanel.tsx';

// Canvas / Visual builder components
export { default as WorkflowCanvas } from './components/canvas/WorkflowCanvas.tsx';
export { default as ConnectionPicklist } from './components/canvas/ConnectionPicklist.tsx';
export { default as DeletableEdge } from './components/canvas/DeletableEdge.tsx';
export { default as EdgeContextMenu } from './components/canvas/EdgeContextMenu.tsx';
export { default as NodeContextMenu } from './components/canvas/NodeContextMenu.tsx';

// Workflow node types registry
export { nodeTypes } from './components/nodes/index.ts';

// Dashboard / Observability components
export { default as SystemDashboard } from './components/dashboard/SystemDashboard.tsx';
export { default as WorkflowDashboard } from './components/dashboard/WorkflowDashboard.tsx';

// Toast component
export { default as ToastContainer } from './components/toast/ToastContainer.tsx';
export type { Toast } from './components/toast/ToastContainer.tsx';
