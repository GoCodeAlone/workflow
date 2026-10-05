//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixtureControlSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range map[string]string{
		"go.mod":    "module fixture.control\n\ngo 1.26.5\n",
		"main.go":   "package main\nimport (\"embed\"; \"fmt\")\nvar _ embed.FS\n//go:embed asset.txt\nvar asset string\nfunc main(){fmt.Print(asset)}\n",
		"asset.txt": "one",
	} {
		fixtureControlWrite(t, filepath.Join(root, name), data)
	}
	return root
}

func fixtureControlWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureControlCommand(ctx context.Context, root, output string, extra ...string) *exec.Cmd {
	args := append([]string{"build", "-o", output}, extra...)
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOENV=off", "GOFLAGS=", "GOTOOLCHAIN=go1.27.1")
	return cmd
}

// Sentinels exercise custody/publication only, never claim host/plugin execution.
func fixtureControlSentinel(cmd *exec.Cmd) ([]byte, error) {
	for i, arg := range cmd.Args {
		if arg == "-o" {
			return nil, os.WriteFile(cmd.Args[i+1], []byte("unit-artifact-sentinel"), 0700)
		}
	}
	return nil, errors.New("control command has no output")
}

func fixtureControlRequest(t *testing.T, c *fixtureBuildArtifacts, root string, extra ...string) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "executable")
	cmd := fixtureControlCommand(t.Context(), root, output, extra...)
	if out, err := c.build(t.Context(), cmd, root, output, fixtureControlSentinel); err != nil {
		t.Fatalf("control build: %v\n%s", err, out)
	}
	return output
}

func fixtureControlCounts(t *testing.T, c *fixtureBuildArtifacts, builds, hits, bypasses int) {
	t.Helper()
	got := c.stats()
	t.Logf("artifact controls builds=%d hits=%d bypasses=%d", got.builds, got.hits, got.bypasses)
	if got != (fixtureBuildCounts{builds, hits, bypasses}) {
		t.Fatalf("repeated-build contract: got builds=%d hits=%d bypasses=%d; want %d/%d/%d", got.builds, got.hits, got.bypasses, builds, hits, bypasses)
	}
}

func TestFixtureBuildArtifactsIdenticalPrivateCopies(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() {
		if err := c.close(); err != nil {
			t.Error(err)
		}
	})
	if c.root != "" {
		t.Fatal("cache must start cold and lazy")
	}
	root := fixtureControlSource(t)
	a, b := fixtureControlRequest(t, &c, root), fixtureControlRequest(t, &c, root)
	fixtureControlCounts(t, &c, 1, 1, 0)
	if a == b || strings.HasPrefix(a, c.root+string(os.PathSeparator)) {
		t.Fatal("consumer received cache path")
	}
	for _, path := range []string{a, b, c.root} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("private mode: %s: %v %v", path, info, err)
		}
	}
	entries, err := os.ReadDir(c.root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("missing private cache artifacts: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0400 {
			t.Fatalf("cache artifact not read-only regular file: %v %v", info, err)
		}
	}
	fixtureControlWrite(t, a, "mutated sibling")
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	d := fixtureControlRequest(t, &c, root)
	data, err := os.ReadFile(d)
	if err != nil || string(data) != "unit-artifact-sentinel" {
		t.Fatalf("sibling mutation poisoned reuse: %q %v", data, err)
	}
	fixtureControlCounts(t, &c, 1, 2, 0)
	cacheRoot := c.root
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned root survived close: %v", err)
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("close removed consumer: %v", err)
	}
	var next fixtureBuildArtifacts
	t.Cleanup(func() { _ = next.close() })
	fixtureControlRequest(t, &next, root)
	fixtureControlCounts(t, &next, 1, 0, 0)
}

func TestFixtureBuildArtifactsInputSeparation(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() { _ = c.close() })
	root := fixtureControlSource(t)
	fixtureControlRequest(t, &c, root)
	fixtureControlRequest(t, &c, root)
	fixtureControlCounts(t, &c, 1, 1, 0)
	for _, name := range []string{"ui/node_modules/dep/index.js", ".git/control"} {
		fixtureControlWrite(t, filepath.Join(root, name), "excluded non-input")
		fixtureControlRequest(t, &c, root)
	}
	fixtureControlCounts(t, &c, 1, 3, 0)
	for i, name := range []string{"asset.txt", "templates/template.txt", "plugin.json", "db/schema.sql", "main.go", "go.mod"} {
		path := filepath.Join(root, name)
		old, _ := os.ReadFile(path)
		fixtureControlWrite(t, path, string(old)+"\n// changed input")
		fixtureControlRequest(t, &c, root)
		fixtureControlCounts(t, &c, i+2, 3, 0)
	}
	if err := os.Chmod(filepath.Join(root, "asset.txt"), 0640); err != nil {
		t.Fatal(err)
	}
	fixtureControlRequest(t, &c, root)
	fixtureControlCounts(t, &c, 8, 3, 0)
	fixtureControlRequest(t, &c, fixtureControlSource(t))
	fixtureControlCounts(t, &c, 9, 3, 0)
}

