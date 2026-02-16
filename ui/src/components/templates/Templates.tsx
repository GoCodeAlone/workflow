import { useState, useMemo } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import useObservabilityStore from '../../store/observabilityStore.ts';
import { parseYaml } from '../../utils/serialization.ts';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Complexity = 'Simple' | 'Intermediate' | 'Advanced';

interface Template {
  id: string;
  name: string;
  description: string;
  complexity: Complexity;
  tags: string[];
  yaml: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const COMPLEXITY_COLORS: Record<Complexity, string> = {
  Simple: '#a6e3a1',
  Intermediate: '#f9e2af',
  Advanced: '#f38ba8',
};

const ALL_TAGS = ['HTTP', 'Pipeline', 'Event-Driven', 'CRON', 'WebSocket', 'Database', 'AI', 'Integration', 'State Machine', 'Monitoring'];

const MOCK_TEMPLATES: Template[] = [
  {
    id: 'rest-api',
    name: 'REST API Gateway',
    description:
      'A complete REST API with CRUD endpoints, input validation, authentication middleware, and database persistence. Includes health check and metrics endpoints.',
    complexity: 'Intermediate',
    tags: ['HTTP', 'Database'],
    yaml: `name: rest-api-gateway
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080
      read_timeout: "30s"

  - name: auth
    type: auth_middleware
    config:
      provider: jwt
      secret: "{{secrets.jwt_secret}}"

  - name: main-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.db_dsn}}"

  - name: validator
    type: json_schema
    config:
      strict: true

workflows:
  - name: list-items
    trigger:
      type: http
      method: GET
      path: /api/items
    steps:
      - module: auth
      - module: main-db
        action: query
        sql: "SELECT * FROM items ORDER BY created_at DESC"

  - name: create-item
    trigger:
      type: http
      method: POST
      path: /api/items
    steps:
      - module: auth
      - module: validator
        schema: item-create
      - module: main-db
        action: exec
        sql: "INSERT INTO items (name, value) VALUES ($1, $2)"`,
  },
  {
    id: 'event-pipeline',
    name: 'Event Processing Pipeline',
    description:
      'An event-driven pipeline that consumes messages from an event bus, transforms data through multiple stages, and publishes results. Includes dead letter queue handling.',
    complexity: 'Advanced',
    tags: ['Event-Driven', 'Pipeline'],
    yaml: `name: event-pipeline
version: "1.0"

modules:
  - name: event-bus
    type: eventbus
    config:
      provider: memory
      buffer_size: 1000

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.raw_data"
          target: "$.processed"

  - name: enricher
    type: http_client
    config:
      base_url: "https://api.enrichment.io"
      timeout: "5s"

  - name: output-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.output_db_dsn}}"

  - name: dlq
    type: eventbus
    config:
      provider: memory
      topic_prefix: "dlq."

workflows:
  - name: process-events
    trigger:
      type: messaging
      topic: incoming-events
    steps:
      - module: transformer
      - module: enricher
        action: POST
        path: /enrich
        on_error: continue
      - module: output-db
        action: exec
    error_handler:
      module: dlq
      topic: failed-events`,
  },
  {
    id: 'cron-job',
    name: 'Scheduled CRON Job',
    description:
      'A scheduled workflow that runs at configurable intervals. Fetches data from an API, processes it, and sends summary reports via email or Slack.',
    complexity: 'Simple',
    tags: ['CRON', 'HTTP'],
    yaml: `name: scheduled-report
version: "1.0"

modules:
  - name: data-source
    type: http_client
    config:
      base_url: "https://api.internal.io"
      timeout: "30s"
      headers:
        Authorization: "Bearer {{secrets.api_token}}"

  - name: formatter
    type: json_transform
    config:
      template: report

  - name: notifier
    type: http_client
    config:
      base_url: "https://hooks.slack.com"

  - name: scheduler
    type: scheduler
    config:
      timezone: "America/New_York"

workflows:
  - name: daily-report
    trigger:
      type: cron
      schedule: "0 9 * * 1-5"
    steps:
      - module: data-source
        action: GET
        path: /metrics/daily
      - module: formatter
      - module: notifier
        action: POST
        path: "/services/{{secrets.slack_webhook_id}}"`,
  },
  {
    id: 'chat-app',
    name: 'Chat Application',
    description:
      'A real-time chat application using WebSocket connections, with message persistence, user presence tracking, and AI-powered auto-moderation.',
    complexity: 'Advanced',
    tags: ['WebSocket', 'Event-Driven', 'AI'],
    yaml: `name: chat-application
version: "1.0"

modules:
  - name: ws-server
    type: httpserver
    config:
      port: 8080
      websocket: true
      ws_path: /ws

  - name: auth
    type: auth_middleware
    config:
      provider: jwt

  - name: message-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.chat_db_dsn}}"

  - name: event-bus
    type: eventbus
    config:
      provider: memory

  - name: ai-moderator
    type: llm_provider
    config:
      provider: anthropic
      model: "claude-haiku-4-20250414"
      max_tokens: 256

  - name: cache
    type: cache
    config:
      provider: redis
      address: "{{secrets.redis_url}}"

workflows:
  - name: send-message
    trigger:
      type: websocket
      event: message
    steps:
      - module: auth
      - module: ai-moderator
        action: moderate
        prompt: "Is this message appropriate? Respond YES or NO."
      - module: message-db
        action: exec
        sql: "INSERT INTO messages (user_id, room_id, content) VALUES ($1, $2, $3)"
      - module: event-bus
        action: publish
        topic: "chat.{{room_id}}"

  - name: join-room
    trigger:
      type: websocket
      event: join
    steps:
      - module: auth
      - module: cache
        action: set
        key: "presence:{{user_id}}"
        ttl: "5m"`,
  },
  {
    id: 'ecommerce-order',
    name: 'E-commerce Order Flow',
    description:
      'Complete order processing pipeline with inventory check, payment processing, order confirmation, shipping label generation, and email notifications.',
    complexity: 'Advanced',
    tags: ['HTTP', 'Pipeline', 'Event-Driven'],
    yaml: `name: ecommerce-order-flow
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: order-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.order_db_dsn}}"

  - name: inventory-svc
    type: http_client
    config:
      base_url: "https://inventory.internal"

  - name: payment-svc
    type: http_client
    config:
      base_url: "https://payments.internal"

  - name: shipping-svc
    type: http_client
    config:
      base_url: "https://shipping.internal"

  - name: email-svc
    type: http_client
    config:
      base_url: "https://email.internal"

  - name: event-bus
    type: eventbus
    config:
      provider: memory

workflows:
  - name: place-order
    trigger:
      type: http
      method: POST
      path: /api/orders
    steps:
      - module: inventory-svc
        action: POST
        path: /check
      - module: payment-svc
        action: POST
        path: /charge
      - module: order-db
        action: exec
        sql: "INSERT INTO orders ..."
      - module: event-bus
        action: publish
        topic: order.created

  - name: fulfill-order
    trigger:
      type: messaging
      topic: order.created
    steps:
      - module: shipping-svc
        action: POST
        path: /labels
      - module: order-db
        action: exec
        sql: "UPDATE orders SET status='shipped' WHERE id=$1"
      - module: email-svc
        action: POST
        path: /send
        template: order-shipped`,
  },
  {
    id: 'data-etl',
    name: 'Data ETL Pipeline',
    description:
      'Extract-Transform-Load pipeline that reads from multiple data sources, applies transformations and aggregations, and loads into a data warehouse.',
    complexity: 'Intermediate',
    tags: ['Pipeline', 'Database'],
    yaml: `name: data-etl-pipeline
version: "1.0"

modules:
  - name: source-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.source_db_dsn}}"

  - name: source-api
    type: http_client
    config:
      base_url: "https://api.datasource.io"
      timeout: "60s"

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.records[*]"
          target: "$.rows"
          transform: "flatten"

  - name: warehouse-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.warehouse_dsn}}"

  - name: scheduler
    type: scheduler

workflows:
  - name: nightly-etl
    trigger:
      type: cron
      schedule: "0 2 * * *"
    steps:
      - module: source-db
        action: query
        sql: "SELECT * FROM transactions WHERE date = CURRENT_DATE - 1"
      - module: source-api
        action: GET
        path: /daily-metrics
      - module: transformer
      - module: warehouse-db
        action: exec
        sql: "INSERT INTO fact_daily ..."`,
  },
  {
    id: 'webhook-processor',
    name: 'Webhook Processor',
    description:
      'Receives webhooks from external services (GitHub, Stripe, Twilio), validates signatures, routes by event type, and triggers appropriate workflow actions.',
    complexity: 'Simple',
    tags: ['HTTP', 'Event-Driven'],
    yaml: `name: webhook-processor
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: event-bus
    type: eventbus
    config:
      provider: memory

  - name: logger
    type: event_logger
    config:
      store: database

  - name: db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.db_dsn}}"

workflows:
  - name: github-webhook
    trigger:
      type: http
      method: POST
      path: /webhooks/github
    steps:
      - module: logger
      - module: event-bus
        action: publish
        topic: "github.{{event_type}}"

  - name: stripe-webhook
    trigger:
      type: http
      method: POST
      path: /webhooks/stripe
    steps:
      - module: logger
      - module: event-bus
        action: publish
        topic: "stripe.{{event_type}}"

  - name: handle-payment
    trigger:
      type: messaging
      topic: stripe.payment_intent.succeeded
    steps:
      - module: db
        action: exec
        sql: "UPDATE orders SET paid=true WHERE stripe_id=$1"`,
  },
  {
    id: 'multi-service',
    name: 'Multi-Service Integration',
    description:
      'Orchestrates communication between multiple microservices with retry logic, circuit breakers, and distributed tracing. Includes health monitoring for all services.',
    complexity: 'Advanced',
    tags: ['HTTP', 'Integration', 'Pipeline'],
    yaml: `name: multi-service-integration
version: "1.0"

modules:
  - name: api-gateway
    type: httpserver
    config:
      port: 8080

  - name: user-svc
    type: http_client
    config:
      base_url: "https://users.internal"
      retry_count: 3
      circuit_breaker:
        threshold: 5
        timeout: "30s"

  - name: billing-svc
    type: http_client
    config:
      base_url: "https://billing.internal"
      retry_count: 3

  - name: notification-svc
    type: http_client
    config:
      base_url: "https://notifications.internal"

  - name: analytics-svc
    type: http_client
    config:
      base_url: "https://analytics.internal"

  - name: cache
    type: cache
    config:
      provider: redis
      address: "{{secrets.redis_url}}"
      default_ttl: "5m"

  - name: metrics
    type: prometheus_exporter
    config:
      port: 9090

workflows:
  - name: user-signup
    trigger:
      type: http
      method: POST
      path: /api/signup
    steps:
      - module: user-svc
        action: POST
        path: /users
      - module: billing-svc
        action: POST
        path: /accounts
      - module: notification-svc
        action: POST
        path: /send
        template: welcome-email
      - module: analytics-svc
        action: POST
        path: /events
        data:
          event: user_signup

  - name: get-user-profile
    trigger:
      type: http
      method: GET
      path: /api/users/:id
    steps:
      - module: cache
        action: get
        key: "user:{{params.id}}"
        on_miss: continue
      - module: user-svc
        action: GET
        path: "/users/{{params.id}}"
      - module: cache
        action: set
        key: "user:{{params.id}}"`,
  },
  {
    id: 'ai-assistant',
    name: 'AI-Powered Assistant',
    description:
      'Conversational AI assistant with tool use, context management, and multi-turn dialogue. Integrates with knowledge base for RAG-powered responses.',
    complexity: 'Intermediate',
    tags: ['AI', 'HTTP', 'Database'],
    yaml: `name: ai-assistant
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: llm
    type: llm_provider
    config:
      provider: anthropic
      model: "claude-sonnet-4-20250514"
      max_tokens: 4096

  - name: vector-db
    type: vector_store
    config:
      backend: pgvector
      dimensions: 1536

  - name: conversation-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.db_dsn}}"

workflows:
  - name: chat
    trigger:
      type: http
      method: POST
      path: /api/chat
    steps:
      - module: conversation-db
        action: query
        sql: "SELECT * FROM messages WHERE session=$1 ORDER BY created_at DESC LIMIT 10"
      - module: vector-db
        action: search
        query: "{{input.message}}"
        top_k: 5
      - module: llm
        action: complete
        system: "You are a helpful assistant. Use the provided context."
        context: "{{steps.vector-db.results}}"`,
  },
  {
    id: 'state-machine-order',
    name: 'State Machine Order Processor',
    description:
      'Order lifecycle management using a state machine engine. Tracks orders through created, validated, paid, shipped, and delivered states with configurable transitions and guards.',
    complexity: 'Advanced',
    tags: ['State Machine', 'Pipeline'],
    yaml: `name: state-machine-order-processor
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: order-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.order_db_dsn}}"

  - name: state-engine
    type: statemachine.engine
    config:
      states:
        - created
        - validated
        - paid
        - shipped
        - delivered
        - cancelled
      transitions:
        - from: created
          to: validated
          action: validate
        - from: validated
          to: paid
          action: charge
        - from: paid
          to: shipped
          action: ship
        - from: shipped
          to: delivered
          action: confirm_delivery
        - from: [created, validated]
          to: cancelled
          action: cancel

  - name: event-bus
    type: eventbus
    config:
      provider: memory

workflows:
  - name: transition-order
    trigger:
      type: http
      method: POST
      path: /api/orders/:id/transition
    steps:
      - module: state-engine
        action: transition
        entity: "{{params.id}}"
      - module: order-db
        action: exec
        sql: "UPDATE orders SET state=$1 WHERE id=$2"
      - module: event-bus
        action: publish
        topic: "order.{{new_state}}"`,
  },
  {
    id: 'cdc-data-sync',
    name: 'CDC Data Sync',
    description:
      'Change data capture pipeline that listens for PostgreSQL changes, transforms records, and syncs them to an analytics database in near real-time.',
    complexity: 'Intermediate',
    tags: ['Database', 'Event-Driven'],
    yaml: `name: cdc-data-sync
version: "1.0"

modules:
  - name: source-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.source_dsn}}"
      cdc:
        enabled: true
        slot: analytics_slot

  - name: event-bus
    type: eventbus
    config:
      provider: memory
      buffer_size: 5000

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.old"
          target: "$.diff.before"
        - source: "$.new"
          target: "$.diff.after"

  - name: analytics-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.analytics_dsn}}"

workflows:
  - name: capture-changes
    trigger:
      type: messaging
      topic: cdc.source-db
    steps:
      - module: transformer
      - module: analytics-db
        action: exec
        sql: "INSERT INTO change_log (table_name, operation, payload) VALUES ($1, $2, $3)"
      - module: event-bus
        action: publish
        topic: "sync.completed"`,
  },
  {
    id: 'rate-limiter-gateway',
    name: 'API Rate Limiter Gateway',
    description:
      'HTTP gateway with built-in rate limiting per client IP, CORS middleware, and request logging. Proxies validated requests to upstream services.',
    complexity: 'Simple',
    tags: ['HTTP'],
    yaml: `name: api-rate-limiter-gateway
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080
      cors:
        allowed_origins: ["*"]
        allowed_methods: ["GET", "POST", "PUT", "DELETE"]
        allowed_headers: ["Authorization", "Content-Type"]
      rate_limit:
        requests_per_second: 100
        burst: 20

  - name: upstream
    type: http_client
    config:
      base_url: "https://api.backend.internal"
      timeout: "10s"

  - name: logger
    type: event_logger
    config:
      store: stdout
      format: json

workflows:
  - name: proxy-request
    trigger:
      type: http
      method: "*"
      path: /api/*
    steps:
      - module: logger
      - module: upstream
        action: proxy
        strip_prefix: /api`,
  },
  {
    id: 'monitoring-dashboard',
    name: 'Real-Time Monitoring Dashboard',
    description:
      'Metrics collection service with health checks, Prometheus-compatible endpoints, and configurable alerting thresholds. Monitors upstream services and reports status.',
    complexity: 'Intermediate',
    tags: ['Monitoring', 'HTTP'],
    yaml: `name: realtime-monitoring-dashboard
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: metrics
    type: prometheus_exporter
    config:
      port: 9090
      path: /metrics

  - name: health-checker
    type: http_client
    config:
      timeout: "5s"

  - name: alert-bus
    type: eventbus
    config:
      provider: memory

  - name: scheduler
    type: scheduler
    config:
      timezone: "UTC"

  - name: cache
    type: cache
    config:
      provider: memory
      default_ttl: "30s"

workflows:
  - name: collect-health
    trigger:
      type: cron
      schedule: "*/30 * * * * *"
    steps:
      - module: health-checker
        action: GET
        path: /health
        targets:
          - "https://api.internal/health"
          - "https://db-proxy.internal/health"
          - "https://queue.internal/health"
      - module: cache
        action: set
        key: "service-health"
      - module: metrics
        action: gauge
        name: service_health_status

  - name: health-endpoint
    trigger:
      type: http
      method: GET
      path: /api/health
    steps:
      - module: cache
        action: get
        key: "service-health"`,
  },
  {
    id: 'saga-travel-booking',
    name: 'Saga Travel Booking',
    description:
      'Distributed saga pattern for travel booking that coordinates flight, hotel, and car rental reservations. Includes compensation logic to roll back on partial failures.',
    complexity: 'Advanced',
    tags: ['Pipeline', 'Integration'],
    yaml: `name: saga-travel-booking
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: flight-svc
    type: http_client
    config:
      base_url: "https://flights.travel.internal"
      timeout: "15s"

  - name: hotel-svc
    type: http_client
    config:
      base_url: "https://hotels.travel.internal"
      timeout: "15s"

  - name: car-svc
    type: http_client
    config:
      base_url: "https://cars.travel.internal"
      timeout: "15s"

  - name: booking-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.booking_dsn}}"

  - name: event-bus
    type: eventbus
    config:
      provider: memory

workflows:
  - name: book-trip
    trigger:
      type: http
      method: POST
      path: /api/bookings
    steps:
      - module: flight-svc
        action: POST
        path: /reserve
        compensate:
          action: POST
          path: /cancel
      - module: hotel-svc
        action: POST
        path: /reserve
        compensate:
          action: POST
          path: /cancel
      - module: car-svc
        action: POST
        path: /reserve
        compensate:
          action: POST
          path: /cancel
      - module: booking-db
        action: exec
        sql: "INSERT INTO bookings (user_id, flight, hotel, car, status) VALUES ($1,$2,$3,$4,'confirmed')"
      - module: event-bus
        action: publish
        topic: booking.confirmed`,
  },
  {
    id: 'mq-consumer',
    name: 'Message Queue Consumer',
    description:
      'Kafka message consumer with configurable consumer groups, automatic retries with exponential backoff, and dead letter queue routing for failed messages.',
    complexity: 'Intermediate',
    tags: ['Event-Driven', 'Pipeline'],
    yaml: `name: message-queue-consumer
version: "1.0"

modules:
  - name: kafka-consumer
    type: eventbus
    config:
      provider: kafka
      brokers: ["{{secrets.kafka_brokers}}"]
      group_id: order-processors
      topics: ["orders.pending"]

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.payload"
          target: "$.order"

  - name: order-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.order_db_dsn}}"

  - name: dlq
    type: eventbus
    config:
      provider: kafka
      brokers: ["{{secrets.kafka_brokers}}"]
      topic_prefix: "dlq."

workflows:
  - name: process-order
    trigger:
      type: messaging
      topic: orders.pending
    retry:
      max_attempts: 3
      backoff: exponential
      initial_delay: "1s"
    steps:
      - module: transformer
      - module: order-db
        action: exec
        sql: "INSERT INTO processed_orders (id, data, status) VALUES ($1, $2, 'done')"
    error_handler:
      module: dlq
      topic: orders.pending`,
  },
  {
    id: 'auth-service',
    name: 'Authentication Service',
    description:
      'JWT-based authentication service with user registration, login, and token refresh endpoints. Passwords are hashed with bcrypt and tokens stored in a database.',
    complexity: 'Intermediate',
    tags: ['HTTP', 'Database'],
    yaml: `name: authentication-service
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: auth
    type: auth_middleware
    config:
      provider: jwt
      secret: "{{secrets.jwt_secret}}"
      expiry: "24h"
      refresh_expiry: "7d"

  - name: user-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.auth_db_dsn}}"

  - name: validator
    type: json_schema
    config:
      strict: true

workflows:
  - name: register
    trigger:
      type: http
      method: POST
      path: /api/auth/register
    steps:
      - module: validator
        schema: user-register
      - module: user-db
        action: exec
        sql: "INSERT INTO users (email, password_hash, created_at) VALUES ($1, $2, NOW())"
      - module: auth
        action: issue_token

  - name: login
    trigger:
      type: http
      method: POST
      path: /api/auth/login
    steps:
      - module: user-db
        action: query
        sql: "SELECT id, password_hash FROM users WHERE email=$1"
      - module: auth
        action: verify_password
      - module: auth
        action: issue_token

  - name: refresh
    trigger:
      type: http
      method: POST
      path: /api/auth/refresh
    steps:
      - module: auth
        action: refresh_token`,
  },
  {
    id: 'file-processing',
    name: 'File Processing Pipeline',
    description:
      'Processes uploaded files through a multi-stage pipeline: receive via HTTP, validate format, apply transformations, and store results in a database with S3 archival.',
    complexity: 'Intermediate',
    tags: ['Pipeline', 'Database'],
    yaml: `name: file-processing-pipeline
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080
      max_body_size: "50MB"

  - name: file-store
    type: http_client
    config:
      base_url: "{{secrets.s3_endpoint}}"
      headers:
        Authorization: "Bearer {{secrets.s3_token}}"

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.raw"
          target: "$.normalized"
          transform: "flatten"

  - name: results-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.results_dsn}}"

  - name: event-bus
    type: eventbus
    config:
      provider: memory

workflows:
  - name: upload-file
    trigger:
      type: http
      method: POST
      path: /api/files/upload
    steps:
      - module: file-store
        action: PUT
        path: "/raw/{{file.name}}"
      - module: event-bus
        action: publish
        topic: file.uploaded

  - name: process-file
    trigger:
      type: messaging
      topic: file.uploaded
    steps:
      - module: file-store
        action: GET
        path: "/raw/{{file.name}}"
      - module: transformer
      - module: results-db
        action: exec
        sql: "INSERT INTO processed_files (name, result, processed_at) VALUES ($1, $2, NOW())"
      - module: file-store
        action: PUT
        path: "/processed/{{file.name}}"`,
  },
  {
    id: 'multi-tenant-saas',
    name: 'Multi-Tenant SaaS Backend',
    description:
      'Multi-tenant backend with company, organization, and project hierarchy. Includes role-based access control, tenant-scoped data isolation, and admin management APIs.',
    complexity: 'Advanced',
    tags: ['HTTP', 'Database'],
    yaml: `name: multi-tenant-saas-backend
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: auth
    type: auth_middleware
    config:
      provider: jwt
      secret: "{{secrets.jwt_secret}}"
      tenant_header: "X-Tenant-ID"

  - name: tenant-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.tenant_db_dsn}}"
      multi_tenant:
        enabled: true
        isolation: schema

  - name: cache
    type: cache
    config:
      provider: redis
      address: "{{secrets.redis_url}}"

  - name: validator
    type: json_schema
    config:
      strict: true

workflows:
  - name: list-projects
    trigger:
      type: http
      method: GET
      path: /api/orgs/:orgId/projects
    steps:
      - module: auth
        required_role: member
      - module: tenant-db
        action: query
        sql: "SELECT * FROM projects WHERE org_id=$1"

  - name: create-project
    trigger:
      type: http
      method: POST
      path: /api/orgs/:orgId/projects
    steps:
      - module: auth
        required_role: admin
      - module: validator
        schema: project-create
      - module: tenant-db
        action: exec
        sql: "INSERT INTO projects (org_id, name, created_by) VALUES ($1, $2, $3)"

  - name: manage-members
    trigger:
      type: http
      method: POST
      path: /api/orgs/:orgId/members
    steps:
      - module: auth
        required_role: owner
      - module: tenant-db
        action: exec
        sql: "INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)"`,
  },
  {
    id: 'iot-telemetry',
    name: 'IoT Device Telemetry',
    description:
      'Ingests telemetry data from IoT devices, stores time-series readings, computes rolling aggregates, and triggers alerts when thresholds are exceeded.',
    complexity: 'Intermediate',
    tags: ['Event-Driven', 'Database', 'Monitoring'],
    yaml: `name: iot-device-telemetry
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: event-bus
    type: eventbus
    config:
      provider: memory
      buffer_size: 10000

  - name: timeseries-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.timeseries_dsn}}"

  - name: alert-notifier
    type: http_client
    config:
      base_url: "https://alerts.internal"
      timeout: "5s"

  - name: metrics
    type: prometheus_exporter
    config:
      port: 9090

workflows:
  - name: ingest-reading
    trigger:
      type: http
      method: POST
      path: /api/devices/:deviceId/telemetry
    steps:
      - module: timeseries-db
        action: exec
        sql: "INSERT INTO readings (device_id, metric, value, ts) VALUES ($1, $2, $3, NOW())"
      - module: metrics
        action: gauge
        name: device_reading
        labels: ["device_id", "metric"]
      - module: event-bus
        action: publish
        topic: "telemetry.{{params.deviceId}}"

  - name: check-thresholds
    trigger:
      type: messaging
      topic: "telemetry.*"
    steps:
      - module: timeseries-db
        action: query
        sql: "SELECT AVG(value) as avg_val FROM readings WHERE device_id=$1 AND ts > NOW() - INTERVAL '5 min'"
      - module: alert-notifier
        action: POST
        path: /evaluate
        condition: "avg_val > threshold"`,
  },
  {
    id: 'content-management',
    name: 'Content Management API',
    description:
      'Blog and CMS REST API with full CRUD operations, content versioning, and draft/publish workflow. Supports categories and tag-based filtering.',
    complexity: 'Simple',
    tags: ['HTTP', 'Database'],
    yaml: `name: content-management-api
version: "1.0"

modules:
  - name: http-server
    type: httpserver
    config:
      port: 8080

  - name: auth
    type: auth_middleware
    config:
      provider: jwt
      secret: "{{secrets.jwt_secret}}"

  - name: content-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.content_dsn}}"

  - name: validator
    type: json_schema
    config:
      strict: true

workflows:
  - name: list-articles
    trigger:
      type: http
      method: GET
      path: /api/articles
    steps:
      - module: content-db
        action: query
        sql: "SELECT id, title, slug, status, updated_at FROM articles WHERE status='published' ORDER BY updated_at DESC"

  - name: create-article
    trigger:
      type: http
      method: POST
      path: /api/articles
    steps:
      - module: auth
      - module: validator
        schema: article-create
      - module: content-db
        action: exec
        sql: "INSERT INTO articles (title, slug, body, status, version, author_id) VALUES ($1,$2,$3,'draft',1,$4)"

  - name: publish-article
    trigger:
      type: http
      method: POST
      path: /api/articles/:id/publish
    steps:
      - module: auth
      - module: content-db
        action: exec
        sql: "UPDATE articles SET status='published', version=version+1, published_at=NOW() WHERE id=$1"`,
  },
  {
    id: 'notification-hub',
    name: 'Notification Hub',
    description:
      'Multi-channel notification dispatch service that routes messages to email, SMS, Slack, and push notification providers based on user preferences and event type.',
    complexity: 'Simple',
    tags: ['Event-Driven', 'Integration'],
    yaml: `name: notification-hub
version: "1.0"

modules:
  - name: event-bus
    type: eventbus
    config:
      provider: memory
      buffer_size: 5000

  - name: email-provider
    type: http_client
    config:
      base_url: "https://api.sendgrid.com/v3"
      headers:
        Authorization: "Bearer {{secrets.sendgrid_key}}"

  - name: sms-provider
    type: http_client
    config:
      base_url: "https://api.twilio.com"
      timeout: "10s"

  - name: slack-provider
    type: http_client
    config:
      base_url: "https://hooks.slack.com"

  - name: push-provider
    type: http_client
    config:
      base_url: "https://fcm.googleapis.com/v1"
      headers:
        Authorization: "Bearer {{secrets.fcm_token}}"

workflows:
  - name: send-email
    trigger:
      type: messaging
      topic: notify.email
    steps:
      - module: email-provider
        action: POST
        path: /mail/send

  - name: send-sms
    trigger:
      type: messaging
      topic: notify.sms
    steps:
      - module: sms-provider
        action: POST
        path: /Messages.json

  - name: send-slack
    trigger:
      type: messaging
      topic: notify.slack
    steps:
      - module: slack-provider
        action: POST
        path: "/services/{{secrets.slack_webhook}}"

  - name: send-push
    trigger:
      type: messaging
      topic: notify.push
    steps:
      - module: push-provider
        action: POST
        path: /messages:send`,
  },
  {
    id: 'search-indexer',
    name: 'Search Indexer Pipeline',
    description:
      'CDC-powered search indexing pipeline that captures database changes, applies JQ transforms to shape documents, and upserts them into a search index for full-text queries.',
    complexity: 'Intermediate',
    tags: ['Pipeline', 'Event-Driven', 'Database'],
    yaml: `name: search-indexer-pipeline
version: "1.0"

modules:
  - name: source-db
    type: database
    config:
      driver: postgres
      dsn: "{{secrets.source_dsn}}"
      cdc:
        enabled: true
        slot: search_indexer_slot

  - name: event-bus
    type: eventbus
    config:
      provider: memory
      buffer_size: 5000

  - name: transformer
    type: json_transform
    config:
      mappings:
        - source: "$.new"
          target: "$.document"
          transform: "jq"
          expression: '{id: .id, title: .title, body: .body, tags: (.tags // [] | join(" ")), updated: .updated_at}'

  - name: search-client
    type: http_client
    config:
      base_url: "{{secrets.search_endpoint}}"
      headers:
        Authorization: "Bearer {{secrets.search_api_key}}"
      timeout: "10s"

  - name: metrics
    type: prometheus_exporter
    config:
      port: 9090

workflows:
  - name: index-change
    trigger:
      type: messaging
      topic: cdc.source-db
    steps:
      - module: transformer
      - module: search-client
        action: PUT
        path: "/indexes/content/documents/{{document.id}}"
      - module: metrics
        action: counter
        name: documents_indexed

  - name: reindex-all
    trigger:
      type: http
      method: POST
      path: /api/reindex
    steps:
      - module: source-db
        action: query
        sql: "SELECT * FROM content ORDER BY id"
      - module: transformer
      - module: search-client
        action: POST
        path: /indexes/content/documents/batch`,
  },
];

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function ComplexityBadge({ complexity }: { complexity: Complexity }) {
  const color = COMPLEXITY_COLORS[complexity];
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 10px',
        borderRadius: 12,
        fontSize: 11,
        fontWeight: 600,
        background: color + '22',
        color,
      }}
    >
      {complexity}
    </span>
  );
}

