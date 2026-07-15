package sdk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type secretStoreTestProvider struct {
	declarations   []*pb.SecretStoreDeclaration
	getResponse    *pb.SecretStoreGetResponse
	listResponse   *pb.SecretStoreListResponse
	statResponse   *pb.SecretStoreStatAllResponse
	accessResponse *pb.SecretStoreCheckAccessResponse
	getErr         error
	listErr        error
	statErr        error
	accessErr      error
	getHook        func(context.Context, *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error)
	calls          atomic.Int32
	listPageSize   atomic.Int32
	statPageSize   atomic.Int32
}

func (p *secretStoreTestProvider) SecretStores() []*pb.SecretStoreDeclaration {
	return p.declarations
}

func (p *secretStoreTestProvider) Get(ctx context.Context, request *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error) {
	p.calls.Add(1)
	if p.getHook != nil {
		return p.getHook(ctx, request)
	}
	mutateSecretStoreTarget(request.GetTarget())
	request.Key = "provider-mutated-key"
	return p.getResponse, p.getErr
}

func (p *secretStoreTestProvider) List(_ context.Context, request *pb.SecretStoreListRequest) (*pb.SecretStoreListResponse, error) {
	p.calls.Add(1)
	p.listPageSize.Store(request.GetPageSize())
	mutateSecretStoreTarget(request.GetTarget())
	if len(request.PageToken) > 0 {
		request.PageToken[0] = 'X'
	}
	request.PageSize = 1
	return p.listResponse, p.listErr
}

func (p *secretStoreTestProvider) StatAll(_ context.Context, request *pb.SecretStoreStatAllRequest) (*pb.SecretStoreStatAllResponse, error) {
	p.calls.Add(1)
	p.statPageSize.Store(request.GetPageSize())
	mutateSecretStoreTarget(request.GetTarget())
	if len(request.PageToken) > 0 {
		request.PageToken[0] = 'X'
	}
	request.PageSize = 1
	return p.statResponse, p.statErr
}

func (p *secretStoreTestProvider) CheckAccess(_ context.Context, request *pb.SecretStoreCheckAccessRequest) (*pb.SecretStoreCheckAccessResponse, error) {
	p.calls.Add(1)
	mutateSecretStoreTarget(request.GetTarget())
	return p.accessResponse, p.accessErr
}

func mutateSecretStoreTarget(target *pb.SecretStoreTarget) {
	if target == nil {
		return
	}
	target.Type = "provider-mutated-type"
	target.Scope = "provider-mutated-scope"
	if len(target.ConfigJson) > 0 {
		target.ConfigJson[0] = 'X'
	}
}

func validSecretStoreTestProvider() *secretStoreTestProvider {
	return &secretStoreTestProvider{
		declarations: []*pb.SecretStoreDeclaration{{
			Type: "store.test", Operations: []string{"get", "list", "stat_all", "check_access"}, Scopes: []string{"account", "region"},
		}},
		getResponse: &pb.SecretStoreGetResponse{Result: &pb.SecretStoreGetResult{Value: []byte("provider-value")}},
		listResponse: &pb.SecretStoreListResponse{Result: &pb.SecretStoreListResult{
			Names: []string{"alpha", "beta"}, NextPageToken: []byte("next-list"),
		}},
		statResponse: &pb.SecretStoreStatAllResponse{Result: &pb.SecretStoreStatAllResult{
			Items: []*pb.SecretStoreMetadata{
				{Name: "alpha", Exists: true, UpdatedAt: timestamppb.New(time.Unix(1_700_000_000, 123))},
				{Name: "beta", Exists: false},
			},
			NextPageToken: []byte("next-stat"),
		}},
		accessResponse: &pb.SecretStoreCheckAccessResponse{},
	}
}

func validSecretStoreTarget() *pb.SecretStoreTarget {
	return &pb.SecretStoreTarget{Type: "store.test", Scope: "region", ConfigJson: []byte(`{"region":"us-test-1","token":"request-secret"}`)}
}

