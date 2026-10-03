package sdk

import (
	"context"
	"errors"
	"math"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type nativeIaCJobRunnerServer struct {
	pb.UnimplementedIaCProviderRunnerServer
	runner interfaces.IaCProviderRunner
}

func (s *nativeIaCJobRunnerServer) RunJob(ctx context.Context, request *pb.JobSpec) (*pb.JobHandle, error) {
	spec := nativeJobSpecFromPB(request)
	if err := spec.Validate(); err != nil {
		return nil, nativeJobRPCError(err)
	}
	handle, err := s.runner.RunJob(ctx, spec)
	if err != nil {
		return nil, nativeJobRPCError(err)
	}
	if handle == nil {
		return nil, status.Error(codes.Internal, "native provider returned nil job handle")
	}
	return nativeJobHandleToPB(*handle), nil
}

func (s *nativeIaCJobRunnerServer) JobStatus(ctx context.Context, request *pb.JobHandle) (*pb.JobStatusReply, error) {
	reply, err := s.runner.JobStatus(ctx, nativeJobHandleFromPB(request))
	if err != nil {
		return nil, nativeJobRPCError(err)
	}
	if reply == nil {
		return nil, status.Error(codes.Internal, "native provider returned nil job status")
	}
	if reply.ExitCode < math.MinInt32 || reply.ExitCode > math.MaxInt32 {
		return nil, status.Error(codes.Internal, "native provider job exit code is out of int32 range")
	}
	return &pb.JobStatusReply{
		Handle: nativeJobHandleToPB(reply.Handle), State: nativeJobStateToPB(reply.State),
		ExitCode: int32(reply.ExitCode), Message: reply.Message, //nolint:gosec // G115: range checked above
	}, nil
}

func (s *nativeIaCJobRunnerServer) JobLogs(request *pb.JobHandle, stream grpc.ServerStreamingServer[pb.LogChunk]) error {
	return nativeJobRPCError(s.runner.JobLogs(stream.Context(), nativeJobHandleFromPB(request), nativeJobLogSink{stream: stream}))
}

type nativeJobLogSink struct {
	stream grpc.ServerStreamingServer[pb.LogChunk]
}

func (s nativeJobLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	return s.stream.Send(&pb.LogChunk{Data: append([]byte(nil), chunk.Data...), Source: chunk.Source, Eof: chunk.EOF})
}

type nativeIaCJobCancelerServer struct {
	pb.UnimplementedIaCProviderJobCancelerServer
	canceler interfaces.IaCProviderJobCanceler
}

func (s *nativeIaCJobCancelerServer) CancelJob(ctx context.Context, request *pb.JobHandle) (*emptypb.Empty, error) {
	if err := s.canceler.CancelJob(ctx, nativeJobHandleFromPB(request)); err != nil {
		return nil, nativeJobRPCError(err)
	}
	return &emptypb.Empty{}, nil
}

func nativeJobRPCError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, interfaces.ErrProviderMethodUnimplemented):
		return status.Error(codes.Unimplemented, err.Error())
	case errors.Is(err, interfaces.ErrValidation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	default:
		return err
	}
}

func nativeJobSpecFromPB(request *pb.JobSpec) interfaces.JobSpec {
	spec := interfaces.JobSpec{
		Name: request.GetName(), Kind: request.GetKind(), Image: request.GetImage(),
		RunCommand: request.GetRunCommand(), Cron: request.GetCron(),
		EnvVars: nativeJobStringMap(request.GetEnvVars()), EnvVarsSecret: nativeJobStringMap(request.GetEnvVarsSecret()),
		TimeoutSeconds: int(request.GetTimeoutSeconds()),
	}
	if target := request.GetTarget(); target != nil {
		spec.Target = &interfaces.ResourceRef{Name: target.GetName(), Type: target.GetType(), ProviderID: target.GetProviderId()}
	}
	if termination := request.GetTermination(); termination != nil {
		spec.Termination = &interfaces.TerminationSpec{
			DrainSeconds: int(termination.GetDrainSeconds()), GracePeriodSeconds: int(termination.GetGracePeriodSeconds()),
		}
	}
	for _, alert := range request.GetAlerts() {
		spec.Alerts = append(spec.Alerts, interfaces.AlertSpec{
			Rule: alert.GetRule(), Operator: alert.GetOperator(), Value: alert.GetValue(), Window: alert.GetWindow(), Disabled: alert.GetDisabled(),
		})
	}
	for _, destination := range request.GetLogDestinations() {
		spec.LogDestinations = append(spec.LogDestinations, interfaces.LogDestinationSpec{
			Name: destination.GetName(), Endpoint: destination.GetEndpoint(), Headers: nativeJobStringMap(destination.GetHeaders()), TLS: destination.GetTls(),
		})
	}
	return spec
}

func nativeJobHandleFromPB(handle *pb.JobHandle) interfaces.JobHandle {
	return interfaces.JobHandle{
		ID: handle.GetId(), Name: handle.GetName(), Provider: handle.GetProvider(), Metadata: nativeJobStringMap(handle.GetMetadata()),
	}
}

func nativeJobHandleToPB(handle interfaces.JobHandle) *pb.JobHandle {
	return &pb.JobHandle{
		Id: handle.ID, Name: handle.Name, Provider: handle.Provider, Metadata: nativeJobStringMap(handle.Metadata),
	}
}

func nativeJobStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func nativeJobStateToPB(state interfaces.JobState) pb.JobState {
	switch state {
	case interfaces.JobStatePending:
		return pb.JobState_JOB_STATE_PENDING
	case interfaces.JobStateRunning:
		return pb.JobState_JOB_STATE_RUNNING
	case interfaces.JobStateSucceeded:
		return pb.JobState_JOB_STATE_SUCCEEDED
	case interfaces.JobStateFailed:
		return pb.JobState_JOB_STATE_FAILED
	case interfaces.JobStateCancelled:
		return pb.JobState_JOB_STATE_CANCELLED
	default:
		return pb.JobState_JOB_STATE_UNSPECIFIED
	}
}
