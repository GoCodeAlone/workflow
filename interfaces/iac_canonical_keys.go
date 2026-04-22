package interfaces

// Canonical IaC config key constants. These are the standard field names used
// across all IaC providers to describe application resource configuration.
const (
	KeyName             = "name"
	KeyRegion           = "region"
	KeyImage            = "image"
	KeyHTTPPort         = "http_port"
	KeyInternalPorts    = "internal_ports"
	KeyProtocol         = "protocol"
	KeyInstanceCount    = "instance_count"
	KeySize             = "size"
	KeyEnvVars          = "env_vars"
	KeyEnvVarsSecret    = "env_vars_secret"
	KeyVPCRef           = "vpc_ref"
	KeyAutoscaling      = "autoscaling"
	KeyRoutes           = "routes"
	KeyCORS             = "cors"
	KeyDomains          = "domains"
	KeyHealthCheck      = "health_check"
	KeyLivenessCheck    = "liveness_check"
	KeyIngress          = "ingress"
	KeyEgress           = "egress"
	KeyAlerts           = "alerts"
	KeyLogDestinations  = "log_destinations"
	KeyTermination      = "termination"
	KeyMaintenance      = "maintenance"
	KeyJobs             = "jobs"
	KeyWorkers          = "workers"
	KeyStaticSites      = "static_sites"
	KeySidecars         = "sidecars"
	KeyBuildCommand     = "build_command"
	KeyRunCommand       = "run_command"
	KeyDockerfilePath   = "dockerfile_path"
	KeySourceDir        = "source_dir"
	KeyProviderSpecific = "provider_specific"
)

// canonicalKeySet is the authoritative set of canonical IaC config keys.
var canonicalKeySet = map[string]struct{}{
	KeyName:             {},
	KeyRegion:           {},
	KeyImage:            {},
	KeyHTTPPort:         {},
	KeyInternalPorts:    {},
	KeyProtocol:         {},
	KeyInstanceCount:    {},
	KeySize:             {},
	KeyEnvVars:          {},
	KeyEnvVarsSecret:    {},
	KeyVPCRef:           {},
	KeyAutoscaling:      {},
	KeyRoutes:           {},
	KeyCORS:             {},
	KeyDomains:          {},
	KeyHealthCheck:      {},
	KeyLivenessCheck:    {},
	KeyIngress:          {},
	KeyEgress:           {},
	KeyAlerts:           {},
	KeyLogDestinations:  {},
	KeyTermination:      {},
	KeyMaintenance:      {},
	KeyJobs:             {},
	KeyWorkers:          {},
	KeyStaticSites:      {},
	KeySidecars:         {},
	KeyBuildCommand:     {},
	KeyRunCommand:       {},
	KeyDockerfilePath:   {},
	KeySourceDir:        {},
	KeyProviderSpecific: {},
}

// IsCanonicalKey returns true if key is a recognized canonical IaC config key.
func IsCanonicalKey(key string) bool {
	_, ok := canonicalKeySet[key]
	return ok
}

// CanonicalKeys returns the full list of canonical IaC config key strings.
func CanonicalKeys() []string {
	keys := make([]string, 0, len(canonicalKeySet))
	for k := range canonicalKeySet {
		keys = append(keys, k)
	}
	return keys
}
