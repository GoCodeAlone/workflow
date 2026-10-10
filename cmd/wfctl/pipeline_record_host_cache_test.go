package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var pipelineRecordHostSourceCache pipelineRecordHostCache

type pipelineRecordHostCacheKey struct {
	fingerprint  string
	directory    string
	compiler     string
	compilerInfo os.FileInfo
	digest       [sha256.Size]byte
	environment  []string
}

func pipelineRecordHostCacheKeyForCommand(cmd *exec.Cmd) (pipelineRecordHostCacheKey, error) {
	var key pipelineRecordHostCacheKey
	directory := cmd.Dir
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return key, err
		}
	}
	var err error
	key.directory, err = filepath.Abs(directory)
	if err != nil {
		return key, err
	}
	key.compiler, err = filepath.Abs(cmd.Path)
	if err != nil {
		return key, err
	}
	key.compiler, err = filepath.EvalSymlinks(key.compiler)
	if err != nil {
		return key, err
	}
	compiler, err := os.Open(key.compiler)
	if err != nil {
		return key, err
	}
	defer compiler.Close()
	key.compilerInfo, err = compiler.Stat()
	if err != nil {
		return key, err
	}
	if !key.compilerInfo.Mode().IsRegular() {
		return key, errors.New("compiler must be a regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, compiler); err != nil {
		return key, err
	}
	copy(key.digest[:], digest.Sum(nil))
	key.environment = cmd.Environ()
	slices.Sort(key.environment)
	return key, nil
}

func (key pipelineRecordHostCacheKey) matches(other pipelineRecordHostCacheKey) bool {
	return key.fingerprint == other.fingerprint && key.directory == other.directory && key.compiler == other.compiler && key.digest == other.digest &&
		os.SameFile(key.compilerInfo, other.compilerInfo) && key.compilerInfo.Mode() == other.compilerInfo.Mode() &&
		key.compilerInfo.ModTime().Equal(other.compilerInfo.ModTime()) && slices.Equal(key.environment, other.environment)
}

type pipelineRecordHostCache struct {
	mu        sync.Mutex
	key       pipelineRecordHostCacheKey
	directory string
	load      func() (string, error)
	closed    bool
}

// Actual source requests retain the accepted fixture input guard. Unclosed
// inputs use the original command directly, without entering either cache.
func (cache *pipelineRecordHostCache) buildCommand(ctx context.Context, cmd *exec.Cmd, sourceRoot, output string, run func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fingerprint, outputIndex, err := fixtureBuildKey(ctx, cmd, sourceRoot)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return run(cmd)
	}
	key, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return run(cmd)
	}
	key.fingerprint = fingerprint
	var data []byte
	err = cache.build(ctx, key, output, func(destination string) error {
		args := cmd.Args
		cmd.Args = slices.Clone(args)
		cmd.Args[outputIndex] = destination
		defer func() { cmd.Args = args }()
		var err error
		data, err = run(cmd)
		return err
	})
	return data, err
}

func (cache *pipelineRecordHostCache) build(ctx context.Context, key pipelineRecordHostCacheKey, output string, build func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if cache.closed {
		return os.ErrClosed
	}
	if cache.load == nil {
		cache.key = key
		cache.key.environment = slices.Clone(key.environment)
		cache.load = sync.OnceValues(func() (string, error) {
			var err error
			cache.directory, err = os.MkdirTemp("", "wfctl-record-host-cache-")
			if err != nil {
				return "", err
			}
			binary := filepath.Join(cache.directory, "wfctl")
			if err := build(binary); err != nil {
				return "", err
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return binary, nil
		})
	}
	if !cache.key.matches(key) {
		return build(output)
	}
	binary, err := cache.load()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return copyFixtureBuildBinary(ctx, binary, output)
}

func (cache *pipelineRecordHostCache) close() error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.closed = true
	if cache.directory == "" {
		return nil
	}
	return os.RemoveAll(cache.directory)
}

func pipelineRecordHostCacheFixture(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	root := t.TempDir()
	compiler := filepath.Join(root, "compiler")
	if err := os.WriteFile(compiler, []byte("compiler identity fixture, not an executable\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := &exec.Cmd{Path: compiler, Dir: root, Env: []string{
		"GOWORK=on", "GORACE=halt_on_error=1", "HOST_CACHE_CUSTOM=original", "GOWORK=off",
	}}
	return cmd, root
}

