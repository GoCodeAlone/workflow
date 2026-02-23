package sandbox

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTarFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("hello world from tar test")
	fp := filepath.Join(dir, "testfile.txt")
	if err := os.WriteFile(fp, content, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	f, err := os.Open(fp)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}

	reader, err := createTarFromFile(f, stat)
	if err != nil {
		t.Fatalf("createTarFromFile: %v", err)
	}

	// Read back the tar archive
	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("read tar header: %v", err)
	}

	if header.Name != "testfile.txt" {
		t.Errorf("expected name 'testfile.txt', got %q", header.Name)
	}
	if header.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), header.Size)
	}

	data, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar content: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("expected content %q, got %q", content, data)
	}

	// Should be no more entries
	_, err = tr.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestCreateTarFromFile_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fp := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(fp, []byte{}, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	f, err := os.Open(fp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	reader, err := createTarFromFile(f, stat)
	if err != nil {
		t.Fatalf("createTarFromFile: %v", err)
	}

	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("read header: %v", err)
	}

	if header.Size != 0 {
		t.Errorf("expected size 0, got %d", header.Size)
	}
}

func TestCopyIn_ReturnsError(t *testing.T) {
	t.Parallel()

	sb := &DockerSandbox{
		config: SandboxConfig{Image: "alpine:latest"},
	}

	err := sb.CopyIn(context.TODO(), "/nonexistent", "/dest")
	if err == nil {
		t.Fatal("expected error from CopyIn")
	}
}

func TestCopyOut_ReturnsError(t *testing.T) {
	t.Parallel()

	sb := &DockerSandbox{
		config: SandboxConfig{Image: "alpine:latest"},
	}

	_, err := sb.CopyOut(context.TODO(), "/src")
	if err == nil {
		t.Fatal("expected error from CopyOut")
	}
}

func TestExec_EmptyCommand(t *testing.T) {
	t.Parallel()

	sb := &DockerSandbox{
		config: SandboxConfig{Image: "alpine:latest"},
	}

	_, err := sb.Exec(context.TODO(), nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestExecInContainer_EmptyCommand(t *testing.T) {
	t.Parallel()

	sb := &DockerSandbox{
		config: SandboxConfig{Image: "alpine:latest"},
	}

	_, _, err := sb.ExecInContainer(context.TODO(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestClose_NilClient(t *testing.T) {
	t.Parallel()

	sb := &DockerSandbox{}
	if err := sb.Close(); err != nil {
		t.Fatalf("Close with nil client should return nil: %v", err)
	}
}
