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
	return "aws-provider"
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

	// Simulate AWS API latency (100-250ms)
	delay := time.Duration(100+r.Intn(150)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	switch action {
	case "send":
		phoneNumber, _ := params["phoneNumber"].(string)
		message, _ := params["message"].(string)
		if phoneNumber == "" || message == "" {
			return nil, fmt.Errorf("send requires 'phoneNumber' and 'message' parameters")
		}
		messageId := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			r.Int31(), r.Int31n(0xffff), r.Int31n(0xffff),
			r.Int31n(0xffff), r.Int63n(0xffffffffffff))
		return map[string]interface{}{
			"messageId":      messageId,
			"status":         "sent",
			"provider":       "aws",
			"deliveryStatus": "SUCCESS",
			"phoneNumber":    phoneNumber,
			"messageType":    "Transactional",
			"requestId":      fmt.Sprintf("req-%d", r.Int63()),
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		}, nil

	case "receive":
		webhookData, _ := params["webhookData"].(map[string]interface{})
		if webhookData == nil {
			return nil, fmt.Errorf("receive requires 'webhookData' parameter")
		}
		// Parse SNS notification format
		messageBody, _ := webhookData["Message"].(string)
		originationNumber, _ := webhookData["originationNumber"].(string)
		destinationNumber, _ := webhookData["destinationNumber"].(string)
		messageId, _ := webhookData["messageId"].(string)
		if messageId == "" {
			messageId = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
				r.Int31(), r.Int31n(0xffff), r.Int31n(0xffff),
				r.Int31n(0xffff), r.Int63n(0xffffffffffff))
		}
		return map[string]interface{}{
			"from":      originationNumber,
			"to":        destinationNumber,
			"body":      strings.TrimSpace(messageBody),
			"messageId": messageId,
			"provider":  "aws",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'send' or 'receive')", action)
	}
}