func TestPipelineRecordHostCacheLazyReuseAndCleanup(t *testing.T) {
	cmd, root := pipelineRecordHostCacheFixture(t)
	key, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var cache pipelineRecordHostCache
	t.Cleanup(func() {
		if err := cache.close(); err != nil {
			t.Error(err)
		}
	})
	if cache.directory != "" {
		t.Fatal("unused cache eagerly created a directory")
	}
	builds := 0
	want := []byte("cache helper sentinel: not runtime host proof\n")
	build := func(output string) error {
		builds++
		return os.WriteFile(output, want, 0600)
	}
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, output := range []string{first, second} {
		if err := cache.build(t.Context(), key, output, build); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(output)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("copied host bytes = %q, %v", got, err)
		}
		info, err := os.Stat(output)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Fatalf("copied host mode = %o, want 0700", info.Mode().Perm())
		}
	}
	if builds != 1 {
		t.Fatalf("identical source host requests built %d times, want 1", builds)
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(firstInfo, secondInfo) {
		t.Fatal("per-test host destinations share one inode")
	}
	owned := cache.directory
	if owned == "" {
		t.Fatal("used cache has no owned directory")
	}
	if err := cache.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned cache directory survived cleanup: %v", err)
	}
	for _, retained := range []string{cmd.Path, first, second} {
		if _, err := os.Stat(retained); err != nil {
			t.Fatalf("cache cleanup removed unrelated path %s: %v", retained, err)
		}
	}
	if err := cache.build(t.Context(), key, filepath.Join(root, "after-close"), build); err == nil {
		t.Fatal("closed cache supplied a binary")
	}
}

