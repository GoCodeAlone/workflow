//go:build linux || darwin

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"golang.org/x/sys/unix"
)

//go:embed plugin.json
var manifestJSON []byte

var manifest = sdk.MustEmbedManifest(manifestJSON)

type provider struct {
	once   sync.Once
	late   *lateOutput
	stdout io.Writer
}

func (*provider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{Name: manifest.Name, Version: manifest.Version, Author: manifest.Author, Description: manifest.Description}
}

func (*provider) StepTypes() []string {
	return []string{"step.record_fixture", "step.record_fixture_aux"}
}

func (p *provider) CreateStep(stepType, _ string, _ map[string]any) (sdk.StepInstance, error) {
	if stepType != "step.record_fixture" && stepType != "step.record_fixture_aux" {
		return nil, fmt.Errorf("unknown fixture step type %q", stepType)
	}
	p.once.Do(func() {
		writer := p.stdout
		if writer == nil {
			writer = os.Stdout
		}
		p.late = &lateOutput{writer: writer, release: make(chan struct{}), emitted: make(chan error, 1)}
	})
	return &recordStep{late: p.late}, nil
}

type lateOutput struct {
	writer      io.Writer
	armed       atomic.Bool
	release     chan struct{}
	emitted     chan error
	releaseOnce sync.Once
}

type recordStep struct {
	late     *lateOutput
	attempts atomic.Uint32
}

func (s *recordStep) Execute(ctx context.Context, input map[string]any, prior map[string]map[string]any, _ map[string]any, metadata, config map[string]any) (*sdk.StepResult, error) {
	mode, _ := config["mode"].(string)
	auditPath, _ := config["audit_path"].(string)
	switch mode {
	case "composite", "composite-flaky", "composite-error", "composite-compensate", "composite-while":
		return s.executeComposite(input, prior, metadata, config, mode, auditPath)
	}
	if err := writeAudit(auditPath, mode); err != nil {
		return nil, err
	}
	switch mode {
	case "arm-late-stdout":
		lateMode, _ := config["late_mode"].(string)
		emissionPath, _ := config["emission_path"].(string)
		payload := "SDK_RECORD_V1 {\"forged_late\":true}\nfixture-late-stdout-canary\n"
		if lateMode == "late-stdout-overflow" {
			payload += strings.Repeat("x", (1<<20)+1-len(payload))
		} else if lateMode != "late-stdout" {
			return nil, errors.New("unknown late-output fixture mode")
		}
		if s.late == nil || emissionPath == "" || !s.late.armed.CompareAndSwap(false, true) {
			return nil, errors.New("late emitter must be armed exactly once")
		}
		go func() {
			<-s.late.release
			prefix := payload
			stage := "complete"
			if lateMode == "late-stdout-overflow" {
				// Any stdout byte triggers cancellation. Audit a bounded completed
				// write before the oversized tail can block and be interrupted.
				prefix = payload[:64]
				stage = "prefix"
			}
			count, err := io.WriteString(s.late.writer, prefix)
			if err == nil && count != len(prefix) {
				err = io.ErrShortWrite
			}
			if err == nil {
				err = writeAuditData(emissionPath, lateMode+"-emitted", map[string]string{
					"bytes": strconv.Itoa(count), "requested_bytes": strconv.Itoa(len(payload)), "stage": stage,
				})
			}
			if err == nil && len(prefix) < len(payload) {
				count, err = io.WriteString(s.late.writer, payload[len(prefix):])
				if err == nil && count != len(payload)-len(prefix) {
					err = io.ErrShortWrite
				}
			}
			s.late.emitted <- err
		}()
		return &sdk.StepResult{Output: map[string]any{"late_stdout": lateMode}}, nil
	case "late-stdout", "late-stdout-overflow":
		// A later Execute can receive this StepOutputs value only after the
		// host has consumed the first successful RPC response.
		if s.late == nil || !s.late.armed.Load() || prior["arm"]["late_stdout"] != mode {
			return nil, errors.New("late output requires the prior successful RPC result")
		}
		if err := writeAudit(auditPath+".response", mode+"-response-confirmed"); err != nil {
			return nil, err
		}
		s.late.releaseOnce.Do(func() { close(s.late.release) })
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		select {
		case err := <-s.late.emitted:
			if err != nil {
				return nil, err
			}
		case <-deadline.C:
			return nil, errors.New("late output did not finish after release")
		}
	case "wait":
		<-ctx.Done()
		return nil, ctx.Err()
	case "term-resistant":
		binary, err := os.Executable()
		if err != nil {
			return nil, err
		}
		child := exec.Command(binary, "--record-fixture-term-resistant", auditPath+".descendant")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return nil, err
		}
		go func() { _ = child.Wait() }()
		<-ctx.Done()
		return nil, ctx.Err()
	case "error":
		return nil, errors.New("fixture-private-error-canary")
	case "stopped-error":
		return &sdk.StepResult{StopPipeline: true, Output: map[string]any{"error": "fixture-private-stopped-canary"}}, nil
	case "number":
		return &sdk.StepResult{Output: map[string]any{"unsafe_number": int64(9007199254740993)}}, nil
	case "result-overflow":
		return &sdk.StepResult{Output: map[string]any{"oversized": strings.Repeat("x", 1<<20)}}, nil
	case "stop":
		return &sdk.StepResult{StopPipeline: true, Output: map[string]any{"ready": true}}, nil
	case "invalid-utf8":
		return &sdk.StepResult{Output: map[string]any{"unsafe_string": string([]byte{0xff})}}, nil
	case "stdout":
		fmt.Fprint(os.Stdout, "SDK_RECORD_V1 {\"forged\":true}\nfixture-stdout-canary\n")
	case "stdout-overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", (1<<20)+1))
	case "stderr":
		fmt.Fprint(os.Stderr, "fixture-bounded-stderr-canary\n")
	case "stderr-overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("y", (1<<20)+1))
	case "success", "descriptors":
	default:
		return nil, fmt.Errorf("unknown fixture mode %q", mode)
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	// Go/runtime descriptors may reuse these numbers, but the private host
	// descriptors would survive exec without FD_CLOEXEC if the boundary broke.
	privateClosed := true
	for fd := 3; fd <= 6; fd++ {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err == nil && flags&unix.FD_CLOEXEC == 0 {
			privateClosed = false
			if mode == "descriptors" {
				_, _ = unix.Write(fd, []byte("FIXTURE_PRIVATE_DESCRIPTOR_WRITE"))
			}
		}
	}
	return &sdk.StepResult{Output: map[string]any{
		"source": "real-sdk", "echo": input["marker"], "prior": prior["prepare"]["greeting"], "config": config["greeting"],
		"process_id": strconv.Itoa(os.Getpid()), "private_descriptors_closed": privateClosed,
		"detail": map[string]any{"z": "last", "a": "first"},
	}}, nil
}

