package sdk

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

type logCaptureOnlyServer struct {
	pb.UnimplementedIaCProviderLogCaptureServer
}

type runnerOnlyServer struct {
	pb.UnimplementedIaCProviderRunnerServer
}

func TestBuildContractRegistryAdvertisesLogCaptureService(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderLogCaptureServer(s, &logCaptureOnlyServer{})
	reg := BuildContractRegistry(s)
	services := map[string]bool{}
	for _, c := range reg.GetContracts() {
		services[c.GetServiceName()] = true
	}
	if !services[pb.IaCProviderLogCapture_ServiceDesc.ServiceName] {
		t.Fatalf("registry did not advertise %s", pb.IaCProviderLogCapture_ServiceDesc.ServiceName)
	}
}

func TestBuildContractRegistryAdvertisesRunnerService(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRunnerServer(s, &runnerOnlyServer{})
	reg := BuildContractRegistry(s)
	services := map[string]bool{}
	for _, c := range reg.GetContracts() {
		services[c.GetServiceName()] = true
	}
	if !services[pb.IaCProviderRunner_ServiceDesc.ServiceName] {
		t.Fatalf("registry did not advertise %s", pb.IaCProviderRunner_ServiceDesc.ServiceName)
	}
}
