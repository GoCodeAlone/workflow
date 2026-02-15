import { useState, useMemo, useCallback } from 'react';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type PluginCategory = 'Connectors' | 'Transforms' | 'Middleware' | 'Storage' | 'AI' | 'Monitoring';

interface Plugin {
  id: string;
  name: string;
  version: string;
  author: string;
  description: string;
  fullDescription: string;
  category: PluginCategory;
  tags: string[];
  rating: number;
  downloads: number;
  installed: boolean;
  configSchema: string;
  exampleUsage: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const CATEGORIES: PluginCategory[] = ['Connectors', 'Transforms', 'Middleware', 'Storage', 'AI', 'Monitoring'];

const CATEGORY_COLORS: Record<PluginCategory, string> = {
  Connectors: '#89b4fa',
  Transforms: '#a6e3a1',
  Middleware: '#fab387',
  Storage: '#f9e2af',
  AI: '#cba6f7',
  Monitoring: '#f38ba8',
};

const MOCK_PLUGINS: Plugin[] = [
  {
    id: 'http-connector',
    name: 'HTTP Connector',
    version: '2.3.1',
    author: 'Workflow Core',
    description: 'Make HTTP requests to external APIs with retry, timeout, and circuit-breaker support.',
    fullDescription:
      'The HTTP Connector module enables workflows to make outbound HTTP requests to any REST API. It supports configurable retry policies with exponential backoff, request/response timeout management, circuit-breaker patterns for fault tolerance, and automatic request/response logging. Supports all HTTP methods, custom headers, query parameters, and request body templates with variable interpolation.',
    category: 'Connectors',
    tags: ['http', 'rest', 'api'],
    rating: 4.8,
    downloads: 12450,
    installed: true,
    configSchema: `type: http_client
config:
  base_url: "https://api.example.com"
  timeout: "30s"
  retry_count: 3
  retry_backoff: "exponential"
  headers:
    Authorization: "Bearer {{secrets.api_token}}"`,
    exampleUsage: `modules:
  - name: external-api
    type: http_client
    config:
      base_url: "https://api.example.com"
      timeout: "10s"
workflows:
  - name: fetch-data
    trigger:
      type: http
      path: /fetch
    steps:
      - module: external-api
        action: GET
        path: /data`,
  },
  {
    id: 'postgres-connector',
    name: 'PostgreSQL Connector',
    version: '1.5.0',
    author: 'Workflow Core',
    description: 'Connect to PostgreSQL databases with connection pooling and prepared statement support.',
    fullDescription:
      'Full-featured PostgreSQL connector with connection pooling (configurable min/max connections), prepared statement caching, transaction support, and automatic reconnection. Supports read replicas for query distribution and includes built-in query logging and slow query detection.',
    category: 'Connectors',
    tags: ['database', 'sql', 'postgres'],
    rating: 4.7,
    downloads: 9830,
    installed: true,
    configSchema: `type: database
config:
  driver: postgres
  dsn: "postgres://user:pass@localhost:5432/mydb?sslmode=require"
  max_open_conns: 25
  max_idle_conns: 5`,
    exampleUsage: `modules:
  - name: main-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.db_dsn}}"
      max_open_conns: 25`,
  },
  {
    id: 'kafka-connector',
    name: 'Kafka Connector',
    version: '1.2.4',
    author: 'Community',
    description: 'Produce and consume messages from Apache Kafka topics with consumer group support.',
    fullDescription:
      'Bi-directional Apache Kafka connector supporting both producing and consuming messages. Features include consumer group management, partition assignment strategies, exactly-once semantics, message serialization/deserialization (JSON, Avro, Protobuf), dead letter queues, and configurable batch processing.',
    category: 'Connectors',
    tags: ['kafka', 'messaging', 'streaming'],
    rating: 4.5,
    downloads: 7620,
    installed: false,
    configSchema: `type: kafka
config:
  brokers:
    - "localhost:9092"
  consumer_group: "my-group"
  topics:
    - "orders"
  serialization: json`,
    exampleUsage: `modules:
  - name: kafka-orders
    type: kafka
    config:
      brokers: ["kafka:9092"]
      topics: ["orders"]
workflows:
  - name: process-orders
    trigger:
      type: messaging
      topic: orders
    steps:
      - module: order-handler`,
  },
  {
    id: 'json-transform',
    name: 'JSON Transform',
    version: '3.0.2',
    author: 'Workflow Core',
    description: 'Transform JSON payloads using JSONPath expressions, JQ-style filters, and mapping templates.',
    fullDescription:
      'Powerful JSON transformation engine supporting JSONPath for data extraction, JQ-style filters for complex transformations, Go template-based mapping, conditional field inclusion, array operations (flatten, group, sort, filter), and schema validation of transformed output.',
    category: 'Transforms',
    tags: ['json', 'mapping', 'transform'],
    rating: 4.9,
    downloads: 15230,
    installed: true,
    configSchema: `type: json_transform
config:
  mappings:
    - source: "$.user.name"
      target: "$.customer_name"
    - source: "$.items[*].price"
      target: "$.total"
      aggregate: sum`,
    exampleUsage: `steps:
  - module: json-transform
    config:
      mappings:
        - source: "$.order.items"
          target: "$.line_items"
          transform: "flatten"`,
  },
  {
    id: 'csv-parser',
    name: 'CSV Parser',
    version: '1.1.0',
    author: 'Community',
    description: 'Parse and generate CSV files with configurable delimiters, headers, and encoding support.',
    fullDescription:
      'Flexible CSV parsing and generation module. Supports custom delimiters, quoted fields, multi-line values, header mapping, type coercion (string to number/boolean/date), encoding detection (UTF-8, Latin-1, etc.), streaming for large files, and column filtering/reordering.',
    category: 'Transforms',
    tags: ['csv', 'parsing', 'data'],
    rating: 4.3,
    downloads: 4120,
    installed: false,
    configSchema: `type: csv_parser
config:
  delimiter: ","
  has_header: true
  encoding: "utf-8"
  type_coercion: true`,
    exampleUsage: `steps:
  - module: csv-parser
    config:
      delimiter: ","
      columns: ["name", "email", "amount"]`,
  },
  {
    id: 'rate-limiter',
    name: 'Rate Limiter',
    version: '2.0.0',
    author: 'Workflow Core',
    description: 'Apply rate limiting to workflow executions using token bucket or sliding window algorithms.',
    fullDescription:
      'Configurable rate limiting middleware supporting token bucket, sliding window, and fixed window algorithms. Features include per-user/per-IP/per-API-key limits, distributed rate limiting via Redis backend, burst allowance, custom response headers (X-RateLimit-*), and configurable limit exceeded responses.',
    category: 'Middleware',
    tags: ['rate-limit', 'throttle', 'security'],
    rating: 4.6,
    downloads: 8940,
    installed: true,
    configSchema: `type: rate_limiter
config:
  algorithm: token_bucket
  rate: 100
  period: "1m"
  burst: 20
  key_extractor: "header:X-API-Key"`,
    exampleUsage: `modules:
  - name: api-limiter
    type: rate_limiter
    config:
      rate: 100
      period: "1m"`,
  },
  {
    id: 'auth-middleware',
    name: 'Auth Middleware',
    version: '1.8.3',
    author: 'Workflow Core',
    description: 'JWT/OAuth2 authentication middleware with role-based access control.',
    fullDescription:
      'Production-ready authentication middleware supporting JWT validation, OAuth2 token introspection, API key authentication, and multi-provider SSO. Includes role-based access control (RBAC), permission caching, token refresh handling, and audit logging of authentication events.',
    category: 'Middleware',
    tags: ['auth', 'jwt', 'security', 'oauth'],
    rating: 4.7,
    downloads: 11200,
    installed: false,
    configSchema: `type: auth_middleware
config:
  provider: jwt
  jwt_secret: "{{secrets.jwt_secret}}"
  issuer: "https://auth.example.com"
  roles:
    admin: ["*"]
    viewer: ["read:*"]`,
    exampleUsage: `modules:
  - name: auth
    type: auth_middleware
    config:
      provider: jwt
      issuer: "{{secrets.jwt_issuer}}"`,
  },
  {
    id: 'redis-cache',
    name: 'Redis Cache',
    version: '2.1.0',
    author: 'Workflow Core',
    description: 'Distributed caching with Redis supporting TTL, eviction policies, and cache invalidation.',
    fullDescription:
      'Redis-backed distributed cache module with support for string, hash, list, and set data structures. Features include configurable TTL per key pattern, LRU/LFU eviction policies, cache-aside and write-through strategies, pub/sub-based cache invalidation, connection pooling, and Sentinel/Cluster mode support.',
    category: 'Storage',
    tags: ['redis', 'cache', 'performance'],
    rating: 4.8,
    downloads: 10560,
    installed: true,
    configSchema: `type: cache
config:
  provider: redis
  address: "localhost:6379"
  password: "{{secrets.redis_password}}"
  db: 0
  default_ttl: "5m"`,
    exampleUsage: `modules:
  - name: app-cache
    type: cache
    config:
      provider: redis
      address: "redis:6379"
      default_ttl: "10m"`,
  },
  {
    id: 's3-storage',
    name: 'S3 Storage',
    version: '1.4.2',
    author: 'Community',
    description: 'Upload, download, and manage files in Amazon S3 or S3-compatible storage.',
    fullDescription:
      'Amazon S3 and S3-compatible (MinIO, DigitalOcean Spaces) storage module. Supports multipart uploads for large files, pre-signed URL generation, server-side encryption (SSE-S3, SSE-KMS, SSE-C), lifecycle policy management, bucket event notifications, and streaming download/upload for memory efficiency.',
    category: 'Storage',
    tags: ['s3', 'files', 'cloud', 'aws'],
    rating: 4.4,
    downloads: 6780,
    installed: false,
    configSchema: `type: s3_storage
config:
  region: "us-east-1"
  bucket: "my-workflow-data"
  access_key: "{{secrets.aws_access_key}}"
  secret_key: "{{secrets.aws_secret_key}}"`,
    exampleUsage: `modules:
  - name: file-store
    type: s3_storage
    config:
      bucket: "workflow-uploads"
      region: "us-east-1"`,
  },
  {
    id: 'llm-provider',
    name: 'LLM Provider',
    version: '0.9.1',
    author: 'Workflow AI',
    description: 'Integrate large language models (Claude, GPT) into workflows with tool-use and streaming.',
    fullDescription:
      'Multi-provider LLM integration supporting Anthropic Claude, OpenAI GPT, and custom model endpoints. Features include tool/function calling, streaming responses, conversation context management, prompt templating with variable interpolation, token usage tracking, response caching, and configurable retry with fallback providers.',
    category: 'AI',
    tags: ['llm', 'ai', 'claude', 'gpt'],
    rating: 4.6,
    downloads: 5430,
    installed: true,
    configSchema: `type: llm_provider
config:
  provider: anthropic
  model: "claude-sonnet-4-20250514"
  api_key: "{{secrets.anthropic_key}}"
  max_tokens: 4096
  temperature: 0.7`,
    exampleUsage: `modules:
  - name: ai-assistant
    type: llm_provider
    config:
      provider: anthropic
      model: "claude-sonnet-4-20250514"
steps:
  - module: ai-assistant
    action: complete
    prompt: "Summarize: {{input.text}}"`,
  },
  {
    id: 'vector-store',
    name: 'Vector Store',
    version: '0.5.0',
    author: 'Workflow AI',
    description: 'Store and query vector embeddings for semantic search using Pinecone or pgvector.',
    fullDescription:
      'Vector embedding storage and retrieval for semantic search workflows. Supports Pinecone, pgvector, and Qdrant backends. Features include automatic embedding generation (via LLM provider integration), similarity search with configurable distance metrics (cosine, euclidean, dot product), metadata filtering, batch upsert, and namespace isolation.',
    category: 'AI',
    tags: ['vectors', 'embeddings', 'search', 'ai'],
    rating: 4.2,
    downloads: 2340,
    installed: false,
    configSchema: `type: vector_store
config:
  backend: pgvector
  dsn: "{{secrets.vector_db_dsn}}"
  dimensions: 1536
  distance_metric: cosine`,
    exampleUsage: `modules:
  - name: doc-search
    type: vector_store
    config:
      backend: pgvector
      dimensions: 1536`,
  },
  {
    id: 'prometheus-exporter',
    name: 'Prometheus Exporter',
    version: '1.3.0',
    author: 'Workflow Core',
    description: 'Export workflow metrics to Prometheus with custom counters, histograms, and gauges.',
    fullDescription:
      'Prometheus metrics exporter for workflow observability. Automatically tracks execution counts, durations, error rates, and queue depths. Supports custom metric definitions (counters, histograms, gauges, summaries), label customization, metric namespacing, and a built-in /metrics HTTP endpoint. Integrates with Grafana dashboards via pre-built dashboard templates.',
    category: 'Monitoring',
    tags: ['prometheus', 'metrics', 'observability'],
    rating: 4.7,
    downloads: 8920,
    installed: true,
    configSchema: `type: prometheus_exporter
config:
  port: 9090
  path: "/metrics"
  namespace: "workflow"
  custom_metrics:
    - name: orders_processed
      type: counter
      help: "Total orders processed"`,
    exampleUsage: `modules:
  - name: metrics
    type: prometheus_exporter
    config:
      port: 9090
      namespace: "myapp"`,
  },
  {
    id: 'alertmanager',
    name: 'Alert Manager',
    version: '1.0.2',
    author: 'Community',
    description: 'Configure alerting rules and send notifications via Slack, PagerDuty, or email.',
    fullDescription:
      'Alerting and notification module that monitors workflow metrics and triggers alerts based on configurable rules. Supports multiple notification channels (Slack, PagerDuty, email, webhooks), alert grouping and deduplication, escalation policies, maintenance windows, and alert acknowledgment tracking.',
    category: 'Monitoring',
    tags: ['alerts', 'slack', 'pagerduty', 'notifications'],
    rating: 4.1,
    downloads: 3560,
    installed: false,
    configSchema: `type: alert_manager
config:
  rules:
    - name: high-error-rate
      condition: "error_rate > 5%"
      duration: "5m"
      channels: ["slack"]
  channels:
    slack:
      webhook_url: "{{secrets.slack_webhook}}"`,
    exampleUsage: `modules:
  - name: alerts
    type: alert_manager
    config:
      rules:
        - name: failures
          condition: "error_rate > 10%"
          channels: ["slack"]`,
  },
];

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StarRating({ rating }: { rating: number }) {
  const full = Math.floor(rating);
  const half = rating - full >= 0.5;
  const stars: string[] = [];
  for (let i = 0; i < 5; i++) {
    if (i < full) stars.push('full');
    else if (i === full && half) stars.push('half');
    else stars.push('empty');
  }
  return (
    <span style={{ display: 'inline-flex', gap: 1, alignItems: 'center' }}>
      {stars.map((type, i) => (
        <svg key={i} width={12} height={12} viewBox="0 0 24 24" fill={type === 'empty' ? 'none' : '#f9e2af'} stroke="#f9e2af" strokeWidth={2}>
          <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
          {type === 'half' && (
            <clipPath id={`half-${i}`}>
              <rect x="0" y="0" width="12" height="24" />
            </clipPath>
          )}
        </svg>
      ))}
      <span style={{ fontSize: 11, color: '#a6adc8', marginLeft: 4 }}>{rating.toFixed(1)}</span>
    </span>
  );
}

function CategoryBadge({ category }: { category: PluginCategory }) {
  const color = CATEGORY_COLORS[category];
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 10,
        fontWeight: 600,
        background: color + '22',
        color,
      }}
    >
      {category}
    </span>
  );
}

