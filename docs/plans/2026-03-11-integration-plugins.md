---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow-plugin-twilio
    commit: 1345186
  - repo: workflow-plugin-monday
    commit: 94ccd28
  - repo: workflow-plugin-turnio
    commit: 670fdbf
external_refs:
  - "workflow-scenarios: scenarios/52-monday-integration"
  - "workflow-scenarios: scenarios/53-turnio-integration"
  - "workflow-scenarios: scenarios/63-twilio-integration"
verification:
  last_checked: 2026-04-25
  commands:
    - "find /Users/jon/workspace/workflow-plugin-{twilio,monday,turnio} -maxdepth 2 -name plugin.json -o -name go.mod -o -name .goreleaser.yml"
    - "git -C /Users/jon/workspace/workflow-plugin-twilio tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-monday tag --list 'v*'"
    - "git -C /Users/jon/workspace/workflow-plugin-turnio tag --list 'v*'"
  result: pass
supersedes: []
superseded_by: []
---

# Integration Plugins Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build three external gRPC plugins for the workflow engine: Twilio (~90 steps), monday.com (~57 steps), turn.io (~25 steps).

**Architecture:** Each plugin is a standalone Go repo following the `workflow-plugin-payments` pattern — `sdk.Serve()` entry point, `PluginProvider` + `ModuleProvider` + `StepProvider` interfaces, one module type per plugin, global provider registry, mock-based unit tests. No live API calls in tests.

**Tech Stack:** Go 1.26, `github.com/GoCodeAlone/workflow` SDK, `github.com/twilio/twilio-go` v1.30.3 (Twilio only), GoReleaser v2, GitHub Actions.

**Parallelism:** Tasks 1-3 (one per plugin) are fully independent and should be executed in parallel via agent teams. Task 4 (registry manifests) depends on all three completing.

---

## Task 1: workflow-plugin-twilio

**Repo:** `GoCodeAlone/workflow-plugin-twilio` (create new)
**License:** MIT
**Dependency:** `github.com/twilio/twilio-go` v1.30.3

### Step 1: Create GitHub repo and scaffold project

```bash
cd /Users/jon/workspace
mkdir workflow-plugin-twilio && cd workflow-plugin-twilio
git init
```

Create these files:

**`cmd/workflow-plugin-twilio/main.go`:**
```go
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-twilio/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.Serve(internal.NewTwilioPlugin())
}
```

**`go.mod`:**
```
module github.com/GoCodeAlone/workflow-plugin-twilio

go 1.26

require (
	github.com/GoCodeAlone/workflow v0.3.32
	github.com/twilio/twilio-go v1.30.3
)
```

Run `go mod tidy` to resolve all transitive dependencies.

**`LICENSE`:** MIT license, copyright GoCodeAlone.

**`Makefile`:** Copy from `workflow-plugin-payments/Makefile`, replace `BINARY_NAME = workflow-plugin-twilio`.

**`.goreleaser.yml`:** Copy from `workflow-plugin-payments/.goreleaser.yml`, replace all `payments` references with `twilio`.

