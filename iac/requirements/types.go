package requirements

import (
	"encoding/json"
	"fmt"
	"regexp"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

type Kind string
type Runtime string
type TelemetrySignal string
type ObservabilityBackend string
type DeploymentMode string

const (
	KindObservability Kind = "observability"
	KindWebAPI        Kind = "web_api"
	KindMessageBroker Kind = "message_broker"
	KindDatabase      Kind = "database"
	KindCache         Kind = "cache"
	KindStorage       Kind = "storage"

	RuntimeKubernetes              Runtime              = "kubernetes"
	RuntimeECS                     Runtime              = "ecs"
	RuntimeCloudRun                Runtime              = "cloud_run"
	RuntimeAzureContainerApps      Runtime              = "azure_container_apps"
	RuntimeDigitalOceanAppPlatform Runtime              = "digitalocean_app_platform"
	TelemetrySignalTraces          TelemetrySignal      = "traces"
	TelemetrySignalMetrics         TelemetrySignal      = "metrics"
	TelemetrySignalLogs            TelemetrySignal      = "logs"
	ObservabilityBackendOTel       ObservabilityBackend = "otel"
	ObservabilityBackendDatadog    ObservabilityBackend = "datadog"
	ObservabilityBackendPrometheus ObservabilityBackend = "prometheus"
	ObservabilityBackendLoki       ObservabilityBackend = "loki"
	ObservabilityBackendGrafana    ObservabilityBackend = "grafana"
	DeploymentModeSidecar          DeploymentMode       = "sidecar"
	DeploymentModeDaemonSet        DeploymentMode       = "daemonset"
	DeploymentModeSiblingService   DeploymentMode       = "sibling_service"
	DeploymentModeManaged          DeploymentMode       = "managed"
)

var requirementKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

var kindToProto = map[Kind]pb.RequirementKind{
	KindObservability: pb.RequirementKind_REQUIREMENT_KIND_OBSERVABILITY,
	KindWebAPI:        pb.RequirementKind_REQUIREMENT_KIND_WEB_API,
	KindMessageBroker: pb.RequirementKind_REQUIREMENT_KIND_MESSAGE_BROKER,
	KindDatabase:      pb.RequirementKind_REQUIREMENT_KIND_DATABASE,
	KindCache:         pb.RequirementKind_REQUIREMENT_KIND_CACHE,
	KindStorage:       pb.RequirementKind_REQUIREMENT_KIND_STORAGE,
}

var runtimeToProto = map[Runtime]pb.RequirementRuntime{
	RuntimeKubernetes:              pb.RequirementRuntime_REQUIREMENT_RUNTIME_KUBERNETES,
	RuntimeECS:                     pb.RequirementRuntime_REQUIREMENT_RUNTIME_ECS,
	RuntimeCloudRun:                pb.RequirementRuntime_REQUIREMENT_RUNTIME_CLOUD_RUN,
	RuntimeAzureContainerApps:      pb.RequirementRuntime_REQUIREMENT_RUNTIME_AZURE_CONTAINER_APPS,
	RuntimeDigitalOceanAppPlatform: pb.RequirementRuntime_REQUIREMENT_RUNTIME_DIGITALOCEAN_APP_PLATFORM,
}

var signalToProto = map[TelemetrySignal]pb.TelemetrySignal{
	TelemetrySignalTraces:  pb.TelemetrySignal_TELEMETRY_SIGNAL_TRACES,
	TelemetrySignalMetrics: pb.TelemetrySignal_TELEMETRY_SIGNAL_METRICS,
	TelemetrySignalLogs:    pb.TelemetrySignal_TELEMETRY_SIGNAL_LOGS,
}

var backendToProto = map[ObservabilityBackend]pb.ObservabilityBackend{
	ObservabilityBackendOTel:       pb.ObservabilityBackend_OBSERVABILITY_BACKEND_OTEL,
	ObservabilityBackendDatadog:    pb.ObservabilityBackend_OBSERVABILITY_BACKEND_DATADOG,
	ObservabilityBackendPrometheus: pb.ObservabilityBackend_OBSERVABILITY_BACKEND_PROMETHEUS,
	ObservabilityBackendLoki:       pb.ObservabilityBackend_OBSERVABILITY_BACKEND_LOKI,
	ObservabilityBackendGrafana:    pb.ObservabilityBackend_OBSERVABILITY_BACKEND_GRAFANA,
}

var deploymentModeToProto = map[DeploymentMode]pb.DeploymentMode{
	DeploymentModeSidecar:        pb.DeploymentMode_DEPLOYMENT_MODE_SIDECAR,
	DeploymentModeDaemonSet:      pb.DeploymentMode_DEPLOYMENT_MODE_DAEMONSET,
	DeploymentModeSiblingService: pb.DeploymentMode_DEPLOYMENT_MODE_SIBLING_SERVICE,
	DeploymentModeManaged:        pb.DeploymentMode_DEPLOYMENT_MODE_MANAGED,
}

// Requirement is Workflow's provider-neutral IaC requirement model. It mirrors
// the strict protobuf contract while staying pleasant to author in Go tests and
// plugin manifest adapters.
type Requirement struct {
	Key                   string
	Kind                  Kind
	Source                string
	ResourceTypeHint      string
	Environment           string
	Runtimes              []Runtime
	TelemetrySignals      []TelemetrySignal
	ObservabilityBackends []ObservabilityBackend
	DeploymentModes       []DeploymentMode
	VendorFeatures        []string
	ParametersJSON        []byte
}

func (r Requirement) Validate() error {
	if !requirementKeyPattern.MatchString(r.Key) {
		return fmt.Errorf("invalid requirement key %q", r.Key)
	}
	if _, ok := kindToProto[r.Kind]; !ok {
		return fmt.Errorf("invalid requirement kind %q", r.Kind)
	}
	for _, runtime := range r.Runtimes {
		if _, ok := runtimeToProto[runtime]; !ok {
			return fmt.Errorf("invalid requirement runtime %q", runtime)
		}
	}
	for _, signal := range r.TelemetrySignals {
		if _, ok := signalToProto[signal]; !ok {
			return fmt.Errorf("invalid telemetry signal %q", signal)
		}
	}
	for _, backend := range r.ObservabilityBackends {
		if _, ok := backendToProto[backend]; !ok {
			return fmt.Errorf("invalid observability backend %q", backend)
		}
	}
	for _, mode := range r.DeploymentModes {
		if _, ok := deploymentModeToProto[mode]; !ok {
			return fmt.Errorf("invalid deployment mode %q", mode)
		}
	}
	if len(r.ParametersJSON) > 0 && !json.Valid(r.ParametersJSON) {
		return fmt.Errorf("parameters_json is not valid JSON")
	}
	return nil
}

func (r Requirement) ToProto() (*pb.IaCRequirement, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &pb.IaCRequirement{
		Key:                   r.Key,
		Kind:                  kindToProto[r.Kind],
		Source:                r.Source,
		ResourceTypeHint:      r.ResourceTypeHint,
		Environment:           r.Environment,
		Runtimes:              mapSlice(r.Runtimes, runtimeToProto),
		TelemetrySignals:      mapSlice(r.TelemetrySignals, signalToProto),
		ObservabilityBackends: mapSlice(r.ObservabilityBackends, backendToProto),
		DeploymentModes:       mapSlice(r.DeploymentModes, deploymentModeToProto),
		VendorFeatures:        append([]string(nil), r.VendorFeatures...),
		ParametersJson:        append([]byte(nil), r.ParametersJSON...),
	}, nil
}

func FromProto(in *pb.IaCRequirement) (Requirement, error) {
	if in == nil {
		return Requirement{}, fmt.Errorf("iac requirement proto is nil")
	}
	out := Requirement{
		Key:                   in.GetKey(),
		Kind:                  reverse(kindToProto, in.GetKind()),
		Source:                in.GetSource(),
		ResourceTypeHint:      in.GetResourceTypeHint(),
		Environment:           in.GetEnvironment(),
		Runtimes:              reverseSlice(runtimeToProto, in.GetRuntimes()),
		TelemetrySignals:      reverseSlice(signalToProto, in.GetTelemetrySignals()),
		ObservabilityBackends: reverseSlice(backendToProto, in.GetObservabilityBackends()),
		DeploymentModes:       reverseSlice(deploymentModeToProto, in.GetDeploymentModes()),
		VendorFeatures:        append([]string(nil), in.GetVendorFeatures()...),
		ParametersJSON:        append([]byte(nil), in.GetParametersJson()...),
	}
	if err := out.Validate(); err != nil {
		return Requirement{}, err
	}
	return out, nil
}

func mapSlice[K comparable, V any](in []K, table map[K]V) []V {
	out := make([]V, 0, len(in))
	for _, item := range in {
		out = append(out, table[item])
	}
	return out
}

func reverse[K comparable, V comparable](table map[K]V, value V) K {
	for k, v := range table {
		if v == value {
			return k
		}
	}
	var zero K
	return zero
}

func reverseSlice[K comparable, V comparable](table map[K]V, in []V) []K {
	out := make([]K, 0, len(in))
	for _, item := range in {
		out = append(out, reverse(table, item))
	}
	return out
}
