package module

import (
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// AppContainerModule manages application containers on top of platform modules.
// Config:
//
//	environment: name of a platform.kubernetes or platform.ecs module (service registry)
//	image:       container image (required)
//	replicas:    desired replica count (default: 1)
//	ports:       list of container ports
//	cpu:         CPU request/limit (default: "256m")
//	memory:      memory request/limit (default: "512Mi")
//	env:         environment variables
//	health_path: HTTP health check path (default: "/healthz")
//	health_port: health check port (default: first port or 8080)
type AppContainerModule struct {
	name         string
	config       map[string]any
	environment  string // platform module name resolved from service registry
	platformType string // "kubernetes" or "ecs"
	spec         AppContainerSpec
	current      *AppDeployResult // current deployment state
	previous     *AppDeployResult // last known-good deployment for rollback
	backend      appContainerBackend
}

// AppContainerSpec describes the desired state of an application container.
type AppContainerSpec struct {
	Image      string            `json:"image"`
	Replicas   int               `json:"replicas"`
	Ports      []int             `json:"ports"`
	Env        map[string]string `json:"env"`
	CPU        string            `json:"cpu"`
	Memory     string            `json:"memory"`
	HealthPath string            `json:"healthPath"`
	HealthPort int               `json:"healthPort"`
}

// ResourceSpec defines CPU and memory limits for a container.
type ResourceSpec struct {
	CPU    string `json:"cpu"`    // e.g. "500m"
	Memory string `json:"memory"` // e.g. "512Mi"
}

// HealthCheckSpec defines the HTTP health check for a container.
type HealthCheckSpec struct {
	Path     string `json:"path"`
	Port     int    `json:"port"`
	Interval int    `json:"interval"` // seconds
}

// AppDeployResult holds the current deployment state.
type AppDeployResult struct {
	Platform string `json:"platform"` // kubernetes, ecs
	Name     string `json:"name"`
	Status   string `json:"status"` // deploying, active, failed, rolled_back
	Endpoint string `json:"endpoint"`
	Replicas int    `json:"replicas"`
	Image    string `json:"image"`
}

// K8sManifests holds the generated Kubernetes manifests for an app container.
type K8sManifests struct {
	Deployment *K8sDeploymentManifest `json:"deployment"`
	Service    *K8sServiceManifest    `json:"service"`
	Ingress    *K8sIngressManifest    `json:"ingress,omitempty"`
}

// ECSAppManifests holds the generated ECS task definition and service config.
type ECSAppManifests struct {
	TaskDefinition ECSAppTaskDef    `json:"taskDefinition"`
	Service        ECSAppServiceCfg `json:"service"`
}

// ECSAppTaskDef represents an ECS task definition for an app container.
type ECSAppTaskDef struct {
	Family     string        `json:"family"`
	CPU        string        `json:"cpu"`
	Memory     string        `json:"memory"`
	Containers []ECSContainer `json:"containers"`
}

// ECSAppServiceCfg represents ECS service configuration for an app container.
type ECSAppServiceCfg struct {
	Name           string `json:"name"`
	TaskDefinition string `json:"taskDefinition"`
	DesiredCount   int    `json:"desiredCount"`
	LaunchType     string `json:"launchType"`
}

// appContainerBackend is the interface implemented by platform-specific backends.
type appContainerBackend interface {
	deploy(a *AppContainerModule) (*AppDeployResult, error)
	status(a *AppContainerModule) (*AppDeployResult, error)
	rollback(a *AppContainerModule, image string) (*AppDeployResult, error)
	manifests(a *AppContainerModule) (any, error)
}

// NewAppContainerModule creates a new AppContainerModule.
func NewAppContainerModule(name string, cfg map[string]any) *AppContainerModule {
	return &AppContainerModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *AppContainerModule) Name() string { return m.name }

// Init resolves the environment module and initialises the platform backend.
func (m *AppContainerModule) Init(app modular.Application) error {
	envName, _ := m.config["environment"].(string)
	if envName != "" {
		svc, ok := app.SvcRegistry()[envName]
		if !ok {
			return fmt.Errorf("app.container %q: environment service %q not found", m.name, envName)
		}
		m.environment = envName
		switch svc.(type) {
		case *PlatformKubernetes:
			m.backend = &k8sAppBackend{}
			m.platformType = "kubernetes"
		case *PlatformECS:
			m.backend = &ecsAppBackend{}
			m.platformType = "ecs"
		default:
			return fmt.Errorf("app.container %q: environment %q is not a platform.kubernetes or platform.ecs module (got %T)", m.name, envName, svc)
		}
	} else {
		// Default to kubernetes mock when no environment is specified.
		m.backend = &k8sAppBackend{}
		m.platformType = "kubernetes"
	}

	m.spec = m.parseSpec()

	return app.RegisterService(m.name, m)
}

// parseSpec parses AppContainerSpec from the module config, applying defaults.
func (m *AppContainerModule) parseSpec() AppContainerSpec {
	spec := AppContainerSpec{
		Env: make(map[string]string),
	}

	spec.Image, _ = m.config["image"].(string)

	if r, ok := intFromAny(m.config["replicas"]); ok && r > 0 {
		spec.Replicas = r
	} else {
		spec.Replicas = 1
	}

	if cpu, ok := m.config["cpu"].(string); ok && cpu != "" {
		spec.CPU = cpu
	} else {
		spec.CPU = "256m"
	}

	if mem, ok := m.config["memory"].(string); ok && mem != "" {
		spec.Memory = mem
	} else {
		spec.Memory = "512Mi"
	}

	if hp, ok := m.config["health_path"].(string); ok && hp != "" {
		spec.HealthPath = hp
	} else {
		spec.HealthPath = "/healthz"
	}

	// ports
	if raw, ok := m.config["ports"].([]any); ok {
		for _, p := range raw {
			if port, ok := intFromAny(p); ok {
				spec.Ports = append(spec.Ports, port)
			}
		}
	}

	// health port — use explicit or fall back to first port or 8080
	if hport, ok := intFromAny(m.config["health_port"]); ok && hport > 0 {
		spec.HealthPort = hport
	} else if len(spec.Ports) > 0 {
		spec.HealthPort = spec.Ports[0]
	} else {
		spec.HealthPort = 8080
	}

	// env vars
	if envRaw, ok := m.config["env"].(map[string]any); ok {
		for k, v := range envRaw {
			spec.Env[k] = fmt.Sprintf("%v", v)
		}
	}

	return spec
}

// ProvidesServices declares the service this module provides.
func (m *AppContainerModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "app.container: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — environment is resolved by name, not declared.
func (m *AppContainerModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Deploy stores the previous deployment state and creates a new deployment.
// The mock backend immediately transitions to "active".
func (m *AppContainerModule) Deploy() (*AppDeployResult, error) {
	// Preserve current as previous for rollback.
	if m.current != nil {
		prev := *m.current
		m.previous = &prev
	}
	result, err := m.backend.deploy(m)
	if err != nil {
		return nil, err
	}
	m.current = result
	return result, nil
}

// Status returns the current deployment result.
func (m *AppContainerModule) Status() (*AppDeployResult, error) {
	if m.current == nil {
		return &AppDeployResult{
			Platform: m.platformType,
			Name:     m.name,
			Status:   "not_deployed",
		}, nil
	}
	return m.current, nil
}

// Rollback reverts the deployment to the previous image/config.
// Returns an error if there is no previous deployment to roll back to.
func (m *AppContainerModule) Rollback() (*AppDeployResult, error) {
	if m.previous == nil {
		return nil, fmt.Errorf("app.container %q: no previous deployment to roll back to", m.name)
	}
	result, err := m.backend.rollback(m, m.previous.Image)
	if err != nil {
		return nil, err
	}
	m.current = result
	m.previous = nil
	return result, nil
}

// Manifests returns the generated platform-specific resource manifests.
func (m *AppContainerModule) Manifests() (any, error) {
	return m.backend.manifests(m)
}

// Spec returns the parsed AppContainerSpec (used in tests and pipeline steps).
func (m *AppContainerModule) Spec() AppContainerSpec {
	return m.spec
}

// ─── Kubernetes resource types ───────────────────────────────────────────────

// K8sDeploymentManifest represents a Kubernetes Deployment resource.
type K8sDeploymentManifest struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   K8sObjectMeta     `json:"metadata"`
	Spec       K8sDeploymentSpec `json:"spec"`
}

// K8sServiceManifest represents a Kubernetes Service resource.
type K8sServiceManifest struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Metadata   K8sObjectMeta `json:"metadata"`
	Spec       K8sServiceSpec `json:"spec"`
}

// K8sIngressManifest represents a Kubernetes Ingress resource.
type K8sIngressManifest struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Metadata   K8sObjectMeta `json:"metadata"`
	Spec       K8sIngressSpec `json:"spec"`
}

