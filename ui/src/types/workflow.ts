export interface ModuleConfig {
  name: string;
  type: string;
  config?: Record<string, unknown>;
  dependsOn?: string[];
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

// Edge type classification
export type WorkflowEdgeType = 'dependency' | 'http-route' | 'messaging-subscription' | 'statemachine' | 'event';

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
  },
  {
    type: 'http.router',
    label: 'HTTP Router',
    category: 'http',
    defaultConfig: {},
    configFields: [
      { key: 'prefix', label: 'Path Prefix', type: 'string' },
    ],
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
  },
  {
    type: 'http.middleware.logging',
    label: 'Logging Middleware',
    category: 'middleware',
    defaultConfig: { level: 'info' },
    configFields: [
      { key: 'level', label: 'Log Level', type: 'select', options: ['debug', 'info', 'warn', 'error'] },
    ],
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
  },
  {
    type: 'messaging.broker.eventbus',
    label: 'EventBus Bridge',
    category: 'messaging',
    defaultConfig: {},
    configFields: [],
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
  },
  {
    type: 'state.tracker',
    label: 'State Tracker',
    category: 'statemachine',
    defaultConfig: {},
    configFields: [
      { key: 'store', label: 'Store Type', type: 'select', options: ['memory', 'redis', 'database'] },
    ],
  },
  {
    type: 'state.connector',
    label: 'State Connector',
    category: 'statemachine',
    defaultConfig: {},
    configFields: [],
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
  },
  {
    type: 'eventbus.modular',
    label: 'Event Bus',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'bufferSize', label: 'Buffer Size', type: 'number', defaultValue: 1024 },
    ],
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
  },
  {
    type: 'chimux.router',
    label: 'Chi Mux Router',
    category: 'http',
    defaultConfig: {},
    configFields: [
      { key: 'prefix', label: 'Path Prefix', type: 'string' },
    ],
  },
  {
    type: 'eventlogger.modular',
    label: 'Event Logger',
    category: 'events',
    defaultConfig: {},
    configFields: [
      { key: 'output', label: 'Output', type: 'select', options: ['stdout', 'file', 'database'] },
    ],
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
  },
  {
    type: 'jsonschema.modular',
    label: 'JSON Schema Validator',
    category: 'infrastructure',
    defaultConfig: {},
    configFields: [
      { key: 'schema', label: 'Schema', type: 'json' },
    ],
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
  },
  // Observability
  {
    type: 'metrics.collector',
    label: 'Metrics Collector',
    category: 'observability',
    defaultConfig: {},
    configFields: [],
  },
  {
    type: 'health.checker',
    label: 'Health Checker',
    category: 'observability',
    defaultConfig: {},
    configFields: [],
  },
  {
    type: 'http.middleware.requestid',
    label: 'Request ID Middleware',
    category: 'middleware',
    defaultConfig: {},
    configFields: [
      { key: 'headerName', label: 'Header Name', type: 'string', defaultValue: 'X-Request-ID' },
    ],
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
  },
  {
    type: 'messaging.nats',
    label: 'NATS Broker',
    category: 'messaging',
    defaultConfig: { url: 'nats://localhost:4222' },
    configFields: [
      { key: 'url', label: 'URL', type: 'string', defaultValue: 'nats://localhost:4222' },
    ],
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
