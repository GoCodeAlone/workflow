//go:build ignore

// Package component is a dynamically loaded workflow component.
//
// This example handles alerts by formatting and "sending" them
// (logging in this mock implementation).
package component

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Name returns the component name.
func Name() string {
	return "alert-handler"
}

// Init initializes the component.
func Init(services map[string]interface{}) error {
	return nil
}

// Start begins the component.
func Start(ctx context.Context) error {
	return nil
}

// Stop halts the component.
func Stop(ctx context.Context) error {
	return nil
}

// Execute processes an alert.
// Params should contain "severity" (string), "message" (string),
// and optionally "recipients" (string, comma-separated).
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	severity, _ := params["severity"].(string)
	if severity == "" {
		severity = "info"
	}
	message, _ := params["message"].(string)
	if message == "" {
		return nil, fmt.Errorf("missing required parameter: message")
	}

	recipientStr, _ := params["recipients"].(string)
	var recipients []string
	if recipientStr != "" {
		for _, r := range strings.Split(recipientStr, ",") {
			recipients = append(recipients, strings.TrimSpace(r))
		}
	}

	alertID := fmt.Sprintf("alert-%d", time.Now().UnixNano())

	return map[string]interface{}{
		"alert_id":   alertID,
		"severity":   strings.ToUpper(severity),
		"message":    message,
		"recipients": recipients,
		"sent_at":    time.Now().Format(time.RFC3339),
		"status":     "sent",
	}, nil
}
