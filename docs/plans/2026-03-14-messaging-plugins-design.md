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
