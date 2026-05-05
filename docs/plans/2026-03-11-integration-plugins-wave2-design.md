---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow-plugin-okta
    commit: 0fb0455
  - repo: workflow-plugin-datadog
    commit: 8d50982
  - repo: workflow-plugin-launchdarkly
    commit: 530a0c7
  - repo: workflow-plugin-salesforce
    commit: 44bd98c
  - repo: workflow-plugin-openlms
    commit: 1070766
  - repo: workflow-plugin-authz
    commit: c50b786
external_refs:
  - "workflow-scenarios: scenarios/54-okta-integration through scenarios/58-openlms-integration"
verification:
  last_checked: 2026-04-25
  commands:
    - "jq -r '.name, (.capabilities.stepTypes // .stepTypes // [] | length)' /Users/jon/workspace/workflow-plugin-{okta,datadog,launchdarkly,salesforce,openlms}/plugin.json"
    - "rg -n \"permit|Permit\" /Users/jon/workspace/workflow-plugin-authz"
    - "rg -n \"scenarios/5[4-8]-.*integration\" /Users/jon/workspace/workflow-scenarios"
  result: pass
supersedes: []
superseded_by: []
---

# Integration Plugins Wave 2 Design: Okta, Datadog, LaunchDarkly, Permit.io, Salesforce, OpenLMS

**Date**: 2026-03-11
**Status**: Approved

## Overview

Five new external gRPC plugins for the workflow engine, plus Permit.io integrated as a new provider in the existing `workflow-plugin-authz`. Continues the integration plugin pattern from wave 1 (Twilio, monday.com, turn.io). All new repos are MIT-licensed, open-source, community-tier.

## Common Architecture

Identical to wave 1 — see `2026-03-11-integration-plugins-design.md`. Each **new** plugin:
- Standalone Go repo: `GoCodeAlone/workflow-plugin-<name>`
- `sdk.Serve(provider)` entry point
- PluginProvider + ModuleProvider + StepProvider interfaces
- One module type per plugin (`<name>.provider`)
- Package-level provider registry with `sync.RWMutex`
- GoReleaser v2, `CGO_ENABLED=0`, linux/darwin x amd64/arm64
- MIT license, community tier, minEngineVersion `0.3.30`

**Exception — Permit.io**: Added as a provider to the existing `workflow-plugin-authz` (alongside Casbin), following the multi-provider pattern used in `workflow-plugin-payments` (Stripe + PayPal). New module type: `permit.provider`. New step types prefixed `step.permit_`.

---

## Plugin 1: workflow-plugin-okta

**Dependency**: `github.com/okta/okta-sdk-golang/v6` v6.0.3 (official, auto-generated from OpenAPI spec)

**Module**: `okta.provider`
- Config: `orgUrl` (required, e.g. `https://dev-123456.okta.com`), `apiToken` (for SSWS auth) OR `clientId` + `privateKey` (for OAuth 2.0 JWT), optional `scopes`
- Initializes Okta SDK client

### Step Types (~130 priority steps, all prefixed `step.okta_`)

