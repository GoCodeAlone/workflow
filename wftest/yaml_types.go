package wftest

// TestFile is the top-level structure of a *_test.yaml file.
type TestFile struct {
	// Config is a file path to a workflow YAML config.
	// Mutually exclusive with YAML.
	Config string `yaml:"config"`
	// YAML is an inline workflow YAML config string.
	// Mutually exclusive with Config.
	YAML string `yaml:"yaml"`
	// Mocks are step-level mocks shared across all test cases.
	// Per-test Mocks override these.
	Mocks MockConfig `yaml:"mocks"`
	// Tests maps test case names to their definitions.
	Tests map[string]TestCase `yaml:"tests"`
}

// MockConfig holds YAML-level step mock declarations.
// Each entry maps a step type (e.g. "step.db_query") to a fixed output map.
type MockConfig struct {
	Steps map[string]map[string]any `yaml:"steps"`
}

// TestCase defines one test within a TestFile.
type TestCase struct {
	// Description is an optional human-readable label shown in test output.
	Description string `yaml:"description"`
	// Trigger describes how to invoke the pipeline or HTTP endpoint.
	// Mutually exclusive with Sequence.
	Trigger TriggerDef `yaml:"trigger"`
	// StopAfter halts pipeline execution after the named step completes.
	StopAfter string `yaml:"stop_after"`
	// Mocks overrides the file-level Mocks for this test case only.
	Mocks *MockConfig `yaml:"mocks"`
	// Assertions is an ordered list of checks applied to the result.
	// Used with Trigger; ignored when Sequence is set.
	Assertions []Assertion `yaml:"assertions"`
	// State configures initial state to seed before the test runs.
	State *StateConfig `yaml:"state"`
	// Sequence replaces the single Trigger for multi-step stateful tests.
	// Each step fires a trigger and may assert pipeline output and state.
	Sequence []SequenceStep `yaml:"sequence"`
}

// StateConfig describes state to set up before a test case runs.
type StateConfig struct {
	// Fixtures are files to load into named stores.
	Fixtures []FixtureDef `yaml:"fixtures"`
	// Seed maps store_name → key → value for inline initial state.
	Seed map[string]map[string]any `yaml:"seed"`
}

// FixtureDef loads a JSON or YAML file into a named state store.
type FixtureDef struct {
	// File is the path to the fixture file (JSON or YAML).
	File string `yaml:"file"`
	// Target is the store name to seed.
	Target string `yaml:"target"`
}

// SequenceStep is one step in a multi-step stateful test.
type SequenceStep struct {
	// Name is a label shown in test output.
	Name string `yaml:"name"`
	// Pipeline is a shorthand for a pipeline trigger name.
	// If Trigger.Name is empty, Pipeline is used.
	Pipeline string `yaml:"pipeline"`
	// Trigger describes how to invoke this step.
	Trigger TriggerDef `yaml:"trigger"`
	// Assertions are checked after this step executes.
	Assertions []Assertion `yaml:"assertions"`
}

// TriggerDef describes how to invoke the system under test.
type TriggerDef struct {
	// Type is the trigger kind: "pipeline", "http".
	Type string `yaml:"type"`
	// Name is the pipeline name (used when Type is "pipeline").
	Name string `yaml:"name"`
	// Data is the trigger input data (pipeline trigger data or HTTP body).
	Data map[string]any `yaml:"data"`
	// Method is the HTTP method (used when Type is "http").
	Method string `yaml:"method"`
	// Path is the HTTP path (used when Type is "http").
	Path string `yaml:"path"`
	// Headers are extra HTTP request headers.
	Headers map[string]string `yaml:"headers"`
}

// Assertion is one check applied to the test result.
type Assertion struct {
	// Step scopes this assertion to a specific step's output.
	// If empty, the assertion applies to the overall pipeline output.
	Step string `yaml:"step"`
	// Output checks that all key/value pairs in this map appear in the output.
	Output map[string]any `yaml:"output"`
	// Executed checks whether the named step ran (true) or was skipped (false).
	Executed *bool `yaml:"executed"`
	// Response checks HTTP response fields (status, body).
	Response *ResponseAssert `yaml:"response"`
	// State checks per-store key/value pairs in the StateStore.
	// Maps store_name → key → expected_value.
	// Requires WithState() (or state config in YAML).
	State map[string]map[string]any `yaml:"state"`
}

// ResponseAssert checks HTTP response fields.
type ResponseAssert struct {
	// Status is the expected HTTP status code (0 means "don't check").
	Status int `yaml:"status"`
	// Body is a substring expected in the response body.
	Body string `yaml:"body"`
	// JSON maps dot-path keys to expected values for exact JSON path equality checks.
	// Example: {"message": "ok", "data.id": "abc123"}
	JSON map[string]any `yaml:"json"`
	// JSONNotEmpty lists dot-paths that must be present and non-empty in the JSON body.
	// Example: ["data", "meta"]
	JSONNotEmpty []string `yaml:"json_not_empty"`
	// Headers maps response header names to expected values.
	// Example: {"Content-Type": "application/json"}
	Headers map[string]string `yaml:"headers"`
}
