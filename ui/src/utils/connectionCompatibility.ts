import type { ModuleTypeInfo, IOPort } from '../types/workflow.ts';
import type { WorkflowNode } from '../store/workflowStore.ts';

/**
 * Type compatibility matrix. Maps output types to the set of input types
 * they can connect to (beyond exact match).
 */
const COERCION_RULES: Record<string, string[]> = {
  // Data types
  'http.Request': ['any', 'PipelineContext'],
  'http.Response': ['any', 'JSON', '[]byte'],
  'JSON': ['any', '[]byte', 'string'],
  '[]byte': ['any', 'string'],
  'Event': ['any', '[]byte', 'JSON'],
  'CloudEvent': ['any', 'Event', '[]byte', 'JSON'],
  'Transition': ['any', 'Event'],
  'State': ['any'],
  'string': ['any'],
  'boolean': ['any'],
  'Token': ['any', 'string'],
  'Credentials': ['any'],
  'Time': ['any', 'Event'],
  'SQL': ['any', 'string'],
  'Rows': ['any', 'JSON'],
  'HealthStatus': ['any', 'JSON'],
  'Metric[]': ['any'],
  'LogEntry': ['any', 'JSON'],
  'LogEntry[]': ['any'],
  '[]LogEntry': ['any'],
  'Span[]': ['any'],
  'Command': ['any', 'PipelineContext'],
  'RouteConfig': ['any', 'JSON'],
  'OpenAPISpec': ['any', 'JSON'],
  'SlackResponse': ['any', 'JSON'],
  'SQLiteStorage': ['any', 'sql.DB'],
  'func()': ['any'],

  // Pipeline types
  'PipelineContext': ['any', 'StepResult', 'PipelineContext'],
  'StepResult': ['any', 'PipelineContext', 'StepResult'],

  // Service/provider types (output by infrastructure modules, consumed by dependents)
  'prometheus.Metrics': ['any'],
  'net.Listener': ['any'],
  'Scheduler': ['any'],
  'AuthService': ['any'],
  'EventBus': ['any'],
  'Cache': ['any'],
  'http.Client': ['any'],
  'sql.DB': ['any'],
  'SchemaValidator': ['any'],
  'StorageProvider': ['any'],
  'SecretProvider': ['any'],
  'PersistenceStore': ['any'],
  'WorkflowRegistry': ['any'],
  'ExternalAPIClient': ['any'],
  'FileStore': ['any', 'StorageProvider'],
  'ObjectStore': ['any', 'StorageProvider'],
  'UserStore': ['any'],
  'trace.Span': ['any'],
  'trace.Tracer': ['any'],
};

/**
 * Check if an output type is compatible with an input type.
 * - Exact match always compatible
 * - 'any' on either side matches everything
 * - Coercion rules for specific type widening
 */
export function isTypeCompatible(outputType: string, inputType: string): boolean {
  if (outputType === inputType) return true;
  if (outputType === 'any' || inputType === 'any') return true;
  const coercions = COERCION_RULES[outputType];
  if (coercions && coercions.includes(inputType)) return true;
  return false;
}

/**
 * Get the output types from a module type's IO signature.
 */
export function getOutputTypes(info: ModuleTypeInfo): string[] {
  if (!info.ioSignature) return [];
  return info.ioSignature.outputs.map((p) => p.type);
}

/**
 * Get the input types from a module type's IO signature.
 */
export function getInputTypes(info: ModuleTypeInfo): string[] {
  if (!info.ioSignature) return [];
  return info.ioSignature.inputs.map((p) => p.type);
}

/**
 * Find module types whose inputs are compatible with a given output type.
 */
export function getCompatibleModuleTypes(
  outputType: string,
  moduleTypes: ModuleTypeInfo[],
): ModuleTypeInfo[] {
  return moduleTypes.filter((mt) => {
    const inputs = getInputTypes(mt);
    if (inputs.length === 0) return false;
    return inputs.some((inputType) => isTypeCompatible(outputType, inputType));
  });
}

/**
 * Find module types whose outputs are compatible with a given input type.
 */
export function getCompatibleSourceModuleTypes(
  inputType: string,
  moduleTypes: ModuleTypeInfo[],
): ModuleTypeInfo[] {
  return moduleTypes.filter((mt) => {
    const outputs = getOutputTypes(mt);
    if (outputs.length === 0) return false;
    return outputs.some((outType) => isTypeCompatible(outType, inputType));
  });
}

/**
 * Find existing nodes on canvas that could accept a connection from/to the source node.
 * When handleType is 'source', we look for nodes whose inputs match our outputs.
 * When handleType is 'target', we look for nodes whose outputs match our inputs.
 */
export function getCompatibleNodes(
  sourceNodeId: string,
  sourceOutputTypes: string[],
  handleType: 'source' | 'target',
  nodes: WorkflowNode[],
  moduleTypeMap: Record<string, ModuleTypeInfo>,
): string[] {
  const ids: string[] = [];

  for (const node of nodes) {
    if (node.id === sourceNodeId) continue;

    const info = moduleTypeMap[node.data.moduleType];
    if (!info?.ioSignature) continue;

    let compatible = false;
    if (handleType === 'source') {
      // We are dragging FROM source handle (output), looking for nodes with compatible inputs
      const targetInputs = info.ioSignature.inputs;
      compatible = targetInputs.some((inp: IOPort) =>
        sourceOutputTypes.some((outType) => isTypeCompatible(outType, inp.type)),
      );
    } else {
      // We are dragging FROM target handle (input), looking for nodes with compatible outputs
      const targetOutputs = info.ioSignature.outputs;
      compatible = targetOutputs.some((out: IOPort) =>
        sourceOutputTypes.some((inType) => isTypeCompatible(out.type, inType)),
      );
    }

    if (compatible) {
      ids.push(node.id);
    }
  }

  return ids;
}

/**
 * Determine if a connection between two module types should use the 'pipeline-flow' edge type.
 * Pipeline-flow applies when:
 * - Both source and target are step.* modules (step-to-step chaining)
 * - Source is a CQRS handler (api.query, api.command) and target is a step.* module
 */
export function isPipelineFlowConnection(sourceModuleType: string, targetModuleType: string): boolean {
  const isSourceStep = sourceModuleType.startsWith('step.');
  const isTargetStep = targetModuleType.startsWith('step.');
  const isSourceHandler = sourceModuleType === 'api.query' || sourceModuleType === 'api.command';

  return (isSourceStep && isTargetStep) || (isSourceHandler && isTargetStep);
}

/**
 * Get the primary output type label for display during connection drag.
 */
export function getPrimaryOutputType(info: ModuleTypeInfo | undefined): string {
  if (!info?.ioSignature) return '';
  const outputs = info.ioSignature.outputs;
  if (outputs.length === 0) return '';
  if (outputs.length === 1) return outputs[0].type;
  return outputs.map((o) => o.type).join(' | ');
}

/**
 * Get the primary input type label for display during connection drag.
 */
export function getPrimaryInputType(info: ModuleTypeInfo | undefined): string {
  if (!info?.ioSignature) return '';
  const inputs = info.ioSignature.inputs;
  if (inputs.length === 0) return '';
  if (inputs.length === 1) return inputs[0].type;
  return inputs.map((i) => i.type).join(' | ');
}