func TestSecretStoreCanonicalizesAndClonesDeclarations(t *testing.T) {
	provider := validSecretStoreTestProvider()
	provider.declarations = []*pb.SecretStoreDeclaration{
		{Type: " z.store ", Operations: []string{" stat_all ", "check_access", " get ", "list"}, Scopes: []string{" region ", " account "}},
		{Type: "a.store", Operations: []string{"list", "get"}, Scopes: []string{"project"}},
	}
	unknown := secretStoreUnknown("provider-declaration-secret")
	provider.declarations[0].ProtoReflect().SetUnknown(unknown)
	server := newSecretStoreServer(provider)
	if err := server.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	first, err := server.DescribeSecretStores(context.Background(), &pb.SecretStoreDeclarationsRequest{})
	if err != nil || first.GetError() != nil {
		t.Fatalf("DescribeSecretStores = %v, %v", first, err)
	}
	if len(first.GetStores()) != 2 || first.GetStores()[0].GetType() != "a.store" || first.GetStores()[1].GetType() != "z.store" {
		t.Fatalf("declaration order = %v", first.GetStores())
	}
	if !sameSDKStrings(first.GetStores()[1].GetOperations(), []string{"get", "list", "stat_all", "check_access"}) {
		t.Fatalf("operation order = %v", first.GetStores()[1].GetOperations())
	}
	if !sameSDKStrings(first.GetStores()[1].GetScopes(), []string{"account", "region"}) {
		t.Fatalf("scope order = %v", first.GetStores()[1].GetScopes())
	}
	assertSecretStoreWireExcludes(t, first, "provider-declaration-secret")
	first.Stores[0].Type = "caller-mutated"
	first.Stores[1].Operations[0] = "caller-mutated"
	provider.declarations[0].Type = "provider-mutated"
	second, err := server.DescribeSecretStores(context.Background(), &pb.SecretStoreDeclarationsRequest{})
	if err != nil || second.GetStores()[0].GetType() != "a.store" || second.GetStores()[1].GetOperations()[0] != "get" {
		t.Fatalf("declarations were not independently reconstructed: %v, %v", second, err)
	}
}

func TestSecretStoreRejectsInvalidProvidersAndDeclarations(t *testing.T) {
	var typedNil *secretStoreTestProvider
	tests := []struct {
		name     string
		provider SecretStoreProvider
		contains string
	}{
		{name: "nil", contains: "nil"},
		{name: "typed nil", provider: typedNil, contains: "nil"},
		{name: "none", provider: &secretStoreTestProvider{}, contains: "at least one"},
		{name: "nil declaration", provider: secretStoreProviderWithDeclarations(nil), contains: "nil"},
		{name: "empty type", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: " ", Operations: []string{"get"}, Scopes: []string{"account"}}), contains: "type"},
		{name: "invalid type utf8", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: string([]byte{0xff}), Operations: []string{"get"}, Scopes: []string{"account"}}), contains: "type"},
		{name: "long type", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: strings.Repeat("t", maxSecretStoreTypeBytes+1), Operations: []string{"get"}, Scopes: []string{"account"}}), contains: "type"},
		{name: "duplicate canonical type", provider: secretStoreProviderWithDeclarations(
			&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}, Scopes: []string{"account"}},
			&pb.SecretStoreDeclaration{Type: " store.test ", Operations: []string{"list"}, Scopes: []string{"region"}},
		), contains: "duplicated"},
		{name: "no operations", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Scopes: []string{"account"}}), contains: "operation"},
		{name: "empty operation", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{" "}, Scopes: []string{"account"}}), contains: "operation"},
		{name: "duplicate canonical operation", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get", " get "}, Scopes: []string{"account"}}), contains: "duplicates"},
		{name: "mutation operation", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"set"}, Scopes: []string{"account"}}), contains: "unsupported"},
		{name: "no scopes", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}}), contains: "scope"},
		{name: "empty scope", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}, Scopes: []string{" "}}), contains: "scope"},
		{name: "invalid scope utf8", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}, Scopes: []string{string([]byte{0xff})}}), contains: "scope"},
		{name: "long scope", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}, Scopes: []string{strings.Repeat("s", maxSecretStoreScopeBytes+1)}}), contains: "scope"},
		{name: "duplicate canonical scope", provider: secretStoreProviderWithDeclarations(&pb.SecretStoreDeclaration{Type: "store.test", Operations: []string{"get"}, Scopes: []string{"account", " account "}}), contains: "duplicates"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newSecretStoreServer(test.provider)
			if err := server.validate(); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("validate = %v, want %q", err, test.contains)
			}
			response, err := server.DescribeSecretStores(context.Background(), &pb.SecretStoreDeclarationsRequest{})
			if err != nil || response.GetError().GetCode() != "invalid_declaration" || len(response.GetStores()) != 0 {
				t.Fatalf("DescribeSecretStores = %v, %v", response, err)
			}
		})
	}
}

