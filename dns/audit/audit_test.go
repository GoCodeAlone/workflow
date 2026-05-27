package audit

import (
	"os"
	"strings"
	"testing"
)

func TestAuditLog_AppendsAttemptThenOutcome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	LogAttempt("user@host", "example.com", "www", "A", "upsert", "multisite", "digitalocean")
	LogOutcome("user@host", "example.com", "www", "A", "success", "")
	path := tmp + "/wfctl/plugins/wfctl/dns-audit.jsonl"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("want 2 lines, got %d: %s", len(lines), data)
	}
}

func TestAuditLog_PolicyEdit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	LogPolicyEdit("sre@wfctl", "example.com", "set-policy", "abc123", "def456")
	path := tmp + "/wfctl/plugins/wfctl/dns-audit.jsonl"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(data), "set-policy") {
		t.Errorf("action missing from audit: %s", data)
	}
	if !strings.Contains(string(data), "abc123") {
		t.Errorf("prior_sha missing from audit: %s", data)
	}
}