| Category | Steps | Count |
|----------|-------|-------|
| Users — CRUD | `user_create`, `user_get`, `user_list`, `user_update`, `user_delete` | 5 |
| Users — Lifecycle | `user_activate`, `user_deactivate`, `user_reactivate`, `user_suspend`, `user_unsuspend`, `user_unlock`, `user_reset_factors` | 7 |
| Users — Credentials | `user_change_password`, `user_reset_password`, `user_expire_password`, `user_set_recovery_question` | 4 |
| Groups — CRUD | `group_create`, `group_get`, `group_list`, `group_delete`, `group_add_user`, `group_remove_user`, `group_list_users` | 7 |
| Group Rules | `group_rule_create`, `group_rule_get`, `group_rule_list`, `group_rule_delete`, `group_rule_activate`, `group_rule_deactivate` | 6 |
| Applications — Core | `app_create`, `app_get`, `app_list`, `app_update`, `app_delete`, `app_activate`, `app_deactivate` | 7 |
| Applications — Users | `app_user_assign`, `app_user_get`, `app_user_list`, `app_user_update`, `app_user_unassign` | 5 |
| Applications — Groups | `app_group_assign`, `app_group_get`, `app_group_list`, `app_group_update`, `app_group_unassign` | 5 |
| Authorization Servers | `authz_server_create`, `authz_server_get`, `authz_server_list`, `authz_server_update`, `authz_server_delete`, `authz_server_activate`, `authz_server_deactivate` | 7 |
| Auth Server — Claims/Scopes/Policies | `authz_claim_create`, `authz_claim_list`, `authz_claim_delete`, `authz_scope_create`, `authz_scope_list`, `authz_scope_delete`, `authz_policy_create`, `authz_policy_list`, `authz_policy_delete`, `authz_policy_rule_create`, `authz_policy_rule_list`, `authz_policy_rule_delete`, `authz_key_list`, `authz_key_rotate` | 14 |
| Policies | `policy_create`, `policy_get`, `policy_list`, `policy_delete`, `policy_activate`, `policy_deactivate`, `policy_rule_create`, `policy_rule_list`, `policy_rule_delete`, `policy_rule_activate`, `policy_rule_deactivate` | 11 |
| Authenticators (MFA) | `authenticator_create`, `authenticator_get`, `authenticator_list`, `authenticator_activate`, `authenticator_deactivate` | 5 |
| User Factors | `factor_enroll`, `factor_list`, `factor_verify`, `factor_unenroll`, `factor_activate` | 5 |
| Identity Providers | `idp_create`, `idp_get`, `idp_list`, `idp_delete`, `idp_activate`, `idp_deactivate` | 6 |
| Sessions | `session_get`, `session_refresh`, `session_revoke` | 3 |
| Network Zones | `network_zone_create`, `network_zone_get`, `network_zone_list`, `network_zone_delete`, `network_zone_activate`, `network_zone_deactivate` | 6 |
| System Log | `log_list` | 1 |
| Event Hooks | `event_hook_create`, `event_hook_get`, `event_hook_list`, `event_hook_delete`, `event_hook_activate`, `event_hook_deactivate`, `event_hook_verify` | 7 |
| Inline Hooks | `inline_hook_create`, `inline_hook_get`, `inline_hook_list`, `inline_hook_delete`, `inline_hook_activate`, `inline_hook_deactivate`, `inline_hook_execute` | 7 |
| Domains | `domain_create`, `domain_get`, `domain_list`, `domain_delete`, `domain_verify` | 5 |
| Brands & Themes | `brand_get`, `brand_list`, `brand_update`, `theme_get`, `theme_list`, `theme_update` | 6 |
| Org Settings | `org_get`, `org_update` | 2 |

---

## Plugin 2: workflow-plugin-datadog

**Dependency**: `github.com/DataDog/datadog-api-client-go/v2` v2.56.0 (official, auto-generated)

**Module**: `datadog.provider`
- Config: `apiKey` (required), `appKey` (required), optional `site` (default `datadoghq.com`), optional `apiUrl`
- Sets up Datadog client context with API + app keys

### Step Types (~120 priority steps, all prefixed `step.datadog_`)

