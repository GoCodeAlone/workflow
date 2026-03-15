# Messaging Plugins Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create four new repos — a shared messaging core interface module and three external workflow plugins for Discord, Slack, and Microsoft Teams.

**Architecture:** Each plugin is a standalone gRPC binary using the workflow external plugin SDK. They share a common `messaging-core` Go module defining the `MessagingProvider` and `EventListener` interfaces. Each plugin provides a `.provider` module (holding credentials/config), step types for platform actions, and a trigger type for real-time events.

**Tech Stack:** Go 1.26, workflow plugin SDK, `bwmarrin/discordgo`, `slack-go/slack`, `microsoftgraph/msgraph-sdk-go`, GoReleaser v2

---

## Task 1: Create workflow-plugin-messaging-core repo

**What:** Shared Go module with interfaces — no binary, just types.

**Step 1:** Create repo on GitHub:
```bash
gh repo create GoCodeAlone/workflow-plugin-messaging-core --public --description "Shared messaging interfaces for workflow platform plugins" --clone
cd /Users/jon/workspace/workflow-plugin-messaging-core
go mod init github.com/GoCodeAlone/workflow-plugin-messaging-core
```

**Step 2:** Create `messaging.go`:
```go
package messaging

import (
    "context"
    "io"
    "time"
)

// MessageOpts configures optional message parameters.
type MessageOpts struct {
    Embeds     []Embed           // Rich embeds (Discord), blocks (Slack), cards (Teams)
    Files      []FileAttachment  // Inline file attachments
    ThreadID   string            // Reply to thread/parent message
    Ephemeral  bool              // Only visible to one user (where supported)
    Components []Component       // Interactive components (buttons, menus)
}

// Embed represents a rich message attachment (platform-specific rendering).
type Embed struct {
    Title       string
    Description string
    URL         string
    Color       int
    Fields      []EmbedField
    ImageURL    string
    FooterText  string
    Timestamp   time.Time
}

type EmbedField struct {
    Name   string
    Value  string
    Inline bool
}

type FileAttachment struct {
    Name   string
    Reader io.Reader
}

type Component struct {
    Type  string         // "button", "select", "action_row"
    Data  map[string]any // Platform-specific component data
}

// Message represents a sent or received message.
type Message struct {
    ID        string
    ChannelID string
    AuthorID  string
    Content   string
    Timestamp time.Time
    ThreadID  string
    Embeds    []Embed
}

// Event represents a real-time platform event.
type Event struct {
    Type      string         // "message_create", "message_update", "reaction_add", "member_join", etc.
    ChannelID string
    UserID    string
    MessageID string
    Content   string
    Data      map[string]any // Platform-specific event data
    Timestamp time.Time
}

// Provider is the common messaging interface implemented by each platform plugin.
type Provider interface {
    // Name returns the platform identifier ("discord", "slack", "teams").
    Name() string

    // SendMessage sends a message to a channel and returns the message ID.
    SendMessage(ctx context.Context, channelID, content string, opts *MessageOpts) (string, error)

    // EditMessage updates an existing message.
    EditMessage(ctx context.Context, channelID, messageID, content string) error

    // DeleteMessage removes a message.
    DeleteMessage(ctx context.Context, channelID, messageID string) error

    // SendReply sends a threaded reply and returns the message ID.
    SendReply(ctx context.Context, channelID, parentID, content string, opts *MessageOpts) (string, error)

    // React adds a reaction to a message.
    React(ctx context.Context, channelID, messageID, emoji string) error

    // UploadFile sends a file to a channel and returns the message/file ID.
    UploadFile(ctx context.Context, channelID string, file io.Reader, filename string) (string, error)
}

// EventListener receives real-time events from the platform.
type EventListener interface {
    // Listen starts receiving events. The returned channel is closed when
    // the context is cancelled or Close is called.
    Listen(ctx context.Context) (<-chan Event, error)

    // Close stops the event listener and releases resources.
    Close() error
}

// VoiceProvider is optionally implemented by platforms with voice support (Discord).
type VoiceProvider interface {
    JoinVoice(ctx context.Context, guildID, channelID string) error
    LeaveVoice(ctx context.Context, guildID string) error
    PlayAudio(ctx context.Context, guildID string, audio io.Reader) error
}
```

