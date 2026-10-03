package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type jobLaunchLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *jobLaunchLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *jobLaunchLog) snapshot() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func buildJobLaunchFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "fixture")
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./testdata/iac-job-plugin")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native SDK fixture: %v\n%s", err, output)
	}
	t.Log("built native external SDK fixture with go build -race")
	for _, name := range []string{"jobs-native", "jobs-legacy"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(binary, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		manifest, err := json.Marshal(pluginpkg.PluginManifest{
			Name: name, Version: "0.1.0", Author: "workflow-tests", Description: "Native SDK job launch fixture",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadJobLaunchFixture(t *testing.T, root, name string) (*ExternalPluginAdapter, *providerclient.Adapter) {
	t.Helper()
	logs := &jobLaunchLog{}
	manager := NewExternalPluginManager(root, log.New(logs, "", 0))
	t.Cleanup(func() {
		manager.Shutdown()
		transcript := logs.snapshot()
		for _, signature := range []string{
			"panic:", "runtime error:", "no such host", "module not found", "import error", "version mismatch",
			"incompatible api version", "schema drift", "missing column", "constraint violation", "permission denied",
			"address already in use", "goroutine ", "data race",
		} {
			if strings.Contains(strings.ToLower(transcript), signature) {
				t.Errorf("external process failure signature %q:\n%s", signature, transcript)
			}
		}
		t.Logf("external process transcript:\n%s", transcript)
	})
	names, err := manager.DiscoverPlugins()
	if err != nil || len(names) != 2 {
		t.Fatalf("discover external fixtures: %v, %v", names, err)
	}
	external, err := manager.LoadPlugin(name)
	if err != nil {
		t.Fatalf("launch external SDK fixture: %v", err)
	}
	if err := external.ContractRegistryError(); err != nil {
		t.Fatal(err)
	}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)
	hooks := external.PreInitWiringHooks()
	if len(hooks) != 1 {
		t.Fatalf("expected IaC provider DI hook, got %d", len(hooks))
	}
	if err := hooks[0].Hook(app, nil); err != nil {
		t.Fatal(err)
	}
	var provider interfaces.IaCProvider
	if err := app.GetService(name, &provider); err != nil {
		t.Fatal(err)
	}
	adapter, ok := provider.(*providerclient.Adapter)
	if !ok {
		t.Fatalf("DI provider = %T, want real providerclient.Adapter", provider)
	}
	return external, adapter
}

func waitJobLaunchStatus(t *testing.T, runner interfaces.IaCProviderRunner, handle interfaces.JobHandle, match func(*interfaces.JobStatusReply) bool) *interfaces.JobStatusReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		reply, err := runner.JobStatus(ctx, handle)
		if err != nil {
			t.Fatal(err)
		}
		if match(reply) {
			return reply
		}
		select {
		case <-ctx.Done():
			t.Fatalf("native job did not reach expected state: %s, %s", reply.State, reply.Message)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestIaCJobExternalProcess(t *testing.T) {
	root := buildJobLaunchFixture(t)
	target := &interfaces.ResourceRef{Name: "parent", Type: "custom.parent", ProviderID: "exact-parent-id"}
	t.Run("native runner and canceler", func(t *testing.T) {
		external, adapter := loadJobLaunchFixture(t, root, "jobs-native")
		advertised := false
		for _, contract := range external.ContractRegistry().GetContracts() {
			if contract.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE && contract.GetServiceName() == providerclient.IaCServiceJobCanceler {
				advertised = true
			}
		}
		if !advertised {
			t.Fatal("launched SDK did not advertise native cancellation")
		}
		runner, canceler := adapter.Runner(), adapter.JobCanceler()
		if runner == nil || canceler == nil {
			t.Fatalf("DI lost advertised native capability: runner=%t canceler=%t", runner != nil, canceler != nil)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		runs := 0
		run := func(name string, timeout int) *interfaces.JobHandle {
			t.Helper()
			handle, err := runner.RunJob(ctx, interfaces.JobSpec{Name: name, Kind: interfaces.JobKindEphemeral, Target: target, TimeoutSeconds: timeout})
			if err != nil {
				t.Fatal(err)
			}
			runs++
			pid, err := strconv.Atoi(handle.Metadata["pid"])
			if err != nil || pid <= 0 || pid == os.Getpid() {
				t.Fatalf("native job was not handled in an external process: pid=%q", handle.Metadata["pid"])
			}
			want := map[string]string{
				"pid": strconv.Itoa(pid), "run_count": strconv.Itoa(runs), "timeout_seconds": strconv.Itoa(timeout),
				"target_name": target.Name, "target_type": target.Type, "target_id": target.ProviderID,
			}
			if !reflect.DeepEqual(handle.Metadata, want) {
				t.Fatalf("native process received different target/timeout: %+v, want %+v", handle.Metadata, want)
			}
			t.Logf("native process pid=%d received target=%s/%s/%s timeout=%ss job=%s", pid, target.Name, target.Type, target.ProviderID, handle.Metadata["timeout_seconds"], handle.ID)
			return handle
		}
		minimum := run("bounded-minimum", 1)
		reply := waitJobLaunchStatus(t, runner, *minimum, func(reply *interfaces.JobStatusReply) bool { return reply.State == interfaces.JobStateFailed })
		if reply.Message != "timeout reached" {
			t.Fatalf("native timeout was not observed: %s", reply.Message)
		}
		t.Logf("native timeout result: state=%s message=%s", reply.State, reply.Message)
		maximum := run("bounded-maximum", 3600)
		if err := canceler.CancelJob(ctx, *maximum); err != nil {
			t.Fatal(err)
		}
		reply = waitJobLaunchStatus(t, runner, *maximum, func(reply *interfaces.JobStatusReply) bool { return reply.State == interfaces.JobStateCancelled })
		if !reflect.DeepEqual(reply.Handle, *maximum) {
			t.Fatal("cancelled native job handle changed across the process boundary")
		}
		t.Logf("native cancellation result: state=%s", reply.State)
		for _, timeout := range []int{-1, 0, 3601} {
			handle, err := runner.RunJob(ctx, interfaces.JobSpec{Name: "invalid", Target: target, TimeoutSeconds: timeout})
			if handle != nil || !errors.Is(err, interfaces.ErrValidation) {
				t.Fatalf("invalid targeted timeout %d: %v", timeout, err)
			}
			_, err = pb.NewIaCProviderRunnerClient(external.Conn()).RunJob(ctx, &pb.JobSpec{
				Name: "invalid", Target: &pb.ResourceRef{Name: target.Name, Type: target.Type, ProviderId: target.ProviderID}, TimeoutSeconds: int32(timeout), //nolint:gosec // G115: fixed test cases are within int32 range
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("native SDK accepted invalid targeted timeout %d: %v", timeout, err)
			}
		}
		for _, tc := range []struct {
			name string
			code codes.Code
		}{
			{"cancel-unimplemented", codes.Unimplemented}, {"cancel-denied", codes.PermissionDenied}, {"cancel-unavailable", codes.Unavailable},
		} {
			handle := run(tc.name, 60)
			err := canceler.CancelJob(ctx, *handle)
			if status.Code(err) != tc.code || errors.Is(err, interfaces.ErrProviderMethodUnimplemented) != (tc.code == codes.Unimplemented) {
				t.Fatalf("native cancel error %s: %v", tc.name, err)
			}
			t.Logf("native cancellation failure preserved: %s", status.Code(err))
		}
		blocked := run("cancel-block", 60)
		callerCtx, callerCancel := context.WithCancel(ctx)
		defer callerCancel()
		result := make(chan error, 1)
		go func() { result <- canceler.CancelJob(callerCtx, *blocked) }()
		waitJobLaunchStatus(t, runner, *blocked, func(reply *interfaces.JobStatusReply) bool { return reply.Message == "cancel waiting" })
		callerCancel()
		select {
		case err := <-result:
			if status.Code(err) != codes.Canceled {
				t.Fatalf("caller cancellation changed across process boundary: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("caller cancellation did not end RPC")
		}
		reply = waitJobLaunchStatus(t, runner, *blocked, func(reply *interfaces.JobStatusReply) bool { return reply.Message == "cancel context canceled" })
		t.Logf("native caller-context observation: %s", reply.Message)
	})
	t.Run("legacy runner without canceler", func(t *testing.T) {
		external, adapter := loadJobLaunchFixture(t, root, "jobs-legacy")
		for _, contract := range external.ContractRegistry().GetContracts() {
			if contract.GetServiceName() == providerclient.IaCServiceJobCanceler {
				t.Fatal("SDK advertised a cancellation interface the provider does not implement")
			}
		}
		if adapter.Runner() == nil || adapter.JobCanceler() != nil {
			t.Fatal("legacy runner discovery changed")
		}
		handle, err := adapter.Runner().RunJob(t.Context(), interfaces.JobSpec{Name: "legacy", Kind: interfaces.JobKindEphemeral})
		if err != nil || handle == nil || handle.Metadata["timeout_seconds"] != "0" || handle.Metadata["target_id"] != "" {
			t.Fatalf("untargeted legacy zero-timeout job: %+v, %v", handle, err)
		}
		t.Log("launched legacy native runner: zero timeout preserved; cancellation absent")
	})
}
