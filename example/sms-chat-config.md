# SMS Chat Workflow Architecture

This diagram visualizes the SMS chat workflow with triage and agent assignment.

## SMS Chat Workflow

```mermaid
graph TD
    subgraph SMSChatWorkflow["SMS Chat Workflow"]
        SMSS["SMS HTTP Server (:8081)"] --> SR["SMS Router"]
        SR --> SWH["SMS Webhook Handler"]
        SWH --> MB["Message Broker"]

        MB -->|Incoming SMS| TSH["Triage Survey Handler"]
        TSH -->|Triage Complete| AAH["Agent Assignment Handler"]
        AAH -->|Agent Response| CRH["Chat Response Handler"]
        CRH -->|Outgoing SMS| NH["Notification Handler"]
    end
```

## SMS Chat Flow

This shows how messages flow through the SMS chat system.

```mermaid
graph LR
    CS["Customer SMS"] --> SW["SMS Webhook"]
    SW --> TS["Triage Survey"]
    TS --> AA["Agent Assignment"]
    AA --> RS["Response to SMS"]
```
