package wftest_test

import (
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

func TestRunYAMLTests_SimpleFixture(t *testing.T) {
	wftest.RunYAMLTests(t, "testdata/simple_test.yaml")
}

func TestRunAllYAMLTests_Testdata(t *testing.T) {
	wftest.RunAllYAMLTests(t, "testdata")
}

func TestHarness_StopAfter_HaltsExecution(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  multi-step:
    steps:
      - name: step1
        type: step.set
        config:
          values: { a: "1" }
      - name: step2
        type: step.set
        config:
          values: { b: "2" }
      - name: step3
        type: step.set
        config:
          values: { c: "3" }
`))

	result := h.ExecutePipelineOpts("multi-step", nil, wftest.StopAfter("step2"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("step1") {
		t.Error("step1 should have executed")
	}
	if !result.StepExecuted("step2") {
		t.Error("step2 should have executed")
	}
	if result.StepExecuted("step3") {
		t.Error("step3 should NOT have executed (stop_after step2)")
	}
}

func TestHarness_StopAfter_FirstStep(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  three-steps:
    steps:
      - name: a
        type: step.set
        config:
          values: { x: "1" }
      - name: b
        type: step.set
        config:
          values: { y: "2" }
      - name: c
        type: step.set
        config:
          values: { z: "3" }
`))

	result := h.ExecutePipelineOpts("three-steps", nil, wftest.StopAfter("a"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("a") {
		t.Error("step a should have executed")
	}
	if result.StepExecuted("b") {
		t.Error("step b should NOT have executed")
	}
	if result.StepExecuted("c") {
		t.Error("step c should NOT have executed")
	}
}

func TestHarness_StopAfter_LastStep_IsNoop(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  two-steps:
    steps:
      - name: first
        type: step.set
        config:
          values: { a: "1" }
      - name: last
        type: step.set
        config:
          values: { b: "2" }
`))

	// Stopping after the last step should run all steps normally.
	result := h.ExecutePipelineOpts("two-steps", nil, wftest.StopAfter("last"))
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.StepExecuted("first") {
		t.Error("step first should have executed")
	}
	if !result.StepExecuted("last") {
		t.Error("step last should have executed")
	}
}

func TestHarness_StopAfter_UnknownStep_ReturnsError(t *testing.T) {
	h := wftest.New(t, wftest.WithYAML(`
pipelines:
  simple:
    steps:
      - name: s
        type: step.set
        config:
          values: { x: 1 }
`))

	result := h.ExecutePipelineOpts("simple", nil, wftest.StopAfter("nonexistent"))
	if result.Error == nil {
		t.Error("expected error for unknown stop_after step")
	}
}

func TestRunYAMLTests_StopAfterInYAML(t *testing.T) {
	// Inline test file via a fixture written in the test.
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/stop_test.yaml", `
yaml: |
  pipelines:
    pipeline1:
      steps:
        - name: alpha
          type: step.set
          config:
            values: { x: "1" }
        - name: beta
          type: step.set
          config:
            values: { y: "2" }
        - name: gamma
          type: step.set
          config:
            values: { z: "3" }
tests:
  partial-run:
    trigger:
      type: pipeline
      name: pipeline1
    stop_after: beta
    assertions:
      - step: alpha
        executed: true
      - step: beta
        executed: true
      - step: gamma
        executed: false
`)
	wftest.RunYAMLTests(t, tmpDir+"/stop_test.yaml")
}

func TestYAMLRunner_SequenceWithState(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/seq_test.yaml", `
yaml: |
  pipelines:
    setup-game:
      steps:
        - name: init
          type: step.set
          config:
            values:
              status: ready
    take-turn:
      steps:
        - name: move
          type: step.set
          config:
            values:
              action: draw
tests:
  multi-step-game:
    description: "sequence test with seeded state preserved across steps"
    state:
      seed:
        sessions:
          game-1:
            turn: p1
    sequence:
      - name: setup
        trigger:
          type: pipeline
          name: setup-game
        assertions:
          - step: init
            executed: true
          - state:
              sessions:
                game-1:
                  turn: p1
      - name: turn
        pipeline: take-turn
        assertions:
          - step: move
            executed: true
          - state:
              sessions:
                game-1:
                  turn: p1
`)
	wftest.RunYAMLTests(t, tmpDir+"/seq_test.yaml")
}

func TestYAMLRunner_StatefulTestData(t *testing.T) {
	wftest.RunYAMLTests(t, "testdata/stateful_test.yaml")
}

func TestYAMLRunner_HTTPPutTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/put_test.yaml", `
    update-resource:
      trigger:
        type: http
        config:
          path: /v1/resource/{id}
          method: PUT
              updated: true
tests:
  update-resource:
    trigger:
      type: http.put
      path: /v1/resource/123
      data:
        name: updated
    assertions:
      - response:
          status: 200
          body: '"updated":true'
  update-resource-short:
    trigger:
      type: put
      path: /v1/resource/123
    assertions:
      - response:
          status: 200
`)
	wftest.RunYAMLTests(t, tmpDir+"/put_test.yaml")
}

func TestYAMLRunner_HTTPPatchTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/patch_test.yaml", `
yaml: |
  modules:
    - name: router
      type: http.router
  pipelines:
    patch-resource:
      trigger:
        type: http
        config:
          path: /v1/resource/{id}
          method: PATCH
      steps:
        - name: respond
          type: step.json_response
          config:
            status: 200
            body:
              patched: true
tests:
  patch-resource:
    trigger:
      type: http.patch
      path: /v1/resource/123
      data:
        field: value
    assertions:
      - response:
          status: 200
          body: '"patched":true'
  patch-resource-short:
    trigger:
      type: patch
      path: /v1/resource/123
    assertions:
      - response:
          status: 200
`)
	wftest.RunYAMLTests(t, tmpDir+"/patch_test.yaml")
}

func TestYAMLRunner_HTTPDeleteTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/delete_test.yaml", `
yaml: |
  modules:
    - name: router
      type: http.router
  pipelines:
    delete-resource:
      trigger:
        type: http
        config:
          path: /v1/resource/{id}
          method: DELETE
      steps:
        - name: respond
          type: step.json_response
          config:
            status: 204
tests:
  delete-resource:
    trigger:
      type: http.delete
      path: /v1/resource/123
    assertions:
      - response:
          status: 204
  delete-resource-short:
    trigger:
      type: delete
      path: /v1/resource/123
    assertions:
      - response:
          status: 204
`)
	wftest.RunYAMLTests(t, tmpDir+"/delete_test.yaml")
}

func TestYAMLRunner_HTTPHeadTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/head_test.yaml", `
yaml: |
  modules:
    - name: router
      type: http.router
  pipelines:
    head-resource:
      trigger:
        type: http
        config:
          path: /v1/resource/{id}
          method: HEAD
      steps:
        - name: respond
          type: step.json_response
          config:
            status: 200
            body:
              exists: true
tests:
  head-resource:
    trigger:
      type: http.head
      path: /v1/resource/123
    assertions:
      - response:
          status: 200
  head-resource-short:
    trigger:
      type: head
      path: /v1/resource/123
    assertions:
      - response:
          status: 200
`)
	wftest.RunYAMLTests(t, tmpDir+"/head_test.yaml")
}

func TestYAMLRunner_ResponseJSON(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/json_test.yaml", `
yaml: |
  modules:
    - name: router
      type: http.router
  pipelines:
    hello:
      trigger:
        type: http
        config:
          path: /hello
          method: GET
      steps:
        - name: respond
          type: step.json_response
          config:
            status: 200
            body:
              message: "hello"
              data:
                id: "abc123"
tests:
  json-path-check:
    trigger:
      type: http
      path: /hello
    assertions:
      - response:
          status: 200
          json:
            message: "hello"
            data.id: "abc123"
          json_not_empty:
            - message
            - data
          headers:
            Content-Type: "application/json"
`)
	wftest.RunYAMLTests(t, tmpDir+"/json_test.yaml")
}

func TestRunYAMLTests_ScheduleTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/schedule_test.yaml", `
yaml: |
  pipelines:
    cleanup-sessions:
      steps:
        - name: run
          type: step.set
          config:
            values:
              status: completed
tests:
  cleanup-job:
    trigger:
      type: schedule
      name: cleanup-sessions
    assertions:
      - output:
          status: completed
`)
	wftest.RunYAMLTests(t, tmpDir+"/schedule_test.yaml")
}

func TestRunYAMLTests_ScheduleTriggerWithData(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/schedule_data_test.yaml", `
yaml: |
  pipelines:
    parameterized-job:
      steps:
        - name: echo
          type: step.set
          config:
            values:
              got: "{{ .param1 }}"
tests:
  job-with-params:
    trigger:
      type: schedule
      name: parameterized-job
      data:
        param1: value1
    assertions:
      - output:
          got: value1
`)
	wftest.RunYAMLTests(t, tmpDir+"/schedule_data_test.yaml")
}

func TestRunYAMLTests_EventTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/event_test.yaml", `
yaml: |
  pipelines:
    on-submission:
      trigger:
        type: eventbus
        config:
          topic: forms.submission.created
      steps:
        - name: process
          type: step.set
          config:
            values:
              handled: true
              form_id: "{{ .form_id }}"
tests:
  submission-event:
    trigger:
      type: event
      name: forms.submission.created
      data:
        affiliate_id: sampleaff1
        form_id: form-uuid-1
    assertions:
      - output:
          handled: true
          form_id: form-uuid-1
`)
	wftest.RunYAMLTests(t, tmpDir+"/event_test.yaml")
}

func TestRunYAMLTests_EventbusTriggerAlias(t *testing.T) {
	tmpDir := t.TempDir()
	writeFile(t, tmpDir+"/eventbus_test.yaml", `
yaml: |
  pipelines:
    on-user-created:
      trigger:
        type: eventbus
        config:
          topic: user.created
      steps:
        - name: log_event
          type: step.set
          config:
            values:
              handled: true
              user_id: "{{ .user_id }}"
tests:
  user-created:
    trigger:
      type: eventbus
      name: user.created
      data:
        user_id: "123"
    assertions:
      - output:
          handled: true
          user_id: "123"
`)
	wftest.RunYAMLTests(t, tmpDir+"/eventbus_test.yaml")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
