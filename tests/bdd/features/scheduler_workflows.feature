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
      | config_file | multi_scheduler_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create data pipeline scheduler workflow
    When I create a workflow with:
      | name        | Data Pipeline Scheduler |
      | description | Scheduled data processing pipeline |
      | config_file | data_pipeline_scheduler.yaml |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create report generation scheduler workflow
    When I create a workflow with:
      | name        | Report Generation Scheduler |
      | description | Automated report generation |
      | config_file | report_generation_scheduler.yaml |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create maintenance scheduler workflow
    When I create a workflow with:
      | name        | System Maintenance Scheduler |
      | description | Automated system maintenance tasks |
      | config_file | maintenance_scheduler.yaml |
    Then the workflow should be created successfully
    And the workflow should execute on schedule

  Scenario: Create monitoring scheduler workflow
    When I create a workflow with:
      | name        | System Monitoring Scheduler |
      | description | Automated system monitoring and alerting |
      | config_file | monitoring_scheduler.yaml |
    Then the workflow should be created successfully
    And the workflow should execute on schedule