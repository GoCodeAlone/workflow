package external

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestExternalPluginManagerContextCancelsStartupHandshake(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable-script fixture")
	}
	pluginRoot := t.TempDir()
	const name = "hung-plugin"
	pluginDir := filepath.Join(pluginRoot, name)
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"hung-plugin","version":"1.0.0","author":"Workflow tests","description":"non-handshaking cancellation fixture"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, name), []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	manager := NewExternalPluginManager(pluginRoot, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := manager.LoadPluginContext(ctx, name)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("startup error=%v, want context cancellation", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("canceled startup took %s", elapsed)
	}
}

func TestExternalPluginManagerReleasedStartupContextDoesNotKillAcceptedPlugin(t *testing.T) {
	pluginsDir := t.TempDir()
	const pluginName = "accepted-context-fixture"
	prepareContainerRegistryFixture(t, pluginsDir, pluginName, true, false)

	manager := NewExternalPluginManager(pluginsDir, log.New(io.Discard, "", 0))
	t.Cleanup(manager.Shutdown)
	startupCtx, releaseStartup := context.WithCancel(context.Background())
	adapter, err := manager.LoadPluginContext(startupCtx, pluginName)
	if err != nil {
		t.Fatalf("load plugin: %v", err)
	}
	releaseStartup()

	callCtx, cancelCall := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCall()
	response, err := adapter.client.ContainerRegistryClient().DescribeRegistries(callCtx, &pb.ContainerRegistryDeclarationsRequest{})
	if err != nil || len(response.GetRegistries()) == 0 {
		t.Fatalf("accepted plugin died with released startup context: response=%v error=%v", response, err)
	}
}

func TestExternalPluginManagerRejectsLifecycleMutationsAfterShutdown(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ExternalPluginManager) error
	}{
		{
			name: "load",
			mutate: func(manager *ExternalPluginManager) error {
				_, err := manager.LoadPluginContext(context.Background(), "orphan")
				return err
			},
		},
		{
			name: "reload",
			mutate: func(manager *ExternalPluginManager) error {
				_, err := manager.ReloadPluginContext(context.Background(), "orphan")
				return err
			},
		},
		{name: "unload", mutate: func(manager *ExternalPluginManager) error {
			return manager.UnloadPluginContext(context.Background(), "orphan")
		}},
		{name: "stage resolvers", mutate: func(manager *ExternalPluginManager) error {
			return manager.StageCredentialResolvers()
		}},
		{name: "activate resolvers", mutate: func(manager *ExternalPluginManager) error {
			return manager.ActivateCredentialResolvers()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := NewExternalPluginManager(t.TempDir(), log.Default())
			if err := manager.ShutdownContext(context.Background()); err != nil {
				t.Fatalf("initial ShutdownContext: %v", err)
			}
			startCalls := 0
			manager.startPlugin = func(string) (*pluginLaunch, error) {
				startCalls++
				return &pluginLaunch{client: &goplugin.Client{}, adapter: &ExternalPluginAdapter{}}, nil
			}

			err := test.mutate(manager)
			if !errors.Is(err, ErrExternalPluginManagerClosed) {
				t.Fatalf("mutation after shutdown error = %v, want terminal shutdown error", err)
			}
			if startCalls != 0 {
				t.Fatalf("mutation after shutdown started %d plugin candidates", startCalls)
			}
			if manager.IsLoaded("orphan") || len(manager.LoadedPlugins()) != 0 {
				t.Fatal("mutation after shutdown published plugin state")
			}
			if _, err := manager.DiscoverPlugins(); err != nil {
				t.Fatalf("read-only discovery after shutdown: %v", err)
			}
			if err := manager.ShutdownContext(context.Background()); err != nil {
				t.Fatalf("idempotent ShutdownContext: %v", err)
			}
		})
	}
}

