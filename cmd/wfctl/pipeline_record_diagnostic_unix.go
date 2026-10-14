//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"syscall"
)

const pipelineRecordDiagnosticEnv = "WFCTL_RECORD_DIAGNOSTICS"
const pipelineRecordDiagnosticPrefix = "WFCTL_RECORD_DIAGNOSTIC_V1 "
const maxPipelineRecordDiagnosticBytes = 1024

// Diagnostic values describe observations, never workload strings or errors.
// Unknown exits are -1; a Docker CLI exit is not a confirmed container exit.
type pipelineRecordDiagnosticReport struct {
	Phase         string `json:"phase"`
	Event         string `json:"event"`
	ChildObserved bool   `json:"child_observed"`
	ChildExit     int    `json:"child_exit"`
	ChildSignal   int    `json:"child_signal"`
	Operation     string `json:"docker_operation"`
	DockerExit    int    `json:"docker_cli_exit"`
	ContainerExit int    `json:"container_exit"`
	Cleanup       string `json:"cleanup"`
	Cancelled     bool   `json:"cancelled"`
	Deadline      bool   `json:"deadline"`
}

var pipelineRecordDiagnosticPhases = map[string]bool{
	"unknown": true, "state_root": true, "state_open": true, "state_entries": true,
	"retained_processes": true, "docker_resolve": true, "retained_cleanup": true,
	"config_load": true, "plugin_inspect": true, "plugin_resolve": true,
	"parent_identity": true, "request_encode": true, "nonce": true, "state_save": true,
	"child_setup": true, "child_start": true, "child_identity": true, "child_journal": true,
	"broker_closed": true, "cancelled": true, "child_quarantine": true, "child_exit": true,
	"child_pipe": true, "child_input": true, "broker_stop": true, "child_cleanup": true,
	"result_frame": true, "child_result": true, "container_args": true,
	"container_create": true, "container_id": true, "container_attach": true,
	"container_wait": true, "container_status": true, "container_command": true,
	"cleanup": true, "pipeline_result": true, "record_write": true,
}

type pipelineRecordDiagnostic struct {
	mu     sync.Mutex
	report pipelineRecordDiagnosticReport
}

type pipelineRecordDiagnosticKey struct{}

func newPipelineRecordDiagnostic() *pipelineRecordDiagnostic {
	return &pipelineRecordDiagnostic{report: pipelineRecordDiagnosticReport{
		Event: "unknown", ChildExit: -1, Operation: "unknown", DockerExit: -1, ContainerExit: -1, Cleanup: "not_started",
	}}
}

func pipelineRecordDiagnosticFromContext(ctx context.Context) *pipelineRecordDiagnostic {
	diagnostic, _ := ctx.Value(pipelineRecordDiagnosticKey{}).(*pipelineRecordDiagnostic)
	return diagnostic
}

func (d *pipelineRecordDiagnostic) fail(phase string) {
	if d == nil {
		return
	}
	if !pipelineRecordDiagnosticPhases[phase] {
		phase = "unknown"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.report.Phase == "" {
		d.report.Phase = phase
	}
}

func (d *pipelineRecordDiagnostic) event(event string) {
	if d == nil {
		return
	}
	switch event {
	case "result", "broker", "context", "quarantine":
	default:
		event = "unknown"
	}
	d.mu.Lock()
	d.report.Event = event
	d.mu.Unlock()
}

func (d *pipelineRecordDiagnostic) child(state *os.ProcessState) {
	if d == nil || state == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.report.ChildObserved = true
	d.report.ChildExit = pipelineRecordDiagnosticExit(state.ExitCode())
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		signal := int(status.Signal())
		if signal > 0 && signal <= 64 {
			d.report.ChildSignal = signal
		}
	}
}

func pipelineRecordDiagnosticExit(code int) int {
	if code < 0 || code > 255 {
		return -1
	}
	return code
}

func (d *pipelineRecordDiagnostic) beginOperation(operation string) {
	if d == nil {
		return
	}
	switch operation {
	case "create", "start", "wait":
	default:
		operation = "unknown"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.report.Phase == "" {
		d.report.Operation = operation
		d.report.DockerExit, d.report.ContainerExit = -1, -1
	}
}

func (d *pipelineRecordDiagnostic) dockerExit(code int) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.report.Phase == "" {
		d.report.DockerExit = pipelineRecordDiagnosticExit(code)
	}
	d.mu.Unlock()
}

func (d *pipelineRecordDiagnostic) containerExit(code int) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.report.Phase == "" {
		d.report.ContainerExit = pipelineRecordDiagnosticExit(code)
	}
	d.mu.Unlock()
}

func (d *pipelineRecordDiagnostic) cleanup(outcome string) {
	if d == nil {
		return
	}
	switch outcome {
	case "complete", "failed", "incomplete", "not_started":
	default:
		outcome = "unknown"
	}
	d.mu.Lock()
	d.report.Cleanup = outcome
	d.mu.Unlock()
}

func (d *pipelineRecordDiagnostic) result(frame []byte) {
	if d == nil {
		return
	}
	// The caller has observed resultDone. This only classifies existing bounded
	// bytes before teardown; the original decoder still makes every decision.
	envelope, err := decodePipelineResultFrame(frame)
	if err != nil {
		d.fail("result_frame")
		return
	}
	_, code, err := selectPipelineRecordEnvelope(envelope)
	if err != nil {
		d.fail("result_frame")
	} else if code != "" {
		d.fail("child_result")
	}
}

func (d *pipelineRecordDiagnostic) emit(ctx context.Context, writer io.Writer) {
	if d == nil {
		return
	}
	d.mu.Lock()
	report := d.report
	d.mu.Unlock()
	if report.Phase == "" {
		report.Phase = "unknown"
	}
	report.Cancelled = ctx.Err() == context.Canceled
	report.Deadline = ctx.Err() == context.DeadlineExceeded
	data, err := json.Marshal(report)
	if err != nil || len(data)+len(pipelineRecordDiagnosticPrefix)+1 > maxPipelineRecordDiagnosticBytes {
		return
	}
	// Diagnostic delivery cannot change the original execution result.
	_, _ = io.WriteString(writer, pipelineRecordDiagnosticPrefix+string(data)+"\n")
}
