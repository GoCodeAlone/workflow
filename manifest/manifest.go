// Package manifest provides static analysis of WorkflowConfig to produce
// infrastructure requirements manifests.
package manifest

import (
	"fmt"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
)

// DatabaseRequirement describes a database dependency.
type DatabaseRequirement struct {
	ModuleName    string `json:"moduleName"`
	Driver        string `json:"driver,omitempty"`
	DSN           string `json:"dsn,omitempty"`
	MaxOpenConns  int    `json:"maxOpenConns,omitempty"`
	MaxIdleConns  int    `json:"maxIdleConns,omitempty"`
	EstCapacityMB int    `json:"estCapacityMB,omitempty"`
}

// ServiceRequirement describes a service the workflow provides.
type ServiceRequirement struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Port      int    `json:"port,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}

// EventBusRequirement describes eventing infrastructure needs.
type EventBusRequirement struct {
	Technology string   `json:"technology"`
	Topics     []string `json:"topics,omitempty"`
	Queues     []string `json:"queues,omitempty"`
}

// StorageRequirement describes an object/file storage dependency.
type StorageRequirement struct {
	ModuleName string `json:"moduleName"`
	Type       string `json:"type"` // s3, local, gcs
	Bucket     string `json:"bucket,omitempty"`
	Region     string `json:"region,omitempty"`
	RootDir    string `json:"rootDir,omitempty"`
}

// ExternalAPIRequirement describes an outbound API dependency.
type ExternalAPIRequirement struct {
	StepName string `json:"stepName"`
	URL      string `json:"url"`
	Method   string `json:"method,omitempty"`
	AuthType string `json:"authType,omitempty"`
}

// PortRequirement describes a network port the workflow listens on.
type PortRequirement struct {
	ModuleName string `json:"moduleName"`
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	Protected  bool   `json:"protected,omitempty"`
}

// ResourceEstimate provides rough resource estimates for capacity planning.
type ResourceEstimate struct {
	CPUCores float64 `json:"cpuCores"`
	MemoryMB int     `json:"memoryMB"`
	DiskMB   int     `json:"diskMB"`
}

// WorkflowManifest is the full requirements report for a workflow config.
type WorkflowManifest struct {
	Name         string                   `json:"name"`
	Databases    []DatabaseRequirement    `json:"databases,omitempty"`
	Services     []ServiceRequirement     `json:"services,omitempty"`
	EventBus     *EventBusRequirement     `json:"eventBus,omitempty"`
	Storage      []StorageRequirement     `json:"storage,omitempty"`
	ExternalAPIs []ExternalAPIRequirement `json:"externalAPIs,omitempty"`
	Ports        []PortRequirement        `json:"ports,omitempty"`
	ResourceEst  ResourceEstimate         `json:"resourceEstimate"`
}

// Analyze walks a WorkflowConfig and returns its infrastructure manifest.
func Analyze(cfg *config.WorkflowConfig) *WorkflowManifest {
	m := &WorkflowManifest{
		Name: deriveName(cfg),
	}

	analyzeModules(cfg, m)
	analyzePipelines(cfg, m)
	analyzeWorkflows(cfg, m)
	estimateResources(m, len(cfg.Modules))

	return m
}

// AnalyzeWithName is like Analyze but overrides the manifest name.
func AnalyzeWithName(cfg *config.WorkflowConfig, name string) *WorkflowManifest {
	m := Analyze(cfg)
	if name != "" {
		m.Name = name
	}
	return m
}

// deriveName extracts a name from the config. Falls back to the config dir base.
func deriveName(cfg *config.WorkflowConfig) string {
	if cfg.ConfigDir != "" {
		return filepath.Base(cfg.ConfigDir)
	}
	return "unknown"
}

// estimateResources produces rough estimates based on module counts and requirements.
func estimateResources(m *WorkflowManifest, moduleCount int) {
	// Base: 0.1 CPU core, 64MB RAM per module
	m.ResourceEst.CPUCores = float64(moduleCount) * 0.1
	if m.ResourceEst.CPUCores < 0.25 {
		m.ResourceEst.CPUCores = 0.25
	}
	m.ResourceEst.MemoryMB = moduleCount * 64
	if m.ResourceEst.MemoryMB < 128 {
		m.ResourceEst.MemoryMB = 128
	}

	// Add disk estimates from databases
	for _, db := range m.Databases {
		m.ResourceEst.DiskMB += db.EstCapacityMB
	}
	if m.ResourceEst.DiskMB == 0 && len(m.Databases) > 0 {
		m.ResourceEst.DiskMB = 256 // default for any DB
	}

	// Add storage estimates
	for range m.Storage {
		m.ResourceEst.DiskMB += 512
	}
}

// Summary returns a human-readable summary string for the manifest.
func (m *WorkflowManifest) Summary() string {
	s := fmt.Sprintf("Workflow: %s\n", m.Name)

	if len(m.Ports) > 0 {
		s += fmt.Sprintf("  Ports: %d\n", len(m.Ports))
		for _, p := range m.Ports {
			s += fmt.Sprintf("    - :%d (%s) [%s]\n", p.Port, p.Protocol, p.ModuleName)
		}
	}

	if len(m.Databases) > 0 {
		s += fmt.Sprintf("  Databases: %d\n", len(m.Databases))
		for _, db := range m.Databases {
			s += fmt.Sprintf("    - %s (driver=%s) [%s]\n", db.ModuleName, db.Driver, db.DSN)
		}
	}

	if m.EventBus != nil {
		s += fmt.Sprintf("  EventBus: %s (%d topics)\n", m.EventBus.Technology, len(m.EventBus.Topics))
	}

	if len(m.Storage) > 0 {
		s += fmt.Sprintf("  Storage: %d\n", len(m.Storage))
		for _, st := range m.Storage {
			s += fmt.Sprintf("    - %s (%s)\n", st.ModuleName, st.Type)
		}
	}

	if len(m.ExternalAPIs) > 0 {
		s += fmt.Sprintf("  External APIs: %d\n", len(m.ExternalAPIs))
		for _, api := range m.ExternalAPIs {
			s += fmt.Sprintf("    - %s %s [%s]\n", api.Method, api.URL, api.StepName)
		}
	}

	if len(m.Services) > 0 {
		s += fmt.Sprintf("  Services: %d\n", len(m.Services))
		for _, svc := range m.Services {
			s += fmt.Sprintf("    - %s (%s)\n", svc.Name, svc.Type)
		}
	}

	s += fmt.Sprintf("  Resource estimates: %.2f CPU cores, %d MB RAM, %d MB disk\n",
		m.ResourceEst.CPUCores, m.ResourceEst.MemoryMB, m.ResourceEst.DiskMB)

	return s
}
