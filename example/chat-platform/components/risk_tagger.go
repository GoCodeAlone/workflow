//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

var riskPatterns = map[string][]string{
	"self-harm":          {"cut myself", "hurt myself", "self-harm", "self harm", "burning myself", "hitting myself"},
	"suicidal-ideation":  {"kill myself", "suicide", "end my life", "not alive", "want to die", "better off dead", "no reason to live"},
	"crisis-immediate":   {"right now", "tonight", "plan to", "going to do it", "goodbye", "final"},
	"substance-abuse":    {"drinking", "drugs", "overdose", "alcohol", "pills", "high right now", "substance"},
	"domestic-violence":  {"hits me", "abuses me", "beats me", "violent", "domestic", "partner hurts"},
}

func Name() string {
	return "risk-tagger"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	messages, _ := params["messages"].([]interface{})
	if len(messages) == 0 {
		return nil, fmt.Errorf("missing required parameter: messages")
	}

	// Simulate analysis delay (50-150ms)
	delay := time.Duration(50+r.Intn(100)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Build combined message text
	var allText strings.Builder
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		body, _ := msg["body"].(string)
		if body != "" {
			allText.WriteString(strings.ToLower(body))
			allText.WriteString(" ")
		}
	}
	combined := allText.String()

	// Merge with existing tags
	existingTags, _ := params["currentTags"].([]interface{})
	tagSet := make(map[string]bool)
	for _, t := range existingTags {
		if s, ok := t.(string); ok {
			tagSet[s] = true
		}
	}

	// Scan for risk patterns
	alerts := make([]interface{}, 0)
	for category, patterns := range riskPatterns {
		for _, pattern := range patterns {
			if strings.Contains(combined, pattern) {
				tagSet[category] = true
				alerts = append(alerts, map[string]interface{}{
					"category":   category,
					"pattern":    pattern,
					"detectedAt": time.Now().UTC().Format(time.RFC3339),
				})
				break
			}
		}
	}

	tags := make([]interface{}, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}

	// Determine risk level from tags
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

	return map[string]interface{}{
		"tags":      tags,
		"riskLevel": riskLevel,
		"alerts":    alerts,
		"scannedAt": time.Now().UTC().Format(time.RFC3339),
	}, nil
}
