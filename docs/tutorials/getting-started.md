# Getting Started with Workflow Engine

## Prerequisites

- Go 1.26+
- Git

## Quick Start

### 1. Clone and Build

```bash
git clone https://github.com/GoCodeAlone/workflow.git
cd workflow
go build -o server ./cmd/server
go build -o wfctl ./cmd/wfctl
```

### 2. Run Your First Workflow

```bash
./server -config example/simple-workflow-config.yaml
```

Visit http://localhost:8080 to see your server running.

### 3. Explore Example Configs

The `example/` directory contains 27 top-level `example/*.yaml` configurations, plus additional configs under application subdirectories:

```bash
ls example/*.yaml | head -20
```

Try the order processing pipeline:

```bash
./server -config example/order-processing-pipeline.yaml
```

### 4. Validate a Config

```bash
./wfctl validate example/order-processing-pipeline.yaml
```

### 5. Inspect a Config

```bash
./wfctl inspect example/order-processing-pipeline.yaml
```

This shows modules, workflows, triggers, and the dependency graph.

## Understanding YAML Configs

A workflow config has three main sections:

```yaml
# 1. Modules - the building blocks
modules:
  - name: my-server
    type: http.server
    config:
      address: ":8080"

# 2. Workflows - how modules connect
workflows:
  http:
    routes:
      - method: GET
        path: /hello
        handler: my-handler

# 3. Triggers - what starts execution
triggers:
  - type: http
    config:
      port: 8080
```

## Next Steps

- [Building Plugins](building-plugins.md) - Create custom components
- [Scaling Workflows](scaling-workflows.md) - Production deployment
- [Chat Platform Walkthrough](chat-platform-walkthrough.md) - Full application example
- [API Reference](../API.md) - REST endpoint documentation
