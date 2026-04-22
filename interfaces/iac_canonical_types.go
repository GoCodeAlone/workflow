package interfaces

// JobSpec describes a one-off or scheduled job workload.
// Kind is one of: PRE_DEPLOY, POST_DEPLOY, FAILED_DEPLOY, SCHEDULED.
type JobSpec struct {
	Name            string            `json:"name" yaml:"name"`
	Kind            string            `json:"kind" yaml:"kind"`
	Image           string            `json:"image,omitempty" yaml:"image,omitempty"`
	RunCommand      string            `json:"run_command" yaml:"run_command"`
	EnvVars         map[string]string `json:"env_vars,omitempty" yaml:"env_vars,omitempty"`
	EnvVarsSecret   map[string]string `json:"env_vars_secret,omitempty" yaml:"env_vars_secret,omitempty"`
	Cron            string            `json:"cron,omitempty" yaml:"cron,omitempty"` // non-empty for SCHEDULED kind
	Termination     *TerminationSpec  `json:"termination,omitempty" yaml:"termination,omitempty"`
	Alerts          []AlertSpec       `json:"alerts,omitempty" yaml:"alerts,omitempty"`
	LogDestinations []LogDestinationSpec `json:"log_destinations,omitempty" yaml:"log_destinations,omitempty"`
}

// WorkerSpec describes a long-running background worker workload.
type WorkerSpec struct {
	Name            string               `json:"name" yaml:"name"`
	Image           string               `json:"image,omitempty" yaml:"image,omitempty"`
	RunCommand      string               `json:"run_command" yaml:"run_command"`
	EnvVars         map[string]string    `json:"env_vars,omitempty" yaml:"env_vars,omitempty"`
	EnvVarsSecret   map[string]string    `json:"env_vars_secret,omitempty" yaml:"env_vars_secret,omitempty"`
	InstanceCount   int                  `json:"instance_count" yaml:"instance_count"`
	Size            string               `json:"size,omitempty" yaml:"size,omitempty"`
	Autoscaling     *AutoscalingSpec     `json:"autoscaling,omitempty" yaml:"autoscaling,omitempty"`
	HealthCheck     *HealthCheckSpec     `json:"health_check,omitempty" yaml:"health_check,omitempty"`
	Resources       *WorkloadResourceSpec `json:"resources,omitempty" yaml:"resources,omitempty"`
	Termination     *TerminationSpec     `json:"termination,omitempty" yaml:"termination,omitempty"`
	Alerts          []AlertSpec          `json:"alerts,omitempty" yaml:"alerts,omitempty"`
	LogDestinations []LogDestinationSpec `json:"log_destinations,omitempty" yaml:"log_destinations,omitempty"`
}

// StaticSiteSpec describes a statically-built web front-end.
type StaticSiteSpec struct {
	Name         string      `json:"name" yaml:"name"`
	BuildCommand string      `json:"build_command" yaml:"build_command"`
	OutputDir    string      `json:"output_dir" yaml:"output_dir"`
	EnvVars      map[string]string `json:"env_vars,omitempty" yaml:"env_vars,omitempty"`
	Routes       []RouteSpec `json:"routes,omitempty" yaml:"routes,omitempty"`
	CORS         *CORSSpec   `json:"cors,omitempty" yaml:"cors,omitempty"`
	Domains      []DomainSpec `json:"domains,omitempty" yaml:"domains,omitempty"`
	Alerts       []AlertSpec `json:"alerts,omitempty" yaml:"alerts,omitempty"`
}

// SidecarSpec describes a companion container that shares the network with
// the main service (e.g. Tailscale, Envoy).
type SidecarSpec struct {
	Name              string               `json:"name" yaml:"name"`
	Image             string               `json:"image,omitempty" yaml:"image,omitempty"`
	RunCommand        string               `json:"run_command" yaml:"run_command"`
	EnvVars           map[string]string    `json:"env_vars,omitempty" yaml:"env_vars,omitempty"`
	EnvVarsSecret     map[string]string    `json:"env_vars_secret,omitempty" yaml:"env_vars_secret,omitempty"`
	Ports             []PortSpec           `json:"ports,omitempty" yaml:"ports,omitempty"`
	Resources         *WorkloadResourceSpec `json:"resources,omitempty" yaml:"resources,omitempty"`
	HealthCheck       *HealthCheckSpec     `json:"health_check,omitempty" yaml:"health_check,omitempty"`
	SharesNetworkWith string               `json:"shares_network_with,omitempty" yaml:"shares_network_with,omitempty"` // name of the service this sidecar attaches to
}

// PortSpec describes a named port exposed by a service or sidecar.
type PortSpec struct {
	Name     string `json:"name" yaml:"name"`
	Port     int    `json:"port" yaml:"port"`
	Protocol string `json:"protocol" yaml:"protocol"` // http | tcp | udp
	Public   bool   `json:"public" yaml:"public"`
}

// AutoscalingSpec controls horizontal-pod / instance autoscaling.
type AutoscalingSpec struct {
	Min           int `json:"min" yaml:"min"`
	Max           int `json:"max" yaml:"max"`
	CPUPercent    int `json:"cpu_percent" yaml:"cpu_percent"`
	MemoryPercent int `json:"memory_percent" yaml:"memory_percent"`
}

