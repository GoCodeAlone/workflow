package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestSecretsListOrphansUsesTypedIssuerPaginationAndDryRun(t *testing.T) {
	client := &orphanCredentialIssuerClient{pages: []*pb.CredentialListResponse{
		{Credentials: []*pb.CredentialRecord{{Identifier: "credential-1", LogicalName: "deploy-key"}}, NextPageToken: "page-2"},
		{Credentials: []*pb.CredentialRecord{{Identifier: "credential-2", LogicalName: "deploy-key"}}},
	}}
	withOrphanIssuerResolver(t, client)
	pluginDir := filepath.Join(t.TempDir(), "plugins")
	if err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", pluginDir}); err != nil {
		t.Fatal(err)
	}
	if client.listCalls != 2 || len(client.deleted) != 0 {
		t.Fatalf("dry-run List calls=%d deleted=%v", client.listCalls, client.deleted)
	}

	client.reset()
	if err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", pluginDir, "--delete"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(client.deleted, ","); got != "credential-1,credential-2" {
		t.Fatalf("deleted=%q", got)
	}
	for _, operationID := range client.deleteOperationIDs {
		if operationID == "" {
			t.Fatal("Delete request omitted stable operation ID")
		}
	}
}

func TestCredentialOrphanDeleteRetriesReuseStableOperationIDs(t *testing.T) {
	client := &orphanCredentialIssuerClient{pages: []*pb.CredentialListResponse{{
		Credentials: []*pb.CredentialRecord{
			{Identifier: "credential-1", LogicalName: "deploy-key"},
			{Identifier: "credential-2", LogicalName: "deploy-key"},
		},
	}}}
	if err := listCredentialIssuerOrphans(context.Background(), client, "example.source", "deploy-key", true, false); err != nil {
		t.Fatal(err)
	}
	first := append([]string(nil), client.deleteOperationIDs...)
	client.reset()
	if err := listCredentialIssuerOrphans(context.Background(), client, "example.source", "deploy-key", true, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(client.deleteOperationIDs, first) {
		t.Fatalf("retry operation IDs=%v, want stable %v", client.deleteOperationIDs, first)
	}
	if len(first) != 2 || first[0] == "" || first[0] == first[1] {
		t.Fatalf("operation IDs are not distinct stable identifiers: %v", first)
	}
}

func TestSecretsListOrphansTypedErrorsBeforeDelete(t *testing.T) {
	client := &orphanCredentialIssuerClient{pages: []*pb.CredentialListResponse{{
		NextPageToken: "still-more",
	}}}
	client.repeatLastPage = true
	withOrphanIssuerResolver(t, client)
	err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir(), "--delete"})
	if err == nil || !strings.Contains(err.Error(), "page limit") {
		t.Fatalf("pagination error=%v", err)
	}
	if len(client.deleted) != 0 {
		t.Fatalf("partial inventory triggered delete: %v", client.deleted)
	}

	client.reset()
	client.pages = []*pb.CredentialListResponse{{Error: &pb.CredentialOperationError{Code: "inventory_failed", Message: "sensitive-value"}}}
	err = runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir(), "--delete"})
	if err == nil || !strings.Contains(err.Error(), "inventory_failed") || strings.Contains(err.Error(), "sensitive-value") || len(client.deleted) != 0 {
		t.Fatalf("structured error=%v deleted=%v", err, client.deleted)
	}

	client.reset()
	client.pages = []*pb.CredentialListResponse{{Credentials: []*pb.CredentialRecord{{
		Identifier: "wrong-record", LogicalName: "different-name",
	}}}}
	err = runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir(), "--delete"})
	if err == nil || !strings.Contains(err.Error(), "selector mismatch") || len(client.deleted) != 0 {
		t.Fatalf("mismatched inventory error=%v deleted=%v", err, client.deleted)
	}
}

