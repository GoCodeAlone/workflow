package alerts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/ai/classifier"
	"github.com/GoCodeAlone/workflow/ai/sentiment"
)

// AlertType represents the type of supervisor alert.
type AlertType string

const (
	AlertRiskEscalation    AlertType = "risk_escalation"
	AlertWorkloadImbalance AlertType = "workload_imbalance"
	AlertSentimentDrop     AlertType = "sentiment_drop"
	AlertLongWaitTime      AlertType = "long_wait_time"
)

// Severity represents the urgency of an alert.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Alert represents a supervisor alert.
type Alert struct {
	ID             string         `json:"id"`
	Type           AlertType      `json:"type"`
	Severity       Severity       `json:"severity"`
	ConversationID string         `json:"conversationId,omitempty"`
	CounselorID    string         `json:"counselorId,omitempty"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	CreatedAt      time.Time      `json:"createdAt"`
	AcknowledgedAt *time.Time     `json:"acknowledgedAt,omitempty"`
	ResolvedAt     *time.Time     `json:"resolvedAt,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// CounselorWorkload represents a counselor's current workload.
type CounselorWorkload struct {
	CounselorID         string        `json:"counselorId"`
	ActiveConversations int           `json:"activeConversations"`
	AverageWaitTime     time.Duration `json:"averageWaitTime"`
}

// AlertRule defines a rule for generating alerts.
type AlertRule struct {
	Type     AlertType
	Evaluate func(ctx *EvalContext) *Alert
}

// EvalContext provides data to alert rules during evaluation.
type EvalContext struct {
	ConversationID string
	CounselorID    string
	Classification *classifier.Classification
	Sentiment      *sentiment.Trend
	Workloads      []CounselorWorkload
	WaitTime       time.Duration
	Timestamp      time.Time
}

// LLMProvider abstracts the AI backend for enhanced alert detection.
type LLMProvider interface {
	EvaluateRisk(ctx context.Context, systemPrompt, userPrompt string) (*Alert, error)
}

// AlertEngine generates and manages supervisor alerts using a hybrid
// rule-based + AI approach.
type AlertEngine struct {
	provider  LLMProvider
	rules     []AlertRule
	alerts    []Alert
	mu        sync.RWMutex
	nextID    int
	maxAlerts int
	onAlert   func(Alert)
}

// Config holds configuration for the AlertEngine.
type Config struct {
	MaxAlerts                int
	MaxConversationsPerAgent int
	MaxWaitTime              time.Duration
	OnAlert                  func(Alert)
}

// DefaultConfig returns default alert engine configuration.
func DefaultConfig() Config {
	return Config{
		MaxAlerts:                1000,
		MaxConversationsPerAgent: 5,
		MaxWaitTime:              5 * time.Minute,
	}
}

// NewAlertEngine creates a new AlertEngine.
// If provider is nil, only rule-based detection is used.
func NewAlertEngine(provider LLMProvider, cfg Config) *AlertEngine {
	if cfg.MaxAlerts == 0 {
		cfg.MaxAlerts = 1000
	}
	if cfg.MaxConversationsPerAgent == 0 {
		cfg.MaxConversationsPerAgent = 5
	}
	if cfg.MaxWaitTime == 0 {
		cfg.MaxWaitTime = 5 * time.Minute
	}

	engine := &AlertEngine{
		provider:  provider,
		alerts:    make([]Alert, 0),
		maxAlerts: cfg.MaxAlerts,
		onAlert:   cfg.OnAlert,
	}

	// Register default rules
	engine.rules = defaultRules(cfg)

	return engine
}

// Evaluate runs all alert rules against the given context and returns any new alerts.
func (e *AlertEngine) Evaluate(ctx context.Context, evalCtx *EvalContext) []Alert {
	if evalCtx == nil {
		return nil
	}
	if evalCtx.Timestamp.IsZero() {
		evalCtx.Timestamp = time.Now().UTC()
	}

	var newAlerts []Alert

	// Run rule-based evaluation
	for _, rule := range e.rules {
		alert := rule.Evaluate(evalCtx)
		if alert != nil {
			e.mu.Lock()
			e.nextID++
			alert.ID = fmt.Sprintf("alert-%d", e.nextID)
			alert.CreatedAt = evalCtx.Timestamp
			e.alerts = append(e.alerts, *alert)

			// Trim old alerts if over limit
			if len(e.alerts) > e.maxAlerts {
				e.alerts = e.alerts[len(e.alerts)-e.maxAlerts:]
			}
			e.mu.Unlock()

			newAlerts = append(newAlerts, *alert)

			if e.onAlert != nil {
				e.onAlert(*alert)
			}
		}
	}

	return newAlerts
}

// EvaluateRiskEscalation checks for risk escalation in a conversation.
func (e *AlertEngine) EvaluateRiskEscalation(ctx context.Context, conversationID string, classification *classifier.Classification) []Alert {
	return e.Evaluate(ctx, &EvalContext{
		ConversationID: conversationID,
		Classification: classification,
		Timestamp:      time.Now().UTC(),
	})
}

// EvaluateSentimentDrop checks for sentiment drops in a conversation.
func (e *AlertEngine) EvaluateSentimentDrop(ctx context.Context, conversationID string, trend *sentiment.Trend) []Alert {
	return e.Evaluate(ctx, &EvalContext{
		ConversationID: conversationID,
		Sentiment:      trend,
		Timestamp:      time.Now().UTC(),
	})
}

// EvaluateWorkload checks workload distribution across counselors.
func (e *AlertEngine) EvaluateWorkload(ctx context.Context, workloads []CounselorWorkload) []Alert {
	return e.Evaluate(ctx, &EvalContext{
		Workloads: workloads,
		Timestamp: time.Now().UTC(),
	})
}

// EvaluateWaitTime checks conversation wait times.
func (e *AlertEngine) EvaluateWaitTime(ctx context.Context, conversationID string, waitTime time.Duration) []Alert {
	return e.Evaluate(ctx, &EvalContext{
		ConversationID: conversationID,
		WaitTime:       waitTime,
		Timestamp:      time.Now().UTC(),
	})
}

// GetAlerts returns alerts matching the given filter.
func (e *AlertEngine) GetAlerts(filter AlertFilter) []Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []Alert
	for i := range e.alerts {
		if filter.matches(e.alerts[i]) {
			result = append(result, e.alerts[i])
		}
	}
	return result
}

