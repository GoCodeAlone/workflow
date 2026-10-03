package module

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type interruptedStateFile struct {
	*os.File
	phase  string
	cause  error
	events *[]string
}

func (f *interruptedStateFile) Write(data []byte) (int, error) {
	*f.events = append(*f.events, "write")
	if f.phase == "write" || f.phase == "short write" {
		n, err := f.File.Write(data[:len(data)/2])
		if err != nil {
			return n, err
		}
		if f.phase == "write" {
			return n, f.cause
		}
		return n, nil
	}
	return f.File.Write(data)
}

func (f *interruptedStateFile) Sync() error {
	*f.events = append(*f.events, "file sync")
	if f.phase == "file sync" {
		return f.cause
	}
	return f.File.Sync()
}

func (f *interruptedStateFile) Close() error {
	*f.events = append(*f.events, "close")
	err := f.File.Close()
	if f.phase == "close" {
		return f.cause
	}
	return err
}

func TestCleanupPending_AtomicWriteDurability(t *testing.T) {
	for _, phase := range []string{"success", "create", "write", "short write", "file sync", "close", "rename", "directory sync"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "database.json")
			old := []byte(`{"resource_id":"database","status":"active"}`)
			next := []byte(`{"resource_id":"database","lifecycle":{"generation":"g1","phase":"cloud_deleted_secret_cleanup_pending"}}`)
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("injected storage failure")
			var events []string
			ops := nativeIaCStateFileOps()
			native := ops
			ops.createTemp = func(parent, pattern string) (iaCStateTempFile, error) {
				events = append(events, "create")
				if parent != dir || strings.HasSuffix(pattern, ".json") {
					t.Fatalf("temporary state must share the directory and not be JSON: %q %q", parent, pattern)
				}
				if phase == "create" {
					return nil, cause
				}
				file, err := os.CreateTemp(parent, pattern)
				if err != nil {
					return nil, err
				}
				info, err := file.Stat()
				if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
					t.Fatalf("temporary record is not private: info=%v err=%v", info, err)
				}
				listed, err := NewFSIaCStateStore(dir).ListStates(t.Context(), nil)
				if err != nil || len(listed) != 1 || listed[0].ResourceID != "database" {
					t.Fatalf("in-flight temporary file exposed by ListStates: states=%v err=%v", listed, err)
				}
				return &interruptedStateFile{File: file, phase: phase, cause: cause, events: &events}, nil
			}
			ops.rename = func(from, to string) error {
				events = append(events, "rename")
				data, err := os.ReadFile(from)
				if err != nil || string(data) != string(next) {
					t.Fatalf("rename attempted before complete write: err=%v", err)
				}
				if phase == "rename" {
					return cause
				}
				return native.rename(from, to)
			}
			ops.syncDir = func(parent string) error {
				events = append(events, "directory sync")
				if parent != dir {
					t.Fatal("wrong parent directory synced")
				}
				if phase == "directory sync" {
					return cause
				}
				return native.syncDir(parent)
			}
			err := writeIaCStateFile(path, next, ops)
			switch phase {
			case "success":
				if err != nil || !reflect.DeepEqual(events, []string{"create", "write", "file sync", "close", "rename", "directory sync"}) {
					t.Fatalf("durability order: events=%v err=%v", events, err)
				}
			case "short write":
				if !errors.Is(err, io.ErrShortWrite) {
					t.Fatalf("short write accepted: %v", err)
				}
			default:
				if !errors.Is(err, cause) {
					t.Fatalf("storage failure swallowed: phase=%s err=%v", phase, err)
				}
			}
			want := old
			// After rename a directory-sync failure is uncertain durability, not
			// permission to roll back or claim the old record is still current.
			if phase == "success" || phase == "directory sync" {
				want = next
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != string(want) {
				t.Fatalf("pre-rename failure damaged old record: phase=%s err=%v", phase, readErr)
			}
			temps, globErr := filepath.Glob(filepath.Join(dir, ".iac-state-*"))
			if globErr != nil || len(temps) != 0 {
				t.Fatalf("temporary files left after return: files=%v err=%v", temps, globErr)
			}
		})
	}
}
