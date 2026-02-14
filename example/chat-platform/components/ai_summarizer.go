//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func Name() string {
	return "ai-summarizer"
}

func Contract() map[string]interface{} {
	return map[string]interface{}{
		"required_inputs": map[string]interface{}{},
		"optional_inputs": map[string]interface{}{
			"messages": map[string]interface{}{
				"type":        "slice",
				"description": "Array of message objects with 'body' fields to summarize. If empty, returns a placeholder summary.",
			},
			"transitionId": map[string]interface{}{
				"type":        "string",
				"description": "State machine transition ID for transition events",
			},
		},
		"outputs": map[string]interface{}{
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "Generated text summary of the conversation",
			},
			"keyTopics": map[string]interface{}{
				"type":        "slice",
				"description": "Detected topic categories",
			},
			"riskLevel": map[string]interface{}{
				"type":        "string",
				"description": "Assessed risk level (low, high, critical)",
			},
			"sentiment": map[string]interface{}{
				"type":        "string",
				"description": "Overall sentiment (negative, neutral, positive)",
			},
			"suggestedTags": map[string]interface{}{
				"type":        "slice",
				"description": "Suggested tags based on detected topics",
			},
			"messageCount": map[string]interface{}{
				"type":        "int",
				"description": "Number of messages processed",
			},
			"generatedAt": map[string]interface{}{
				"type":        "string",
				"description": "ISO 8601 timestamp of when the summary was generated",
			},
		},
	}
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

var topicKeywords = map[string][]string{
	"anxiety":         {"anxious", "worried", "panic", "nervous", "fear", "scared"},
	"depression":      {"sad", "hopeless", "depressed", "empty", "numb", "tired"},
	"relationships":   {"friend", "family", "partner", "boyfriend", "girlfriend", "parent"},
	"school":          {"school", "exam", "grade", "teacher", "homework", "college"},
	"self-harm":       {"cut", "hurt myself", "self-harm", "pain"},
	"suicidal":        {"suicide", "kill myself", "end it", "die", "not alive"},
	"substance-use":   {"drink", "drug", "alcohol", "high", "smoke", "substance"},
	"bullying":        {"bully", "picked on", "harassed", "mean"},
	"trauma":          {"abuse", "assault", "trauma", "ptsd", "nightmare"},
	"grief":           {"died", "lost", "death", "grief", "mourning", "funeral"},
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	messages, _ := params["messages"].([]interface{})

	// When called from a state machine transition without message data,
	// return a placeholder summary.
	if len(messages) == 0 {
		transitionId, _ := params["transitionId"].(string)
		return map[string]interface{}{
			"summary":      "Transition event processed",
			"transition":   transitionId,
			"keyTopics":    []interface{}{"general-support"},
			"riskLevel":    "low",
			"sentiment":    "neutral",
			"messageCount": 0,
			"generatedAt":  time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	// Simulate AI processing time (300-600ms)
	delay := time.Duration(300+r.Intn(300)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Build combined text from messages
	var allText strings.Builder
	msgCount := 0
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		body, _ := msg["body"].(string)
		if body != "" {
			allText.WriteString(body)
			allText.WriteString(" ")
			msgCount++
		}
	}
	combined := strings.ToLower(allText.String())

	// Detect topics
	topics := make([]interface{}, 0)
	for topic, keywords := range topicKeywords {
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				topics = append(topics, topic)
				break
			}
		}
	}
	if len(topics) == 0 {
		topics = append(topics, "general-support")
	}

	// Determine risk level
	riskLevel := "low"
	if containsAny(combined, topicKeywords["self-harm"]) {
		riskLevel = "high"
	}
	if containsAny(combined, topicKeywords["suicidal"]) {
		riskLevel = "critical"
	}

	// Determine sentiment
	sentiment := "neutral"
	negativeWords := []string{"sad", "angry", "scared", "hopeless", "hate", "terrible", "awful", "worst"}
	positiveWords := []string{"better", "good", "thanks", "helped", "grateful", "hopeful"}
	negCount := countMatches(combined, negativeWords)
	posCount := countMatches(combined, positiveWords)
	if negCount > posCount {
		sentiment = "negative"
	} else if posCount > negCount {
		sentiment = "positive"
	}

	// Build summary
	summary := fmt.Sprintf("Conversation with %d messages. ", msgCount)
	if len(topics) > 0 {
		topicStrs := make([]string, len(topics))
		for i, t := range topics {
			topicStrs[i] = fmt.Sprintf("%v", t)
		}
		summary += fmt.Sprintf("Key topics: %s. ", strings.Join(topicStrs, ", "))
	}
	summary += fmt.Sprintf("Overall sentiment: %s. Risk level: %s.", sentiment, riskLevel)

	suggestedTags := make([]interface{}, len(topics))
	copy(suggestedTags, topics)

	return map[string]interface{}{
		"summary":       summary,
		"keyTopics":     topics,
		"riskLevel":     riskLevel,
		"sentiment":     sentiment,
		"suggestedTags": suggestedTags,
		"messageCount":  msgCount,
		"generatedAt":   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func countMatches(text string, words []string) int {
	count := 0
	for _, w := range words {
		if strings.Contains(text, w) {
			count++
		}
	}
	return count
}