**Step 3:** Create `messaging_test.go` — compile check:
```go
package messaging

// Compile-time interface satisfaction checks are done per-plugin.
```

**Step 4:**
```bash
go mod tidy
git add -A && git commit -m "feat: shared messaging interfaces (Provider, EventListener, VoiceProvider)"
git push origin main
git tag -a v0.1.0 -m "v0.1.0: initial messaging interfaces" && git push origin v0.1.0
```

---

## Task 2: Create workflow-plugin-discord repo

**What:** External gRPC plugin using `bwmarrin/discordgo`.

**Step 1:** Create repo and scaffold:
```bash
gh repo create GoCodeAlone/workflow-plugin-discord --public --description "Workflow plugin for Discord messaging, bots, and voice" --clone
cd /Users/jon/workspace/workflow-plugin-discord
go mod init github.com/GoCodeAlone/workflow-plugin-discord
```

**Step 2:** Add dependencies:
```bash
go get github.com/GoCodeAlone/workflow@v0.3.40
go get github.com/GoCodeAlone/workflow-plugin-messaging-core@v0.1.0
go get github.com/bwmarrin/discordgo
go mod tidy
```

**Step 3:** Create the plugin structure:

```
cmd/workflow-plugin-discord/main.go    ← one-liner: sdk.Serve(internal.New())
internal/
  plugin.go          ← PluginProvider + ModuleProvider + StepProvider + TriggerProvider
  provider.go        ← discord.provider module (holds discordgo.Session)
  step_send.go       ← step.discord_send_message
  step_embed.go      ← step.discord_send_embed
  step_edit.go       ← step.discord_edit_message
  step_delete.go     ← step.discord_delete_message
  step_react.go      ← step.discord_add_reaction
  step_upload.go     ← step.discord_upload_file
  step_thread.go     ← step.discord_create_thread
  step_voice.go      ← step.discord_voice_join / leave / play
  trigger.go         ← trigger.discord (WebSocket Gateway listener)
  convert.go         ← messaging.Message ↔ discordgo type conversions
plugin.json          ← manifest
.goreleaser.yaml
.github/workflows/release.yml
```

**Step 4:** Implement `internal/plugin.go`:
```go
package internal

import "github.com/GoCodeAlone/workflow/plugin/external/sdk"

type discordPlugin struct{}

func New() *discordPlugin { return &discordPlugin{} }

func (p *discordPlugin) Manifest() sdk.PluginManifest {
    return sdk.PluginManifest{
        Name: "discord", Version: "0.1.0", Author: "GoCodeAlone",
        Description: "Discord messaging, bots, and voice",
    }
}

func (p *discordPlugin) ModuleTypes() []string { return []string{"discord.provider"} }
func (p *discordPlugin) StepTypes() []string {
    return []string{
        "step.discord_send_message", "step.discord_send_embed",
        "step.discord_edit_message", "step.discord_delete_message",
        "step.discord_add_reaction", "step.discord_upload_file",
        "step.discord_create_thread",
        "step.discord_voice_join", "step.discord_voice_leave",
    }
}
func (p *discordPlugin) TriggerTypes() []string { return []string{"trigger.discord"} }

// CreateModule, CreateStep, CreateTrigger dispatch to constructors...
```

**Step 5:** Implement `internal/provider.go` — the module that holds the discordgo session:
```go
type discordProvider struct {
    name    string
    token   string
    session *discordgo.Session
}

func (m *discordProvider) Init() error {
    dg, err := discordgo.New("Bot " + m.token)
    if err != nil {
        return fmt.Errorf("discord session: %w", err)
    }
    dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages |
        discordgo.IntentsGuildVoiceStates | discordgo.IntentsGuildMessageReactions
    m.session = dg
    return nil
}

func (m *discordProvider) Start(ctx context.Context) error {
    return m.session.Open()
}

func (m *discordProvider) Stop(ctx context.Context) error {
    return m.session.Close()
}
```

Provider also implements `messaging.Provider` and `messaging.VoiceProvider`.

**Step 6:** Implement each step type. Each step reads config (channel_id, content, etc.) from the `current` map (NOT from `config` — external plugin gotcha), looks up the discord.provider module by name, and calls the appropriate discordgo method.

