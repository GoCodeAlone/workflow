package wftest_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

const queryUsersPipeline = `
pipelines:
  query-users:
    steps:
      - name: fetch
        type: step.db_query
        config:
          database: db
          query: "SELECT * FROM users"
          mode: list
      - name: respond
        type: step.set
        config:
          values:
            count: "{{ index .steps \"fetch\" \"count\" }}"
`

func TestHarness_MockStep_Returns(t *testing.T) {
	h := wftest.New(t,
		wftest.WithYAML(queryUsersPipeline),
		wftest.MockStep("step.db_query", wftest.Returns(map[string]any{
			"rows":  []any{map[string]any{"id": 1, "email": "test@example.com"}},
			"count": 1,
		})),
	)

	result := h.ExecutePipeline("query-users", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// count should be templated from the mock fetch step output
	if result.Output["count"] != "1" {
		t.Errorf("expected count='1', got %v", result.Output["count"])
	}
}

func TestHarness_MockStep_Recorder_CallCount(t *testing.T) {
	recorder := wftest.NewRecorder()
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  insert-user:
    steps:
      - name: write
        type: step.db_exec
        config:
          database: db
          query: "INSERT INTO users (email) VALUES (?)"
          mode: exec
`),
		wftest.MockStep("step.db_exec", recorder),
	)

	h.ExecutePipeline("insert-user", map[string]any{"email": "test@example.com"})
	if recorder.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", recorder.CallCount())
	}
}

func TestHarness_MockStep_Recorder_InputCaptured(t *testing.T) {
	recorder := wftest.NewRecorder()
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  greet:
    steps:
      - name: setup
        type: step.set
        config:
          values:
            name: "{{ .user }}"
      - name: log
        type: step.db_exec
        config:
          database: db
          query: "INSERT INTO log (msg) VALUES (?)"
          mode: exec
`),
		wftest.MockStep("step.db_exec", recorder),
	)

	h.ExecutePipeline("greet", map[string]any{"user": "alice"})

	calls := recorder.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Input["name"] != "alice" {
		t.Errorf("expected input name='alice', got %v", calls[0].Input["name"])
	}
}

func TestHarness_MockStep_Recorder_WithOutput(t *testing.T) {
	recorder := wftest.NewRecorder().WithOutput(map[string]any{
		"rows":  []any{map[string]any{"id": 42}},
		"count": 1,
	})
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  fetch:
    steps:
      - name: db
        type: step.db_query
        config:
          database: db
          query: "SELECT id FROM users LIMIT 1"
          mode: list
`),
		wftest.MockStep("step.db_query", recorder),
	)

	result := h.ExecutePipeline("fetch", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if recorder.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", recorder.CallCount())
	}
}

func TestHarness_MockStep_Recorder_Reset(t *testing.T) {
	recorder := wftest.NewRecorder()
	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  noop:
    steps:
      - name: s
        type: step.db_exec
        config:
          database: db
          query: "SELECT 1"
          mode: exec
`),
		wftest.MockStep("step.db_exec", recorder),
	)

	h.ExecutePipeline("noop", nil)
	if recorder.CallCount() != 1 {
		t.Errorf("expected 1 call before reset, got %d", recorder.CallCount())
	}

	recorder.Reset()
	if recorder.CallCount() != 0 {
		t.Errorf("expected 0 calls after reset, got %d", recorder.CallCount())
	}
}

func TestHarness_MockStep_MultipleTypes(t *testing.T) {
	queryRec := wftest.NewRecorder().WithOutput(map[string]any{"rows": []any{}, "count": 0})
	execRec := wftest.NewRecorder()

	h := wftest.New(t,
		wftest.WithYAML(`
pipelines:
  combined:
    steps:
      - name: read
        type: step.db_query
        config:
          database: db
          query: "SELECT 1"
          mode: list
      - name: write
        type: step.db_exec
        config:
          database: db
          query: "INSERT INTO log SELECT 1"
          mode: exec
`),
		wftest.MockStep("step.db_query", queryRec),
		wftest.MockStep("step.db_exec", execRec),
	)

	result := h.ExecutePipeline("combined", nil)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if queryRec.CallCount() != 1 {
		t.Errorf("expected 1 query call, got %d", queryRec.CallCount())
	}
	if execRec.CallCount() != 1 {
		t.Errorf("expected 1 exec call, got %d", execRec.CallCount())
	}
}
