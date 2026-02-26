package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"
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
//	provider:     mock | aws (default: mock; aws selected when account is set)
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

	// Select backend: use "provider" config key, or fall back based on the cloud account.
	providerType, _ := m.config["provider"].(string)
	if providerType == "" && m.provider != nil {
		providerType = m.provider.Provider()
	}
	if providerType == "" {
		providerType = "mock"
	}
	if providerType == "aws" {
		m.backend = &codebuildAWSBackend{}
	} else {
		m.backend = &codebuildMockBackend{}
	}

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
		lines = append(lines, "env:", "  variables:")
		for k, v := range vars {
			lines = append(lines, fmt.Sprintf("    %s: %q", k, fmt.Sprint(v)))
		}
		lines = append(lines, "")
	}

	lines = append(lines, "phases:")

	// install phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "install_commands"); len(cmds) > 0 {
		lines = append(lines, "  install:", "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// pre_build phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "pre_build_commands"); len(cmds) > 0 {
		lines = append(lines, "  pre_build:", "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// build phase (required — always present)
	buildCmds := codebuildExtractStringSlice(pipelineConfig, "build_commands")
	if len(buildCmds) == 0 {
		buildCmds = []string{"echo Build started", "echo Build complete"}
	}
	lines = append(lines, "  build:", "    commands:")
	for _, cmd := range buildCmds {
		lines = append(lines, "      - "+cmd)
	}

	// post_build phase
	if cmds := codebuildExtractStringSlice(pipelineConfig, "post_build_commands"); len(cmds) > 0 {
		lines = append(lines, "  post_build:", "    commands:")
		for _, cmd := range cmds {
			lines = append(lines, "      - "+cmd)
		}
	}

	// Artifacts section
	if files := codebuildExtractStringSlice(pipelineConfig, "artifact_files"); len(files) > 0 {
		lines = append(lines, "", "artifacts:", "  files:")
		for _, f := range files {
			lines = append(lines, "    - "+f)
		}
		if dir, ok := pipelineConfig["artifact_dir"].(string); ok && dir != "" {
			lines = append(lines, "  base-directory: "+dir)
		}
	}

	// Cache section
	if paths := codebuildExtractStringSlice(pipelineConfig, "cache_paths"); len(paths) > 0 {
		lines = append(lines, "", "cache:", "  paths:")
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

// codebuildMockBackend implements codebuildBackend using in-memory state for
// local testing and development. Selected via provider: mock config.
type codebuildMockBackend struct {
	buildCounter int64
}

func (b *codebuildMockBackend) createProject(m *CodeBuildModule) error {
	if m.state.Status == "pending" || m.state.Status == "deleted" {
		m.state.Status = "creating"
		m.state.CreatedAt = time.Now()
		m.state.Status = "ready"
	}
	return nil
}

func (b *codebuildMockBackend) deleteProject(m *CodeBuildModule) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleting"
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

// ─── AWS CodeBuild backend ────────────────────────────────────────────────────

// codebuildAWSBackend manages AWS CodeBuild projects and builds using
// aws-sdk-go-v2/service/codebuild. Selected via provider: aws config.
type codebuildAWSBackend struct{}

func (b *codebuildAWSBackend) awsClient(m *CodeBuildModule) (*codebuild.Client, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return nil, fmt.Errorf("codebuild aws: no AWS cloud account configured")
	}
	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("codebuild aws: AWS config: %w", err)
	}
	return codebuild.NewFromConfig(cfg), nil
}

func (b *codebuildAWSBackend) createProject(m *CodeBuildModule) error {
	client, err := b.awsClient(m)
	if err != nil {
		return err
	}

	// Check if project already exists so we can update instead of create.
	batchOut, getErr := client.BatchGetProjects(context.Background(), &codebuild.BatchGetProjectsInput{
		Names: []string{m.state.Name},
	})
	projectExists := getErr == nil && len(batchOut.Projects) > 0

	env := &cbtypes.ProjectEnvironment{
		Type:           cbtypes.EnvironmentTypeLinuxContainer,
		ComputeType:    cbtypes.ComputeType(m.state.ComputeType),
		Image:          aws.String(m.state.Image),
		PrivilegedMode: aws.Bool(false),
	}
	src := &cbtypes.ProjectSource{Type: cbtypes.SourceType(m.state.SourceType)}
	artifacts := &cbtypes.ProjectArtifacts{Type: cbtypes.ArtifactsTypeNoArtifacts}

	if projectExists {
		if _, updateErr := client.UpdateProject(context.Background(), &codebuild.UpdateProjectInput{
			Name:        aws.String(m.state.Name),
			ServiceRole: aws.String(m.state.ServiceRole),
			Environment: env,
			Source:      src,
			Artifacts:   artifacts,
		}); updateErr != nil {
			return fmt.Errorf("codebuild aws: UpdateProject: %w", updateErr)
		}
		m.state.Status = "ready"
		return nil
	}

	out, err := client.CreateProject(context.Background(), &codebuild.CreateProjectInput{
		Name:        aws.String(m.state.Name),
		ServiceRole: aws.String(m.state.ServiceRole),
		Environment: env,
		Source:      src,
		Artifacts:   artifacts,
	})
	if err != nil {
		return fmt.Errorf("codebuild aws: CreateProject: %w", err)
	}

	if out.Project != nil {
		if out.Project.Arn != nil {
			m.state.ARN = aws.ToString(out.Project.Arn)
		}
		if out.Project.Created != nil {
			m.state.CreatedAt = *out.Project.Created
		}
	}
	m.state.Status = "ready"
	return nil
}

func (b *codebuildAWSBackend) deleteProject(m *CodeBuildModule) error {
	client, err := b.awsClient(m)
	if err != nil {
		return err
	}
	if _, err := client.DeleteProject(context.Background(), &codebuild.DeleteProjectInput{
		Name: aws.String(m.state.Name),
	}); err != nil {
		return fmt.Errorf("codebuild aws: DeleteProject: %w", err)
	}
	m.state.Status = "deleted"
	return nil
}

func (b *codebuildAWSBackend) startBuild(m *CodeBuildModule, envOverrides map[string]string) (*CodeBuildBuild, error) {
	client, err := b.awsClient(m)
	if err != nil {
		return nil, err
	}

	input := &codebuild.StartBuildInput{
		ProjectName: aws.String(m.state.Name),
	}
	if len(envOverrides) > 0 {
		envVars := make([]cbtypes.EnvironmentVariable, 0, len(envOverrides))
		for k, v := range envOverrides {
			k, v := k, v
			envVars = append(envVars, cbtypes.EnvironmentVariable{
				Name:  aws.String(k),
				Value: aws.String(v),
				Type:  cbtypes.EnvironmentVariableTypePlaintext,
			})
		}
		input.EnvironmentVariablesOverride = envVars
	}

	out, err := client.StartBuild(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("codebuild aws: StartBuild: %w", err)
	}
	if out.Build == nil {
		return nil, fmt.Errorf("codebuild aws: StartBuild returned nil build")
	}

	build := awsCodeBuildToInternal(out.Build, envOverrides)
	m.builds[build.ID] = build
	return build, nil
}

func (b *codebuildAWSBackend) getBuildStatus(m *CodeBuildModule, buildID string) (*CodeBuildBuild, error) {
	client, err := b.awsClient(m)
	if err != nil {
		return nil, err
	}
	out, err := client.BatchGetBuilds(context.Background(), &codebuild.BatchGetBuildsInput{
		Ids: []string{buildID},
	})
	if err != nil {
		return nil, fmt.Errorf("codebuild aws: BatchGetBuilds: %w", err)
	}
	if len(out.Builds) == 0 {
		return nil, fmt.Errorf("codebuild: build %q not found", buildID)
	}
	build := awsCodeBuildToInternal(&out.Builds[0], nil)
	m.builds[buildID] = build
	return build, nil
}

func (b *codebuildAWSBackend) getBuildLogs(m *CodeBuildModule, buildID string) ([]string, error) {
	build, err := b.getBuildStatus(m, buildID)
	if err != nil {
		return nil, err
	}
	return build.Logs, nil
}

func (b *codebuildAWSBackend) listBuilds(m *CodeBuildModule) ([]*CodeBuildBuild, error) {
	client, err := b.awsClient(m)
	if err != nil {
		return nil, err
	}

	listOut, err := client.ListBuildsForProject(context.Background(), &codebuild.ListBuildsForProjectInput{
		ProjectName: aws.String(m.state.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("codebuild aws: ListBuildsForProject: %w", err)
	}
	if len(listOut.Ids) == 0 {
		return nil, nil
	}

	batchOut, err := client.BatchGetBuilds(context.Background(), &codebuild.BatchGetBuildsInput{
		Ids: listOut.Ids,
	})
	if err != nil {
		return nil, fmt.Errorf("codebuild aws: BatchGetBuilds: %w", err)
	}

	builds := make([]*CodeBuildBuild, 0, len(batchOut.Builds))
	for i := range batchOut.Builds {
		build := awsCodeBuildToInternal(&batchOut.Builds[i], nil)
		m.builds[build.ID] = build
		builds = append(builds, build)
	}
	return builds, nil
}

// awsCodeBuildToInternal converts an AWS SDK Build to the internal CodeBuildBuild type.
func awsCodeBuildToInternal(b *cbtypes.Build, envOverrides map[string]string) *CodeBuildBuild {
	build := &CodeBuildBuild{EnvVars: envOverrides}
	if b.Id != nil {
		build.ID = aws.ToString(b.Id)
	}
	if b.ProjectName != nil {
		build.ProjectName = aws.ToString(b.ProjectName)
	}
	if b.BuildStatus != "" {
		build.Status = string(b.BuildStatus)
	}
	if b.CurrentPhase != nil {
		build.Phase = aws.ToString(b.CurrentPhase)
	}
	if b.StartTime != nil {
		build.StartTime = *b.StartTime
	}
	if b.EndTime != nil {
		build.EndTime = b.EndTime
	}
	if b.BuildNumber != nil {
		build.BuildNumber = *b.BuildNumber
	}
	if b.Logs != nil {
		if b.Logs.GroupName != nil {
			build.Logs = append(build.Logs, fmt.Sprintf("log group: %s", aws.ToString(b.Logs.GroupName)))
		}
		if b.Logs.StreamName != nil {
			build.Logs = append(build.Logs, fmt.Sprintf("log stream: %s", aws.ToString(b.Logs.StreamName)))
		}
		if b.Logs.DeepLink != nil {
			build.Logs = append(build.Logs, fmt.Sprintf("deep link: %s", aws.ToString(b.Logs.DeepLink)))
		}
	}
	return build
}
