// This fixture substitutes only cloud execution with an in-memory native job
// provider. The SDK owns registration, protobuf conversion, and process serving.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type jobRecord struct {
	handle   interfaces.JobHandle
	state    interfaces.JobState
	message  string
	deadline time.Time
}

type jobProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	mu   sync.Mutex
	next int
	jobs map[string]*jobRecord
}

func (p *jobProvider) RunJob(_ context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	metadata := map[string]string{
		"pid": strconv.Itoa(os.Getpid()), "run_count": strconv.Itoa(p.next),
		"timeout_seconds": strconv.Itoa(spec.TimeoutSeconds),
	}
	if spec.Target != nil {
		metadata["target_name"] = spec.Target.Name
		metadata["target_type"] = spec.Target.Type
		metadata["target_id"] = spec.Target.ProviderID
	}
	handle := interfaces.JobHandle{ID: fmt.Sprintf("job-%d", p.next), Name: spec.Name, Provider: "native-launch-fixture", Metadata: metadata}
	record := &jobRecord{handle: handle, state: interfaces.JobStateRunning, message: "running"}
	if spec.TimeoutSeconds > 0 {
		record.deadline = time.Now().Add(time.Duration(spec.TimeoutSeconds) * time.Second)
	}
	p.jobs[handle.ID] = record
	return &handle, nil
}

func (p *jobProvider) JobStatus(_ context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	record := p.jobs[handle.ID]
	if record == nil {
		return nil, interfaces.ErrResourceNotFound
	}
	if record.state == interfaces.JobStateRunning && !record.deadline.IsZero() && !time.Now().Before(record.deadline) {
		record.state, record.message = interfaces.JobStateFailed, "timeout reached"
	}
	return &interfaces.JobStatusReply{Handle: record.handle, State: record.state, Message: record.message}, nil
}

func (*jobProvider) JobLogs(_ context.Context, _ interfaces.JobHandle, sink interfaces.LogCaptureSink) error {
	return sink.WriteLogChunk(interfaces.LogChunk{Data: []byte("native job fixture\n"), Source: "stdout", EOF: true})
}

type cancelableJobProvider struct{ *jobProvider }

func (p *cancelableJobProvider) CancelJob(ctx context.Context, handle interfaces.JobHandle) error {
	p.mu.Lock()
	record := p.jobs[handle.ID]
	if record == nil {
		p.mu.Unlock()
		return interfaces.ErrResourceNotFound
	}
	switch record.handle.Name {
	case "cancel-unimplemented":
		p.mu.Unlock()
		return interfaces.ErrProviderMethodUnimplemented
	case "cancel-denied":
		p.mu.Unlock()
		return status.Error(codes.PermissionDenied, "fixture denied")
	case "cancel-unavailable":
		p.mu.Unlock()
		return status.Error(codes.Unavailable, "fixture unavailable")
	case "cancel-block":
		record.message = "cancel waiting"
		p.mu.Unlock()
		<-ctx.Done()
		p.mu.Lock()
		record.message = "cancel context canceled"
		p.mu.Unlock()
		return ctx.Err()
	default:
		record.state, record.message = interfaces.JobStateCancelled, "cancelled"
		p.mu.Unlock()
		return nil
	}
}

func main() {
	provider := &jobProvider{jobs: make(map[string]*jobRecord)}
	if filepath.Base(os.Args[0]) == "jobs-legacy" {
		sdk.ServeIaCPlugin(provider, sdk.IaCServeOptions{})
		return
	}
	sdk.ServeIaCPlugin(&cancelableJobProvider{provider}, sdk.IaCServeOptions{})
}
