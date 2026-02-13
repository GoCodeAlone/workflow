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
	conversations     = make(map[string]map[string]interface{})
	phoneToConvo      = make(map[string]string)
	queueCounts       = make(map[string]int)
	conversationsLock sync.Mutex
)

func Name() string {
	return "conversation-router"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	conversationsLock.Lock()
	conversations = make(map[string]map[string]interface{})
	phoneToConvo = make(map[string]string)
	queueCounts = make(map[string]int)
	conversationsLock.Unlock()
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	from, _ := params["from"].(string)
	body, _ := params["body"].(string)
	provider, _ := params["provider"].(string)
	affiliateId, _ := params["affiliateId"].(string)
	programId, _ := params["programId"].(string)

	if from == "" {
		return nil, fmt.Errorf("missing required parameter: from")
	}

	// Simulate routing logic delay (30-80ms)
	delay := time.Duration(30+r.Intn(50)) * time.Millisecond
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	conversationsLock.Lock()
	defer conversationsLock.Unlock()

	// Check for existing active conversation from this sender
	if existingId, ok := phoneToConvo[from]; ok {
		convo := conversations[existingId]
		return map[string]interface{}{
			"conversationId": existingId,
			"programId":      convo["programId"],
			"affiliateId":    convo["affiliateId"],
			"isNew":          false,
			"queuePosition":  0,
			"status":         convo["status"],
		}, nil
	}

	// Create new conversation
	if programId == "" {
		programId = "prog-001"
	}
	if affiliateId == "" {
		affiliateId = "aff-001"
	}

	convoId := fmt.Sprintf("convo-%d", r.Int63())
	queueCounts[programId]++

	convo := map[string]interface{}{
		"id":          convoId,
		"from":        from,
		"provider":    provider,
		"programId":   programId,
		"affiliateId": affiliateId,
		"status":      "queued",
		"firstMessage": body,
		"createdAt":   time.Now().UTC().Format(time.RFC3339),
	}
	conversations[convoId] = convo
	phoneToConvo[from] = convoId

	return map[string]interface{}{
		"conversationId": convoId,
		"programId":      programId,
		"affiliateId":    affiliateId,
		"isNew":          true,
		"queuePosition":  queueCounts[programId],
		"status":         "queued",
	}, nil
}
