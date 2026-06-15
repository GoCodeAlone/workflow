package main

import (
	"bytes"
	"io"
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

func TestReadDownloadBodyWithProgressCanBeSuppressedByEnv(t *testing.T) {
	t.Setenv("WFCTL_PLUGIN_INSTALL_QUIET", "true")

	got, err := readDownloadBodyWithProgress(strings.NewReader("payload"), int64(len("payload")))
	if err != nil {
		t.Fatalf("readDownloadBodyWithProgress: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("readDownloadBodyWithProgress = %q, want payload", got)
	}
}

func TestDownloadProgressQuietFlagScope(t *testing.T) {
	restore := setDownloadProgressQuiet(true)
	if !shouldSuppressDownloadProgress() {
		t.Fatal("quiet scope did not suppress download progress")
	}
	got, err := readDownloadBodyWithProgress(strings.NewReader("quiet"), int64(len("quiet")))
	if err != nil {
		t.Fatalf("readDownloadBodyWithProgress: %v", err)
	}
	if string(got) != "quiet" {
		t.Fatalf("readDownloadBodyWithProgress = %q, want quiet", got)
	}
	restore()
	if shouldSuppressDownloadProgress() {
		t.Fatal("quiet scope leaked after restore")
	}
}

func TestReadDownloadBodyWithProgressQuietPropagatesReadErrors(t *testing.T) {
	restore := setDownloadProgressQuiet(true)
	defer restore()

	_, err := readDownloadBodyWithProgress(errReader{}, 10)
	if err == nil {
		t.Fatal("readDownloadBodyWithProgress returned nil error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
