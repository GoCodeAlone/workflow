# Trigger-Based Workflow Architecture

This diagram visualizes how triggers connect to workflows.

## Workflow Engine with Triggers

```mermaid
graph TD
    subgraph WorkflowEngine["Workflow Engine with Triggers"]
        HT["HTTP Triggers"] --> WE["Workflow Engine"]
        ET["Event Triggers"] --> WE
        ST["Schedule Triggers"] --> WE

        HT --> AR["API Router (/workflows)"]
        ET --> MB["Message Broker"]
        ST --> CS["Cron Scheduler"]
    end
```

## Workflow Execution Paths

This shows how different triggers initiate the same workflows.

```mermaid
graph TD
    HR["HTTP Request"] --> TS["Trigger System"]
    EM["Event Message"] --> TS
    CS["Cron Schedule"] --> TS

    TS --> WE["Workflow Engine"]

    WE --> HW["HTTP Workflows"]
    WE --> SM["State Machines"]
    WE --> EW["Event Workflows"]
```

## Example: Order Processing with Multiple Triggers

Shows how an order workflow can be initiated from multiple sources.

```mermaid
graph TD
    HP["HTTP POST<br/>/workflows/orders/submit"] --> SMO
    EV["Event:<br/>order.created"] --> SMO
    SC["Schedule:<br/>Hourly Batch Processing"] --> SMO

    SMO["State Machine:<br/>order-workflow<br/>Action: submit"]
    SMO --> OP["Order in<br/>processing State"]
```
