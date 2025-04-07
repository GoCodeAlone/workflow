# Workflow Engine Examples

This directory contains example configurations for the workflow engine. Each example demonstrates a different type of workflow and functionality.

## Running an Example

There are two ways to run the examples:

### Option 1: Specify a configuration file

```bash
go run main.go -config <configuration-file>.yaml
```

For instance, to run the state machine workflow example:

```bash
go run main.go -config state-machine-workflow.yaml
```

### Option 2: Interactive selection menu

Simply run the main.go without any arguments to see an interactive menu of all available workflow configurations:

```bash
go run main.go
```

This will display a numbered list of all available workflow configurations. Enter the number corresponding to the workflow you want to run.

## Example Types

### State Machine Workflow

A workflow that tracks explicit states and transitions, demonstrated in `state-machine-workflow.yaml`. This example models an e-commerce order processing system with states like "new", "validating", "paid", and "shipped".

### Event-Driven Workflow

A complex event pattern processing system, demonstrated in `event-driven-workflow.yaml`. This example detects patterns like login brute force attempts and system faults.

### Integration Workflow

An integration workflow connecting to third-party services, demonstrated in `integration-workflow.yaml`. It shows how to connect to external APIs for CRM, payment processing, and inventory management.

### Multi-Workflow Configuration

Running multiple workflows in parallel, demonstrated in `multi-workflow-config.yaml`. This example combines HTTP routing with message-based workflows.

### Advanced Scheduler Workflow

A scheduler for executing various jobs at different intervals, demonstrated in `advanced-scheduler-workflow.yaml`. It includes jobs running at minute, hourly, and daily intervals.

### API Gateway and Server

HTTP API examples shown in `api-gateway-config.yaml` and `api-server-config.yaml`. These demonstrate building RESTful APIs and routing between services.

### Other Examples

- `data-pipeline-config.yaml`: Data processing workflows
- `dependency-injection-example.yaml`: Service injection patterns
- `event-processor-config.yaml`: Event processing with messaging
- `scheduled-jobs-config.yaml`: Recurring task scheduling
- `sms-chat-config.yaml`: Messaging-based workflow

## Understanding the Diagrams

Some examples include `.txt` files with ASCII diagrams that visualize the workflow structure. For example, `state-machine-workflow.txt` shows the state diagram for the order processing workflow.