**`.github/workflows/release.yml`:** Copy from `workflow-plugin-payments/.github/workflows/release.yml` verbatim (it's generic).

### Step 2: Create internal helpers and registry

**`internal/helpers.go`:** Copy verbatim from `/Users/jon/workspace/workflow-plugin-payments/internal/helpers.go`. Change the default module name from `"payments"` to `"twilio"` in `getModuleName()`. Also add these additional helpers:

```go
// resolveStringSlice looks up key as []string or []any in current then config.
func resolveStringSlice(key string, current, config map[string]any) []string {
	for _, m := range []map[string]any{current, config} {
		if m == nil {
			continue
		}
		switch v := m[key].(type) {
		case []string:
			return v
		case []any:
			var result []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	return nil
}

// resolveMap looks up key as map[string]any in current then config.
func resolveMap(key string, current, config map[string]any) map[string]any {
	for _, m := range []map[string]any{current, config} {
		if m == nil {
			continue
		}
		if v, ok := m[key].(map[string]any); ok {
			return v
		}
	}
	return nil
}

// resolveBool looks up key as bool in current then config.
func resolveBool(key string, current, config map[string]any) bool {
	for _, m := range []map[string]any{current, config} {
		if m == nil {
			continue
		}
		if v, ok := m[key].(bool); ok {
			return v
		}
	}
	return false
}

// resolveInt looks up key as int in current then config.
func resolveInt(key string, current, config map[string]any) int {
	return int(resolveInt64(key, current, config))
}
```

**`internal/registry.go`:**
```go
package internal

import (
	"sync"

	twilio "github.com/twilio/twilio-go"
)

var (
	clientMu       sync.RWMutex
	clientRegistry = make(map[string]*twilio.RestClient)
)

func RegisterClient(name string, c *twilio.RestClient) {
	clientMu.Lock()
	defer clientMu.Unlock()
	clientRegistry[name] = c
}

func GetClient(name string) (*twilio.RestClient, bool) {
	clientMu.RLock()
	defer clientMu.RUnlock()
	c, ok := clientRegistry[name]
	return c, ok
}

func UnregisterClient(name string) {
	clientMu.Lock()
	defer clientMu.Unlock()
	delete(clientRegistry, name)
}
```

### Step 3: Create module provider

**`internal/module.go`:**
```go
package internal

import (
	"context"
	"fmt"

	twilio "github.com/twilio/twilio-go"
)

type twilioModule struct {
	name   string
	config map[string]any
}

func newTwilioModule(name string, config map[string]any) (*twilioModule, error) {
	sid, _ := config["accountSid"].(string)
	token, _ := config["authToken"].(string)
	apiKey, _ := config["apiKey"].(string)
	apiSecret, _ := config["apiSecret"].(string)

	if sid == "" {
		return nil, fmt.Errorf("twilio.provider %q: accountSid is required", name)
	}
	if token == "" && (apiKey == "" || apiSecret == "") {
		return nil, fmt.Errorf("twilio.provider %q: authToken or apiKey+apiSecret required", name)
	}

	return &twilioModule{name: name, config: config}, nil
}

func (m *twilioModule) Init() error {
	sid, _ := m.config["accountSid"].(string)
	token, _ := m.config["authToken"].(string)
	apiKey, _ := m.config["apiKey"].(string)
	apiSecret, _ := m.config["apiSecret"].(string)

	params := twilio.ClientParams{AccountSid: sid}
	if apiKey != "" && apiSecret != "" {
		params.Username = apiKey
		params.Password = apiSecret
	} else {
		params.Username = sid
		params.Password = token
	}
	if region, ok := m.config["region"].(string); ok {
		params.Region = region
	}
	if edge, ok := m.config["edge"].(string); ok {
		params.Edge = edge
	}

	client := twilio.NewRestClientWithParams(params)
	RegisterClient(m.name, client)
	return nil
}

func (m *twilioModule) Start(_ context.Context) error { return nil }
func (m *twilioModule) Stop(_ context.Context) error {
	UnregisterClient(m.name)
	return nil
}
func (m *twilioModule) Name() string { return m.name }
```

### Step 4: Create plugin.go with all step type registrations

**`internal/plugin.go`:**
```go
package internal

import (
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type twilioPlugin struct{}

func NewTwilioPlugin() sdk.PluginProvider {
	return &twilioPlugin{}
}

func (p *twilioPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "workflow-plugin-twilio",
		Version:     "0.1.0",
		Author:      "GoCodeAlone",
		Description: "Comprehensive Twilio integration — SMS, Voice, Verify, Video, Conversations, and 40+ products",
	}
}

func (p *twilioPlugin) ModuleTypes() []string {
	return []string{"twilio.provider"}
}

func (p *twilioPlugin) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
	if typeName == "twilio.provider" {
		return newTwilioModule(name, config)
	}
	return nil, fmt.Errorf("twilio plugin: unknown module type %q", typeName)
}

func (p *twilioPlugin) StepTypes() []string {
	return allStepTypes()
}

func (p *twilioPlugin) CreateStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
	return createStep(typeName, name, config)
}
```

**`internal/step_registry.go`:** A central dispatch file that returns all step type names and dispatches `CreateStep` calls. This avoids a massive switch in plugin.go. Pattern:

```go
package internal

import (
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type stepConstructor func(name string, config map[string]any) (sdk.StepInstance, error)

var stepFactories = map[string]stepConstructor{
	// Messaging
	"step.twilio_send_sms":       newSendSMSStep,
	"step.twilio_send_mms":       newSendMMSStep,
	"step.twilio_send_whatsapp":  newSendWhatsAppStep,
	"step.twilio_list_messages":  newListMessagesStep,
	"step.twilio_fetch_message":  newFetchMessageStep,
	"step.twilio_delete_message": newDeleteMessageStep,
	"step.twilio_fetch_media":    newFetchMediaStep,
	"step.twilio_create_messaging_service": newCreateMessagingServiceStep,
	// Voice
	"step.twilio_create_call":       newCreateCallStep,
	"step.twilio_fetch_call":        newFetchCallStep,
	"step.twilio_list_calls":        newListCallsStep,
	"step.twilio_update_call":       newUpdateCallStep,
	"step.twilio_create_conference": newCreateConferenceStep,
	"step.twilio_list_conferences":  newListConferencesStep,
	"step.twilio_add_participant":   newAddParticipantStep,
	"step.twilio_create_queue":      newCreateQueueStep,
	"step.twilio_fetch_recording":   newFetchRecordingStep,
	"step.twilio_list_recordings":   newListRecordingsStep,
	"step.twilio_delete_recording":  newDeleteRecordingStep,
	// Verify
	"step.twilio_send_verification":    newSendVerificationStep,
	"step.twilio_check_verification":   newCheckVerificationStep,
	"step.twilio_create_verify_service": newCreateVerifyServiceStep,
	"step.twilio_list_verify_services": newListVerifyServicesStep,
	// Lookup
	"step.twilio_lookup_phone": newLookupPhoneStep,
	// Conversations
	"step.twilio_create_conversation":         newCreateConversationStep,
	"step.twilio_send_conversation_message":   newSendConversationMessageStep,
	"step.twilio_add_conversation_participant": newAddConversationParticipantStep,
	"step.twilio_list_conversations":          newListConversationsStep,
	"step.twilio_fetch_conversation":          newFetchConversationStep,
	"step.twilio_list_conversation_messages":  newListConversationMessagesStep,
	"step.twilio_create_conversation_user":    newCreateConversationUserStep,
	// Video
	"step.twilio_create_room":           newCreateRoomStep,
	"step.twilio_list_rooms":            newListRoomsStep,
	"step.twilio_fetch_room":            newFetchRoomStep,
	"step.twilio_complete_room":         newCompleteRoomStep,
	"step.twilio_list_room_recordings":  newListRoomRecordingsStep,
	"step.twilio_create_composition":    newCreateCompositionStep,
	// Notify
	"step.twilio_send_notification":   newSendNotificationStep,
	"step.twilio_create_binding":      newCreateBindingStep,
	"step.twilio_list_bindings":       newListBindingsStep,
	"step.twilio_create_notify_service": newCreateNotifyServiceStep,
	// TaskRouter
	"step.twilio_create_workspace":  newCreateWorkspaceStep,
	"step.twilio_create_task":       newCreateTaskStep,
	"step.twilio_create_worker":     newCreateWorkerStep,
	"step.twilio_create_task_queue": newCreateTaskQueueStep,
	"step.twilio_create_tr_workflow": newCreateTRWorkflowStep,
	"step.twilio_list_tasks":        newListTasksStep,
	"step.twilio_update_task":       newUpdateTaskStep,
	// Phone Numbers
	"step.twilio_search_available": newSearchAvailableStep,
	"step.twilio_buy_number":       newBuyNumberStep,
	"step.twilio_list_numbers":     newListNumbersStep,
	"step.twilio_update_number":    newUpdateNumberStep,
	"step.twilio_release_number":   newReleaseNumberStep,
	// Studio
	"step.twilio_trigger_flow":    newTriggerFlowStep,
	"step.twilio_list_flows":      newListFlowsStep,
	"step.twilio_fetch_execution": newFetchExecutionStep,
	// Serverless
	"step.twilio_create_serverless_service":  newCreateServerlessServiceStep,
	"step.twilio_create_function":            newCreateFunctionStep,
	"step.twilio_create_build":              newCreateBuildStep,
	"step.twilio_list_serverless_services":  newListServerlessServicesStep,
	// Intelligence
	"step.twilio_create_transcript": newCreateTranscriptStep,
	"step.twilio_fetch_transcript":  newFetchTranscriptStep,
	"step.twilio_list_transcripts":  newListTranscriptsStep,
	// Flex
	"step.twilio_create_flex_flow":   newCreateFlexFlowStep,
	"step.twilio_create_web_channel": newCreateWebChannelStep,
	"step.twilio_list_flex_flows":    newListFlexFlowsStep,
	// Proxy
	"step.twilio_create_proxy_service":    newCreateProxyServiceStep,
	"step.twilio_create_session":          newCreateSessionStep,
	"step.twilio_add_proxy_participant":   newAddProxyParticipantStep,
	// Sync
	"step.twilio_create_sync_service": newCreateSyncServiceStep,
	"step.twilio_create_document":     newCreateDocumentStep,
	"step.twilio_update_document":     newUpdateDocumentStep,
	"step.twilio_create_sync_map":     newCreateSyncMapStep,
	"step.twilio_create_sync_list":    newCreateSyncListStep,
	// Wireless / SuperSIM
	"step.twilio_list_sims":     newListSimsStep,
	"step.twilio_fetch_sim":     newFetchSimStep,
	"step.twilio_update_sim":    newUpdateSimStep,
	"step.twilio_create_fleet":  newCreateFleetStep,
	"step.twilio_send_command":  newSendCommandStep,
	// Pricing / Usage
	"step.twilio_fetch_pricing":      newFetchPricingStep,
	"step.twilio_list_usage_records": newListUsageRecordsStep,
	// Accounts / IAM
	"step.twilio_list_accounts":  newListAccountsStep,
	"step.twilio_create_api_key": newCreateAPIKeyStep,
	"step.twilio_list_api_keys":  newListAPIKeysStep,
	// Content
	"step.twilio_create_content_template": newCreateContentTemplateStep,
	"step.twilio_list_content_templates":  newListContentTemplatesStep,
	"step.twilio_fetch_content_template":  newFetchContentTemplateStep,
	// TrustHub
	"step.twilio_create_trust_product": newCreateTrustProductStep,
	"step.twilio_list_trust_products":  newListTrustProductsStep,
	"step.twilio_fetch_trust_product":  newFetchTrustProductStep,
	// Assistants
	"step.twilio_create_assistant":      newCreateAssistantStep,
	"step.twilio_list_assistants":       newListAssistantsStep,
	"step.twilio_create_knowledge_base": newCreateKnowledgeBaseStep,
}

func allStepTypes() []string {
	types := make([]string, 0, len(stepFactories))
	for t := range stepFactories {
		types = append(types, t)
	}
	return types
}

func createStep(typeName, name string, config map[string]any) (sdk.StepInstance, error) {
	ctor, ok := stepFactories[typeName]
	if !ok {
		return nil, fmt.Errorf("twilio plugin: unknown step type %q", typeName)
	}
	return ctor(name, config)
}
```

### Step 5: Implement step files by product group

Create one file per product group. Each step follows this pattern:
1. Struct with `name` and `moduleName` fields
2. Constructor that calls `getModuleName(config)`
3. `Execute` method that resolves params from `current`/`config`, calls the Twilio SDK, returns output map

**File organization** (one file per product group):

| File | Steps |
|------|-------|
| `internal/step_messaging.go` | send_sms, send_mms, send_whatsapp, list_messages, fetch_message, delete_message, fetch_media, create_messaging_service |
| `internal/step_voice.go` | create_call, fetch_call, list_calls, update_call, create_conference, list_conferences, add_participant, create_queue, fetch_recording, list_recordings, delete_recording |
| `internal/step_verify.go` | send_verification, check_verification, create_verify_service, list_verify_services |
| `internal/step_lookup.go` | lookup_phone |
| `internal/step_conversations.go` | create_conversation, send_conversation_message, add_conversation_participant, list_conversations, fetch_conversation, list_conversation_messages, create_conversation_user |
| `internal/step_video.go` | create_room, list_rooms, fetch_room, complete_room, list_room_recordings, create_composition |
| `internal/step_notify.go` | send_notification, create_binding, list_bindings, create_notify_service |
| `internal/step_taskrouter.go` | create_workspace, create_task, create_worker, create_task_queue, create_tr_workflow, list_tasks, update_task |
| `internal/step_phone_numbers.go` | search_available, buy_number, list_numbers, update_number, release_number |
| `internal/step_studio.go` | trigger_flow, list_flows, fetch_execution |
| `internal/step_serverless.go` | create_serverless_service, create_function, create_build, list_serverless_services |
| `internal/step_intelligence.go` | create_transcript, fetch_transcript, list_transcripts |
| `internal/step_flex.go` | create_flex_flow, create_web_channel, list_flex_flows |
| `internal/step_proxy.go` | create_proxy_service, create_session, add_proxy_participant |
| `internal/step_sync.go` | create_sync_service, create_document, update_document, create_sync_map, create_sync_list |
| `internal/step_wireless.go` | list_sims, fetch_sim, update_sim, create_fleet, send_command |
| `internal/step_pricing.go` | fetch_pricing, list_usage_records |
| `internal/step_accounts.go` | list_accounts, create_api_key, list_api_keys |
| `internal/step_content.go` | create_content_template, list_content_templates, fetch_content_template |
| `internal/step_trusthub.go` | create_trust_product, list_trust_products, fetch_trust_product |
| `internal/step_assistants.go` | create_assistant, list_assistants, create_knowledge_base |

**Example step pattern** (use for ALL steps — `step_messaging.go` shown):

```go
package internal

import (
	"context"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// --- step.twilio_send_sms ---

type sendSMSStep struct {
	name       string
	moduleName string
}

func newSendSMSStep(name string, config map[string]any) (sdk.StepInstance, error) {
	return &sendSMSStep{name: name, moduleName: getModuleName(config)}, nil
}

func (s *sendSMSStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	client, ok := GetClient(s.moduleName)
	if !ok {
		return &sdk.StepResult{Output: map[string]any{"error": "twilio client not found: " + s.moduleName}}, nil
	}

	to := resolveValue("to", current, config)
	from := resolveValue("from", current, config)
	body := resolveValue("body", current, config)

	if to == "" || body == "" {
		return &sdk.StepResult{Output: map[string]any{"error": "to and body are required"}}, nil
	}

	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetBody(body)
	if from != "" {
		params.SetFrom(from)
	}
	if svc := resolveValue("messaging_service_sid", current, config); svc != "" {
		params.SetMessagingServiceSid(svc)
	}
	if statusCallback := resolveValue("status_callback", current, config); statusCallback != "" {
		params.SetStatusCallback(statusCallback)
	}

	msg, err := client.Api.CreateMessage(params)
	if err != nil {
		return &sdk.StepResult{Output: map[string]any{"error": err.Error()}}, nil
	}

	output := map[string]any{
		"sid":    derefStr(msg.Sid),
		"status": derefStr(msg.Status),
	}
	if msg.DateCreated != nil {
		output["date_created"] = msg.DateCreated.Format("2006-01-02T15:04:05Z")
	}
	if msg.ErrorCode != nil {
		output["error_code"] = *msg.ErrorCode
	}
	return &sdk.StepResult{Output: output}, nil
}

// derefStr safely dereferences a *string, returning "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
```

**Key SDK patterns to follow for all steps:**
- Twilio Go SDK uses pointer fields in response structs (`*string`, `*int`). Always dereference safely with helper functions.
- SDK uses setter methods on param structs (`params.SetTo(...)`) not struct fields.
- Each product has its own package under `rest/` (e.g., `rest/api/v2010`, `rest/verify/v2`, `rest/video/v1`).
- Import the specific sub-package for each product group.

**Add deref helpers to `internal/helpers.go`:**
```go
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
```

### Step 6: Write tests

**`internal/step_messaging_test.go`** (example pattern — repeat for each product group):

Tests use a mock HTTP server that returns canned JSON responses, overriding the Twilio client's base URL. This avoids needing a provider interface:

```go
package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	twilio "github.com/twilio/twilio-go"
	twilioClient "github.com/twilio/twilio-go/client"
)

func setupMockTwilio(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: "ACtest",
		Password: "test_token",
	})
	// Override the base URL to point to the mock server
	client.SetAccountSid("ACtest")
	requestHandler := twilioClient.NewRequestHandler(client.Client)
	// The SDK client's base URL can be overridden by setting the client edge/region
	// or by wrapping. For testing, register the client and use the mock server URL
	// in the step tests by using the requestHandler.

	moduleName := "test-" + t.Name()
	RegisterClient(moduleName, client)
	t.Cleanup(func() { UnregisterClient(moduleName) })

	return moduleName
}

// For simpler testing, create mock clients with canned responses
func setupTestClient(t *testing.T, name string) {
	t.Helper()
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: "ACtest123",
		Password: "test_auth_token",
	})
	RegisterClient(name, client)
	t.Cleanup(func() { UnregisterClient(name) })
}

func TestSendSMSStep_MissingTo(t *testing.T) {
	setupTestClient(t, "test-sms")

	step, err := newSendSMSStep("sms", map[string]any{"module": "test-sms"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := step.Execute(context.Background(), nil, nil,
		map[string]any{"body": "hello"},
		nil, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["error"] == nil {
		t.Error("expected error for missing 'to'")
	}
}

func TestSendSMSStep_MissingClient(t *testing.T) {
	step, _ := newSendSMSStep("sms", map[string]any{"module": "nonexistent"})
	result, err := step.Execute(context.Background(), nil, nil,
		map[string]any{"to": "+1234", "body": "hi"},
		nil, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	errStr, _ := result.Output["error"].(string)
	if errStr == "" {
		t.Error("expected error for missing client")
	}
}
```

**Test strategy:**
- Test validation logic (missing required params → error output)
- Test missing client → error output
- Test param resolution from current vs config
- Do NOT test actual API calls (those require live credentials)
- Each product group file gets a corresponding `_test.go`
- **Module lifecycle tests** (`internal/module_test.go`): Test that Init registers the client in the registry and Stop unregisters it. Verify GetClient returns non-nil after Init, nil after Stop.

### Step 7: Create plugin.json

```json
{
    "name": "workflow-plugin-twilio",
    "version": "0.1.0",
    "description": "Comprehensive Twilio integration — SMS, Voice, Verify, Video, Conversations, TaskRouter, and 40+ products",
    "author": "GoCodeAlone",
    "license": "MIT",
    "type": "external",
    "tier": "community",
    "private": false,
    "minEngineVersion": "0.3.30",
    "keywords": ["twilio", "sms", "voice", "verify", "whatsapp", "video", "conversations", "messaging"],
    "homepage": "https://github.com/GoCodeAlone/workflow-plugin-twilio",
    "repository": "https://github.com/GoCodeAlone/workflow-plugin-twilio",
    "capabilities": {
        "configProvider": false,
        "moduleTypes": ["twilio.provider"],
        "stepTypes": [/* all ~90 step type strings */],
        "triggerTypes": []
    }
}
```

### Step 8: Build, test, commit

```bash
cd /Users/jon/workspace/workflow-plugin-twilio
go mod tidy
go vet ./...
go test ./... -v -race
go build -o bin/workflow-plugin-twilio ./cmd/workflow-plugin-twilio
git add .
git commit -m "feat: workflow-plugin-twilio v0.1.0 — comprehensive Twilio integration (~90 step types)"
```

Create the GitHub repo and push:
```bash
gh repo create GoCodeAlone/workflow-plugin-twilio --public --description "Workflow engine plugin for Twilio — SMS, Voice, Verify, Video, and 40+ products" --license MIT
git remote add origin git@github.com:GoCodeAlone/workflow-plugin-twilio.git
git push -u origin main
```

---

## Task 2: workflow-plugin-monday

**Repo:** `GoCodeAlone/workflow-plugin-monday` (create new)
**License:** MIT
**Dependencies:** None external (direct GraphQL via `net/http`)

### Step 1: Create GitHub repo and scaffold project

Same structure as Task 1. Key differences:

**`cmd/workflow-plugin-monday/main.go`:**
```go
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-monday/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.Serve(internal.NewMondayPlugin())
}
```

**`go.mod`:**
```
module github.com/GoCodeAlone/workflow-plugin-monday

go 1.26

require github.com/GoCodeAlone/workflow v0.3.32
```

### Step 2: Create GraphQL client

**`internal/client.go`:**
```go
package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL   = "https://api.monday.com/v2"
	defaultAPIVersion = "2026-04"
)

type MondayClient struct {
	baseURL    string
	apiToken   string
	apiVersion string
	httpClient *http.Client
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data       json.RawMessage `json:"data"`
	Errors     []graphQLError  `json:"errors,omitempty"`
	Complexity *complexity     `json:"complexity,omitempty"`
}

type graphQLError struct {
	Message    string `json:"message"`
	StatusCode int    `json:"status_code,omitempty"`
}

type complexity struct {
	Before int `json:"before"`
	After  int `json:"after"`
	Query  int `json:"query"`
}

func NewMondayClient(apiToken string, opts ...ClientOption) *MondayClient {
	c := &MondayClient{
		baseURL:    defaultBaseURL,
		apiToken:   apiToken,
		apiVersion: defaultAPIVersion,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type ClientOption func(*MondayClient)

func WithBaseURL(url string) ClientOption {
	return func(c *MondayClient) { c.baseURL = url }
}

func WithAPIVersion(version string) ClientOption {
	return func(c *MondayClient) { c.apiVersion = version }
}

// Execute runs a GraphQL query/mutation and returns the raw data JSON.
func (c *MondayClient) Execute(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiToken)
	req.Header.Set("API-Version", c.apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (HTTP 429)")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}

	return gqlResp.Data, nil
}

// ExecuteInto runs a GraphQL query and unmarshals the data into target.
func (c *MondayClient) ExecuteInto(ctx context.Context, query string, variables map[string]any, target any) error {
	data, err := c.Execute(ctx, query, variables)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
```

### Step 3: Create registry and module

**`internal/registry.go`:**
```go
package internal

import "sync"

var (
	clientMu       sync.RWMutex
	clientRegistry = make(map[string]*MondayClient)
)

func RegisterClient(name string, c *MondayClient)         { clientMu.Lock(); defer clientMu.Unlock(); clientRegistry[name] = c }
func GetClient(name string) (*MondayClient, bool)         { clientMu.RLock(); defer clientMu.RUnlock(); c, ok := clientRegistry[name]; return c, ok }
func UnregisterClient(name string)                         { clientMu.Lock(); defer clientMu.Unlock(); delete(clientRegistry, name) }
```

**`internal/module.go`:**
```go
package internal

import (
	"context"
	"fmt"
)

type mondayModule struct {
	name   string
	config map[string]any
}

func newMondayModule(name string, config map[string]any) (*mondayModule, error) {
	token, _ := config["apiToken"].(string)
	if token == "" {
		return nil, fmt.Errorf("monday.provider %q: apiToken is required", name)
	}
	return &mondayModule{name: name, config: config}, nil
}

func (m *mondayModule) Init() error {
	token, _ := m.config["apiToken"].(string)
	var opts []ClientOption
	if v, ok := m.config["apiVersion"].(string); ok && v != "" {
		opts = append(opts, WithAPIVersion(v))
	}
	if v, ok := m.config["baseUrl"].(string); ok && v != "" {
		opts = append(opts, WithBaseURL(v))
	}
	RegisterClient(m.name, NewMondayClient(token, opts...))
	return nil
}

func (m *mondayModule) Start(_ context.Context) error { return nil }
func (m *mondayModule) Stop(_ context.Context) error  { UnregisterClient(m.name); return nil }
func (m *mondayModule) Name() string                  { return m.name }
```

### Step 4: Create plugin.go and step registry

Same pattern as Twilio — `step_registry.go` with factory map. Default module name `"monday"`.

### Step 5: Implement step files

**File organization:**

| File | Steps |
|------|-------|
| `internal/step_boards.go` | create_board, list_boards, fetch_board, update_board, delete_board, duplicate_board, archive_board |
| `internal/step_items.go` | create_item, list_items, fetch_item, update_item, move_item, archive_item, delete_item, search_items |
| `internal/step_subitems.go` | create_subitem, list_subitems, update_subitem, delete_subitem |
| `internal/step_columns.go` | get_column_values, change_column_value, create_column |
| `internal/step_groups.go` | create_group, list_groups, update_group, move_group, delete_group |
| `internal/step_workspaces.go` | create_workspace, list_workspaces, update_workspace, delete_workspace |
| `internal/step_folders.go` | create_folder, list_folders, update_folder, delete_folder |
| `internal/step_updates.go` | create_update, list_updates, edit_update, delete_update |
| `internal/step_users.go` | list_users, fetch_user, invite_user |
| `internal/step_teams.go` | list_teams, add_team_to_workspace |
| `internal/step_tags.go` | list_tags, create_tag |
| `internal/step_files.go` | upload_file, list_files |
| `internal/step_notifications.go` | create_notification |
| `internal/step_webhooks.go` | create_webhook, list_webhooks, delete_webhook |
| `internal/step_documents.go` | create_document, list_documents, update_document |
| `internal/step_generic.go` | query, mutate |

**Example step** (`step_boards.go`):
```go
package internal

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// --- step.monday_create_board ---

type createBoardStep struct {
	name       string
	moduleName string
}

func newCreateBoardStep(name string, config map[string]any) (sdk.StepInstance, error) {
	return &createBoardStep{name: name, moduleName: getModuleName(config)}, nil
}

func (s *createBoardStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	client, ok := GetClient(s.moduleName)
	if !ok {
		return &sdk.StepResult{Output: map[string]any{"error": "monday client not found: " + s.moduleName}}, nil
	}

	boardName := resolveValue("board_name", current, config)
	boardKind := resolveValue("board_kind", current, config)
	if boardName == "" {
		return &sdk.StepResult{Output: map[string]any{"error": "board_name is required"}}, nil
	}
	if boardKind == "" {
		boardKind = "public"
	}

	query := `mutation ($boardName: String!, $boardKind: BoardKind!) {
		create_board(board_name: $boardName, board_kind: $boardKind) {
			id name board_kind state
		}
	}`
	variables := map[string]any{
		"boardName": boardName,
		"boardKind": boardKind,
	}

	// Add optional workspace_id
	if wsID := resolveValue("workspace_id", current, config); wsID != "" {
		query = `mutation ($boardName: String!, $boardKind: BoardKind!, $workspaceId: ID!) {
			create_board(board_name: $boardName, board_kind: $boardKind, workspace_id: $workspaceId) {
				id name board_kind state
			}
		}`
		variables["workspaceId"] = wsID
	}

	data, err := client.Execute(ctx, query, variables)
	if err != nil {
		return &sdk.StepResult{Output: map[string]any{"error": err.Error()}}, nil
	}

	var result struct {
		CreateBoard map[string]any `json:"create_board"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return &sdk.StepResult{Output: map[string]any{"error": fmt.Sprintf("parse response: %v", err)}}, nil
	}

	return &sdk.StepResult{Output: result.CreateBoard}, nil
}
```

**monday.com GraphQL reference** (use these for building mutations/queries):
- API docs: `https://developer.monday.com/api-reference/reference/boards`
- All mutations use `mutation { ... }` with typed variables
- All queries use `query { ... }` with cursor-based pagination
- Column values use typed JSON: `change_multiple_column_values(item_id: $id, board_id: $boardId, column_values: $columnValues)`

