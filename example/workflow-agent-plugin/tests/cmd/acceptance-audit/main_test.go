package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

func TestCLIReportsViolationsWithoutPayloads(t *testing.T) {
	raw, err := os.ReadFile("../../internal/acceptance/testdata/policy-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := auditInput(bytes.NewReader(raw), &output); code != 1 {
		t.Fatalf("exit %d", code)
	}
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["attempted_calls"] != float64(75) || report["violating_calls"] != float64(22) || report["clean_acceptance"] != false || strings.Contains(output.String(), "yaml_content") {
		t.Fatalf("unexpected bounded summary: %s", output.String())
	}
}

type unboundedSpaces struct{ read int }

func (r *unboundedSpaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	r.read += len(p)
	return len(p), nil
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic output failure") }

func TestCLIBoundsAndIOFailures(t *testing.T) {
	reader := &unboundedSpaces{}
	var output bytes.Buffer
	if auditInput(reader, &output) != 2 || reader.read != maxEvidenceBytes+1 || output.Len() != 0 {
		t.Fatal("overflow was not bounded")
	}
	if auditInput(iotest.ErrReader(errors.New("synthetic reader failure")), &output) != 2 || output.Len() != 0 {
		t.Fatal("reader failure accepted or echoed")
	}
	if auditInput(strings.NewReader(`{"records":[]}`), failingWriter{}) != 2 {
		t.Fatal("writer failure accepted")
	}
	var _ io.Writer = failingWriter{}
}
func TestCLIInputFailureIsBounded(t *testing.T) {
	var output bytes.Buffer
	if run(nil, &output) != 2 || run([]string{"missing-fixture"}, &output) != 2 || auditInput(strings.NewReader("not json"), &output) != 2 || output.Len() != 0 {
		t.Fatal("input error was accepted or echoed")
	}
	if auditInput(strings.NewReader(`{"records":[]}`), &output) != 0 || !strings.Contains(output.String(), `"clean_acceptance": false`) {
		t.Fatal("empty lint must remain non-acceptance")
	}
}
