# Mental Health Platform Best Practices

This document outlines the clinical and operational principles embedded in the Chat Platform's design. Every technical decision reflects a commitment to safe, effective, and compassionate mental health support.

---

## Platform Design Principles

1. **Safety first**: Automated risk assessment at multiple points in the conversation lifecycle. High-risk indicators trigger immediate alerts.
2. **No texter left behind**: Warm handoff procedures ensure texters are never left unattended during transfers, escalations, or responder shift changes.
3. **Non-judgmental communication**: All auto-responses and system messages use non-judgmental, supportive language. No diagnostic labels, no assumptions.
4. **Follow-up as standard practice**: Follow-up check-ins are a core feature, not an exception. Every closed conversation can have a scheduled follow-up.
5. **Responder wellness**: The platform monitors responder load and enforces maximum concurrent conversation limits to prevent burnout.
6. **Privacy by design**: PII is encrypted at the field level before storage. Data retention policies are enforced automatically per affiliate.
7. **Cultural sensitivity**: Multi-affiliate, multi-region architecture supports localized programs with region-specific configurations.

---

## Risk Assessment

### How the Risk Tagger Works

The `risk_tagger` component analyzes conversation messages for risk indicators at multiple points:

- **On message receipt**: Every inbound texter message is scanned for risk keywords and patterns
- **On tag update**: When a responder manually tags a conversation, the risk level is recalculated
- **On wrap-up**: Final risk assessment before closing, ensuring nothing was missed

### Risk Categories

| Category | Indicators | Severity |
|----------|-----------|----------|
| `self-harm` | References to self-injury, cutting, physical harm to self | High |
| `suicidal-ideation` | Expressions of hopelessness, wanting to end life, suicide plans | Critical |
| `substance-abuse` | References to drug/alcohol misuse, overdose risk | Medium-High |
| `domestic-violence` | References to abuse, unsafe home environment, threats | High |
| `crisis-immediate` | Immediate danger, active self-harm, imminent threat | Critical |

### Risk Levels

| Level | Response | System Action |
|-------|----------|---------------|
| `low` | Standard conversation | No additional action |
| `medium` | Heightened awareness | Tag applied, visible to responder |
| `high` | Active monitoring | Supervisor notified, conversation flagged |
| `critical` | Immediate response | Supervisor alerted, escalation recommended, conversation prioritized in queue |

### Risk Assessment Output

The risk tagger returns:
- `tags[]`: Applied risk category tags
- `riskLevel`: Overall risk level (low/medium/high/critical)
- `alerts[]`: Specific alert messages for the responder and supervisor

---

## Escalation Protocols

When a conversation requires intervention beyond the responder's scope, the platform supports two escalation types.

### Medical Escalation

**When to escalate**: Texter reports a medical emergency, active self-harm requiring medical attention, overdose, or physical injury.

**Protocol**:
1. Responder initiates escalation via `POST /api/conversations/:id/escalate` with `type: "medical"`
2. The `escalation_handler` component contacts medical services (simulated in demo mode)
3. The supervisor is notified immediately via the `notification_sender`
4. A reference number is generated and attached to the conversation
5. The conversation transitions to `escalated_medical` state
6. The responder remains connected with the texter until medical help is confirmed
7. Once resolved, the conversation transitions back to `active` via `resolve_escalation`

**Information provided to medical services**:
- Urgency level (low/medium/high/critical)
- Texter location (if provided)
- Brief situation description
- Conversation reference number

### Police/Emergency Services Escalation

**When to escalate**: Imminent threat to life, active violence, missing person, or texter in immediate physical danger.

**Protocol**:
1. Responder initiates escalation via `POST /api/conversations/:id/escalate` with `type: "police"`
2. The `escalation_handler` contacts local emergency services with texter location
3. The supervisor is notified immediately
4. A case reference number is generated
5. The conversation transitions to `escalated_police` state
6. The responder keeps the texter engaged and calm while help is dispatched
7. Once resolved, the conversation transitions back to `active` via `resolve_police_escalation`

**Information provided to emergency services**:
- Texter location (required for police escalation)
- Nature of threat
- Urgency level
- Conversation reference number

### Escalation Audit Trail

All escalations are:
- Logged with full detail (type, urgency, outcome, reference number)
- Published to the `conversation.escalated` Kafka topic
- Visible to supervisors in the oversight dashboard
- Included in conversation summary

---

## Follow-up Procedures

Follow-up is a core part of the conversation lifecycle, not an afterthought.

### Scheduling

During wrap-up, the responder can schedule a follow-up check-in:

```
POST /api/conversations/:id/follow-up
{
  "scheduledTime": "2026-02-14T10:00:00Z",
  "message": "Hi, just checking in. How are you doing today?"
}
```

The `followup_scheduler` creates a record with the scheduled time.

### Triggering

When the scheduled time arrives, the `trigger_follow_up` transition fires:
1. The conversation transitions from `follow_up_scheduled` to `follow_up_active`
2. The follow-up message is sent to the texter via their original provider
3. If the texter responds, a new active conversation begins
4. If no response within the follow-up window, the conversation closes

### Follow-up Best Practices

- Schedule follow-ups for all medium and high-risk conversations
- Use warm, non-intrusive language ("checking in" rather than "following up")
- Allow the texter to opt out of follow-ups
- Follow-up timing should consider the texter's timezone and preferences

---

## Responder Wellness

### Concurrent Conversation Limits

Each responder has a configurable `maxConcurrent` limit:

