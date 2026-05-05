package validation

import (
	"net/http"
	"strings"
)

// ParsePRCommentOverride extracts an override token from a GitHub PR comment.
// The expected format is "/wfctl-override <token>".
// Returns (token, true) if found, ("", false) otherwise.
func ParsePRCommentOverride(comment string) (string, bool) {
	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/wfctl-override ") {
			token := strings.TrimSpace(strings.TrimPrefix(line, "/wfctl-override "))
			if token != "" {
				return token, true
			}
		}
	}
	return "", false
}

// ParseAPIHeaderOverride extracts an override token from the X-Workflow-Override
// HTTP request header. Returns (token, true) if present, ("", false) otherwise.
func ParseAPIHeaderOverride(r *http.Request) (string, bool) {
	token := strings.TrimSpace(r.Header.Get("X-Workflow-Override"))
	if token != "" {
		return token, true
	}
	return "", false
}

// ParseWorkflowDispatchOverride extracts an override token from GitHub Actions
// workflow_dispatch inputs. Looks for the key "override_token" in the inputs map.
// Returns (token, true) if present, ("", false) otherwise.
func ParseWorkflowDispatchOverride(inputs map[string]string) (string, bool) {
	token := strings.TrimSpace(inputs["override_token"])
	if token != "" {
		return token, true
	}
	return "", false
}