func secretStoreProviderWithDeclarations(declarations ...*pb.SecretStoreDeclaration) *secretStoreTestProvider {
	return &secretStoreTestProvider{declarations: declarations}
}

func TestSecretStoreRejectsRequestsBeforeProvider(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*secretStoreServer) (proto.Message, error)
		wantCode string
	}{
		{name: "nil get", call: func(s *secretStoreServer) (proto.Message, error) { return s.Get(context.Background(), nil) }, wantCode: "target_type_required"},
		{name: "nil list", call: func(s *secretStoreServer) (proto.Message, error) { return s.List(context.Background(), nil) }, wantCode: "target_type_required"},
		{name: "nil stat", call: func(s *secretStoreServer) (proto.Message, error) { return s.StatAll(context.Background(), nil) }, wantCode: "target_type_required"},
		{name: "nil access", call: func(s *secretStoreServer) (proto.Message, error) { return s.CheckAccess(context.Background(), nil) }, wantCode: "target_type_required"},
		{name: "missing type", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Scope: "region"}, Key: "key"})
		}, wantCode: "target_type_required"},
		{name: "non-exact type", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test ", Scope: "region"}, Key: "key"})
		}, wantCode: "unsupported_store_type"},
		{name: "missing scope", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test"}, Key: "key"})
		}, wantCode: "target_scope_required"},
		{name: "non-exact scope", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test", Scope: "region "}, Key: "key"})
		}, wantCode: "unsupported_scope"},
		{name: "invalid config", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test", Scope: "region", ConfigJson: []byte("request-secret{")}, Key: "key"})
		}, wantCode: "invalid_config"},
		{name: "non-object config", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test", Scope: "region", ConfigJson: []byte(`"request-secret"`)}, Key: "key"})
		}, wantCode: "invalid_config"},
		{name: "invalid config utf8", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test", Scope: "region", ConfigJson: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}}, Key: "key"})
		}, wantCode: "invalid_config"},
		{name: "oversized config", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: &pb.SecretStoreTarget{Type: "store.test", Scope: "region", ConfigJson: bytes.Repeat([]byte{'x'}, maxSecretStoreConfigBytes+1)}, Key: "key"})
		}, wantCode: "invalid_config"},
		{name: "missing key", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget()})
		}, wantCode: "key_required"},
		{name: "non-canonical key", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: " key "})
		}, wantCode: "invalid_key"},
		{name: "long key", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: strings.Repeat("k", maxSecretStoreNameBytes+1)})
		}, wantCode: "invalid_key"},
		{name: "negative list page", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: -1})
		}, wantCode: "invalid_page_size"},
		{name: "large list page", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: maxSecretStorePageSize + 1})
		}, wantCode: "invalid_page_size"},
		{name: "large list token", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageToken: bytes.Repeat([]byte{'t'}, maxSecretStorePageTokenBytes+1)})
		}, wantCode: "invalid_page_token"},
		{name: "large stat token", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageToken: bytes.Repeat([]byte{'t'}, maxSecretStorePageTokenBytes+1)})
		}, wantCode: "invalid_page_token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validSecretStoreTestProvider()
			response, err := test.call(newSecretStoreServer(provider))
			if err != nil || secretStoreResponseError(response).GetCode() != test.wantCode || provider.calls.Load() != 0 {
				t.Fatalf("response = %v, %v; calls = %d", response, err, provider.calls.Load())
			}
			if strings.Contains(fmt.Sprint(response), "request-secret") {
				t.Fatalf("request config leaked in error response: %v", response)
			}
		})
	}

	provider := validSecretStoreTestProvider()
	provider.declarations = []*pb.SecretStoreDeclaration{{Type: "store.test", Operations: []string{"get"}, Scopes: []string{"region"}}}
	for _, call := range []func(*secretStoreServer) (proto.Message, error){
		func(s *secretStoreServer) (proto.Message, error) {
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
		},
		func(s *secretStoreServer) (proto.Message, error) {
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
		},
		func(s *secretStoreServer) (proto.Message, error) {
			return s.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
		},
	} {
		response, err := call(newSecretStoreServer(provider))
		if err != nil || secretStoreResponseError(response).GetCode() != "unsupported_operation" || provider.calls.Load() != 0 {
			t.Fatalf("undeclared operation = %v, %v; calls = %d", response, err, provider.calls.Load())
		}
	}
}

