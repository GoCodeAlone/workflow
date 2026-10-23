//go:build unix

package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSpecialFileCannotBlockController(t *testing.T) {
	host, options := readFixture(t)
	path := filepath.Join(options.RunRoot, "installed", "references", "workflow-contract.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan ReadReport, 1)
	go func() { done <- SupportingFilePreflight(context.Background(), host, options) }()
	select {
	case result := <-done:
		if result.Passed || len(host.commands) != 0 {
			t.Fatal("special file accepted")
		}
	case <-time.After(time.Second):
		// Release the old blocking reader so the red regression leaves no goroutine.
		writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("public unblock canary")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("blocked reader did not close")
		}
		t.Fatal("special file blocked the controller before the host timeout")
	}
}
