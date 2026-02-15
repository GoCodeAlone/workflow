import { create } from 'zustand';
import type { ModuleTypeInfo, ConfigFieldDef, ModuleCategory, IOSignature } from '../types/workflow.ts';
import { MODULE_TYPES, MODULE_TYPE_MAP as STATIC_MODULE_TYPE_MAP } from '../types/workflow.ts';

// Shape of a server-side I/O port definition
interface ServerIODef {
  name: string;
  type: string;
  description?: string;
}

// Shape of a server-side module schema (from /api/v1/module-schemas)
interface ServerModuleSchema {
  type: string;
  label: string;
  category: string;
  description?: string;
  inputs?: ServerIODef[];
  outputs?: ServerIODef[];
  configFields: ServerConfigField[];
  defaultConfig?: Record<string, unknown>;
}

interface ServerConfigField {
  key: string;
  label: string;
  type: string; // "string" | "number" | "boolean" | "select" | "json" | "duration" | "array" | "map"
  description?: string;
  required?: boolean;
  defaultValue?: unknown;
  options?: string[];
  placeholder?: string;
  group?: string;
  arrayItemType?: string;
  mapValueType?: string;
  inheritFrom?: string;
  sensitive?: boolean;
}

interface ModuleSchemaState {
  /** Whether schemas have been loaded from the server */
  loaded: boolean;
  /** Whether loading is in progress */
  loading: boolean;
  /** Server-provided schemas keyed by module type */
  serverSchemas: Record<string, ServerModuleSchema>;
  /** Merged MODULE_TYPES array (server schemas take priority for configFields) */
  moduleTypes: ModuleTypeInfo[];
  /** Merged MODULE_TYPE_MAP */
  moduleTypeMap: Record<string, ModuleTypeInfo>;
  /** Fetch schemas from server and merge with static definitions */
  fetchSchemas: () => Promise<void>;
}

/** Map server field types to UI field types */
function mapFieldType(serverType: string): ConfigFieldDef['type'] {
  switch (serverType) {
    case 'string':
    case 'duration':
      return 'string';
    case 'number':
      return 'number';
    case 'boolean':
      return 'boolean';
    case 'select':
      return 'select';
    case 'array':
      return 'array';
    case 'map':
      return 'map';
    case 'json':
      return 'json';
    case 'filepath':
      return 'filepath';
    default:
      return 'string';
  }
}

/** Convert server config fields to UI config fields */
function convertFields(serverFields: ServerConfigField[]): ConfigFieldDef[] {
  return serverFields.map((f) => ({
    key: f.key,
    label: f.label,
    type: mapFieldType(f.type),
    options: f.options,
    defaultValue: f.defaultValue,
    description: f.description,
    placeholder: f.placeholder,
    required: f.required,
    group: f.group,
    arrayItemType: f.arrayItemType,
    mapValueType: f.mapValueType,
    inheritFrom: f.inheritFrom,
    sensitive: f.sensitive,
  }));
}

/** Convert server I/O definitions to an IOSignature for UI rendering */
function convertIOSignature(inputs?: ServerIODef[], outputs?: ServerIODef[]): IOSignature | undefined {
  const ins = inputs ?? [];
  const outs = outputs ?? [];
  if (ins.length === 0 && outs.length === 0) return undefined;
  return {
    inputs: ins.map((p) => ({ name: p.name, type: p.type })),
    outputs: outs.map((p) => ({ name: p.name, type: p.type })),
  };
}

const VALID_CATEGORIES: ModuleCategory[] = [
  'http', 'messaging', 'statemachine', 'events', 'integration',
  'scheduling', 'infrastructure', 'middleware', 'database', 'observability',
  'pipeline',
];

function normalizeCategory(cat: string): ModuleCategory {
  if (VALID_CATEGORIES.includes(cat as ModuleCategory)) {
    return cat as ModuleCategory;
  }
  return 'infrastructure';
}

/** Merge server schemas with static MODULE_TYPES.
 * Server schemas take priority for: configFields, defaultConfig, label, category, description.
 * Static definitions are preserved for: ioSignature, conditional types.
 * Server-only types (not in static) are added as new entries.
 */
function mergeSchemas(
  staticTypes: ModuleTypeInfo[],
  serverSchemas: Record<string, ServerModuleSchema>,
): ModuleTypeInfo[] {
  const merged: ModuleTypeInfo[] = [];
  const seen = new Set<string>();

  // Start with static types, overlaying server fields
  for (const staticType of staticTypes) {
    seen.add(staticType.type);
    const server = serverSchemas[staticType.type];
    if (server) {
      const serverIO = convertIOSignature(server.inputs, server.outputs);
      merged.push({
        ...staticType,
        label: server.label || staticType.label,
        category: normalizeCategory(server.category || staticType.category),
        configFields: server.configFields.length > 0 ? convertFields(server.configFields) : staticType.configFields,
        defaultConfig: server.defaultConfig ?? staticType.defaultConfig,
        ioSignature: serverIO ?? staticType.ioSignature,
      });
    } else {
      merged.push(staticType);
    }
  }

  // Add server-only types not in static definitions
  for (const [type, server] of Object.entries(serverSchemas)) {
    if (!seen.has(type)) {
      merged.push({
        type,
        label: server.label,
        category: normalizeCategory(server.category),
        configFields: convertFields(server.configFields),
        defaultConfig: server.defaultConfig ?? {},
        ioSignature: convertIOSignature(server.inputs, server.outputs),
      });
    }
  }

  return merged;
}

const useModuleSchemaStore = create<ModuleSchemaState>((set, get) => ({
  loaded: false,
  loading: false,
  serverSchemas: {},
  moduleTypes: MODULE_TYPES,
  moduleTypeMap: STATIC_MODULE_TYPE_MAP,

  fetchSchemas: async () => {
    if (get().loading) return;
    set({ loading: true });
    try {
      const token = localStorage.getItem('auth_token');
      const headers: Record<string, string> = {};
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }
      const res = await fetch('/api/v1/module-schemas', { headers });
      if (!res.ok) {
        console.warn('Failed to fetch module schemas, using static defaults');
        set({ loading: false, loaded: true });
        return;
      }
      const schemas: Record<string, ServerModuleSchema> = await res.json();
      const merged = mergeSchemas(MODULE_TYPES, schemas);
      const mergedMap = Object.fromEntries(merged.map((t) => [t.type, t]));
      set({
        serverSchemas: schemas,
        moduleTypes: merged,
        moduleTypeMap: mergedMap,
        loaded: true,
        loading: false,
      });
    } catch (e) {
      console.warn('Error fetching module schemas:', e);
      set({ loading: false, loaded: true });
    }
  },
}));

export default useModuleSchemaStore;
