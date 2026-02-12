# Event-Driven Workflow Architecture

This diagram visualizes the complex event pattern processing system with its components.

## Event-Driven Workflow Engine

```mermaid
graph TD
    subgraph EventDrivenWorkflow["Event-Driven Workflow Engine"]
        HS["HTTP Server (:8080)"] --> EB["Event Broker (Message Queue)"]
        EB -->|Events| EP["Event Processor (Pattern Matching)"]

        EP --> P1["Pattern: Login Brute Force<br/>(3+ failed logins)"]
        EP --> P2["Pattern: Critical System Fault<br/>(Error Sequence)"]
        EP --> P3["Pattern: Data Breach<br/>(Access seq.)"]
        EP --> P4["Pattern: Purchase Opportunity<br/>(Cart abandon)"]

        P1 --> SAH["Security Alert Handler"]
        P2 --> SFH["System Fault Handler"]
        P3 --> SAH2["Security Alert Handler"]
        P4 --> BIH["Business Insight Handler"]
    end
```

## Event Pattern Detection Example

This visualizes how the login brute force pattern is detected over time.

| Time | Event | Buffer | Pattern Match |
|------|-------|--------|---------------|
| 00:00 | `user.login.failed` | [1] | No match (< 3) |
| 00:01 | `user.login.failed` | [1,2] | No match (< 3) |
| 00:03 | `user.login.failed` | [1,2,3] | MATCH! (>= 3) |
| 00:04 | `user.login.failed` | [1,2,3,4] | MATCH! (>= 3) |
| ... | | | |
| 00:06 | [5 min window start] | | |
| 00:06 | [event 1 expires] | [2,3,4] | MATCH! (>= 3) |
| 00:07 | [event 2 expires] | [3,4] | No match (< 3) |

## Complex Event Pattern - Critical System Fault

This shows detection of an ordered sequence of events.

```mermaid
graph LR
    DBE["DB Error<br/>t=0"] --> APIE["API Error<br/>t=30s"]
    APIE --> AE["Auth Error<br/>t=1m"]
    AE --> M["MATCH!"]
```

> Must occur within a 2-minute window.

## Complex Event Pattern - Data Breach

Shows detection of a potential data breach sequence.

```mermaid
graph LR
    PE["Permission Escalation<br/>t=0"] --> SDA["Sensitive Data Access<br/>t=3m"]
    SDA --> UL["Unusual Location<br/>t=5m"]
    UL --> M["MATCH!"]
```

> Must occur within a 10-minute window.
