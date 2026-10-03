//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/sandbox"
	"golang.org/x/sys/unix"
)

var errPipelineCleanupIncomplete = errors.New("pipeline cleanup is incomplete")

type pipelineRecordCapture struct {
	data      bytes.Buffer
	limit     int
	overflow  bool
	err       error
	stdout    bool
	violation func()
}

func (capture *pipelineRecordCapture) Write(data []byte) (int, error) {
	length := len(data)
	remaining := capture.limit - capture.data.Len()
	if len(data) > remaining {
		capture.overflow = true
		data = data[:remaining]
	}
	_, _ = capture.data.Write(data)
	if (capture.overflow || (capture.stdout && length != 0)) && capture.violation != nil {
		capture.violation()
	}
	return length, nil
}

func runPipelineRecord(request pipelineRecordRequest, options pipelineRecordOptions) error {
	if err := validatePipelineRecordSelectors(request.ResultStep, options.SuccessPrefix, options.ErrorPrefix); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, deadlineCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer deadlineCancel()
	output, code := executePipelineRecordParent(ctx, request, options.ConfigPath)
	if code != "" {
		if err := writePipelineRecord(os.Stdout, options.ErrorPrefix, pipelineRecordErrorPayload(code)); err != nil {
			return errors.New("pipeline record output failed")
		}
		return errors.New("pipeline record execution failed")
	}
	if err := writePipelineRecord(os.Stdout, options.SuccessPrefix, output); err != nil {
		return errors.New("pipeline record output failed")
	}
	return nil
}

func pipelineCleanupRoot() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("pipeline cleanup state must be absolute")
	}
	return filepath.Join(base, "wfctl", "pipeline-cleanup"), nil
}

func executePipelineRecordParent(ctx context.Context, request pipelineRecordRequest, configPath string) (map[string]any, string) {
	fail := func() (map[string]any, string) { return nil, "pipeline_failed" }
	root, err := pipelineCleanupRoot()
	if err != nil {
		return fail()
	}
	state, err := openPipelineCleanupState(root)
	if err != nil {
		return fail()
	}
	defer state.Close()
	retained, err := state.Entries()
	if err != nil {
		return fail()
	}
	// Kill reuse-safe retained process groups before contacting any daemon or
	// loading config. A daemon outage must not leave an old child doing work.
	for i := range retained {
		entry := retained[i]
		if reconcilePipelineProcesses(entry) != nil {
			return fail()
		}
	}
	client, err := resolvePipelineDockerClient(ctx)
	if err != nil {
		return fail()
	}
	defer client.Close()
	for i := range retained {
		entry := retained[i]
		if entry.Docker != client.Identity || finishPipelineCleanup(state, client, entry) != nil {
			return fail()
		}
	}
	request.Config, err = config.LoadFromFile(configPath)
	if err != nil {
		return fail()
	}
	closure, err := selectPipelineClosure(request.Config, request.Pipeline, true)
	if err != nil || closure.names[request.ResultStep] != 1 {
		return nil, "result_invalid"
	}
	installed, err := inspectPipelineInstallations(request.PluginDir)
	if err != nil {
		return fail()
	}
	if _, err := installed.resolve(closure.types); err != nil {
		return fail()
	}
	request.Config = closure.config
	parentToken, err := pipelineProcessStartToken(os.Getpid())
	if err != nil {
		return fail()
	}
	request.ParentPID, request.ParentStart = os.Getpid(), parentToken
	data, err := json.Marshal(request)
	if err != nil || len(data) > maxPipelineRecordBytes {
		return nil, "result_invalid"
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fail()
	}
	deadline, _ := ctx.Deadline()
	entry := pipelineCleanupEntry{Version: 1, Label: "wfctl-" + hex.EncodeToString(nonce[:]),
		Parent: pipelineProcessIdentity{PID: os.Getpid(), Start: parentToken}, Docker: client.Identity,
		Deadline: deadline.UTC().Format(time.RFC3339Nano)}
	if err := state.Save(entry); err != nil {
		return fail()
	}
	envelope, childErr := executeIsolatedPipelineChild(ctx, request, data, state, client, &entry)
	if errors.Is(childErr, errPipelineCleanupIncomplete) {
		return fail()
	}
	if err := finishPipelineCleanup(state, client, entry); err != nil {
		return fail()
	}
	if childErr != nil {
		return fail()
	}
	output, code, err := selectPipelineRecordEnvelope(envelope)
	if err != nil {
		return nil, "result_invalid"
	}
	return output, code
}

