# State Machine Workflow Architecture

This diagram visualizes the order processing state machine workflow.

## State Machine Workflow Engine

```mermaid
graph TD
    API["HTTP API Endpoints"] --> OPE["Order Processor Engine"]

    subgraph StateMachine["Order Processing State Machine Workflow"]
        NEW["NEW"]
        VALIDATING["VALIDATING"]
        VALIDATED["VALIDATED"]
        INVALID["INVALID"]
        CANCELED["CANCELED"]
        PP["PAYMENT PENDING"]
        PF["PAYMENT FAILED"]
        PAID["PAID"]
        FP["FULFILLMENT PENDING"]
        SHIPPED["SHIPPED"]
        DELIVERED["DELIVERED"]
        REFUNDED["REFUNDED"]

        NEW -->|submit order| VALIDATING
        NEW -->|cancel| CANCELED
        VALIDATING -->|validation passed| VALIDATED
        VALIDATING -->|failed| INVALID
        INVALID --> CANCELED
        VALIDATED -->|process payment| PP
        PP -->|declined| PF
        PF -->|retry| PP
        PP -->|succeeded| PAID
        PAID -->|auto fulfill| FP
        FP -->|ship| SHIPPED
        SHIPPED -->|deliver| DELIVERED
        SHIPPED -->|refund shipped| REFUNDED
        CANCELED --> REFUNDED
    end

    OPE --> StateMachine
```

## State Transition Triggers and Handlers

This shows how transitions trigger handlers in the order workflow.

```mermaid
graph LR
    TT["Transition Trigger"] --> OPE["Order Processor Engine"]
    OPE --> H["Handler"]
    OPE --> NH["Notification Handler"]
```

## Example Transition Flow - Happy Path

Visualizes a normal order flow from creation to delivery.

```mermaid
graph LR
    NEW["NEW"] --> VALIDATING["VALIDATING"]
    VALIDATING --> VALIDATED["VALIDATED"]
    VALIDATED --> PP["PAYMENT PENDING"]
    PP --> PAID["PAID"]
    PAID --> FP["FULFILLMENT PENDING"]
    FP --> SHIPPED["SHIPPED"]
    SHIPPED --> DELIVERED["DELIVERED"]
```

## Example Transition Flow - Payment Failure with Retry

Shows how order handles payment failure with retry.

```mermaid
graph LR
    NEW["NEW"] --> VALIDATING["VALIDATING"]
    VALIDATING --> VALIDATED["VALIDATED"]
    VALIDATED --> PP["PAYMENT PENDING"]
    PP --> PF["PAYMENT FAILED"]
    PF -->|retry| PP2["PAYMENT PENDING"]
```