// K8sObjectMeta holds Kubernetes resource metadata.
type K8sObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// K8sDeploymentSpec is the spec for a Kubernetes Deployment.
type K8sDeploymentSpec struct {
	Replicas int            `json:"replicas"`
	Selector K8sSelector    `json:"selector"`
	Template K8sPodTemplate `json:"template"`
}

// K8sSelector selects pods by label.
type K8sSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

// K8sPodTemplate is the pod template in a Deployment spec.
type K8sPodTemplate struct {
	Metadata K8sObjectMeta `json:"metadata"`
	Spec     K8sPodSpec    `json:"spec"`
}

// K8sPodSpec is the spec for a pod.
type K8sPodSpec struct {
	Containers []K8sContainerSpec `json:"containers"`
}

// K8sContainerSpec is the spec for a container within a pod.
type K8sContainerSpec struct {
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	Ports          []K8sContainerPort `json:"ports,omitempty"`
	Env            []K8sEnvVar        `json:"env,omitempty"`
	Resources      K8sResourceReq     `json:"resources"`
	ReadinessProbe *K8sProbe          `json:"readinessProbe,omitempty"`
}

// K8sContainerPort is a port exposed by a container.
type K8sContainerPort struct {
	ContainerPort int `json:"containerPort"`
}

// K8sEnvVar is an environment variable in a container.
type K8sEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// K8sResourceReq holds resource requests and limits.
type K8sResourceReq struct {
	Limits   map[string]string `json:"limits,omitempty"`
	Requests map[string]string `json:"requests,omitempty"`
}