func TestPluginStderrForwarderPrefixesPluginLines(t *testing.T) {
	var out bytes.Buffer
	logger := log.New(&out, "[external-plugins] ", 0)
	forwarder := newPluginStderrForwarder("hover", logger)

	n, err := forwarder.Write([]byte("first line\nsecond line\r\n\nthird"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("first line\nsecond line\r\n\nthird") {
		t.Fatalf("Write returned n=%d, want %d", n, len("first line\nsecond line\r\n\nthird"))
	}
	if strings.Contains(out.String(), "third") {
		t.Fatalf("forwarded stderr should not emit partial line:\n%s", out.String())
	}
	n, err = forwarder.Write([]byte(" line\n"))
	if err != nil {
		t.Fatalf("Write continuation: %v", err)
	}
	if n != len(" line\n") {
		t.Fatalf("Write continuation returned n=%d, want %d", n, len(" line\n"))
	}
	got := out.String()
	for _, want := range []string{
		`[external-plugins] plugin "hover" stderr: first line`,
		`[external-plugins] plugin "hover" stderr: second line`,
		`[external-plugins] plugin "hover" stderr: third line`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("forwarded stderr missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `stderr: `+"\n") {
		t.Fatalf("forwarded stderr should skip blank lines:\n%s", got)
	}
}

func TestExternalPluginManagerStoresCallbackServer(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	callback := NewCallbackServer(nil, nil, log.Default())

	manager.SetCallbackServer(callback)

	if manager.callbackServer != callback {
		t.Fatal("expected manager to retain callback server for plugin load")
	}
}

func TestExternalPluginManagerReloadFailureKeepsExistingPluginLoaded(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate handshake failed")
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected reload failure")
	}

	got := manager.clients["safe-plugin"]
	if got != oldClient {
		t.Fatal("reload failure replaced or removed the active plugin client")
	}
	if !manager.IsLoaded("safe-plugin") {
		t.Fatal("reload failure should leave active plugin loaded")
	}
}

func TestExternalPluginManagerLoadPluginStoresCandidateAfterValidation(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	candidate := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.startPlugin = func(name string) (*pluginLaunch, error) {
		if name != "safe-plugin" {
			t.Fatalf("unexpected plugin name %q", name)
		}
		return &pluginLaunch{client: candidate, adapter: adapter}, nil
	}

	got, err := manager.LoadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got != adapter {
		t.Fatal("load did not return candidate adapter")
	}
	if manager.clients["safe-plugin"] != candidate {
		t.Fatal("load did not register candidate client")
	}
	if _, err := manager.LoadPlugin("safe-plugin"); err == nil {
		t.Fatal("duplicate load should fail")
	}
}

func TestExternalPluginManagerLoadPluginDoesNotStarveLoadedReads(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	started := make(chan struct{})
	release := make(chan struct{})
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		close(started)
		<-release
		return &pluginLaunch{client: &goplugin.Client{}, adapter: &ExternalPluginAdapter{}}, nil
	}

	loadDone := make(chan error, 1)
	go func() {
		_, err := manager.LoadPlugin("slow-plugin")
		loadDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("LoadPlugin did not reach start hook")
	}

	readDone := make(chan bool, 1)
	go func() {
		readDone <- manager.IsLoaded("other-plugin")
	}()

	select {
	case loaded := <-readDone:
		if loaded {
			close(release)
			t.Fatal("unexpected loaded result for unrelated plugin")
		}
	case <-time.After(100 * time.Millisecond):
		close(release)
		t.Fatal("IsLoaded blocked behind plugin startup")
	}

	close(release)
	if err := <-loadDone; err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
}

func TestExternalPluginManagerLoadPluginRejectsInvalidCandidate(t *testing.T) {
	for name, launch := range map[string]*pluginLaunch{
		"nil-launch":  nil,
		"nil-client":  {adapter: &ExternalPluginAdapter{}},
		"nil-adapter": {client: &goplugin.Client{}},
	} {
		t.Run(name, func(t *testing.T) {
			manager := NewExternalPluginManager(t.TempDir(), log.Default())
			manager.startPlugin = func(string) (*pluginLaunch, error) {
				return launch, nil
			}

			if _, err := manager.LoadPlugin("safe-plugin"); err == nil {
				t.Fatal("expected invalid candidate error")
			}
			if manager.IsLoaded("safe-plugin") {
				t.Fatal("invalid candidate should not be registered")
			}
		})
	}
}

func TestExternalPluginManagerLoadPluginReturnsStartError(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate start failed")
	}

	if _, err := manager.LoadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "candidate start failed") {
		t.Fatalf("expected start error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("start failure should not register plugin")
	}
}

func TestExternalPluginManagerLoadPluginValidatesDiskCandidateBeforeStart(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginDir := filepath.Join(pluginsDir, "safe-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "safe-plugin"), 0o755); err != nil {
		t.Fatalf("create fake binary directory: %v", err)
	}
	manifest := []byte(`{
		"name": "safe-plugin",
		"version": "1.0.0",
		"author": "test",
		"description": "test plugin"
	}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manager := NewExternalPluginManager(pluginsDir, log.Default())

	if _, err := manager.LoadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "binary path is a directory") {
		t.Fatalf("expected directory binary validation error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("disk validation failure should not register plugin")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginLoadsCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	candidate := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: candidate, adapter: adapter}, nil
	}

	got, err := manager.ReloadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got != adapter {
		t.Fatal("reload did not return candidate adapter")
	}
	if manager.clients["safe-plugin"] != candidate {
		t.Fatal("reload without active plugin did not register candidate client")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginRejectsInvalidCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}}, nil
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected invalid candidate error")
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("invalid reload candidate should not be registered")
	}
}

func TestExternalPluginManagerReloadWithoutActivePluginReturnsStartError(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return nil, errors.New("candidate start failed")
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil || !strings.Contains(err.Error(), "candidate start failed") {
		t.Fatalf("expected start error, got %v", err)
	}
	if manager.IsLoaded("safe-plugin") {
		t.Fatal("start failure should not register plugin")
	}
}

func TestExternalPluginManagerReloadSuccessSwapsAfterCandidateStarts(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	newClient := &goplugin.Client{}
	adapter := &ExternalPluginAdapter{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		if manager.clients["safe-plugin"] != oldClient {
			t.Fatal("candidate started after old plugin was removed")
		}
		return &pluginLaunch{client: newClient, adapter: adapter}, nil
	}

	got, err := manager.ReloadPlugin("safe-plugin")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if got != adapter {
		t.Fatal("reload did not return candidate adapter")
	}
	if got := manager.clients["safe-plugin"]; got != newClient {
		t.Fatal("reload success did not register candidate plugin client")
	}
}

func TestExternalPluginManagerReloadLoadedRejectsInvalidCandidate(t *testing.T) {
	manager := NewExternalPluginManager(t.TempDir(), log.Default())
	oldClient := &goplugin.Client{}
	manager.clients["safe-plugin"] = oldClient
	manager.startPlugin = func(string) (*pluginLaunch, error) {
		return &pluginLaunch{client: &goplugin.Client{}}, nil
	}

	if _, err := manager.ReloadPlugin("safe-plugin"); err == nil {
		t.Fatal("expected invalid candidate error")
	}
	if manager.clients["safe-plugin"] != oldClient {
		t.Fatal("invalid reload candidate replaced active plugin")
	}
}
