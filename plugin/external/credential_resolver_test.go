package external

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
)

func TestCredentialResolverExternalLoaderCloudAccountLifecycle(t *testing.T) {
	pluginsDir := t.TempDir()
	pluginName := "credential-resolver-fixture"
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("create plugin directory: %v", err)
	}
	buildCredentialResolverFixture(t, filepath.Join(pluginDir, pluginName))
	manifest := `{
		"name":"` + pluginName + `",
		"version":"1.0.0",
		"author":"Workflow tests",
		"description":"credential resolver transport fixture",
		"credentialResolvers":[{"provider":"aws","credentialTypes":["static"]}]
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}

	manager := NewExternalPluginManager(pluginsDir, nil)
	t.Cleanup(manager.Shutdown)
	adapter, err := manager.LoadPlugin(pluginName)
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if !contractRegistryAdvertisesCredentialResolver(adapter.ContractRegistry()) {
		t.Fatalf("resolver contract not advertised: %v", adapter.ContractRegistry())
	}
	if accessKey := externalLoaderCloudAccountAccessKey(t); accessKey != "external-loader-access" {
		t.Fatalf("loaded cloud.account access key = %q", accessKey)
	}
	if err := manager.UnloadPlugin(pluginName); err != nil {
		t.Fatalf("UnloadPlugin: %v", err)
	}
	if accessKey := externalLoaderCloudAccountAccessKey(t); accessKey != "builtin-access" {
		t.Fatalf("unloaded cloud.account access key = %q", accessKey)
	}
}

func externalLoaderCloudAccountAccessKey(t *testing.T) string {
	t.Helper()
	account := module.NewCloudAccount("loader-account", map[string]any{
		"provider": "aws",
		"credentials": map[string]any{
			"type":      "static",
			"accessKey": "builtin-access",
		},
	})
	if err := account.Init(module.NewMockApplication()); err != nil {
		t.Fatalf("cloud.account Init: %v", err)
	}
	credentials, err := account.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	return credentials.AccessKey
}

func buildCredentialResolverFixture(t *testing.T, output string) {
	t.Helper()
	sourceDir := t.TempDir()
	source := `package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type provider struct{}

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name: "credential-resolver-fixture",
		Version: "1.0.0",
		Author: "Workflow tests",
		Description: "credential resolver transport fixture",
	}
}

func (*provider) CredentialResolvers() []*pb.CredentialResolverDeclaration {
	return []*pb.CredentialResolverDeclaration{{Provider: "aws", CredentialTypes: []string{"static"}}}
}

func (*provider) Resolve(_ context.Context, request *pb.CredentialResolveRequest) (*pb.CredentialResolveResponse, error) {
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(),
		AccessKey: "external-loader-access",
	}}, nil
}

func main() {
	p := &provider{}
	sdk.Serve(p, sdk.WithCredentialResolverProvider(p))
}
`
	sourcePath := filepath.Join(sourceDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write credential resolver fixture: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, sourcePath)
	cmd.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build credential resolver fixture for %s/%s: %v\n%s", runtime.GOOS, runtime.GOARCH, err, strings.TrimSpace(string(combined)))
	}
	if info, err := os.Stat(output); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("fixture is not executable: %s (%v)", output, err)
	}
}