### Step 6: Write tests

Use `httptest.NewServer` to mock the monday.com GraphQL endpoint. Register the client with `WithBaseURL(server.URL)`. Test validation, GraphQL error handling, and successful responses.

**Module lifecycle tests** (`internal/module_test.go`): Test that Init registers the client in the registry and Stop unregisters it. Verify GetClient returns non-nil after Init, nil after Stop.

### Step 7: Build, test, commit, push

Same pattern as Twilio task.

---

## Task 3: workflow-plugin-turnio

**Repo:** `GoCodeAlone/workflow-plugin-turnio` (create new)
**License:** MIT
**Dependencies:** None external

### Step 1: Scaffold project

Same structure. Entry point calls `sdk.Serve(internal.NewTurnIOPlugin())`.

**`go.mod`:**
```
module github.com/GoCodeAlone/workflow-plugin-turnio

go 1.26

require github.com/GoCodeAlone/workflow v0.3.32
```

### Step 2: Create REST client

**`internal/client.go`:**
```go
package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTurnBaseURL = "https://whatsapp.turn.io"

type TurnClient struct {
	mu                 sync.Mutex
	rateLimitRemaining int
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

func NewTurnClient(apiToken string, baseURL string) *TurnClient {
	if baseURL == "" {
		baseURL = defaultTurnBaseURL
	}
	return &TurnClient{
		baseURL:    baseURL,
		apiToken:   apiToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *TurnClient) Do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Track rate limit budget from response headers
	if remaining := resp.Header.Get("X-Ratelimit-Remaining"); remaining != "" {
		if n, err := strconv.Atoi(remaining); err == nil {
			c.mu.Lock()
			c.rateLimitRemaining = n
			c.mu.Unlock()
		}
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return nil, fmt.Errorf("rate limited (HTTP 429), retry after: %s", retryAfter)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.RawMessage(respBody), nil
}

func (c *TurnClient) DoInto(ctx context.Context, method, path string, body, target any) error {
	data, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
```

