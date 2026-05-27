package audit_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/admin/audit"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"google.golang.org/protobuf/encoding/protojson"
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

// TestWrite_AppendsOneProtojsonLineWithSchemaVersion1 pins the
// wire shape per design + spec-reviewer T14 F1: each Write emits
// exactly one protojson line carrying schema_version:1. Multiple
// writes append; lines do not overlap. Per-line round-trip
// through protojson.Unmarshal into AdminAuditEntry asserts the
// contract end-to-end (this is the test that would catch the
// earlier draft's schema-mismatch bug).
func TestWrite_AppendsOneProtojsonLineWithSchemaVersion1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	entries := []*audit.Entry{
		{
			TsUnix:     time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Unix(),
			Subject:    "user:alice",
			Action:     "list_resources",
			Result:     "ok",
			AppContext: "web",
		},
		{
			TsUnix:     time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC).Unix(),
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
		// Round-trip through protojson against the actual proto
		// message. This is the contract guard: a future regression
		// to encoding/json or to a non-proto struct will fail here.
		var got adminpb.AdminAuditEntry
		if err := protojson.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d: protojson.Unmarshal into AdminAuditEntry: %v\n%s", i, err, line)
		}
		if got.SchemaVersion != 1 {
			t.Errorf("line %d schema_version = %d, want 1", i, got.SchemaVersion)
		}
		// snake_case key shape: assert literal "ts_unix" / "schema_version" appear.
		if !strings.Contains(line, "\"schema_version\"") {
			t.Errorf("line %d missing snake_case schema_version key: %s", i, line)
		}
		if !strings.Contains(line, "\"ts_unix\"") {
			t.Errorf("line %d missing snake_case ts_unix key (writer must use UseProtoNames=true): %s", i, line)
		}
	}
}

// TestWrite_ConcurrentAppendsAreSerialised pins the mutex around
// the write path so two goroutines writing simultaneously don't
// interleave bytes. We launch N writers; final line count == N AND
// every line is valid protojson (decoded back into AdminAuditEntry).
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
				_ = w.Write(&audit.Entry{
					TsUnix:  time.Now().UTC().Unix(),
					Subject: "user:test",
					Action:  "list_resources",
					Result:  "ok",
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
		var got adminpb.AdminAuditEntry
		if err := protojson.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d not valid protojson (interleaved write?): %v\n%s", count, err, line)
		}
		count++
	}
	want := writers * writesEach
	if count != want {
		t.Errorf("got %d valid protojson lines, want %d (lost writes or interleaved bytes)", count, want)
	}
}

// TestSIGHUP_ReopensFileHandle verifies the SIGHUP-reopen contract
// from design Security Review: when an external log-rotation tool
// (logrotate, etc.) renames the audit file and sends SIGHUP, the
// writer reopens at the original path. Subsequent writes land in
// the NEW file (the original on-disk path), not in the moved
// inode the old fd still pointed at.
//
// Subject is used as the pre/post discriminator (the proto-aligned
// Entry doesn't carry an `action_id` field; subject is the closest
// per-entry label that survives the protojson round-trip).
func TestSIGHUP_ReopensFileHandle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write one entry pre-rotation.
	if err := w.Write(&audit.Entry{Subject: "subject:pre", Action: "list_resources", Result: "ok"}); err != nil {
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
	if err := w.Write(&audit.Entry{Subject: "subject:post", Action: "list_resources", Result: "ok"}); err != nil {
		t.Fatalf("post-rotation Write: %v", err)
	}

	// The rotated file holds only the pre-rotation entry.
	preData, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preData), `"subject":"subject:pre"`) {
		t.Errorf("rotated file missing pre-rotation entry: %s", string(preData))
	}
	if strings.Contains(string(preData), `"subject":"subject:post"`) {
		t.Errorf("rotated file contains POST-rotation entry — SIGHUP reopen failed: %s", string(preData))
	}

	// The current (re-created) file holds only the post-rotation entry.
	postData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read post-rotation file: %v", err)
	}
	if !strings.Contains(string(postData), `"subject":"subject:post"`) {
		t.Errorf("post-rotation file missing post-entry: %s", string(postData))
	}
	if strings.Contains(string(postData), `"subject":"subject:pre"`) {
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
	if err := w.Write(&audit.Entry{Subject: "after-close"}); err == nil {
		t.Error("expected Write after Close to error, got nil")
	}
}

// TestWrite_NilEntryReturnsError pins the defensive nil-guard so
// a future caller accidentally passing nil doesn't crash the host
// process — audit data integrity is preserved by surfacing the
// programming error.
func TestWrite_NilEntryReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	w, err := audit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Write(nil); err == nil {
		t.Error("expected Write(nil) to error, got nil")
	}
}