// K8sProbe defines a health check probe.
type K8sProbe struct {
	HTTPGet             K8sHTTPGetAction `json:"httpGet"`
	InitialDelaySeconds int              `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int              `json:"periodSeconds,omitempty"`
}

// K8sHTTPGetAction defines an HTTP GET health check.
type K8sHTTPGetAction struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// K8sServiceSpec defines a Kubernetes Service.
type K8sServiceSpec struct {
	Selector map[string]string `json:"selector"`
	Ports    []K8sServicePort  `json:"ports"`
	Type     string            `json:"type,omitempty"`
}

// K8sServicePort defines a port exposed by a Service.
type K8sServicePort struct {
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol,omitempty"`
}

// K8sIngressSpec defines a Kubernetes Ingress.
type K8sIngressSpec struct {
	Rules []K8sIngressRule `json:"rules"`
}

// K8sIngressRule defines an ingress routing rule.
type K8sIngressRule struct {
	Host string         `json:"host,omitempty"`
	HTTP K8sIngressHTTP `json:"http"`
}

// K8sIngressHTTP defines the HTTP routes in an ingress rule.
type K8sIngressHTTP struct {
	Paths []K8sIngressPath `json:"paths"`
}

// K8sIngressPath defines an HTTP path in an ingress rule.
type K8sIngressPath struct {
	Path     string           `json:"path"`
	PathType string           `json:"pathType"`
	Backend  K8sIngressBackend `json:"backend"`
}

// K8sIngressBackend defines the backend for an ingress path.
type K8sIngressBackend struct {
	Service K8sIngressSvcBackend `json:"service"`
}

// K8sIngressSvcBackend defines the service backend for an ingress.
type K8sIngressSvcBackend struct {
	Name string            `json:"name"`
	Port K8sServicePortRef `json:"port"`
}

// K8sServicePortRef defines a port reference in an ingress service backend.
type K8sServicePortRef struct {
	Number int `json:"number"`
}

// ─── Kubernetes backend ───────────────────────────────────────────────────────

// k8sAppBackend implements appContainerBackend for platform.kubernetes environments.
// Generates Deployment + Service + optional Ingress manifests as Go structs
// (no real k8s API calls — suitable for testing and local development).
type k8sAppBackend struct{}

func (b *k8sAppBackend) deploy(a *AppContainerModule) (*AppDeployResult, error) {
	port := 80
	if len(a.spec.Ports) > 0 {
		port = a.spec.Ports[0]
	}
	return &AppDeployResult{
		Platform: a.platformType,
		Name:     a.name,
		Status:   "active",
		Endpoint: fmt.Sprintf("http://%s.default.svc.cluster.local:%d", a.name, port),
		Replicas: a.spec.Replicas,
		Image:    a.spec.Image,
	}, nil
}

func (b *k8sAppBackend) status(a *AppContainerModule) (*AppDeployResult, error) {
	return a.current, nil
}

func (b *k8sAppBackend) rollback(a *AppContainerModule, image string) (*AppDeployResult, error) {
	if image == "" {
		return nil, fmt.Errorf("rollback: image is required")
	}
	return &AppDeployResult{
		Platform: a.platformType,
		Name:     a.name,
		Status:   "rolled_back",
		Endpoint: fmt.Sprintf("http://%s.default.svc.cluster.local", a.name),
		Replicas: a.spec.Replicas,
		Image:    image,
	}, nil
}

func (b *k8sAppBackend) manifests(a *AppContainerModule) (any, error) {
	return buildK8sManifests(a), nil
}

