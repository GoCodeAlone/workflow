//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var (
	followups     = make(map[string]map[string]interface{})
	followupsLock sync.Mutex
)

func Name() string {
	return "followup-scheduler"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	followupsLock.Lock()
	followups = make(map[string]map[string]interface{})
	followupsLock.Unlock()
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := params["action"].(string)

	// When called from a state machine transition without an explicit action,
	// return success without scheduling anything.
	if action == "" {
		transitionId, _ := params["transitionId"].(string)
		return map[string]interface{}{
			"action":     "transition_hook",
			"transition": transitionId,
			"status":     "processed",
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	switch action {
	case "schedule":
		conversationId, _ := params["conversationId"].(string)
		message, _ := params["message"].(string)
		programId, _ := params["programId"].(string)
		scheduledTime, _ := params["scheduledTime"].(string)

		if conversationId == "" {
			return nil, fmt.Errorf("schedule requires 'conversationId' parameter")
		}
		if scheduledTime == "" {
			// Default to 24 hours from now
			scheduledTime = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		}
		if message == "" {
			message = "Hi, we're checking in to see how you're doing. Reply if you'd like to chat."
		}

		followUpId := fmt.Sprintf("fu-%d", r.Int63())
		record := map[string]interface{}{
			"id":             followUpId,
			"conversationId": conversationId,
			"programId":      programId,
			"message":        message,
			"scheduledFor":   scheduledTime,
			"status":         "scheduled",
			"createdAt":      time.Now().UTC().Format(time.RFC3339),
		}
		followupsLock.Lock()
		followups[followUpId] = record
		followupsLock.Unlock()

		return map[string]interface{}{
			"followUpId":   followUpId,
			"scheduledFor": scheduledTime,
			"status":       "scheduled",
		}, nil

	case "check_due":
		now := time.Now().UTC()
		due := make([]interface{}, 0)

		followupsLock.Lock()
		for _, fu := range followups {
			if fu["status"] != "scheduled" {
				continue
			}
			scheduledStr, _ := fu["scheduledFor"].(string)
			scheduled, err := time.Parse(time.RFC3339, scheduledStr)
			if err != nil {
				continue
			}
			if scheduled.Before(now) || scheduled.Equal(now) {
				due = append(due, map[string]interface{}{
					"followUpId":     fu["id"],
					"conversationId": fu["conversationId"],
					"message":        fu["message"],
					"scheduledFor":   fu["scheduledFor"],
				})
			}
		}
		followupsLock.Unlock()

		return map[string]interface{}{
			"dueCount":  len(due),
			"followUps": due,
			"checkedAt": now.Format(time.RFC3339),
		}, nil

	case "complete":
		followUpId, _ := params["followUpId"].(string)
		if followUpId == "" {
			return nil, fmt.Errorf("complete requires 'followUpId' parameter")
		}

		followupsLock.Lock()
		fu, ok := followups[followUpId]
		if !ok {
			followupsLock.Unlock()
			return nil, fmt.Errorf("follow-up not found: %s", followUpId)
		}
		fu["status"] = "completed"
		fu["completedAt"] = time.Now().UTC().Format(time.RFC3339)
		followupsLock.Unlock()

		return map[string]interface{}{
			"followUpId":  followUpId,
			"status":      "completed",
			"completedAt": fu["completedAt"],
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'schedule', 'check_due', or 'complete')", action)
	}
}
