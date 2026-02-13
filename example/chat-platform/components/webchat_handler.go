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
	sessions     = make(map[string][]map[string]interface{})
	sessionsLock sync.Mutex
)

func Name() string {
	return "webchat-handler"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	sessionsLock.Lock()
	sessions = make(map[string][]map[string]interface{})
	sessionsLock.Unlock()
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}

	switch action {
	case "receive":
		sessionId, _ := params["sessionId"].(string)
		message, _ := params["message"].(string)
		if message == "" {
			return nil, fmt.Errorf("receive requires 'message' parameter")
		}
		if sessionId == "" {
			sessionId = fmt.Sprintf("ws-%d", r.Int63())
		}
		metadata, _ := params["metadata"].(map[string]interface{})
		msg := map[string]interface{}{
			"id":        fmt.Sprintf("msg-%d", r.Int63()),
			"sessionId": sessionId,
			"body":      message,
			"sender":    "texter",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		if metadata != nil {
			msg["metadata"] = metadata
		}
		sessionsLock.Lock()
		sessions[sessionId] = append(sessions[sessionId], msg)
		sessionsLock.Unlock()
		return map[string]interface{}{
			"sessionId": sessionId,
			"messageId": msg["id"],
			"provider":  "webchat",
			"status":    "received",
			"timestamp": msg["timestamp"],
		}, nil

	case "send":
		sessionId, _ := params["sessionId"].(string)
		message, _ := params["message"].(string)
		if sessionId == "" || message == "" {
			return nil, fmt.Errorf("send requires 'sessionId' and 'message' parameters")
		}
		msg := map[string]interface{}{
			"id":        fmt.Sprintf("msg-%d", r.Int63()),
			"sessionId": sessionId,
			"body":      message,
			"sender":    "responder",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		sessionsLock.Lock()
		sessions[sessionId] = append(sessions[sessionId], msg)
		sessionsLock.Unlock()
		return map[string]interface{}{
			"sessionId": sessionId,
			"messageId": msg["id"],
			"provider":  "webchat",
			"status":    "queued",
			"timestamp": msg["timestamp"],
		}, nil

	case "poll":
		sessionId, _ := params["sessionId"].(string)
		if sessionId == "" {
			return nil, fmt.Errorf("poll requires 'sessionId' parameter")
		}
		sessionsLock.Lock()
		msgs := sessions[sessionId]
		pending := make([]interface{}, 0)
		for _, m := range msgs {
			if m["sender"] == "responder" {
				pending = append(pending, m)
			}
		}
		sessionsLock.Unlock()
		return map[string]interface{}{
			"sessionId": sessionId,
			"messages":  pending,
			"count":     len(pending),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'receive', 'send', or 'poll')", action)
	}
}