Example `step_send.go`:
```go
func (s *sendMessageStep) Execute(ctx context.Context, triggerData, stepOutputs, current, metadata, cfg map[string]any) (*sdk.StepResult, error) {
    channelID, _ := current["channel_id"].(string)
    content, _ := current["content"].(string)
    if channelID == "" || content == "" {
        return nil, fmt.Errorf("discord_send_message: channel_id and content required")
    }
    msg, err := s.session.ChannelMessageSend(channelID, content)
    if err != nil {
        return nil, fmt.Errorf("discord_send_message: %w", err)
    }
    return &sdk.StepResult{Output: map[string]any{
        "message_id": msg.ID,
        "channel_id": msg.ChannelID,
    }}, nil
}
```

**Step 7:** Implement `internal/trigger.go` — WebSocket Gateway event listener:
```go
type discordTrigger struct {
    session  *discordgo.Session
    callback sdk.TriggerCallback
    cancel   context.CancelFunc
}

func (t *discordTrigger) Start(ctx context.Context) error {
    ctx, t.cancel = context.WithCancel(ctx)
    t.session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
        t.callback.Fire(map[string]any{
            "type":       "message_create",
            "channel_id": m.ChannelID,
            "message_id": m.ID,
            "content":    m.Content,
            "author_id":  m.Author.ID,
            "guild_id":   m.GuildID,
        })
    })
    // Add handlers for other event types...
    return nil
}
```

**Step 8:** Write tests. Use discordgo's test helpers or mock HTTP for API calls. Test each step's Execute with expected inputs/outputs.

**Step 9:** Create `plugin.json`, `.goreleaser.yaml`, `.github/workflows/release.yml` — copy patterns from workflow-plugin-admin. See the plugin pattern exploration for exact templates.

**Step 10:** Build, test, commit, tag:
```bash
go build ./... && go test ./... -v -count=1
git add -A && git commit -m "feat: Discord plugin with messaging, embeds, voice, and event trigger"
git tag -a v0.1.0 -m "v0.1.0: initial Discord plugin"
git push origin main --tags
```

---

## Task 3: Create workflow-plugin-slack repo

**What:** External gRPC plugin using `slack-go/slack`.

Same structure as Discord. Key differences:

**Dependencies:**
```bash
go get github.com/slack-go/slack
```

**Module:** `slack.provider` holds `*slack.Client` + Socket Mode client:
```go
type slackProvider struct {
    name      string
    botToken  string  // xoxb-...
    appToken  string  // xapp-... (for Socket Mode)
    client    *slack.Client
    socketClient *socketmode.Client
}

func (m *slackProvider) Init() error {
    m.client = slack.New(m.botToken, slack.OptionAppLevelToken(m.appToken))
    m.socketClient = socketmode.New(m.client)
    return nil
}
```

**Step types:**
- `step.slack_send_message` — `client.PostMessage(channelID, slack.MsgOptionText(content, false))`
- `step.slack_send_blocks` — `client.PostMessage(channelID, slack.MsgOptionBlocks(blocks...))`
- `step.slack_edit_message` — `client.UpdateMessage(channelID, timestamp, slack.MsgOptionText(...))`
- `step.slack_delete_message` — `client.DeleteMessage(channelID, timestamp)`
- `step.slack_add_reaction` — `client.AddReaction(emoji, slack.ItemRef{Channel, Timestamp})`
- `step.slack_upload_file` — `client.UploadFile(slack.FileUploadParameters{...})`
- `step.slack_send_thread_reply` — `client.PostMessage(channelID, slack.MsgOptionTS(threadTS), ...)`
- `step.slack_set_topic` — `client.SetTopicOfConversation(channelID, topic)`

**Trigger:** `trigger.slack` uses Socket Mode:
```go
func (t *slackTrigger) Start(ctx context.Context) error {
    go t.socketClient.Run()
    go func() {
        for evt := range t.socketClient.Events {
            switch evt.Type {
            case socketmode.EventTypeEventsAPI:
                // Extract message event, fire callback
            case socketmode.EventTypeSlashCommand:
                // Extract command, fire callback
            }
        }
    }()
    return nil
}
```

