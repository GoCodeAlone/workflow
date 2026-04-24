package config

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// resetInlinePluginDeprecationOnce resets the once guard so each test gets a
// fresh run. Defined here (test file) so the reset helper is not part of the
// production binary.
func resetInlinePluginDeprecationOnce() {
	inlinePluginDeprecationOnce = sync.Once{}
}

func TestInlinePluginVersion_EmitsDeprecationWarning(t *testing.T) {
	// Reset the once so each test gets a fresh run.
	inlinePluginDeprecationOnce.Do(func() {}) // no-op to ensure it's initialized
	resetInlinePluginDeprecationOnce()

	// Capture stderr.
	old := os.Stderr
	t.Cleanup(func() { os.Stderr = old })
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := LoadFromString(`
modules: []
requires:
  plugins:
    - name: workflow-plugin-foo
      version: v1.0.0
      source: github.com/GoCodeAlone/workflow-plugin-foo
`)
	if err != nil {
		t.Fatalf("LoadFromString: %v", err)
	}

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stderr = old

	output := buf.String()
	if !strings.Contains(output, "deprecated") && !strings.Contains(output, "wfctl migrate plugins") {
		t.Errorf("expected deprecation warning on stderr, got: %q", output)
	}
}

func TestInlinePluginNameOnly_NoDeprecationWarning(t *testing.T) {
	resetInlinePluginDeprecationOnce()

	old := os.Stderr
	t.Cleanup(func() { os.Stderr = old })
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, err := LoadFromString(`
modules: []
requires:
  plugins:
    - name: workflow-plugin-foo
`)
	if err != nil {
		t.Fatalf("LoadFromString: %v", err)
	}

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stderr = old

	output := buf.String()
	if strings.Contains(output, "deprecated") {
		t.Errorf("unexpected deprecation warning for name-only plugin: %q", output)
	}
}
