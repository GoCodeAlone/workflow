# Integration Plugins Design: Twilio, monday.com, turn.io

**Date**: 2026-03-11
**Status**: Approved

## Overview

Three new external gRPC plugins for the workflow engine, providing comprehensive API coverage for Twilio, monday.com, and turn.io. All are MIT-licensed, open-source, community-tier plugins following the established `workflow-plugin-*` pattern.

## Common Architecture

Each plugin follows the standard external plugin pattern:

```
workflow-plugin-<name>/
├── cmd/workflow-plugin-<name>/main.go    # sdk.Serve(provider)
├── internal/
│   ├── plugin.go                         # PluginProvider + ModuleProvider + StepProvider
│   ├── client.go                         # API client (SDK or direct HTTP)
│   ├── module.go                         # Module instance (credentials + client init)
│   ├── registry.go                       # Global provider registry
│   └── step_*.go                         # Step implementations grouped by product
├── plugin.json                           # Registry manifest
├── go.mod
├── .goreleaser.yml                       # v2, linux/darwin x amd64/arm64, CGO_ENABLED=0
├── .github/workflows/release.yml
├── Makefile
└── LICENSE (MIT)
```

**Module pattern**: One module type per plugin (e.g., `twilio.provider`) that initializes the API client from config. Steps resolve the provider by module name from a package-level registry map.

**Config pattern**:
```yaml
modules:
  - name: my-twilio
    type: twilio.provider
    config:
      accountSid: "${TWILIO_ACCOUNT_SID}"
      authToken: "${TWILIO_AUTH_TOKEN}"
```

**Build**: GoReleaser v2, `CGO_ENABLED=0`, linux/darwin x amd64/arm64. GitHub Release workflow on `v*` tags. Archives include `plugin.json` and `LICENSE`.

**Repos**: `GoCodeAlone/workflow-plugin-twilio`, `GoCodeAlone/workflow-plugin-monday`, `GoCodeAlone/workflow-plugin-turnio`

---

## Plugin 1: workflow-plugin-twilio

**Dependency**: `github.com/twilio/twilio-go` v1.30.3 (official, auto-generated from OpenAPI specs, MIT)

**Module**: `twilio.provider`
- Config: `accountSid`, `authToken` (or `apiKey` + `apiSecret`), optional `region`, `edge`
- Initializes `twilio.NewRestClientWithParams()`

### Step Types (~90, all prefixed `step.twilio_`)

| Product | Steps | Count |
|---------|-------|-------|
| Messaging | `send_sms`, `send_mms`, `send_whatsapp`, `list_messages`, `fetch_message`, `delete_message`, `fetch_media`, `create_messaging_service` | 8 |
| Voice | `create_call`, `fetch_call`, `list_calls`, `update_call`, `create_conference`, `list_conferences`, `add_participant`, `create_queue`, `fetch_recording`, `list_recordings`, `delete_recording` | 11 |
| Verify | `send_verification`, `check_verification`, `create_verify_service`, `list_verify_services` | 4 |
| Lookup | `lookup_phone` | 1 |
| Conversations | `create_conversation`, `send_conversation_message`, `add_conversation_participant`, `list_conversations`, `fetch_conversation`, `list_conversation_messages`, `create_conversation_user` | 7 |
| Video | `create_room`, `list_rooms`, `fetch_room`, `complete_room`, `list_room_recordings`, `create_composition` | 6 |
| Notify | `send_notification`, `create_binding`, `list_bindings`, `create_notify_service` | 4 |
| TaskRouter | `create_workspace`, `create_task`, `create_worker`, `create_task_queue`, `create_tr_workflow`, `list_tasks`, `update_task` | 7 |
| Phone Numbers | `search_available`, `buy_number`, `list_numbers`, `update_number`, `release_number` | 5 |
| Studio | `trigger_flow`, `list_flows`, `fetch_execution` | 3 |
| Serverless | `create_service`, `create_function`, `create_build`, `list_services` | 4 |
| Intelligence | `create_transcript`, `fetch_transcript`, `list_transcripts` | 3 |
| Flex | `create_flex_flow`, `create_web_channel`, `list_flex_flows` | 3 |
| Proxy | `create_proxy_service`, `create_session`, `add_proxy_participant` | 3 |
| Sync | `create_sync_service`, `create_document`, `update_document`, `create_sync_map`, `create_sync_list` | 5 |
| Wireless/SuperSIM | `list_sims`, `fetch_sim`, `update_sim`, `create_fleet`, `send_command` | 5 |
| Pricing/Usage | `fetch_pricing`, `list_usage_records` | 2 |
| Accounts/IAM | `list_accounts`, `create_api_key`, `list_api_keys` | 3 |
| Content | `create_content_template`, `list_content_templates`, `fetch_content_template` | 3 |
| TrustHub | `create_trust_product`, `list_trust_products`, `fetch_trust_product` | 3 |
| Assistants | `create_assistant`, `list_assistants`, `create_knowledge_base` | 3 |