**Rate limits:** Wrap API calls with retry on `slack.RateLimitedError`:
```go
if rateLimitErr, ok := err.(*slack.RateLimitedError); ok {
    time.Sleep(rateLimitErr.RetryAfter)
    // retry
}
```

**Build, test, tag v0.1.0.**

---

## Task 4: Create workflow-plugin-teams repo

**What:** External gRPC plugin using `microsoftgraph/msgraph-sdk-go`.

**Dependencies:**
```bash
go get github.com/microsoftgraph/msgraph-sdk-go
go get github.com/Azure/azure-sdk-for-go/sdk/azidentity
```

**Module:** `teams.provider` holds Graph client with Azure AD auth:
```go
type teamsProvider struct {
    name     string
    tenantID string
    clientID string
    secret   string
    client   *msgraphsdk.GraphServiceClient
}

func (m *teamsProvider) Init() error {
    cred, err := azidentity.NewClientSecretCredential(m.tenantID, m.clientID, m.secret, nil)
    if err != nil {
        return fmt.Errorf("teams auth: %w", err)
    }
    m.client, err = msgraphsdk.NewGraphServiceClientWithCredentials(cred, []string{"https://graph.microsoft.com/.default"})
    return err
}
```

**Step types:** All use Graph API via the SDK:
- `step.teams_send_message` — `client.Teams().ByTeamId().Channels().ByChannelId().Messages().Post(ctx, body)`
- `step.teams_send_card` — same but with Adaptive Card in body content type
- `step.teams_reply_message` — `.Messages().ByMessageId().Replies().Post(ctx, body)`
- `step.teams_delete_message` — `.Messages().ByMessageId().Delete(ctx)`
- `step.teams_upload_file` — SharePoint/OneDrive via `client.Drives().ByDriveId().Items()...Upload()`
- `step.teams_create_channel` — `client.Teams().ByTeamId().Channels().Post(ctx, body)`
- `step.teams_add_member` — `client.Teams().ByTeamId().Members().Post(ctx, body)`

**Trigger:** `trigger.teams` uses Graph Change Notifications (HTTP webhook subscriptions):
```go
type teamsTrigger struct {
    client       *msgraphsdk.GraphServiceClient
    callback     sdk.TriggerCallback
    callbackURL  string  // public HTTPS URL for Graph to POST to
    subscription string  // subscription ID for cleanup
}

func (t *teamsTrigger) Start(ctx context.Context) error {
    // Create subscription on /teams/{id}/channels/{id}/messages
    // Graph POSTs events to callbackURL
    // Start HTTP server to receive notifications
    // Parse notification, fire callback
}
```

Note: Teams trigger requires a public HTTPS endpoint for Graph change notifications. Document this requirement clearly.

**Rate limits:** The Graph SDK includes automatic retry middleware for 429/503 — no manual handling needed.

**Build, test, tag v0.1.0.**

---

## Task 5: Register all plugins in workflow-registry

**What:** Add manifest files for each plugin.

Create in `/Users/jon/workspace/workflow-registry/plugins/`:
- `discord/manifest.json`
- `slack/manifest.json`
- `teams/manifest.json`

Each follows the registry schema with `capabilities`, `keywords`, download URLs from GitHub releases.

```bash
cd /Users/jon/workspace/workflow-registry
# Add the three manifest files
git add plugins/discord plugins/slack plugins/teams
git commit -m "feat: add Discord, Slack, Teams plugin manifests"
git push origin main
```

---

## Task 6: Integration tests + QA

**For each plugin:**
1. Build the binary: `go build -o /tmp/test-plugin ./cmd/workflow-plugin-<name>`
2. Run with mock credentials and verify:
   - Plugin loads and responds to health checks
   - Module creates with valid config
   - Steps return expected errors for missing credentials
   - Step types and module types match plugin.json
3. If real tokens available, test live send/receive

---

## Execution Order

```
Task 1 (messaging-core) → Tasks 2, 3, 4 (plugins, parallel)
                         → Task 5 (registry)
                         → Task 6 (QA)
```

**Parallel groups:**
- Group A: Task 2 (Discord)
- Group B: Task 3 (Slack)
- Group C: Task 4 (Teams)
- All three depend only on Task 1 (messaging-core v0.1.0 tag)
