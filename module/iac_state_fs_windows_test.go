package module

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCleanupPending_WindowsReplacementFlagsAndErrors(t *testing.T) {
	for _, cause := range []error{nil, windows.ERROR_ACCESS_DENIED, windows.ERROR_NOT_SUPPORTED} {
		called := false
		err := replaceIaCStateFileWith("old.tmp", "state.json", func(from, to *uint16, flags uint32) error {
			called = true
			if windows.UTF16PtrToString(from) != "old.tmp" || windows.UTF16PtrToString(to) != "state.json" {
				t.Fatal("replacement lost exact paths")
			}
			if flags != windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH {
				t.Fatalf("durable same-volume replacement flags missing or unsafe: %#x", flags)
			}
			return cause
		})
		if !called || !errors.Is(err, cause) {
			t.Fatalf("native replacement error lost: called=%t err=%v cause=%v", called, err, cause)
		}
	}
	called := false
	err := replaceIaCStateFileWith("bad\x00path", "state.json", func(*uint16, *uint16, uint32) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatal("invalid path reached native replacement")
	}
}

func TestCleanupPending_WindowsNativeDurableReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, value := range []string{"old-state", "new-state"} {
		if err := WriteIaCStateFile(path, []byte(value)); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != value {
			t.Fatalf("native replacement failed: err=%v", err)
		}
	}
	blocked := filepath.Join(dir, "blocked.json")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteIaCStateFile(blocked, []byte("must-not-replace-directory")); err == nil {
		t.Fatal("native replacement failure was ignored")
	}
	info, err := os.Stat(blocked)
	if err != nil || !info.IsDir() {
		t.Fatal("failed replacement damaged target")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary records remain after failed replacement: err=%v", err)
	}
}
