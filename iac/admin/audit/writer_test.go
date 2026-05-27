package audit_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/admin/audit"
)

// TestOpen_CreatesFileIfMissing pins that Open creates the audit
// file on first run rather than erroring on ENOENT — the host
// module's first start with a fresh access_log_path config must
// succeed.
func TestOpen_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected audit file at %s, stat err: %v", path, err)
	}
}

// TestOpen_FatalOnDirPath verifies the design's "FATAL on open
// failure" contract surfaces a clear error rather than silently
// swallowing. Passing an existing directory triggers an OS-level
// open error.
func TestOpen_FatalOnDirPath(t *testing.T) {
	dir := t.TempDir() // existing directory; opening it as a regular file fails
	_, err := audit.Open(dir)
	if err == nil {
		t.Fatal("expected Open to error when path is a directory, got nil")
	}
}

// TestWrite_AppendsOneJSONLineWithSchemaVersion1 pins the wire
// shape per design: each Write emits exactly one JSON line carrying
// schema_version:1. Multiple writes append; lines do not overlap.
func TestWrite_AppendsOneJSONLineWithSchemaVersion1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	entries := []audit.Entry{
		{
			TS:         time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
			ActionID:   "id-1",
			Subject:    "user:alice",
			Action:     "list_resources",
			Targets:    []string{},
			Result:     "ok",
			AppContext: "web",
		},
		{
			TS:         time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC),
			ActionID:   "id-2",
			Subject:    "user:bob",
			Action:     "get_resource",
			Targets:    []string{"vpc-prod"},
			Result:     "ok",
			AppContext: "api",
		},
	}
	for _, e := range entries {
		if err := w.Write(e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), string(data))
	}
	for i, line := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d not valid JSON: %v\n%s", i, err, line)
		}
		v, _ := got["schema_version"].(float64)
		if int(v) != 1 {
			t.Errorf("line %d schema_version = %v, want 1", i, got["schema_version"])
		}
	}
}

// TestWrite_ConcurrentAppendsAreSerialised pins the mutex around
// the write path so two goroutines writing simultaneously don't
// interleave bytes. We launch N writers; final line count == N AND
// every line is valid JSON.
func TestWrite_ConcurrentAppendsAreSerialised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const writers = 32
	const writesEach = 16
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range writesEach {
				_ = w.Write(audit.Entry{
					TS:       time.Now().UTC(),
					ActionID: "concurrent",
					Subject:  "user:test",
					Action:   "list_resources",
					Result:   "ok",
				})
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d not valid JSON (interleaved write?): %v\n%s", count, err, line)
		}
		count++
	}
	want := writers * writesEach
	if count != want {
		t.Errorf("got %d valid JSON lines, want %d (lost writes or interleaved bytes)", count, want)
	}
}

// TestSIGHUP_ReopensFileHandle verifies the SIGHUP-reopen contract
// from design Security Review: when an external log-rotation tool
// (logrotate, etc.) renames the audit file and sends SIGHUP, the
// writer reopens at the original path. Subsequent writes land in
// the NEW file (the original on-disk path), not in the moved
// inode the old fd still pointed at.
func TestSIGHUP_ReopensFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write one entry pre-rotation.
	if err := w.Write(audit.Entry{ActionID: "pre", Action: "list_resources", Result: "ok"}); err != nil {
		t.Fatal(err)
	}

	// Simulate log-rotation: move the file aside.
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("rename to simulate rotation: %v", err)
	}

	// Send SIGHUP to ourselves; the writer's signal handler should
	// reopen `path` (creating a new file since the old name was
	// renamed away).
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	// Give the signal handler a moment to process.
	time.Sleep(100 * time.Millisecond)

	// Write one entry post-rotation.
	if err := w.Write(audit.Entry{ActionID: "post", Action: "list_resources", Result: "ok"}); err != nil {
		t.Fatalf("post-rotation Write: %v", err)
	}

	// The rotated file holds only the pre-rotation entry.
	preData, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preData), `"action_id":"pre"`) {
		t.Errorf("rotated file missing pre-rotation entry: %s", string(preData))
	}
	if strings.Contains(string(preData), `"action_id":"post"`) {
		t.Errorf("rotated file contains POST-rotation entry — SIGHUP reopen failed: %s", string(preData))
	}

	// The current (re-created) file holds only the post-rotation entry.
	postData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read post-rotation file: %v", err)
	}
	if !strings.Contains(string(postData), `"action_id":"post"`) {
		t.Errorf("post-rotation file missing post-entry: %s", string(postData))
	}
	if strings.Contains(string(postData), `"action_id":"pre"`) {
		t.Errorf("post-rotation file contains pre-rotation entry — SIGHUP reopen wrote to wrong path: %s", string(postData))
	}
}

// TestClose_IsIdempotent verifies double-Close doesn't panic or
// error — host module's Stop() path may race with shutdown signals.
func TestClose_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close (should be no-op): %v", err)
	}
}

// TestWrite_AfterCloseReturnsError verifies post-Close Writes
// surface a clear error rather than silently dropping audit
// entries.
func TestWrite_AfterCloseReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := w.Write(audit.Entry{ActionID: "after-close"}); err == nil {
		t.Error("expected Write after Close to error, got nil")
	}
}