// AcknowledgeAlert marks an alert as acknowledged.
func (e *AlertEngine) AcknowledgeAlert(alertID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.alerts {
		if e.alerts[i].ID == alertID {
			now := time.Now().UTC()
			e.alerts[i].AcknowledgedAt = &now
			return nil
		}
	}
	return fmt.Errorf("alert %q not found", alertID)
}

// ResolveAlert marks an alert as resolved.
func (e *AlertEngine) ResolveAlert(alertID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.alerts {
		if e.alerts[i].ID == alertID {
			now := time.Now().UTC()
			e.alerts[i].ResolvedAt = &now
			return nil
		}
	}
	return fmt.Errorf("alert %q not found", alertID)
}

// AlertFilter defines criteria for filtering alerts.
type AlertFilter struct {
	Type           *AlertType `json:"type,omitempty"`
	Severity       *Severity  `json:"severity,omitempty"`
	ConversationID string     `json:"conversationId,omitempty"`
	Unresolved     bool       `json:"unresolved,omitempty"`
	Since          *time.Time `json:"since,omitempty"`
	Limit          int        `json:"limit,omitempty"`
}

func (f AlertFilter) matches(a Alert) bool {
	if f.Type != nil && a.Type != *f.Type {
		return false
	}
	if f.Severity != nil && a.Severity != *f.Severity {
		return false
	}
	if f.ConversationID != "" && a.ConversationID != f.ConversationID {
		return false
	}
	if f.Unresolved && a.ResolvedAt != nil {
		return false
	}
	if f.Since != nil && a.CreatedAt.Before(*f.Since) {
		return false
	}
	return true
}

// defaultRules returns the built-in alert rules.
func defaultRules(cfg Config) []AlertRule {
	return []AlertRule{
		riskEscalationRule(),
		workloadImbalanceRule(cfg.MaxConversationsPerAgent),
		sentimentDropRule(),
		longWaitTimeRule(cfg.MaxWaitTime),
	}
}

