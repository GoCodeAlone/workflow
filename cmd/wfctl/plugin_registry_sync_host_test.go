package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registrySyncRequestedHostBinary(host, requested string, provided bool) (string, error) {
	if !provided {
		return "", nil
	}
	if host != "linux" {
		return "", fmt.Errorf("WFCTL_REGISTRY_SYNC_HOST_BINARY proof requires Linux: Go does not use SSL_CERT_FILE on %s", host)
	}
	if requested == "" {
		return "", fmt.Errorf("WFCTL_REGISTRY_SYNC_HOST_BINARY must name an executable file")
	}
	path, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("WFCTL_REGISTRY_SYNC_HOST_BINARY: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("WFCTL_REGISTRY_SYNC_HOST_BINARY: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("WFCTL_REGISTRY_SYNC_HOST_BINARY must be a regular executable file, not a directory or symlink")
	}
	return path, nil
}

func registrySyncActualCLIBinary(t *testing.T, host, apiURL string) string {
	t.Helper()
	requested, provided := os.LookupEnv("WFCTL_REGISTRY_SYNC_HOST_BINARY")
	binary, err := registrySyncRequestedHostBinary(host, requested, provided)
	if err != nil {
		t.Fatal(err)
	}
	if provided {
		return binary
	}
	binary = filepath.Join(t.TempDir(), "wfctl")
	buildCtx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", registrySyncHostBuildArguments(host, binary, apiURL)...)
	build.Env = registrySyncHostEnvironment(os.Environ())
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI: %v\n%s", err, out)
	}
	return binary
}

func registrySyncHostBuildArguments(host, binary, apiURL string) []string {
	args := []string{"build", "-p=2", "-o", binary}
	if host != "linux" {
		args = append(args, "-ldflags", "-s -w -X main.gitHubAPIBaseURL="+apiURL)
	}
	return append(args, ".")
}

func registrySyncHostEnvironment(source []string) []string {
	env := make([]string, 0, len(source)+2)
	for _, variable := range source {
		name, _, _ := strings.Cut(variable, "=")
		switch strings.ToUpper(name) {
		case "RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "GOWORK", "GOTOOLCHAIN":
			continue
		}
		env = append(env, variable)
	}
	return append(env, "GOWORK=off", "GOTOOLCHAIN=go1.27.1")
}

func TestPluginRegistrySyncHostBinary_Selection(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "wfctl")
	if err := os.WriteFile(binary, []byte("selector fixture only"), 0700); err != nil {
		t.Fatal(err)
	}
	nonExecutable := filepath.Join(root, "non-executable")
	if err := os.WriteFile(nonExecutable, []byte("selector fixture only"), 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(binary, symlink); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, host, path string
		provided, valid  bool
	}{
		{"explicit", "linux", binary, true, true},
		{"absent", "linux", "", false, true},
		{"empty", "linux", "", true, false},
		{"missing", "linux", filepath.Join(root, "missing"), true, false},
		{"directory", "linux", root, true, false},
		{"non-executable", "linux", nonExecutable, true, false},
		{"symlink", "linux", symlink, true, false},
		{"darwin", "darwin", binary, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := registrySyncRequestedHostBinary(tc.host, tc.path, tc.provided)
			if tc.valid {
				if err != nil || got != tc.path {
					t.Fatalf("selection = %q, %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "WFCTL_REGISTRY_SYNC_HOST_BINARY") {
				t.Fatalf("invalid explicit selection was accepted: %q, %v", got, err)
			}
		})
	}
}

func TestPluginRegistrySyncHostBinary_BuildArguments(t *testing.T) {
	for _, host := range []string{"linux", "darwin"} {
		args := strings.Join(registrySyncHostBuildArguments(host, "/fixture/wfctl", "http://fixture.invalid"), " ")
		modified := strings.Contains(args, "-ldflags") || strings.Contains(args, "gitHubAPIBaseURL")
		if modified != (host == "darwin") {
			t.Fatalf("%s build unexpectedly modifies the default API: %s", host, args)
		}
	}
}

func TestPluginRegistrySyncHostBinary_CredentialFreeEnvironment(t *testing.T) {
	env := registrySyncHostEnvironment([]string{
		"PATH=/fixture", "HOME=/fixture/home", "RELEASES_TOKEN=secret", "GH_TOKEN=secret", "GITHUB_TOKEN=secret",
		"HTTP_PROXY=http://user:secret@outside.invalid", "https_proxy=http://outside.invalid", "ALL_PROXY=http://outside.invalid", "no_proxy=*",
		"SSL_CERT_FILE=/outside/ca", "SSL_CERT_DIR=/outside/roots", "GOWORK=/outside/go.work", "GOTOOLCHAIN=go1.27.1",
	})
	if got := strings.Join(env, "\n"); got != "PATH=/fixture\nHOME=/fixture/home\nGOWORK=off\nGOTOOLCHAIN=go1.27.1" {
		t.Fatalf("host environment retained credentials or transport overrides:\n%s", got)
	}
}

func TestPluginRegistrySyncHostBinary_ExplicitNeverBuilds(t *testing.T) {
	root := t.TempDir()
	goTrap := filepath.Join(root, "go")
	if err := os.WriteFile(goTrap, []byte("#!/bin/sh\nprintf 'unexpected source-build fallback\\n' >&2\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), self, "-test.run=^TestPluginRegistrySyncTarget_ActualCLI$", "-test.v")
	cmd.Env = append(os.Environ(),
		"WFCTL_REGISTRY_SYNC_HOST_BINARY="+filepath.Join(root, "missing-wfctl"),
		"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOWORK=off", "GOTOOLCHAIN=go1.27.1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "WFCTL_REGISTRY_SYNC_HOST_BINARY") || strings.Contains(string(out), "unexpected source-build fallback") {
		t.Fatalf("explicit binary selection did not fail closed before building: %v\n%s", err, out)
	}
}

func TestPluginRegistrySyncHostBinary_ExplicitSelection(t *testing.T) {
	root := t.TempDir()
	goTrap := filepath.Join(root, "go")
	if err := os.WriteFile(goTrap, []byte("#!/bin/sh\nprintf 'unexpected source-build fallback\\n' >&2\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_REGISTRY_SYNC_HOST_BINARY", self)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	// This exercises selection only, not Linux execution or release proof.
	if got := registrySyncActualCLIBinary(t, "linux", "http://fixture.invalid"); got != self {
		t.Fatalf("provided executable was not selected unchanged: %q", got)
	}
}
