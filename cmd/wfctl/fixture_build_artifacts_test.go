package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

type fixtureBuildCounts struct{ builds, hits, bypasses int }

type fixtureBuildArtifacts struct {
	mu      sync.Mutex
	root    string
	entries map[string]*fixtureBuildEntry
	counts  fixtureBuildCounts
}

type fixtureBuildEntry struct {
	ready chan struct{}
	path  string
	data  []byte
}

var invocationFixtureBuilds fixtureBuildArtifacts

// External native inputs are not fingerprinted, so recognized overrides bypass.
var fixtureNativeOverrideNames = []string{
	"CC", "CXX", "AR", "PKG_CONFIG",
	"CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS",
	"CPATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH", "OBJC_INCLUDE_PATH",
	"LIBRARY_PATH", "COMPILER_PATH", "GCC_EXEC_PREFIX",
	"SDKROOT", "DEVELOPER_DIR", "TOOLCHAINS",
	"PKG_CONFIG_PATH", "PKG_CONFIG_LIBDIR", "PKG_CONFIG_SYSROOT_DIR", "PKG_CONFIG_TOP_BUILD_DIR",
	"LD_LIBRARY_PATH", "LD_PRELOAD", "LD_AUDIT", "LD_RUN_PATH",
	"DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH", "DYLD_FALLBACK_LIBRARY_PATH",
	"DYLD_FALLBACK_FRAMEWORK_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_ROOT_PATH",
}

// Only the fixed committed fixture callers opt in. Modules, native tools and
// VCS state must remain immutable for this invocation; the suite driver checks
// them before/after. Unclosed inputs take the original command, unchanged.
func (c *fixtureBuildArtifacts) build(ctx context.Context, cmd *exec.Cmd, sourceRoot, output string, run func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, outputIndex, err := fixtureBuildKey(ctx, cmd, sourceRoot)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.counts.builds++
		c.counts.bypasses++
		c.mu.Unlock()
		return run(cmd)
	}
	return c.buildKey(ctx, cmd, key, outputIndex, output, run)
}

func (c *fixtureBuildArtifacts) buildKey(ctx context.Context, cmd *exec.Cmd, key string, outputIndex int, output string, run func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		entry := c.entries[key]
		if entry != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-entry.ready:
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if entry.path == "" {
				continue
			}
			c.mu.Lock()
			c.counts.hits++
			c.mu.Unlock()
			return entry.data, copyFixtureBuildBinary(ctx, entry.path, output)
		}
		if c.root == "" {
			root, err := os.MkdirTemp("", "wfctl-fixture-builds-")
			if err != nil {
				c.mu.Unlock()
				return nil, err
			}
			c.root = root
			c.entries = make(map[string]*fixtureBuildEntry)
		}
		entry = &fixtureBuildEntry{ready: make(chan struct{})}
		c.entries[key] = entry
		root := c.root
		c.mu.Unlock()

		data, buildErr := c.publish(ctx, cmd, outputIndex, root, key, run)
		c.mu.Lock()
		if buildErr == nil {
			entry.path, entry.data = filepath.Join(root, key), data
		} else {
			delete(c.entries, key)
		}
		close(entry.ready)
		c.mu.Unlock()
		if buildErr != nil {
			return data, buildErr
		}
		return data, copyFixtureBuildBinary(ctx, entry.path, output)
	}
}

func (c *fixtureBuildArtifacts) publish(ctx context.Context, cmd *exec.Cmd, outputIndex int, root, key string, run func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	stage, err := os.MkdirTemp(root, "build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	artifact := filepath.Join(stage, "artifact")
	args := cmd.Args
	cmd.Args = append([]string(nil), args...)
	cmd.Args[outputIndex] = artifact
	defer func() { cmd.Args = args }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.counts.builds++
	c.mu.Unlock()
	data, err := run(cmd)
	if err != nil {
		return data, err
	}
	if err := ctx.Err(); err != nil {
		return data, err
	}
	info, err := os.Lstat(artifact)
	if err != nil {
		return data, err
	}
	if !info.Mode().IsRegular() {
		return data, errors.New("fixture build output is not a regular file")
	}
	if err := os.Chmod(artifact, 0400); err != nil {
		return data, err
	}
	path := filepath.Join(root, key)
	if err := os.Rename(artifact, path); err != nil {
		return data, err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(path)
		return data, err
	}
	return data, nil
}

func (c *fixtureBuildArtifacts) stats() fixtureBuildCounts {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts
}

func (c *fixtureBuildArtifacts) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == "" {
		return nil
	}
	err := os.RemoveAll(c.root)
	c.root, c.entries = "", nil
	return err
}

func buildFixtureArtifact(t *testing.T, ctx context.Context, cmd *exec.Cmd, sourceRoot, output string) ([]byte, error) {
	t.Helper()
	data, err := invocationFixtureBuilds.build(ctx, cmd, sourceRoot, output, (*exec.Cmd).CombinedOutput)
	counts := invocationFixtureBuilds.stats()
	t.Logf("fixture artifacts: builds=%d hits=%d bypasses=%d", counts.builds, counts.hits, counts.bypasses)
	return data, err
}

// Share the existing pipeline host O_EXCL/0700 copy mechanism, also for cache
// consumers. Wrapping the reader charges copying to the caller's context.
func copyFixtureBuildBinary(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, fixtureContextReader{ctx, input})
	err = errors.Join(copyErr, output.Chmod(0700), output.Close(), ctx.Err())
	if err != nil {
		_ = os.Remove(destination)
	}
	return err
}

type fixtureContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r fixtureContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func fixtureBuildKey(ctx context.Context, cmd *exec.Cmd, sourceRoot string) (string, int, error) {
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", 0, err
	}
	cwd := cmd.Dir
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return "", 0, err
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", 0, err
	}
	if len(cmd.Args) < 2 || cmd.Args[1] != "build" {
		return "", 0, errors.New("not a fixture build")
	}
	args := []string{cmd.Args[0], "build"}
	outputIndex := 0
	for i := 2; i < len(cmd.Args); i++ {
		arg := cmd.Args[i]
		switch {
		case arg == "-o" && outputIndex == 0 && i+1 < len(cmd.Args):
			args = append(args, arg)
			i++
			outputIndex = i
		case arg == "-race" || arg == "-p=2":
			args = append(args, arg)
		case arg == "-ldflags" && i+1 < len(cmd.Args):
			i++
			flags := cmd.Args[i]
			fields := strings.Fields(flags)
			for j := 0; j < len(fields); j++ {
				switch fields[j] {
				case "-s", "-w":
				case "-X":
					j++
					if j >= len(fields) || !strings.HasPrefix(fields[j], "main.gitHubAPIBaseURL=") {
						return "", 0, errors.New("unclosed linker inputs")
					}
				default:
					return "", 0, errors.New("unclosed linker inputs")
				}
			}
			args = append(args, arg, flags)
		case !strings.HasPrefix(arg, "-"):
			args = append(args, arg)
		default:
			return "", 0, errors.New("unclosed build flags")
		}
	}
	if outputIndex == 0 {
		return "", 0, errors.New("missing fixture output")
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	values := make(map[string]string)
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if ok {
			values[name] = value
		}
	}
	for _, name := range fixtureNativeOverrideNames {
		if values[name] != "" {
			return "", 0, errors.New("external native override")
		}
	}
	probe := exec.CommandContext(ctx, cmd.Path, "env", "-json")
	probe.Dir, probe.Env, probe.WaitDelay = cmd.Dir, cmd.Env, cmd.WaitDelay
	data, err := probe.Output()
	if err != nil {
		return "", 0, err
	}
	var effective map[string]string
	if err := json.Unmarshal(data, &effective); err != nil {
		return "", 0, err
	}
	if flags := effective["GOFLAGS"]; flags != "" && flags != "-mod=readonly" {
		return "", 0, errors.New("unclosed effective Go flags")
	}
	if work := effective["GOWORK"]; work != "" && work != "off" {
		return "", 0, errors.New("external workspace")
	}
	for name, want := range map[string]string{"AR": "ar", "PKG_CONFIG": "pkg-config", "CGO_CFLAGS": "-O2 -g", "CGO_CPPFLAGS": "", "CGO_CXXFLAGS": "-O2 -g", "CGO_FFLAGS": "-O2 -g", "CGO_LDFLAGS": "-O2 -g"} {
		if effective[name] != want {
			return "", 0, errors.New("external effective native override")
		}
	}
	if cc := effective["CC"]; cc != "cc" && cc != "gcc" && cc != "clang" {
		return "", 0, errors.New("external effective compiler")
	}
	if cxx := effective["CXX"]; cxx != "c++" && cxx != "g++" && cxx != "clang++" {
		return "", 0, errors.New("external effective compiler")
	}
	// GOGCCFLAGS contains a fresh go-build temp path, not an independent input.
	delete(effective, "GOGCCFLAGS")
	config := []byte(nil)
	if path := effective["GOENV"]; path != "" && path != "off" {
		config, err = os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", 0, err
		}
	}
	normalizedEnv := make([]string, 0, len(values))
	for name, value := range values {
		normalizedEnv = append(normalizedEnv, name+"="+value)
	}
	sort.Strings(normalizedEnv)
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	if err := encoder.Encode(struct {
		Root, Cwd string
		Args, Env []string
		Effective map[string]string
		Config    []byte
	}{root, cwd, args, normalizedEnv, effective, config}); err != nil {
		return "", 0, err
	}
	launcher, err := filepath.Abs(cmd.Path)
	if err != nil {
		return "", 0, err
	}
	for _, path := range []string{launcher, filepath.Join(effective["GOROOT"], "bin", "go"), filepath.Join(effective["GOTOOLDIR"], "compile"), filepath.Join(effective["GOTOOLDIR"], "link")} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", 0, err
		}
		if err := fixtureHashFile(ctx, encoder, hash, resolved, resolved); err != nil {
			return "", 0, err
		}
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || rel == filepath.Join("ui", "node_modules") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		return fixtureHashFile(ctx, encoder, hash, path, rel)
	})
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), outputIndex, ctx.Err()
}

func fixtureHashFile(ctx context.Context, encoder *json.Encoder, hash io.Writer, path, name string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("unclosed nonregular source input")
	}
	if err := encoder.Encode(struct {
		Name string
		Mode fs.FileMode
		Size int64
	}{name, info.Mode(), info.Size()}); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(hash, fixtureContextReader{ctx, file})
	return errors.Join(copyErr, file.Close(), ctx.Err())
}

func closeInvocationFixtureBuilds() error {
	counts := invocationFixtureBuilds.stats()
	if counts != (fixtureBuildCounts{}) {
		fmt.Printf("fixture artifacts final: builds=%d hits=%d bypasses=%d\n", counts.builds, counts.hits, counts.bypasses)
	}
	return invocationFixtureBuilds.close()
}
