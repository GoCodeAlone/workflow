// Package cigen provides an analyze → CIPlan → render pipeline for generating
// CI/CD configuration files from workflow YAML configs.
package cigen

// CIPlan is a platform-neutral representation of a CI/CD plan derived from
// one or more workflow config files.
type CIPlan struct {
	// Project is the name of the project (derived from config or directory).
	Project string `json:"project"`
	// WfctlVersion is the wfctl version to pin in generated CI files.
	WfctlVersion string `json:"wfctl_version"`
	// DefaultBranch is the branch that triggers apply jobs.
	DefaultBranch string `json:"default_branch"`
	// Runner is the runner label used for GitHub Actions jobs.
	Runner string `json:"runner"`
	// PluginInstall is true when wfctl plugin install should be run before deploy.
	PluginInstall bool `json:"plugin_install"`
	// Build describes the build phase, or nil when no build is needed.
	Build *BuildSpec `json:"build,omitempty"`
	// Secrets is the union of all secret references needed by CI.
	Secrets []SecretRef `json:"secrets"`
	// Phases is the ordered list of deploy phases.
	Phases []DeployPhase `json:"phases"`
	// Migrations describes database migration config, or nil when none.
	Migrations *MigrationsSpec `json:"migrations,omitempty"`
	// Smoke describes the smoke test, or nil when no smoke test can be derived.
	Smoke *SmokeSpec `json:"smoke,omitempty"`
	// PlanGuard is true when a protected resource requires a plan-before-apply gate.
	PlanGuard bool `json:"plan_guard"`
	// Triggers describes which GitHub events trigger CI jobs.
	Triggers TriggerSpec `json:"triggers"`
	// Warnings is a list of non-fatal advisory messages surfaced to the operator.
	Warnings []string `json:"warnings"`
}

// BuildSpec describes the build phase.
type BuildSpec struct {
	// Docker is true when a Dockerfile was detected.
	Docker bool `json:"docker"`
	// Image is the image name to build (if derivable).
	Image string `json:"image,omitempty"`
}

// SecretRef is a reference to a named secret required by CI.
type SecretRef struct {
	// Name is the secret name as it appears in the CI platform's secret store.
	Name string `json:"name"`
}

// DeployPhase is a single phase in a potentially multi-phase deploy pipeline.
type DeployPhase struct {
	// Name is the human-readable phase name (e.g. "prereq", "deploy").
	Name string `json:"name"`
	// ConfigPath is the workflow config file for this phase.
	ConfigPath string `json:"config_path"`
	// Include is an optional list of module names to include in this phase.
	Include []string `json:"include,omitempty"`
}

// MigrationsSpec describes the database migration step.
type MigrationsSpec struct {
	// DBEnv is the environment variable name that holds the database URL.
	DBEnv string `json:"db_env"`
	// Source is the migrations source directory.
	Source string `json:"source,omitempty"`
}

// SmokeSpec describes the post-deploy smoke test.
type SmokeSpec struct {
	// URL is the full URL to curl for a 2xx response.
	URL string `json:"url"`
	// Path is the HTTP path component (e.g. "/healthz").
	Path string `json:"path"`
}

// TriggerSpec describes which CI events should trigger each class of job.
type TriggerSpec struct {
	// PR triggers plan/lint jobs on pull requests.
	PR bool `json:"pr"`
	// PushMain triggers apply jobs on push to the default branch.
	PushMain bool `json:"push_main"`
	// Dispatch allows manual workflow_dispatch triggers.
	Dispatch bool `json:"dispatch"`
}
