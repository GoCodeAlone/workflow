package sdk_test

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
)

type typedJobRunnerOnly struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderRunnerServer
}

type typedJobCancelerOnly struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderJobCancelerServer
}

func TestProviderJobTypedSDKCapabilitiesRegisterIndependently(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider any
		runner   bool
		canceler bool
	}{
		{"legacy", &pb.UnimplementedIaCProviderRequiredServer{}, false, false},
		{"typed runner", &typedJobRunnerOnly{}, true, false},
		{"typed canceler", &typedJobCancelerOnly{}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := grpc.NewServer()
			t.Cleanup(server.Stop)
			if err := sdk.RegisterAllIaCProviderServices(server, tc.provider); err != nil {
				t.Fatal(err)
			}
			services := serviceNamesFromRegistry(sdk.BuildContractRegistry(server))
			if services[pb.IaCProviderRunner_ServiceDesc.ServiceName] != tc.runner || services[pb.IaCProviderJobCanceler_ServiceDesc.ServiceName] != tc.canceler {
				t.Fatalf("SDK service registration = %v; want runner %v, canceler %v", services, tc.runner, tc.canceler)
			}
		})
	}
}