### Step 3: Create registry and module

Same pattern as monday. Module type: `turnio.provider`. Config: `apiToken`, optional `baseUrl`. Default module name: `"turnio"`.

### Step 4: Implement step files

| File | Steps |
|------|-------|
| `internal/step_messages.go` | send_text, send_media, send_template, send_interactive, send_location, list_messages |
| `internal/step_contacts.go` | check_contact, upload_contacts, update_profile |
| `internal/step_media.go` | upload_media, get_media, delete_media |
| `internal/step_templates.go` | create_template, list_templates, fetch_template, update_template, delete_template |
| `internal/step_webhooks.go` | configure_webhook |
| `internal/step_flows.go` | create_flow, list_flows, send_flow |
| `internal/step_journeys.go` | list_journeys, trigger_journey |
| `internal/step_context.go` | get_context, set_context |

**Example step** (`step_messages.go`):
```go
package internal

import (
	"context"
	"encoding/json"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type sendTextStep struct {
	name       string
	moduleName string
}

func newSendTextStep(name string, config map[string]any) (sdk.StepInstance, error) {
	return &sendTextStep{name: name, moduleName: getModuleName(config)}, nil
}

func (s *sendTextStep) Execute(ctx context.Context, _ map[string]any, _ map[string]map[string]any, current map[string]any, _ map[string]any, config map[string]any) (*sdk.StepResult, error) {
	client, ok := GetClient(s.moduleName)
	if !ok {
		return &sdk.StepResult{Output: map[string]any{"error": "turnio client not found: " + s.moduleName}}, nil
	}

	to := resolveValue("to", current, config)
	body := resolveValue("body", current, config)
	if to == "" || body == "" {
		return &sdk.StepResult{Output: map[string]any{"error": "to and body are required"}}, nil
	}

	payload := map[string]any{
		"to":   to,
		"type": "text",
		"text": map[string]any{"body": body},
	}

	// Preview URL opt-in
	if resolveBool("preview_url", current, config) {
		payload["preview_url"] = true
	}

	data, err := client.Do(ctx, "POST", "/v1/messages", payload)
	if err != nil {
		return &sdk.StepResult{Output: map[string]any{"error": err.Error()}}, nil
	}

	var result map[string]any
	json.Unmarshal(data, &result)
	return &sdk.StepResult{Output: result}, nil
}
```

