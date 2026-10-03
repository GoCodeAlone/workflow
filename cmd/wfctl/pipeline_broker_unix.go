//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GoCodeAlone/workflow/sandbox"
)

// JSON escaping can expand each output byte to six bytes (for example, \u0000).
const maxPipelineBrokerResponseBytes = 6*(2*sandbox.MaxOutputBytes) + 16384

type pipelineBrokerRequest struct {
	Operation string                `json:"operation"`
	Config    sandbox.SandboxConfig `json:"config"`
	Command   []string              `json:"command"`
	Deadline  string                `json:"deadline"`
}

type pipelineBrokerResponse struct {
	Result *sandbox.ExecResult `json:"result,omitempty"`
	Error  string              `json:"error,omitempty"`
}

func writePipelineBrokerFrame(writer io.Writer, value any, limit int) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || len(data) > limit {
		return errPipelineRecordInvalid
	}
	frame := []byte("WFD2\x00\x00\x00\x00")
	binary.BigEndian.PutUint32(frame[4:], uint32(len(data))) // #nosec G115 -- callers bound payloads to at most 12 MiB + 16 KiB.
	frame = append(frame, data...)
	n, err := writer.Write(frame)
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	return err
}

func readPipelineBrokerFrame(reader io.Reader, value any, limit int) error {
	var header [8]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(header[4:])
	if string(header[:4]) != "WFD2" || length == 0 || uint64(length) > uint64(limit) { // #nosec G115 -- callers use positive, bounded protocol limits.
		return errPipelineRecordInvalid
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	if err := validatePipelinePrivateJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errPipelineRecordInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errPipelineRecordInvalid
	}
	return nil
}

// Private requests allow typed numeric resource limits, but never duplicate
// object keys, invalid UTF-8, excessive nesting, or trailing JSON values.
func validatePipelinePrivateJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errPipelineRecordInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errPipelineRecordInvalid
		}
		token, err := decoder.Token()
		if err != nil {
			return errPipelineRecordInvalid
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return errPipelineRecordInvalid
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			if close, err := decoder.Token(); err != nil || close != json.Delim('}') {
				return errPipelineRecordInvalid
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			if close, err := decoder.Token(); err != nil || close != json.Delim(']') {
				return errPipelineRecordInvalid
			}
		default:
			return errPipelineRecordInvalid
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errPipelineRecordInvalid
	}
	return nil
}

type pipelineBrokerClient struct {
	conn   net.Conn
	mu     sync.Mutex
	failed bool
}

func newPipelineBrokerClient(conn net.Conn) *pipelineBrokerClient {
	return &pipelineBrokerClient{conn: conn}
}

func (c *pipelineBrokerClient) Runner(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
	return &pipelineBrokerRunner{client: c, config: cfg}, nil
}

type pipelineBrokerRunner struct {
	client *pipelineBrokerClient
	config sandbox.SandboxConfig
}

func (r *pipelineBrokerRunner) Close() error { return nil }

func (r *pipelineBrokerRunner) Exec(ctx context.Context, command []string) (*sandbox.ExecResult, error) {
	client := r.client
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.failed || ctx.Err() != nil {
		return nil, errors.New("record sandbox broker unavailable")
	}
	timeout := r.config.Timeout
	if timeout <= 0 || timeout > 10*time.Minute {
		return nil, errors.New("invalid record sandbox timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := client.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = client.conn.SetDeadline(time.Now()) })
	defer stop()
	request := pipelineBrokerRequest{Operation: "exec", Config: r.config, Command: command, Deadline: deadline.UTC().Format(time.RFC3339Nano)}
	if err := writePipelineBrokerFrame(client.conn, request, maxPipelineRecordBytes); err != nil {
		client.failed = true
		return nil, errors.New("record sandbox broker request failed")
	}
	var response pipelineBrokerResponse
	if err := readPipelineBrokerFrame(client.conn, &response, maxPipelineBrokerResponseBytes); err != nil {
		client.failed = true
		return nil, errors.New("record sandbox broker response failed")
	}
	if response.Error != "" || !validPipelineBrokerResult(response.Result) {
		return nil, errors.New("record sandbox execution failed")
	}
	return response.Result, nil
}

func validPipelineBrokerResult(result *sandbox.ExecResult) bool {
	return result != nil && len(result.Stdout) <= sandbox.MaxOutputBytes && len(result.Stderr) <= sandbox.MaxOutputBytes &&
		utf8.ValidString(result.Stdout) && utf8.ValidString(result.Stderr)
}

func servePipelineBroker(ctx context.Context, conn net.Conn, execute func(context.Context, pipelineBrokerRequest) (*sandbox.ExecResult, error)) error {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		var request pipelineBrokerRequest
		if err := readPipelineBrokerFrame(conn, &request, maxPipelineRecordBytes); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		deadline, err := time.Parse(time.RFC3339Nano, request.Deadline)
		if err != nil || request.Operation != "exec" || len(request.Command) == 0 || !deadline.After(time.Now()) || time.Until(deadline) > 10*time.Minute {
			return fmt.Errorf("invalid record sandbox request")
		}
		requestCtx, cancel := context.WithDeadline(ctx, deadline)
		result, err := execute(requestCtx, request)
		cancel()
		response := pipelineBrokerResponse{Result: result}
		if err != nil || !validPipelineBrokerResult(result) {
			response = pipelineBrokerResponse{Error: "sandbox_failed"}
		}
		if err := writePipelineBrokerFrame(conn, response, maxPipelineBrokerResponseBytes); err != nil {
			return err
		}
	}
}
