package requirements

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
)

type Input struct {
	Config      *config.WorkflowConfig
	Manifests   map[string]*config.PluginManifestFile
	Providers   []Provider
	Environment string
}

type Provider interface {
	IaCRequirements(context.Context, Input) ([]Requirement, error)
}

type ProviderFunc func(context.Context, Input) ([]Requirement, error)

func (f ProviderFunc) IaCRequirements(ctx context.Context, input Input) ([]Requirement, error) {
	return f(ctx, input)
}

func Discover(ctx context.Context, input Input) ([]Requirement, error) {
	var out []Requirement
	seen := make(map[string]struct{})

	add := func(req Requirement) error {
		if req.Environment == "" {
			req.Environment = input.Environment
		}
		if err := req.Validate(); err != nil {
			return err
		}
		if _, ok := seen[req.Key]; ok {
			return nil
		}
		seen[req.Key] = struct{}{}
		out = append(out, req)
		return nil
	}

	builtIns := discoverBuiltIns(input.Config)
	for i := range builtIns {
		req := builtIns[i]
		if err := add(req); err != nil {
			return nil, err
		}
	}
	manifestReqs, err := discoverManifestRequirements(input.Config, input.Manifests)
	if err != nil {
		return nil, err
	}
	for i := range manifestReqs {
		req := manifestReqs[i]
		if err := add(req); err != nil {
			return nil, err
		}
	}
	for _, provider := range input.Providers {
		reqs, err := provider.IaCRequirements(ctx, input)
		if err != nil {
			return nil, err
		}
		for i := range reqs {
			req := reqs[i]
			if err := add(req); err != nil {
				return nil, err
			}
		}
	}

	satisfied := satisfiedKeys(input.Config)
	if len(satisfied) == 0 {
		return out, nil
	}
	filtered := out[:0]
	for i := range out {
		req := out[i]
		if _, ok := satisfied[req.Key]; ok {
			continue
		}
		filtered = append(filtered, req)
	}
	return filtered, nil
}

func DiscoverManifestRequirements(cfg *config.WorkflowConfig, manifests map[string]*config.PluginManifestFile) ([]Requirement, error) {
	reqs, err := discoverManifestRequirements(cfg, manifests)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]Requirement, 0, len(reqs))
	for i := range reqs {
		req := reqs[i]
		if err := req.Validate(); err != nil {
			return nil, err
		}
		if _, ok := seen[req.Key]; ok {
			continue
		}
		seen[req.Key] = struct{}{}
		out = append(out, req)
	}
	satisfied := satisfiedKeys(cfg)
	if len(satisfied) == 0 {
		return out, nil
	}
	filtered := out[:0]
	for i := range out {
		req := out[i]
		if _, ok := satisfied[req.Key]; ok {
			continue
		}
		filtered = append(filtered, req)
	}
	return filtered, nil
}

func discoverBuiltIns(cfg *config.WorkflowConfig) []Requirement {
	if cfg == nil {
		return nil
	}
	var out []Requirement
	for _, name := range sortedServiceNames(cfg.Services) {
		svc := cfg.Services[name]
		if svc == nil {
			continue
		}
		if svc.Binary != "" || len(svc.Expose) > 0 {
			out = append(out, Requirement{
				Key:              "web.api." + name,
				Kind:             KindWebAPI,
				Source:           "services." + name,
				ResourceTypeHint: "infra.container_service",
			})
		}
	}
	if cfg.Mesh != nil && (cfg.Mesh.Transport == "nats" || cfg.Mesh.NATS != nil) {
		out = append(out, Requirement{
			Key:              "messaging.nats.default",
			Kind:             KindMessageBroker,
			Source:           "mesh",
			ResourceTypeHint: "infra.message_broker",
			VendorFeatures:   []string{"nats"},
		})
	}
	return out
}

func discoverManifestRequirements(cfg *config.WorkflowConfig, manifests map[string]*config.PluginManifestFile) ([]Requirement, error) {
	if cfg == nil || len(manifests) == 0 {
		return nil, nil
	}
	var out []Requirement
	for _, mod := range allModules(cfg) {
		for _, manifestName := range sortedManifestNames(manifests) {
			manifest := manifests[manifestName]
			if manifest == nil || manifest.ModuleInfraRequirementsV2 == nil {
				continue
			}
			spec := manifest.ModuleInfraRequirementsV2[mod.Type]
			if spec == nil {
				continue
			}
			for i := range spec.Requires {
				raw := spec.Requires[i]
				req, err := FromManifestRequirement(raw)
				if err != nil {
					return nil, fmt.Errorf("module %q requirement %q: %w", mod.Type, raw.Key, err)
				}
				if req.Source == "" {
					req.Source = mod.Type
				}
				out = append(out, req)
			}
		}
	}
	return out, nil
}

func FromManifestRequirement(raw config.ModuleInfraRequirementV2) (Requirement, error) {
	var params []byte
	if len(raw.Parameters) > 0 {
		encoded, err := json.Marshal(raw.Parameters)
		if err != nil {
			return Requirement{}, fmt.Errorf("marshal requirement parameters: %w", err)
		}
		params = encoded
	}
	req := Requirement{
		Key:                   raw.Key,
		Kind:                  Kind(raw.Kind),
		Source:                raw.Source,
		ResourceTypeHint:      raw.ResourceTypeHint,
		Environment:           raw.Environment,
		Runtimes:              castSlice[Runtime](raw.Runtimes),
		TelemetrySignals:      castSlice[TelemetrySignal](raw.TelemetrySignals),
		ObservabilityBackends: castSlice[ObservabilityBackend](raw.ObservabilityBackends),
		DeploymentModes:       castSlice[DeploymentMode](raw.DeploymentModes),
		VendorFeatures:        append([]string(nil), raw.VendorFeatures...),
		ParametersJSON:        params,
	}
	if err := req.Validate(); err != nil {
		return Requirement{}, err
	}
	return req, nil
}

func allModules(cfg *config.WorkflowConfig) []config.ModuleConfig {
	if cfg == nil {
		return nil
	}
	out := append([]config.ModuleConfig(nil), cfg.Modules...)
	for _, name := range sortedServiceNames(cfg.Services) {
		svc := cfg.Services[name]
		if svc != nil {
			out = append(out, svc.Modules...)
		}
	}
	return out
}

func satisfiedKeys(cfg *config.WorkflowConfig) map[string]struct{} {
	out := make(map[string]struct{})
	for _, mod := range allModules(cfg) {
		for _, key := range mod.Satisfies {
			out[key] = struct{}{}
		}
	}
	return out
}

func castSlice[T ~string](in []string) []T {
	out := make([]T, 0, len(in))
	for _, item := range in {
		out = append(out, T(item))
	}
	return out
}

func sortedServiceNames(services map[string]*config.ServiceConfig) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedManifestNames(manifests map[string]*config.PluginManifestFile) []string {
	names := make([]string, 0, len(manifests))
	for name := range manifests {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