**turn.io API reference** for message format:
- Text: `{"to": "27123456789", "type": "text", "text": {"body": "Hello"}}`
- Template: `{"to": "...", "type": "template", "template": {"namespace": "...", "name": "...", "language": {"code": "en"}, "components": [...]}}`
- Media: `{"to": "...", "type": "image", "image": {"link": "https://..."}}`
- Interactive buttons: `{"to": "...", "type": "interactive", "interactive": {"type": "button", "body": {...}, "action": {...}}}`

### Step 5: Write tests, build, commit, push

Same pattern as other plugins. Use `httptest.NewServer` to mock turn.io REST API. Include rate limit header tracking tests — verify `X-Ratelimit-Remaining` is parsed and stored.

**Module lifecycle tests** (`internal/module_test.go`): Test that Init registers the client in the registry and Stop unregisters it. Verify GetClient returns non-nil after Init, nil after Stop.

---

## Task 4: Registry Manifests

**Depends on:** Tasks 1-3 completing.

### Step 1: Create registry manifests

Create three manifest files in `workflow-registry`:

- `/Users/jon/workspace/workflow-registry/plugins/twilio/manifest.json`
- `/Users/jon/workspace/workflow-registry/plugins/monday/manifest.json`
- `/Users/jon/workspace/workflow-registry/plugins/turnio/manifest.json`

