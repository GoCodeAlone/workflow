//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Name returns the component name.
func Name() string {
	return "notification-sender"
}

// Init initializes the component with service references.
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

// Execute sends a notification about a workflow state change.
// Always succeeds. Logs what it would send.
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	toState, _ := params["toState"].(string)
	workflowID, _ := params["workflowId"].(string)
	if toState == "" {
		toState = "unknown"
	}

	template := fmt.Sprintf("order_%s", toState)
	notifID := fmt.Sprintf("notif-%d", r.Int63())

	fmt.Printf("[notification-sender] Sending %s notification for workflow %s (template: %s)\n",
		toState, workflowID, template)

	return map[string]interface{}{
		"notification_id": notifID,
		"channel":         "email",
		"template":        template,
		"sent_at":         time.Now().Format(time.RFC3339),
	}, nil
}
