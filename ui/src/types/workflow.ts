import type { Node, Edge as RFEdge } from '@xyflow/react';

export interface ModuleConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
  dependsOn?: string[];
  branches?: Record<string, string>;
  ui_position?: { x: number; y: number };
}

export interface WorkflowConfig {
  modules: ModuleConfig[];
  workflows: Record<string, unknown>;
  triggers: Record<string, unknown>;
  pipelines?: Record<string, unknown>;
}

// Workflow section types for edge extraction
export interface HTTPWorkflowConfig {
  server: string;
  router: string;
  routes?: Array<{
    method: string;
    path: string;
    handler: string;
    middlewares?: string[];
  }>;
}

export interface MessagingWorkflowConfig {
  broker: string;
  subscriptions?: Array<{
    topic: string;
    handler: string;
  }>;
}

export interface StateMachineWorkflowConfig {
  engine: string;
  definitions?: Array<{
    name: string;
    [key: string]: unknown;
  }>;
}

export interface EventWorkflowConfig {
  processor: string;
  handlers?: string[];
  adapters?: string[];
}

export interface IntegrationWorkflowConfig {
  registry: string;
  connectors?: string[];
}

// I/O Port types for component signatures
export interface IOPort {
  name: string;
  type: string;
  handleId?: string;
}

export interface IOSignature {
  inputs: IOPort[];
  outputs: IOPort[];
}

// Conditional node data (extends WorkflowNodeData from workflowStore)
export interface ConditionalNodeData {
  moduleType: string;
  label: string;
  config: Record<string, unknown>;
  conditionType: 'ifelse' | 'switch' | 'expression';
  expression: string;
  cases?: string[];
  synthesized?: boolean;
  [key: string]: unknown;
}

// Edge type classification
export type WorkflowEdgeType = 'dependency' | 'http-route' | 'messaging-subscription' | 'statemachine' | 'event' | 'conditional' | 'auto-wire' | 'middleware-chain' | 'pipeline-flow';

export interface WorkflowEdgeData extends Record<string, unknown> {
  edgeType: WorkflowEdgeType;
  label?: string;
  chainOrder?: number;
}

export type ModuleCategory =
  | 'http'
  | 'messaging'
  | 'statemachine'
  | 'events'
  | 'integration'
  | 'scheduling'
  | 'infrastructure'
  | 'middleware'
  | 'database'
  | 'observability'
  | 'pipeline'
  | 'cicd'
  | 'security'
  | 'deployment';

export interface ModuleTypeInfo {
  type: string;
  label: string;
  category: ModuleCategory;
  defaultConfig: Record<string, unknown>;
  configFields: ConfigFieldDef[];
  ioSignature?: IOSignature;
}

export interface ConfigFieldDef {
  key: string;
  label: string;
  type: 'string' | 'number' | 'boolean' | 'select' | 'json' | 'array' | 'map' | 'filepath' | 'sql';
  options?: string[];
  defaultValue?: unknown;
  description?: string;
  placeholder?: string;
  required?: boolean;
  group?: string;
  arrayItemType?: string; // element type for array fields ("string", "number")
  mapValueType?: string;  // value type for map fields ("string", "number")
  inheritFrom?: string;   // "{edgeType}.{sourceField}" pattern for config inheritance from connected nodes
  sensitive?: boolean;    // when true, render as password input with visibility toggle
}

export const CATEGORY_COLORS: Record<ModuleCategory, string> = {
  http: '#3b82f6',
  messaging: '#8b5cf6',
  statemachine: '#f59e0b',
  events: '#ef4444',
  integration: '#10b981',
  scheduling: '#6366f1',
  infrastructure: '#64748b',
  middleware: '#06b6d4',
  database: '#f97316',
  observability: '#84cc16',
  pipeline: '#e879f9',
  cicd: '#f472b6',
  security: '#fb923c',
  deployment: '#34d399',
};

