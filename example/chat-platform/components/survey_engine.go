//go:build ignore

package component

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var (
	surveyResponses     = make(map[string]map[string]interface{})
	surveyResponsesLock sync.Mutex
)

var surveyDB = map[string]map[string]interface{}{
	"survey-001": {
		"id":        "survey-001",
		"programId": "prog-001",
		"type":      "entry",
		"title":     "Initial Check-in",
		"questions": []interface{}{
			map[string]interface{}{"id": "q1", "text": "On a scale of 1-5, how are you feeling right now?", "type": "scale", "min": 1, "max": 5},
			map[string]interface{}{"id": "q2", "text": "What brings you here today?", "type": "text"},
		},
	},
	"survey-002": {
		"id":        "survey-002",
		"programId": "prog-001",
		"type":      "exit",
		"title":     "Session Feedback",
		"questions": []interface{}{
			map[string]interface{}{"id": "q1", "text": "On a scale of 1-5, how are you feeling now?", "type": "scale", "min": 1, "max": 5},
			map[string]interface{}{"id": "q2", "text": "Did you find this session helpful?", "type": "choice", "options": []interface{}{"Yes", "Somewhat", "No"}},
			map[string]interface{}{"id": "q3", "text": "Any additional feedback?", "type": "text"},
		},
	},
	"survey-003": {
		"id":        "survey-003",
		"programId": "prog-002",
		"type":      "entry",
		"title":     "Teen Check-in",
		"questions": []interface{}{
			map[string]interface{}{"id": "q1", "text": "How would you rate your mood today?", "type": "scale", "min": 1, "max": 5},
			map[string]interface{}{"id": "q2", "text": "Is there something specific on your mind?", "type": "text"},
		},
	},
}

func Name() string {
	return "survey-engine"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	surveyResponsesLock.Lock()
	surveyResponses = make(map[string]map[string]interface{})
	surveyResponsesLock.Unlock()
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	action, _ := params["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("missing required parameter: action")
	}

	switch action {
	case "get_survey":
		surveyId, _ := params["surveyId"].(string)
		if surveyId == "" {
			return nil, fmt.Errorf("get_survey requires 'surveyId' parameter")
		}
		survey, ok := surveyDB[surveyId]
		if !ok {
			return map[string]interface{}{
				"found":    false,
				"surveyId": surveyId,
			}, nil
		}
		return map[string]interface{}{
			"found":     true,
			"surveyId":  survey["id"],
			"title":     survey["title"],
			"type":      survey["type"],
			"questions": survey["questions"],
		}, nil

	case "submit":
		surveyId, _ := params["surveyId"].(string)
		conversationId, _ := params["conversationId"].(string)
		responses, _ := params["responses"].(map[string]interface{})
		if surveyId == "" || conversationId == "" {
			return nil, fmt.Errorf("submit requires 'surveyId' and 'conversationId' parameters")
		}
		if responses == nil {
			return nil, fmt.Errorf("submit requires 'responses' parameter")
		}
		responseId := fmt.Sprintf("resp-%d", r.Int63())
		record := map[string]interface{}{
			"responseId":     responseId,
			"surveyId":       surveyId,
			"conversationId": conversationId,
			"responses":      responses,
			"completedAt":    time.Now().UTC().Format(time.RFC3339),
		}
		surveyResponsesLock.Lock()
		surveyResponses[responseId] = record
		surveyResponsesLock.Unlock()

		// Serialize for storage confirmation
		data, _ := json.Marshal(responses)
		return map[string]interface{}{
			"responseId":     responseId,
			"surveyId":       surveyId,
			"conversationId": conversationId,
			"status":         "completed",
			"completedAt":    record["completedAt"],
			"answerCount":    len(responses),
			"rawData":        string(data),
		}, nil

	case "check_pending":
		conversationId, _ := params["conversationId"].(string)
		if conversationId == "" {
			return nil, fmt.Errorf("check_pending requires 'conversationId' parameter")
		}
		surveyResponsesLock.Lock()
		hasPending := true
		for _, resp := range surveyResponses {
			if resp["conversationId"] == conversationId {
				hasPending = false
				break
			}
		}
		surveyResponsesLock.Unlock()
		return map[string]interface{}{
			"conversationId": conversationId,
			"hasPending":     hasPending,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s (expected 'get_survey', 'submit', or 'check_pending')", action)
	}
}
