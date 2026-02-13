//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

func Name() string {
	return "notification-sender"
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

	notifType, _ := params["type"].(string)
	if notifType == "" {
		return nil, fmt.Errorf("missing required parameter: type")
	}

	recipients, _ := params["recipients"].([]interface{})
	data, _ := params["data"].(map[string]interface{})

	// Simulate notification dispatch delay (20-80ms)
	delay := time.Duration(20+r.Intn(60)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	notificationId := fmt.Sprintf("notif-%d", r.Int63())
	timestamp := time.Now().UTC().Format(time.RFC3339)

	var message string
	var priority string

	switch notifType {
	case "queue_alert":
		programId := ""
		queueDepth := 0
		if data != nil {
			programId, _ = data["programId"].(string)
			depthF, _ := data["queueDepth"].(float64)
			queueDepth = int(depthF)
		}
		message = fmt.Sprintf("Queue alert: Program %s has %d conversations waiting", programId, queueDepth)
		priority = "high"

	case "escalation":
		escalationType := ""
		conversationId := ""
		if data != nil {
			escalationType, _ = data["escalationType"].(string)
			conversationId, _ = data["conversationId"].(string)
		}
		message = fmt.Sprintf("Escalation (%s) for conversation %s requires supervisor attention", escalationType, conversationId)
		priority = "critical"

	case "transfer":
		fromResponder := ""
		toResponder := ""
		if data != nil {
			fromResponder, _ = data["fromResponder"].(string)
			toResponder, _ = data["toResponder"].(string)
		}
		message = fmt.Sprintf("Conversation transferred from %s to %s", fromResponder, toResponder)
		priority = "normal"

	case "followup_reminder":
		conversationId := ""
		if data != nil {
			conversationId, _ = data["conversationId"].(string)
		}
		message = fmt.Sprintf("Follow-up reminder: Conversation %s is due for follow-up", conversationId)
		priority = "normal"

	default:
		return nil, fmt.Errorf("unknown notification type: %s (expected 'queue_alert', 'escalation', 'transfer', or 'followup_reminder')", notifType)
	}

	recipientCount := len(recipients)
	if recipientCount == 0 {
		recipientCount = 1
	}

	return map[string]interface{}{
		"notificationId": notificationId,
		"type":           notifType,
		"message":        message,
		"priority":       priority,
		"sent":           timestamp,
		"recipientCount": recipientCount,
		"status":         "delivered",
	}, nil
}