// HealthCheckSpec describes an HTTP or TCP health probe.
type HealthCheckSpec struct {
	HTTPPath            string `json:"http_path,omitempty" yaml:"http_path,omitempty"`
	TCPPort             int    `json:"tcp_port,omitempty" yaml:"tcp_port,omitempty"`
	Port                int    `json:"port,omitempty" yaml:"port,omitempty"`
	InitialDelaySeconds int    `json:"initial_delay_seconds" yaml:"initial_delay_seconds"`
	PeriodSeconds       int    `json:"period_seconds" yaml:"period_seconds"`
	TimeoutSeconds      int    `json:"timeout_seconds" yaml:"timeout_seconds"`
	SuccessThreshold    int    `json:"success_threshold" yaml:"success_threshold"`
	FailureThreshold    int    `json:"failure_threshold" yaml:"failure_threshold"`
}

// RouteSpec maps an HTTP path prefix to this service.
type RouteSpec struct {
	Path               string `json:"path" yaml:"path"`
	PreservePathPrefix bool   `json:"preserve_path_prefix" yaml:"preserve_path_prefix"`
}

// CORSSpec configures Cross-Origin Resource Sharing headers.
type CORSSpec struct {
	AllowOrigins     []string `json:"allow_origins,omitempty" yaml:"allow_origins,omitempty"`
	AllowMethods     []string `json:"allow_methods,omitempty" yaml:"allow_methods,omitempty"`
	AllowHeaders     []string `json:"allow_headers,omitempty" yaml:"allow_headers,omitempty"`
	ExposeHeaders    []string `json:"expose_headers,omitempty" yaml:"expose_headers,omitempty"`
	AllowCredentials bool     `json:"allow_credentials" yaml:"allow_credentials"`
	MaxAge           string   `json:"max_age,omitempty" yaml:"max_age,omitempty"`
}

// DomainSpec describes a custom domain attached to a service.
type DomainSpec struct {
	Name     string `json:"name" yaml:"name"`
	Zone     string `json:"zone,omitempty" yaml:"zone,omitempty"`
	Type     string `json:"type" yaml:"type"` // PRIMARY | ALIAS
	Wildcard bool   `json:"wildcard" yaml:"wildcard"`
}

// AlertSpec defines an observable metric threshold alert.
type AlertSpec struct {
	Rule     string  `json:"rule" yaml:"rule"`
	Operator string  `json:"operator" yaml:"operator"` // GREATER_THAN | LESS_THAN
	Value    float64 `json:"value" yaml:"value"`
	Window   string  `json:"window" yaml:"window"` // e.g. "10m"
	Disabled bool    `json:"disabled" yaml:"disabled"`
}

// LogDestinationSpec forwards logs to an external system.
type LogDestinationSpec struct {
	Name     string            `json:"name" yaml:"name"`
	Endpoint string            `json:"endpoint" yaml:"endpoint"`
	Headers  map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	TLS      bool              `json:"tls" yaml:"tls"`
}

// TerminationSpec controls drain / graceful-shutdown behaviour.
type TerminationSpec struct {
	DrainSeconds       int `json:"drain_seconds" yaml:"drain_seconds"`
	GracePeriodSeconds int `json:"grace_period_seconds" yaml:"grace_period_seconds"`
}

// IngressSpec controls inbound traffic routing.
type IngressSpec struct {
	LoadBalancer string        `json:"load_balancer,omitempty" yaml:"load_balancer,omitempty"` // e.g. round_robin | least_connections
	Rules        []IngressRule `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// IngressRule maps an HTTP match to a backend component.
type IngressRule struct {
	Match     string `json:"match" yaml:"match"`     // path prefix or header expression
	Component string `json:"component" yaml:"component"` // target service/worker name
}

// EgressSpec controls outbound traffic policy.
type EgressSpec struct {
	Bandwidth string      `json:"bandwidth,omitempty" yaml:"bandwidth,omitempty"` // informational budget, e.g. "1Gbps"
	Rules     []EgressRule `json:"rules,omitempty" yaml:"rules,omitempty"`
}

// EgressRule is a single outbound allow/deny entry.
type EgressRule struct {
	Destination string `json:"destination" yaml:"destination"`
	Protocol    string `json:"protocol" yaml:"protocol"`
	Allow       bool   `json:"allow" yaml:"allow"`
}

// MaintenanceSpec defines a recurring maintenance window.
type MaintenanceSpec struct {
	Window  string `json:"window" yaml:"window"` // e.g. "Sun 03:00-05:00 UTC"
	Enabled bool   `json:"enabled" yaml:"enabled"`
}

// WorkloadResourceSpec declares CPU and memory resource requests/limits for a workload.
// Named distinctly from interfaces.ResourceSpec which is used for IaC infrastructure resources.
type WorkloadResourceSpec struct {
	CPUMillis   int `json:"cpu_millis" yaml:"cpu_millis"` // 1000 = 1 vCPU
	MemoryMiB   int `json:"memory_mib" yaml:"memory_mib"`
	CPULimit    int `json:"cpu_limit" yaml:"cpu_limit"`
	MemoryLimit int `json:"memory_limit" yaml:"memory_limit"`
}