func buildK8sManifests(a *AppContainerModule) *K8sManifests {
	labels := map[string]string{"app": a.name}

	container := K8sContainerSpec{
		Name:  a.name,
		Image: a.spec.Image,
		Resources: K8sResourceReq{
			Limits:   map[string]string{"cpu": a.spec.CPU, "memory": a.spec.Memory},
			Requests: map[string]string{"cpu": a.spec.CPU, "memory": a.spec.Memory},
		},
	}

	for _, p := range a.spec.Ports {
		container.Ports = append(container.Ports, K8sContainerPort{ContainerPort: p})
	}

	for k, v := range a.spec.Env {
		container.Env = append(container.Env, K8sEnvVar{Name: k, Value: v})
	}

	if a.spec.HealthPath != "" && a.spec.HealthPort > 0 {
		container.ReadinessProbe = &K8sProbe{
			HTTPGet:       K8sHTTPGetAction{Path: a.spec.HealthPath, Port: a.spec.HealthPort},
			PeriodSeconds: 10,
		}
	}

	deployment := &K8sDeploymentManifest{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata:   K8sObjectMeta{Name: a.name, Labels: labels},
		Spec: K8sDeploymentSpec{
			Replicas: a.spec.Replicas,
			Selector: K8sSelector{MatchLabels: labels},
			Template: K8sPodTemplate{
				Metadata: K8sObjectMeta{Labels: labels},
				Spec:     K8sPodSpec{Containers: []K8sContainerSpec{container}},
			},
		},
	}

	var svcPorts []K8sServicePort
	for _, p := range a.spec.Ports {
		svcPorts = append(svcPorts, K8sServicePort{Port: p, TargetPort: p, Protocol: "TCP"})
	}
	if len(svcPorts) == 0 {
		svcPorts = []K8sServicePort{{Port: 80, TargetPort: 80, Protocol: "TCP"}}
	}

	service := &K8sServiceManifest{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata:   K8sObjectMeta{Name: a.name, Labels: labels},
		Spec: K8sServiceSpec{
			Selector: labels,
			Ports:    svcPorts,
			Type:     "ClusterIP",
		},
	}

	manifests := &K8sManifests{
		Deployment: deployment,
		Service:    service,
	}

	// Generate an Ingress when a health path is configured (implies an HTTP service).
	if a.spec.HealthPath != "" && len(a.spec.Ports) > 0 {
		port := a.spec.Ports[0]
		manifests.Ingress = &K8sIngressManifest{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
			Metadata:   K8sObjectMeta{Name: a.name, Labels: labels},
			Spec: K8sIngressSpec{
				Rules: []K8sIngressRule{{
					HTTP: K8sIngressHTTP{
						Paths: []K8sIngressPath{{
							Path:     "/",
							PathType: "Prefix",
							Backend: K8sIngressBackend{
								Service: K8sIngressSvcBackend{
									Name: a.name,
									Port: K8sServicePortRef{Number: port},
								},
							},
						}},
					},
				}},
			},
		}
	}

	return manifests
}

// ─── ECS backend ──────────────────────────────────────────────────────────────

// ecsAppBackend implements appContainerBackend for platform.ecs environments.
// Generates ECS task definition and service config as Go structs
// (no real ECS API calls).
type ecsAppBackend struct{}

func (b *ecsAppBackend) deploy(a *AppContainerModule) (*AppDeployResult, error) {
	return &AppDeployResult{
		Platform: a.platformType,
		Name:     a.name,
		Status:   "active",
		Endpoint: fmt.Sprintf("http://%s.ecs.local:80", a.name),
		Replicas: a.spec.Replicas,
		Image:    a.spec.Image,
	}, nil
}

func (b *ecsAppBackend) status(a *AppContainerModule) (*AppDeployResult, error) {
	return a.current, nil
}

func (b *ecsAppBackend) rollback(a *AppContainerModule, image string) (*AppDeployResult, error) {
	if image == "" {
		return nil, fmt.Errorf("rollback: image is required")
	}
	return &AppDeployResult{
		Platform: a.platformType,
		Name:     a.name,
		Status:   "rolled_back",
		Endpoint: fmt.Sprintf("http://%s.ecs.local:80", a.name),
		Replicas: a.spec.Replicas,
		Image:    image,
	}, nil
}

func (b *ecsAppBackend) manifests(a *AppContainerModule) (any, error) {
	return buildECSManifests(a), nil
}

func buildECSManifests(a *AppContainerModule) *ECSAppManifests {
	port := 0
	if len(a.spec.Ports) > 0 {
		port = a.spec.Ports[0]
	}

	return &ECSAppManifests{
		TaskDefinition: ECSAppTaskDef{
			Family:     a.name + "-task",
			CPU:        a.spec.CPU,
			Memory:     a.spec.Memory,
			Containers: []ECSContainer{{Name: a.name, Image: a.spec.Image, Port: port}},
		},
		Service: ECSAppServiceCfg{
			Name:           a.name,
			TaskDefinition: a.name + "-task",
			DesiredCount:   a.spec.Replicas,
			LaunchType:     "FARGATE",
		},
	}
}