function TagBadge({ tag }: { tag: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '1px 6px',
        borderRadius: 3,
        fontSize: 10,
        background: '#45475a',
        color: '#a6adc8',
      }}
    >
      {tag}
    </span>
  );
}

function PluginCard({
  plugin,
  onToggleInstall,
  onClick,
}: {
  plugin: Plugin;
  onToggleInstall: (id: string) => void;
  onClick: (plugin: Plugin) => void;
}) {
  return (
    <div
      onClick={() => onClick(plugin)}
      style={{
        background: '#313244',
        border: '1px solid #45475a',
        borderRadius: 8,
        padding: 16,
        cursor: 'pointer',
        transition: 'border-color 0.15s',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
      onMouseEnter={(e) => (e.currentTarget.style.borderColor = '#89b4fa')}
      onMouseLeave={(e) => (e.currentTarget.style.borderColor = '#45475a')}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{plugin.name}</div>
          <div style={{ fontSize: 11, color: '#6c7086', marginTop: 2 }}>
            v{plugin.version} by {plugin.author}
          </div>
        </div>
        <CategoryBadge category={plugin.category} />
      </div>

      <div style={{ fontSize: 12, color: '#a6adc8', lineHeight: 1.4, flex: 1 }}>
        {plugin.description.length > 120 ? plugin.description.slice(0, 120) + '...' : plugin.description}
      </div>

      <StarRating rating={plugin.rating} />

      <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
        {plugin.tags.map((tag) => (
          <TagBadge key={tag} tag={tag} />
        ))}
      </div>

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 4 }}>
        <span style={{ fontSize: 11, color: '#6c7086' }}>
          {plugin.downloads.toLocaleString()} downloads
        </span>
        <button
          onClick={(e) => {
            e.stopPropagation();
            onToggleInstall(plugin.id);
          }}
          style={{
            padding: '4px 14px',
            borderRadius: 4,
            border: 'none',
            fontSize: 12,
            fontWeight: 600,
            cursor: 'pointer',
            background: plugin.installed ? '#f38ba822' : '#89b4fa',
            color: plugin.installed ? '#f38ba8' : '#1e1e2e',
          }}
        >
          {plugin.installed ? 'Uninstall' : 'Install'}
        </button>
      </div>
    </div>
  );
}

