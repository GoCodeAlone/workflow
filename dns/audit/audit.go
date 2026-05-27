package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type auditEntry struct {
	TS         string `json:"ts"`
	Actor      string `json:"actor"`
	Zone       string `json:"zone"`
	Action     string `json:"action,omitempty"`      // for policy edits
	Name       string `json:"name,omitempty"`        // for apply attempts
	RecordType string `json:"record_type,omitempty"` // for apply attempts
	Operation  string `json:"operation,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Error      string `json:"error,omitempty"`
	PriorSHA   string `json:"prior_sha256,omitempty"`
	NewSHA     string `json:"new_sha256,omitempty"`
}

func auditPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "wfctl", "plugins", "workflow-plugin-infra", "dns-policy-audit.jsonl")
}

func appendEntry(e auditEntry) error {
	p := auditPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}

// LogAttempt records a DNS record mutation attempt before the gate decision.
func LogAttempt(actor, zone, name, recordType, operation, owner, provider string) {
	_ = appendEntry(auditEntry{
		Actor: actor, Zone: zone, Name: name, RecordType: recordType,
		Operation: operation, Owner: owner, Provider: provider, Outcome: "attempted",
	})
}

// LogOutcome records the gate or apply outcome for a DNS record mutation.
func LogOutcome(actor, zone, name, recordType, outcome, errMsg string) {
	_ = appendEntry(auditEntry{
		Actor: actor, Zone: zone, Name: name, RecordType: recordType,
		Outcome: outcome, Error: errMsg,
	})
}

// LogPolicyEdit records a policy write operation (set-policy / transfer-ownership).
func LogPolicyEdit(actor, zone, action, priorSHA, newSHA string) {
	_ = appendEntry(auditEntry{
		Actor: actor, Zone: zone, Action: action, PriorSHA: priorSHA, NewSHA: newSHA,
	})
}