func TestSecretStoreClonesRequestsAndReconstructsSuccessResponses(t *testing.T) {
	target := validSecretStoreTarget()
	getRequest := &pb.SecretStoreGetRequest{Target: target, Key: "caller-key"}
	getBefore := proto.Clone(getRequest).(*pb.SecretStoreGetRequest)
	provider := validSecretStoreTestProvider()
	getResponse, err := newSecretStoreServer(provider).Get(context.Background(), getRequest)
	if err != nil || getResponse.GetError() != nil || string(getResponse.GetResult().GetValue()) != "provider-value" || !proto.Equal(getRequest, getBefore) {
		t.Fatalf("Get = %v, %v; request = %v", getResponse, err, getRequest)
	}
	getResponse.Result.Value[0] = 'X'
	if string(provider.getResponse.GetResult().GetValue()) != "provider-value" {
		t.Fatal("Get returned provider-owned value storage")
	}

	listRequest := &pb.SecretStoreListRequest{Target: target, PageSize: 2, PageToken: []byte("list-token")}
	listBefore := proto.Clone(listRequest).(*pb.SecretStoreListRequest)
	listResponse, err := newSecretStoreServer(provider).List(context.Background(), listRequest)
	if err != nil || listResponse.GetError() != nil || !sameSDKStrings(listResponse.GetResult().GetNames(), []string{"alpha", "beta"}) || !proto.Equal(listRequest, listBefore) {
		t.Fatalf("List = %v, %v; request = %v", listResponse, err, listRequest)
	}
	listResponse.Result.Names[0] = "caller-mutated"
	listResponse.Result.NextPageToken[0] = 'X'
	if provider.listResponse.GetResult().GetNames()[0] != "alpha" || string(provider.listResponse.GetResult().GetNextPageToken()) != "next-list" {
		t.Fatal("List returned provider-owned storage")
	}

	statRequest := &pb.SecretStoreStatAllRequest{Target: target, PageSize: 2, PageToken: []byte("stat-token")}
	statBefore := proto.Clone(statRequest).(*pb.SecretStoreStatAllRequest)
	statResponse, err := newSecretStoreServer(provider).StatAll(context.Background(), statRequest)
	if err != nil || statResponse.GetError() != nil || len(statResponse.GetResult().GetItems()) != 2 || !proto.Equal(statRequest, statBefore) {
		t.Fatalf("StatAll = %v, %v; request = %v", statResponse, err, statRequest)
	}
	statResponse.Result.Items[0].Name = "caller-mutated"
	statResponse.Result.Items[0].UpdatedAt.Seconds = 1
	if provider.statResponse.GetResult().GetItems()[0].GetName() != "alpha" || provider.statResponse.GetResult().GetItems()[0].GetUpdatedAt().GetSeconds() != 1_700_000_000 {
		t.Fatal("StatAll returned provider-owned storage")
	}

	accessRequest := &pb.SecretStoreCheckAccessRequest{Target: target}
	accessBefore := proto.Clone(accessRequest).(*pb.SecretStoreCheckAccessRequest)
	accessResponse, err := newSecretStoreServer(provider).CheckAccess(context.Background(), accessRequest)
	if err != nil || accessResponse.GetError() != nil || !proto.Equal(accessRequest, accessBefore) {
		t.Fatalf("CheckAccess = %v, %v; request = %v", accessResponse, err, accessRequest)
	}
}

