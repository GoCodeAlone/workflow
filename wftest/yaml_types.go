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
	Trigger TriggerDef `yaml:"trigger"`
	// StopAfter halts pipeline execution after the named step completes.
	StopAfter string `yaml:"stop_after"`
	// Mocks overrides the file-level Mocks for this test case only.
	Mocks *MockConfig `yaml:"mocks"`
	// Assertions is an ordered list of checks applied to the result.
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
}

// ResponseAssert checks HTTP response fields.
type ResponseAssert struct {
	// Status is the expected HTTP status code (0 means "don't check").
	Status int `yaml:"status"`
	// Body is a substring expected in the response body.
	Body string `yaml:"body"`
}
