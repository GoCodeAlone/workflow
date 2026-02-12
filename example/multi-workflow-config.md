# Multi-Workflow Architecture

This diagram visualizes how multiple workflows (HTTP and messaging) work together.

## Multi-Workflow Engine

```mermaid
graph TD
    subgraph HTTPWorkflow["HTTP Workflow"]
        HS["HTTP Server (:8080)"] --> AR["API Router"]
        AR --> HH["Health Handler"]
        AR --> AM["Auth Middleware"]
        AM --> UA["Users API"]
        AR --> PA["Products API"]
        HS --> PA
    end

    UA -->|User Events| MB

    subgraph MessagingWorkflow["Messaging Workflow"]
        MB["Message Broker"] --> UEH["User Event Handler"]
        MB --> ALH["Audit Log Handler"]
    end
```

## Request Flow

This shows how a request flows through the system.

```mermaid
graph LR
    CR["Client Request"] --> HS["HTTP Server"]
    HS --> AM["Auth Middleware"]
    AM --> AH["API Handler"]
    AH --> MB["Message Broker"]
    MB --> UEH["User Event Handler"]
    MB --> ALH["Audit Log Handler"]
```
