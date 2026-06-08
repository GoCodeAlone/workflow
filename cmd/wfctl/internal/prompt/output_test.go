package prompt

import (
	"bytes"
	"testing"
)

func TestChooseOutputWriterPrefersStderrTTY(t *testing.T) {
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	got, ok := chooseOutputWriter(true, true, stderr, stdout)
	if !ok {
		t.Fatal("expected output writer")
	}
	if got != stderr {
		t.Fatalf("writer = %p, want stderr %p", got, stderr)
	}
}

func TestChooseOutputWriterFallsBackToStdoutTTY(t *testing.T) {
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	got, ok := chooseOutputWriter(false, true, stderr, stdout)
	if !ok {
		t.Fatal("expected output writer")
	}
	if got != stdout {
		t.Fatalf("writer = %p, want stdout %p", got, stdout)
	}
}

func TestChooseOutputWriterRejectsNonTTYOutput(t *testing.T) {
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	got, ok := chooseOutputWriter(false, false, stderr, stdout)
	if ok {
		t.Fatalf("expected no output writer, got %p", got)
	}
}