func (s *recordStep) executeComposite(input map[string]any, prior map[string]map[string]any, metadata, config map[string]any, mode, auditPath string) (*sdk.StepResult, error) {
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	data := make(map[string]string)
	for _, key := range []string{"participant", "pool", "ordinal", "action"} {
		value, ok := config[key].(string)
		if !ok || value == "" {
			return nil, fmt.Errorf("composite fixture requires resolved string config %q", key)
		}
		data[key] = value
	}
	data["marker"], _ = input["marker"].(string)
	data["prior_greeting"], _ = prior["prepare"]["greeting"].(string)
	data["pipeline"], _ = metadata["pipeline"].(string)
	data["attempt"] = "1"
	if mode == "composite-flaky" {
		data["attempt"] = strconv.FormatUint(uint64(s.attempts.Add(1)), 10)
	}
	if err := writeAuditData(auditPath, mode, data); err != nil {
		return nil, err
	}
	if mode == "composite-error" || (mode == "composite-flaky" && data["attempt"] == "1") {
		return nil, errors.New("fixture-private-composite-error-canary")
	}
	output := map[string]any{
		"participant": data["participant"], "pool": data["pool"], "ordinal": data["ordinal"],
		"action": data["action"], "attempt": data["attempt"], "ready": true, "process_id": strconv.Itoa(os.Getpid()),
		"echo": data["marker"], "prior_greeting": data["prior_greeting"],
	}
	if mode == "composite-while" {
		last, ok := config["last_ordinal"].(string)
		if !ok || last == "" {
			return nil, errors.New("while fixture requires resolved last ordinal")
		}
		output["continue"] = data["ordinal"] != last
	}
	return &sdk.StepResult{Output: output}, nil
}

func validateMetadata(metadata map[string]any) error {
	approved := map[string]bool{"pipeline": true, "started_at": true, "execution_id": true, "tenant_id": true, "request_id": true, "trace_id": true}
	for key := range metadata {
		if !approved[key] {
			return fmt.Errorf("unapproved RPC metadata key %q", key)
		}
	}
	if name, ok := metadata["pipeline"].(string); !ok || name == "" || metadata["started_at"] == nil {
		return errors.New("pipeline metadata did not cross the SDK boundary")
	}
	return nil
}

func writeAudit(path, mode string) error {
	return writeAuditData(path, mode, nil)
}

func writeAuditData(path, mode string, data map[string]string) error {
	if path == "" {
		return errors.New("fixture audit path is required")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	event := map[string]any{"pid": strconv.Itoa(os.Getpid()), "mode": mode, "argv": os.Args}
	if data != nil {
		event["data"] = data
	}
	err = json.NewEncoder(file).Encode(event)
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--record-fixture-term-resistant" {
		signal.Ignore(syscall.SIGTERM)
		if err := writeAudit(os.Args[2], "term-resistant-descendant"); err != nil {
			os.Exit(1)
		}
		for range time.Tick(time.Hour) {
		}
		return
	}
	if path := os.Getenv("WFCTL_RECORD_FIXTURE_STARTUP"); path != "" {
		if err := writeAudit(path, "startup"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	sdk.Serve(&provider{}, sdk.WithManifestProvider(manifest))
}
