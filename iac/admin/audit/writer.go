// Package audit hosts the JSONL audit-log writer used by the host-side
// infra.admin workflow module to record every admin action (read or
// future-mutating). Writer is concurrent-safe, append-only, and
// reopens its file handle on SIGHUP so external log-rotation tools
// (logrotate, etc.) can move the file aside without losing
// subsequent entries.
//
// Design: docs/plans/2026-05-27-infra-admin-dynamic-design.md §Security Review
// Plan:   docs/plans/2026-05-27-infra-admin-dynamic.md (Task 14)
//
// Entry schema (proto AdminAuditEntry mirror) carries schema_version:1
// so future-additive changes can be detected by reader tools. The
// JSON form intentionally uses snake_case keys matching the proto
// field names so downstream consumers (a future audit-tail HTTP
// endpoint streaming AdminAuditEntry over ndjson) see the same wire
// shape.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Entry is the host-side struct form of an audit record. Mirrors
// workflow.iac.v1.AdminAuditEntry from iac/admin/proto/infra_admin.
// proto so a future HTTP audit-tail endpoint can stream the on-disk
// JSONL line-for-line as AdminAuditEntry protojson.
type Entry struct {
	// SchemaVersion is set automatically by Write; callers don't
	// populate it. Hardcoded to 1 per design Security Review;
	// future schema-breaking changes bump the major.
	SchemaVersion int `json:"schema_version"`

	TS         time.Time `json:"ts"`
	ActionID   string    `json:"action_id"`
	Subject    string    `json:"subject"`
	Action     string    `json:"action"`
	Targets    []string  `json:"targets,omitempty"`
	Result     string    `json:"result"`
	AppContext string    `json:"app_context,omitempty"`

	// DryRun + ConfirmDestroy carry context for future mutating
	// actions (not exercised in v1 read-only). Per plan §Task 14
	// the schema declaration spans these so v1.1 lands without a
	// schema bump.
	DryRun         bool   `json:"dry_run,omitempty"`
	ConfirmDestroy string `json:"confirm_destroy,omitempty"`
}

// Writer wraps an append-only JSONL file with concurrent-safe writes
// and SIGHUP reopen. The host module (T15) holds one Writer for the
// lifetime of the infra.admin module; tests can create + close them
// at will.
//
// Close-safety: double-Close is a no-op. Post-Close Write returns a
// clear error rather than silently dropping audit entries — losing
// audit data is worse than a noisy error per design Security Review.
//
// SIGHUP handling: the writer registers a signal handler on Open
// that reopens the file path under the mutex. External rotation
// (logrotate, mv + SIGHUP) works without losing in-flight writes.
type Writer struct {
	path string

	mu     sync.Mutex
	file   *os.File
	closed bool

	sigC chan os.Signal
	done chan struct{}
}

// Open creates or appends-to the audit file at path and starts the
// SIGHUP-reopen goroutine. Per design Security Review: a non-nil
// error MUST be treated as FATAL at module Init — silently failing
// to open the audit log is the opposite of the "default-audit-
// everything" posture the design mandates. The caller (T15 module
// Init) propagates Open errors up as a module-init failure.
func Open(path string) (*Writer, error) {
	if path == "" {
		return nil, errors.New("audit.Open: empty path")
	}
	// 0o600 (owner-only) per gosec G302 + design Security Review's
	// "audit logs MUST NOT be world-readable" stance — even the
	// host's syslog group should not have read access without an
	// explicit operator decision.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit.Open %q: %w", path, err)
	}
	w := &Writer{
		path: path,
		file: f,
		sigC: make(chan os.Signal, 1),
		done: make(chan struct{}),
	}
	// Register SIGHUP handler. signal.Notify is goroutine-safe and
	// multiple writers in the same process all receive the signal
	// (each reopens its own file). Stop() in Close() unregisters.
	signal.Notify(w.sigC, syscall.SIGHUP)
	go w.reopenLoop()
	return w, nil
}

// reopenLoop is the background SIGHUP-reopen goroutine. Runs until
// done is closed by Close().
func (w *Writer) reopenLoop() {
	for {
		select {
		case <-w.sigC:
			w.reopen()
		case <-w.done:
			return
		}
	}
}

// reopen closes the current file handle (if any) and opens a fresh
// handle at the original path. Called from the SIGHUP handler
// goroutine. Errors during reopen are not propagated (no caller is
// listening) but a future enhancement could emit a stderr line so
// operators see the failure. For now, log via fmt.Fprintln so the
// host process's stderr captures it.
func (w *Writer) reopen() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // 0o600 per gosec G302; see Open
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit.Writer: SIGHUP reopen %q failed: %v (subsequent writes will error)\n", w.path, err)
		w.file = nil
		return
	}
	w.file = f
}

// Write serializes the entry to one JSON line + newline and appends
// it under the mutex. Empty path means the writer was never opened
// (defensive — should be caught at Open); Closed-after returns a
// clear error.
//
// SchemaVersion is set to 1 on the caller-provided entry before
// marshaling so callers cannot accidentally emit a different
// version. If the schema ever bumps to 2, this is the single
// change-point.
func (w *Writer) Write(e Entry) error {
	e.SchemaVersion = 1
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit.Write: marshal: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("audit.Write: writer is closed")
	}
	if w.file == nil {
		return errors.New("audit.Write: writer has no file handle (SIGHUP reopen failed earlier)")
	}
	if _, err := w.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("audit.Write: %w", err)
	}
	return nil
}

// Close stops the SIGHUP goroutine, unregisters the signal handler,
// and closes the file handle. Double-Close is a no-op; post-Close
// Write returns a clear error.
func (w *Writer) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	file := w.file
	w.file = nil
	w.mu.Unlock()

	// Stop the goroutine + unregister the signal handler. signal.Stop
	// is goroutine-safe; the channel close signals reopenLoop to exit.
	signal.Stop(w.sigC)
	close(w.done)

	if file != nil {
		return file.Close()
	}
	return nil
}