| Program | Default Max | Rationale |
|---------|-------------|-----------|
| Crisis Text Line | 3 | High-intensity conversations require full attention |
| Teen Support Line | 2 | Teen conversations require additional care and patience |
| Wellness Chat | 4 | Lower-intensity wellness conversations |
| Partner Assist | 2 | Variable intensity, conservative default |

The platform enforces these limits when assigning conversations from the queue. A responder at their limit will not receive new assignments.

### Load Monitoring

The queue health endpoint (`GET /api/queue/health`) provides:
- Per-program queue depth
- Average wait time
- Responder availability (how many responders are below their max)
- Alert status when queue depth exceeds program thresholds

Supervisors monitor these metrics to:
- Identify responders who may be overwhelmed
- Redistribute load when needed
- Escalate staffing concerns

### Supervisor Oversight

Supervisors have read-only access to all conversations in their programs. This enables:
- Monitoring conversation quality
- Identifying responders who may need support
- Reviewing AI-generated summaries for concerning patterns
- Intervening through the escalation process when needed

---

## Data Privacy

### PII Encryption

The `pii_encryptor` component provides field-level AES-256 encryption for:

| Field | When Encrypted | When Decrypted |
|-------|---------------|----------------|
| `phoneNumber` | On inbound message receipt | When sending outbound message |
| `name` | On collection | When displayed to authorized responder |
| `messageBody` | On storage | When displayed in chat view |
| `address` | On collection (escalation) | When provided to emergency services |

Each affiliate has a unique encryption key, ensuring data isolation between organizations.

### Data Retention

The `data_retention` component enforces configurable retention policies:

| Affiliate | Retention Period |
|-----------|-----------------|
| Crisis Support International | 365 days |
| Youth Mental Health Alliance | 180 days |
| Global Wellness Network | 90 days |

When retention expires:
1. PII fields are anonymized (replaced with hashed values)
2. Message content is purged
3. Conversation metadata (state transitions, tags, duration) is preserved for aggregate reporting
4. The conversation is marked as `expired`

### GDPR Considerations

The platform design supports GDPR compliance through:

- **Right to erasure**: Data retention enforcement can be triggered on demand per texter
- **Data minimization**: Only necessary PII is collected; encryption reduces exposure
- **Purpose limitation**: Data is used solely for support delivery and quality assurance
- **Storage limitation**: Automatic retention enforcement per affiliate policy
- **Data portability**: Conversation data can be exported in JSON format
- **Consent**: Entry surveys can include consent questions before data collection

### Access Controls

- Responders see only conversations assigned to them
- Supervisors see conversations within their programs (read-only)
- Admins manage configuration but do not access conversation content
- All API access is authenticated via JWT with role-based authorization
- Webhook endpoints are public but validate provider-specific signatures

---

## Entry and Exit Surveys

### Purpose

Surveys serve two critical functions:
1. **Clinical measurement**: Track texter wellbeing before and after conversations to measure outcomes
2. **Quality improvement**: Collect feedback to improve responder training and platform features

### Entry Surveys

Presented when a conversation becomes active. Typical questions:

- "On a scale of 1-5, how are you feeling right now?" (scale)
- "What brings you here today?" (free text)
- "How would you rate your mood today?" (scale)

Entry surveys establish a baseline and help the responder understand the texter's state.

### Exit Surveys

Presented during wrap-up. Typical questions:

- "On a scale of 1-5, how are you feeling now?" (scale)
- "Did you find this session helpful?" (choice: Yes/Somewhat/No)
- "Any additional feedback?" (free text)
- "Would you reach out again?" (choice: Definitely/Maybe/Probably not)

### Outcomes Tracking

By comparing entry and exit survey scores, the platform can measure:
- Per-conversation mood improvement
- Program-level effectiveness
- Responder performance trends
- Aggregate outcome metrics for affiliates

---

## AI-Assisted Summaries

### Warm Handoffs

When a conversation is transferred between responders, the `ai_summarizer` generates a handoff summary containing:

- **Summary**: Brief narrative of the conversation so far
- **Key topics**: Main issues discussed
- **Risk level**: Current risk assessment
- **Sentiment**: Overall texter sentiment
- **Suggested tags**: Recommended conversation tags

This ensures the receiving responder has full context without requiring the texter to repeat themselves.

### Supervisor Oversight

Supervisors can view AI-generated summaries for any conversation in their programs. This enables efficient oversight without reading every message.

### Summary Generation

Summaries are generated:
- On transfer (automatic)
- On request via `GET /api/conversations/:id/summary`
- During wrap-up (for final records)

---

## Cultural Sensitivity

### Multi-Affiliate Architecture

The platform's multi-tenant design enables:
- Region-specific program configurations
- Localized auto-response messages per keyword
- Per-affiliate data retention policies reflecting local regulations
- Per-affiliate encryption keys ensuring data isolation

### Multi-Region Support

| Affiliate | Region | Considerations |
|-----------|--------|----------------|
| Crisis Support International | US-East | English language, US crisis protocols |
| Youth Mental Health Alliance | US-West | Youth-focused language, school-related keywords |
| Global Wellness Network | EU-West | GDPR compliance, 90-day retention, potential multilingual support |
| Partner Assist | Variable | Partner-specific protocols and integration |

### Language and Tone

All system-generated messages (auto-responses, surveys, follow-ups) are:
- Written in non-judgmental, supportive language
- Free of clinical jargon or diagnostic labels
- Culturally neutral while remaining warm and empathetic
- Configurable per program to support localization
