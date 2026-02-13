//go:build ignore

package component

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var keywordDB = map[string]map[string]interface{}{
	"HELLO": {
		"programId":  "prog-001",
		"action":     "route",
		"subProgram": "general",
		"response":   "You've reached Crisis Support. A counselor will be with you shortly.",
	},
	"HELP": {
		"programId":  "prog-001",
		"action":     "route",
		"subProgram": "general",
		"response":   "Help is here. You'll be connected to a counselor shortly.",
	},
	"TEEN": {
		"programId":  "prog-002",
		"action":     "route",
		"subProgram": "teen-support",
		"response":   "Welcome to Teen Support. We're here for you.",
	},
	"CRISIS": {
		"programId":  "prog-001",
		"action":     "route_priority",
		"subProgram": "crisis-immediate",
		"response":   "We hear you. A counselor will be with you right away.",
	},
	"WELLNESS": {
		"programId":  "prog-003",
		"action":     "route",
		"subProgram": "general",
		"response":   "Welcome to Wellness Chat. How can we support you today?",
	},
}

func Name() string {
	return "keyword-matcher"
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
	body, _ := params["body"].(string)
	if body == "" {
		// Twilio webhook sends "Body" (capitalized)
		body, _ = params["Body"].(string)
	}
	if body == "" {
		// Webchat sends "message"
		body, _ = params["message"].(string)
	}
	if body == "" {
		return nil, fmt.Errorf("missing required parameter: body")
	}

	// Extract first word and normalize
	words := strings.Fields(body)
	if len(words) == 0 {
		return map[string]interface{}{
			"matched":   false,
			"keyword":   "",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	firstWord := strings.ToUpper(words[0])

	if entry, ok := keywordDB[firstWord]; ok {
		programId, _ := entry["programId"].(string)
		subProgram, _ := entry["subProgram"].(string)
		action, _ := entry["action"].(string)
		response, _ := entry["response"].(string)

		return map[string]interface{}{
			"matched":      true,
			"keyword":      firstWord,
			"programId":    programId,
			"subProgram":   subProgram,
			"action":       action,
			"autoResponse": response,
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	return map[string]interface{}{
		"matched":   false,
		"keyword":   firstWord,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil
}
