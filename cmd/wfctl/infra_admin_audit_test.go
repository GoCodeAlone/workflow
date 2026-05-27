package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestRenderAuditTable_DecodesProtojsonNdjson is the regression
// guard for code-reviewer T19+T20 I-2: the CLI's audit-tail body
// decoder MUST handle protojson's int64-as-decimal-string wire
// convention. An earlier draft used encoding/json which rejected
// `"ts_unix": "1234567890"` (string form) when decoding into
// AdminAuditEntry's int64 TsUnix field. This test feeds a
// protojson-encoded fixture (the same shape T14's writer emits +
// T15's audit-tail HTTP endpoint serves) and asserts the decoder
// round-trips every line + renders the expected table columns.
func TestRenderAuditTable_DecodesProtojsonNdjson(t *testing.T) {
	// Build two AdminAuditEntry fixtures and serialise via protojson —
	// mimics exactly what T14's audit.Writer emits to disk and what
	// T15's audit-tail HTTP handler will stream over ndjson.
	entries := []*adminpb.AdminAuditEntry{
		{
			SchemaVersion: 1,
			TsUnix:        time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Unix(),
			Subject:       "user:alice",
			Action:        "list_resources",
			Result:        "ok",
			AppContext:    "web",
		},
		{
			SchemaVersion: 1,
			TsUnix:        time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC).Unix(),
			Subject:       "user:bob",
			Action:        "get_resource",
			Targets:       []string{"vpc-prod"},
			Result:        "ok",
			AppContext:    "api",
		},
	}
	opts := protojson.MarshalOptions{UseProtoNames: true}
	var body bytes.Buffer
	for _, e := range entries {
		data, err := opts.Marshal(e)
		if err != nil {
			t.Fatalf("protojson.Marshal fixture: %v", err)
		}
		body.Write(data)
		body.WriteByte('\n')
	}

	// Sanity: the fixture must actually carry the int64-as-string
	// form for ts_unix — this is the bit `encoding/json` chokes on.
	// If protojson ever changes encoding rules, this assertion
	// catches it so the decoder swap can be re-evaluated.
	if !strings.Contains(body.String(), `"ts_unix":"`) {
		t.Fatalf("fixture missing int64-as-string ts_unix encoding — protojson convention changed?\n%s", body.String())
	}

	// Redirect stdout for renderAuditTable. We don't assert on the
	// exact rendered bytes (column widths depend on tabwriter); we
	// only assert the decode loop completes + the row count matches
	// the fixture count.
	oldStdout := opensTabwriterStdout(t)
	defer oldStdout.restore()
	if err := renderAuditTable(&body); err != nil {
		t.Fatalf("renderAuditTable: %v", err)
	}
	out := oldStdout.captured()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// One header + 2 data rows = 3 lines.
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 entries):\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "TS") || !strings.Contains(lines[0], "SUBJECT") || !strings.Contains(lines[0], "ACTION") {
		t.Errorf("header row missing expected columns: %q", lines[0])
	}
	if !strings.Contains(lines[1], "user:alice") || !strings.Contains(lines[1], "list_resources") {
		t.Errorf("row 1 missing expected content: %q", lines[1])
	}
	if !strings.Contains(lines[2], "user:bob") || !strings.Contains(lines[2], "vpc-prod") {
		t.Errorf("row 2 missing expected content: %q", lines[2])
	}
}

// TestRenderAuditTable_HandlesEmptyBody guards against decoder
// crashes on an empty endpoint response (e.g. no entries since
// --since cutoff).
func TestRenderAuditTable_HandlesEmptyBody(t *testing.T) {
	oldStdout := opensTabwriterStdout(t)
	defer oldStdout.restore()
	if err := renderAuditTable(strings.NewReader("")); err != nil {
		t.Errorf("renderAuditTable on empty body: %v", err)
	}
	out := oldStdout.captured()
	// Header line should still print so operators see column shape
	// even when there are no entries.
	if !strings.Contains(out, "TS") {
		t.Errorf("header missing on empty body: %q", out)
	}
}

// opensTabwriterStdout swaps os.Stdout for a buffer for the test
// duration. renderAuditTable writes to os.Stdout directly, so we
// can't inject a writer without refactoring the function — the
// stdout redirect is the minimum-invasive approach.
type stdoutCapture struct {
	prev    *os.File
	r, w    *os.File
	done    chan struct{}
	collect *bytes.Buffer
}

func opensTabwriterStdout(t *testing.T) *stdoutCapture {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	c := &stdoutCapture{
		prev:    prev,
		r:       r,
		w:       w,
		done:    make(chan struct{}),
		collect: &bytes.Buffer{},
	}
	go func() {
		_, _ = c.collect.ReadFrom(c.r)
		close(c.done)
	}()
	return c
}

func (c *stdoutCapture) captured() string {
	_ = c.w.Close()
	<-c.done
	return c.collect.String()
}

func (c *stdoutCapture) restore() {
	os.Stdout = c.prev
}
