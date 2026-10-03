package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/GoCodeAlone/workflow/module"
)

const maxPipelineRecordBytes = 1 << 20

const pipelineRecordChildCommand = "__wfctl-pipeline-record-child-v1"

var errPipelineRecordInvalid = errors.New("invalid pipeline record")

type pipelineRecordOptions struct {
	ConfigPath    string
	SuccessPrefix string
	ErrorPrefix   string
}

func selectPipelineRecordEnvelope(envelope map[string]any) (map[string]any, string, error) {
	status, _ := envelope["status"].(string)
	if len(envelope) != 2 {
		return nil, "", errPipelineRecordInvalid
	}
	switch status {
	case "ok":
		output, ok := envelope["output"].(map[string]any)
		if !ok {
			return nil, "", errPipelineRecordInvalid
		}
		if _, err := workflowCanonicalJSONV1(output); err != nil {
			return nil, "", err
		}
		return output, "", nil
	case "error":
		code, _ := envelope["code"].(string)
		if code != "pipeline_failed" && code != "result_missing" && code != "result_invalid" {
			return nil, "", errPipelineRecordInvalid
		}
		return nil, code, nil
	default:
		return nil, "", errPipelineRecordInvalid
	}
}

func pipelineRecordErrorPayload(code string) map[string]any {
	message := "Pipeline execution failed."
	switch code {
	case "result_missing":
		message = "The selected result is missing."
	case "result_invalid":
		message = "The pipeline result is invalid."
	default:
		code = "pipeline_failed"
	}
	return map[string]any{"code": code, "message": message, "retryable": false}
}

func pipelineRecordEarlyDispatch(args []string) (bool, int) {
	if len(args) == 1 && args[0] == pipelineRecordChildCommand {
		return true, runPipelineRecordChild()
	}
	if len(args) < 2 || args[0] != "pipeline" || args[1] != "run" {
		return false, 0
	}
	for index, arg := range args[2:] {
		if arg == "--output=record" || arg == "-output=record" ||
			((arg == "--output" || arg == "-output") && index+3 < len(args) && args[index+3] == "record") {
			if err := runPipelineRun(args[2:]); err != nil {
				return true, 1
			}
			return true, 0
		}
	}
	return false, 0
}

func workflowCanonicalJSONV1(value any) ([]byte, error) {
	var buf bytes.Buffer
	write := func(data []byte) error {
		if len(data) > maxPipelineRecordBytes-buf.Len() {
			return errPipelineRecordInvalid
		}
		_, _ = buf.Write(data)
		return nil
	}
	quoted := func(s string) error {
		if !utf8.ValidString(s) || len(s) > maxPipelineRecordBytes {
			return errPipelineRecordInvalid
		}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(s); err != nil {
			return errPipelineRecordInvalid
		}
		return write(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
	}
	var encode func(any, int) error
	encode = func(value any, depth int) error {
		if depth > 64 {
			return errPipelineRecordInvalid
		}
		switch value := value.(type) {
		case nil:
			return write([]byte("null"))
		case bool:
			if value {
				return write([]byte("true"))
			}
			return write([]byte("false"))
		case string:
			return quoted(value)
		case []any:
			if err := write([]byte{'['}); err != nil {
				return err
			}
			for i, item := range value {
				if i != 0 {
					if err := write([]byte{','}); err != nil {
						return err
					}
				}
				if err := encode(item, depth+1); err != nil {
					return err
				}
			}
			return write([]byte{']'})
		case map[string]any:
			if err := write([]byte{'{'}); err != nil {
				return err
			}
			for i, key := range slices.Sorted(maps.Keys(value)) {
				if i != 0 {
					if err := write([]byte{','}); err != nil {
						return err
					}
				}
				if err := quoted(key); err != nil {
					return err
				}
				if err := write([]byte{':'}); err != nil {
					return err
				}
				if err := encode(value[key], depth+1); err != nil {
					return err
				}
			}
			return write([]byte{'}'})
		default:
			return errPipelineRecordInvalid
		}
	}
	if err := encode(value, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodePipelineResultFrame(value map[string]any) ([]byte, error) {
	payload, err := workflowCanonicalJSONV1(value)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 8, 8+len(payload))
	copy(frame, "WFR1")
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload))) // #nosec G115 -- canonical encoder bounds payload to 1 MiB.
	return append(frame, payload...), nil
}

func decodePipelineResultFrame(frame []byte) (map[string]any, error) {
	if len(frame) < 8 || string(frame[:4]) != "WFR1" {
		return nil, errPipelineRecordInvalid
	}
	length := binary.BigEndian.Uint32(frame[4:8])
	if length == 0 || length > maxPipelineRecordBytes || int(length) != len(frame)-8 {
		return nil, errPipelineRecordInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(frame[8:]))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errPipelineRecordInvalid
	}
	canonical, err := workflowCanonicalJSONV1(value)
	if err != nil || !bytes.Equal(canonical, frame[8:]) {
		return nil, errPipelineRecordInvalid
	}
	return value, nil
}

func validatePipelineRecordSelectors(result, success, failure string) error {
	validPrefix := func(s string) bool {
		if len(s) == 0 || len(s) > 64 || s[0] < 'A' || s[0] > 'Z' {
			return false
		}
		for _, c := range s {
			if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
		return true
	}
	if !validPrefix(success) || !validPrefix(failure) || success == failure {
		return fmt.Errorf("record prefixes must be distinct ASCII [A-Z][A-Z0-9_]{0,63}")
	}
	if result == "" || !utf8.ValidString(result) || strings.ContainsAny(result, "{}") || strings.ContainsFunc(result, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return fmt.Errorf("record result-step must be one literal reachable step name")
	}
	return nil
}

func writePipelineRecord(w io.Writer, prefix string, value map[string]any) error {
	payload, err := workflowCanonicalJSONV1(value)
	if err != nil {
		return err
	}
	line := append([]byte(prefix+" "), payload...)
	line = append(line, '\n')
	n, err := w.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	return nil
}

func stoppedPipelineError(result *module.StepResult) error {
	if result == nil || !result.Stop {
		return nil
	}
	for _, key := range []string{"error", "_error"} {
		if value, exists := result.Output[key]; exists && value != nil && value != "" {
			return errors.New("pipeline step reported a stopped error")
		}
	}
	return nil
}