| Category | Steps | Count |
|----------|-------|-------|
| Metrics | `metric_submit`, `metric_query`, `metric_query_scalar`, `metric_metadata_get`, `metric_metadata_update`, `metric_list_active`, `metric_tag_config_create`, `metric_tag_config_update`, `metric_tag_config_delete`, `metric_tag_config_list` | 10 |
| Events | `event_create`, `event_get`, `event_list`, `event_search` | 4 |
| Monitors | `monitor_create`, `monitor_get`, `monitor_update`, `monitor_delete`, `monitor_list`, `monitor_search`, `monitor_validate` | 7 |
| Dashboards | `dashboard_create`, `dashboard_get`, `dashboard_update`, `dashboard_delete`, `dashboard_list` | 5 |
| Logs | `log_submit`, `log_search`, `log_aggregate`, `log_archive_create`, `log_archive_list`, `log_archive_delete`, `log_pipeline_create`, `log_pipeline_list`, `log_pipeline_delete` | 9 |
| Synthetics | `synthetics_test_create`, `synthetics_test_get`, `synthetics_test_update`, `synthetics_test_delete`, `synthetics_test_list`, `synthetics_test_trigger`, `synthetics_results_get`, `synthetics_global_var_create`, `synthetics_global_var_list`, `synthetics_global_var_delete` | 10 |
| SLOs | `slo_create`, `slo_get`, `slo_update`, `slo_delete`, `slo_list`, `slo_search`, `slo_history_get` | 7 |
| Downtimes | `downtime_create`, `downtime_get`, `downtime_update`, `downtime_cancel`, `downtime_list` | 5 |
| Incidents | `incident_create`, `incident_get`, `incident_update`, `incident_delete`, `incident_list`, `incident_todo_create`, `incident_todo_update`, `incident_todo_delete` | 8 |
| Security | `security_rule_create`, `security_rule_get`, `security_rule_update`, `security_rule_delete`, `security_rule_list`, `security_signal_list`, `security_signal_state_update` | 7 |
| Users | `user_create`, `user_get`, `user_update`, `user_disable`, `user_list`, `user_invite` | 6 |
| Roles | `role_create`, `role_get`, `role_update`, `role_delete`, `role_list`, `role_permission_add`, `role_permission_remove` | 7 |
| Teams | `team_create`, `team_get`, `team_update`, `team_delete`, `team_list`, `team_member_add`, `team_member_remove` | 7 |
| Key Management | `api_key_create`, `api_key_get`, `api_key_update`, `api_key_delete`, `api_key_list`, `app_key_create`, `app_key_list`, `app_key_delete` | 8 |
| Notebooks | `notebook_create`, `notebook_get`, `notebook_update`, `notebook_delete`, `notebook_list` | 5 |
| Hosts | `host_list`, `host_mute`, `host_unmute`, `host_totals_get` | 4 |
| Tags | `tags_get`, `tags_update`, `tags_delete`, `tags_list` | 4 |
| Service Catalog | `service_definition_upsert`, `service_definition_get`, `service_definition_delete`, `service_definition_list` | 4 |
| APM | `apm_retention_filter_create`, `apm_retention_filter_update`, `apm_retention_filter_delete`, `apm_retention_filter_list`, `span_search`, `span_aggregate` | 6 |
| Audit | `audit_log_search`, `audit_log_list` | 2 |

---

## Plugin 3: workflow-plugin-launchdarkly

**Dependency**: `github.com/launchdarkly/api-client-go/v22` (official, auto-generated from OpenAPI)

**Module**: `launchdarkly.provider`
- Config: `apiKey` (required), optional `apiUrl` (default `https://app.launchdarkly.com`)
- Uses context-based auth: `context.WithValue(ctx, ldapi.ContextAPIKey, ...)`

### Step Types (~100 priority steps, all prefixed `step.launchdarkly_`)

| Category | Steps | Count |
|----------|-------|-------|
| Feature Flags | `flag_list`, `flag_get`, `flag_create`, `flag_update`, `flag_delete`, `flag_copy`, `flag_status_get`, `flag_status_list` | 8 |
| Projects | `project_list`, `project_get`, `project_create`, `project_update`, `project_delete` | 5 |
| Environments | `environment_list`, `environment_get`, `environment_create`, `environment_update`, `environment_delete`, `environment_reset_sdk_key`, `environment_reset_mobile_key` | 7 |
| Segments | `segment_list`, `segment_get`, `segment_create`, `segment_update`, `segment_delete` | 5 |
| Contexts | `context_list`, `context_get`, `context_search`, `context_kind_list`, `context_kind_upsert`, `context_evaluate` | 6 |
| Metrics | `metric_list`, `metric_get`, `metric_create`, `metric_update`, `metric_delete` | 5 |
| Experiments | `experiment_list`, `experiment_get`, `experiment_create`, `experiment_update`, `experiment_results_get` | 5 |
| Approvals | `approval_list`, `approval_get`, `approval_create`, `approval_delete`, `approval_apply`, `approval_review` | 6 |
| Scheduled Changes | `scheduled_change_list`, `scheduled_change_create`, `scheduled_change_update`, `scheduled_change_delete` | 4 |
| Flag Triggers | `trigger_list`, `trigger_create`, `trigger_get`, `trigger_update`, `trigger_delete` | 5 |
| Workflows | `workflow_list`, `workflow_get`, `workflow_create`, `workflow_delete` | 4 |
| Audit Log | `audit_log_list`, `audit_log_get` | 2 |
| Members | `member_list`, `member_get`, `member_create`, `member_update`, `member_delete` | 5 |
| Teams | `team_list`, `team_get`, `team_create`, `team_update`, `team_delete` | 5 |
| Custom Roles | `role_list`, `role_get`, `role_create`, `role_update`, `role_delete` | 5 |
| Access Tokens | `token_list`, `token_get`, `token_create`, `token_update`, `token_delete`, `token_reset` | 6 |
| Webhooks | `webhook_list`, `webhook_get`, `webhook_create`, `webhook_update`, `webhook_delete` | 5 |
| Relay Proxy | `relay_config_list`, `relay_config_get`, `relay_config_create`, `relay_config_update`, `relay_config_delete` | 5 |
| Release Pipelines | `release_pipeline_list`, `release_pipeline_get`, `release_pipeline_create`, `release_pipeline_update`, `release_pipeline_delete` | 5 |
| Code References | `code_ref_repo_list`, `code_ref_repo_create`, `code_ref_repo_delete`, `code_ref_extinction_list` | 4 |

