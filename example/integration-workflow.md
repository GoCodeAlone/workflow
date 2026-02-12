# Integration Workflow Architecture

This diagram visualizes the integration workflow with multiple third-party service connectors.

## Integration Workflow Engine

```mermaid
graph TD
    subgraph IntegrationWorkflow["Integration Workflow Engine"]
        API["HTTP API Endpoints"] --> IR["Integration Registry"]

        IR --> CRM["CRM Connector"]
        IR --> PAY["Payment Connector"]
        IR --> EMAIL["Email Connector"]
        IR --> INV["Inventory Connector"]
        IR --> WH["Webhook Receiver"]

        CRM --> ECRM["External CRM API"]
        PAY --> EPAY["External Payment API"]
        EMAIL --> EEMAIL["External Email API"]
        INV --> EINV["External Inventory System"]
        WH --> ECB["External Services Callbacks"]
    end
```

## Order Processing Integration Flow

This diagram shows the sequence of integration steps for processing an order.

```mermaid
graph LR
    A["(1) HTTP API Request"] --> B["(2) Check Customer<br/>(CRM API)"]
    B --> C["(3) Check Inventory<br/>(Inventory)"]
    C --> D["(4) Process Payment<br/>(Payment API)"]
    D --> E["(5) Update Inventory<br/>(Inventory)"]
    E --> F["(6) Send Confirmation<br/>(Email API)"]
```

## Integration Step with Retry Logic

This shows how retries work in the integration workflow.

```mermaid
graph TD
    SE["Step Execution"] --> S{"Success?"}
    S -->|Yes| NS["Next Step"]
    S -->|No| RA{"Retry Available?"}
    RA -->|Yes| RS["Retry Step"]
    RS --> SE
    RA -->|No| EH["Error Handler"]
    RS -->|Fail| EH
```

## Data Transformation Between Steps

This shows how data is transformed between steps.

```mermaid
graph LR
    S1["Step 1 Result<br/>{id: 123, name: 'X'}"]
    T["Transform Data<br/>{customerId: data.id}"]
    S2["Step 2 Input<br/>{customerId: 123}"]

    S1 --> T --> S2
```
