//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
	"golang.org/x/sys/unix"
)

const pipelineDockerTestLabel = "wfctl-0123456789abcdef0123456789abcdef"

type pipelineDockerFixture struct {
	Context         string                  `json:"context"`
	ServerID        string                  `json:"server_id"`
	Containers      map[string]string       `json:"containers"`
	Fail            string                  `json:"fail"`
	Sleep           string                  `json:"sleep"`
	Flood           string                  `json:"flood"`
	Keep            bool                    `json:"keep"`
	ListOverride    string                  `json:"list_override"`
	InspectOverride string                  `json:"inspect_override"`
	UnavailableHost string                  `json:"unavailable_host"`
	Helper          string                  `json:"helper"`
	HelperBlocks    bool                    `json:"helper_blocks"`
	Group           pipelineProcessIdentity `json:"group"`
	LateCreate      bool                    `json:"late_create"`
}

type pipelineDockerInvocation struct {
	Args   []string          `json:"args"`
	Env    []string          `json:"env"`
	Files  map[string]string `json:"files"`
	Config string            `json:"config"`
	PID    int               `json:"pid"`
	PGID   int               `json:"pgid"`
}

// The helper substitutes only the Docker CLI dependency. Resolution, execution,
// capture, identity verification, argument building, and cleanup are real code.
func TestPipelineDockerCLIHelper(t *testing.T) {
	if slices.Contains(os.Args, "--pipeline-docker-group-leader") {
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	if marker := slices.Index(os.Args, "--pipeline-docker-late-create"); marker >= 0 {
		time.Sleep(2 * time.Second)
		_ = os.WriteFile(filepath.Join(os.Args[marker+1], "late-create"), []byte("created after empty probe"), 0600)
		os.Exit(0)
	}
	if marker := slices.Index(os.Args, "--pipeline-docker-transport-parent"); marker >= 0 {
		dir := os.Args[marker+1]
		data, _ := os.ReadFile(filepath.Join(dir, "fixture.json"))
		var fixture pipelineDockerFixture
		_ = json.Unmarshal(data, &fixture)
		client, err := resolvePipelineDockerClient(context.Background())
		if err != nil {
			os.Exit(96)
		}
		defer client.Close()
		_, _ = client.RunInProcessGroup(context.Background(), fixture.Group, "create", "--label", "wfctl.pipeline.cleanup="+pipelineDockerTestLabel, "--", "alpine:latest", "true")
		os.Exit(0)
	}
	if slices.Contains(os.Args, "--pipeline-docker-inherited-fd") {
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	marker := slices.Index(os.Args, "--pipeline-docker-fixture")
	if marker < 0 {
		return
	}
	dir, args := os.Args[marker+1], os.Args[marker+2:]
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		os.Exit(90)
	}
	var fixture pipelineDockerFixture
	if json.Unmarshal(data, &fixture) != nil {
		os.Exit(91)
	}
	invocation := pipelineDockerInvocation{Args: args, Env: os.Environ(), Files: map[string]string{}}
	invocation.PID, invocation.PGID = os.Getpid(), syscall.Getpgrp()
	commandArgs := args
	host := "unix:///daemon-b.sock"
	for len(commandArgs) > 0 && strings.HasPrefix(commandArgs[0], "--") {
		flag, value, hasValue := strings.Cut(commandArgs[0], "=")
		commandArgs = commandArgs[1:]
		if !hasValue {
			if len(commandArgs) == 0 {
				os.Exit(92)
			}
			value, commandArgs = commandArgs[0], commandArgs[1:]
		}
		switch flag {
		case "--host":
			host = value
		case "--config":
			config, _ := os.ReadFile(filepath.Join(value, "config.json"))
			invocation.Config = string(config)
		case "--tlscacert", "--tlscert", "--tlskey":
			material, _ := os.ReadFile(value)
			invocation.Files[flag] = string(material)
		}
	}
	log, _ := os.OpenFile(filepath.Join(dir, "calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	_ = json.NewEncoder(log).Encode(invocation)
	_ = log.Close()
	if len(commandArgs) == 0 {
		os.Exit(93)
	}
	command := commandArgs[0]
	if fixture.Helper == command {
		binary, _ := os.Executable()
		helperArgs := []string{"-test.run=^TestPipelineDockerCLIHelper$", "--", "--pipeline-docker-inherited-fd"}
		if fixture.LateCreate {
			helperArgs = []string{"-test.run=^TestPipelineDockerCLIHelper$", "--", "--pipeline-docker-late-create", dir}
		}
		helper := exec.Command(binary, helperArgs...)
		helper.Stdout, helper.Stderr, helper.Env = os.Stdout, os.Stderr, os.Environ()
		if helper.Start() != nil {
			os.Exit(95)
		}
		_ = os.WriteFile(filepath.Join(dir, "helper.pid"), []byte(fmt.Sprint(helper.Process.Pid)), 0600)
		if fixture.HelperBlocks {
			time.Sleep(20 * time.Second)
		}
	}
	if command != "context" && host == fixture.UnavailableHost {
		fmt.Fprint(os.Stderr, "unavailable daemon credential=TEST_SECRET_TOKEN")
		os.Exit(7)
	}
	if fixture.Sleep == command {
		time.Sleep(20 * time.Second)
	}
	if fixture.Fail == command {
		fmt.Fprint(os.Stderr, "credential=TEST_SECRET_TOKEN raw daemon stderr")
		os.Exit(7)
	}
	if fixture.Flood == command {
		fmt.Fprint(os.Stdout, strings.Repeat("x", sandbox.MaxOutputBytes+1))
		fmt.Fprint(os.Stderr, strings.Repeat("y", sandbox.MaxOutputBytes+1))
		os.Exit(0)
	}
	switch command {
	case "context":
		fmt.Print(fixture.Context)
	case "info":
		_ = json.NewEncoder(os.Stdout).Encode(fixture.ServerID)
	case "ps":
		if fixture.ListOverride != "" {
			fmt.Print(fixture.ListOverride)
			break
		}
		filter := ""
		for i, arg := range commandArgs {
			if arg == "--filter" && i+1 < len(commandArgs) {
				filter = commandArgs[i+1]
			}
		}
		for id, label := range fixture.Containers {
			if filter == "id="+id || filter == "label=wfctl.pipeline.cleanup="+label {
				fmt.Println(id)
			}
		}
	case "inspect":
		if fixture.InspectOverride != "" {
			fmt.Print(fixture.InspectOverride)
			break
		}
		id := commandArgs[len(commandArgs)-1]
		label, ok := fixture.Containers[id]
		if !ok {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": id, "labels": map[string]string{"wfctl.pipeline.cleanup": label}})
	case "rm":
		if !fixture.Keep {
			delete(fixture.Containers, commandArgs[len(commandArgs)-1])
			data, _ = json.Marshal(fixture)
			_ = os.WriteFile(filepath.Join(dir, "fixture.json"), data, 0600)
		}
	case "wait":
		fmt.Println("0")
	case "create":
		id := strings.Repeat("a", 64)
		for i, arg := range commandArgs {
			if arg == "--cidfile" && i+1 < len(commandArgs) {
				file, err := os.Create(commandArgs[i+1])
				if err != nil {
					os.Exit(97)
				}
				if _, err := file.WriteString(id); err != nil {
					os.Exit(98)
				}
				if file.Close() != nil {
					os.Exit(99)
				}
				break
			}
		}
		fmt.Println(id)
	case "start", "attach":
		fmt.Print("captured output")
	default:
		os.Exit(94)
	}
	os.Exit(0)
}

func newPipelineDockerFixture(t *testing.T) (string, *pipelineDockerFixture) {
	t.Helper()
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	// Preserve outer race policy; exit sleep must not consume the fault deadline.
	wrapper := "#!/bin/sh\nexport GORACE=" + quote(os.Getenv("GORACE")+" atexit_sleep_ms=0") + "\nexec " + quote(binary) + " -test.run=^TestPipelineDockerCLIHelper$ -- --pipeline-docker-fixture " + quote(dir) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
		t.Setenv(key, "")
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", dir)
	fixture := &pipelineDockerFixture{
		Context:    `[{"Name":"default","Endpoints":{"docker":{"Host":"unix:///daemon-a.sock","SkipTLSVerify":false}},"TLSMaterial":{},"Storage":{}}]`,
		ServerID:   "daemon-A",
		Containers: map[string]string{},
	}
	writePipelineDockerFixture(t, dir, fixture)
	return dir, fixture
}

func writePipelineDockerFixture(t *testing.T, dir string, fixture *pipelineDockerFixture) {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func pipelineDockerCalls(t *testing.T, dir string) []pipelineDockerInvocation {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []pipelineDockerInvocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call pipelineDockerInvocation
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func resolvePipelineDockerFixture(t *testing.T) *pipelineDockerClient {
	t.Helper()
	client, err := resolvePipelineDockerClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestPipelineDockerFixturePreservesRaceOptions(t *testing.T) {
	const policy = "exitcode=87 halt_on_error=1 strip_path_prefix=/task35/report-policy atexit_sleep_ms=1000"
	t.Setenv("GORACE", policy)
	dir, _ := newPipelineDockerFixture(t)
	t.Setenv("GORACE", "exitcode=99")
	_ = resolvePipelineDockerFixture(t)
	want := "GORACE=" + policy + " atexit_sleep_ms=0"
	for _, call := range pipelineDockerCalls(t, dir) {
		if !slices.Contains(call.Env, want) {
			t.Fatalf("fake CLI lost captured race policy: want %q", want)
		}
	}
}

func TestPipelineDockerPinnedAmbient(t *testing.T) {
	dir, _ := newPipelineDockerFixture(t)
	t.Setenv("DOCKER_HOST", "unix:///daemon-a.sock")
	t.Setenv("DOCKER_CUSTOM_HEADERS", "Authorization=TEST_SECRET_TOKEN")
	t.Setenv("GITHUB_TOKEN", "TEST_SECRET_TOKEN")
	t.Setenv("HTTPS_PROXY", "https://TEST_SECRET_TOKEN@proxy.invalid")
	if err := os.MkdirAll(filepath.Join(dir, ".docker"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".docker", "config.json"), []byte(`{"auths":{"registry":{"auth":"TEST_SECRET_TOKEN"}},"proxies":{"default":{"httpProxy":"TEST_SECRET_TOKEN"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	client := resolvePipelineDockerFixture(t)
	identity := client.Identity
	if identity.Endpoint != "unix:///daemon-a.sock" || identity.ServerID != "daemon-A" || len(identity.TLSHash) != 64 {
		t.Fatalf("identity was not pinned: %+v", identity)
	}
	t.Setenv("DOCKER_HOST", "unix:///daemon-b.sock")
	t.Setenv("DOCKER_CONTEXT", "daemon-b")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CONFIG", "/foreign/config")
	t.Setenv("PATH", "/foreign/path")
	result, err := client.Run(context.Background(), "wait", strings.Repeat("a", 64))
	if err != nil || result.Stdout != "0\n" || result.ExitCode != 0 {
		t.Fatalf("pinned Run: result=%+v err=%v", result, err)
	}
	calls := pipelineDockerCalls(t, dir)
	if slices.ContainsFunc(calls[1:], func(call pipelineDockerInvocation) bool { return slices.Contains(call.Args, "context") }) {
		t.Fatal("context was resolved more than once")
	}
	for _, call := range calls[1:] {
		if !slices.Contains(call.Args, identity.Endpoint) || call.Config != "{}\n" {
			t.Fatalf("ambient transport/config used: %+v", call)
		}
		for _, env := range call.Env {
			if strings.HasPrefix(env, "DOCKER_") || strings.Contains(env, "TEST_SECRET_TOKEN") || strings.HasPrefix(env, "HTTPS_PROXY=") {
				t.Fatalf("ambient Docker/credential/proxy env inherited: %q", env)
			}
		}
	}
}

func TestPipelineDockerContextTLSMaterial(t *testing.T) {
	dir, fixture := newPipelineDockerFixture(t)
	tlsPath := filepath.Join(dir, "context-tls")
	if err := os.MkdirAll(filepath.Join(tlsPath, "docker"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(tlsPath, "docker", name), []byte("original-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := []any{map[string]any{
		"Name": "remote", "Endpoints": map[string]any{"docker": map[string]any{"Host": "tcp://daemon.example:2376", "SkipTLSVerify": false}},
		"TLSMaterial": map[string]any{"docker": []string{"ca.pem", "cert.pem", "key.pem"}}, "Storage": map[string]any{"TLSPath": tlsPath},
	}}
	data, _ := json.Marshal(metadata)
	fixture.Context = string(data)
	writePipelineDockerFixture(t, dir, fixture)
	t.Setenv("DOCKER_CONTEXT", "remote")
	t.Setenv("DOCKER_HOST", "unix:///ignored.sock")
	client := resolvePipelineDockerFixture(t)
	originalIdentity := client.Identity
	if again := resolvePipelineDockerFixture(t); again.Identity != originalIdentity {
		t.Fatal("identity digest depends on temporary TLS file paths")
	}
	if err := os.WriteFile(filepath.Join(tlsPath, "docker", "key.pem"), []byte("changed-key.pem"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Run(context.Background(), "wait", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, call := range pipelineDockerCalls(t, dir)[1:] {
		if slices.Contains(call.Args, "context") {
			continue
		}
		if call.Files["--tlskey"] != "original-key.pem" || !slices.Contains(call.Args, "--tlsverify=true") {
			t.Fatalf("TLS material was not snapshotted: %+v", call)
		}
	}
	changed := resolvePipelineDockerFixture(t)
	if changed.Identity.TLSHash == originalIdentity.TLSHash {
		t.Fatal("TLS bytes change did not change identity")
	}
	entry := pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: originalIdentity}
	before := len(pipelineDockerCalls(t, dir))
	if err := changed.Cleanup(context.Background(), entry); err == nil {
		t.Fatal("TLS identity mismatch did not fail closed")
	}
	if len(pipelineDockerCalls(t, dir)) != before {
		t.Fatal("mismatched cleanup contacted daemon")
	}
}

func TestPipelineDockerResolutionFailClosed(t *testing.T) {
	for _, test := range []struct{ name, context, server, fail string }{
		{"info failure", "", "daemon-A", "info"},
		{"missing server ID", "", "", ""},
		{"unsafe server ID", "", "ID\nTEST_SECRET_TOKEN", ""},
		{"malformed context", "TEST_SECRET_TOKEN", "daemon-A", ""},
		{"SSH unsupported", `[{"Name":"ssh","Endpoints":{"docker":{"Host":"ssh://user@remote"}}}]`, "daemon-A", ""},
		{"endpoint credentials", `[{"Endpoints":{"docker":{"Host":"tcp://user:TEST_SECRET_TOKEN@remote:2376"}}}]`, "daemon-A", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			fixture.ServerID, fixture.Fail = test.server, test.fail
			if test.context != "" {
				fixture.Context = test.context
			}
			writePipelineDockerFixture(t, dir, fixture)
			client, err := resolvePipelineDockerClient(context.Background())
			if client != nil {
				_ = client.Close()
			}
			if err == nil || strings.Contains(err.Error(), "TEST_SECRET_TOKEN") || strings.Contains(err.Error(), "raw daemon stderr") {
				t.Fatalf("resolution must fail with redacted error: %v", err)
			}
			if test.name == "SSH unsupported" && !strings.Contains(err.Error(), "SSH") {
				t.Fatalf("unsupported SSH requires explicit message: %v", err)
			}
		})
	}
}

func TestPipelineDockerRunFaults(t *testing.T) {
	for _, fault := range []string{"daemon drift", "info failure", "operation failure", "timeout", "overflow", "TLS snapshot drift", "identity field drift", "transport override", "closed"} {
		t.Run(fault, func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			client := resolvePipelineDockerFixture(t)
			args := []string{"wait", strings.Repeat("a", 64)}
			ctx := context.Background()
			switch fault {
			case "daemon drift":
				fixture.ServerID = "daemon-B"
			case "info failure":
				fixture.Fail = "info"
			case "operation failure":
				fixture.Fail = "wait"
			case "timeout":
				fixture.Sleep = "wait"
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
			case "overflow":
				fixture.Flood = "wait"
			case "TLS snapshot drift":
				if err := os.WriteFile(filepath.Join(client.configDir, "config.json"), []byte(`{"proxies":{"default":{"httpProxy":"TEST_SECRET_TOKEN"}}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "identity field drift":
				client.Identity.ServerID = "daemon-B"
			case "transport override":
				args = []string{"wait", "--host=unix:///daemon-b.sock", strings.Repeat("a", 64)}
			case "closed":
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
			}
			writePipelineDockerFixture(t, dir, fixture)
			started := time.Now()
			result, err := client.Run(ctx, args...)
			if err == nil || strings.Contains(err.Error(), "TEST_SECRET_TOKEN") || strings.Contains(err.Error(), "raw daemon stderr") {
				t.Fatalf("fault must fail with redacted error: %v", err)
			}
			if fault == "timeout" && time.Since(started) > 2*time.Second {
				t.Fatal("Docker timeout was not bounded")
			}
			if fault == "overflow" && (result == nil || len(result.Stdout) != sandbox.MaxOutputBytes || len(result.Stderr) != sandbox.MaxOutputBytes) {
				t.Fatalf("output not bounded: %+v", result)
			}
			if fault == "daemon drift" || fault == "info failure" {
				for _, call := range pipelineDockerCalls(t, dir) {
					if slices.Contains(call.Args, "wait") {
						t.Fatal("operation ran without matching daemon identity")
					}
				}
			}
		})
	}
}

func TestPipelineDockerRunRejectsNonFullIDs(t *testing.T) {
	dir, _ := newPipelineDockerFixture(t)
	client := resolvePipelineDockerFixture(t)
	for _, command := range []string{"start", "attach", "wait", "inspect", "rm"} {
		for _, id := range []string{"foreign-name", strings.Repeat("a", 12), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
			before := len(pipelineDockerCalls(t, dir))
			if _, err := client.Run(context.Background(), command, id); err == nil {
				t.Errorf("%s accepted non-full ID", command)
			}
			if len(pipelineDockerCalls(t, dir)) != before {
				t.Errorf("%s contacted daemon with non-full ID", command)
			}
		}
	}
}

func TestPipelineDockerCleanupBeforeCIDJournaled(t *testing.T) {
	dir, fixture := newPipelineDockerFixture(t)
	id := strings.Repeat("a", 64)
	fixture.Containers[id] = pipelineDockerTestLabel
	fixture.Containers[strings.Repeat("b", 64)] = "foreign"
	writePipelineDockerFixture(t, dir, fixture)
	client := resolvePipelineDockerFixture(t)
	entry := pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: client.Identity}
	cidfile := filepath.Join(dir, pipelineDockerTestLabel+"-1.cid")
	if err := os.WriteFile(cidfile, []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry.CIDFiles = []string{filepath.Base(cidfile)}
	if err := client.Cleanup(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	calls := pipelineDockerCalls(t, dir)
	var removed []string
	for _, call := range calls {
		if slices.Contains(call.Args, "rm") {
			removed = append(removed, call.Args[len(call.Args)-1])
		}
	}
	if !slices.Equal(removed, []string{id}) {
		t.Fatalf("cleanup did not remove exact pre-CID label match: %v", removed)
	}
	if data, err := os.ReadFile(cidfile); err != nil || string(data) != id+"\n" {
		t.Fatal("Docker cleanup must leave CID files for the parent")
	}
	if err := client.Cleanup(context.Background(), entry); err != nil {
		t.Fatalf("empty readback cleanup must be idempotent: %v", err)
	}
}

func TestPipelineDockerUnavailableDaemonRetainsForRetry(t *testing.T) {
	dir, fixture := newPipelineDockerFixture(t)
	id := strings.Repeat("a", 64)
	fixture.Containers[id] = pipelineDockerTestLabel
	writePipelineDockerFixture(t, dir, fixture)
	client := resolvePipelineDockerFixture(t)
	entry := pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: client.Identity, IDs: []string{id}}
	fixture.UnavailableHost = "unix:///daemon-a.sock"
	writePipelineDockerFixture(t, dir, fixture)
	t.Setenv("DOCKER_HOST", "unix:///daemon-b.sock")
	if err := client.Cleanup(context.Background(), entry); err == nil {
		t.Fatal("daemon A unavailable must not be declared empty using ambient B")
	}
	for _, call := range pipelineDockerCalls(t, dir)[1:] {
		if !slices.Contains(call.Args, client.Identity.Endpoint) || slices.Contains(call.Args, "rm") {
			t.Fatal("unavailable daemon failed closed without retaining exact identity")
		}
	}
	fixture.UnavailableHost = ""
	writePipelineDockerFixture(t, dir, fixture)
	if err := client.Cleanup(context.Background(), entry); err != nil {
		t.Fatalf("same daemon after restart must reconcile retained entry: %v", err)
	}
}

func TestPipelineDockerCleanupCallCancellation(t *testing.T) {
	for _, command := range []string{"info", "ps", "inspect", "rm"} {
		t.Run(command, func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			id := strings.Repeat("a", 64)
			fixture.Containers[id] = pipelineDockerTestLabel
			writePipelineDockerFixture(t, dir, fixture)
			client := resolvePipelineDockerFixture(t)
			fixture.Sleep = command
			writePipelineDockerFixture(t, dir, fixture)
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			started := time.Now()
			if err := client.Cleanup(ctx, pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: client.Identity}); err == nil {
				t.Fatal("blocked cleanup call must fail closed")
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("blocked cleanup call exceeded deadline")
			}
		})
	}
}

func TestPipelineDockerEnvironmentTLS(t *testing.T) {
	for _, verify := range []bool{false, true} {
		t.Run(fmt.Sprint(verify), func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			fixture.Context = `[{"Name":"default","Endpoints":{"docker":{"Host":"tcp://daemon.example:2376","SkipTLSVerify":false}},"TLSMaterial":{},"Storage":{"TLSPath":"<IN MEMORY>"}}]`
			writePipelineDockerFixture(t, dir, fixture)
			certs := filepath.Join(dir, "certs")
			if err := os.Mkdir(certs, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
				if err := os.WriteFile(filepath.Join(certs, name), []byte("env-"+name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("DOCKER_CERT_PATH", certs)
			t.Setenv("DOCKER_TLS", "1")
			if verify {
				t.Setenv("DOCKER_TLS_VERIFY", "1")
			}
			client := resolvePipelineDockerFixture(t)
			for _, call := range pipelineDockerCalls(t, dir)[1:] {
				if call.Files["--tlskey"] != "env-key.pem" || !slices.Contains(call.Args, "--tlsverify="+fmt.Sprint(verify)) {
					t.Fatalf("effective TLS env not explicitly pinned: %+v", call)
				}
			}
			if err := os.WriteFile(filepath.Join(client.configDir, "key.pem"), []byte("modified-key"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Run(context.Background(), "wait", strings.Repeat("a", 64)); err == nil {
				t.Fatal("private TLS digest drift must fail closed")
			}
		})
	}
}

func TestPipelineDockerCIDFilePrivateWithoutChangingCallerUmask(t *testing.T) {
	marker := slices.Index(os.Args, "--pipeline-docker-cid-umask")
	if marker < 0 {
		dir, fixture := newPipelineDockerFixture(t)
		fixture.Group = newPipelineDockerProcessGroup(t)
		writePipelineDockerFixture(t, dir, fixture)
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Control only the fixture subprocess's umask, never the Go test host's.
		child := exec.CommandContext(ctx, "/bin/sh", "-c", `umask 022; exec "$@"`, "wfctl-umask-fixture", binary,
			"-test.run=^TestPipelineDockerCIDFilePrivateWithoutChangingCallerUmask$", "--", "--pipeline-docker-cid-umask", dir)
		child.WaitDelay = 250 * time.Millisecond
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("controlled-umask client regression: %v\n%s", err, output)
		}
		return
	}
	dir := os.Args[marker+1]
	data, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture pipelineDockerFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	client := resolvePipelineDockerFixture(t)
	sentinel := filepath.Join(dir, "shell-evaluation")
	literal := "literal ' \" ; $(touch " + sentinel + ") `touch " + sentinel + "`\n--host=not-a-flag"
	command := []string{"printf", "%s", literal}
	cfg := sandbox.DefaultSecureSandboxConfig("alpine:latest")
	cfg.Env = map[string]string{"LITERAL": literal}
	cidfile := filepath.Join(dir, pipelineDockerTestLabel+"-literal '$.cid")
	args, err := client.CreateArgs(cfg, command, pipelineDockerTestLabel, cidfile)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.RunInProcessGroup(context.Background(), fixture.Group, append([]string{"create"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	id, err := os.ReadFile(cidfile)
	if err != nil || string(id) != strings.TrimSpace(created.Stdout) || !validPipelineDockerID(string(id)) {
		t.Fatalf("CID file/output mismatch: %q, %v", id, err)
	}
	// os.Create here must still observe the caller's original umask after Run.
	probe, err := os.Create(filepath.Join(dir, "caller-umask"))
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(probe.Name()); err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("Docker execution changed caller umask: %v, %v", info, err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("Docker argument evaluated as shell code")
	}
	calls := pipelineDockerCalls(t, dir)
	for _, call := range calls[2:] {
		if call.PGID != fixture.Group.PGID {
			t.Fatal("CID-file execution or server-ID probe escaped recorded group")
		}
	}
	last := calls[len(calls)-1]
	if !slices.Equal(last.Args[len(last.Args)-len(args):], args) {
		t.Fatalf("wrapper changed literal arguments: %v", last.Args)
	}
	info, err := os.Stat(cidfile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("Docker os.Create CID file mode = %04o; want 0600", info.Mode().Perm())
	}
	if err := validatePipelinePrivateFile(info, 0600, false); err != nil {
		t.Fatalf("CID file rejected by unchanged custody policy: %v", err)
	}
}

func TestPipelineDockerCreateStartWaitUsesPinnedClient(t *testing.T) {
	dir, _ := newPipelineDockerFixture(t)
	process := newPipelineDockerProcessGroup(t)
	client := resolvePipelineDockerFixture(t)
	cfg := sandbox.DefaultSecureSandboxConfig("alpine:latest")
	args, err := client.CreateArgs(cfg, []string{"sh", "-euc", "echo literal --host=unchanged"}, pipelineDockerTestLabel, filepath.Join(dir, pipelineDockerTestLabel+"-1.cid"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.RunInProcessGroup(context.Background(), process, append([]string{"create"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(created.Stdout)
	if !validPipelineDockerID(id) {
		t.Fatal("create did not return a full container ID")
	}
	started, err := client.RunInProcessGroup(context.Background(), process, "start", "-a", id)
	if err != nil || started.Stdout != "captured output" {
		t.Fatalf("start capture: %+v, %v", started, err)
	}
	waited, err := client.RunInProcessGroup(context.Background(), process, "wait", id)
	if err != nil || waited.Stdout != "0\n" {
		t.Fatalf("wait capture: %+v, %v", waited, err)
	}
	for _, call := range pipelineDockerCalls(t, dir)[2:] {
		if call.PGID != process.PGID {
			t.Fatalf("broker command/server-ID probe escaped recorded group: %+v", call)
		}
	}
}

func newPipelineDockerProcessGroup(t *testing.T) pipelineProcessIdentity {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	leader := exec.Command(binary, "-test.run=^TestPipelineDockerCLIHelper$", "--", "--pipeline-docker-group-leader")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.Kill(-leader.Process.Pid, unix.SIGKILL)
		_ = leader.Wait()
	})
	token, err := pipelineProcessStartToken(leader.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return pipelineProcessIdentity{PID: leader.Process.Pid, PGID: leader.Process.Pid, Start: token}
}

func TestPipelineDockerProcessGroupIdentityFailsClosed(t *testing.T) {
	dir, _ := newPipelineDockerFixture(t)
	process := newPipelineDockerProcessGroup(t)
	client := resolvePipelineDockerFixture(t)
	for _, bad := range []pipelineProcessIdentity{
		{}, {PID: process.PID, PGID: process.PGID, Start: "reused"},
		{PID: process.PID, PGID: process.PGID + 1, Start: process.Start},
		{PID: process.PID, PGID: process.PGID},
	} {
		before := len(pipelineDockerCalls(t, dir))
		if _, err := client.RunInProcessGroup(context.Background(), bad, "wait", strings.Repeat("a", 64)); err == nil {
			t.Fatal("unverified recorded process group accepted")
		}
		if len(pipelineDockerCalls(t, dir)) != before {
			t.Fatal("invalid process group contacted daemon")
		}
	}
	if _, err := client.Run(context.Background(), "create", "--", "alpine:latest", "true"); err == nil {
		t.Fatal("unjournaled process group retained Docker create authority")
	}
	_ = unix.Kill(-process.PGID, unix.SIGKILL)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, err := pipelineProcessGroupActive(process.PGID)
		if err == nil && !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := client.RunInProcessGroup(context.Background(), process, "wait", strings.Repeat("a", 64)); err == nil {
		t.Fatal("dead recorded group accepted")
	}
}

func TestPipelineDockerGroupCancellationLeavesRootCustody(t *testing.T) {
	dir, fixture := newPipelineDockerFixture(t)
	process := newPipelineDockerProcessGroup(t)
	client := resolvePipelineDockerFixture(t)
	fixture.Helper, fixture.HelperBlocks = "wait", true
	writePipelineDockerFixture(t, dir, fixture)
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.RunInProcessGroup(ctx, process, "wait", strings.Repeat("a", 64)); err == nil {
		t.Fatal("group-bound blocked CLI must fail at its deadline")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("group-bound inherited FD exceeded bounded wait")
	}
	if token, err := pipelineProcessStartToken(process.PID); err != nil || token != process.Start {
		t.Fatal("CLI cancellation killed the root-owned recorded group")
	}
	data, err := os.ReadFile(filepath.Join(dir, "helper.pid"))
	if err != nil {
		t.Fatal("CLI helper was not launched")
	}
	var pid int
	_, _ = fmt.Sscan(string(data), &pid)
	if group, err := unix.Getpgid(pid); err != nil || group != process.PGID {
		t.Fatal("helper did not remain in recorded root custody")
	}
}

func TestPipelineDockerParentSIGKILLStopsLateCreateBeforeEmptyProbe(t *testing.T) {
	dir, fixture := newPipelineDockerFixture(t)
	t.Setenv("TMPDIR", dir)
	process := newPipelineDockerProcessGroup(t)
	client := resolvePipelineDockerFixture(t)
	fixture.Group, fixture.Helper, fixture.HelperBlocks, fixture.LateCreate = process, "create", true, true
	writePipelineDockerFixture(t, dir, fixture)
	binary, _ := os.Executable()
	parent := exec.Command(binary, "-test.run=^TestPipelineDockerCLIHelper$", "--", "--pipeline-docker-transport-parent", dir)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	})
	var helperPID int
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(dir, "helper.pid")); err == nil {
			_, _ = fmt.Sscan(string(data), &helperPID)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if helperPID <= 0 {
		t.Fatal("parent never launched the late-create helper")
	}
	for _, call := range pipelineDockerCalls(t, dir) {
		if slices.Contains(call.Args, "create") {
			t.Cleanup(func() { _ = unix.Kill(call.PID, unix.SIGKILL) })
		}
	}
	t.Cleanup(func() { _ = unix.Kill(helperPID, unix.SIGKILL) })
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	group, err := unix.Getpgid(helperPID)
	if err != nil || group != process.PGID {
		t.Fatal("parent SIGKILL left a late-create helper outside recorded custody")
	}
	// Restart reconciliation kills the recorded group before its empty probe.
	if err := unix.Kill(-process.PGID, unix.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, err := pipelineProcessGroupActive(process.PGID)
		if err == nil && !active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if active, err := pipelineProcessGroupActive(process.PGID); err != nil || active {
		t.Fatal("recorded group was not empty before daemon readback")
	}
	if err := client.Cleanup(context.Background(), pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: client.Identity}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "late-create")); !os.IsNotExist(err) {
		t.Fatal("create completed after empty label readback")
	}
}

func TestPipelineDockerInheritedFDDeadlineAndHelperCancellation(t *testing.T) {
	for _, blocks := range []bool{false, true} {
		t.Run(fmt.Sprint(blocks), func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			client := resolvePipelineDockerFixture(t)
			fixture.Helper, fixture.HelperBlocks = "wait", blocks
			writePipelineDockerFixture(t, dir, fixture)
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			started := time.Now()
			if _, err := client.Run(ctx, "wait", strings.Repeat("a", 64)); err == nil {
				t.Fatal("inherited output FD must not appear as successful completion")
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("inherited output FD exceeded bounded wait")
			}
			data, err := os.ReadFile(filepath.Join(dir, "helper.pid"))
			if err != nil {
				t.Fatalf("helper was not launched; deadline test did not exercise inherited FD: %v", err)
			}
			var pid int
			if _, err := fmt.Sscan(string(data), &pid); err != nil || pid <= 0 {
				t.Fatal("invalid helper PID")
			}
			t.Cleanup(func() { _ = unix.Kill(pid, unix.SIGKILL) })
			deadline := time.Now().Add(time.Second)
			for unix.Kill(pid, 0) == nil && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if err := unix.Kill(pid, 0); err == nil {
				t.Fatal("Docker helper survived bounded executor completion/cancellation")
			}
		})
	}
}

func TestPipelineDockerCleanupRejectsAndRetains(t *testing.T) {
	for _, fault := range []string{"endpoint drift", "TLS drift", "server drift", "foreign label", "short ID", "uppercase ID", "label prefix", "bad list", "inspect mismatch", "remove failure", "readback nonempty", "list failure", "inspect failure"} {
		t.Run(fault, func(t *testing.T) {
			dir, fixture := newPipelineDockerFixture(t)
			id := strings.Repeat("a", 64)
			fixture.Containers[id] = pipelineDockerTestLabel
			writePipelineDockerFixture(t, dir, fixture)
			client := resolvePipelineDockerFixture(t)
			entry := pipelineCleanupEntry{Label: pipelineDockerTestLabel, Docker: client.Identity, IDs: []string{id}}
			switch fault {
			case "endpoint drift":
				entry.Docker.Endpoint = "unix:///daemon-b.sock"
			case "TLS drift":
				entry.Docker.TLSHash = strings.Repeat("b", 64)
			case "server drift":
				entry.Docker.ServerID = "daemon-B"
			case "foreign label":
				fixture.Containers[id] = "foreign"
			case "short ID":
				entry.IDs = []string{id[:12]}
			case "uppercase ID":
				entry.IDs = []string{strings.ToUpper(id)}
			case "label prefix":
				entry.Label += "-foreign"
			case "bad list":
				fixture.ListOverride = id[:12] + "\n"
			case "inspect mismatch":
				fixture.InspectOverride = `{"id":"` + strings.Repeat("b", 64) + `","labels":{"wfctl.pipeline.cleanup":"` + pipelineDockerTestLabel + `"}}`
			case "remove failure":
				fixture.Fail = "rm"
			case "readback nonempty":
				fixture.Keep = true
			case "list failure":
				fixture.Fail = "ps"
			case "inspect failure":
				fixture.Fail = "inspect"
			}
			writePipelineDockerFixture(t, dir, fixture)
			before, _ := json.Marshal(entry)
			if err := client.Cleanup(context.Background(), entry); err == nil || strings.Contains(err.Error(), "TEST_SECRET_TOKEN") {
				t.Fatalf("cleanup must fail closed: %v", err)
			}
			after, _ := json.Marshal(entry)
			if string(before) != string(after) {
				t.Fatal("failed cleanup changed journal identity")
			}
			if fault != "remove failure" && fault != "readback nonempty" {
				for _, call := range pipelineDockerCalls(t, dir) {
					if slices.Contains(call.Args, "rm") {
						t.Fatal("unsafe removal before ownership/identity verification")
					}
				}
			}
		})
	}
}

func TestPipelineDockerCreateArgs(t *testing.T) {
	cfg := sandbox.DefaultSecureSandboxConfig("alpine:latest")
	cfg.NetworkMode, cfg.WorkDir = "bridge", "/work"
	cfg.Tmpfs["/work"] = "size=64m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev"
	cfg.Env = map[string]string{"Z": "explicit", "A": "--host=not-a-flag"}
	client := &pipelineDockerClient{}
	command := []string{"sh", "-euc", "printf '%s' --host=literal"}
	cidfile := filepath.Join(t.TempDir(), pipelineDockerTestLabel+"-1.cid")
	args, err := client.CreateArgs(cfg, command, pipelineDockerTestLabel, cidfile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--label", "wfctl.pipeline.cleanup=" + pipelineDockerTestLabel, "--cidfile", cidfile}
	if !slices.Equal(args[:len(want)], want) {
		t.Fatalf("ownership arguments missing: %v", args)
	}
	for _, arg := range []string{"65532:65532", "ALL", "no-new-privileges", "--read-only", "bridge", "A=--host=not-a-flag", "/work:size=64m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev"} {
		if !slices.Contains(args, arg) {
			t.Fatalf("strict argument %q missing: %v", arg, args)
		}
	}
	if slices.Index(args, "A=--host=not-a-flag") > slices.Index(args, "Z=explicit") || !slices.Equal(args[len(args)-len(command):], command) {
		t.Fatal("env must be deterministic; command must remain literal")
	}
	if _, err := client.CreateArgs(cfg, command, "foreign", cidfile); err == nil {
		t.Fatal("invalid cleanup label accepted")
	}
	if _, err := client.CreateArgs(cfg, command, pipelineDockerTestLabel, "/foreign.cid"); err == nil {
		t.Fatal("unowned CID filename accepted")
	}
	cfg.Env["GITHUB_TOKEN"] = "TEST_SECRET_TOKEN"
	if _, err := client.CreateArgs(cfg, command, pipelineDockerTestLabel, cidfile); err == nil || strings.Contains(err.Error(), "TEST_SECRET_TOKEN") {
		t.Fatal("builder must apply the redacted record config policy")
	}
}