func TestSecretStoreRejectsUnsafeProviderResults(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*secretStoreServer) (proto.Message, error)
		wantCode string
	}{
		{name: "get error", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).getErr = errors.New("provider-secret")
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		}, wantCode: "provider_error"},
		{name: "get nil", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).getResponse = nil
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		}, wantCode: "empty_response"},
		{name: "get missing result", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).getResponse = &pb.SecretStoreGetResponse{}
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		}, wantCode: "empty_response"},
		{name: "get oversized", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).getResponse.Result.Value = bytes.Repeat([]byte{'v'}, maxSecretStoreValueBytes+1)
			return s.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		}, wantCode: "invalid_response"},
		{name: "list error", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listErr = errors.New("provider-secret")
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
		}, wantCode: "provider_error"},
		{name: "list nil", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse = nil
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
		}, wantCode: "empty_response"},
		{name: "list missing result", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse = &pb.SecretStoreListResponse{}
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
		}, wantCode: "empty_response"},
		{name: "list too many", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{"one", "two"}
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "list duplicate", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{"same", "same"}
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 2})
		}, wantCode: "invalid_response"},
		{name: "list empty name", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{" "}
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "list long name", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{strings.Repeat("n", maxSecretStoreNameBytes+1)}
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "list long token", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{"one"}
			s.provider.(*secretStoreTestProvider).listResponse.Result.NextPageToken = bytes.Repeat([]byte{'t'}, maxSecretStorePageTokenBytes+1)
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "list stagnant token", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).listResponse.Result.Names = []string{"one"}
			s.provider.(*secretStoreTestProvider).listResponse.Result.NextPageToken = []byte("same")
			return s.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 1, PageToken: []byte("same")})
		}, wantCode: "invalid_response"},
		{name: "stat nil item", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse.Result.Items = []*pb.SecretStoreMetadata{nil}
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "stat error", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statErr = errors.New("provider-secret")
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
		}, wantCode: "provider_error"},
		{name: "stat nil", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse = nil
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
		}, wantCode: "empty_response"},
		{name: "stat missing result", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse = &pb.SecretStoreStatAllResponse{}
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
		}, wantCode: "empty_response"},
		{name: "stat duplicate", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse.Result.Items = []*pb.SecretStoreMetadata{{Name: "same"}, {Name: "same"}}
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 2})
		}, wantCode: "invalid_response"},
		{name: "stat invalid timestamp", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse.Result.Items = []*pb.SecretStoreMetadata{{Name: "one", UpdatedAt: &timestamppb.Timestamp{Seconds: 253_402_300_800}}}
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "stat too many", call: func(s *secretStoreServer) (proto.Message, error) {
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 1})
		}, wantCode: "invalid_response"},
		{name: "stat stagnant token", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).statResponse.Result.Items = []*pb.SecretStoreMetadata{{Name: "one"}}
			s.provider.(*secretStoreTestProvider).statResponse.Result.NextPageToken = []byte("same")
			return s.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 1, PageToken: []byte("same")})
		}, wantCode: "invalid_response"},
		{name: "access nil", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).accessResponse = nil
			return s.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
		}, wantCode: "empty_response"},
		{name: "access error", call: func(s *secretStoreServer) (proto.Message, error) {
			s.provider.(*secretStoreTestProvider).accessErr = errors.New("provider-secret")
			return s.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
		}, wantCode: "provider_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validSecretStoreTestProvider()
			response, err := test.call(newSecretStoreServer(provider))
			if err != nil || secretStoreResponseError(response).GetCode() != test.wantCode || secretStoreResponseHasResult(response) {
				t.Fatalf("response = %v, %v", response, err)
			}
			if strings.Contains(fmt.Sprint(response), "provider-secret") || strings.Contains(fmt.Sprint(response), "request-secret") {
				t.Fatalf("secret leaked in response: %v", response)
			}
		})
	}
}

