# package manifest

Import path: `github.com/GoCodeAlone/workflow/manifest`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/manifest

## Warnings

None

## Synopsis

Package manifest provides static analysis of WorkflowConfig to produce
infrastructure requirements manifests.

## Types

### type DatabaseRequirement

DatabaseRequirement describes a database dependency.

```go
type DatabaseRequirement struct {
	ModuleName	string	`json:"moduleName"`
	Driver		string	`json:"driver,omitempty"`
	DSN		string	`json:"dsn,omitempty"`
	MaxOpenConns	int	`json:"maxOpenConns,omitempty"`
	MaxIdleConns	int	`json:"maxIdleConns,omitempty"`
	EstCapacityMB	int	`json:"estCapacityMB,omitempty"`
}
```

### type EventBusRequirement

EventBusRequirement describes eventing infrastructure needs.

```go
type EventBusRequirement struct {
	Technology	string		`json:"technology"`
	Topics		[]string	`json:"topics,omitempty"`
	Queues		[]string	`json:"queues,omitempty"`
}
```

### type ExternalAPIRequirement

ExternalAPIRequirement describes an outbound API dependency.

```go
type ExternalAPIRequirement struct {
	StepName	string	`json:"stepName"`
	URL		string	`json:"url"`
	Method		string	`json:"method,omitempty"`
	AuthType	string	`json:"authType,omitempty"`
}
```

### type PortRequirement

PortRequirement describes a network port the workflow listens on.

```go
type PortRequirement struct {
	ModuleName	string	`json:"moduleName"`
	Port		int	`json:"port"`
	Protocol	string	`json:"protocol"`
	Protected	bool	`json:"protected,omitempty"`
}
```

### type ResourceEstimate

ResourceEstimate provides rough resource estimates for capacity planning.

```go
type ResourceEstimate struct {
	CPUCores	float64	`json:"cpuCores"`
	MemoryMB	int	`json:"memoryMB"`
	DiskMB		int	`json:"diskMB"`
}
```

### type ServiceRequirement

ServiceRequirement describes a service the workflow provides.

```go
type ServiceRequirement struct {
	Name		string	`json:"name"`
	Type		string	`json:"type"`
	Port		int	`json:"port,omitempty"`
	Protected	bool	`json:"protected,omitempty"`
}
```

### type SidecarRequirement

SidecarRequirement describes a sidecar container dependency.

```go
type SidecarRequirement struct {
	Name		string		`json:"name"`
	Type		string		`json:"type"`
	Config		map[string]any	`json:"config,omitempty"`
	DependsOn	[]string	`json:"dependsOn,omitempty"`
}
```

### type StorageRequirement

StorageRequirement describes an object/file storage dependency.

```go
type StorageRequirement struct {
	ModuleName	string	`json:"moduleName"`
	Type		string	`json:"type"`	// s3, local, gcs
	Bucket		string	`json:"bucket,omitempty"`
	Region		string	`json:"region,omitempty"`
	RootDir		string	`json:"rootDir,omitempty"`
}
```

### type WorkflowManifest

WorkflowManifest is the full requirements report for a workflow config.

```go
type WorkflowManifest struct {
	Name		string				`json:"name"`
	Databases	[]DatabaseRequirement		`json:"databases,omitempty"`
	Services	[]ServiceRequirement		`json:"services,omitempty"`
	EventBus	*EventBusRequirement		`json:"eventBus,omitempty"`
	Storage		[]StorageRequirement		`json:"storage,omitempty"`
	ExternalAPIs	[]ExternalAPIRequirement	`json:"externalAPIs,omitempty"`
	Ports		[]PortRequirement		`json:"ports,omitempty"`
	ResourceEst	ResourceEstimate		`json:"resourceEstimate"`
	Sidecars	[]SidecarRequirement		`json:"sidecars,omitempty"`
}
```

## Functions

### func Analyze

Analyze walks a WorkflowConfig and returns its infrastructure manifest.

```go
func Analyze(cfg *config.WorkflowConfig) *WorkflowManifest
```

### func AnalyzeWithName

AnalyzeWithName is like Analyze but overrides the manifest name.

```go
func AnalyzeWithName(cfg *config.WorkflowConfig, name string) *WorkflowManifest
```

## Methods

### func Summary

Summary returns a human-readable summary string for the manifest.

```go
func (m *WorkflowManifest) Summary() string
```

