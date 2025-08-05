Feature: Scheduler Workflow Testing
  As a workflow developer
  I want to create and test scheduler-based workflows
  So that I can build time-driven and cron-based automation systems

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create a basic daily scheduler workflow
    When I create a scheduler workflow with:
      | cronExpression | 0 0 * * * |
      | jobName        | daily-backup |
    Then the workflow should be created successfully
    And the workflow should use "scheduler" module
    And the workflow should execute on schedule

  Scenario: Create hourly data processing workflow
    When I create a scheduler workflow with:
      | cronExpression | 0 * * * * |
      | jobName        | hourly-processor |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create multiple scheduled jobs workflow
    When I create a workflow with:
      | name        | Multi-Schedule Workflow |
      | description | Multiple scheduled jobs in one workflow |
      | config      | modules:\n  - name: daily-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 0 * * *"\n  - name: hourly-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 * * * *"\n  - name: minute-scheduler\n    type: scheduler\n    config:\n      cronExpression: "* * * * *"\n  - name: backup-job\n    type: messaging.handler\n  - name: cleanup-job\n    type: messaging.handler\n  - name: health-check-job\n    type: messaging.handler\n  - name: message-broker\n    type: messaging.broker\n\nworkflows:\n  scheduler:\n    jobs:\n      - scheduler: daily-scheduler\n        job: backup-job\n      - scheduler: hourly-scheduler\n        job: cleanup-job\n      - scheduler: minute-scheduler\n        job: health-check-job |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create data pipeline scheduler workflow
    When I create a workflow with:
      | name        | Data Pipeline Scheduler |
      | description | Scheduled data processing pipeline |
      | config      | modules:\n  - name: pipeline-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 2 * * *"\n  - name: data-extractor\n    type: messaging.handler\n  - name: data-transformer\n    type: messaging.handler\n  - name: data-loader\n    type: messaging.handler\n  - name: pipeline-broker\n    type: messaging.broker\n\nworkflows:\n  scheduler:\n    jobs:\n      - scheduler: pipeline-scheduler\n        job: data-extractor\n  messaging:\n    subscriptions:\n      - topic: extracted-data\n        handler: data-transformer\n      - topic: transformed-data\n        handler: data-loader\n    producers:\n      - name: data-extractor\n        forwardTo:\n          - extracted-data\n      - name: data-transformer\n        forwardTo:\n          - transformed-data |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create report generation scheduler workflow
    When I create a workflow with:
      | name        | Report Generation Scheduler |
      | description | Automated report generation |
      | config      | modules:\n  - name: daily-reports\n    type: scheduler\n    config:\n      cronExpression: "0 6 * * *"\n  - name: weekly-reports\n    type: scheduler\n    config:\n      cronExpression: "0 6 * * 1"\n  - name: monthly-reports\n    type: scheduler\n    config:\n      cronExpression: "0 6 1 * *"\n  - name: daily-report-job\n    type: messaging.handler\n  - name: weekly-report-job\n    type: messaging.handler\n  - name: monthly-report-job\n    type: messaging.handler\n  - name: report-broker\n    type: messaging.broker\n\nworkflows:\n  scheduler:\n    jobs:\n      - scheduler: daily-reports\n        job: daily-report-job\n      - scheduler: weekly-reports\n        job: weekly-report-job\n      - scheduler: monthly-reports\n        job: monthly-report-job |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create maintenance scheduler workflow
    When I create a workflow with:
      | name        | System Maintenance Scheduler |
      | description | Automated system maintenance tasks |
      | config      | modules:\n  - name: log-cleanup-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 1 * * 0"\n  - name: cache-cleanup-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 2 * * *"\n  - name: db-maintenance-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 3 * * 0"\n  - name: log-cleanup-job\n    type: messaging.handler\n  - name: cache-cleanup-job\n    type: messaging.handler\n  - name: db-maintenance-job\n    type: messaging.handler\n  - name: maintenance-broker\n    type: messaging.broker\n\nworkflows:\n  scheduler:\n    jobs:\n      - scheduler: log-cleanup-scheduler\n        job: log-cleanup-job\n      - scheduler: cache-cleanup-scheduler\n        job: cache-cleanup-job\n      - scheduler: db-maintenance-scheduler\n        job: db-maintenance-job |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create monitoring scheduler workflow
    When I create a workflow with:
      | name        | System Monitoring Scheduler |
      | description | Automated system monitoring and alerting |
      | config      | modules:\n  - name: health-check-scheduler\n    type: scheduler\n    config:\n      cronExpression: "*/5 * * * *"\n  - name: metrics-scheduler\n    type: scheduler\n    config:\n      cronExpression: "* * * * *"\n  - name: alert-scheduler\n    type: scheduler\n    config:\n      cronExpression: "*/10 * * * *"\n  - name: health-check-job\n    type: messaging.handler\n  - name: metrics-collection-job\n    type: messaging.handler\n  - name: alert-check-job\n    type: messaging.handler\n  - name: monitoring-broker\n    type: messaging.broker\n\nworkflows:\n  scheduler:\n    jobs:\n      - scheduler: health-check-scheduler\n        job: health-check-job\n      - scheduler: metrics-scheduler\n        job: metrics-collection-job\n      - scheduler: alert-scheduler\n        job: alert-check-job |
    Then the workflow should be created successfully
    And the workflow should execute on schedule