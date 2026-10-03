//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
	"golang.org/x/sys/unix"
)

func pipelineRecordChildEnvironment(source []string, toolDirectory string) []string {
	env := make([]string, 0, len(source)+3)
	for _, variable := range source {
		name, _, _ := strings.Cut(variable, "=")
		if strings.HasPrefix(name, "DOCKER_") || name == "PATH" {
			continue
		}
		env = append(env, variable)
	}
	return append(env, "PATH="+toolDirectory, "DOCKER_CONFIG="+toolDirectory,
		"DOCKER_HOST=unix:///wfctl-record-mode-daemon-denied.sock")
}

func runPipelineRecordChild() int {
	if syscall.Getpgrp() != os.Getpid() {
		return 1
	}
	// Parent teardown closes captured stdout/stderr. Forwarding must return
	// EPIPE instead of terminating the leader before its custody defer runs.
	signal.Ignore(syscall.SIGPIPE)
	// Do this before config, engine, plugin, or sandbox construction. A native
	// plugin's exec must never inherit either private pipe or broker endpoint.
	for _, fd := range []int{3, 4, 5, 6} {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil { // #nosec G115 -- fd is one of the fixed descriptors 3..6.
			return 1
		}
		unix.CloseOnExec(fd)
	}
	input := os.NewFile(3, "pipeline private input")
	result := os.NewFile(4, "pipeline private result")
	control := os.NewFile(5, "pipeline private cancellation")
	brokerFile := os.NewFile(6, "pipeline private sandbox broker")
	defer input.Close()
	defer result.Close()
	defer control.Close()
	brokerConn, err := net.FileConn(brokerFile)
	_ = brokerFile.Close()
	if err != nil {
		return 1
	}
	defer brokerConn.Close()
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, control); cancel() }()
	broker := newPipelineBrokerClient(brokerConn)
	ctx = sandbox.WithLocalRunnerFactory(ctx, broker.Runner)
	data, err := io.ReadAll(io.LimitReader(input, maxPipelineRecordBytes+1))
	_ = input.Close()
	if err != nil || len(data) > maxPipelineRecordBytes || validatePipelinePrivateJSON(data) != nil {
		return 1
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request pipelineRecordRequest
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		return 1
	}
	if request.ParentPID <= 0 || request.ParentStart == "" {
		return 1
	}
	defer retainPipelineRecordChildCustody()
	if request.ParentPID != os.Getppid() {
		return 1
	}
	parentStart, err := pipelineProcessStartToken(request.ParentPID)
	if err != nil || parentStart != request.ParentStart {
		return 1
	}
	if ctx.Err() != nil {
		return 1
	}
	envelope := executePipelineRecord(ctx, request)
	frame, err := encodePipelineResultFrame(envelope)
	if err != nil {
		frame, err = encodePipelineResultFrame(map[string]any{"status": "error", "code": "result_invalid"})
	}
	if err != nil {
		return 1
	}
	if count, err := result.Write(frame); err != nil || count != len(frame) {
		return 1
	}
	if err := result.Close(); err != nil {
		return 1
	}
	// Keep the group leader alive until the parent has drained the result and
	// requested shutdown; no child may race a late Docker request past cleanup.
	<-ctx.Done()
	return 0
}

func retainPipelineRecordChildCustody() {
	// Parent teardown can race any liveness lookup or pipe return. Keep our
	// leader pinned until other actors are gone; census errors are not empty.
	pid := os.Getpid()
	for _, signal := range []unix.Signal{0, unix.SIGTERM} {
		deadline := time.Now().Add(5 * time.Second)
		for syscall.Getpgrp() == pid {
			if active, err := pipelineProcessGroupActiveExcept(pid, pid); err == nil && !active {
				return
			}
			if signal != 0 {
				_ = unix.Kill(-pid, signal)
				signal = 0
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	for syscall.Getpgrp() == pid {
		if active, err := pipelineProcessGroupActiveExcept(pid, pid); err == nil && !active {
			return
		}
		_ = unix.Kill(-pid, unix.SIGKILL)
		time.Sleep(25 * time.Millisecond)
	}
}