Each manifest includes the full list of `stepTypes` from the corresponding `plugin.json`.

### Step 2: Commit and push

```bash
cd /Users/jon/workspace/workflow-registry
git add plugins/twilio plugins/monday plugins/turnio
git commit -m "feat: add twilio, monday.com, and turn.io plugin manifests"
git push
```

---

## Key Reference Files

| File | Purpose |
|------|---------|
| `/Users/jon/workspace/workflow/plugin/external/sdk/interfaces.go` | SDK interface definitions |
| `/Users/jon/workspace/workflow/plugin/external/sdk/serve.go` | `sdk.Serve()` entry point |
| `/Users/jon/workspace/workflow-plugin-payments/internal/plugin.go` | Reference plugin implementation |
| `/Users/jon/workspace/workflow-plugin-payments/internal/step_charge.go` | Reference step pattern |
| `/Users/jon/workspace/workflow-plugin-payments/internal/helpers.go` | Helper functions to copy |
| `/Users/jon/workspace/workflow-plugin-payments/.goreleaser.yml` | GoReleaser config template |
| `/Users/jon/workspace/workflow-plugin-payments/.github/workflows/release.yml` | Release workflow template |
| `/Users/jon/workspace/workflow-plugin-payments/plugin.json` | Manifest template |
| `/Users/jon/workspace/workflow-plugin-payments/Makefile` | Build template |
| `/Users/jon/workspace/workflow-registry/schema/registry-schema.json` | Manifest validation schema |
