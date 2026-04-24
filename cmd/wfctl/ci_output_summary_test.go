package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func TestRenderSummary_Failure_Golden(t *testing.T) {
	in := SummaryInput{
		Operation:  "deploy",
		Env:        "staging",
		Resource:   "bmw-staging",
		Outcome:    "FAILED",
		ConsoleURL: "https://cloud.digitalocean.com/apps/abc",
		RootCause:  "workflow-migrate up: first .: file does not exist",
		Phases: []PhaseTiming{
			{Name: "build", Status: "SUCCESS", Duration: 134 * time.Second},
			{Name: "pre_deploy", Status: "ERROR", Duration: 3 * time.Second},
		},
		Diagnostics: []interfaces.Diagnostic{
			{ID: "dep-123", Phase: "pre_deploy", Cause: "exit status 1",
				At:     mustTime("2026-04-24T17:42:45Z"),
				Detail: "workflow-migrate up: first .: file does not exist\nError: exit status 1"},
		},
	}
	var got bytes.Buffer
	if err := renderSummary(&got, in); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "summary_failure.golden.md", got.String())
}

func TestRenderSummary_Success_Golden(t *testing.T) {
	in := SummaryInput{
		Operation: "deploy", Env: "staging", Resource: "bmw-staging",
		Outcome: "SUCCESS", ConsoleURL: "https://cloud.digitalocean.com/apps/abc",
		Phases: []PhaseTiming{
			{Name: "build", Status: "SUCCESS", Duration: 134 * time.Second},
			{Name: "pre_deploy", Status: "SUCCESS", Duration: 12 * time.Second},
			{Name: "deploy", Status: "SUCCESS", Duration: 45 * time.Second},
		},
	}
	var got bytes.Buffer
	if err := renderSummary(&got, in); err != nil {
		t.Fatal(err)
	}
	compareGolden(t, "summary_success.golden.md", got.String())
}

func TestWriteStepSummary_NoPathNoop(t *testing.T) {
	e := plainEmitter{}
	if err := WriteStepSummary(e, SummaryInput{}); err != nil {
		t.Fatalf("plain emitter should noop, got err: %v", err)
	}
}

func TestWriteStepSummary_GHA_AppendsToFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "summary.md")
	e := githubEmitter{summaryPath: path}
	if err := WriteStepSummary(e, SummaryInput{
		Operation: "apply", Env: "staging", Outcome: "SUCCESS",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("## wfctl: apply staging — SUCCESS")) {
		t.Errorf("summary missing header: %q", data)
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update-golden)", err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch in %s\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}
