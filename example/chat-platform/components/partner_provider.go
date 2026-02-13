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
	return "partner-provider"
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

	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}

	// Simulate partner API latency (120-300ms)
	delay := time.Duration(120+r.Intn(180)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	switch action {
	case "send":
		endpoint, _ := params["endpoint"].(string)
		payload, _ := params["payload"].(map[string]interface{})
		if endpoint == "" {
			endpoint = "https://partner-api.example.com/messages"
		}
		if payload == nil {
			return nil, fmt.Errorf("send requires 'payload' parameter")
		}
		requestId := fmt.Sprintf("partner-%d", r.Int63())
		// 95% success rate for partner API
		if r.Float64() < 0.95 {
			return map[string]interface{}{
				"requestId":  requestId,
				"status":     "delivered",
				"provider":   "partner",
				"endpoint":   endpoint,
				"statusCode": 200,
				"timestamp":  time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
		return map[string]interface{}{
			"requestId":  requestId,
			"status":     "failed",
			"provider":   "partner",
			"endpoint":   endpoint,
			"statusCode": 503,
			"error":      "partner service temporarily unavailable",
			"retryable":  true,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		}, nil

	case "receive":
		webhookData, _ := params["webhookData"].(map[string]interface{})
		if webhookData == nil {
			return nil, fmt.Errorf("receive requires 'webhookData' parameter")
		}
		externalId, _ := webhookData["externalId"].(string)
		sender, _ := webhookData["sender"].(string)
		recipient, _ := webhookData["recipient"].(string)
		content, _ := webhookData["content"].(string)
		if externalId == "" {
			externalId = fmt.Sprintf("ext-%d", r.Int63())
		}
		return map[string]interface{}{
			"from":       sender,
			"to":         recipient,
			"body":       strings.TrimSpace(content),
			"externalId": externalId,
			"provider":   "partner",
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'send' or 'receive')", action)
	}
}
