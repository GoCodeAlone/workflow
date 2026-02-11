import type { Node, Edge as RFEdge } from '@xyflow/react';

export interface ModuleConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
  dependsOn?: string[];
  branches?: Record<string, string>;
}

export interface WorkflowConfig {
  modules: ModuleConfig[];
  workflows: Record<string, unknown>;
  triggers: Record<string, unknown>;
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
export type WorkflowEdgeType = 'dependency' | 'http-route' | 'messaging-subscription' | 'statemachine' | 'event' | 'conditional';

export interface WorkflowEdgeData extends Record<string, unknown> {
  edgeType: WorkflowEdgeType;
  label?: string;
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
  | 'observability';

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
  type: 'string' | 'number' | 'boolean' | 'select' | 'json';
  options?: string[];
  defaultValue?: unknown;
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
    defaultConfig: { allowOrigins: ['*'] },
    configFields: [
      { key: 'allowOrigins', label: 'Allowed Origins', type: 'string', defaultValue: '*' },
      { key: 'allowMethods', label: 'Allowed Methods', type: 'string', defaultValue: 'GET,POST,PUT,DELETE' },
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
      { key: 'cases', label: 'Cases', type: 'json' },
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
      { key: 'outputs', label: 'Output Labels', type: 'json' },
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
    type: 'http.middleware.requestid',
    label: 'Request ID Middleware',
    category: 'middleware',
    defaultConfig: {},
    configFields: [
      { key: 'headerName', label: 'Header Name', type: 'string', defaultValue: 'X-Request-ID' },
    ],
    ioSignature: { inputs: [{ name: 'request', type: 'http.Request' }], outputs: [{ name: 'tagged', type: 'http.Request' }] },
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
    defaultConfig: { brokers: 'localhost:9092' },
    configFields: [
      { key: 'brokers', label: 'Brokers', type: 'string', defaultValue: 'localhost:9092' },
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
