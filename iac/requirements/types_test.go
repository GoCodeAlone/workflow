package requirements

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestRequirementValidation(t *testing.T) {
	req := Requirement{
		Key:                   "observability.telemetry.default",
		Kind:                  KindObservability,
		Runtimes:              []Runtime{RuntimeKubernetes, RuntimeDigitalOceanAppPlatform},
		TelemetrySignals:      []TelemetrySignal{TelemetrySignalTraces, TelemetrySignalMetrics, TelemetrySignalLogs},
		ObservabilityBackends: []ObservabilityBackend{ObservabilityBackendOTel, ObservabilityBackendDatadog},
		DeploymentModes:       []DeploymentMode{DeploymentModeSidecar, DeploymentModeSiblingService},
		VendorFeatures:        []string{"datadog.apm"},
		ResourceTypeHint:      "infra.container_service",
		ParametersJSON:        []byte(`{"collector":"otel"}`),
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRequirementValidationRejectsInvalidKey(t *testing.T) {
	req := Requirement{Key: "CMS telemetry", Kind: KindObservability}
	if err := req.Validate(); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestRequirementValidationRejectsUnknownEnum(t *testing.T) {
	req := Requirement{Key: "observability.telemetry.default", Kind: Kind("telemetryish")}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected invalid kind error")
	}
	if got := err.Error(); got != `invalid requirement kind "telemetryish"` {
		t.Fatalf("error = %q", got)
	}
}

func TestRequirementProtoRoundTrip(t *testing.T) {
	req := Requirement{
		Key:                   "observability.telemetry.default",
		Kind:                  KindObservability,
		Source:                "observability.telemetry",
		ResourceTypeHint:      "infra.container_service",
		Environment:           "production",
		Runtimes:              []Runtime{RuntimeECS},
		TelemetrySignals:      []TelemetrySignal{TelemetrySignalTraces},
		ObservabilityBackends: []ObservabilityBackend{ObservabilityBackendOTel},
		DeploymentModes:       []DeploymentMode{DeploymentModeSidecar},
		VendorFeatures:        []string{"datadog.apm"},
		ParametersJSON:        []byte(`{"sample":true}`),
	}
	proto, err := req.ToProto()
	if err != nil {
		t.Fatalf("ToProto: %v", err)
	}
	if proto.Kind != pb.RequirementKind_REQUIREMENT_KIND_OBSERVABILITY {
		t.Fatalf("proto kind = %v", proto.Kind)
	}
	round, err := FromProto(proto)
	if err != nil {
		t.Fatalf("FromProto: %v", err)
	}
	if round.Key != req.Key || round.Kind != req.Kind || round.Runtimes[0] != RuntimeECS {
		t.Fatalf("round trip = %+v, want %+v", round, req)
	}
}
