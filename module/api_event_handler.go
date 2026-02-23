package module

import (
	"encoding/json"
	"fmt"
	"strings"
)

// defaultRiskPatterns contains the built-in crisis-detection keyword patterns.
// These can be overridden per-handler via RESTAPIHandlerConfig.RiskPatterns or SetRiskPatterns.
var defaultRiskPatterns = map[string][]string{
	"self-harm":         {"cut myself", "cutting myself", "hurt myself", "hurting myself", "self-harm", "self harm", "burning myself", "hitting myself"},
	"suicidal-ideation": {"kill myself", "suicide", "end my life", "not alive", "want to die", "better off dead", "no reason to live", "dont want to be alive"},
	"crisis-immediate":  {"right now", "tonight", "plan to", "going to do it", "goodbye", "final"},
	"substance-abuse":   {"drinking", "drugs", "overdose", "alcohol", "pills", "high right now", "substance"},
	"domestic-violence": {"hits me", "abuses me", "beats me", "violent", "domestic", "partner hurts", "partner hits"},
}

// assessRiskLevel analyzes messages against the handler's risk patterns and returns
// the risk level and detected category tags.
func (h *RESTAPIHandler) assessRiskLevel(messages []any) (string, []string) {
	var allText strings.Builder
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"body", "Body", "content", "message"} {
			if body, ok := msg[field].(string); ok && body != "" {
				allText.WriteString(strings.ToLower(body))
				allText.WriteString(" ")
				break
			}
		}
	}
	combined := allText.String()
	if combined == "" {
		return "low", nil
	}

	patterns := h.riskPatterns
	if len(patterns) == 0 {
		patterns = defaultRiskPatterns
	}

	tagSet := make(map[string]bool)
	for category, pats := range patterns {
		for _, pattern := range pats {
			if strings.Contains(combined, pattern) {
				tagSet[category] = true
				break
			}
		}
	}

	riskLevel := "low"
	if tagSet["substance-abuse"] || tagSet["domestic-violence"] {
		riskLevel = "medium"
	}
	if tagSet["self-harm"] {
		riskLevel = "high"
	}
	if tagSet["suicidal-ideation"] {
		riskLevel = "high"
	}
	if tagSet["crisis-immediate"] {
		riskLevel = "critical"
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	return riskLevel, tags
}

// publishEvent publishes a resource event to the event broker if one is configured.
// The event is sent asynchronously (non-blocking) to avoid delaying HTTP responses.
func (h *RESTAPIHandler) publishEvent(payload map[string]any) {
	if h.eventBroker == nil {
		return
	}
	eventData, _ := json.Marshal(payload)
	go func() {
		if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
			fmt.Printf("Failed to publish event: %v\n", err)
		}
	}()
}
