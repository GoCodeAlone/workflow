# Scheduled Jobs Architecture

This diagram visualizes the scheduled jobs workflow with different schedulers.

## Scheduled Jobs Workflow

```mermaid
graph TD
    subgraph ScheduledJobsWorkflow["Scheduled Jobs Workflow"]
        DS["Daily Scheduler<br/>(0 0 * * *)"]
        HS["Hourly Scheduler<br/>(0 * * * *)"]
        MS["Minute Scheduler<br/>(* * * * *)"]

        DS --> RGJ["Report Generation Job"]
        HS --> DCJ["Data Cleanup Job"]
        MS --> HCJ["Health Check Job"]
        MS --> MCJ["Metrics Collection Job"]

        RGJ --> MB["Message Broker"]
        DCJ --> MB
        HCJ --> MB
        MCJ --> MB

        MB --> JSS["Job Status Store"]

        subgraph Monitoring["Monitoring Interface"]
            HTTP["HTTP Server (:8086)"] --> JSAPI["Job Status API"]
        end

        JSS --> Monitoring
    end
```

## Job Schedule Timeline

This shows when each job runs over a 24-hour period.

```mermaid
gantt
    title Job Schedule Timeline (24-hour period)
    dateFormat HH:mm
    axisFormat %H:%M

    section Daily
    Report Generation       :d1, 00:00, 1h

    section Hourly
    Data Cleanup (repeats every hour) :h1, 00:00, 24h

    section Minutely
    Health Check + Metrics (every minute) :m1, 00:00, 24h
```

| Schedule | Jobs | Cron Expression |
|----------|------|-----------------|
| **Daily** | Report Generation | `0 0 * * *` (midnight) |
| **Hourly** | Data Cleanup | `0 * * * *` (top of each hour) |
| **Minutely** | Health Check, Metrics Collection | `* * * * *` (every minute) |