function PluginDetailModal({
  plugin,
  onClose,
  onToggleInstall,
}: {
  plugin: Plugin;
  onClose: () => void;
  onToggleInstall: (id: string) => void;
}) {
  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          width: '90%',
          maxWidth: 700,
          maxHeight: '85vh',
          overflow: 'auto',
          padding: 24,
        }}
      >
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 16 }}>
          <div>
            <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 20, fontWeight: 600 }}>{plugin.name}</h2>
            <div style={{ fontSize: 12, color: '#6c7086', marginTop: 4 }}>
              v{plugin.version} by {plugin.author}
            </div>
          </div>
          <button
            onClick={onClose}
            style={{
              background: 'none',
              border: 'none',
              color: '#6c7086',
              fontSize: 20,
              cursor: 'pointer',
              padding: 4,
            }}
          >
            x
          </button>
        </div>

        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 16 }}>
          <CategoryBadge category={plugin.category} />
          <StarRating rating={plugin.rating} />
          <span style={{ fontSize: 11, color: '#6c7086' }}>{plugin.downloads.toLocaleString()} downloads</span>
        </div>

        <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginBottom: 16 }}>
          {plugin.tags.map((tag) => (
            <TagBadge key={tag} tag={tag} />
          ))}
        </div>

        {/* Description */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Description</h3>
        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.6, marginBottom: 20 }}>{plugin.fullDescription}</p>

        {/* Config Schema */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Configuration Schema</h3>
        <pre
          style={{
            background: '#181825',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: 12,
            fontSize: 12,
            color: '#a6e3a1',
            overflow: 'auto',
            marginBottom: 20,
          }}
        >
          {plugin.configSchema}
        </pre>

        {/* Example */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Example Usage</h3>
        <pre
          style={{
            background: '#181825',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: 12,
            fontSize: 12,
            color: '#89b4fa',
            overflow: 'auto',
            marginBottom: 20,
          }}
        >
          {plugin.exampleUsage}
        </pre>

        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button
            onClick={() => onToggleInstall(plugin.id)}
            style={{
              padding: '8px 24px',
              borderRadius: 6,
              border: 'none',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              background: plugin.installed ? '#f38ba822' : '#89b4fa',
              color: plugin.installed ? '#f38ba8' : '#1e1e2e',
            }}
          >
            {plugin.installed ? 'Uninstall' : 'Install'}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Marketplace() {
  const [plugins, setPlugins] = useState<Plugin[]>(MOCK_PLUGINS);
  const [search, setSearch] = useState('');
  const [categoryFilter, setCategoryFilter] = useState<PluginCategory | 'All'>('All');
  const [selectedPlugin, setSelectedPlugin] = useState<Plugin | null>(null);

  const toggleInstall = useCallback(
    (id: string) => {
      setPlugins((prev) =>
        prev.map((p) => (p.id === id ? { ...p, installed: !p.installed } : p)),
      );
      // Also update the selected plugin if it's open
      setSelectedPlugin((prev) => {
        if (prev && prev.id === id) return { ...prev, installed: !prev.installed };
        return prev;
      });
    },
    [],
  );

  const filtered = useMemo(() => {
    return plugins.filter((p) => {
      const matchesSearch =
        !search ||
        p.name.toLowerCase().includes(search.toLowerCase()) ||
        p.description.toLowerCase().includes(search.toLowerCase()) ||
        p.tags.some((t) => t.toLowerCase().includes(search.toLowerCase()));
      const matchesCategory = categoryFilter === 'All' || p.category === categoryFilter;
      return matchesSearch && matchesCategory;
    });
  }, [plugins, search, categoryFilter]);

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>Plugin Marketplace</h2>
      <p style={{ color: '#6c7086', fontSize: 13, margin: '0 0 20px' }}>
        Browse and install modules to extend your workflow engine.
      </p>

      {/* Search & filter bar */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap' }}>
        <input
          type="text"
          placeholder="Search plugins..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{
            flex: 1,
            minWidth: 200,
            padding: '8px 12px',
            borderRadius: 6,
            border: '1px solid #45475a',
            background: '#313244',
            color: '#cdd6f4',
            fontSize: 13,
            outline: 'none',
          }}
        />
        <select
          value={categoryFilter}
          onChange={(e) => setCategoryFilter(e.target.value as PluginCategory | 'All')}
          style={{
            padding: '8px 12px',
            borderRadius: 6,
            border: '1px solid #45475a',
            background: '#313244',
            color: '#cdd6f4',
            fontSize: 13,
            outline: 'none',
            cursor: 'pointer',
          }}
        >
          <option value="All">All Categories</option>
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </div>

      {/* Summary */}
      <div style={{ fontSize: 12, color: '#6c7086', marginBottom: 12 }}>
        Showing {filtered.length} of {plugins.length} plugins
        {categoryFilter !== 'All' && <> in {categoryFilter}</>}
        {search && <> matching &quot;{search}&quot;</>}
      </div>

      {/* Plugin grid */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
          gap: 12,
        }}
      >
        {filtered.map((plugin) => (
          <PluginCard
            key={plugin.id}
            plugin={plugin}
            onToggleInstall={toggleInstall}
            onClick={setSelectedPlugin}
          />
        ))}
        {filtered.length === 0 && (
          <div style={{ color: '#6c7086', fontSize: 13, gridColumn: '1 / -1', padding: 40, textAlign: 'center' }}>
            No plugins match your search criteria.
          </div>
        )}
      </div>

      {/* Detail modal */}
      {selectedPlugin && (
        <PluginDetailModal
          plugin={selectedPlugin}
          onClose={() => setSelectedPlugin(null)}
          onToggleInstall={toggleInstall}
        />
      )}
    </div>
  );
}
