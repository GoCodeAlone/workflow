package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDownloadProgressTTYUsesLiveLine(t *testing.T) {
	var out bytes.Buffer
	p := newDownloadProgressWithTerminal(&out, 100, true)
	if _, err := p.Write([]byte(strings.Repeat("x", 25))); err != nil {
		t.Fatalf("Write: %v", err)
	}
	p.finish()

	got := out.String()
	if !strings.Contains(got, "\rDownload progress:") {
		t.Fatalf("TTY progress output = %q, want carriage-return live progress", got)
	}
	if !strings.Contains(got, "\rDownload complete:") {
		t.Fatalf("TTY progress output = %q, want live completion line", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("TTY progress output = %q, want final newline", got)
	}
}

func TestDownloadProgressNonTTYUsesLogLines(t *testing.T) {
	var out bytes.Buffer
	p := newDownloadProgressWithTerminal(&out, 100, false)
	if _, err := p.Write([]byte(strings.Repeat("x", 25))); err != nil {
		t.Fatalf("Write: %v", err)
	}
	p.finish()

	got := out.String()
	if strings.Contains(got, "\r") {
		t.Fatalf("non-TTY progress output = %q, want no carriage returns", got)
	}
	if !strings.Contains(got, "Download progress:") || !strings.Contains(got, "Download complete:") {
		t.Fatalf("non-TTY progress output = %q, want progress and completion lines", got)
	}
}