export const MODULE_TYPES: ModuleTypeInfo[] = [
  // HTTP
  {
    type: 'http.server',
    label: 'HTTP Server',
    category: 'http',
    defaultConfig: { address: ':8080' },
    configFields: [
      { key: 'address', label: 'Address', type: 'string', defaultValue: ':8080' },
      { key: 'readTimeout', label: 'Read Timeout', type: 'string', defaultValue: '30s' },
      { key: 'writeTimeout', label: 'Write Timeout', type: 'string', defaultValue: '30s' },
    ],
    ioSignature: { inputs: [], outputs: [{ name: 'request', type: 'http.Request' }] },
  },
  {
    type: 'http.router',
    label: 'HTTP Router',
    category: 'http',
    defaultConfig: {},
    configFields: [
      { key: 'prefix', label: 'Path Prefix', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'routed', type: 'http.Request' }] },
  },
  {
    type: 'http.handler',
    label: 'HTTP Handler',
    category: 'http',
    defaultConfig: { method: 'GET', path: '/' },
    configFields: [
      { key: 'method', label: 'Method', type: 'select', options: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'] },
      { key: 'path', label: 'Path', type: 'string', defaultValue: '/' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'response', type: 'http.Response' }] },
  },
  {
    type: 'http.proxy',
    label: 'HTTP Proxy',
    category: 'http',
    defaultConfig: { target: '' },
    configFields: [
      { key: 'target', label: 'Target URL', type: 'string' },
      { key: 'pathRewrite', label: 'Path Rewrite', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'proxied', type: 'http.Response' }] },
  },
  {
    type: 'http.simple_proxy',
    label: 'Simple Proxy',
    category: 'http',
    defaultConfig: {},
    configFields: [
      { key: 'targets', label: 'Targets', type: 'map', mapValueType: 'string', description: 'Map of URL prefix to backend URL (e.g. /api -> http://localhost:3000)', placeholder: '/api=http://backend:8080' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'proxied', type: 'http.Response' }] },
  },
  {
    type: 'static.fileserver',
    label: 'Static File Server',
    category: 'http',
    defaultConfig: { prefix: '/', spaFallback: true, cacheMaxAge: 3600 },
    configFields: [
      { key: 'root', label: 'Root Directory', type: 'string', required: true, description: 'Path to the directory containing static files', placeholder: './ui/dist' },
      { key: 'prefix', label: 'URL Prefix', type: 'string', defaultValue: '/', description: 'URL path prefix to serve files under', placeholder: '/' },
      { key: 'spaFallback', label: 'SPA Fallback', type: 'boolean', defaultValue: true, description: 'When enabled, serves index.html for unmatched paths (for single-page apps)' },
      { key: 'cacheMaxAge', label: 'Cache Max-Age (sec)', type: 'number', defaultValue: 3600, description: 'Cache-Control max-age in seconds for static assets' },
      { key: 'router', label: 'Router Name', type: 'string', description: 'Explicit router module name to register on (auto-detected if omitted)', placeholder: 'my-router', inheritFrom: 'dependency.name' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'file', type: 'http.Response' }] },
  },
  {
    type: 'api.handler',
    label: 'API Handler',
    category: 'http',
    defaultConfig: { method: 'GET', path: '/api' },
    configFields: [
      { key: 'method', label: 'Method', type: 'select', options: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'] },
      { key: 'path', label: 'Path', type: 'string', defaultValue: '/api' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'response', type: 'JSON' }] },
  },
  // Middleware
  {
    type: 'http.middleware.auth',
    label: 'Auth Middleware',
    category: 'middleware',
    defaultConfig: { type: 'jwt' },
    configFields: [
      { key: 'type', label: 'Auth Type', type: 'select', options: ['jwt', 'basic', 'apikey'] },
      { key: 'secret', label: 'Secret', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'authed', type: 'http.Request' }] },
  },
  {
    type: 'http.middleware.logging',
    label: 'Logging Middleware',
    category: 'middleware',
    defaultConfig: { level: 'info' },
    configFields: [
      { key: 'level', label: 'Log Level', type: 'select', options: ['debug', 'info', 'warn', 'error'] },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'logged', type: 'http.Request' }] },
  },
  {
    type: 'http.middleware.ratelimit',
    label: 'Rate Limiter',
    category: 'middleware',
    defaultConfig: { rps: 100 },
    configFields: [
      { key: 'rps', label: 'Requests/sec', type: 'number', defaultValue: 100 },
      { key: 'burst', label: 'Burst', type: 'number', defaultValue: 200 },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'limited', type: 'http.Request' }] },
  },
  {
    type: 'http.middleware.cors',
    label: 'CORS Middleware',
    category: 'middleware',
    defaultConfig: { allowedOrigins: ['*'], allowedMethods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'] },
    configFields: [
      { key: 'allowedOrigins', label: 'Allowed Origins', type: 'array', arrayItemType: 'string', defaultValue: ['*'], description: 'Allowed origins' },
      { key: 'allowedMethods', label: 'Allowed Methods', type: 'array', arrayItemType: 'string', defaultValue: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'], description: 'Allowed HTTP methods' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'cors', type: 'http.Request' }] },
  },
  // Messaging
  {
    type: 'messaging.broker',
    label: 'Message Broker',
    category: 'messaging',
    defaultConfig: { provider: 'nats' },
    configFields: [
      { key: 'provider', label: 'Provider', type: 'select', options: ['nats', 'rabbitmq', 'kafka'] },
      { key: 'url', label: 'URL', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'message', type: '[]byte' }], outputs: [{ name: 'message', type: '[]byte' }] },
  },
  {
    type: 'messaging.handler',
    label: 'Message Handler',
    category: 'messaging',
    defaultConfig: { topic: '' },
    configFields: [
      { key: 'topic', label: 'Topic', type: 'string' },
      { key: 'queue', label: 'Queue Group', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'message', type: '[]byte' }], outputs: [{ name: 'result', type: '[]byte' }] },
  },
  {
    type: 'messaging.broker.eventbus',
    label: 'EventBus Bridge',
    category: 'messaging',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [{ name: 'event', type: 'Event' }], outputs: [{ name: 'message', type: '[]byte' }] },
  },
  // State Machine
  {
    type: 'statemachine.engine',
    label: 'State Machine',
    category: 'statemachine',
    defaultConfig: {},
    configFields: [
      { key: 'initialState', label: 'Initial State', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'event', type: 'Event' }], outputs: [{ name: 'transition', type: 'Transition' }] },
  },
  {
    type: 'state.tracker',
    label: 'State Tracker',
    category: 'statemachine',
    defaultConfig: {},
    configFields: [
      { key: 'store', label: 'Store Type', type: 'select', options: ['memory', 'redis', 'database'] },
    ],
    ioSignature: { inputs: [{ name: 'state', type: 'State' }], outputs: [{ name: 'tracked', type: 'State' }] },
  },
  {
    type: 'state.connector',
    label: 'State Connector',
    category: 'statemachine',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [{ name: 'state', type: 'State' }], outputs: [{ name: 'connected', type: 'State' }] },
  },
  {
    type: 'processing.step',
    label: 'Processing Step',
    category: 'statemachine',
    defaultConfig: { maxRetries: 2, retryBackoffMs: 1000, timeoutSeconds: 30 },
    configFields: [
      { key: 'componentId', label: 'Component ID', type: 'string', required: true, description: 'Service name of the component to execute', inheritFrom: 'dependency.name' },
      { key: 'successTransition', label: 'Success Transition', type: 'string', description: 'State transition to trigger on success', placeholder: 'completed' },
      { key: 'compensateTransition', label: 'Compensate Transition', type: 'string', description: 'State transition to trigger on failure for compensation', placeholder: 'failed' },
      { key: 'maxRetries', label: 'Max Retries', type: 'number', defaultValue: 2, description: 'Maximum number of retry attempts' },
      { key: 'retryBackoffMs', label: 'Retry Backoff (ms)', type: 'number', defaultValue: 1000, description: 'Base backoff duration in milliseconds between retries' },
      { key: 'timeoutSeconds', label: 'Timeout (sec)', type: 'number', defaultValue: 30, description: 'Maximum execution time per attempt in seconds' },
    ],
    ioSignature: { inputs: [{ name: 'input', type: 'any' }], outputs: [{ name: 'result', type: 'any' }, { name: 'transition', type: 'string' }] },
  },
  // Conditional (branching nodes)
  {
    type: 'conditional.ifelse',
    label: 'If/Else Branch',
    category: 'statemachine',
    defaultConfig: { expression: '', true_target: '', false_target: '' },
    configFields: [
      { key: 'expression', label: 'Condition Expression', type: 'string' },
      { key: 'true_target', label: 'True Target', type: 'string' },
      { key: 'false_target', label: 'False Target', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'input', type: 'any' }], outputs: [{ name: 'true', type: 'any', handleId: 'true' }, { name: 'false', type: 'any', handleId: 'false' }] },
  },
  {
    type: 'conditional.switch',
    label: 'Switch Branch',
    category: 'statemachine',
    defaultConfig: { expression: '', cases: [] },
    configFields: [
      { key: 'expression', label: 'Switch Expression', type: 'string' },
      { key: 'cases', label: 'Cases', type: 'array', arrayItemType: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'input', type: 'any' }], outputs: [{ name: 'default', type: 'any' }] },
  },
  {
    type: 'conditional.expression',
    label: 'Expression Branch',
    category: 'statemachine',
    defaultConfig: { expression: '', outputs: [] },
    configFields: [
      { key: 'expression', label: 'Expression', type: 'string' },
      { key: 'outputs', label: 'Output Labels', type: 'array', arrayItemType: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'input', type: 'any' }], outputs: [{ name: 'result', type: 'any' }] },
  },
  // Scheduling
  {
    type: 'scheduler.modular',
    label: 'Scheduler',
    category: 'scheduling',
    defaultConfig: {},
    configFields: [
      { key: 'interval', label: 'Interval', type: 'string', defaultValue: '1m' },
      { key: 'cron', label: 'Cron Expression', type: 'string' },
    ],
    ioSignature: { inputs: [], outputs: [{ name: 'tick', type: 'Time' }] },
  },
  // Infrastructure
  {
    type: 'auth.modular',
    label: 'Auth Service',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'provider', label: 'Provider', type: 'select', options: ['jwt', 'oauth2', 'apikey'] },
    ],
    ioSignature: { inputs: [{ name: 'credentials', type: 'Credentials' }], outputs: [{ name: 'token', type: 'Token' }] },
  },
  {
    type: 'eventbus.modular',
    label: 'Event Bus',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'bufferSize', label: 'Buffer Size', type: 'number', defaultValue: 1024 },
    ],
    ioSignature: { inputs: [{ name: 'event', type: 'Event' }], outputs: [{ name: 'event', type: 'Event' }] },
  },
  {
    type: 'cache.modular',
    label: 'Cache',
    category: 'infrastructure',
    defaultConfig: { provider: 'memory' },
    configFields: [
      { key: 'provider', label: 'Provider', type: 'select', options: ['memory', 'redis'] },
      { key: 'ttl', label: 'TTL', type: 'string', defaultValue: '5m' },
    ],
    ioSignature: { inputs: [{ name: 'key', type: 'string' }], outputs: [{ name: 'value', type: 'any' }] },
  },
  {
    type: 'chimux.router',
    label: 'Chi Mux Router',
    category: 'http',
    defaultConfig: {},
    configFields: [
      { key: 'prefix', label: 'Path Prefix', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'routed', type: 'http.Request' }] },
  },
  {
    type: 'eventlogger.modular',
    label: 'Event Logger',
    category: 'events',
    defaultConfig: {},
    configFields: [
      { key: 'output', label: 'Output', type: 'select', options: ['stdout', 'file', 'database'] },
    ],
    ioSignature: { inputs: [{ name: 'event', type: 'Event' }], outputs: [] },
  },
  {
    type: 'httpclient.modular',
    label: 'HTTP Client',
    category: 'integration',
    defaultConfig: {},
    configFields: [
      { key: 'baseURL', label: 'Base URL', type: 'string' },
      { key: 'timeout', label: 'Timeout', type: 'string', defaultValue: '30s' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'response', type: 'http.Response' }] },
  },
  {
    type: 'database.modular',
    label: 'Database',
    category: 'infrastructure',
    defaultConfig: { driver: 'postgres' },
    configFields: [
      { key: 'driver', label: 'Driver', type: 'select', options: ['postgres', 'mysql', 'sqlite'] },
      { key: 'dsn', label: 'DSN', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'query', type: 'SQL' }], outputs: [{ name: 'result', type: 'Rows' }] },
  },
  {
    type: 'jsonschema.modular',
    label: 'JSON Schema Validator',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'schema', label: 'Schema', type: 'json' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: 'JSON' }], outputs: [{ name: 'validated', type: 'JSON' }] },
  },
  // Database
  {
    type: 'database.workflow',
    label: 'Workflow Database',
    category: 'database',
    defaultConfig: { driver: 'postgres' },
    configFields: [
      { key: 'driver', label: 'Driver', type: 'select', options: ['postgres', 'mysql', 'sqlite'] },
      { key: 'dsn', label: 'DSN', type: 'string' },
      { key: 'maxOpenConns', label: 'Max Open Connections', type: 'number', defaultValue: 25 },
      { key: 'maxIdleConns', label: 'Max Idle Connections', type: 'number', defaultValue: 5 },
    ],
    ioSignature: { inputs: [{ name: 'query', type: 'SQL' }], outputs: [{ name: 'result', type: 'Rows' }] },
  },
  {
    type: 'persistence.store',
    label: 'Persistence Store',
    category: 'database',
    defaultConfig: { database: 'database' },
    configFields: [
      { key: 'database', label: 'Database Service', type: 'string', defaultValue: 'database', description: 'Name of the database module to use for storage', placeholder: 'database', inheritFrom: 'dependency.name' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: 'any' }], outputs: [{ name: 'persistence', type: 'PersistenceStore' }] },
  },
  // Observability
  {
    type: 'metrics.collector',
    label: 'Metrics Collector',
    category: 'observability',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [{ name: 'metrics', type: 'Metric[]' }], outputs: [] },
  },
  {
    type: 'health.checker',
    label: 'Health Checker',
    category: 'observability',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [], outputs: [{ name: 'status', type: 'HealthStatus' }] },
  },
  {
    type: 'log.collector',
    label: 'Log Collector',
    category: 'observability',
    defaultConfig: { logLevel: 'info', outputFormat: 'json', retentionDays: 7 },
    configFields: [
      { key: 'logLevel', label: 'Log Level', type: 'select', options: ['debug', 'info', 'warn', 'error'], defaultValue: 'info', description: 'Minimum log level to collect' },
      { key: 'outputFormat', label: 'Output Format', type: 'select', options: ['json', 'text'], defaultValue: 'json', description: 'Format for log output' },
      { key: 'retentionDays', label: 'Retention Days', type: 'number', defaultValue: 7, description: 'Number of days to retain log entries' },
    ],
    ioSignature: { inputs: [{ name: 'logs', type: 'LogEntry[]' }], outputs: [] },
  },
  {
    type: 'http.middleware.requestid',
    label: 'Request ID Middleware',
    category: 'middleware',
    defaultConfig: {},
    configFields: [
      { key: 'headerName', label: 'Header Name', type: 'string', defaultValue: 'X-Request-ID' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'tagged', type: 'http.Request' }] },
  },
  {
    type: 'http.middleware.securityheaders',
    label: 'Security Headers',
    category: 'middleware',
    defaultConfig: { frameOptions: 'DENY', contentTypeOptions: 'nosniff', hstsMaxAge: 31536000, referrerPolicy: 'strict-origin-when-cross-origin', permissionsPolicy: 'camera=(), microphone=(), geolocation=()' },
    configFields: [
      { key: 'contentSecurityPolicy', label: 'Content Security Policy', type: 'string', description: 'CSP header value', placeholder: "default-src 'self'", group: 'headers' },
      { key: 'frameOptions', label: 'X-Frame-Options', type: 'select', options: ['DENY', 'SAMEORIGIN'], defaultValue: 'DENY', description: 'Controls whether the page can be embedded in frames', group: 'headers' },
      { key: 'contentTypeOptions', label: 'X-Content-Type-Options', type: 'string', defaultValue: 'nosniff', description: 'Prevents MIME type sniffing', group: 'headers' },
      { key: 'hstsMaxAge', label: 'HSTS Max-Age (sec)', type: 'number', defaultValue: 31536000, description: 'HTTP Strict Transport Security max-age in seconds', group: 'headers' },
      { key: 'referrerPolicy', label: 'Referrer Policy', type: 'select', options: ['no-referrer', 'no-referrer-when-downgrade', 'origin', 'origin-when-cross-origin', 'same-origin', 'strict-origin', 'strict-origin-when-cross-origin', 'unsafe-url'], defaultValue: 'strict-origin-when-cross-origin', description: 'Controls the Referer header sent with requests', group: 'headers' },
      { key: 'permissionsPolicy', label: 'Permissions Policy', type: 'string', defaultValue: 'camera=(), microphone=(), geolocation=()', description: 'Controls which browser features are allowed', group: 'headers' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'secured', type: 'http.Request' }] },
  },
  {
    type: 'auth.jwt',
    label: 'JWT Auth',
    category: 'middleware',
    defaultConfig: { tokenExpiry: '24h', issuer: 'workflow' },
    configFields: [
      { key: 'secret', label: 'JWT Secret', type: 'string', required: true, description: 'Secret key for signing JWT tokens (supports $ENV_VAR expansion)', placeholder: '$JWT_SECRET', sensitive: true },
      { key: 'tokenExpiry', label: 'Token Expiry', type: 'string', defaultValue: '24h', description: 'Token expiration duration (e.g. 1h, 24h, 7d)', placeholder: '24h' },
      { key: 'issuer', label: 'Issuer', type: 'string', defaultValue: 'workflow', description: 'Token issuer claim', placeholder: 'workflow' },
      { key: 'seedFile', label: 'Seed Users File', type: 'string', description: 'Path to JSON file with initial user accounts', placeholder: 'data/users.json' },
      { key: 'responseFormat', label: 'Response Format', type: 'select', options: ['standard', 'oauth2'], description: 'Format of authentication response payloads' },
    ],
    ioSignature: { inputs: [{ name: 'credentials', type: 'Credentials' }], outputs: [{ name: 'auth', type: 'AuthService' }] },
  },
  // Integration additions
  {
    type: 'data.transformer',
    label: 'Data Transformer',
    category: 'integration',
    defaultConfig: {},
    configFields: [
      { key: 'pipelines', label: 'Pipeline Config', type: 'json' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: 'any' }], outputs: [{ name: 'transformed', type: 'any' }] },
  },
  {
    type: 'webhook.sender',
    label: 'Webhook Sender',
    category: 'integration',
    defaultConfig: { maxRetries: 3 },
    configFields: [
      { key: 'maxRetries', label: 'Max Retries', type: 'number', defaultValue: 3 },
      { key: 'initialBackoff', label: 'Initial Backoff', type: 'string', defaultValue: '1s' },
      { key: 'maxBackoff', label: 'Max Backoff', type: 'string', defaultValue: '60s' },
      { key: 'timeout', label: 'Timeout', type: 'string', defaultValue: '30s' },
    ],
    ioSignature: { inputs: [{ name: 'payload', type: 'JSON' }], outputs: [{ name: 'response', type: 'http.Response' }] },
  },
  // 3rd Party Integrations
  {
    type: 'notification.slack',
    label: 'Slack Notification',
    category: 'integration',
    defaultConfig: { username: 'workflow-bot' },
    configFields: [
      { key: 'webhookURL', label: 'Webhook URL', type: 'string' },
      { key: 'channel', label: 'Channel', type: 'string' },
      { key: 'username', label: 'Username', type: 'string', defaultValue: 'workflow-bot' },
    ],
    ioSignature: { inputs: [{ name: 'message', type: 'string' }], outputs: [{ name: 'sent', type: 'boolean' }] },
  },
  {
    type: 'storage.s3',
    label: 'S3 Storage',
    category: 'integration',
    defaultConfig: { region: 'us-east-1' },
    configFields: [
      { key: 'bucket', label: 'Bucket', type: 'string' },
      { key: 'region', label: 'Region', type: 'string', defaultValue: 'us-east-1' },
      { key: 'endpoint', label: 'Endpoint', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: '[]byte' }], outputs: [{ name: 'url', type: 'string' }] },
  },
  {
    type: 'messaging.nats',
    label: 'NATS Broker',
    category: 'messaging',
    defaultConfig: { url: 'nats://localhost:4222' },
    configFields: [
      { key: 'url', label: 'URL', type: 'string', defaultValue: 'nats://localhost:4222' },
    ],
    ioSignature: { inputs: [{ name: 'message', type: '[]byte' }], outputs: [{ name: 'message', type: '[]byte' }] },
  },
  {
    type: 'messaging.kafka',
    label: 'Kafka Broker',
    category: 'messaging',
    defaultConfig: { brokers: ['localhost:9092'] },
    configFields: [
      { key: 'brokers', label: 'Brokers', type: 'array', arrayItemType: 'string', defaultValue: ['localhost:9092'], placeholder: 'localhost:9092' },
      { key: 'groupID', label: 'Group ID', type: 'string' },
    ],
    ioSignature: { inputs: [{ name: 'message', type: '[]byte' }], outputs: [{ name: 'message', type: '[]byte' }] },
  },
  {
    type: 'observability.otel',
    label: 'OpenTelemetry',
    category: 'observability',
    defaultConfig: { endpoint: 'localhost:4318', serviceName: 'workflow' },
    configFields: [
      { key: 'endpoint', label: 'OTLP Endpoint', type: 'string', defaultValue: 'localhost:4318' },
      { key: 'serviceName', label: 'Service Name', type: 'string', defaultValue: 'workflow' },
    ],
    ioSignature: { inputs: [{ name: 'spans', type: 'Span[]' }], outputs: [{ name: 'exported', type: 'boolean' }] },
  },
  // Storage
  {
    type: 'storage.local',
    label: 'Local Storage',
    category: 'integration',
    defaultConfig: { rootDir: './data/storage' },
    configFields: [
      { key: 'rootDir', label: 'Root Directory', type: 'string', defaultValue: './data/storage' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: '[]byte' }], outputs: [{ name: 'storage', type: 'StorageProvider' }] },
  },
  {
    type: 'storage.gcs',
    label: 'GCS Storage',
    category: 'integration',
    defaultConfig: {},
    configFields: [
      { key: 'bucket', label: 'Bucket', type: 'string', required: true },
      { key: 'project', label: 'GCP Project', type: 'string' },
      { key: 'credentialsFile', label: 'Credentials File', type: 'filepath' },
    ],
    ioSignature: { inputs: [{ name: 'data', type: '[]byte' }], outputs: [{ name: 'storage', type: 'StorageProvider' }] },
  },
  // Secrets
  {
    type: 'secrets.vault',
    label: 'Vault Secrets',
    category: 'infrastructure',
    defaultConfig: { mountPath: 'secret' },
    configFields: [
      { key: 'address', label: 'Vault Address', type: 'string', required: true, placeholder: 'https://vault.example.com:8200' },
      { key: 'token', label: 'Vault Token', type: 'string', required: true, placeholder: '${VAULT_TOKEN}', sensitive: true },
      { key: 'mountPath', label: 'Mount Path', type: 'string', defaultValue: 'secret', placeholder: 'secret' },
      { key: 'namespace', label: 'Namespace', type: 'string', placeholder: 'admin' },
    ],
    ioSignature: { inputs: [], outputs: [{ name: 'secrets', type: 'SecretProvider' }] },
  },
  {
    type: 'secrets.aws',
    label: 'AWS Secrets Manager',
    category: 'infrastructure',
    defaultConfig: { region: 'us-east-1' },
    configFields: [
      { key: 'region', label: 'AWS Region', type: 'string', defaultValue: 'us-east-1', placeholder: 'us-east-1' },
      { key: 'accessKeyId', label: 'Access Key ID', type: 'string', placeholder: '${AWS_ACCESS_KEY_ID}', sensitive: true },
      { key: 'secretAccessKey', label: 'Secret Access Key', type: 'string', placeholder: '${AWS_SECRET_ACCESS_KEY}', sensitive: true },
    ],
    ioSignature: { inputs: [], outputs: [{ name: 'secrets', type: 'SecretProvider' }] },
  },
  // OpenAPI
  {
    type: 'openapi.generator',
    label: 'OpenAPI Generator',
    category: 'integration',
    defaultConfig: { title: 'Workflow API', version: '1.0.0' },
    configFields: [
      { key: 'title', label: 'API Title', type: 'string', defaultValue: 'Workflow API', placeholder: 'My API' },
      { key: 'version', label: 'API Version', type: 'string', defaultValue: '1.0.0', placeholder: '1.0.0' },
      { key: 'description', label: 'Description', type: 'string', placeholder: 'API generated from workflow routes' },
      { key: 'servers', label: 'Server URLs', type: 'array', arrayItemType: 'string', placeholder: 'http://localhost:8080' },
    ],
    ioSignature: { inputs: [{ name: 'routes', type: 'RouteConfig' }], outputs: [{ name: 'spec', type: 'OpenAPISpec' }] },
  },
  {
    type: 'openapi.consumer',
    label: 'OpenAPI Consumer',
    category: 'integration',
    defaultConfig: {},
    configFields: [
      { key: 'specUrl', label: 'Spec URL', type: 'string', placeholder: 'https://api.example.com/openapi.json' },
      { key: 'specFile', label: 'Spec File', type: 'filepath', placeholder: 'specs/external-api.json' },
      { key: 'fieldMapping', label: 'Field Mapping', type: 'map', mapValueType: 'string', description: 'Custom field name mapping between local and external schemas', group: 'advanced' },
    ],
    ioSignature: { inputs: [{ name: 'spec', type: 'OpenAPISpec' }], outputs: [{ name: 'client', type: 'ExternalAPIClient' }] },
  },
  // Admin Infrastructure
  {
    type: 'storage.sqlite',
    label: 'SQLite Storage',
    category: 'database',
    defaultConfig: { dbPath: 'data/workflow.db', maxConnections: 5, walMode: true },
    configFields: [
      { key: 'dbPath', label: 'Database Path', type: 'string', defaultValue: 'data/workflow.db', placeholder: 'data/workflow.db' },
      { key: 'maxConnections', label: 'Max Connections', type: 'number', defaultValue: 5 },
      { key: 'walMode', label: 'WAL Mode', type: 'boolean', defaultValue: true },
    ],
    ioSignature: { inputs: [], outputs: [{ name: 'database', type: 'sql.DB' }] },
  },
  {
    type: 'auth.user-store',
    label: 'User Store',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [{ name: 'credentials', type: 'Credentials' }], outputs: [{ name: 'user-store', type: 'UserStore' }] },
  },
  {
    type: 'httpserver.modular',
    label: 'Modular HTTP Server',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [],
    ioSignature: { inputs: [], outputs: [{ name: 'http-server', type: 'net.Listener' }] },
  },
  {
    type: 'dynamic.component',
    label: 'Dynamic Component',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'componentId', label: 'Component ID', type: 'string', description: 'ID to look up in the dynamic component registry (defaults to module name)' },
      { key: 'source', label: 'Source File', type: 'string', description: 'Path to Go source file to load dynamically', placeholder: 'components/my_processor.go' },
      { key: 'provides', label: 'Provides Services', type: 'array', arrayItemType: 'string', description: 'Service names this component provides', placeholder: 'my-service' },
      { key: 'requires', label: 'Requires Services', type: 'array', arrayItemType: 'string', description: 'Service names this component depends on', placeholder: 'database' },
    ],
    ioSignature: { inputs: [{ name: 'input', type: 'any' }], outputs: [{ name: 'output', type: 'any' }] },
  },
  {
    type: 'workflow.registry',
    label: 'Workflow Registry',
    category: 'infrastructure',
    defaultConfig: { storageBackend: '' },
    configFields: [
      { key: 'storageBackend', label: 'Storage Backend', type: 'string', defaultValue: '', placeholder: 'admin-db', description: 'Name of a storage.sqlite module to share its DB connection', inheritFrom: 'dependency.name' },
    ],
    ioSignature: { inputs: [{ name: 'storageBackend', type: 'SQLiteStorage' }], outputs: [{ name: 'registry', type: 'WorkflowRegistry' }] },
  },
  // Pipeline step types
  {
    type: 'step.validate',
    label: 'Validate',
    category: 'pipeline',
    defaultConfig: { strategy: 'required_fields', required_fields: [] },
    configFields: [
      { key: 'strategy', label: 'Strategy', type: 'select', options: ['required_fields', 'json_schema'], defaultValue: 'required_fields', description: 'Validation strategy' },
      { key: 'required_fields', label: 'Required Fields', type: 'array', arrayItemType: 'string', description: 'Fields that must be present in the pipeline context' },
      { key: 'schema', label: 'JSON Schema', type: 'json', description: 'JSON Schema for json_schema strategy' },
    ],
  },
  {
    type: 'step.set',
    label: 'Set Values',
    category: 'pipeline',
    defaultConfig: { values: {} },
    configFields: [
      { key: 'values', label: 'Values', type: 'map', mapValueType: 'string', description: 'Key-value pairs to set in the pipeline context (supports {{ template }} expressions)' },
    ],
  },
  {
    type: 'step.log',
    label: 'Log',
    category: 'pipeline',
    defaultConfig: { level: 'info', message: '' },
    configFields: [
      { key: 'level', label: 'Level', type: 'select', options: ['debug', 'info', 'warn', 'error'], defaultValue: 'info' },
      { key: 'message', label: 'Message', type: 'string', description: 'Log message (supports {{ template }} expressions)', required: true },
    ],
  },
  {
    type: 'step.conditional',
    label: 'Conditional Route',
    category: 'pipeline',
    defaultConfig: { field: '', routes: {}, default: '' },
    configFields: [
      { key: 'field', label: 'Field', type: 'string', description: 'Field to evaluate (supports {{ template }} expressions)', required: true },
      { key: 'routes', label: 'Routes', type: 'map', mapValueType: 'string', description: 'Map of field values to target step names' },
      { key: 'default', label: 'Default Step', type: 'string', description: 'Step to route to when no match is found' },
    ],
  },
  {
    type: 'step.transform',
    label: 'Transform',
    category: 'pipeline',
    defaultConfig: { transformer: '', pipeline: '' },
    configFields: [
      { key: 'transformer', label: 'Transformer Service', type: 'string', description: 'Name of the DataTransformer module to use', inheritFrom: 'dependency.name' },
      { key: 'pipeline', label: 'Pipeline Name', type: 'string', description: 'Named transformation pipeline within the transformer' },
      { key: 'operations', label: 'Operations', type: 'json', description: 'Inline transformation operations (alternative to named pipeline)' },
    ],
  },
  {
    type: 'step.publish',
    label: 'Publish Event',
    category: 'pipeline',
    defaultConfig: { topic: '', payload: {} },
    configFields: [
      { key: 'topic', label: 'Topic', type: 'string', description: 'Event topic to publish to (supports {{ template }} expressions)', required: true },
      { key: 'payload', label: 'Payload', type: 'json', description: 'Event payload (supports {{ template }} expressions in string values)' },
      { key: 'broker', label: 'Broker Service', type: 'string', description: 'Name of the messaging broker module (optional, uses EventBus by default)', inheritFrom: 'dependency.name' },
    ],
  },
  {
    type: 'step.http_call',
    label: 'HTTP Call',
    category: 'pipeline',
    defaultConfig: { url: '', method: 'GET' },
    configFields: [
      { key: 'url', label: 'URL', type: 'string', description: 'Request URL (supports {{ template }} expressions)', required: true },
      { key: 'method', label: 'Method', type: 'select', options: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD'], defaultValue: 'GET' },
      { key: 'headers', label: 'Headers', type: 'map', mapValueType: 'string', description: 'Request headers' },
      { key: 'body', label: 'Body', type: 'json', description: 'Request body (supports {{ template }} expressions in string values)' },
      { key: 'timeout', label: 'Timeout', type: 'string', defaultValue: '30s', description: 'Request timeout' },
    ],
  },
];

