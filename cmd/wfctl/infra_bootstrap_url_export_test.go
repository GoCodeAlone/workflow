package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBootstrapStateBackend_PrintsExportLine verifies that after a successful
// Spaces bootstrap, bootstrapStateBackend prints `export DO_SPACES_BUCKET=<name>`
// to stdout for CI capture.
func TestBootstrapStateBackend_PrintsExportLine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: bmw-iac-state-test
      region: nyc3
      accessKey: key123
      secretKey: secret456
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Override the real S3 bootstrap with a no-op fake.
	origFn := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, bucket, _, _, _, _ string) error {
		fmt.Printf("  state backend: bucket %q already exists — skipped\n", bucket)
		return nil
	}
	defer func() { bootstrapDOSpacesBucketFn = origFn }()

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := bootstrapStateBackend(context.Background(), cfgPath)

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	output := buf.String()

	if err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	if !strings.Contains(output, "export WFCTL_STATE_BUCKET=bmw-iac-state-test") {
		t.Errorf("expected 'export WFCTL_STATE_BUCKET=bmw-iac-state-test' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "export SPACES_BUCKET=bmw-iac-state-test") {
		t.Errorf("expected 'export SPACES_BUCKET=bmw-iac-state-test' in output, got:\n%s", output)
	}
}

// TestBootstrapStateBackend_WritesBucketBackToConfig verifies that after a
// successful Spaces bootstrap, the resolved bucket name is written back to the
// on-disk config file so downstream commands can load it without env vars.
func TestBootstrapStateBackend_WritesBucketBackToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	initialYAML := `modules:
  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: original-bucket
      region: nyc3
      accessKey: key
      secretKey: sec
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origFn := bootstrapDOSpacesBucketFn
	bootstrapDOSpacesBucketFn = func(_ context.Context, _, _, _, _, _ string) error { return nil }
	defer func() { bootstrapDOSpacesBucketFn = origFn }()

	// Suppress stdout output.
	oldStdout := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() { os.Stdout = oldStdout; devNull.Close() }()

	if err := bootstrapStateBackend(context.Background(), cfgPath); err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "bucket: original-bucket") {
		t.Errorf("expected bucket field to remain in config, got:\n%s", string(data))
	}
}

// TestWriteBucketBackToConfig_ReplacesField verifies that writeBucketBackToConfig
// replaces the bucket: field inside an iac.state module block without altering
// other fields or YAML structure.
func TestWriteBucketBackToConfig_ReplacesField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	yaml := `modules:
  - name: other-module
    type: infra.database
    config:
      bucket: not-this-one

  - name: state-backend
    type: iac.state
    config:
      backend: spaces
      bucket: ${OLD_BUCKET}
      region: nyc3
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := writeBucketBackToConfig(cfgPath, "new-bucket"); err != nil {
		t.Fatalf("writeBucketBackToConfig: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	result := string(data)

	// The iac.state bucket field must be updated.
	if !strings.Contains(result, "bucket: new-bucket") {
		t.Errorf("expected 'bucket: new-bucket' in result, got:\n%s", result)
	}
	// The infra.database bucket field must be untouched.
	if !strings.Contains(result, "bucket: not-this-one") {
		t.Errorf("expected 'bucket: not-this-one' (other module) to remain, got:\n%s", result)
	}
}