function TemplateBadge({ tag }: { tag: string }) {
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

function TemplateCard({
  template,
  onPreview,
  onUse,
}: {
  template: Template;
  onPreview: (t: Template) => void;
  onUse: (t: Template) => void;
}) {
  return (
    <div
      onClick={() => onPreview(template)}
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
        <div style={{ color: '#cdd6f4', fontWeight: 600, fontSize: 14 }}>{template.name}</div>
        <ComplexityBadge complexity={template.complexity} />
      </div>

      <div style={{ fontSize: 12, color: '#a6adc8', lineHeight: 1.4, flex: 1 }}>
        {template.description}
      </div>

      <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
        {template.tags.map((tag) => (
          <TemplateBadge key={tag} tag={tag} />
        ))}
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 4 }}>
        <button
          onClick={(e) => {
            e.stopPropagation();
            onUse(template);
          }}
          style={{
            padding: '6px 16px',
            borderRadius: 4,
            border: 'none',
            fontSize: 12,
            fontWeight: 600,
            cursor: 'pointer',
            background: '#89b4fa',
            color: '#1e1e2e',
          }}
        >
          Use Template
        </button>
      </div>
    </div>
  );
}

function TemplatePreviewModal({
  template,
  onClose,
  onUse,
}: {
  template: Template;
  onClose: () => void;
  onUse: (t: Template) => void;
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
            <h2 style={{ color: '#cdd6f4', margin: 0, fontSize: 20, fontWeight: 600 }}>{template.name}</h2>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
              <ComplexityBadge complexity={template.complexity} />
              {template.tags.map((tag) => (
                <TemplateBadge key={tag} tag={tag} />
              ))}
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

        <p style={{ color: '#a6adc8', fontSize: 13, lineHeight: 1.6, marginBottom: 20 }}>{template.description}</p>

        {/* YAML config */}
        <h3 style={{ color: '#cdd6f4', fontSize: 14, fontWeight: 600, marginBottom: 8 }}>Workflow Configuration</h3>
        <pre
          style={{
            background: '#181825',
            border: '1px solid #313244',
            borderRadius: 6,
            padding: 12,
            fontSize: 12,
            color: '#89b4fa',
            overflow: 'auto',
            maxHeight: 400,
            marginBottom: 20,
            lineHeight: 1.5,
          }}
        >
          {template.yaml}
        </pre>

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button
            onClick={onClose}
            style={{
              padding: '8px 20px',
              borderRadius: 6,
              border: '1px solid #45475a',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              background: 'transparent',
              color: '#a6adc8',
            }}
          >
            Close
          </button>
          <button
            onClick={() => onUse(template)}
            style={{
              padding: '8px 24px',
              borderRadius: 6,
              border: 'none',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
              background: '#89b4fa',
              color: '#1e1e2e',
            }}
          >
            Use Template
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Templates() {
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [complexityFilter, setComplexityFilter] = useState<Complexity | 'All'>('All');
  const [tagFilter, setTagFilter] = useState<string>('All');
  const [usedTemplates, setUsedTemplates] = useState<Set<string>>(new Set());

  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const clearCanvas = useWorkflowStore((s) => s.clearCanvas);
  const addToast = useWorkflowStore((s) => s.addToast);
  const nodes = useWorkflowStore((s) => s.nodes);
  const setActiveView = useObservabilityStore((s) => s.setActiveView);

  const filtered = useMemo(() => {
    return MOCK_TEMPLATES.filter((t) => {
      const matchesComplexity = complexityFilter === 'All' || t.complexity === complexityFilter;
      const matchesTag = tagFilter === 'All' || t.tags.includes(tagFilter);
      return matchesComplexity && matchesTag;
    });
  }, [complexityFilter, tagFilter]);

  const handleUse = (template: Template) => {
    if (nodes.length > 0 && !window.confirm('This will replace your current workflow. Any unsaved changes will be lost. Continue?')) return;
    try {
      const config = parseYaml(template.yaml);
      clearCanvas();
      importFromConfig(config);
      setUsedTemplates((prev) => new Set(prev).add(template.id));
      setSelectedTemplate(null);
      setActiveView('editor');
      addToast(`Template "${template.name}" loaded`, 'success');
    } catch (e) {
      console.error('Failed to load template:', e);
      addToast('Failed to load template', 'error');
    }
  };

  return (
    <div
      style={{
        flex: 1,
        background: '#1e1e2e',
        overflow: 'auto',
        padding: 24,
      }}
    >
      <h2 style={{ color: '#cdd6f4', margin: '0 0 4px', fontSize: 20, fontWeight: 600 }}>Template Gallery</h2>
      <p style={{ color: '#6c7086', fontSize: 13, margin: '0 0 20px' }}>
        Start with a pre-built workflow template and customize it for your needs.
      </p>

      {/* Filters */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 20, flexWrap: 'wrap' }}>
        <select
          value={complexityFilter}
          onChange={(e) => setComplexityFilter(e.target.value as Complexity | 'All')}
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
          <option value="All">All Complexities</option>
          <option value="Simple">Simple</option>
          <option value="Intermediate">Intermediate</option>
          <option value="Advanced">Advanced</option>
        </select>
        <select
          value={tagFilter}
          onChange={(e) => setTagFilter(e.target.value)}
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
          <option value="All">All Tags</option>
          {ALL_TAGS.map((tag) => (
            <option key={tag} value={tag}>
              {tag}
            </option>
          ))}
        </select>
      </div>

      {/* Summary */}
      <div style={{ fontSize: 12, color: '#6c7086', marginBottom: 12 }}>
        Showing {filtered.length} template{filtered.length !== 1 ? 's' : ''}
      </div>

      {/* Template grid */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
          gap: 12,
        }}
      >
        {filtered.map((template) => (
          <div key={template.id} style={{ position: 'relative' }}>
            {usedTemplates.has(template.id) && (
              <div
                style={{
                  position: 'absolute',
                  top: 8,
                  right: 8,
                  background: '#a6e3a122',
                  color: '#a6e3a1',
                  padding: '2px 8px',
                  borderRadius: 4,
                  fontSize: 10,
                  fontWeight: 600,
                  zIndex: 1,
                }}
              >
                Added
              </div>
            )}
            <TemplateCard template={template} onPreview={setSelectedTemplate} onUse={handleUse} />
          </div>
        ))}
        {filtered.length === 0 && (
          <div style={{ color: '#6c7086', fontSize: 13, gridColumn: '1 / -1', padding: 40, textAlign: 'center' }}>
            No templates match your filter criteria.
          </div>
        )}
      </div>

      {/* Preview modal */}
      {selectedTemplate && (
        <TemplatePreviewModal
          template={selectedTemplate}
          onClose={() => setSelectedTemplate(null)}
          onUse={handleUse}
        />
      )}
    </div>
  );
}