---

## Plugin 2: workflow-plugin-monday

**Dependency**: None (direct GraphQL client via `net/http` + `encoding/json`)

**API**: `POST https://api.monday.com/v2` with `API-Version: 2026-04` header

**Module**: `monday.provider`
- Config: `apiToken`, optional `apiVersion` (default `2026-04`)
- Internal GraphQL client with complexity budget monitoring

### Step Types (~57, all prefixed `step.monday_`)

| Resource | Steps | Count |
|----------|-------|-------|
| Boards | `create_board`, `list_boards`, `fetch_board`, `update_board`, `delete_board`, `duplicate_board`, `archive_board` | 7 |
| Items | `create_item`, `list_items`, `fetch_item`, `update_item`, `move_item`, `archive_item`, `delete_item`, `search_items` | 8 |
| Subitems | `create_subitem`, `list_subitems`, `update_subitem`, `delete_subitem` | 4 |
| Columns | `get_column_values`, `change_column_value`, `create_column` | 3 |
| Groups | `create_group`, `list_groups`, `update_group`, `move_group`, `delete_group` | 5 |
| Workspaces | `create_workspace`, `list_workspaces`, `update_workspace`, `delete_workspace` | 4 |
| Folders | `create_folder`, `list_folders`, `update_folder`, `delete_folder` | 4 |
| Updates | `create_update`, `list_updates`, `edit_update`, `delete_update` | 4 |
| Users | `list_users`, `fetch_user`, `invite_user` | 3 |
| Teams | `list_teams`, `add_team_to_workspace` | 2 |
| Tags | `list_tags`, `create_tag` | 2 |
| Files | `upload_file`, `list_files` | 2 |
| Notifications | `create_notification` | 1 |
| Webhooks | `create_webhook`, `list_webhooks`, `delete_webhook` | 3 |
| Documents | `create_document`, `list_documents`, `update_document` | 3 |
| Generic | `query`, `mutate` | 2 |

---

## Plugin 3: workflow-plugin-turnio

**Dependency**: None (direct REST client via `net/http` + `encoding/json`)

**API**: `https://whatsapp.turn.io` with Bearer token auth

**Module**: `turnio.provider`
- Config: `apiToken`, optional `baseUrl` (default `https://whatsapp.turn.io`)
- Rate limit tracking via `X-Ratelimit-Remaining` response headers

### Step Types (~25, all prefixed `step.turnio_`)

| Resource | Steps | Count |
|----------|-------|-------|
| Messages | `send_text`, `send_media`, `send_template`, `send_interactive`, `send_location`, `list_messages` | 6 |
| Contacts | `check_contact`, `upload_contacts`, `update_profile` | 3 |
| Media | `upload_media`, `get_media`, `delete_media` | 3 |
| Templates | `create_template`, `list_templates`, `fetch_template`, `update_template`, `delete_template` | 5 |
| Webhooks | `configure_webhook` | 1 |
| Flows | `create_flow`, `list_flows`, `send_flow` | 3 |
| Journeys | `list_journeys`, `trigger_journey` | 2 |
| Context | `get_context`, `set_context` | 2 |

---

## Registry Manifests

Each plugin gets a `manifest.json` in `workflow-registry/plugins/<name>/`:

- **type**: `external`
- **tier**: `community`
- **license**: `MIT`
- **capabilities**: lists all `moduleTypes` and `stepTypes`
- **minEngineVersion**: `0.3.30`

---

## Testing Strategy

Each plugin includes unit tests with mock HTTP servers (no live API calls in CI):
- Mock the API client interface behind a provider abstraction
- Test step execute logic with canned responses
- Test error handling (rate limits, auth failures, invalid params)
- Test module init/start/stop lifecycle

---

## Implementation Order

All three plugins built in parallel using agent teams. Each plugin is independent with no cross-dependencies. Estimated scope:
- **Twilio**: ~90 steps, largest plugin, uses official SDK
- **monday.com**: ~57 steps, GraphQL client + typed mutations
- **turn.io**: ~25 steps, smallest, straightforward REST