func TestSecretsListOrphansUsesBoundedSignalAwareContext(t *testing.T) {
	client := &orphanCredentialIssuerClient{requireContextError: true}
	withOrphanIssuerResolver(t, client)
	originalCommandContext := providerCommandContext
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })

	err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want prompt command cancellation", err)
	}

	client.requireContextError = false
	client.listContextCanceled = false
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		return context.WithCancel(context.Background())
	}
	originalTimeout := credentialOrphanOperationTimeout
	credentialOrphanOperationTimeout = time.Minute
	t.Cleanup(func() { credentialOrphanOperationTimeout = originalTimeout })
	if err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if !client.listContextDeadline {
		t.Fatal("List did not receive the bounded command context")
	}
}

func TestSecretsListOrphansStopsDeletingAfterCancellation(t *testing.T) {
	client := &orphanCredentialIssuerClient{pages: []*pb.CredentialListResponse{{Credentials: []*pb.CredentialRecord{
		{Identifier: "credential-1", LogicalName: "deploy-key"},
		{Identifier: "credential-2", LogicalName: "deploy-key"},
	}}}}
	withOrphanIssuerResolver(t, client)
	originalCommandContext := providerCommandContext
	var cancel context.CancelFunc
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, commandCancel := context.WithCancel(context.Background())
		cancel = commandCancel
		return ctx, commandCancel
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })
	client.cancelAfterDelete = func() { cancel() }

	err := runSecretsListOrphans([]string{"--source", "example.source", "--name", "deploy-key", "--plugin-dir", t.TempDir(), "--delete"})
	if err == nil || len(client.deleted) != 1 {
		t.Fatalf("error=%v deleted=%v, want cancellation after first delete", err, client.deleted)
	}
}

func TestSecretsListOrphansLegacyFallbackStopsAfterCancellation(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	oldResolve := resolveCredentialIssuerCapability
	resolveCredentialIssuerCapability = func(context.Context, string, string) (pb.CredentialIssuerClient, func(), config.CredentialSourceDecl, string, string, bool, error) {
		return nil, nil, config.CredentialSourceDecl{}, "", "", false, nil
	}
	t.Cleanup(func() { resolveCredentialIssuerCapability = oldResolve })
	originalCommandContext := providerCommandContext
	var cancel context.CancelFunc
	providerCommandContext = func() (context.Context, context.CancelFunc) {
		ctx, commandCancel := context.WithCancel(context.Background())
		cancel = commandCancel
		return ctx, commandCancel
	}
	t.Cleanup(func() { providerCommandContext = originalCommandContext })
	originalList := paginateSpacesKeysByNameForOrphans
	listSawDeadline := false
	paginateSpacesKeysByNameForOrphans = func(ctx context.Context, _, _ string) ([]string, error) {
		_, listSawDeadline = ctx.Deadline()
		return []string{"credential-1", "credential-2"}, nil
	}
	t.Cleanup(func() { paginateSpacesKeysByNameForOrphans = originalList })
	originalDelete := deleteSpacesKeyForOrphans
	var deleted []string
	deleteSpacesKeyForOrphans = func(_ context.Context, _ string, identifier string) error {
		deleted = append(deleted, identifier)
		if len(deleted) == 1 {
			cancel()
		}
		return nil
	}
	t.Cleanup(func() { deleteSpacesKeyForOrphans = originalDelete })

	err := runSecretsListOrphans([]string{"--source", "digitalocean.spaces", "--name", "deploy-key", "--delete"})
	if !errors.Is(err, context.Canceled) || !listSawDeadline || len(deleted) != 1 {
		t.Fatalf("error=%v deadline=%v deleted=%v", err, listSawDeadline, deleted)
	}
}

func TestCredentialOrphanTransportErrorsExposeOnlyCanonicalCodes(t *testing.T) {
	const providerText = "provider-sensitive-transport-detail"
	client := &orphanCredentialIssuerClient{listErr: status.Error(codes.Internal, providerText)}
	err := listCredentialIssuerOrphans(context.Background(), client, "example.source", "deploy-key", false, false)
	if err == nil || !strings.Contains(err.Error(), codes.Internal.String()) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("List transport error=%v", err)
	}

	client = &orphanCredentialIssuerClient{
		pages:     []*pb.CredentialListResponse{{Credentials: []*pb.CredentialRecord{{Identifier: "credential-1", LogicalName: "deploy-key"}}}},
		deleteErr: status.Error(codes.ResourceExhausted, providerText),
	}
	err = listCredentialIssuerOrphans(context.Background(), client, "example.source", "deploy-key", true, false)
	if err == nil || !strings.Contains(err.Error(), codes.ResourceExhausted.String()) || strings.Contains(err.Error(), providerText) {
		t.Fatalf("Delete transport error=%v", err)
	}
}