func finishPipelineCleanup(state *pipelineCleanupState, client *pipelineDockerClient, entry pipelineCleanupEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if entry.Docker != client.Identity {
		return fmt.Errorf("pipeline cleanup daemon identity changed")
	}
	if err := client.Cleanup(ctx, entry); err != nil {
		return err
	}
	for _, name := range entry.CIDFiles {
		path := filepath.Join(state.root, name)
		file, err := openPipelinePrivateFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if err := state.sync(); err != nil {
		return err
	}
	return state.Delete(entry.Label)
}

func executeIsolatedPipelineChild(ctx context.Context, request pipelineRecordRequest, data []byte, state *pipelineCleanupState, client *pipelineDockerClient, entry *pipelineCleanupEntry) (map[string]any, error) {
	var files []*os.File
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	pipe := func() (*os.File, *os.File, error) {
		read, write, err := os.Pipe()
		if err == nil {
			files = append(files, read, write)
		}
		return read, write, err
	}
	inputRead, inputWrite, err := pipe()
	if err != nil {
		return nil, err
	}
	resultRead, resultWrite, err := pipe()
	if err != nil {
		return nil, err
	}
	controlRead, controlWrite, err := pipe()
	if err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := pipe()
	if err != nil {
		return nil, err
	}
	stderrRead, stderrWrite, err := pipe()
	if err != nil {
		return nil, err
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	for _, fd := range sockets {
		unix.CloseOnExec(fd)
	}
	brokerParent := os.NewFile(uintptr(sockets[0]), "parent sandbox broker") // #nosec G115 -- successful Socketpair returns nonnegative descriptors.
	brokerChild := os.NewFile(uintptr(sockets[1]), "child sandbox broker")   // #nosec G115 -- successful Socketpair returns nonnegative descriptors.
	files = append(files, brokerParent, brokerChild)
	brokerConn, err := net.FileConn(brokerParent)
	if err != nil {
		return nil, err
	}
	_ = brokerParent.Close()
	defer brokerConn.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	child := exec.Command(executable, pipelineRecordChildCommand) // #nosec G204 -- own executable; no application data in argv.
	child.Env = pipelineRecordChildEnvironment(os.Environ(), filepath.Join(state.root, entry.Label+".tools"))
	child.ExtraFiles = []*os.File{inputRead, resultWrite, controlRead, brokerChild}
	child.Stdout, child.Stderr = stdoutWrite, stderrWrite
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		return nil, err
	}
	for _, file := range []*os.File{inputRead, resultWrite, controlRead, brokerChild, stdoutWrite, stderrWrite} {
		_ = file.Close()
	}
	childToken, err := pipelineProcessStartToken(child.Process.Pid)
	if err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		return nil, err
	}
	entry.Child = pipelineProcessIdentity{PID: child.Process.Pid, PGID: child.Process.Pid, Start: childToken}
	if err := state.Save(*entry); err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		return nil, err
	}
	violation := make(chan struct{}, 1)
	notifyViolation := func() {
		select {
		case violation <- struct{}{}:
		default:
		}
	}
	stdout := &pipelineRecordCapture{limit: maxPipelineRecordBytes, stdout: true, violation: notifyViolation}
	stderr := &pipelineRecordCapture{limit: maxPipelineRecordBytes, violation: notifyViolation}
	result := &pipelineRecordCapture{limit: maxPipelineRecordBytes + 8, violation: notifyViolation}
	drain := func(reader *os.File, capture *pipelineRecordCapture) <-chan struct{} {
		done := make(chan struct{})
		go func() { _, capture.err = io.Copy(capture, reader); close(done) }()
		return done
	}
	stdoutDone, stderrDone, resultDone := drain(stdoutRead, stdout), drain(stderrRead, stderr), drain(resultRead, result)
	brokerCtx, brokerCancel := context.WithCancel(ctx)
	defer brokerCancel()
	brokerDone := make(chan error, 1)
	go func() {
		brokerDone <- servePipelineBroker(brokerCtx, brokerConn, func(ctx context.Context, request pipelineBrokerRequest) (*sandbox.ExecResult, error) {
			return executePipelineBrokerSandbox(ctx, request, state, client, entry)
		})
	}()
	inputDone := make(chan error, 1)
	go func() {
		_, err := inputWrite.Write(data)
		closeErr := inputWrite.Close()
		inputDone <- errors.Join(err, closeErr)
	}()
	var protocolErr error
	brokerStopped := false
	select {
	case <-resultDone:
	case protocolErr = <-brokerDone:
		brokerStopped = true
		if protocolErr == nil {
			protocolErr = errors.New("sandbox broker closed before result")
		}
	case <-ctx.Done():
		protocolErr = errors.New("pipeline record cancelled")
	case <-violation:
		protocolErr = errors.New("pipeline child output quarantine failed")
	}
	_ = controlWrite.Close()
	brokerCancel()
	_ = brokerConn.Close()
	groupErr := stopPipelineProcessGroup(entry.Child, 5*time.Second)
	// The leader is deliberately not reaped before escalation, so its PID/PGID
	// cannot be recycled while TERM/KILL still targets the recorded group.
	if groupErr != nil {
		_ = child.Process.Kill()
	}
	waitErr := child.Wait()
	if waitErr != nil && protocolErr == nil {
		protocolErr = errors.New("pipeline child failed")
	}
	// Every drain has a bounded termination deadline even if a nonconforming
	// native plugin escaped its group or retained a pipe descriptor.
	for _, done := range []<-chan struct{}{stdoutDone, stderrDone, resultDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = stdoutRead.Close()
			_ = stderrRead.Close()
			_ = resultRead.Close()
			<-done
			protocolErr = errors.New("pipeline child pipe did not close")
		}
	}
	_ = inputWrite.Close()
	if inputErr := <-inputDone; inputErr != nil {
		protocolErr = errors.New("pipeline child input failed")
	}
	if !brokerStopped {
		select {
		case <-brokerDone:
		case <-time.After(31 * time.Second):
			return nil, errPipelineCleanupIncomplete
		}
	}
	if groupErr != nil {
		return nil, errPipelineCleanupIncomplete
	}
	if err := waitPipelineProcessGroupEmpty(entry.Child.PGID, time.Second); err != nil {
		return nil, errPipelineCleanupIncomplete
	}
	if stdout.err != nil || stderr.err != nil || result.err != nil || stdout.data.Len() != 0 || stdout.overflow || stderr.overflow || result.overflow {
		return nil, errors.New("pipeline child quarantine or cleanup failed")
	}
	if protocolErr != nil {
		return nil, protocolErr
	}
	return decodePipelineResultFrame(result.data.Bytes())
}