func TestSecretStorePaginationNormalizesDefaultsAndAllowsEmptyAdvancingPages(t *testing.T) {
	provider := validSecretStoreTestProvider()
	provider.listResponse.Result.Names = nil
	provider.listResponse.Result.NextPageToken = []byte("next-list")
	listResponse, err := newSecretStoreServer(provider).List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
	if err != nil || listResponse.GetError() != nil || string(listResponse.GetResult().GetNextPageToken()) != "next-list" {
		t.Fatalf("List empty advancing page = %v, %v", listResponse, err)
	}
	if provider.listPageSize.Load() != defaultSecretStorePageSize {
		t.Fatalf("provider List page size = %d, want default %d", provider.listPageSize.Load(), defaultSecretStorePageSize)
	}

	provider = validSecretStoreTestProvider()
	provider.statResponse.Result.Items = nil
	provider.statResponse.Result.NextPageToken = []byte("next-stat")
	statResponse, err := newSecretStoreServer(provider).StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
	if err != nil || statResponse.GetError() != nil || string(statResponse.GetResult().GetNextPageToken()) != "next-stat" {
		t.Fatalf("StatAll empty advancing page = %v, %v", statResponse, err)
	}
	if provider.statPageSize.Load() != defaultSecretStorePageSize {
		t.Fatalf("provider StatAll page size = %d, want default %d", provider.statPageSize.Load(), defaultSecretStorePageSize)
	}
}

func TestSecretStoreSanitizesStructuredErrorsForEveryOperation(t *testing.T) {
	providerError := &pb.SecretStoreError{Code: "safe_code", Message: "provider-secret", Retryable: true}
	provider := validSecretStoreTestProvider()
	provider.getResponse = &pb.SecretStoreGetResponse{Result: &pb.SecretStoreGetResult{Value: []byte("provider-secret")}, Error: providerError}
	provider.listResponse = &pb.SecretStoreListResponse{Result: &pb.SecretStoreListResult{Names: []string{"provider-secret"}}, Error: providerError}
	provider.statResponse = &pb.SecretStoreStatAllResponse{Result: &pb.SecretStoreStatAllResult{Items: []*pb.SecretStoreMetadata{{Name: "provider-secret"}}}, Error: providerError}
	provider.accessResponse = &pb.SecretStoreCheckAccessResponse{Error: providerError}
	server := newSecretStoreServer(provider)
	calls := []func() (proto.Message, error){
		func() (proto.Message, error) {
			return server.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		},
		func() (proto.Message, error) {
			return server.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
		},
		func() (proto.Message, error) {
			return server.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
		},
		func() (proto.Message, error) {
			return server.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
		},
	}
	for _, call := range calls {
		response, err := call()
		if err != nil || secretStoreResponseError(response).GetCode() != "safe_code" || !secretStoreResponseError(response).GetRetryable() || secretStoreResponseHasResult(response) {
			t.Fatalf("structured error = %v, %v", response, err)
		}
		if strings.Contains(fmt.Sprint(response), "provider-secret") {
			t.Fatalf("provider secret leaked: %v", response)
		}
	}
	provider = validSecretStoreTestProvider()
	provider.getResponse = &pb.SecretStoreGetResponse{Error: &pb.SecretStoreError{Code: "BAD provider-secret", Message: "provider-secret", Retryable: true}}
	response, err := newSecretStoreServer(provider).Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
	if err != nil || response.GetError().GetCode() != "provider_error" || strings.Contains(response.String(), "provider-secret") {
		t.Fatalf("invalid error code = %v, %v", response, err)
	}
}

