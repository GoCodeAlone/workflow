/**
 * @gocodalone/workflow-sdk
 *
 * TypeScript client SDK for the GoCodeAlone Workflow engine.
 *
 * @example
 * ```ts
 * import { createClient } from "@gocodalone/workflow-sdk";
 *
 * const client = createClient({
 *   baseURL: "http://localhost:8080",
 *   apiKey: "optional-api-key",
 * });
 *
 * // List workflows
 * const workflows = await client.listWorkflows();
 *
 * // Execute a workflow
 * const execution = await client.executeWorkflow("my-workflow", {
 *   order_id: "12345",
 * });
 *
 * // Stream execution events via SSE
 * for await (const event of client.streamExecution(execution.id)) {
 *   console.log(`[${event.event}] ${event.data}`);
 * }
 * ```
 *
 * @packageDocumentation
 */

export { createClient, WorkflowError } from "./client.js";
export type { WorkflowClient } from "./client.js";
export type {
  ClientOptions,
  DLQEntry,
  DLQFilter,
  Execution,
  ExecutionFilter,
  ExecutionStatus,
  HealthCheck,
  HealthStatus,
  ModuleConfig,
  SSEEvent,
  StepConfig,
  StepEventData,
  StepExecution,
  TriggerConfig,
  Workflow,
  WorkflowConfig,
  WorkflowDefinition,
  WorkflowStatus,
} from "./types.js";