func TestFixtureBuildArtifactsCommandEnvironmentVariants(t *testing.T) {
	for _, variant := range []string{"race", "ldflags", "api", "environment", "compiler", "launcher", "effective-config", "readonly", "argv0", "output-position"} {
		t.Run(variant, func(t *testing.T) {
			var c fixtureBuildArtifacts
			t.Cleanup(func() { _ = c.close() })
			root := fixtureControlSource(t)
			fixtureControlRequest(t, &c, root)
			output := filepath.Join(t.TempDir(), "executable")
			cmd := fixtureControlCommand(t.Context(), root, output)
			switch variant {
			case "race":
				cmd = fixtureControlCommand(t.Context(), root, output, "-race")
			case "ldflags":
				cmd = fixtureControlCommand(t.Context(), root, output, "-ldflags", "-s -w")
			case "api":
				cmd = fixtureControlCommand(t.Context(), root, output, "-ldflags", "-X main.gitHubAPIBaseURL=http://fixture.invalid")
			case "environment":
				cmd.Env = append(cmd.Env, "FIXTURE_CONTROL_VARIANT=changed")
			case "compiler":
				cmd.Env = append(cmd.Env, "GOTOOLCHAIN=go1.26.5")
			case "launcher":
				path := filepath.Join(t.TempDir(), "go-launcher")
				fixtureControlWrite(t, path, "#!/bin/sh\nexec "+cmd.Path+" \"$@\"\n")
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
				cmd.Path = path
			case "effective-config":
				path := filepath.Join(t.TempDir(), "goenv")
				fixtureControlWrite(t, path, "GOPRIVATE=fixture.control\n")
				cmd.Env = append(cmd.Env, "GOENV="+path)
			case "readonly":
				cmd.Env = append(cmd.Env, "GOFLAGS=-mod=readonly")
			case "argv0":
				cmd.Args[0] = "distinct-launcher-argv0"
			case "output-position":
				cmd.Args = []string{cmd.Args[0], "build", ".", "-o", output}
			}
			if out, err := c.build(t.Context(), cmd, root, output, fixtureControlSentinel); err != nil {
				t.Fatalf("variant build: %v\n%s", err, out)
			}
			fixtureControlCounts(t, &c, 2, 0, 0)
		})
	}
}

func TestFixtureBuildArtifactsExternalInputsBypass(t *testing.T) {
	for _, flag := range []string{"-overlay", "-modfile", "-ldflags=-s", "-gcflags=all=-N", "-tags=unknown", "native", "goenv-native", "goenv-overlay"} {
		t.Run(flag, func(t *testing.T) {
			var c fixtureBuildArtifacts
			t.Cleanup(func() { _ = c.close() })
			root := fixtureControlSource(t)
			external := filepath.Join(t.TempDir(), "external")
			for i := 0; i < 2; i++ {
				fixtureControlWrite(t, external, fmt.Sprintf("changed %d", i))
				output := filepath.Join(t.TempDir(), "executable")
				cmd := fixtureControlCommand(t.Context(), root, output)
				value := flag
				if flag == "-overlay" || flag == "-modfile" {
					value += "=" + external
				}
				switch flag {
				case "native":
					cmd.Env = append(cmd.Env, "CGO_CFLAGS=-DFIXTURE_EXTERNAL")
				case "goenv-native", "goenv-overlay":
					config := filepath.Join(filepath.Dir(external), "goenv")
					setting := "CGO_CFLAGS=-DFIXTURE_EXTERNAL\n"
					if flag == "goenv-overlay" {
						setting = "GOFLAGS=-overlay=" + external + "\n"
					}
					fixtureControlWrite(t, config, setting)
					cmd.Env = append(cmd.Env, "GOENV="+config)
					// Empty process values would mask GOENV. Remove those control defaults.
					var env []string
					for _, item := range cmd.Env {
						if !strings.HasPrefix(item, "GOFLAGS=") && !strings.HasPrefix(item, "CGO_CFLAGS=") {
							env = append(env, item)
						}
					}
					cmd.Env = env
				default:
					cmd.Env = append(cmd.Env, "GOFLAGS="+value)
				}
				wantArgs, wantEnv := strings.Join(cmd.Args, "\x00"), strings.Join(cmd.Env, "\x00")
				run := func(got *exec.Cmd) ([]byte, error) {
					if strings.Join(got.Args, "\x00") != wantArgs || strings.Join(got.Env, "\x00") != wantEnv {
						return nil, errors.New("bypass rewrote original command")
					}
					return fixtureControlSentinel(got)
				}
				if out, err := c.build(t.Context(), cmd, root, output, run); err != nil {
					t.Fatalf("bypass: %v\n%s", err, out)
				}
			}
			fixtureControlCounts(t, &c, 2, 0, 2)
			if c.root != "" {
				t.Fatal("bypass allocated cache")
			}
		})
	}
}

