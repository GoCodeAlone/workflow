# Data Pipeline Architecture

This diagram visualizes the data processing pipeline workflow.

## Data Pipeline Workflow

```mermaid
graph TD
    subgraph DataPipeline["Data Pipeline Workflow"]
        HAPI["HTTP API Input"] --> MB["Message Broker"]
        CSV["CSV Input"] --> MB

        MB --> RI["Raw Input"]

        RI --> V["Validation"]
        V --> VE["Validation Errors"]
        V --> VD["Validated Data"]
        V --> VO["Validation Output"]

        VD --> T["Transformation"]
        T --> TE["Transformation Errors"]
        T --> TD["Transformed Data"]
        T --> TO["Transformation Output"]

        TD --> E["Enrichment"]
        E --> EE["Enrichment Errors"]
        E --> PD["Processed Data"]
        E --> EO["Enrichment Output"]

        PD --> DBO["DB Output"]
        PD --> NO["Notification Output"]
    end
```

## Data Flow in Pipeline

This shows how data flows through the pipeline stages.

```mermaid
graph LR
    I["Inputs"] --> V["Validation"]
    V --> T["Transformation"]
    T --> E["Enrichment"]
    E --> O["Outputs"]
```
