//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
)

func TestPipelineBrokerFrameBoundsAndTypes(t *testing.T) {
	for name, data := range map[string][]byte{
		"bad magic": []byte("NOPE\x00\x00\x00\x02{}"),
		"oversize":  []byte("WFD2\xff\xff\xff\xff"),
		"truncated": []byte("WFD2\x00\x00\x00\x02{"),
		"unknown":   brokerTestFrame(`{"operation":"exec","foreign":true}`),
		"duplicate": brokerTestFrame(`{"operation":"exec","operation":"cancel"}`),
		"trailing":  brokerTestFrame(`{"operation":"exec"} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			var request pipelineBrokerRequest
			if err := readPipelineBrokerFrame(bytes.NewReader(data), &request, maxPipelineRecordBytes); err == nil {
				t.Fatal("accepted ambiguous or unbounded sandbox request")
			}
		})
	}
}

func TestPipelineBrokerRunnerCarriesResolvedConfig(t *testing.T) {
	child, parent := net.Pipe()
	defer child.Close()
	defer parent.Close()
	requestDone := make(chan pipelineBrokerRequest, 1)
	go func() {
		var request pipelineBrokerRequest
		if readPipelineBrokerFrame(parent, &request, maxPipelineRecordBytes) == nil {
			requestDone <- request
			_ = writePipelineBrokerFrame(parent, pipelineBrokerResponse{Result: &sandbox.ExecResult{ExitCode: 0, Stdout: "resolved"}}, maxPipelineBrokerResponseBytes)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	broker := newPipelineBrokerClient(child)
	cfg := sandbox.DefaultSecureSandboxConfig("alpine:3.21")
	cfg.Env = map[string]string{"PUBLIC_VALUE": "selected"}
	runner, err := broker.Runner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Exec(ctx, []string{"sh", "-euc", "printf %s selected"})
	if err != nil || result == nil || result.Stdout != "resolved" {
		t.Fatalf("broker execution failed: %#v, %v", result, err)
	}
	select {
	case request := <-requestDone:
		if request.Operation != "exec" || request.Config.Env["PUBLIC_VALUE"] != "selected" || strings.Join(request.Command, " ") != "sh -euc printf %s selected" || request.Deadline == "" {
			t.Fatalf("broker did not carry the execution boundary: %#v", request)
		}
	case <-ctx.Done():
		t.Fatal("broker did not deliver its request")
	}
}

func TestPipelineBrokerEscapedOutputWithinRawCaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdout string
		stderr string
	}{
		{name: "html escaping", stdout: strings.Repeat("<", 400000)},
		{name: "control characters at both caps", stdout: strings.Repeat("\x00", sandbox.MaxOutputBytes), stderr: strings.Repeat("\x1f", sandbox.MaxOutputBytes)},
		{name: "plain text at both caps", stdout: strings.Repeat("x", sandbox.MaxOutputBytes), stderr: strings.Repeat("y", sandbox.MaxOutputBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := &sandbox.ExecResult{ExitCode: 0, Stdout: tc.stdout, Stderr: tc.stderr}
			got, err, peerErr := brokerTestExec(t, func(ctx context.Context, conn net.Conn) error {
				return servePipelineBroker(ctx, conn, func(context.Context, pipelineBrokerRequest) (*sandbox.ExecResult, error) {
					return want, nil
				})
			})
			if err != nil || peerErr != nil || got == nil {
				t.Fatalf("valid bounded escaped output rejected: stdout=%d stderr=%d client=%v peer=%v", len(tc.stdout), len(tc.stderr), err, peerErr)
			}
			if *got != *want {
				t.Fatalf("broker changed bounded output: stdout=%d stderr=%d", len(got.Stdout), len(got.Stderr))
			}
		})
	}
}

func TestPipelineBrokerRejectsInvalidRawOutputBeforeEncoding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result sandbox.ExecResult
	}{
		{name: "stdout invalid UTF-8", result: sandbox.ExecResult{Stdout: "private-stdout\xff"}},
		{name: "stderr invalid UTF-8", result: sandbox.ExecResult{Stderr: "private-stderr\xc3"}},
		{name: "stdout over cap", result: sandbox.ExecResult{Stdout: strings.Repeat("x", sandbox.MaxOutputBytes+1)}},
		{name: "stderr over cap", result: sandbox.ExecResult{Stderr: strings.Repeat("y", sandbox.MaxOutputBytes+1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err, peerErr := brokerTestRawResponse(t, &tc.result)
			if err != nil || peerErr != nil {
				t.Fatalf("invalid raw output did not return a bounded error frame: client=%v peer=%v", err, peerErr)
			}
			if response.Result != nil || response.Error != "sandbox_failed" {
				t.Fatal("broker accepted invalid raw output, including JSON replacement of invalid UTF-8")
			}
		})
	}
}

func TestPipelineBrokerRejectsDecodedOutputOverRawCaps(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			result := &sandbox.ExecResult{}
			if stream == "stdout" {
				result.Stdout = strings.Repeat("x", sandbox.MaxOutputBytes+1)
			} else {
				result.Stderr = strings.Repeat("y", sandbox.MaxOutputBytes+1)
			}
			got, err, peerErr := brokerTestExec(t, func(_ context.Context, conn net.Conn) error {
				var request pipelineBrokerRequest
				if err := readPipelineBrokerFrame(conn, &request, maxPipelineRecordBytes); err != nil {
					return err
				}
				return writePipelineBrokerFrame(conn, pipelineBrokerResponse{Result: result}, maxPipelineBrokerResponseBytes)
			})
			if peerErr != nil {
				t.Fatalf("oversized response did not reach the decoded raw-size guard: %v", peerErr)
			}
			if err == nil || got != nil {
				t.Fatal("broker accepted decoded output above the raw cap")
			}
		})
	}
}

func brokerTestExec(t *testing.T, peer func(context.Context, net.Conn) error) (*sandbox.ExecResult, error, error) {
	t.Helper()
	var result *sandbox.ExecResult
	err, peerErr := brokerTestChannel(t, peer, func(ctx context.Context, conn net.Conn) error {
		cfg := sandbox.DefaultSecureSandboxConfig("alpine:3.21")
		cfg.Timeout = 5 * time.Second
		runner, err := newPipelineBrokerClient(conn).Runner(cfg)
		if err != nil {
			return err
		}
		result, err = runner.Exec(ctx, []string{"printf", "fixture"})
		return err
	})
	return result, err, peerErr
}

func brokerTestRawResponse(t *testing.T, result *sandbox.ExecResult) (pipelineBrokerResponse, error, error) {
	t.Helper()
	var response pipelineBrokerResponse
	err, peerErr := brokerTestChannel(t, func(ctx context.Context, conn net.Conn) error {
		return servePipelineBroker(ctx, conn, func(context.Context, pipelineBrokerRequest) (*sandbox.ExecResult, error) {
			return result, nil
		})
	}, func(ctx context.Context, conn net.Conn) error {
		deadline, _ := ctx.Deadline()
		request := pipelineBrokerRequest{Operation: "exec", Command: []string{"printf", "fixture"}, Deadline: deadline.UTC().Format(time.RFC3339Nano)}
		if err := writePipelineBrokerFrame(conn, request, maxPipelineRecordBytes); err != nil {
			return err
		}
		return readPipelineBrokerFrame(conn, &response, maxPipelineBrokerResponseBytes)
	})
	return response, err, peerErr
}

func brokerTestChannel(t *testing.T, peer, exchange func(context.Context, net.Conn) error) (error, error) {
	t.Helper()
	child, parent := net.Pipe()
	defer child.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	peerDone := make(chan error, 1)
	go func() {
		defer parent.Close()
		err := peer(ctx, parent)
		if ctx.Err() != nil && (errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)) {
			err = nil
		}
		peerDone <- err
	}()
	_ = child.SetDeadline(time.Now().Add(5 * time.Second))
	err := exchange(ctx, child)
	_ = child.Close()
	cancel()
	select {
	case peerErr := <-peerDone:
		return err, peerErr
	case <-time.After(time.Second):
		t.Fatal("broker test peer did not stop after cancellation and connection close")
		return nil, nil
	}
}

func brokerTestFrame(payload string) []byte {
	header := []byte("WFD2\x00\x00\x00\x00")
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}