---

## Plugin 4: Permit.io provider in workflow-plugin-authz

**Repo**: `GoCodeAlone/workflow-plugin-authz` (existing — add Permit.io as a new provider alongside Casbin)
**New Dependency**: `github.com/permitio/permit-golang` v1.2.8 (official Go SDK)

**Module**: `permit.provider` (new module type in the authz plugin)
- Config: `apiKey` (required), optional `pdpUrl` (default `https://cloudpdp.api.permit.io`), optional `apiUrl` (default `https://api.permit.io`), optional `project`, `environment`
- Initializes Permit SDK client
- Coexists with the existing `authz.provider` (Casbin) — both can be configured simultaneously

### Step Types (~80 steps, all prefixed `step.permit_`)

| Category | Steps | Count |
|----------|-------|-------|
| Authorization Checks | `check`, `check_bulk`, `user_permissions`, `authorized_users` | 4 |
| Users | `user_create`, `user_get`, `user_list`, `user_update`, `user_delete`, `user_sync`, `user_get_roles` | 7 |
| Tenants | `tenant_create`, `tenant_get`, `tenant_list`, `tenant_update`, `tenant_delete`, `tenant_list_users` | 6 |
| Roles (RBAC) | `role_create`, `role_get`, `role_list`, `role_update`, `role_delete`, `role_assign_permissions`, `role_remove_permissions` | 7 |
| Role Assignments | `role_assign`, `role_unassign`, `role_assignment_list`, `role_bulk_assign`, `role_bulk_unassign` | 5 |
| Resources | `resource_create`, `resource_get`, `resource_list`, `resource_update`, `resource_delete` | 5 |
| Resource Actions | `resource_action_create`, `resource_action_get`, `resource_action_list`, `resource_action_update`, `resource_action_delete` | 5 |
| Resource Roles | `resource_role_create`, `resource_role_get`, `resource_role_list`, `resource_role_update`, `resource_role_delete` | 5 |
| Resource Relations (ReBAC) | `resource_relation_create`, `resource_relation_list`, `resource_relation_delete` | 3 |
| Resource Instances (ReBAC) | `resource_instance_create`, `resource_instance_get`, `resource_instance_list`, `resource_instance_update`, `resource_instance_delete` | 5 |
| Relationship Tuples | `relationship_tuple_create`, `relationship_tuple_delete`, `relationship_tuple_list`, `relationship_tuple_bulk_create`, `relationship_tuple_bulk_delete` | 5 |
| Condition Sets (ABAC) | `condition_set_create`, `condition_set_get`, `condition_set_list`, `condition_set_update`, `condition_set_delete` | 5 |
| Projects | `project_create`, `project_get`, `project_list`, `project_update`, `project_delete` | 5 |
| Environments | `env_create`, `env_get`, `env_list`, `env_update`, `env_delete`, `env_copy` | 6 |
| API Keys | `api_key_create`, `api_key_list`, `api_key_delete`, `api_key_rotate` | 4 |
| Organizations | `org_get`, `org_update`, `member_list`, `member_invite`, `member_remove` | 5 |

