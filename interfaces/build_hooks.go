package interfaces

// HookEvent is the string identifier for a build-pipeline hook event.
type HookEvent string

const (
	// Build lifecycle events emitted by wfctl build.
	HookEventPreBuild              HookEvent = "pre_build"
	HookEventPreTargetBuild        HookEvent = "pre_target_build"
	HookEventPostTargetBuild       HookEvent = "post_target_build"
	HookEventPreContainerBuild     HookEvent = "pre_container_build"
	HookEventPostContainerBuild    HookEvent = "post_container_build"
	HookEventPreContainerPush      HookEvent = "pre_container_push"
	HookEventPostContainerPush     HookEvent = "post_container_push"
	HookEventPreArtifactsPublish   HookEvent = "pre_artifacts_publish"
	HookEventPostArtifactsPublish  HookEvent = "post_artifacts_publish"
	HookEventPreBuildFail          HookEvent = "pre_build_fail"
	HookEventPostBuild             HookEvent = "post_build"

	// HookEventInstallVerify is emitted by wfctl plugin install after tarball
	// download and before extraction. The supply-chain plugin registers a handler
	// that verifies cosign signatures, SBOM presence, and OSV vulnerability policy.
	HookEventInstallVerify HookEvent = "install_verify"
)

var allHookEvents = []HookEvent{
	HookEventPreBuild,
	HookEventPreTargetBuild,
	HookEventPostTargetBuild,
	HookEventPreContainerBuild,
	HookEventPostContainerBuild,
	HookEventPreContainerPush,
	HookEventPostContainerPush,
	HookEventPreArtifactsPublish,
	HookEventPostArtifactsPublish,
	HookEventPreBuildFail,
	HookEventPostBuild,
	HookEventInstallVerify,
}

// AllHookEvents returns all defined hook event constants.
func AllHookEvents() []HookEvent {
	out := make([]HookEvent, len(allHookEvents))
	copy(out, allHookEvents)
	return out
}

// IsValidHookEvent returns true if s matches a known HookEvent constant.
func IsValidHookEvent(s string) bool {
	for _, e := range allHookEvents {
		if string(e) == s {
			return true
		}
	}
	return false
}

// HookPayload is the JSON envelope passed to a plugin hook handler on stdin.
// Plugins decode this and then further decode Data into the event-specific struct.
type HookPayload struct {
	Event     HookEvent      `json:"event"`
	Plugin    string         `json:"plugin"`
	BuildID   string         `json:"build_id"`
	Timestamp int64          `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// --- Per-event typed payload structs ---

// PreBuildPayload is the Data payload for HookEventPreBuild.
type PreBuildPayload struct {
	ConfigPath string   `json:"config_path"`
	Targets    []string `json:"targets"`
}

// PreTargetBuildPayload / PostTargetBuildPayload carry per-target context.
type PreTargetBuildPayload struct {
	Target string `json:"target"`
	Type   string `json:"type"`
}

type PostTargetBuildPayload struct {
	Target     string `json:"target"`
	Type       string `json:"type"`
	Outputs    []string `json:"outputs,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// PreContainerBuildPayload carries Dockerfile + context for a container build start.
type PreContainerBuildPayload struct {
	Target         string `json:"target"`
	DockerfilePath string `json:"dockerfile_path"`
	ContextPath    string `json:"context_path"`
	Tags           []string `json:"tags"`
}

// PostContainerBuildPayload carries the built image digest.
type PostContainerBuildPayload struct {
	Target         string `json:"target"`
	ImageRef       string `json:"image_ref"`
	Digest         string `json:"digest"`
	DurationMs     int64  `json:"duration_ms"`
}

// PreContainerPushPayload / PostContainerPushPayload describe registry push.
type PreContainerPushPayload struct {
	ImageRef   string   `json:"image_ref"`
	Registries []string `json:"registries"`
}

type PostContainerPushPayload struct {
	ImageRef   string   `json:"image_ref"`
	Digest     string   `json:"digest"`
	Registries []string `json:"registries"`
	DurationMs int64    `json:"duration_ms"`
}

// PreArtifactsPublishPayload / PostArtifactsPublishPayload describe asset publishing.
type PreArtifactsPublishPayload struct {
	Assets []string `json:"assets"`
}

type PostArtifactsPublishPayload struct {
	Assets     []ArtifactEntry `json:"assets"`
	DurationMs int64           `json:"duration_ms"`
}

// ArtifactEntry is a single published artifact with its URL.
type ArtifactEntry struct {
	Path string `json:"path"`
	URL  string `json:"url"`
}

// PreBuildFailPayload carries the error that caused the build to fail.
type PreBuildFailPayload struct {
	Error  string `json:"error"`
	Target string `json:"target,omitempty"`
}

// PostBuildPayload is the final build summary.
type PostBuildPayload struct {
	Targets    []string `json:"targets"`
	Success    bool     `json:"success"`
	DurationMs int64    `json:"duration_ms"`
}

// InstallVerifyPayload is the Data payload for HookEventInstallVerify.
// The supply-chain plugin handler verifies the tarball's cosign signature,
// SBOM presence, and OSV vulnerability scan per the declared policy.
type InstallVerifyPayload struct {
	TarballPath               string `json:"tarball_path"`
	ExpectedSignatureIdentity string `json:"expected_signature_identity,omitempty"`
	ExpectedSBOMHash          string `json:"expected_sbom_hash,omitempty"`
	VulnPolicy                string `json:"vuln_policy,omitempty"` // block-critical | warn | off
}
