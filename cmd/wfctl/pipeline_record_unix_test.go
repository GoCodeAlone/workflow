//go:build linux || darwin

package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPipelineCleanupRejectsReusedPIDBeforeEmptyGroup(t *testing.T) {
	identity := pipelineProcessIdentity{PID: os.Getpid(), PGID: os.Getpid(), Start: "wrong-start-token"}
	if err := stopPipelineProcessGroup(identity, 0); err == nil {
		t.Fatal("an empty historical group hid a reused PID")
	}
}

func TestPipelineRecordCaptureImmediatelyReportsQuarantine(t *testing.T) {
	for _, stdout := range []bool{true, false} {
		signalled := 0
		capture := pipelineRecordCapture{limit: 3, stdout: stdout, violation: func() { signalled++ }}
		if count, err := capture.Write([]byte("ok")); count != 2 || err != nil {
			t.Fatalf("capture did not drain: %d, %v", count, err)
		}
		if stdout && signalled == 0 {
			t.Fatal("unexpected stdout did not interrupt the run")
		}
		if !stdout && signalled != 0 {
			t.Fatal("bounded diagnostics were treated as a violation")
		}
		if count, err := capture.Write([]byte("more")); count != 4 || err != nil {
			t.Fatalf("overflow capture stopped draining: %d, %v", count, err)
		}
		if !capture.overflow || capture.data.Len() != 3 || signalled == 0 {
			t.Fatal("overflow did not interrupt the run with bounded retained bytes")
		}
	}
}

func TestPipelineRecordChildHasNoAmbientDockerAuthority(t *testing.T) {
	env := pipelineRecordChildEnvironment([]string{"PATH=/host/tools", "DOCKER_HOST=tcp://daemon-A:2376", "DOCKER_CONTEXT=private", "DOCKER_CERT_PATH=/private/certs", "DOCKER_TLS_VERIFY=1", "DOCKER_CONFIG=/private/docker", "PUBLIC_VALUE=selected"}, "/private/record-tools")
	for _, variable := range env {
		if strings.HasPrefix(variable, "DOCKER_") && variable != "DOCKER_HOST=unix:///wfctl-record-mode-daemon-denied.sock" && variable != "DOCKER_CONFIG=/private/record-tools" {
			t.Fatalf("child inherited Docker authority: %q", variable)
		}
	}
	if !slices.Contains(env, "PATH=/private/record-tools") || !slices.Contains(env, "PUBLIC_VALUE=selected") {
		t.Fatalf("child tool boundary not applied: %#v", env)
	}
}