---

## Plugin 5: workflow-plugin-salesforce

**Dependency**: `github.com/k-capehart/go-salesforce/v3` v3.1.1 (community, well-maintained)

**Module**: `salesforce.provider`
- Config: `loginUrl` (required), `clientId`, `clientSecret` (for OAuth client credentials), OR `accessToken` (direct), optional `apiVersion` (default `v63.0`)
- Initializes Salesforce client with OAuth

### Step Types (~75 steps, all prefixed `step.salesforce_`)

| Category | Steps | Count |
|----------|-------|-------|
| SObject CRUD | `record_get`, `record_create`, `record_update`, `record_upsert`, `record_delete`, `record_describe`, `describe_global` | 7 |
| SOQL Query | `query`, `query_all` | 2 |
| SOSL Search | `search` | 1 |
| Collections | `collection_insert`, `collection_update`, `collection_upsert`, `collection_delete` | 4 |
| Composite | `composite_request`, `composite_tree` | 2 |
| Bulk API v2 | `bulk_insert`, `bulk_update`, `bulk_upsert`, `bulk_delete`, `bulk_query`, `bulk_query_results`, `bulk_job_status`, `bulk_job_abort` | 8 |
| Tooling | `tooling_query`, `tooling_get`, `tooling_create`, `tooling_update`, `tooling_delete`, `apex_execute` | 6 |
| Apex REST | `apex_get`, `apex_post`, `apex_patch`, `apex_put`, `apex_delete` | 5 |
| Reports | `report_list`, `report_describe`, `report_run`, `dashboard_list`, `dashboard_describe`, `dashboard_refresh` | 6 |
| Approval | `approval_list`, `approval_submit`, `approval_approve`, `approval_reject` | 4 |
| Chatter | `chatter_post`, `chatter_comment`, `chatter_like`, `chatter_feed_list` | 4 |
| Files | `file_upload`, `file_download`, `content_version_create`, `content_document_get`, `content_document_delete` | 5 |
| Users | `user_get`, `user_list`, `user_create`, `user_update`, `identity_get`, `org_limits` | 6 |
| Flows | `flow_list`, `flow_run` | 2 |
| Events | `event_publish` | 1 |
| Metadata | `metadata_describe`, `metadata_list`, `metadata_read`, `metadata_create`, `metadata_update`, `metadata_delete`, `metadata_deploy`, `metadata_retrieve` | 8 |
| Generic | `raw_request` | 1 |

---

## Plugin 6: workflow-plugin-openlms

**Dependency**: Direct REST client (Moodle Web Services API, form-POST or Catalyst RESTful plugin)

**Module**: `openlms.provider`
- Config: `siteUrl` (required, e.g. `https://lms.example.com`), `token` (required, Web Services token), optional `restful` (bool, default false — use Catalyst RESTful plugin endpoint)
- Initializes HTTP client with token auth

### Step Types (~120 priority steps, all prefixed `step.openlms_`)

