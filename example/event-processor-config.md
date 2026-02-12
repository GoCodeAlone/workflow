# Event Processor Architecture

This diagram visualizes the event processing workflow with multiple handlers.

## Event Processor Workflow

```mermaid
graph TD
    subgraph EventProcessorWorkflow["Event Processor Workflow"]
        EB["Event Broker"]
        ER["Event Receiver"]
        EH["Error Handler"]
        PE["Processed Events"]
        EE["Error Events"]
        EN["Event Notifier"]

        EB --> ER
        EB --> EH
        ER --> PE
        ER --> EE
        EE --> EH
        PE --> EN
    end
```

## Event Flow

This shows how events flow through the system.

```mermaid
graph LR
    IE["Incoming Events"] --> ER["Event Receiver"]
    ER --> PE["Processed Events"]
    PE --> EN["Event Notifier"]
    ER --> EE["Error Events"]
    EE --> EH["Error Handler"]
```
