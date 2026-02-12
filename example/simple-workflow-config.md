# Simple Workflow Architecture

This diagram visualizes a simple workflow with HTTP and messaging components.

## Simple Workflow Engine

```mermaid
graph TD
    subgraph HTTPWorkflow["HTTP Workflow"]
        HS["HTTP Server (:8080)"] --> HR["HTTP Router"]
    end

    AM["Auth Middleware"]

    HR --> AM

    subgraph Services["Services"]
        US["User Service"]
    end

    AM -->|GET /api/users| US
    HR -->|GET /health| US

    subgraph MessagingWorkflow["Messaging Workflow"]
        MB["Message Broker"]
        NS["Notification Service"]
        MB --> NS
    end

    US --> MB
```

## Request Flow

This shows how a request flows through the system.

```mermaid
graph LR
    CR["Client Request"] --> HS["HTTP Server"]
    HS --> HR["HTTP Router"]
    HR --> AM["Auth Middleware"]
    AM --> US["User Service"]
    US --> MB["Message Broker"]
    MB --> NS["Notification Service"]
```
