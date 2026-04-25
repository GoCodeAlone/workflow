---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow-plugin-data-engineering
    commit: 0a65400
  - repo: workflow-plugin-data-engineering
    commit: 9fd31aa
  - repo: workflow-plugin-data-engineering
    commit: 01352fd
  - repo: workflow-plugin-data-engineering
    commit: 655f082
  - repo: workflow-plugin-data-engineering
    commit: b50a5eb
external_refs:
  - "/Users/jon/workspace/workflow-plugin-data-engineering"
verification:
  last_checked: 2026-04-25
  commands:
    - "git -C /Users/jon/workspace/workflow-plugin-data-engineering log --oneline --all"
    - "GOWORK=off go test ./..."
  result: pass
supersedes: []
superseded_by: []
---

# Data Engineering Plugin Design

**Date:** 2026-03-28
**Status:** Approved
**Plugin:** `workflow-plugin-data-engineering`

## Overview

A single gRPC plugin providing data engineering capabilities for the workflow engine: CDC from relational databases, lakehouse table management (Apache Iceberg), time-series ingestion and querying, graph database support (Neo4j + knowledge graphs), schema migrations, data quality checks, and data catalog integration. Multi-tenant by design with configurable isolation strategies.

**Target users:** Data engineers building CDC pipelines, data lake ingestion, real-time analytics, and reporting infrastructure — typically Python-heavy teams moving toward or integrating with Go-based workflow orchestration.

## Architecture

### Approach: Hybrid Monolith

Single plugin binary with modular internal packages organized by domain. Can be split into separate plugins later if needed.

```
workflow-plugin-data-engineering/
├── main.go
├── internal/
│   ├── cdc/          # CDC providers (Bento, Debezium, DMS)
│   ├── lakehouse/    # Iceberg tables, catalog, Trino queries
│   ├── timeseries/   # InfluxDB, TimescaleDB, ClickHouse, QuestDB, Druid
│   ├── graph/        # Neo4j, knowledge graph extraction
│   ├── quality/      # Go-native checks + optional Python tool providers
│   ├── migrate/      # Declarative + scripted schema migrations
│   ├── catalog/      # DataHub, OpenMetadata, schema registry
│   └── tenancy/      # Multi-tenancy strategies
├── embedded/
└── go.mod
```

### Go-Native Libraries

