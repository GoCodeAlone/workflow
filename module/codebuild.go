package module

import (
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
)

// CodeBuildProjectState holds the current state of a managed CodeBuild project.
type CodeBuildProjectState struct {
	Name          string            `json:"name"`
	Region        string            `json:"region"`
	ServiceRole   string            `json:"serviceRole"`
	ComputeType   string            `json:"computeType"`
	Image         string            `json:"image"`
	Status        string            `json:"status"` // pending, creating, ready, deleting, deleted
	SourceType    string            `json:"sourceType"`
	BuildspecPath string            `json:"buildspecPath"`
	EnvVars       map[string]string `json:"envVars,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	ARN           string            `json:"arn"`
}

// CodeBuildBuild represents a single CodeBuild build invocation.
type CodeBuildBuild struct {
	ID          string            `json:"id"`
	ProjectName string            `json:"projectName"`
	Status      string            `json:"status"` // IN_PROGRESS, SUCCEEDED, FAILED, STOPPED
	Phase       string            `json:"phase"`
	StartTime   time.Time         `json:"startTime"`
	EndTime     *time.Time        `json:"endTime,omitempty"`
	Logs        []string          `json:"logs"`
	EnvVars     map[string]string `json:"envVars,omitempty"`
	BuildNumber int64             `json:"buildNumber"`
}

// CodeBuildModule manages AWS CodeBuild projects and builds via pluggable backends.
// Config:
//
//	account:      name of a cloud.account module (resolved from service registry)
//	region:       AWS region (e.g. us-east-1)
//	service_role: IAM role ARN for CodeBuild
//	compute_type: BUILD_GENERAL1_SMALL, BUILD_GENERAL1_MEDIUM, etc.
//	image:        CodeBuild managed image (e.g. aws/codebuild/standard:7.0)
//	source_type:  GITHUB, CODECOMMIT, BITBUCKET, NO_SOURCE (default: NO_SOURCE)
type CodeBuildModule struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *CodeBuildProjectState
	builds   map[string]*CodeBuildBuild
	backend  codebuildBackend
}

// codebuildBackend is the internal interface for CodeBuild backends.
type codebuildBackend interface {
	createProject(m *CodeBuildModule) error
	deleteProject(m *CodeBuildModule) error
	startBuild(m *CodeBuildModule, envOverrides map[string]string) (*CodeBuildBuild, error)
	getBuildStatus(m *CodeBuildModule, buildID string) (*CodeBuildBuild, error)
	getBuildLogs(m *CodeBuildModule, buildID string) ([]string, error)
	listBuilds(m *CodeBuildModule) ([]*CodeBuildBuild, error)
}

// NewCodeBuildModule creates a new CodeBuildModule.
func NewCodeBuildModule(name string, cfg map[string]any) *CodeBuildModule {
	return &CodeBuildModule{
		name:   name,
		config: cfg,
		builds: make(map[string]*CodeBuildBuild),
	}
}

// Name returns the module name.
func (m *CodeBuildModule) Name() string { return m.name }

// Init resolves the cloud.account service and initializes the backend.
func (m *CodeBuildModule) Init(app modular.Application) error {
	region, _ := m.config["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	serviceRole, _ := m.config["service_role"].(string)
	if serviceRole == "" {
		serviceRole = fmt.Sprintf("arn:aws:iam::123456789012:role/CodeBuildServiceRole-%s", m.name)
	}

	computeType, _ := m.config["compute_type"].(string)
	if computeType == "" {
		computeType = "BUILD_GENERAL1_SMALL"
	}

	image, _ := m.config["image"].(string)
	if image == "" {
		image = "aws/codebuild/standard:7.0"
	}

	sourceType, _ := m.config["source_type"].(string)
	if sourceType == "" {
		sourceType = "NO_SOURCE"
	}

	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("aws.codebuild %q: account service %q not found", m.name, accountName)
		}
		provider, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("aws.codebuild %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = provider
	}

	m.state = &CodeBuildProjectState{
		Name:        m.name,
		Region:      region,
		ServiceRole: serviceRole,
		ComputeType: computeType,
		Image:       image,
		Status:      "pending",
		SourceType:  sourceType,
		ARN:         fmt.Sprintf("arn:aws:codebuild:%s:123456789012:project/%s", region, m.name),
	}

	m.backend = &codebuildMockBackend{}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *CodeBuildModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "CodeBuild project: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name, not declared.
func (m *CodeBuildModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// CreateProject creates or updates the CodeBuild project.
func (m *CodeBuildModule) CreateProject() error {
	return m.backend.createProject(m)
}

// DeleteProject removes the CodeBuild project.
func (m *CodeBuildModule) DeleteProject() error {
	return m.backend.deleteProject(m)
}

// StartBuild starts a new build, optionally overriding environment variables.
func (m *CodeBuildModule) StartBuild(envOverrides map[string]string) (*CodeBuildBuild, error) {
	return m.backend.startBuild(m, envOverrides)
}

// GetBuildStatus returns the current status of a build.
func (m *CodeBuildModule) GetBuildStatus(buildID string) (*CodeBuildBuild, error) {
	return m.backend.getBuildStatus(m, buildID)
}

// GetBuildLogs retrieves the log lines for a build.
func (m *CodeBuildModule) GetBuildLogs(buildID string) ([]string, error) {
	return m.backend.getBuildLogs(m, buildID)
}

// ListBuilds returns all builds for this project.
func (m *CodeBuildModule) ListBuilds() ([]*CodeBuildBuild, error) {
	return m.backend.listBuilds(m)
}

// GenerateBuildspec translates a pipeline step config into a CodeBuild buildspec YAML string.
// Supported config keys: install_commands, pre_build_commands, build_commands,
// post_build_commands, artifact_files, artifact_dir, cache_paths, env_variables.
func (m *CodeBuildModule) GenerateBuildspec(pipelineConfig map[string]any) string {
	lines := []string{"version: 0.2", ""}

	// Env section
	if vars, ok := pipelineConfig["env_variables"].(map[string]any); ok && len(vars) > 0 {
		lines = append(lines, "env:")
		lines = append(lines, "  variables:")
		for k, v := range vars {
			lines = append(lines, fmt.Sprintf("    %s: %q", k, fmt.Sprint(v)))
		}
		lines = append(lines, "")
	}

	lines = append(lines, "phases:")

	// install phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "install_commands"); len(cmds) > 0 {
		lines = append(lines, "  install:")
		lines = append(lines, "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// pre_build phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "pre_build_commands"); len(cmds) > 0 {
		lines = append(lines, "  pre_build:")
		lines = append(lines, "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// build phase (required — always present)
	buildCmds := codebuildExtractStringSlice(pipelineConfig, "build_commands")
	if len(buildCmds) == 0 {
		buildCmds = []string{"echo Build started", "echo Build complete"}
	}
	lines = append(lines, "  build:")
	lines = append(lines, "    commands:")
	for _, cmd := range buildCmds {
		lines = append(lines, "      - "+cmd)
	}

	// post_build phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "post_build_commands"); len(cmds) > 0 {
		lines = append(lines, "  post_build:")
		lines = append(lines, "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// Artifacts section
	if files := codebuildExtractStringSlice(pipelineConfig, "artifact_files"); len(files) > 0 {
		lines = append(lines, "")
		lines = append(lines, "artifacts:")
		lines = append(lines, "  files:")
		for _, f := range files {
			lines = append(lines, "    - "+f)
		}
		if dir, ok := pipelineConfig["artifact_dir"].(string); ok && dir != "" {
			lines = append(lines, "  base-directory: "+dir)
		}
	}

	// Cache section
	if paths := codebuildExtractStringSlice(pipelineConfig, "cache_paths"); len(paths) > 0 {
		lines = append(lines, "")
		lines = append(lines, "cache:")
		lines = append(lines, "  paths:")
		for _, p := range paths {
			lines = append(lines, "    - "+p)
		}
	}

	return strings.Join(lines, "\n")
}

// codebuildExtractStringSlice extracts a []string from a map value that may be []any or []string.
func codebuildExtractStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// ─── mock backend ─────────────────────────────────────────────────────────────

// codebuildMockBackend implements codebuildBackend using in-memory state.
// Real implementation would use aws-sdk-go-v2/service/codebuild.
type codebuildMockBackend struct {
	buildCounter int64
}

func (b *codebuildMockBackend) createProject(m *CodeBuildModule) error {
	if m.state.Status == "pending" || m.state.Status == "deleted" {
		m.state.Status = "creating"
		m.state.CreatedAt = time.Now()
		// In-memory: immediately transition to ready.
		m.state.Status = "ready"
	}
	return nil
}

func (b *codebuildMockBackend) deleteProject(m *CodeBuildModule) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleting"
	// In-memory: immediately mark deleted.
	m.state.Status = "deleted"
	return nil
}

func (b *codebuildMockBackend) startBuild(m *CodeBuildModule, envOverrides map[string]string) (*CodeBuildBuild, error) {
	if m.state.Status != "ready" {
		return nil, fmt.Errorf("codebuild: project %q is not ready (status=%s)", m.name, m.state.Status)
	}

	b.buildCounter++
	buildID := fmt.Sprintf("%s:%x", m.name, b.buildCounter)
	now := time.Now()
	end := now.Add(30 * time.Second)

	build := &CodeBuildBuild{
		ID:          buildID,
		ProjectName: m.name,
		Status:      "SUCCEEDED",
		Phase:       "COMPLETED",
		StartTime:   now,
		EndTime:     &end,
		Logs: []string{
			"[Container] Build started",
			"[Container] Running install commands",
			"[Container] Running build commands",
			"[Container] Build succeeded",
		},
		EnvVars:     envOverrides,
		BuildNumber: b.buildCounter,
	}

	m.builds[buildID] = build
	return build, nil
}

func (b *codebuildMockBackend) getBuildStatus(m *CodeBuildModule, buildID string) (*CodeBuildBuild, error) {
	build, ok := m.builds[buildID]
	if !ok {
		return nil, fmt.Errorf("codebuild: build %q not found in project %q", buildID, m.name)
	}
	return build, nil
}

func (b *codebuildMockBackend) getBuildLogs(m *CodeBuildModule, buildID string) ([]string, error) {
	build, ok := m.builds[buildID]
	if !ok {
		return nil, fmt.Errorf("codebuild: build %q not found in project %q", buildID, m.name)
	}
	return build.Logs, nil
}

func (b *codebuildMockBackend) listBuilds(m *CodeBuildModule) ([]*CodeBuildBuild, error) {
	result := make([]*CodeBuildBuild, 0, len(m.builds))
	for _, build := range m.builds {
		result = append(result, build)
	}
	return result, nil
}
