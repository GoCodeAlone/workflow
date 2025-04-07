// Package mock provides common mock implementations for testing
package mock

// LogLevel represents logging severity levels
type LogLevel int

const (
	// These are placeholder values - adjust them according to actual requirements
	LogDebug LogLevel = iota
	LogInfo
	LogWarning
	LogError
	LogFatal
)

// ConfigProvider implements a mock config provider
type ConfigProvider struct {
	ConfigData map[string]interface{}
}

// GetConfig implements the ConfigProvider interface
func (m *ConfigProvider) GetConfig() any {
	return m.ConfigData
}

// UpdateConfigWithProperEnvStructure updates the configuration with proper environment structure
// This method fixes the "env: env: invalid structure" error that occurs with golobby config
func (m *ConfigProvider) UpdateConfigWithProperEnvStructure() *ConfigProvider {
	// Create a completely fresh config map without any env section
	m.ConfigData = map[string]interface{}{
		// Basic sections needed by workflow tests
		"modules":   []interface{}{},
		"workflows": map[string]interface{}{},
	}

	// Important: Do NOT include any "env" field at all
	// This should prevent the "env: env: invalid structure" error

	return m
}

// NewConfigProvider creates a new mock config provider with proper env structure
func NewConfigProvider() *ConfigProvider {
	cp := &ConfigProvider{
		ConfigData: make(map[string]interface{}),
	}
	return cp.UpdateConfigWithProperEnvStructure()
}

// WithWorkflow adds a workflow configuration to the mock config
func (m *ConfigProvider) WithWorkflow(name string, config map[string]interface{}) *ConfigProvider {
	if m.ConfigData == nil {
		m.ConfigData = make(map[string]interface{})
	}

	// Ensure workflows section exists
	if _, ok := m.ConfigData["workflows"]; !ok {
		m.ConfigData["workflows"] = make(map[string]interface{})
	}

	workflows := m.ConfigData["workflows"].(map[string]interface{})

	// Add the workflow config under its name
	workflows[name] = config

	return m
}

// Logger implements a mock logger
type Logger struct {
	LogEntries []string
}

// Debug implements the Logger interface
func (m *Logger) Debug(msg string, args ...interface{}) {
	if m.LogEntries == nil {
		m.LogEntries = make([]string, 0)
	}
	m.LogEntries = append(m.LogEntries, msg)
}

// Info implements the Logger interface
func (m *Logger) Info(msg string, args ...interface{}) {
	if m.LogEntries == nil {
		m.LogEntries = make([]string, 0)
	}
	m.LogEntries = append(m.LogEntries, msg)
}

// Error implements the Logger interface
func (m *Logger) Error(msg string, args ...interface{}) {
	if m.LogEntries == nil {
		m.LogEntries = make([]string, 0)
	}
	m.LogEntries = append(m.LogEntries, msg)
}

// Warn implements the Logger interface
func (m *Logger) Warn(msg string, args ...interface{}) {
	if m.LogEntries == nil {
		m.LogEntries = make([]string, 0)
	}
	m.LogEntries = append(m.LogEntries, msg)
}