func waitPipelineProcessGroupEmpty(pgid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Kill(-pgid, 0)
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("pipeline process group is not empty")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func executePipelineBrokerSandbox(ctx context.Context, request pipelineBrokerRequest, state *pipelineCleanupState, client *pipelineDockerClient, entry *pipelineCleanupEntry) (*sandbox.ExecResult, error) {
	name := fmt.Sprintf("%s-%d.cid", entry.Label, len(entry.CIDFiles))
	args, err := client.CreateArgs(request.Config, request.Command, entry.Label, filepath.Join(state.root, name))
	if err != nil {
		return nil, err
	}
	entry.CIDFiles = append(entry.CIDFiles, name)
	if err := state.Save(*entry); err != nil {
		return nil, err
	}
	created, err := client.RunInProcessGroup(ctx, entry.Child, append([]string{"create"}, args...)...)
	if err != nil || created == nil || created.ExitCode != 0 {
		return nil, errors.New("record sandbox create failed")
	}
	id := strings.TrimSpace(created.Stdout)
	if len(id) != 64 || strings.ToLower(id) != id {
		return nil, errors.New("invalid record sandbox ID")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, errors.New("invalid record sandbox ID")
	}
	entry.IDs = append(entry.IDs, id)
	if err := state.Save(*entry); err != nil {
		return nil, err
	}
	result, err := client.RunInProcessGroup(ctx, entry.Child, "start", "-a", id)
	if err != nil || result == nil {
		return nil, errors.New("record sandbox attach failed")
	}
	wait, err := client.RunInProcessGroup(ctx, entry.Child, "wait", id)
	if err != nil || wait == nil || wait.ExitCode != 0 {
		return nil, errors.New("record sandbox wait failed")
	}
	code, err := strconv.Atoi(strings.TrimSpace(wait.Stdout))
	if err != nil || code < 0 || code > 255 {
		return nil, errors.New("invalid record sandbox status")
	}
	result.ExitCode = code
	return result, nil
}

func reconcilePipelineProcesses(entry pipelineCleanupEntry) error {
	if token, err := pipelineProcessStartToken(entry.Parent.PID); err == nil {
		if token != entry.Parent.Start {
			return fmt.Errorf("pipeline cleanup parent PID was reused")
		}
		return fmt.Errorf("recorded pipeline parent is still active")
	} else if !pipelineProcessAbsent(err) {
		return err
	}
	if entry.Child.PID == 0 {
		return nil
	}
	if err := stopPipelineProcessGroup(entry.Child, 5*time.Second); err != nil {
		return err
	}
	return waitPipelineProcessGroupEmpty(entry.Child.PGID, time.Second)
}

func pipelineProcessAbsent(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH)
}

func stopPipelineProcessGroup(process pipelineProcessIdentity, grace time.Duration) error {
	if process.PID <= 0 || process.PGID != process.PID || process.Start == "" {
		return fmt.Errorf("invalid cleanup process group")
	}
	if token, err := pipelineProcessStartToken(process.PID); err == nil {
		if token != process.Start {
			return fmt.Errorf("pipeline cleanup child PID was reused")
		}
	} else if !pipelineProcessAbsent(err) {
		return err
	}
	for _, signal := range []unix.Signal{0, unix.SIGTERM, unix.SIGKILL} {
		deadline := time.Now().Add(grace)
		for {
			active, err := pipelineProcessGroupActive(process.PGID)
			if err != nil {
				return err
			}
			if !active {
				return nil
			}
			token, err := pipelineProcessStartToken(process.PID)
			if err != nil || token != process.Start {
				return fmt.Errorf("cleanup process-group identity cannot be proven")
			}
			if signal != 0 {
				if err := unix.Kill(-process.PGID, signal); err != nil && !errors.Is(err, unix.ESRCH) {
					return err
				}
				signal = 0
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	return fmt.Errorf("pipeline process group survived cleanup")
}
