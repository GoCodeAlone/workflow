package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The fixture replaces only the command under test. exec keeps the sleep at
// the recorded PID so CommandContext must terminate and reap that exact child.
func docsCancellationCommand(t *testing.T, name, prefix string) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	script := "#!/bin/sh\n" + prefix + "\necho $$ > '" + marker + "'\nexec /bin/sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func cancelDocsAfterChildStarts(t *testing.T, marker string, run func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("command returned before child started: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("child never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("cancelled command did not return")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child %d was not reaped: %v", pid, err)
	}
	t.Logf("owned child %d terminated and reaped", pid)
}

func TestDocsCancellationGoListChild(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(fmt.Sprint(multiple), func(t *testing.T) {
			marker := docsCancellationCommand(t, "go", "")
			cancelDocsAfterChildStarts(t, marker, func(ctx context.Context) error {
				if multiple {
					_, err := goListAPIPackages(ctx, t.TempDir())
					return err
				}
				_, err := goListAPIPackage(ctx, t.TempDir(), "example.com/fixture", ".")
				return err
			})
		})
	}
}

func TestDocsCancellationRefreshPreservesCache(t *testing.T) {
	cache := t.TempDir()
	dest := filepath.Join(cache, "fixture")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dest, "keep")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := docsCancellationCommand(t, "git", "")
	manifest := &RegistryManifest{Name: "fixture", Repository: "https://github.com/GoCodeAlone/fixture", Version: "v1.0.0"}
	cancelDocsAfterChildStarts(t, marker, func(ctx context.Context) error {
		_, err := checkoutDocsPluginRepo(ctx, manifest, cache, false)
		return err
	})
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "unchanged" {
		t.Fatalf("cancelled refresh changed cache: %q, %v", got, err)
	}
}

func docsCancellationOptions(t *testing.T) docsGenerateAPIOptions {
	t.Helper()
	return docsGenerateAPIOptions{Source: t.TempDir(), Out: filepath.Join(t.TempDir(), "output"), Module: "example.com/fixture", Version: "v1.0.0", Packages: "first,second", Subjects: "workflow"}
}

func docsSuccessfulFirstPackage(t *testing.T, opts docsGenerateAPIOptions) string {
	t.Helper()
	dir := filepath.Join(opts.Source, "first")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "first.go"), []byte("// Package first is a fixture.\npackage first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(goListPackage{Dir: dir, ImportPath: opts.Module + "/first", Name: "first", GoFiles: []string{"first.go"}})
	if err != nil {
		t.Fatal(err)
	}
	return "if [ \"$3\" = 'example.com/fixture/first' ]; then\nprintf '%s' '" + string(data) + "'\nexit 0\nfi"
}

func assertNoDocsOutput(t *testing.T, out string) {
	t.Helper()
	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			t.Errorf("unexpected output after cancellation: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocsCancellationPreCancelledInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts := docsCancellationOptions(t)
	err := runDocsContext(ctx, []string{"generate", "--source", opts.Source, "--out", opts.Out, "--module", opts.Module, "--version", opts.Version, "--packages", opts.Packages})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if _, err := os.Stat(opts.Out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-cancelled invocation created output: %v", err)
	}
}

func TestDocsCancellationAfterSuccessfulPackage(t *testing.T) {
	opts := docsCancellationOptions(t)
	marker := docsCancellationCommand(t, "go", docsSuccessfulFirstPackage(t, opts))
	cancelDocsAfterChildStarts(t, marker, func(ctx context.Context) error { return runDocsGenerateAPIContext(ctx, opts) })
	assertNoDocsOutput(t, opts.Out)
}

func TestDocsCancellationPluginIsFatal(t *testing.T) {
	opts := docsCancellationOptions(t)
	opts.Subjects = "workflow,plugins"
	opts.Packages = "first"
	opts.CacheDir = t.TempDir()
	dest := filepath.Join(opts.CacheDir, "fixture")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "go.mod"), []byte("module example.com/plugin\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts.Registry = filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(opts.Registry, []byte(`[{"name":"fixture","repository":"https://github.com/GoCodeAlone/fixture","version":"v1.0.0"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	docsCancellationCommand(t, "git", "if [ \"$1\" = remote ]; then echo https://github.com/GoCodeAlone/fixture; fi\nexit 0")
	marker := docsCancellationCommand(t, "go", docsSuccessfulFirstPackage(t, opts))
	cancelDocsAfterChildStarts(t, marker, func(ctx context.Context) error { return runDocsGenerateAPIContext(ctx, opts) })
	assertNoDocsOutput(t, opts.Out)
}

func TestDocsCancellationOrdinaryErrorRemainsWarning(t *testing.T) {
	opts := docsCancellationOptions(t)
	docsCancellationCommand(t, "go", docsSuccessfulFirstPackage(t, opts)+"\necho ordinary-package-error >&2\nexit 1")
	if err := runDocsGenerateAPIContext(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(opts.Out, "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta docsAPIMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Packages) != 1 || len(meta.Warnings) != 1 || !strings.Contains(meta.Warnings[0], "ordinary-package-error") {
		t.Fatalf("unexpected normal result: %+v", meta)
	}
}
