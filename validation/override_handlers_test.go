package validation

import (
	"net/http"
	"testing"
)

func TestParsePRCommentOverride(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    string
		ok      bool
	}{
		{
			name:    "valid token",
			comment: "/wfctl-override able-about-above",
			want:    "able-about-above",
			ok:      true,
		},
		{
			name:    "token in multiline comment",
			comment: "This config looks good.\n/wfctl-override acid-actor-adapt\nThanks!",
			want:    "acid-actor-adapt",
			ok:      true,
		},
		{
			name:    "no override",
			comment: "LGTM",
			ok:      false,
		},
		{
			name:    "empty token",
			comment: "/wfctl-override ",
			ok:      false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePRCommentOverride(tc.comment)
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("token = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAPIHeaderOverride(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("X-Workflow-Override", "able-about-acid")
	token, ok := ParseAPIHeaderOverride(r)
	if !ok {
		t.Fatal("expected override token from header")
	}
	if token != "able-about-acid" {
		t.Errorf("token = %q, want %q", token, "able-about-acid")
	}

	r2, _ := http.NewRequest("GET", "/", nil)
	_, ok2 := ParseAPIHeaderOverride(r2)
	if ok2 {
		t.Error("expected no token when header absent")
	}
}

func TestParseWorkflowDispatchOverride(t *testing.T) {
	inputs := map[string]string{"override_token": "able-about-above"}
	token, ok := ParseWorkflowDispatchOverride(inputs)
	if !ok {
		t.Fatal("expected token from inputs")
	}
	if token != "able-about-above" {
		t.Errorf("token = %q, want %q", token, "able-about-above")
	}

	_, ok2 := ParseWorkflowDispatchOverride(map[string]string{})
	if ok2 {
		t.Error("expected no token for empty inputs")
	}
}
