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

Conversations flow through: new -> queued -> assigned -> active -> wrap_up -> closed

With branches for: transferred, escalated_medical, escalated_police, follow_up_scheduled.

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
