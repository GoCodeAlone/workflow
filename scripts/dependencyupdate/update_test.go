//go:build unix

package dependencyupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests exercise module selection and consumer compilation with real Go
// commands and a local file proxy. They do not contact a registry.
func TestUpdateGoDependenciesLocalProxy(t *testing.T) {
	suiteCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	script, err := filepath.Abs("../update-go-dependencies.sh")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, current, required, wantDirect, wantIndirect string
		blanketUpgrade                                    bool
	}{
		{"preserve_unrequested_indirect", "v1.0.0", "v0.1.0", "v1.1.0", "v0.1.0", false},
		{"allow_required_indirect_upgrade", "v1.0.0", "v0.1.1", "v1.1.0", "v0.1.1", false},
		{"preserve_newer_prerelease", "v1.2.0-rc.1", "v0.1.0", "v1.2.0-rc.1", "v0.1.0", false},
		{"no_eligible_targets", "", "v0.1.0", "", "", false},
		{"blanket_upgrade_breaks_indirect_api", "v1.0.0", "v0.1.0", "v1.1.0", "v0.2.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := suiteCtx.Err(); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			proxy := filepath.Join(root, "proxy")
			writeProxyModule(t, proxy, "example.test/indirect", []proxyVersion{
				{"v0.1.0", "", "package indirect\nconst Old = 1\n"},
				{"v0.1.1", "", "package indirect\nconst Old = 2\n"},
				// A blanket -u would select this version, removing the API used below.
				{"v0.2.0", "", "package indirect\nconst New = 3\n"},
			})
			directSource := "package direct\nimport \"example.test/indirect\"\nconst Value = indirect.Old\n"
			writeProxyModule(t, proxy, "example.test/direct", []proxyVersion{
				{"v1.0.0", "require example.test/indirect v0.1.0\n", directSource},
				{"v1.1.0", "require example.test/indirect " + tc.required + "\n", directSource},
				{"v1.2.0-rc.1", "require example.test/indirect v0.1.0\n", directSource},
			})
			// A published upgrade makes accidental targeting of the replacement
			// observable in go.mod, even though its source still resolves locally.
			writeProxyModule(t, proxy, "example.test/local", []proxyVersion{
				{"v1.0.0", "", "package local\nconst Value = 99\n"},
			})
			moduleDir := filepath.Join(root, "consumer")
			mod := "module example.test/consumer\n\ngo 1.27.2\n\nrequire example.test/local v0.0.0\nreplace example.test/local => ./local\n"
			source := "package consumer\nimport \"example.test/local\"\nvar Value = local.Value\n"
			want := map[string]string{"example.test/local": "v0.0.0"}
			if tc.current != "" {
				mod += "require example.test/direct " + tc.current + "\nrequire example.test/indirect v0.1.0 // indirect\n"
				source = "package consumer\nimport (\"example.test/local\"; \"example.test/direct\")\nvar Value = local.Value + direct.Value\n"
				want["example.test/direct"], want["example.test/indirect"] = tc.wantDirect, tc.wantIndirect
			}
			writeFixtureFile(t, filepath.Join(moduleDir, "go.mod"), []byte(mod))
			writeFixtureFile(t, filepath.Join(moduleDir, "consumer.go"), []byte(source))
			writeFixtureFile(t, filepath.Join(moduleDir, "local", "go.mod"), []byte("module example.test/local\ngo 1.27.2\n"))
			writeFixtureFile(t, filepath.Join(moduleDir, "local", "local.go"), []byte("package local\nconst Value = 1\n"))
			env := []string{
				"PATH=" + filepath.Dir(goBinary) + string(os.PathListSeparator) + "/usr/bin:/bin",
				"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOSUMDB=off", "GOVCS=*:off", "GOTELEMETRY=off", "GOAUTH=off", "GOFLAGS=-modcacherw",
				"CGO_ENABLED=0", "GOMAXPROCS=2",
				"GOPROXY=" + (&url.URL{Scheme: "file", Path: filepath.ToSlash(proxy)}).String(),
			}
			for _, key := range []string{"HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOTMPDIR", "TMPDIR"} {
				dir := filepath.Join(root, key)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				env = append(env, key+"="+dir)
			}
			runCommand := func(dir, command string, args ...string) ([]byte, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(suiteCtx, 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, time.Second
				// A timeout must stop the shell's Go children as well as the shell.
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				cmd.Cancel = func() error {
					err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					if err == syscall.ESRCH {
						return os.ErrProcessDone
					}
					return err
				}
				output, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					return output, ctx.Err()
				}
				return output, err
			}
			run := func(dir, command string, args ...string) {
				t.Helper()
				if output, err := runCommand(dir, command, args...); err != nil {
					t.Fatalf("%s %v: %v\n%s", command, args, err, output)
				}
			}
			// Populate the starting graph and sums without upgrading dependencies.
			run(moduleDir, goBinary, "mod", "tidy")
			if tc.blanketUpgrade {
				run(moduleDir, goBinary, "get", "-u", "./...")
				run(moduleDir, goBinary, "mod", "tidy")
			} else if tc.current == "" {
				run(root, "/bin/bash", script, "consumer")
			} else {
				run(moduleDir, "/bin/bash", script)
			}
			data, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && fields[0] == "require" {
					fields = fields[1:]
				}
				if len(fields) >= 2 && strings.HasPrefix(fields[0], "example.test/") {
					got[fields[0]] = fields[1]
				}
			}
			if tc.blanketUpgrade {
				if got["example.test/indirect"] != tc.wantIndirect {
					t.Fatalf("blanket upgrade selected indirect %q, want %q", got["example.test/indirect"], tc.wantIndirect)
				}
				output, err := runCommand(moduleDir, goBinary, "build", "-mod=readonly", "./...")
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) || exitError.ExitCode() != 1 || !strings.Contains(string(output), "undefined: indirect.Old") {
					t.Fatalf("blanket upgrade must break the indirect API, got %v\n%s", err, output)
				}
				t.Logf("blanket upgrade compiler output:\n%s", output)
				return
			}
			if !maps.Equal(got, want) {
				t.Fatalf("selected modules = %v, want %v\ngo.mod:\n%s", got, want, data)
			}
			if !strings.Contains(string(data), "replace example.test/local => ./local") {
				t.Fatalf("local replacement lost:\n%s", data)
			}
			run(moduleDir, goBinary, "build", "-mod=readonly", "./...")
		})
	}
}

type proxyVersion struct{ version, requirements, source string }

func writeProxyModule(t *testing.T, proxy, module string, versions []proxyVersion) {
	t.Helper()
	var list strings.Builder
	for _, v := range versions {
		base := filepath.Join(proxy, filepath.FromSlash(module), "@v", v.version)
		mod := "module " + module + "\ngo 1.27.2\n" + v.requirements
		writeFixtureFile(t, base+".mod", []byte(mod))
		writeFixtureFile(t, base+".info", []byte(fmt.Sprintf("{\"Version\":%q,\"Time\":\"2026-01-01T00:00:00Z\"}\n", v.version)))
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		for _, f := range []struct{ name, content string }{{"go.mod", mod}, {"module.go", v.source}} {
			entry, err := zw.Create(module + "@" + v.version + "/" + f.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(f.content)); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, base+".zip", archive.Bytes())
		fmt.Fprintln(&list, v.version)
	}
	writeFixtureFile(t, filepath.Join(proxy, filepath.FromSlash(module), "@v", "list"), []byte(list.String()))
}

func writeFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