func riskEscalationRule() AlertRule {
	return AlertRule{
		Type: AlertRiskEscalation,
		Evaluate: func(ctx *EvalContext) *Alert {
			if ctx.Classification == nil {
				return nil
			}

			if ctx.Classification.Category != classifier.CategoryCrisis {
				return nil
			}

			severity := SeverityHigh
			if ctx.Classification.Priority == 1 {
				severity = SeverityCritical
			}

			return &Alert{
				Type:           AlertRiskEscalation,
				Severity:       severity,
				ConversationID: ctx.ConversationID,
				CounselorID:    ctx.CounselorID,
				Title:          "Risk Escalation Detected",
				Description: fmt.Sprintf("Conversation classified as crisis (subcategory: %s, priority: %d, confidence: %.2f)",
					ctx.Classification.Subcategory, ctx.Classification.Priority, ctx.Classification.Confidence),
				Metadata: map[string]any{
					"category":    string(ctx.Classification.Category),
					"subcategory": ctx.Classification.Subcategory,
					"priority":    ctx.Classification.Priority,
					"confidence":  ctx.Classification.Confidence,
				},
			}
		},
	}
}

func workloadImbalanceRule(maxConversations int) AlertRule {
	return AlertRule{
		Type: AlertWorkloadImbalance,
		Evaluate: func(ctx *EvalContext) *Alert {
			if len(ctx.Workloads) == 0 {
				return nil
			}

			var overloaded []string
			maxActive := 0
			minActive := int(^uint(0) >> 1) // max int
			for _, w := range ctx.Workloads {
				if w.ActiveConversations > maxActive {
					maxActive = w.ActiveConversations
				}
				if w.ActiveConversations < minActive {
					minActive = w.ActiveConversations
				}
				if w.ActiveConversations > maxConversations {
					overloaded = append(overloaded, w.CounselorID)
				}
			}

			if len(overloaded) == 0 {
				return nil
			}

			severity := SeverityMedium
			if maxActive > maxConversations*2 {
				severity = SeverityHigh
			}

			return &Alert{
				Type:     AlertWorkloadImbalance,
				Severity: severity,
				Title:    "Workload Imbalance Detected",
				Description: fmt.Sprintf("%d counselor(s) over capacity (max: %d conversations). Range: %d-%d active.",
					len(overloaded), maxConversations, minActive, maxActive),
				Metadata: map[string]any{
					"overloadedCounselors": overloaded,
					"maxActive":            maxActive,
					"minActive":            minActive,
					"threshold":            maxConversations,
				},
			}
		},
	}
}

func sentimentDropRule() AlertRule {
	return AlertRule{
		Type: AlertSentimentDrop,
		Evaluate: func(ctx *EvalContext) *Alert {
			if ctx.Sentiment == nil || !ctx.Sentiment.SharpDrop {
				return nil
			}

			severity := SeverityMedium
			if ctx.Sentiment.CurrentScore <= sentiment.Score(-0.7) {
				severity = SeverityHigh
			}

			var dropAt string
			if ctx.Sentiment.SharpDropAt != nil {
				dropAt = ctx.Sentiment.SharpDropAt.Format(time.RFC3339)
			}

			return &Alert{
				Type:           AlertSentimentDrop,
				Severity:       severity,
				ConversationID: ctx.ConversationID,
				CounselorID:    ctx.CounselorID,
				Title:          "Sharp Sentiment Drop Detected",
				Description: fmt.Sprintf("Sentiment dropped sharply in conversation (current: %.2f, average: %.2f, direction: %s)",
					ctx.Sentiment.CurrentScore, ctx.Sentiment.AverageScore, ctx.Sentiment.Direction),
				Metadata: map[string]any{
					"currentScore": float64(ctx.Sentiment.CurrentScore),
					"averageScore": float64(ctx.Sentiment.AverageScore),
					"direction":    ctx.Sentiment.Direction,
					"sharpDropAt":  dropAt,
				},
			}
		},
	}
}

func longWaitTimeRule(maxWait time.Duration) AlertRule {
	return AlertRule{
		Type: AlertLongWaitTime,
		Evaluate: func(ctx *EvalContext) *Alert {
			if ctx.WaitTime == 0 || ctx.WaitTime < maxWait {
				return nil
			}

			severity := SeverityMedium
			if ctx.WaitTime > maxWait*2 {
				severity = SeverityHigh
			}
			if ctx.WaitTime > maxWait*4 {
				severity = SeverityCritical
			}

			return &Alert{
				Type:           AlertLongWaitTime,
				Severity:       severity,
				ConversationID: ctx.ConversationID,
				Title:          "Long Wait Time",
				Description: fmt.Sprintf("Conversation waiting for %s (threshold: %s)",
					ctx.WaitTime.Round(time.Second), maxWait.Round(time.Second)),
				Metadata: map[string]any{
					"waitTime":  ctx.WaitTime.String(),
					"threshold": maxWait.String(),
				},
			}
		},
	}
}
