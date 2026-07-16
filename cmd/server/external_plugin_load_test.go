package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginexternal "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

type serverRollbackCredentialResolverClient struct{}

func (*serverRollbackCredentialResolverClient) DescribeResolvers(context.Context, *pb.CredentialResolverDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialResolverDeclarationsResponse, error) {
	return &pb.CredentialResolverDeclarationsResponse{Resolvers: []*pb.CredentialResolverDeclaration{{
		Provider: "aws", CredentialTypes: []string{"static"},
	}}}, nil
}

func (*serverRollbackCredentialResolverClient) Resolve(_ context.Context, request *pb.CredentialResolveRequest, _ ...grpc.CallOption) (*pb.CredentialResolveResponse, error) {
	return &pb.CredentialResolveResponse{Credentials: &pb.ResolvedCloudCredentials{
		Provider: request.GetProvider(), AccessKey: "rejected-external",
	}}, nil
}

type serverRollbackExternalManager struct {
	adapter   *pluginexternal.ExternalPluginAdapter
	cleanup   func()
	loaded    bool
	unloadErr error
}

func (m *serverRollbackExternalManager) LoadPlugin(string) (*pluginexternal.ExternalPluginAdapter, error) {
	if m.cleanup == nil {
		cleanup, err := module.RegisterExternalCredentialResolver(context.Background(), &serverRollbackCredentialResolverClient{})
		if err != nil {
			return nil, err
		}
		m.cleanup = cleanup
	}
	m.loaded = true
	return m.adapter, nil
}

func (m *serverRollbackExternalManager) UnloadPlugin(string) error {
	if m.cleanup != nil {
		m.cleanup()
		m.cleanup = nil
	}
	m.loaded = false
	return m.unloadErr
}

type rejectingExternalEngine struct {
	err error
}

func (e rejectingExternalEngine) LoadPlugin(plugin.EnginePlugin) error { return e.err }

func TestLoadExternalPluginIntoEngineRollsBackRejectedResolver(t *testing.T) {
	manager := &serverRollbackExternalManager{adapter: &pluginexternal.ExternalPluginAdapter{}}
	t.Cleanup(func() {
		if manager.loaded {
			_ = manager.UnloadPlugin("rejected")
		}
	})
	rejection := errors.New("engine rejected duplicate plugin")
	_, err := loadExternalPluginIntoEngine(manager, rejectingExternalEngine{err: rejection}, "rejected")
	if !errors.Is(err, rejection) {
		t.Fatalf("load error = %v, want engine rejection", err)
	}
	if manager.loaded {
		t.Fatal("engine rejection left external manager loaded")
	}

	account := module.NewCloudAccount("rollback-account", map[string]any{
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
	if err != nil || credentials.AccessKey != "builtin-access" {
		t.Fatalf("post-rollback credentials = %+v, %v", credentials, err)
	}
}

func TestLoadExternalPluginIntoEngineJoinsRollbackFailure(t *testing.T) {
	rejection := errors.New("engine rejection")
	rollback := errors.New("rollback failure")
	manager := &serverRollbackExternalManager{
		adapter:   &pluginexternal.ExternalPluginAdapter{},
		unloadErr: rollback,
	}
	_, err := loadExternalPluginIntoEngine(manager, rejectingExternalEngine{err: rejection}, "joined")
	if !errors.Is(err, rejection) || !errors.Is(err, rollback) {
		t.Fatalf("joined error = %v", err)
	}
	for _, forbidden := range []string{"rejected-external", "builtin-access"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("rollback error leaked credential %q: %v", forbidden, err)
		}
	}
}