func TestFixtureBuildArtifactsRealOverlayMutation(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() { _ = c.close() })
	root := fixtureControlSource(t)
	external := t.TempDir()
	asset, overlay := filepath.Join(external, "asset.txt"), filepath.Join(external, "overlay.json")
	fixtureControlWrite(t, overlay, fmt.Sprintf(`{"Replace":{%q:%q}}`, filepath.Join(root, "asset.txt"), asset))
	for _, want := range []string{"first-overlay", "second-overlay"} {
		fixtureControlWrite(t, asset, want)
		output := filepath.Join(t.TempDir(), "executable")
		cmd := fixtureControlCommand(t.Context(), root, output)
		cmd.Env = append(cmd.Env, "GOFLAGS=-overlay="+overlay)
		if out, err := c.build(t.Context(), cmd, root, output, (*exec.Cmd).CombinedOutput); err != nil {
			t.Fatalf("real overlay build: %v\n%s", err, out)
		}
		got, err := exec.CommandContext(t.Context(), output).CombinedOutput()
		if err != nil || string(got) != want {
			t.Fatalf("real overlay output=%q: %v", got, err)
		}
	}
	fixtureControlCounts(t, &c, 2, 0, 2)
}

func TestFixtureBuildArtifactsFailureRetry(t *testing.T) {
	for _, failure := range []string{"failed-partial", "cancelled-partial", "symlink", "directory", "special"} {
		t.Run(failure, func(t *testing.T) {
			var c fixtureBuildArtifacts
			t.Cleanup(func() { _ = c.close() })
			root := fixtureControlSource(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			output := filepath.Join(t.TempDir(), "executable")
			cmd := fixtureControlCommand(ctx, root, output)
			partial := ""
			run := func(cmd *exec.Cmd) ([]byte, error) {
				for i, arg := range cmd.Args {
					if arg == "-o" {
						partial = cmd.Args[i+1]
					}
				}
				switch failure {
				case "failed-partial":
					_, _ = fixtureControlSentinel(cmd)
					return nil, errors.New("expected build failure")
				case "cancelled-partial":
					_, _ = fixtureControlSentinel(cmd)
					cancel()
					return nil, nil
				case "symlink":
					return nil, os.Symlink(filepath.Join(root, "asset.txt"), partial)
				case "directory":
					return nil, os.Mkdir(partial, 0700)
				case "special":
					return exec.Command("mkfifo", partial).CombinedOutput()
				}
				return nil, nil
			}
			if _, err := c.build(ctx, cmd, root, output, run); err == nil {
				t.Error("invalid/failed/cancelled artifact was published")
			}
			if _, err := os.Lstat(partial); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("partial artifact survived failed publication: %v", err)
			}
			fixtureControlRequest(t, &c, root)
			fixtureControlRequest(t, &c, root)
			fixtureControlCounts(t, &c, 2, 1, 0)
		})
	}
}

func TestFixtureBuildArtifactsConcurrentCancellation(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() { _ = c.close() })
	root := fixtureControlSource(t)
	started, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	run := func(cmd *exec.Cmd) ([]byte, error) {
		if attempts.Add(1) == 1 {
			close(started)
			<-release
		}
		return fixtureControlSentinel(cmd)
	}
	owner := filepath.Join(t.TempDir(), "owner")
	done := make(chan error, 1)
	go func() {
		_, err := c.build(t.Context(), fixtureControlCommand(t.Context(), root, owner), root, owner, run)
		done <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	waitOutput := filepath.Join(t.TempDir(), "cancelled-waiter")
	waitCmd := fixtureControlCommand(ctx, root, waitOutput)
	key, outputIndex, err := fixtureBuildKey(t.Context(), waitCmd, root)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	waiting := make(chan struct{})
	waitContext := &fixtureControlWaitContext{Context: ctx, waiting: waiting}
	go func() {
		_, err := c.buildKey(waitContext, waitCmd, key, outputIndex, waitOutput, run)
		waitDone <- err
	}()
	<-waiting
	cancel()
	if err := <-waitDone; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled waiter consumed/build artifact: %v", err)
	}
	if _, err := os.Stat(waitOutput); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cancelled waiter created executable: %v", err)
	}
	const callers = 6
	outputs := make([]string, callers)
	for i := range outputs {
		outputs[i] = filepath.Join(t.TempDir(), "waiter")
	}
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for _, output := range outputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.build(t.Context(), fixtureControlCommand(t.Context(), root, output), root, output, run)
			errs <- err
		}()
	}
	close(release)
	if err := <-done; err != nil {
		t.Error(err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("repeated-build concurrency contract: actual builds=%d; want 1", got)
	}
	fixtureControlCounts(t, &c, 1, callers, 0)
}

type fixtureControlWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *fixtureControlWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestFixtureBuildArtifactsUnknownArgumentsBypass(t *testing.T) {
	for _, args := range [][]string{{"-mod=mod"}, {"-overlay=outside.json"}, {"-modfile=outside.mod"}, {"-gcflags=all=-N"}, {"-ldflags", "-extldflags=-L/outside"}, {"-ldflags", "-importcfg /outside/config"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var c fixtureBuildArtifacts
			t.Cleanup(func() { _ = c.close() })
			root := fixtureControlSource(t)
			for i := 0; i < 2; i++ {
				output := filepath.Join(t.TempDir(), "executable")
				cmd := fixtureControlCommand(t.Context(), root, output, args...)
				want := strings.Join(cmd.Args, "\x00")
				run := func(got *exec.Cmd) ([]byte, error) {
					if strings.Join(got.Args, "\x00") != want {
						return nil, errors.New("unknown-argument bypass rewrote build")
					}
					return fixtureControlSentinel(got)
				}
				if out, err := c.build(t.Context(), cmd, root, output, run); err != nil {
					t.Fatalf("argument bypass: %v\n%s", err, out)
				}
			}
			fixtureControlCounts(t, &c, 2, 0, 2)
		})
	}
}

func TestFixtureBuildArtifactsExclusiveCopy(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() { _ = c.close() })
	root := fixtureControlSource(t)
	output := fixtureControlRequest(t, &c, root)
	cmd := fixtureControlCommand(t.Context(), root, output)
	if _, err := c.build(t.Context(), cmd, root, output, fixtureControlSentinel); err == nil {
		t.Fatal("existing consumer executable was overwritten instead of exclusive copy failure")
	}
	fixtureControlCounts(t, &c, 1, 1, 0)
}

func TestFixtureBuildArtifactsEffectiveConfigMutation(t *testing.T) {
	var c fixtureBuildArtifacts
	t.Cleanup(func() { _ = c.close() })
	root := fixtureControlSource(t)
	config := filepath.Join(t.TempDir(), "goenv")
	for i := 0; i < 2; i++ {
		fixtureControlWrite(t, config, fmt.Sprintf("GOPRIVATE=fixture%d.control\n", i))
		output := filepath.Join(t.TempDir(), "executable")
		cmd := fixtureControlCommand(t.Context(), root, output)
		cmd.Env = append(cmd.Env, "GOENV="+config)
		if out, err := c.build(t.Context(), cmd, root, output, fixtureControlSentinel); err != nil {
			t.Fatalf("effective config mutation: %v\n%s", err, out)
		}
	}
	fixtureControlCounts(t, &c, 2, 0, 0)
}

func TestFixtureBuildArtifactsOrdinaryFailureTeardown(t *testing.T) {
	if path := os.Getenv("WFCTL_FIXTURE_TEARDOWN_CONTROL"); path != "" {
		root := fixtureControlSource(t)
		fixtureControlRequest(t, &invocationFixtureBuilds, root)
		if invocationFixtureBuilds.root == "" {
			t.Fatal("lazy invocation cache was not allocated")
		}
		fixtureControlWrite(t, path, invocationFixtureBuilds.root)
		t.Error("expected ordinary failure teardown control")
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "owned-root")
	cmd := exec.CommandContext(t.Context(), self, "-test.run=^TestFixtureBuildArtifactsOrdinaryFailureTeardown$", "-test.count=1")
	cmd.Env = append(os.Environ(), "WFCTL_FIXTURE_TEARDOWN_CONTROL="+path)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "expected ordinary failure teardown control") {
		t.Fatalf("not an ordinary assertion failure: %v\n%s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("no teardown root evidence: %v", err)
	}
	if _, err := os.Lstat(string(data)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invocation root survived ordinary test failure: %v", err)
	}
}
