//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

func Name() string {
	return "escalation-handler"
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

	escalationType, _ := params["type"].(string)

	// When called from a state machine transition without an explicit type,
	// return success as a system event.
	if escalationType == "" {
		transitionId, _ := params["transitionId"].(string)
		return map[string]interface{}{
			"escalationId": fmt.Sprintf("esc-%d", r.Int63()),
			"type":         "system",
			"transition":   transitionId,
			"status":       "processed",
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	conversationId, _ := params["conversationId"].(string)
	urgency, _ := params["urgency"].(string)
	if urgency == "" {
		urgency = "standard"
	}

	// Simulate escalation processing delay (200-500ms)
	delay := time.Duration(200+r.Intn(300)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	escalationId := fmt.Sprintf("esc-%d", r.Int63())
	refNumber := fmt.Sprintf("REF-%06d", r.Intn(999999))

	switch escalationType {
	case "medical":
		location, _ := params["location"].(string)
		if location == "" {
			location = "not provided"
		}
		return map[string]interface{}{
			"escalationId":    escalationId,
			"type":            "medical",
			"conversationId":  conversationId,
			"status":          "contacted",
			"contactedService": "National Crisis Medical Line",
			"referenceNumber": refNumber,
			"urgency":         urgency,
			"location":        location,
			"referralType":    "mental_health_professional",
			"responseTime":    "15-30 minutes",
			"instructions":    "Medical professional has been notified. Maintain contact with texter until professional arrives.",
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		}, nil

	case "police":
		location, _ := params["location"].(string)
		if location == "" {
			return map[string]interface{}{
				"escalationId":   escalationId,
				"type":           "police",
				"conversationId": conversationId,
				"status":         "pending_location",
				"message":        "Location is required for police escalation. Please obtain texter location.",
				"timestamp":      time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
		caseNumber := fmt.Sprintf("PD-%04d-%06d", time.Now().Year(), r.Intn(999999))
		return map[string]interface{}{
			"escalationId":    escalationId,
			"type":            "police",
			"conversationId":  conversationId,
			"status":          "dispatched",
			"contactedService": "Local Emergency Services",
			"referenceNumber": refNumber,
			"caseNumber":      caseNumber,
			"urgency":         urgency,
			"location":        location,
			"responseTime":    "immediate",
			"instructions":    "Emergency services dispatched. Keep texter engaged and calm. Do NOT disconnect.",
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown escalation type: %s (expected 'medical' or 'police')", escalationType)
	}
}