| Domain | Library |
|--------|---------|
| CDC/Bento | Existing `workflow-plugin-bento` (subprocess) or `warpstreamlabs/bento` direct import |
| Debezium | HTTP client → Kafka Connect REST API |
| AWS DMS | `aws-sdk-go-v2/service/databasemigrationservice` |
| Iceberg catalog | HTTP client → Iceberg REST Catalog spec |
| Schema registry | HTTP client → Confluent Schema Registry REST API |
| Neo4j | `neo4j/neo4j-go-driver/v5` (Bolt protocol) |
| InfluxDB | `influxdata/influxdb-client-go/v2` |
| TimescaleDB | `jackc/pgx` via `database.workflow` (it's Postgres) |
| ClickHouse | `ClickHouse/clickhouse-go/v2` (native protocol) |
| QuestDB | `questdb/go-questdb-client/v3` (ILP protocol) |
| Druid | HTTP client → Druid SQL/native query API |
| DataHub | HTTP client → DataHub GMS REST API |
| OpenMetadata | HTTP client → OpenMetadata REST API |
| Data quality | Go-native (SQL queries, `gonum` for statistics) |
| dbt/Soda/GE | `os/exec` wrappers (opt-in Python fallback providers) |

## Module Types

### CDC

| Module | Purpose | Key Config |
|--------|---------|------------|
| `cdc.source` | CDC stream from a database | `provider: bento\|debezium\|dms`, `source: {type: postgres, connection: ..., tables: [...]}` |

**CDCProvider interface:**

```go
type CDCProvider interface {
    // Connect establishes a connection and starts the CDC stream.
    Connect(ctx context.Context, config SourceConfig) error
    // Disconnect stops the CDC stream and releases resources.
    Disconnect(ctx context.Context, sourceID string) error
    // Status returns the current status of a CDC stream.
    Status(ctx context.Context, sourceID string) (*CDCStatus, error)
    // Snapshot triggers a full table snapshot for the given tables.
    Snapshot(ctx context.Context, sourceID string, tables []string) error
    // SchemaHistory returns the schema change history for a table.
    SchemaHistory(ctx context.Context, sourceID string, table string) ([]SchemaVersion, error)
    // RegisterEventHandler registers a callback for CDC events.
    // The trigger uses this to wire workflow callbacks to the stream.
    RegisterEventHandler(sourceID string, h EventHandler) error
}
```

- **Bento provider**: Generates Bento YAML configs for Postgres/MySQL/DynamoDB CDC inputs. Leverages existing `bento.stream` module pattern.
- **Debezium provider**: Manages Debezium connectors via Kafka Connect REST API. Requires external Kafka Connect cluster.
- **DMS provider**: Creates/manages AWS DMS replication tasks via AWS SDK. Targets Kinesis or Kafka.

### Lakehouse

| Module | Purpose | Key Config |
|--------|---------|------------|
| `lakehouse.table` | Iceberg table management | `catalog: my-catalog`, `namespace: analytics`, `format: iceberg` |
| `catalog.iceberg` | Iceberg REST Catalog connection | `endpoint: https://...`, `warehouse: s3://...`, `credentials: {...}` |
| `catalog.schema_registry` | Schema registry connection | `provider: confluent\|glue`, `endpoint: https://...` |

### Time-Series

| Module | Purpose | Key Config |
|--------|---------|------------|
| `timeseries.influxdb` | InfluxDB connection | `url: https://...`, `token: ...`, `org: ...`, `bucket: ...` |
| `timeseries.timescaledb` | TimescaleDB (Postgres ext) | `connection: postgres://...` |
| `timeseries.clickhouse` | ClickHouse connection | `endpoints: [https://...]`, `database: analytics` |
| `timeseries.questdb` | QuestDB connection (ILP) | `endpoint: localhost:9009`, `auth: {...}` |
| `timeseries.druid` | Apache Druid connection | `routerUrl: https://...`, `auth: {user, password}` |

### Graph

| Module | Purpose | Key Config |
|--------|---------|------------|
| `graph.neo4j` | Neo4j database connection | `uri: bolt://...`, `auth: {user, password}`, `database: neo4j` |

### Data Quality

| Module | Purpose | Key Config |
|--------|---------|------------|
| `quality.checks` | Data quality check definitions | `provider: builtin\|dbt\|soda\|great_expectations` |

### Migrations

| Module | Purpose | Key Config |
|--------|---------|------------|
| `migrate.schema` | Schema migration management | `strategy: declarative\|scripted`, `target: my-db` |

### Tenancy

| Module | Purpose | Key Config |
|--------|---------|------------|
| `data.tenancy` | Multi-tenancy configuration | `strategy: schema_per_tenant\|db_per_tenant\|row_level`, `tenantKey: ctx.tenant_id` |

## Step Types

### CDC Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.cdc_start` | Start a CDC stream | `source: my-cdc-source`, `tables: [users, orders]`, `snapshot: initial` |
| `step.cdc_stop` | Gracefully stop a CDC stream | `source: my-cdc-source` |
| `step.cdc_status` | Check stream health/lag | `source: my-cdc-source` → outputs: `lag`, `status`, `tables` |
| `step.cdc_snapshot` | Trigger re-snapshot of tables | `source: my-cdc-source`, `tables: [users]` |
| `step.cdc_schema_history` | Get schema evolution history | `source: my-cdc-source`, `table: users` |

### Lakehouse Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.lakehouse_create_table` | Create/evolve Iceberg table | `catalog`, `namespace`, `table`, `schema: {fields: [...]}` |
| `step.lakehouse_evolve_schema` | Safe schema evolution | `table`, `changes: [{add_column: {name, type}}]` |
| `step.lakehouse_write` | Write data to table | `table`, `data`, `mode: append\|upsert\|overwrite`, `mergeKey` |
| `step.lakehouse_compact` | Trigger compaction | `table`, `targetFileSize: 256MB` |
| `step.lakehouse_snapshot` | Create/manage snapshots | `table`, `action: create\|list\|rollback` |
| `step.lakehouse_query` | Query via Trino/catalog | `engine: trino`, `query`, `params` |
| `step.lakehouse_expire_snapshots` | Retention cleanup | `table`, `olderThan: 7d` |

### Time-Series Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.ts_write` | Write data points | `module`, `measurement`, `tags`, `fields`, `timestamp` |
| `step.ts_write_batch` | Batch write (high throughput) | `module`, `points: [...]` |
| `step.ts_query` | Query time-series data | `module`, `query` |
| `step.ts_downsample` | Aggregate/downsample | `module`, `source`, `target`, `aggregation`, `interval` |
| `step.ts_retention` | Manage retention policies | `module`, `policy: {duration, replication}` |
| `step.ts_continuous_query` | Continuous aggregation | `module`, `query`, `interval`, `materialized: true` |
| `step.ts_druid_ingest` | Druid ingestion spec | `module`, `spec: {type: kafka, topic: ...}` |
| `step.ts_druid_query` | Druid SQL/native query | `module`, `query`, `queryType: sql\|native` |
| `step.ts_druid_datasource` | Manage Druid datasources | `module`, `action: create\|delete\|disable` |
| `step.ts_druid_compact` | Trigger Druid compaction | `module`, `datasource`, `interval` |

### Graph Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.graph_query` | Execute Cypher query | `module`, `cypher`, `params` |
| `step.graph_write` | Create/update nodes + rels | `module`, `nodes: [...]`, `relationships: [...]` |
| `step.graph_import` | Bulk import from relational data | `module`, `source`, `mapping: {node, properties}` |
| `step.graph_extract_entities` | Entity extraction for knowledge graph | `text`, `types: [person, org, location]` |
| `step.graph_link` | Create relationships between entities | `module`, `from: {label, key}`, `to: {label, key}`, `type` |

### Data Quality Steps

Go-native implementations as primary path. Python tool providers (dbt, Soda, GE) available as opt-in fallbacks.

| Step | Purpose | Implementation |
|------|---------|----------------|
| `step.quality_check` | Run quality assertions (not_null, unique, freshness, row_count, referential) | Go-native SQL queries |
| `step.quality_schema_validate` | Validate data against schema contract | Go-native JSON Schema |
| `step.quality_profile` | Profile dataset (stats, distributions, percentiles) | Go-native (`gonum`) |
| `step.quality_compare` | Compare two datasets (regression detection) | Go-native diff |
| `step.quality_anomaly` | Anomaly detection (Z-score, IQR) on values | Go-native stats |
| `step.quality_dbt_test` | Run dbt tests | Shell out (opt-in) |
| `step.quality_soda_check` | Run Soda checks | Shell out (opt-in) |
| `step.quality_ge_validate` | Run Great Expectations | Shell out (opt-in) |

**Data contract format:**

```yaml
dataset: raw.users
owner: data-team
schema:
  columns:
    - name: id
      type: bigint
      nullable: false
    - name: email
      type: varchar
      nullable: false
      pattern: "^[a-zA-Z0-9+_.-]+@[a-zA-Z0-9.-]+$"
quality:
  - type: freshness
    maxAge: 1h
  - type: row_count
    min: 1000
  - type: not_null
    columns: [id, email]
  - type: unique
    columns: [id]
```

### Migration Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.migrate_plan` | Diff current vs desired schema | `module`, `desired: ./schemas/v2.yaml` → outputs: `plan`, `safe: bool` |
| `step.migrate_apply` | Apply migration plan | `plan`, `mode: online\|blue_green` |
| `step.migrate_run` | Run numbered migration script | `module`, `script: ./migrations/003_add_email.sql` |
| `step.migrate_rollback` | Rollback last migration | `module`, `steps: 1` |
| `step.migrate_status` | Check migration state | `module` → outputs: `version`, `pending`, `applied` |

**Declarative schema definition:**

```yaml
table: users
columns:
  - name: id
    type: bigint
    primaryKey: true
  - name: email
    type: varchar(255)
    nullable: false
    unique: true
  - name: created_at
    type: timestamptz
    default: now()
indexes:
  - columns: [email]
    unique: true
```

Engine diffs against live schema. Non-breaking changes apply automatically. Breaking changes respect `onBreakingChange` policy (block/warn/blue_green).

### Catalog Steps

| Step | Purpose | Key Config |
|------|---------|------------|
| `step.catalog_register` | Register dataset in catalog | `catalog: datahub\|openmetadata`, `dataset`, `schema`, `owner` |
| `step.catalog_search` | Search catalog for datasets | `catalog`, `query` |
| `step.schema_register` | Register schema version | `registry`, `subject`, `schema` |
| `step.schema_validate` | Validate against registered schema | `registry`, `subject`, `data` |
| `step.contract_validate` | Validate data contract | `contract: contracts/users.yaml`, `data` |

### Tenancy Steps

| Step | Purpose |
|------|---------|
| `step.tenant_provision` | Create schema/database/namespace for new tenant |
| `step.tenant_deprovision` | Archive/clean up tenant data |
| `step.tenant_migrate` | Run migrations per-tenant (parallelism + circuit-breaker) |

## Multi-Tenancy

The `data.tenancy` module configures tenant isolation across all data modules:

```yaml
modules:
  - name: tenancy
    type: data.tenancy
    config:
      strategy: schema_per_tenant
      tenantKey: ctx.tenant_id
      schemaPrefix: "tenant_"
```

**Strategies:**
- `schema_per_tenant`: Table references auto-prefixed with `tenant_<id>.` schema
- `db_per_tenant`: Per-tenant connection pool from `connectionTemplate`
- `row_level`: Auto-injected `WHERE tenant_id = $1` and tenant_id in writes

All data steps resolve tenant context via `tenantKey` dot-path. CDC sources can be shared (with Bento processor routing) or per-tenant.

## Example Pipeline: Full CDC → Lakehouse

```yaml
modules:
  - name: cdc_source
    type: cdc.source
    config:
      provider: bento
      source:
        type: postgres
        connection: "{{ config \"aurora_connection\" }}"
        tables: [users, orders]
        publication: workflow_cdc

  - name: analytics_catalog
    type: catalog.iceberg
    config:
      endpoint: "{{ config \"iceberg_catalog_url\" }}"
      warehouse: "s3://data-lake/warehouse"

  - name: tenancy
    type: data.tenancy
    config:
      strategy: schema_per_tenant
      tenantKey: ctx.tenant_id

pipelines:
  - name: cdc_users_to_lakehouse
    trigger:
      type: event
      config:
        topic: cdc.public.users
    steps:
      - name: transform
        type: step.bento
        config:
          processors:
            - bloblang: |
                root = this
                root.ingested_at = now()
                root.tenant_id = meta("tenant_id")

      - name: validate
        type: step.quality_check
        config:
          checks:
            - type: not_null
              columns: [id, email]
            - type: schema
              contract: contracts/users.yaml

      - name: write_lakehouse
        type: step.lakehouse_write
        config:
          catalog: analytics_catalog
          table: raw.users
          data: "{{ .steps.transform.records }}"
          mode: upsert
          mergeKey: id

      - name: write_timeseries
        type: step.ts_write
        config:
          module: metrics_influx
          measurement: cdc_events
          tags:
            table: users
            operation: "{{ .steps.transform.op }}"
          fields:
            count: 1
          timestamp: "{{ .steps.transform.ingested_at }}"

      - name: update_catalog
        type: step.catalog_register
        config:
          catalog: datahub
          dataset: raw.users
          freshness: "{{ now }}"
```

## Phased Delivery

### Phase 1: CDC + Data Movement
- `cdc.source` module (Bento, Debezium, DMS providers)
- `step.cdc_*` steps (start, stop, status, snapshot, schema_history)
- `data.tenancy` module (all 3 strategies)
- `step.tenant_*` steps (provision, deprovision, migrate)
- Unit + integration tests, 2-3 workflow-scenarios

### Phase 2: Lakehouse + Time-Series
- `catalog.iceberg` module + Iceberg REST client
- `lakehouse.table` module + `step.lakehouse_*` steps
- Time-series modules: InfluxDB, TimescaleDB, ClickHouse, QuestDB, Druid
- `step.ts_*` steps (write, batch, query, downsample, retention, continuous_query)
- Druid-specific steps (ingest, query, datasource, compact)
- `catalog.schema_registry` module

### Phase 3: Migrations + Data Quality
- `migrate.schema` module (declarative + scripted)
- `step.migrate_*` steps (plan, apply, run, rollback, status)
- `quality.checks` module with Go-native provider
- `step.quality_*` steps (check, schema_validate, profile, compare, anomaly)
- Optional Python providers (dbt, Soda, GE)

### Phase 4: Graph + Knowledge Graphs + Catalog
- `graph.neo4j` module + Neo4j Go driver
- `step.graph_*` steps (query, write, import, extract_entities, link)
- DataHub/OpenMetadata catalog integration
- `step.catalog_*` and `step.contract_*` steps

## Registry Manifest

```yaml
name: data-engineering
version: "0.1.0"
description: "Data engineering: CDC, lakehouse, time-series, graph, data quality, migrations"
type: external
license: Commercial
repository: GoCodeAlone/workflow-plugin-data-engineering
capabilities:
  modules:
    - cdc.source
    - lakehouse.table
    - catalog.iceberg
    - catalog.schema_registry
    - graph.neo4j
    - quality.checks
    - migrate.schema
    - data.tenancy
    - timeseries.influxdb
    - timeseries.timescaledb
    - timeseries.clickhouse
    - timeseries.questdb
    - timeseries.druid
  triggers:
    - trigger.cdc
```
