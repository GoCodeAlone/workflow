# Chat Platform - Implementation Specification

## Overview

A text-based crisis/support chat platform built entirely on the Workflow engine.
Supports inbound/outbound SMS (Twilio, AWS, partner webhooks) and webchat.
Operates on behalf of multiple client organizations (Affiliates) with Programs.
Responders handle conversations, Supervisors oversee Responders, Admins configure.

## Architecture

### Containers (Docker Compose)

```
Browser -> [gateway:8080] -> reverse proxy -> [api:8081]          (auth, platform, admin)
                                            -> [conversation:8082] (chat, state machine, providers)

[conversation] <-> [Kafka] <-> [conversation]  (event-driven message routing)
```

### Service: Gateway (gateway.yaml)
- Static file server for SPA (/spa/) and webchat widget
- Reverse proxy routing:
  - /api/auth/*        -> api:8081
  - /api/affiliates/*  -> api:8081
  - /api/programs/*    -> api:8081
  - /api/users/*       -> api:8081
  - /api/keywords/*    -> api:8081
  - /api/surveys/*     -> api:8081
  - /api/admin/*       -> api:8081
  - /api/conversations/* -> conversation:8082
  - /api/messages/*    -> conversation:8082
  - /api/queue/*       -> conversation:8082
  - /api/webhooks/*    -> conversation:8082
  - /api/webchat/*     -> conversation:8082
  - /api/providers/*   -> conversation:8082
- Health + metrics endpoints

### Service: API (api.yaml) - Port 8081
- HTTP server on :8081
- Auth (JWT): login, register, OAuth simulation, token validation
- Platform CRUD:
  - affiliates: id, name, region, dataRetentionDays, encryptionKey, settings
  - programs: id, affiliateId, name, providers[], keywords[], settings
  - users: id, email, name, role(responder|supervisor|admin), affiliateId, status
  - keywords: id, programId, keyword, action, subProgram
  - surveys: id, programId, type(entry|exit), questions[]
- Persistence (SQLite)
- Middleware: CORS, request-id, rate-limiter, auth

### Service: Conversation (conversation.yaml) - Port 8082
- HTTP server on :8082
- Conversation state machine (see below)
- API endpoints:
  - POST /api/webhooks/twilio     - Inbound Twilio webhook
  - POST /api/webhooks/aws        - Inbound AWS webhook
  - POST /api/webhooks/partner    - Inbound partner webhook
  - POST /api/webchat/message     - Inbound webchat message
  - GET  /api/webchat/poll/:id    - Poll for new messages (webchat)
  - GET  /api/conversations       - List conversations (filtered by user role)
  - GET  /api/conversations/:id   - Get conversation detail
  - POST /api/conversations/:id/messages  - Send message (responder)
  - POST /api/conversations/:id/assign    - Assign to responder
  - POST /api/conversations/:id/transfer  - Transfer to another responder
  - POST /api/conversations/:id/escalate  - Escalate (medical/police)
  - POST /api/conversations/:id/wrap-up   - Begin wrap-up
  - POST /api/conversations/:id/close     - Close conversation
  - POST /api/conversations/:id/follow-up - Schedule follow-up
  - POST /api/conversations/:id/tag       - Add tag/category
  - POST /api/conversations/:id/survey    - Submit survey response
  - GET  /api/conversations/:id/summary   - AI-generated summary
  - GET  /api/queue                       - Queue status by program
  - GET  /api/queue/health                - Queue health metrics
  - GET  /api/providers                   - List configured providers
- Dynamic components for all business logic
- Processing steps for state transitions
- Kafka messaging for events
- Persistence (SQLite)

## State Machine: conversation-lifecycle

### States
| State | Description | isFinal | isError |
|-------|-------------|---------|---------|
| new | Initial message received, not yet queued | false | false |
| queued | In queue waiting for responder | false | false |
| assigned | Responder assigned, not yet active | false | false |
| active | Active conversation in progress | false | false |
| transferred | Being transferred to another responder | false | false |
| escalated_medical | Escalated to medical professional | false | false |
| escalated_police | Escalated to police/emergency services | false | false |
| wrap_up | Responder wrapping up, exit survey may be active | false | false |
| follow_up_scheduled | Follow-up scheduled for later | false | false |
| follow_up_active | Follow-up conversation in progress | false | false |
| closed | Conversation ended normally | true | false |
| expired | Timed out or retention policy applied | true | false |
| failed | Processing error | true | true |

### Transitions
| Transition | From | To | autoTransform | Hook |
|-----------|------|-----|---------------|------|
| route_message | new | queued | true | step-route-message |
| assign_responder | queued | assigned | false | step-assign |
| start_conversation | assigned | active | true | step-start-convo |
| send_entry_survey | active | active | false | step-survey |
| transfer_to_responder | active | transferred | false | step-transfer |
| accept_transfer | transferred | active | false | step-accept-transfer |
| escalate_to_medical | active | escalated_medical | false | step-escalate |
| escalate_to_police | active | escalated_police | false | step-escalate |
| resolve_escalation | escalated_medical | active | false | - |
| resolve_police_escalation | escalated_police | active | false | - |
| begin_wrap_up | active | wrap_up | false | step-wrap-up |
| schedule_follow_up | wrap_up | follow_up_scheduled | false | step-schedule-followup |
| trigger_follow_up | follow_up_scheduled | follow_up_active | false | step-trigger-followup |
| close_from_wrap_up | wrap_up | closed | false | step-close |
| close_from_followup | follow_up_active | closed | false | step-close |
| close_from_active | active | closed | false | step-close |
| timeout_expire | queued | expired | false | - |

### Hooks (processing steps triggered on transitions)
- step-route-message: keyword_matcher + conversation_router (route to correct program queue)
- step-assign: Assigns responder, sends AI summary if transfer
- step-start-convo: Logs conversation start, optional entry survey
- step-survey: survey_engine (entry or exit)
- step-transfer: Transfer logic, AI summary generation
- step-accept-transfer: Notify previous responder
- step-escalate: escalation_handler (contact medical/police)
- step-wrap-up: Begin exit survey, generate tags
- step-schedule-followup: followup_scheduler
- step-trigger-followup: Send follow-up message to texter
- step-close: data_retention check, final cleanup

## Dynamic Components

All in example/chat-platform/components/. Each follows the pattern:
- `//go:build ignore`
- `package component`
- Implements Name(), Init(), Start(), Stop(), Execute(ctx, params) (map, error)

### 1. twilio_provider.go
Simulates Twilio SMS. Execute receives {action, to, body, from, webhookData}.
- action "send": Returns {sid, status:"sent", provider:"twilio"}
- action "receive": Parses Twilio webhook format, returns {from, to, body, sid}

### 2. aws_provider.go
Simulates AWS SNS/Pinpoint SMS. Execute receives {action, phoneNumber, message}.
- action "send": Returns {messageId, status:"sent", provider:"aws"}
- action "receive": Parses SNS notification format

### 3. partner_provider.go
Simulates partner webhook provider. Execute receives {action, endpoint, payload}.
- action "send": Simulates API call to partner, returns {requestId, status}
- action "receive": Parses partner webhook format

### 4. webchat_handler.go
Handles webchat messages. Execute receives {action, sessionId, message, metadata}.
- action "receive": Creates/updates webchat session, returns {sessionId, message}
- action "send": Queues message for webchat polling endpoint

### 5. conversation_router.go
Routes inbound messages to correct program/queue. Execute receives {from, body, provider, affiliateId, programId}.
- Determines program from phone number mapping or keyword
- Creates or finds existing conversation
- Returns {conversationId, programId, affiliateId, isNew, queuePosition}

### 6. keyword_matcher.go
Matches texter keywords to programs. Execute receives {body, affiliateId}.
- Checks first word against keyword database
- Returns {matched, programId, keyword, subProgram}

### 7. pii_encryptor.go
Field-level AES-256 encryption. Execute receives {action, data, fields[], key}.
- action "encrypt": Encrypts specified fields in data map
- action "decrypt": Decrypts specified fields
- Fields: phoneNumber, name, messageBody, address

### 8. survey_engine.go
Manages entry/exit surveys. Execute receives {action, surveyId, conversationId, responses}.
- action "get_survey": Returns survey questions for program
- action "submit": Validates and stores responses
- Returns {surveyId, status, completedAt}

### 9. followup_scheduler.go
Schedules follow-up check-ins. Execute receives {conversationId, scheduledTime, message, programId}.
- Creates follow-up record with scheduled time
- Returns {followUpId, scheduledFor, status:"scheduled"}

### 10. escalation_handler.go
Handles escalation to medical/police. Execute receives {type, conversationId, texterInfo, urgency, location}.
- type "medical": Simulates contacting medical professional
- type "police": Simulates contacting local police with texter location
- Returns {escalationId, status, contactedService, referenceNumber}

### 11. ai_summarizer.go
Generates AI conversation summaries. Execute receives {conversationId, messages[], context}.
- Analyzes message history (simulated AI)
- Returns {summary, keyTopics[], riskLevel, sentiment, suggestedTags[]}

### 12. risk_tagger.go
Tags conversations for risk assessment. Execute receives {conversationId, messages[], currentTags[]}.
- Analyzes for risk indicators (keywords, patterns)
- Returns {tags[], riskLevel:"low|medium|high|critical", alerts[]}
- Risk categories: self-harm, substance-abuse, domestic-violence, suicidal-ideation, crisis-immediate

### 13. data_retention.go
Enforces data retention policies. Execute receives {action, affiliateId, programId, retentionDays}.
- action "check": Returns conversations eligible for deletion
- action "enforce": Marks expired conversations, anonymizes PII
- Returns {processed, anonymized, deleted}

### 14. message_processor.go
Central message processing. Execute receives {direction, conversationId, content, provider, from, to}.
- direction "inbound": Encrypts PII, stores message, publishes event
- direction "outbound": Decrypts PII if needed, routes to provider, stores delivery status
- Returns {messageId, status, encryptedFields[]}

### 15. notification_sender.go
System notifications. Execute receives {type, recipients[], data}.
- type "queue_alert": Queue depth exceeded threshold
- type "escalation": Escalation notification to supervisor
- type "transfer": Transfer notification to receiving responder
- Returns {sent, notificationId}

## Kafka Topics
- conversation.created
- conversation.assigned
- conversation.message.inbound
- conversation.message.outbound
- conversation.transferred
- conversation.escalated
- conversation.closed
- conversation.followup.scheduled
- conversation.followup.triggered
- conversation.survey.completed
- conversation.tag.updated

## Seed Data

### seed/affiliates.json
```json
[
  {"id":"aff-001","data":{"name":"Crisis Support International","region":"US-East","dataRetentionDays":365,"encryptionKeyId":"key-001","contactEmail":"admin@csi.org","status":"active"},"state":"active"},
  {"id":"aff-002","data":{"name":"Youth Mental Health Alliance","region":"US-West","dataRetentionDays":180,"encryptionKeyId":"key-002","contactEmail":"admin@ymha.org","status":"active"},"state":"active"},
  {"id":"aff-003","data":{"name":"Global Wellness Network","region":"EU-West","dataRetentionDays":90,"encryptionKeyId":"key-003","contactEmail":"admin@gwn.eu","status":"active"},"state":"active"}
]
```

### seed/programs.json
```json
[
  {"id":"prog-001","data":{"name":"Crisis Text Line","affiliateId":"aff-001","providers":["twilio","webchat"],"shortCode":"741741","description":"24/7 crisis support via text","status":"active","settings":{"maxConcurrentPerResponder":3,"queueAlertThreshold":10,"entrySurveyId":"survey-001","exitSurveyId":"survey-002"}},"state":"active"},
  {"id":"prog-002","data":{"name":"Teen Support Line","affiliateId":"aff-001","providers":["twilio"],"shortCode":"741742","description":"Dedicated teen mental health support","status":"active","settings":{"maxConcurrentPerResponder":2,"queueAlertThreshold":5,"entrySurveyId":"survey-003","exitSurveyId":"survey-004"}},"state":"active"},
  {"id":"prog-003","data":{"name":"Wellness Chat","affiliateId":"aff-002","providers":["webchat","aws"],"shortCode":"","description":"Web-based wellness support","status":"active","settings":{"maxConcurrentPerResponder":4,"queueAlertThreshold":15,"entrySurveyId":"","exitSurveyId":"survey-005"}},"state":"active"},
  {"id":"prog-004","data":{"name":"Partner Assist","affiliateId":"aff-003","providers":["partner"],"shortCode":"","description":"Partner-integrated support service","status":"active","settings":{"maxConcurrentPerResponder":2,"queueAlertThreshold":8}},"state":"active"}
]
```

### seed/users.json
```json
[
  {"id":"user-001","data":{"email":"responder1@example.com","name":"Alex Rivera","role":"responder","affiliateId":"aff-001","programIds":["prog-001","prog-002"],"status":"active","maxConcurrent":3,"password":"demo123"},"state":"active"},
  {"id":"user-002","data":{"email":"responder2@example.com","name":"Jordan Chen","role":"responder","affiliateId":"aff-001","programIds":["prog-001"],"status":"active","maxConcurrent":3,"password":"demo123"},"state":"active"},
  {"id":"user-003","data":{"email":"responder3@example.com","name":"Sam Okafor","role":"responder","affiliateId":"aff-002","programIds":["prog-003"],"status":"active","maxConcurrent":4,"password":"demo123"},"state":"active"},
  {"id":"user-004","data":{"email":"supervisor1@example.com","name":"Dr. Maria Santos","role":"supervisor","affiliateId":"aff-001","programIds":["prog-001","prog-002"],"status":"active","password":"demo123"},"state":"active"},
  {"id":"user-005","data":{"email":"supervisor2@example.com","name":"Taylor Kim","role":"supervisor","affiliateId":"aff-002","programIds":["prog-003"],"status":"active","password":"demo123"},"state":"active"},
  {"id":"user-006","data":{"email":"admin@example.com","name":"Admin User","role":"admin","affiliateId":"aff-001","programIds":[],"status":"active","password":"demo123"},"state":"active"}
]
```

### seed/keywords.json
```json
[
  {"id":"kw-001","data":{"programId":"prog-001","keyword":"HELLO","action":"route","subProgram":"general","response":"You've reached Crisis Support. A counselor will be with you shortly."},"state":"active"},
  {"id":"kw-002","data":{"programId":"prog-001","keyword":"HELP","action":"route","subProgram":"general","response":"Help is here. You'll be connected to a counselor shortly."},"state":"active"},
  {"id":"kw-003","data":{"programId":"prog-002","keyword":"TEEN","action":"route","subProgram":"teen-support","response":"Welcome to Teen Support. We're here for you."},"state":"active"},
  {"id":"kw-004","data":{"programId":"prog-001","keyword":"CRISIS","action":"route_priority","subProgram":"crisis-immediate","response":"We hear you. A counselor will be with you right away."},"state":"active"},
  {"id":"kw-005","data":{"programId":"prog-003","keyword":"WELLNESS","action":"route","subProgram":"general","response":"Welcome to Wellness Chat. How can we support you today?"},"state":"active"}
]
```

### seed/surveys.json
```json
[
  {"id":"survey-001","data":{"programId":"prog-001","type":"entry","title":"Initial Check-in","questions":[{"id":"q1","text":"On a scale of 1-5, how are you feeling right now?","type":"scale","min":1,"max":5},{"id":"q2","text":"What brings you here today?","type":"text"}]},"state":"active"},
  {"id":"survey-002","data":{"programId":"prog-001","type":"exit","title":"Session Feedback","questions":[{"id":"q1","text":"On a scale of 1-5, how are you feeling now?","type":"scale","min":1,"max":5},{"id":"q2","text":"Did you find this session helpful?","type":"choice","options":["Yes","Somewhat","No"]},{"id":"q3","text":"Any additional feedback?","type":"text"}]},"state":"active"},
  {"id":"survey-003","data":{"programId":"prog-002","type":"entry","title":"Teen Check-in","questions":[{"id":"q1","text":"How would you rate your mood today?","type":"scale","min":1,"max":5},{"id":"q2","text":"Is there something specific on your mind?","type":"text"}]},"state":"active"},
  {"id":"survey-004","data":{"programId":"prog-002","type":"exit","title":"Teen Session Feedback","questions":[{"id":"q1","text":"How are you feeling after our chat?","type":"scale","min":1,"max":5},{"id":"q2","text":"Would you reach out again?","type":"choice","options":["Definitely","Maybe","Probably not"]}]},"state":"active"},
  {"id":"survey-005","data":{"programId":"prog-003","type":"exit","title":"Wellness Session Review","questions":[{"id":"q1","text":"Rate your experience (1-5)","type":"scale","min":1,"max":5},{"id":"q2","text":"What could we improve?","type":"text"}]},"state":"active"}
]
```

## SPA Structure

Hash-based router (vanilla JS, no framework). All in spa/ directory.

### Pages/Views
1. **#/login** - Login form (email + password), role-based redirect
2. **#/responder** - Responder dashboard: active conversations list, queue count, pick from queue
3. **#/responder/chat/:id** - Active chat: message thread, send input, texter info sidebar, actions (transfer, escalate, tag, wrap-up, survey)
4. **#/supervisor** - Supervisor overview: responder list with status, conversation counts, queue health
5. **#/supervisor/responder/:id** - Responder detail: their active conversations
6. **#/supervisor/chat/:id** - Read-only chat view with AI summary
7. **#/queue** - Queue health: per-program queue depth, wait times, alerts
8. **#/admin/affiliates** - Affiliate CRUD
9. **#/admin/programs** - Program CRUD with provider config
10. **#/admin/users** - User management
11. **#/admin/keywords** - Keyword routing rules
12. **#/admin/surveys** - Survey template editor
13. **/webchat/widget.html** - Standalone webchat widget (separate HTML page, not hash-routed)