export const MODULE_TYPE_MAP: Record<string, ModuleTypeInfo> = Object.fromEntries(
  MODULE_TYPES.map((t) => [t.type, t])
);

export const CATEGORIES: { key: ModuleCategory; label: string }[] = [
  { key: 'http', label: 'HTTP' },
  { key: 'middleware', label: 'Middleware' },
  { key: 'messaging', label: 'Messaging' },
  { key: 'statemachine', label: 'State Machine' },
  { key: 'events', label: 'Events' },
  { key: 'integration', label: 'Integration' },
  { key: 'scheduling', label: 'Scheduling' },
  { key: 'infrastructure', label: 'Infrastructure' },
  { key: 'database', label: 'Database' },
  { key: 'observability', label: 'Observability' },
  { key: 'pipeline', label: 'Pipeline Steps' },
  { key: 'cicd', label: 'CI/CD' },
  { key: 'security', label: 'Security' },
  { key: 'deployment', label: 'Deployment' },
];

// Multi-workflow tab management
export interface HistoryEntry {
  nodes: Node[];
  edges: RFEdge[];
}

export interface WorkflowTab {
  id: string;
  name: string;
  nodes: Node[];
  edges: RFEdge[];
  undoStack: HistoryEntry[];
  redoStack: HistoryEntry[];
  dirty: boolean;
}

// Cross-workflow event links
export interface CrossWorkflowLink {
  id: string;
  fromWorkflowId: string;
  fromNodeId: string;
  toWorkflowId: string;
  toNodeId: string;
  eventPattern?: string;
  label?: string;
}
