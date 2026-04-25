---
status: implemented
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow-plugin-messaging-core
    commit: e68b2ab
  - repo: workflow-plugin-discord
    commit: 505cee9
  - repo: workflow-plugin-slack
    commit: 39124bf
  - repo: workflow-plugin-teams
    commit: ee881db
external_refs:
  - "workflow-scenarios: scenarios/59-discord-messaging"
  - "workflow-scenarios: scenarios/60-slack-messaging"
  - "workflow-scenarios: scenarios/61-teams-messaging"
  - "workflow-scenarios: scenarios/62-cross-platform-messaging"
verification:
  last_checked: 2026-04-25
  commands:
    - "jq -r '.name, (.capabilities.stepTypes // .stepTypes // [] | length), (.capabilities.triggerTypes // .triggerTypes // [] | length)' /Users/jon/workspace/workflow-plugin-{discord,slack,teams}/plugin.json"
    - "rg -n \"MessagingProvider|EventListener\" /Users/jon/workspace/workflow-plugin-messaging-core"
    - "rg -n \"discord-messaging|slack-messaging|teams-messaging|cross-platform-messaging\" /Users/jon/workspace/workflow-scenarios"
  result: pass
supersedes: []
superseded_by: []
---

# Messaging Platform Plugins Design

## Overview

Three external workflow plugins for Discord, Slack, and Microsoft Teams, sharing a common messaging interface. Each uses official/standard Go SDKs.

## Architecture

```
workflow-plugin-messaging-core/   ← shared Go module, interfaces only
workflow-plugin-discord/          ← bwmarrin/discordgo
workflow-plugin-slack/            ← slack-go/slack
workflow-plugin-teams/            ← microsoftgraph/msgraph-sdk-go
```

## Common Interface

```go
type MessagingProvider interface {
    SendMessage(ctx, channelID, content string, opts MessageOpts) (string, error)
    EditMessage(ctx, channelID, messageID, content string) error
    DeleteMessage(ctx, channelID, messageID string) error
    SendReply(ctx, channelID, parentID, content string) (string, error)
    React(ctx, channelID, messageID, emoji string) error
    UploadFile(ctx, channelID string, file io.Reader, filename string) (string, error)
}

type EventListener interface {
    Listen(ctx context.Context) (<-chan Event, error)
    Close() error
}
```

## Per-Plugin Step Types

Discord: send_message, send_embed, edit_message, delete_message, add_reaction, upload_file, create_thread, voice_join, voice_leave, voice_play
Slack: send_message, send_blocks, edit_message, delete_message, add_reaction, upload_file, send_thread_reply, set_topic
Teams: send_message, send_card, reply_message, delete_message, upload_file, create_channel, add_member

## Triggers

trigger.discord (WebSocket Gateway), trigger.slack (Socket Mode), trigger.teams (Graph change notifications)

## Out of Scope

Screen sharing, video conferencing, Slack/Teams voice (no Go APIs available)