| Category | Steps | Count |
|----------|-------|-------|
| Users | `user_create`, `user_update`, `user_delete`, `user_get`, `user_get_by_field`, `user_search` | 6 |
| Courses | `course_create`, `course_update`, `course_delete`, `course_get`, `course_get_by_field`, `course_search`, `course_get_contents`, `course_get_categories`, `course_create_categories`, `course_delete_categories`, `course_duplicate` | 11 |
| Enrollments | `enrol_get_enrolled_users`, `enrol_get_user_courses`, `enrol_manual_enrol`, `enrol_manual_unenrol`, `enrol_self_enrol`, `enrol_get_course_methods` | 6 |
| Grades | `grade_get_grades`, `grade_update_grades`, `grade_get_grade_items`, `grade_get_grades_table` | 4 |
| Assignments | `assign_get_assignments`, `assign_get_submissions`, `assign_get_grades`, `assign_save_submission`, `assign_submit_for_grading`, `assign_save_grade` | 6 |
| Quizzes | `quiz_get_by_course`, `quiz_get_attempts`, `quiz_get_attempt_data`, `quiz_get_attempt_review`, `quiz_start_attempt`, `quiz_save_attempt`, `quiz_process_attempt` | 7 |
| Forums | `forum_get_by_course`, `forum_get_discussions`, `forum_get_posts`, `forum_add_discussion`, `forum_add_post`, `forum_delete_post` | 6 |
| Groups | `group_create`, `group_delete`, `group_get_course_groups`, `group_get_members`, `group_add_members`, `group_delete_members` | 6 |
| Messages | `message_send`, `message_get_messages`, `message_get_conversations`, `message_get_unread_count`, `message_mark_read`, `message_block_user`, `message_unblock_user` | 7 |
| Calendar | `calendar_create_events`, `calendar_delete_events`, `calendar_get_events`, `calendar_get_day_view`, `calendar_get_monthly_view` | 5 |
| Competencies | `competency_create`, `competency_list`, `competency_delete`, `competency_create_framework`, `competency_list_frameworks`, `competency_create_plan`, `competency_list_plans`, `competency_add_to_course`, `competency_grade` | 9 |
| Completion | `completion_get_activities_status`, `completion_get_course_status`, `completion_update_activity`, `completion_mark_self_completed` | 4 |
| Files | `file_get_files`, `file_upload` | 2 |
| Badges | `badge_get_user_badges` | 1 |
| Cohorts | `cohort_create`, `cohort_delete`, `cohort_get`, `cohort_search`, `cohort_add_members`, `cohort_delete_members` | 6 |
| Roles | `role_assign`, `role_unassign` | 2 |
| Notes | `note_create`, `note_get`, `note_delete` | 3 |
| SCORM | `scorm_get_by_course`, `scorm_get_attempt_count`, `scorm_get_scos`, `scorm_get_user_data`, `scorm_insert_tracks`, `scorm_launch_sco` | 6 |
| H5P | `h5p_get_by_course`, `h5p_get_attempts`, `h5p_get_results` | 3 |
| Reports | `reportbuilder_list`, `reportbuilder_get`, `reportbuilder_retrieve` | 3 |
| Site Info | `site_get_info`, `webservice_get_site_info` | 2 |
| Lessons | `lesson_get_by_course`, `lesson_get_pages`, `lesson_get_page_data`, `lesson_launch_attempt`, `lesson_process_page`, `lesson_finish_attempt` | 6 |
| Glossary | `glossary_get_by_course`, `glossary_get_entries`, `glossary_add_entry`, `glossary_delete_entry` | 4 |
| Search | `search_get_results` | 1 |
| Tags | `tag_get_tags`, `tag_update` | 2 |
| LTI | `lti_get_by_course`, `lti_get_tool_launch_data`, `lti_get_tool_types` | 3 |
| xAPI | `xapi_statement_post`, `xapi_get_state`, `xapi_post_state` | 3 |
| Generic | `call_function` | 1 |

---

## Registry Manifests

Each plugin gets a `manifest.json` in `workflow-registry/plugins/<name>/`:
- **type**: `external`
- **tier**: `community`
- **license**: `MIT`
- **minEngineVersion**: `0.3.30`

## Testing Strategy

Same as wave 1: unit tests with mock HTTP servers, no live API calls in CI. Test validation, error handling, module lifecycle (Init/Stop).

## Implementation Order

All six plugins built in parallel using agent teams. Each plugin is independent. Estimated scope:
- **Okta**: ~130 steps, official SDK
- **Datadog**: ~120 steps, official SDK
- **LaunchDarkly**: ~100 steps, official SDK
- **Permit.io**: ~80 steps, official SDK
- **Salesforce**: ~75 steps, community SDK + direct REST
- **OpenLMS**: ~120 steps, direct REST client (Moodle Web Services)

**New Repos**: `GoCodeAlone/workflow-plugin-okta`, `GoCodeAlone/workflow-plugin-datadog`, `GoCodeAlone/workflow-plugin-launchdarkly`, `GoCodeAlone/workflow-plugin-salesforce`, `GoCodeAlone/workflow-plugin-openlms`
**Existing Repo (extended)**: `GoCodeAlone/workflow-plugin-authz` (Permit.io provider added)
