# Chat Platform Walkthrough

## Overview

The chat platform is a production-grade mental health support application built entirely on the workflow engine. It demonstrates multi-service architecture, 18 dynamic components, real-time messaging, and role-based access control.

## Architecture

```
Browser -> [gateway:8080] -> reverse proxy -> [api:8081]          (auth, admin)
                                            -> [conversation:8082] (chat, state machine)

[conversation] <-> [Kafka] <-> [conversation]  (event-driven routing)
```

## Running

### Docker Compose (distributed)

```bash
cd example/chat-platform
docker compose --profile distributed up --build
```

### Monolith (development)

```bash
go run ./cmd/server -config example/chat-platform/workflow.yaml
```

Open http://localhost:8080 and log in with `responder1@example.com` / `demo123`.

## Key Components

### Conversation State Machine

Conversations follow this lifecycle:

```
new -> queued (auto via route_message) -> assigned (accept) -> active (auto via start_conversation) -> wrap_up -> closed
```

With branches from "active" for: transferred, escalated_medical, escalated_police, follow_up_scheduled.

**Key transitions:**
- `new` -> `queued`: Automatic when `route_message` processes an inbound message and assigns it to a program queue.
- `queued` -> `assigned`: Triggered when a responder clicks "Pick from Queue" on the dashboard or "Accept Conversation" on a queued conversation's banner.
- `assigned` -> `active`: Automatic via `start_conversation` once a responder is assigned.
- `active` -> `transferred`: Via Actions > Transfer (select target responder, add note).
- `active` -> `escalated_medical` / `escalated_police`: Via Actions > Escalate Medical or Escalate Police.
- `active` -> `wrap_up`: Via Actions > Wrap Up.
- `wrap_up` -> `closed`: Via Actions > Close Conversation.

### Dynamic Components (18)

| Component | Purpose |
|-----------|---------|
| keyword_matcher | Routes inbound messages by keyword |
| conversation_router | Assigns conversations to programs/queues |
| message_processor | Handles inbound/outbound message flow |
| risk_tagger | Real-time risk assessment (5 categories) |
| ai_summarizer | AI-generated conversation summaries |
| pii_encryptor | AES-256-GCM field-level encryption |
| escalation_handler | Medical/police escalation flow |
| survey_engine | Entry/exit survey management |
| followup_scheduler | Scheduled follow-up check-ins |
| webchat_handler | Web-based chat sessions |
| twilio_provider | Twilio SMS simulation |
| aws_provider | AWS SNS/Pinpoint simulation |
| partner_provider | Partner webhook simulation |
| notification_sender | System notifications |
| data_retention | Data retention policy enforcement |

### Role-Based Views

- **Responder**: Dashboard, chat view, multi-chat, DM, actions (transfer, escalate, tag, wrap-up)
- **Supervisor**: Overview with KPIs, responder monitoring, read-only chat, queue health
- **Admin**: Affiliate/program/user/keyword/survey CRUD

### Multi-Tenancy

Three demo affiliates operate independently:
- Crisis Support International (US-East)
- Youth Mental Health Alliance (US-West)
- Global Wellness Network (EU-West)

Each has its own responders, supervisors, programs, and data isolation.

## Testing the Responder Workflow

### Accept Conversation via Banner

1. Simulate an inbound message (see "Simulating Conversations" below) to create a queued conversation.
2. Log in as `responder1@example.com` / `demo123`.
3. Navigate to the queued conversation directly (e.g., from the dashboard queue count).
4. A banner appears at the top of the conversation view with an "Accept Conversation" button.
5. Click the button -- the conversation is assigned to you and auto-transitions to "active" state.
6. You can now send messages, transfer, escalate, tag, or wrap up the conversation.

### Verifying Cross-Affiliate Isolation

1. Log in as `responder1@example.com` (Crisis Support International). Note the conversations and queue data visible.
2. Log out, then log in as `responder3@example.com` (Youth Mental Health Alliance). You should see a completely different set of conversations and queue data -- no overlap with the previous session.
3. Log in as `supervisor1@example.com` (Crisis Support International). Verify you only see Crisis Support International responders and conversations.
4. Log in as `supervisor2@example.com` (Youth Mental Health Alliance). Verify you only see Youth Mental Health Alliance data.
5. Log in as `admin@example.com`. Verify that all affiliates' data is visible across the platform.

### Testing Supervisor Read-Only Access

1. Log in as `supervisor1@example.com` / `demo123`.
2. Navigate to any active conversation via the supervisor overview.
3. The chat view displays a read-only badge and the message input is disabled. Supervisors can observe but cannot send messages.

## Simulating Conversations

```bash
# Inbound SMS via Twilio
curl -X POST http://localhost:8080/api/webhooks/twilio \
  -d "From=%2B15551234567&To=%2B1741741&Body=HELLO&MessageSid=SM001"

# Webchat
curl -X POST http://localhost:8080/api/webchat/message \
  -d '{"sessionId": "web-001", "message": "I need help"}'
```

## Further Reading

- [User Guide](../../example/chat-platform/docs/USER_GUIDE.md) - Full feature documentation with screenshots
- [PLAN.md](../../example/chat-platform/PLAN.md) - Implementation specification
- [API Reference](../API.md) - REST endpoint documentation