func TestSecretStoreStripsProviderUnknownFieldsFromEveryResponse(t *testing.T) {
	unknown := secretStoreUnknown("provider-unknown-secret")
	provider := validSecretStoreTestProvider()
	provider.getResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.getResponse.Result.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.listResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.listResponse.Result.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.statResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.statResponse.Result.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.statResponse.Result.Items[0].ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.statResponse.Result.Items[0].UpdatedAt.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.accessResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	server := newSecretStoreServer(provider)
	responses := []proto.Message{}
	get, _ := server.Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
	responses = append(responses, get)
	list, _ := server.List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: 2})
	responses = append(responses, list)
	statAll, _ := server.StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget(), PageSize: 2})
	responses = append(responses, statAll)
	access, _ := server.CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
	responses = append(responses, access)
	for _, response := range responses {
		assertSecretStoreWireExcludes(t, response, "provider-unknown-secret")
	}

	provider = validSecretStoreTestProvider()
	providerError := func() *pb.SecretStoreError {
		errorValue := &pb.SecretStoreError{Code: "safe_code", Message: "provider-known-secret", Retryable: true}
		errorValue.ProtoReflect().SetUnknown(bytes.Clone(unknown))
		return errorValue
	}
	provider.getResponse = &pb.SecretStoreGetResponse{Error: providerError()}
	provider.getResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.listResponse = &pb.SecretStoreListResponse{Error: providerError()}
	provider.listResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.statResponse = &pb.SecretStoreStatAllResponse{Error: providerError()}
	provider.statResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	provider.accessResponse = &pb.SecretStoreCheckAccessResponse{Error: providerError()}
	provider.accessResponse.ProtoReflect().SetUnknown(bytes.Clone(unknown))
	errorResponses := []proto.Message{}
	getError, _ := newSecretStoreServer(provider).Get(context.Background(), &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
	errorResponses = append(errorResponses, getError)
	listError, _ := newSecretStoreServer(provider).List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget()})
	errorResponses = append(errorResponses, listError)
	statError, _ := newSecretStoreServer(provider).StatAll(context.Background(), &pb.SecretStoreStatAllRequest{Target: validSecretStoreTarget()})
	errorResponses = append(errorResponses, statError)
	accessError, _ := newSecretStoreServer(provider).CheckAccess(context.Background(), &pb.SecretStoreCheckAccessRequest{Target: validSecretStoreTarget()})
	errorResponses = append(errorResponses, accessError)
	for _, errorResponse := range errorResponses {
		assertSecretStoreWireExcludes(t, errorResponse, "provider-unknown-secret", "provider-known-secret")
	}
}

func TestSecretStoreAcceptsExactLimitsWithIndependentStorage(t *testing.T) {
	config := exactSecretStoreConfig(maxSecretStoreConfigBytes)
	storeType := strings.Repeat("t", maxSecretStoreTypeBytes)
	target := &pb.SecretStoreTarget{Type: storeType, Scope: strings.Repeat("s", maxSecretStoreScopeBytes), ConfigJson: config}
	provider := validSecretStoreTestProvider()
	provider.declarations[0].Type = storeType
	provider.declarations[0].Scopes = []string{target.Scope}
	provider.getResponse.Result.Value = bytes.Repeat([]byte{'v'}, maxSecretStoreValueBytes)
	request := &pb.SecretStoreGetRequest{Target: target, Key: strings.Repeat("k", maxSecretStoreNameBytes)}
	before := proto.Clone(request).(*pb.SecretStoreGetRequest)
	response, err := newSecretStoreServer(provider).Get(context.Background(), request)
	if err != nil || response.GetError() != nil || len(response.GetResult().GetValue()) != maxSecretStoreValueBytes || !proto.Equal(request, before) {
		t.Fatalf("Get at limits = %v, %v", response, err)
	}
	provider.getResponse.Result.Value[0] = 'P'
	if response.GetResult().GetValue()[0] != 'v' {
		t.Fatal("provider retained returned Get value storage")
	}

	names := make([]string, maxSecretStorePageSize)
	for index := range names {
		prefix := fmt.Sprintf("%04d-", index)
		names[index] = prefix + strings.Repeat("x", maxSecretStoreNameBytes-len(prefix))
	}
	provider = validSecretStoreTestProvider()
	provider.listResponse.Result.Names = names
	provider.listResponse.Result.NextPageToken = bytes.Repeat([]byte{'t'}, maxSecretStorePageTokenBytes)
	list, err := newSecretStoreServer(provider).List(context.Background(), &pb.SecretStoreListRequest{Target: validSecretStoreTarget(), PageSize: maxSecretStorePageSize, PageToken: bytes.Repeat([]byte{'p'}, maxSecretStorePageTokenBytes)})
	if err != nil || list.GetError() != nil || len(list.GetResult().GetNames()) != int(maxSecretStorePageSize) || len(list.GetResult().GetNextPageToken()) != maxSecretStorePageTokenBytes {
		t.Fatalf("List at limits = %v, %v", list.GetError(), err)
	}
	provider.listResponse.Result.NextPageToken[0] = 'P'
	if list.GetResult().GetNextPageToken()[0] != 't' {
		t.Fatal("provider retained returned List token storage")
	}
}

