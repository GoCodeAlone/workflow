//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPipelineCleanupStateLockAndPrivateFiles(t *testing.T) {
	root := filepath.Join(cleanupTestDirectory(t), "cleanup")
	state, err := openPipelineCleanupState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if other, err := openPipelineCleanupState(root); err == nil {
		other.Close()
		t.Fatal("a second parent acquired the execution cleanup lock")
	}
	entry := cleanupTestEntry(t)
	if err := state.Save(entry); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{root: 0700, filepath.Join(root, "lock"): 0600, filepath.Join(root, entry.Label+".json"): 0600} {
		info, err := os.Lstat(name)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("cleanup custody permissions for %s: %v, %v", name, info, err)
		}
	}
	entries, err := state.Entries()
	if err != nil || len(entries) != 1 || entries[0].Label != entry.Label {
		t.Fatalf("retained journal was not read back: %#v, %v", entries, err)
	}
	if err := state.Delete(entry.Label); err != nil {
		t.Fatal(err)
	}
	entries, err = state.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("journal deletion was not durable: %#v, %v", entries, err)
	}
}

func TestPipelineCleanupStateRejectsUnsafeCustody(t *testing.T) {
	for _, kind := range []string{"root mode", "root symlink", "lock symlink", "record mode", "record symlink", "unknown field", "duplicate field", "truncated", "unexpected file"} {
		t.Run(kind, func(t *testing.T) {
			base := cleanupTestDirectory(t)
			root := filepath.Join(base, "cleanup")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "root mode":
				if err := os.Chmod(root, 0755); err != nil {
					t.Fatal(err)
				}
			case "root symlink":
				root = filepath.Join(base, "linked")
				if err := os.Symlink(filepath.Join(base, "cleanup"), root); err != nil {
					t.Fatal(err)
				}
			case "lock symlink":
				if err := os.Symlink(filepath.Join(base, "foreign"), filepath.Join(root, "lock")); err != nil {
					t.Fatal(err)
				}
			}
			state, err := openPipelineCleanupState(root)
			if kind == "root mode" || kind == "root symlink" || kind == "lock symlink" {
				if err == nil {
					state.Close()
					t.Fatal("accepted unsafe cleanup custody")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			entry := cleanupTestEntry(t)
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, entry.Label+".json")
			switch kind {
			case "unknown field":
				data = append(data[:len(data)-1], []byte(`,"foreign":true}`)...)
			case "duplicate field":
				data = append(data[:len(data)-1], []byte(`,"version":1}`)...)
			case "truncated":
				data = data[:len(data)/2]
			case "unexpected file":
				path = filepath.Join(root, "unowned")
			case "record symlink":
				foreign := filepath.Join(base, "foreign")
				if err := os.WriteFile(foreign, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign, path); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "record symlink" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "record mode" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := state.Entries(); err == nil {
				t.Fatal("accepted an unsafe retained cleanup record")
			}
		})
	}
}

func TestPipelineCleanupStateRejectsFIFO(t *testing.T) {
	for _, kind := range []string{"journal", "cidfile"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(cleanupTestDirectory(t), "cleanup")
			state, err := openPipelineCleanupState(root)
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			entry := cleanupTestEntry(t)
			name := entry.Label + ".json"
			if kind == "cidfile" {
				name = entry.Label + "-0.cid"
				entry.CIDFiles = []string{name}
				if err := state.Save(entry); err != nil {
					t.Fatal(err)
				}
			}
			if err := unix.Mkfifo(filepath.Join(root, name), 0600); err != nil {
				t.Fatal(err)
			}
			if err := state.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPipelineCleanupFIFOHelper$")
			cmd.Env = append(os.Environ(), "WFCTL_PIPELINE_FIFO_HELPER=1", "WFCTL_PIPELINE_FIFO_ROOT="+root)
			cmd.WaitDelay = 250 * time.Millisecond
			output, err := cmd.CombinedOutput()
			if !bytes.Contains(output, []byte("fifo-ready\n")) {
				t.Fatalf("FIFO helper did not reach the actual journal read: %v, %s", err, output)
			}
			if ctx.Err() != nil {
				t.Fatalf("%s FIFO open blocked instead of failing closed; helper killed and reaped: %v", kind, err)
			}
			if err != nil || !bytes.Contains(output, []byte("fifo-rejected\n")) {
				t.Fatalf("FIFO custody was not rejected: %v, %s", err, output)
			}
		})
	}
}

func TestPipelineCleanupFIFOHelper(t *testing.T) {
	if os.Getenv("WFCTL_PIPELINE_FIFO_HELPER") != "1" {
		return
	}
	state, err := openPipelineCleanupState(os.Getenv("WFCTL_PIPELINE_FIFO_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	fmt.Fprintln(os.Stdout, "fifo-ready")
	if _, err := state.Entries(); err == nil {
		t.Fatal("accepted FIFO custody")
	}
	fmt.Fprintln(os.Stdout, "fifo-rejected")
}

func TestPipelineProcessStartToken(t *testing.T) {
	first, err := pipelineProcessStartToken(os.Getpid())
	if err != nil || first == "" {
		t.Fatalf("missing process identity: %q, %v", first, err)
	}
	second, err := pipelineProcessStartToken(os.Getpid())
	if err != nil || second != first {
		t.Fatalf("unstable process identity: %q, %v", second, err)
	}
	active, err := pipelineProcessGroupActive(syscall.Getpgrp())
	if err != nil || !active {
		t.Fatalf("current group missing from process census: %v, %v", active, err)
	}
}

func cleanupTestEntry(t *testing.T) pipelineCleanupEntry {
	t.Helper()
	token, err := pipelineProcessStartToken(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return pipelineCleanupEntry{Version: 1, Label: "wfctl-0123456789abcdef0123456789abcdef",
		Parent:   pipelineProcessIdentity{PID: os.Getpid(), Start: token},
		Docker:   pipelineDockerIdentity{Endpoint: "unix:///fixture/docker.sock", TLSHash: "none", ServerID: "fixture-A"},
		Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
}

func cleanupTestDirectory(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