### SPA Files
- spa/index.html - Main app shell
- spa/styles.css - Full stylesheet (dark theme, chat-optimized)
- spa/js/api.js - HTTP client with Bearer auth
- spa/js/app.js - Hash router, role-based navigation
- spa/js/auth.js - Login/logout, token management
- spa/js/responder.js - Responder dashboard + chat view
- spa/js/supervisor.js - Supervisor dashboard + oversight views
- spa/js/admin.js - All admin CRUD views
- spa/js/queue.js - Queue health dashboard
- spa/js/components.js - Shared UI components (nav, modals, toasts)
- spa/js/chat.js - Chat UI component (message thread, input, polling)
- spa/js/webchat-client.js - Webchat widget client logic
- spa/webchat/widget.html - Standalone webchat widget page

## Docker Compose

Services: gateway, api, conversation, kafka, prometheus, grafana
Profile: distributed (all services)

Environment variables:
- JWT_SECRET (shared between api and conversation)
- ENCRYPTION_KEY (for PII)
- KAFKA_BROKERS (kafka:9092)

Volumes: api-data, conversation-data

## Mental Health Best Practices

The platform should reflect:
- Non-judgmental language in all auto-responses
- Warm handoff procedures (never leave texter unattended)
- Risk assessment at multiple points
- Mandatory safety planning for high-risk conversations
- Supervisor escalation for critical situations
- Follow-up as standard practice, not exception
- Responder wellness checks and load balancing
- Data privacy (PII encryption, retention policies)
- Cultural sensitivity (multi-affiliate, multi-region)