func TestSecretStorePropagatesCancellation(t *testing.T) {
	provider := validSecretStoreTestProvider()
	entered := make(chan struct{})
	provider.getHook = func(ctx context.Context, _ *pb.SecretStoreGetRequest) (*pb.SecretStoreGetResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := newSecretStoreServer(provider).Get(ctx, &pb.SecretStoreGetRequest{Target: validSecretStoreTarget(), Key: "key"})
		result <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-result:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("Get cancellation = %v, want Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Get did not return after cancellation")
	}
}

func TestSecretStoreContractAdvertisementIsCanonicalAndNonMutating(t *testing.T) {
	services := &providerServices{secretStore: newSecretStoreServer(validSecretStoreTestProvider())}
	base := &pb.ContractRegistry{Contracts: []*pb.ContractDescriptor{
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.Unrelated", ContractType: "fixture.service"},
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: pb.SecretStore_ServiceDesc.ServiceName, ContractType: "wrong.contract"},
		{Kind: pb.ContractKind_CONTRACT_KIND_SERVICE, ServiceName: "fixture.WrongService", ContractType: SecretStoreContractID},
	}}
	original := proto.Clone(base).(*pb.ContractRegistry)
	merged := mergeProviderServiceContracts(base, services)
	if !proto.Equal(base, original) {
		t.Fatalf("base registry mutated:\n got %v\nwant %v", base, original)
	}
	var canonical *pb.ContractDescriptor
	for _, descriptor := range merged.GetContracts() {
		if descriptor.GetServiceName() == pb.SecretStore_ServiceDesc.ServiceName || descriptor.GetContractType() == SecretStoreContractID {
			if canonical != nil {
				t.Fatalf("multiple secret store descriptors: %v", merged.GetContracts())
			}
			canonical = descriptor
		}
	}
	if canonical == nil || canonical.GetKind() != pb.ContractKind_CONTRACT_KIND_SERVICE ||
		canonical.GetMode() != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO ||
		canonical.GetServiceName() != pb.SecretStore_ServiceDesc.ServiceName ||
		canonical.GetContractType() != SecretStoreContractID ||
		canonical.GetProtocolVersion() != SecretStoreProtocolVersion {
		t.Fatalf("canonical descriptor = %v", canonical)
	}
	for _, messageName := range []string{"SecretStoreGetResult", "SecretStoreListResult", "SecretStoreStatAllResult"} {
		found := false
		for _, advertised := range canonical.GetMessageNames() {
			if advertised == messageName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("canonical descriptor omitted %s: %v", messageName, canonical.GetMessageNames())
		}
	}
	if len(merged.GetContracts()) != 2 || merged.GetContracts()[0].GetContractType() != "fixture.service" {
		t.Fatalf("merged contracts = %v", merged.GetContracts())
	}
}

func exactSecretStoreConfig(size int) []byte {
	prefix := []byte(`{"data":"`)
	suffix := []byte(`"}`)
	return append(append(prefix, bytes.Repeat([]byte{'x'}, size-len(prefix)-len(suffix))...), suffix...)
}

func secretStoreUnknown(value string) []byte {
	unknown := protowire.AppendTag(nil, 100, protowire.BytesType)
	return protowire.AppendBytes(unknown, []byte(value))
}

func assertSecretStoreWireExcludes(t *testing.T, message proto.Message, forbidden ...string) {
	t.Helper()
	wire, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("marshal %T: %v", message, err)
	}
	for _, value := range forbidden {
		if bytes.Contains(wire, []byte(value)) {
			t.Fatalf("%T leaked %q: %x", message, value, wire)
		}
	}
}

func secretStoreResponseError(message proto.Message) *pb.SecretStoreError {
	switch response := message.(type) {
	case *pb.SecretStoreGetResponse:
		return response.GetError()
	case *pb.SecretStoreListResponse:
		return response.GetError()
	case *pb.SecretStoreStatAllResponse:
		return response.GetError()
	case *pb.SecretStoreCheckAccessResponse:
		return response.GetError()
	default:
		return nil
	}
}

func secretStoreResponseHasResult(message proto.Message) bool {
	switch response := message.(type) {
	case *pb.SecretStoreGetResponse:
		return response.GetResult() != nil
	case *pb.SecretStoreListResponse:
		return response.GetResult() != nil
	case *pb.SecretStoreStatAllResponse:
		return response.GetResult() != nil
	default:
		return false
	}
}