func TestSecretsListOrphansUnknownSourceHasInstallGuidance(t *testing.T) {
	err := runSecretsListOrphans([]string{
		"--source", "example.missing", "--name", "deploy-key", "--plugin-dir", t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "wfctl plugin install") || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("error=%v", err)
	}
}

func withOrphanIssuerResolver(t *testing.T, client pb.CredentialIssuerClient) {
	t.Helper()
	oldResolve := resolveCredentialIssuerCapability
	resolveCredentialIssuerCapability = func(context.Context, string, string) (pb.CredentialIssuerClient, func(), config.CredentialSourceDecl, string, string, bool, error) {
		return client, func() {}, config.CredentialSourceDecl{
			Source: "example.source", ConcurrencyMode: config.CredentialConcurrencyProviderIdempotent,
			Outputs: []config.CredentialOutputDecl{{Key: "id"}}, IdentifierKey: "id",
		}, "workflow-plugin-example", "1.2.3", true, nil
	}
	t.Cleanup(func() { resolveCredentialIssuerCapability = oldResolve })
}

type orphanCredentialIssuerClient struct {
	pages               []*pb.CredentialListResponse
	listCalls           int
	deleted             []string
	deleteOperationIDs  []string
	repeatLastPage      bool
	listErr             error
	deleteErr           error
	requireContextError bool
	listContextCanceled bool
	listContextDeadline bool
	cancelAfterDelete   func()
}

func (c *orphanCredentialIssuerClient) reset() {
	c.listCalls = 0
	c.deleted = nil
	c.deleteOperationIDs = nil
}

func (*orphanCredentialIssuerClient) DescribeSources(context.Context, *pb.CredentialSourceDeclarationsRequest, ...grpc.CallOption) (*pb.CredentialSourceDeclarationsResponse, error) {
	return nil, errors.New("unexpected DescribeSources")
}

func (*orphanCredentialIssuerClient) Issue(context.Context, *pb.CredentialIssueRequest, ...grpc.CallOption) (*pb.CredentialIssueResponse, error) {
	return nil, errors.New("unexpected Issue")
}

func (c *orphanCredentialIssuerClient) List(ctx context.Context, request *pb.CredentialListRequest, _ ...grpc.CallOption) (*pb.CredentialListResponse, error) {
	c.listContextCanceled = c.listContextCanceled || errors.Is(ctx.Err(), context.Canceled)
	_, c.listContextDeadline = ctx.Deadline()
	if c.requireContextError && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.listErr != nil {
		return nil, c.listErr
	}
	index := c.listCalls
	c.listCalls++
	var response *pb.CredentialListResponse
	if index < len(c.pages) {
		response = c.pages[index]
	} else if c.repeatLastPage && len(c.pages) > 0 {
		response = c.pages[len(c.pages)-1]
	} else {
		response = &pb.CredentialListResponse{}
	}
	cloned := proto.Clone(response).(*pb.CredentialListResponse)
	for _, record := range cloned.GetCredentials() {
		if record != nil && record.LogicalName == "" {
			record.LogicalName = request.GetSelector().GetLogicalName()
		}
	}
	return cloned, nil
}

func (c *orphanCredentialIssuerClient) Delete(_ context.Context, request *pb.CredentialDeleteRequest, _ ...grpc.CallOption) (*pb.CredentialDeleteResponse, error) {
	c.deleted = append(c.deleted, request.GetIdentifier())
	c.deleteOperationIDs = append(c.deleteOperationIDs, request.GetOperationId())
	if c.cancelAfterDelete != nil {
		cancel := c.cancelAfterDelete
		c.cancelAfterDelete = nil
		cancel()
	}
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	return &pb.CredentialDeleteResponse{
		Identifier: request.GetIdentifier(), ReconciliationState: pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED,
	}, nil
}
