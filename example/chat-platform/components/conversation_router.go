//go:build ignore

package component

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

var (
	conversations     = make(map[string]map[string]interface{})
	phoneToConvo      = make(map[string]string)
	queueCounts       = make(map[string]int)
	conversationsLock sync.Mutex
)

// programAffiliateMap maps programId -> affiliateId
var programAffiliateMap = map[string]string{
	"prog-001": "aff-001", // Crisis Text Line -> Crisis Support International
	"prog-002": "aff-001", // Teen Support Line -> Crisis Support International
	"prog-003": "aff-002", // Wellness Chat -> Youth Mental Health Alliance
	"prog-004": "aff-003", // Partner Assist -> Global Wellness Network
}

// shortCodeProgramMap maps short codes to programIds
var shortCodeProgramMap = map[string]string{
	"741741": "prog-001", // Crisis Text Line
	"741742": "prog-002", // Teen Support Line
}

// providerProgramMap maps provider types to default programIds
var providerProgramMap = map[string]string{
	"twilio":  "prog-001", // Default Twilio -> Crisis Text Line
	"webchat": "prog-001", // Default webchat -> Crisis Text Line
	"aws":     "prog-003", // AWS -> Wellness Chat
	"partner": "prog-004", // Partner -> Partner Assist
}

// keywordProgramMap maps keywords to programIds (mirrors keyword_matcher.go)
var keywordProgramMap = map[string]string{
	"HELLO":    "prog-001",
	"HELP":     "prog-001",
	"CRISIS":   "prog-001",
	"TEEN":     "prog-002",
	"WELLNESS": "prog-003",
	"PARTNER":  "prog-004",
}

// programNameMap maps programId to display name
var programNameMap = map[string]string{
	"prog-001": "Crisis Text Line",
	"prog-002": "Teen Support Line",
	"prog-003": "Wellness Chat",
	"prog-004": "Partner Assist",
}

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

// resolveRouting determines the programId and affiliateId based on message content,
// shortcode, and provider — in priority order.
func resolveRouting(body, shortCode, provider string) (programId, affiliateId string) {
	// 1. Keyword match (highest priority)
	if body != "" {
		words := strings.Fields(body)
		if len(words) > 0 {
			firstWord := strings.ToUpper(words[0])
			if pid, ok := keywordProgramMap[firstWord]; ok {
				programId = pid
				affiliateId = programAffiliateMap[pid]
				return
			}
		}
	}

	// 2. Shortcode match
	if shortCode != "" {
		if pid, ok := shortCodeProgramMap[shortCode]; ok {
			programId = pid
			affiliateId = programAffiliateMap[pid]
			return
		}
	}

	// 3. Provider match
	if provider != "" {
		if pid, ok := providerProgramMap[strings.ToLower(provider)]; ok {
			programId = pid
			affiliateId = programAffiliateMap[pid]
			return
		}
	}

	// 4. Default fallback
	programId = "prog-001"
	affiliateId = "aff-001"
	return
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	from, _ := params["from"].(string)
	if from == "" {
		// Twilio webhook sends "From" (capitalized)
		from, _ = params["From"].(string)
	}
	body, _ := params["body"].(string)
	if body == "" {
		body, _ = params["Body"].(string)
	}
	if body == "" {
		body, _ = params["message"].(string)
	}
	provider, _ := params["provider"].(string)
	shortCode, _ := params["shortCode"].(string)
	if shortCode == "" {
		shortCode, _ = params["toNumber"].(string)
	}

	// Explicit overrides from params (e.g., if caller already resolved)
	explicitAffiliateId, _ := params["affiliateId"].(string)
	explicitProgramId, _ := params["programId"].(string)

	// If called for an assign/transition (responderId present, no "from"),
	// look up the conversation by ID and return existing routing info.
	if from == "" {
		responderId, _ := params["responderId"].(string)
		convoId, _ := params["id"].(string)
		if convoId == "" {
			convoId, _ = params["conversationId"].(string)
		}
		if responderId != "" && convoId != "" {
			conversationsLock.Lock()
			defer conversationsLock.Unlock()
			if convo, ok := conversations[convoId]; ok {
				convo["responderId"] = responderId
				convo["status"] = "active"
				conversations[convoId] = convo
				return map[string]interface{}{
					"conversationId": convoId,
					"responderId":    responderId,
					"programId":      convo["programId"],
					"affiliateId":    convo["affiliateId"],
					"status":         "active",
				}, nil
			}
			// Conversation not in router's memory; return success to allow assignment
			return map[string]interface{}{
				"conversationId": convoId,
				"responderId":    responderId,
				"status":         "active",
			}, nil
		}
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
			"programName":    convo["programName"],
			"isNew":          false,
			"queuePosition":  0,
			"status":         convo["status"],
		}, nil
	}

	// Resolve routing for new conversation
	var programId, affiliateId string
	if explicitProgramId != "" && explicitAffiliateId != "" {
		programId = explicitProgramId
		affiliateId = explicitAffiliateId
	} else {
		programId, affiliateId = resolveRouting(body, shortCode, provider)
		// Allow explicit overrides to take precedence for individual fields
		if explicitProgramId != "" {
			programId = explicitProgramId
			affiliateId = programAffiliateMap[programId]
		}
		if explicitAffiliateId != "" {
			affiliateId = explicitAffiliateId
		}
	}

	programName := programNameMap[programId]

	convoId := fmt.Sprintf("convo-%d", r.Int63())
	queueCounts[programId]++

	convo := map[string]interface{}{
		"id":           convoId,
		"from":         from,
		"provider":     provider,
		"programId":    programId,
		"programName":  programName,
		"affiliateId":  affiliateId,
		"status":       "queued",
		"firstMessage": body,
		"createdAt":    time.Now().UTC().Format(time.RFC3339),
	}
	conversations[convoId] = convo
	phoneToConvo[from] = convoId

	return map[string]interface{}{
		"conversationId": convoId,
		"programId":      programId,
		"programName":    programName,
		"affiliateId":    affiliateId,
		"isNew":          true,
		"queuePosition":  queueCounts[programId],
		"status":         "queued",
	}, nil
}
