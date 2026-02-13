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
	return "twilio-provider"
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

	// Simulate Twilio API latency (80-200ms)
	delay := time.Duration(80+r.Intn(120)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	switch action {
	case "send":
		to, _ := params["to"].(string)
		body, _ := params["body"].(string)
		from, _ := params["from"].(string)
		if to == "" || body == "" {
			return nil, fmt.Errorf("send requires 'to' and 'body' parameters")
		}
		if from == "" {
			from = "+18001234567"
		}
		sid := fmt.Sprintf("SM%032x", r.Int63())
		return map[string]interface{}{
			"sid":         sid,
			"status":      "sent",
			"provider":    "twilio",
			"to":          to,
			"from":        from,
			"body":        body,
			"dateCreated": time.Now().UTC().Format(time.RFC3339),
			"direction":   "outbound-api",
			"price":       "-0.0075",
			"priceUnit":   "USD",
		}, nil

	case "receive":
		webhookData, _ := params["webhookData"].(map[string]interface{})
		if webhookData == nil {
			return nil, fmt.Errorf("receive requires 'webhookData' parameter")
		}
		from, _ := webhookData["From"].(string)
		to, _ := webhookData["To"].(string)
		body, _ := webhookData["Body"].(string)
		sid, _ := webhookData["MessageSid"].(string)
		if sid == "" {
			sid = fmt.Sprintf("SM%032x", r.Int63())
		}
		numMedia, _ := webhookData["NumMedia"].(string)
		if numMedia == "" {
			numMedia = "0"
		}
		return map[string]interface{}{
			"from":      from,
			"to":        to,
			"body":      strings.TrimSpace(body),
			"sid":       sid,
			"provider":  "twilio",
			"numMedia":  numMedia,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'send' or 'receive')", action)
	}
}