func TestPipelineRecordHostCacheChangedInputsBuildIndependently(t *testing.T) {
	for _, change := range []string{
		"cwd", "compiler-path", "compiler-bytes", "environment", "environment-unset",
		"GOFLAGS", "GOTOOLCHAIN", "GORACE", "PATH", "HOME",
	} {
		t.Run(change, func(t *testing.T) {
			cmd, root := pipelineRecordHostCacheFixture(t)
			key, err := pipelineRecordHostCacheKeyForCommand(cmd)
			if err != nil {
				t.Fatal(err)
			}
			var cache pipelineRecordHostCache
			t.Cleanup(func() {
				if err := cache.close(); err != nil {
					t.Error(err)
				}
			})
			builds := 0
			build := func(output string) error {
				builds++
				return os.WriteFile(output, []byte{byte(builds)}, 0600)
			}
			if err := cache.build(t.Context(), key, filepath.Join(root, "baseline"), build); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "cwd":
				cmd.Dir = t.TempDir()
			case "compiler-path":
				cmd.Path = filepath.Join(root, "different-compiler")
				if err := os.WriteFile(cmd.Path, []byte("compiler identity fixture, not an executable\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "compiler-bytes":
				info, err := os.Stat(cmd.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cmd.Path, []byte("modified identity fixture, not an executable\n"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(cmd.Path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "environment":
				cmd.Env = append(cmd.Env, "HOST_CACHE_CUSTOM=changed")
			case "environment-unset":
				cmd.Env = slices.DeleteFunc(cmd.Env, func(value string) bool { return value == "HOST_CACHE_CUSTOM=original" })
			case "GOFLAGS", "GOTOOLCHAIN", "GORACE", "PATH", "HOME":
				cmd.Env = append(cmd.Env, change+"=override")
			}
			changed, err := pipelineRecordHostCacheKeyForCommand(cmd)
			if err != nil {
				t.Fatal(err)
			}
			for _, output := range []string{"changed-first", "changed-second"} {
				if err := cache.build(t.Context(), changed, filepath.Join(root, output), build); err != nil {
					t.Fatal(err)
				}
			}
			if builds != 3 {
				t.Fatalf("changed %s reused or replaced the baseline cache: builds = %d, want 3", change, builds)
			}
			if err := cache.build(t.Context(), key, filepath.Join(root, "baseline-again"), build); err != nil || builds != 3 {
				t.Fatalf("changed input displaced cached baseline: builds = %d, error = %v", builds, err)
			}
		})
	}
}

func TestPipelineRecordHostCacheEffectiveEnvironment(t *testing.T) {
	cmd, root := pipelineRecordHostCacheFixture(t)
	key, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	before := slices.Clone(cmd.Env)
	cmd.Env = []string{"HOST_CACHE_CUSTOM=original", "GOWORK=off", "GORACE=halt_on_error=1"}
	reordered, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var cache pipelineRecordHostCache
	t.Cleanup(func() {
		if err := cache.close(); err != nil {
			t.Error(err)
		}
	})
	builds := 0
	build := func(output string) error {
		builds++
		return os.WriteFile(output, []byte("unit sentinel"), 0600)
	}
	if err := cache.build(t.Context(), key, filepath.Join(root, "first"), build); err != nil {
		t.Fatal(err)
	}
	if err := cache.build(t.Context(), reordered, filepath.Join(root, "second"), build); err != nil || builds != 1 {
		t.Fatalf("equivalent effective environment did not reuse: builds = %d, error = %v", builds, err)
	}
	cmd.Env = before
	if _, err := pipelineRecordHostCacheKeyForCommand(cmd); err != nil || !slices.Equal(cmd.Env, before) {
		t.Fatalf("key resolution mutated compiler environment: %v", err)
	}
}

func TestPipelineRecordHostCacheFailureAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "build-error", true: "build-canceled"}[canceled], func(t *testing.T) {
			cmd, root := pipelineRecordHostCacheFixture(t)
			key, err := pipelineRecordHostCacheKeyForCommand(cmd)
			if err != nil {
				t.Fatal(err)
			}
			var cache pipelineRecordHostCache
			t.Cleanup(func() {
				if err := cache.close(); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wantErr := errors.New("compiler failed")
			builds := 0
			build := func(output string) error {
				builds++
				if err := os.WriteFile(output, []byte("partial compiler output"), 0600); err != nil {
					return err
				}
				if canceled {
					cancel()
					return nil
				}
				return wantErr
			}
			if canceled {
				wantErr = context.Canceled
			}
			for index, requestCtx := range []context.Context{ctx, t.Context()} {
				output := filepath.Join(root, []string{"first", "second"}[index])
				if err := cache.build(requestCtx, key, output, build); !errors.Is(err, wantErr) {
					t.Fatalf("failed build error = %v, want %v", err, wantErr)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed build supplied partial host bytes: %v", err)
				}
			}
			if builds != 1 {
				t.Fatalf("failed identical cache build retried %d times", builds)
			}
		})
	}
}

func TestPipelineRecordHostCacheConcurrentRequests(t *testing.T) {
	cmd, root := pipelineRecordHostCacheFixture(t)
	key, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var cache pipelineRecordHostCache
	t.Cleanup(func() {
		if err := cache.close(); err != nil {
			t.Error(err)
		}
	})
	var builds atomic.Int32
	build := func(output string) error {
		builds.Add(1)
		return os.WriteFile(output, []byte("concurrent unit sentinel"), 0600)
	}
	var group sync.WaitGroup
	errorsOut := make(chan error, 4)
	for _, name := range []string{"one", "two", "three", "four"} {
		group.Go(func() {
			errorsOut <- cache.build(t.Context(), key, filepath.Join(root, name), build)
		})
	}
	group.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Error(err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("concurrent identical requests built %d times", builds.Load())
	}
}

func TestPipelineRecordHostCacheCanceledRequestStaysLazy(t *testing.T) {
	cmd, root := pipelineRecordHostCacheFixture(t)
	key, err := pipelineRecordHostCacheKeyForCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var cache pipelineRecordHostCache
	builds := 0
	err = cache.build(ctx, key, filepath.Join(root, "canceled"), func(string) error {
		builds++
		return nil
	})
	if !errors.Is(err, context.Canceled) || builds != 0 || cache.directory != "" {
		t.Fatalf("canceled request initialized cache: error=%v, builds=%d, directory=%q", err, builds, cache.directory)
	}
}

func TestPipelineRecordHostCacheProcessCleanup(t *testing.T) {
	if record := os.Getenv("WFCTL_RECORD_HOST_CACHE_CLEANUP_PROBE"); record != "" {
		cmd, root := pipelineRecordHostCacheFixture(t)
		key, err := pipelineRecordHostCacheKeyForCommand(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if err := pipelineRecordHostSourceCache.build(t.Context(), key, filepath.Join(root, "copy"), func(output string) error {
			return os.WriteFile(output, []byte("process cleanup helper sentinel"), 0600)
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(record, []byte(pipelineRecordHostSourceCache.directory), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	record := filepath.Join(root, "owned-directory")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestPipelineRecordHostCacheProcessCleanup$", "-test.count=1")
	cmd.Env = append(os.Environ(), "WFCTL_RECORD_HOST_CACHE_CLEANUP_PROBE="+record)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cache cleanup subprocess failed: %v\n%s", err, data)
	}
	owned, err := os.ReadFile(record)
	if err != nil || len(owned) == 0 {
		t.Fatalf("cleanup subprocess did not identify owned cache: %v", err)
	}
	if _, err := os.Stat(string(owned)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("TestMain did not remove its process cache before exit: %v", err)
	}
}
