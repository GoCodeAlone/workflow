package interfaces

// JobSpec describes a one-off or scheduled job workload.
// Kind is one of: PRE_DEPLOY, POST_DEPLOY, FAILED_DEPLOY, SCHEDULED.
type JobSpec struct {
	Name            string
	Kind            string
	Image           string
	RunCommand      string
	EnvVars         map[string]string
	EnvVarsSecret   map[string]string
	Cron            string // non-empty for SCHEDULED kind
	Termination     *TerminationSpec
	Alerts          []AlertSpec
	LogDestinations []LogDestinationSpec
}

// WorkerSpec describes a long-running background worker workload.
type WorkerSpec struct {
	Name            string
	Image           string
	RunCommand      string
	EnvVars         map[string]string
	EnvVarsSecret   map[string]string
	InstanceCount   int
	Size            string
	Autoscaling     *AutoscalingSpec
	HealthCheck     *HealthCheckSpec
	Resources       *WorkloadResourceSpec
	Termination     *TerminationSpec
	Alerts          []AlertSpec
	LogDestinations []LogDestinationSpec
}

// StaticSiteSpec describes a statically-built web front-end.
type StaticSiteSpec struct {
	Name         string
	BuildCommand string
	OutputDir    string
	EnvVars      map[string]string
	Routes       []RouteSpec
	CORS         *CORSSpec
	Domains      []DomainSpec
	Alerts       []AlertSpec
}

// SidecarSpec describes a companion container that shares the network with
// the main service (e.g. Tailscale, Envoy).
type SidecarSpec struct {
	Name              string
	Image             string
	RunCommand        string
	EnvVars           map[string]string
	EnvVarsSecret     map[string]string
	Ports             []PortSpec
	Resources         *WorkloadResourceSpec
	HealthCheck       *HealthCheckSpec
	SharesNetworkWith string // name of the service this sidecar attaches to
}

// PortSpec describes a named port exposed by a service or sidecar.
type PortSpec struct {
	Name     string
	Port     int
	Protocol string // http | tcp | udp
	Public   bool
}

// AutoscalingSpec controls horizontal-pod / instance autoscaling.
type AutoscalingSpec struct {
	Min           int
	Max           int
	CPUPercent    int
	MemoryPercent int
}

// HealthCheckSpec describes an HTTP or TCP health probe.
type HealthCheckSpec struct {
	HTTPPath            string
	TCPPort             int
	Port                int
	InitialDelaySeconds int
	PeriodSeconds       int
	TimeoutSeconds      int
	SuccessThreshold    int
	FailureThreshold    int
}

// RouteSpec maps an HTTP path prefix to this service.
type RouteSpec struct {
	Path               string
	PreservePathPrefix bool
}

// CORSSpec configures Cross-Origin Resource Sharing headers.
type CORSSpec struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           string
}

// DomainSpec describes a custom domain attached to a service.
type DomainSpec struct {
	Name     string
	Zone     string
	Type     string // PRIMARY | ALIAS
	Wildcard bool
}

// AlertSpec defines an observable metric threshold alert.
type AlertSpec struct {
	Rule     string
	Operator string // GREATER_THAN | LESS_THAN
	Value    float64
	Window   string // e.g. "10m"
	Disabled bool
}

// LogDestinationSpec forwards logs to an external system.
type LogDestinationSpec struct {
	Name     string
	Endpoint string
	Headers  map[string]string
	TLS      bool
}

// TerminationSpec controls drain / graceful-shutdown behaviour.
type TerminationSpec struct {
	DrainSeconds       int
	GracePeriodSeconds int
}

// IngressSpec controls inbound traffic routing.
type IngressSpec struct {
	LoadBalancer string // e.g. round_robin | least_connections
	Rules        []IngressRule
}

// IngressRule maps an HTTP match to a backend component.
type IngressRule struct {
	Match     string // path prefix or header expression
	Component string // target service/worker name
}

// EgressSpec controls outbound traffic policy.
type EgressSpec struct {
	Bandwidth string // informational budget, e.g. "1Gbps"
	Rules     []EgressRule
}

// EgressRule is a single outbound allow/deny entry.
type EgressRule struct {
	Destination string
	Protocol    string
	Allow       bool
}

// MaintenanceSpec defines a recurring maintenance window.
type MaintenanceSpec struct {
	Window  string // e.g. "Sun 03:00-05:00 UTC"
	Enabled bool
}

// WorkloadResourceSpec declares CPU and memory resource requests/limits for a workload.
// Named distinctly from interfaces.ResourceSpec which is used for IaC infrastructure resources.
type WorkloadResourceSpec struct {
	CPUMillis   int // 1000 = 1 vCPU
	MemoryMiB   int
	CPULimit    int
	MemoryLimit int
}
