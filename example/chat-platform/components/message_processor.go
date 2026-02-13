//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

func Name() string {
	return "message-processor"
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

// piiFieldNames lists fields that should be flagged for encryption.
var piiFieldNames = []string{"phoneNumber", "name", "address"}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	direction, _ := params["direction"].(string)
	conversationId, _ := params["conversationId"].(string)
	content, _ := params["content"].(string)
	provider, _ := params["provider"].(string)
	from, _ := params["from"].(string)
	to, _ := params["to"].(string)

	// Simulate processing delay (30-100ms)
	delay := time.Duration(30+r.Intn(70)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	messageId := fmt.Sprintf("msg-%d", r.Int63())
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// System event — no direction specified (e.g., start_conversation transition)
	if direction == "" {
		transitionId, _ := params["transitionId"].(string)
		return map[string]interface{}{
			"messageId":      messageId,
			"conversationId": conversationId,
			"direction":      "system",
			"status":         "processed",
			"transition":     transitionId,
			"timestamp":      timestamp,
			"event":          "conversation.system",
		}, nil
	}

	switch direction {
	case "inbound":
		if from == "" || content == "" {
			return nil, fmt.Errorf("inbound requires 'from' and 'content' parameters")
		}
		// Identify which PII fields are present and need encryption
		encryptedFields := make([]interface{}, 0)
		encryptedFields = append(encryptedFields, "phoneNumber")
		if content != "" {
			encryptedFields = append(encryptedFields, "messageBody")
		}
		return map[string]interface{}{
			"messageId":       messageId,
			"conversationId":  conversationId,
			"direction":       "inbound",
			"from":            from,
			"to":              to,
			"content":         content,
			"provider":        provider,
			"status":          "processed",
			"encryptedFields": encryptedFields,
			"timestamp":       timestamp,
			"event":           "conversation.message.inbound",
		}, nil

	case "outbound":
		if to == "" || content == "" {
			return nil, fmt.Errorf("outbound requires 'to' and 'content' parameters")
		}
		// 95% delivery success simulation
		status := "sent"
		if r.Float64() > 0.95 {
			status = "delivery_pending"
		}
		return map[string]interface{}{
			"messageId":      messageId,
			"conversationId": conversationId,
			"direction":      "outbound",
			"from":           from,
			"to":             to,
			"content":        content,
			"provider":       provider,
			"status":         status,
			"timestamp":      timestamp,
			"event":          "conversation.message.outbound",
		}, nil

	default:
		return nil, fmt.Errorf("unknown direction: %s (expected 'inbound' or 'outbound')", direction)
	}
}
