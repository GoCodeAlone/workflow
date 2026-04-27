package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pluginAuditResult struct {
	RepoPath          string                 `json:"repoPath"`
	Name              string                 `json:"name"`
	HasGoMod          bool                   `json:"hasGoMod"`
	HasPluginJSON     bool                   `json:"hasPluginJSON"`
	ManifestShape     string                 `json:"manifestShape"`
	ManifestName      string                 `json:"manifestName"`
	ManifestVersion   string                 `json:"manifestVersion"`
	ContractCoverage  pluginContractCoverage `json:"contractCoverage,omitempty"`
	ContractFile      string                 `json:"contractFile,omitempty"`
	ContractFileFound bool                   `json:"contractFileFound"`
	Findings          []planFinding          `json:"findings"`
}

func auditPluginRepo(path string) pluginAuditResult {
	return auditPluginRepoWithOptions(path, pluginAuditOptions{})
}

type pluginAuditOptions struct {
	StrictContracts bool
}

type pluginContractCoverage struct {
	Modules        pluginContractKindCoverage `json:"modules"`
	Steps          pluginContractKindCoverage `json:"steps"`
	Triggers       pluginContractKindCoverage `json:"triggers"`
	ServiceMethods pluginContractKindCoverage `json:"serviceMethods"`
}

type pluginContractKindCoverage struct {
	Total   int `json:"total"`
	Strict  int `json:"strict"`
	Legacy  int `json:"legacy"`
	Missing int `json:"missing"`
}

type pluginContractDescriptorFile struct {
	Version          string                     `json:"version"`
	DescriptorSetRef string                     `json:"descriptorSetRef,omitempty"`
	Contracts        []pluginContractDescriptor `json:"contracts"`
}

type pluginContractDescriptor struct {
	Kind             string `json:"kind"`
	Type             string `json:"type"`
	Mode             string `json:"mode"`
	Config           string `json:"config,omitempty"`
	Input            string `json:"input,omitempty"`
	Output           string `json:"output,omitempty"`
	ModuleType       string `json:"moduleType,omitempty"`
	StepType         string `json:"stepType,omitempty"`
	TriggerType      string `json:"triggerType,omitempty"`
	ServiceName      string `json:"serviceName,omitempty"`
	Method           string `json:"method,omitempty"`
	DescriptorSetRef string `json:"descriptorSetRef,omitempty"`
}

func (d *pluginContractDescriptor) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.Kind = firstStringField(raw, "kind")
	d.Type = firstStringField(raw, "type")
	d.Mode = firstStringField(raw, "mode")
	d.Config = firstStringField(raw, "config", "configMessage", "config_message")
	d.Input = firstStringField(raw, "input", "inputMessage", "input_message")
	d.Output = firstStringField(raw, "output", "outputMessage", "output_message")
	d.ModuleType = firstStringField(raw, "moduleType", "module_type")
	d.StepType = firstStringField(raw, "stepType", "step_type")
	d.TriggerType = firstStringField(raw, "triggerType", "trigger_type")
	d.ServiceName = firstStringField(raw, "serviceName", "service_name")
	d.Method = firstStringField(raw, "method")
	d.DescriptorSetRef = firstStringField(raw, "descriptorSetRef", "descriptor_set_ref")
	return nil
}

type pluginAdvertisedContracts struct {
	Modules        []string
	Steps          []string
	Triggers       []string
	ServiceMethods []string
}

func auditPluginRepoWithOptions(path string, opts pluginAuditOptions) pluginAuditResult {
	result := pluginAuditResult{
		RepoPath:        path,
		Name:            filepath.Base(path),
		HasGoMod:        pluginAuditFileExists(filepath.Join(path, "go.mod")),
		HasPluginJSON:   pluginAuditFileExists(filepath.Join(path, "plugin.json")),
		ManifestShape:   "missing",
		ManifestName:    "",
		ManifestVersion: "",
	}
	if !result.HasPluginJSON {
		if result.HasGoMod {
			result.Findings = append(result.Findings, planFinding{
				Path:    filepath.Join(path, "plugin.json"),
				Level:   "ERROR",
				Code:    "missing_plugin_manifest",
				Message: "workflow plugin repo has no plugin.json",
			})
		}
		return result
	}

	data, err := os.ReadFile(filepath.Join(path, "plugin.json"))
	if err != nil {
		result.ManifestShape = "unreadable"
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(path, "plugin.json"),
			Level:   "ERROR",
			Code:    "read_plugin_manifest",
			Message: err.Error(),
		})
		return result
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		result.ManifestShape = "invalid-json"
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(path, "plugin.json"),
			Level:   "ERROR",
			Code:    "invalid_plugin_manifest_json",
			Message: err.Error(),
		})
		return result
	}

	result.ManifestName = stringFromAny(manifest["name"])
	result.ManifestVersion = stringFromAny(manifest["version"])
	result.ManifestShape = classifyPluginManifestShape(manifest)
	addPluginManifestFindings(&result)
	addPluginContractFindings(&result, manifest, opts)
	return result
}

func auditPluginRepos(root string) ([]pluginAuditResult, error) {
	return auditPluginReposWithOptions(root, pluginAuditOptions{})
}

func auditPluginReposWithOptions(root string, opts pluginAuditOptions) ([]pluginAuditResult, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	results := make([]pluginAuditResult, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "workflow-plugin-") {
			continue
		}
		repoPath := filepath.Join(root, entry.Name())
		if !pluginAuditFileExists(filepath.Join(repoPath, "go.mod")) && !pluginAuditFileExists(filepath.Join(repoPath, "plugin.json")) {
			continue
		}
		results = append(results, auditPluginRepoWithOptions(repoPath, opts))
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func classifyPluginManifestShape(manifest map[string]any) string {
	if _, ok := manifest["moduleTypes"]; ok {
		return "top-level-types"
	}
	if _, ok := manifest["stepTypes"]; ok {
		return "top-level-types"
	}
	if capabilities, ok := manifest["capabilities"]; ok {
		switch capabilities.(type) {
		case []any:
			return "capabilities-array"
		case map[string]any:
			return "canonical"
		}
	}
	if stringFromAny(manifest["type"]) == "iac_provider" {
		if _, ok := manifest["resources"]; ok {
			return "provider-resources"
		}
	}
	return "unknown"
}

func addPluginManifestFindings(result *pluginAuditResult) {
	switch result.ManifestShape {
	case "canonical":
	case "top-level-types", "capabilities-array", "provider-resources":
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "WARN",
			Code:    "legacy_plugin_manifest",
			Message: fmt.Sprintf("plugin manifest uses %s shape", result.ManifestShape),
		})
	default:
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "WARN",
			Code:    "unknown_plugin_manifest_shape",
			Message: "plugin manifest shape is not recognized",
		})
	}

	if result.ManifestName == "workflow-plugin-TEMPLATE" {
		result.Findings = append(result.Findings, planFinding{
			Path:    filepath.Join(result.RepoPath, "plugin.json"),
			Level:   "ERROR",
			Code:    "placeholder_plugin_identity",
			Message: fmt.Sprintf("plugin manifest name %q appears to be a placeholder", result.ManifestName),
		})
	}
}

func addPluginContractFindings(result *pluginAuditResult, manifest map[string]any, opts pluginAuditOptions) {
	advertised := advertisedPluginContracts(manifest)
	descriptors, contractPath, found, findings := loadPluginContractDescriptors(result.RepoPath, manifest, opts)
	result.ContractFile = contractPath
	result.ContractFileFound = found
	result.Findings = append(result.Findings, findings...)

	byKindType := make(map[string]pluginContractDescriptor)
	for i := range descriptors {
		descriptor := descriptors[i]
		kind := normalizePluginContractKind(descriptor.Kind)
		typ := strings.TrimSpace(descriptor.contractType(kind))
		if kind == "" || typ == "" {
			continue
		}
		descriptor.Kind = kind
		descriptor.Mode = normalizePluginContractMode(descriptor.Mode)
		byKindType[kind+"\x00"+typ] = descriptor
	}

	result.ContractCoverage.Modules = addPluginContractKindFindings(result, "module", advertised.Modules, byKindType, opts)
	result.ContractCoverage.Steps = addPluginContractKindFindings(result, "step", advertised.Steps, byKindType, opts)
	result.ContractCoverage.Triggers = addPluginContractKindFindings(result, "trigger", advertised.Triggers, byKindType, opts)
	result.ContractCoverage.ServiceMethods = addPluginContractKindFindings(result, "service_method", advertised.ServiceMethods, byKindType, opts)
}

func loadPluginContractDescriptors(repoPath string, manifest map[string]any, opts pluginAuditOptions) ([]pluginContractDescriptor, string, bool, []planFinding) {
	var descriptors []pluginContractDescriptor
	var findings []planFinding
	if raw, ok := manifest["contracts"]; ok {
		inline, err := parsePluginContractDescriptors(raw)
		if err != nil {
			findings = append(findings, planFinding{
				Path:    filepath.Join(repoPath, "plugin.json"),
				Level:   strictContractFindingLevel(opts),
				Code:    "invalid_plugin_contract_descriptors",
				Message: err.Error(),
			})
		} else {
			descriptors = append(descriptors, inline...)
		}
	}

	contractPath := filepath.Join(repoPath, "plugin.contracts.json")
	data, err := os.ReadFile(contractPath)
	if err == nil {
		var file pluginContractDescriptorFile
		if err := json.Unmarshal(data, &file); err != nil {
			findings = append(findings, planFinding{
				Path:    contractPath,
				Level:   strictContractFindingLevel(opts),
				Code:    "invalid_plugin_contract_descriptors",
				Message: err.Error(),
			})
			return descriptors, contractPath, true, findings
		}
		applyDescriptorSetRef(file.Contracts, file.DescriptorSetRef)
		descriptors = append(descriptors, file.Contracts...)
		return descriptors, contractPath, true, findings
	}
	if os.IsNotExist(err) {
		return descriptors, contractPath, false, findings
	}
	findings = append(findings, planFinding{
		Path:    contractPath,
		Level:   strictContractFindingLevel(opts),
		Code:    "read_plugin_contract_descriptors",
		Message: err.Error(),
	})
	return descriptors, contractPath, false, findings
}

func parsePluginContractDescriptors(raw any) ([]pluginContractDescriptor, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var descriptors []pluginContractDescriptor
	if err := json.Unmarshal(data, &descriptors); err == nil {
		return descriptors, nil
	}
	var file pluginContractDescriptorFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	applyDescriptorSetRef(file.Contracts, file.DescriptorSetRef)
	return file.Contracts, nil
}

func applyDescriptorSetRef(descriptors []pluginContractDescriptor, descriptorSetRef string) {
	if descriptorSetRef == "" {
		return
	}
	for i := range descriptors {
		if descriptors[i].DescriptorSetRef == "" {
			descriptors[i].DescriptorSetRef = descriptorSetRef
		}
	}
}

func addPluginContractKindFindings(result *pluginAuditResult, kind string, advertised []string, descriptors map[string]pluginContractDescriptor, opts pluginAuditOptions) pluginContractKindCoverage {
	coverage := pluginContractKindCoverage{Total: len(advertised)}
	for _, typ := range advertised {
		descriptor, ok := descriptors[kind+"\x00"+typ]
		if !ok {
			coverage.Missing++
			result.Findings = append(result.Findings, planFinding{
				Path:    filepath.Join(result.RepoPath, "plugin.json"),
				Level:   strictContractFindingLevel(opts),
				Code:    fmt.Sprintf("missing_%s_contract_descriptor", kind),
				Message: fmt.Sprintf("%s type %q has no strict contract descriptor", pluginContractKindLabel(kind), typ),
			})
			continue
		}
		switch descriptor.Mode {
		case "strict":
			coverage.Strict++
		default:
			coverage.Legacy++
			result.Findings = append(result.Findings, planFinding{
				Path:    result.ContractFile,
				Level:   strictContractFindingLevel(opts),
				Code:    fmt.Sprintf("legacy_%s_contract_descriptor", kind),
				Message: fmt.Sprintf("%s type %q uses legacy contract mode %q", pluginContractKindLabel(kind), typ, descriptor.Mode),
			})
		}
	}
	return coverage
}

func (d pluginContractDescriptor) contractType(kind string) string {
	if d.Type != "" {
		return d.Type
	}
	switch kind {
	case "module":
		return d.ModuleType
	case "step":
		return d.StepType
	case "trigger":
		return d.TriggerType
	case "service_method":
		if d.Type != "" {
			return d.Type
		}
		if d.ServiceName != "" && d.Method != "" {
			return d.ServiceName + "/" + d.Method
		}
		return d.Method
	default:
		return ""
	}
}

func advertisedPluginContracts(manifest map[string]any) pluginAdvertisedContracts {
	var advertised pluginAdvertisedContracts
	advertised.Modules = append(advertised.Modules, stringSliceFromAny(manifest["moduleTypes"])...)
	advertised.Steps = append(advertised.Steps, stringSliceFromAny(manifest["stepTypes"])...)
	advertised.Triggers = append(advertised.Triggers, stringSliceFromAny(manifest["triggerTypes"])...)
	advertised.ServiceMethods = append(advertised.ServiceMethods, stringSliceFromAny(manifest["serviceMethods"])...)

	if capabilities, ok := manifest["capabilities"].(map[string]any); ok {
		advertised.Modules = append(advertised.Modules, stringSliceFromAny(capabilities["moduleTypes"])...)
		advertised.Steps = append(advertised.Steps, stringSliceFromAny(capabilities["stepTypes"])...)
		advertised.Triggers = append(advertised.Triggers, stringSliceFromAny(capabilities["triggerTypes"])...)
		advertised.ServiceMethods = append(advertised.ServiceMethods, stringSliceFromAny(capabilities["serviceMethods"])...)
	}

	advertised.Modules = uniqueSortedStrings(advertised.Modules)
	advertised.Steps = uniqueSortedStrings(advertised.Steps)
	advertised.Triggers = uniqueSortedStrings(advertised.Triggers)
	advertised.ServiceMethods = uniqueSortedStrings(advertised.ServiceMethods)
	return advertised
}

func strictContractFindingLevel(opts pluginAuditOptions) string {
	if opts.StrictContracts {
		return "ERROR"
	}
	return "WARN"
}

func normalizePluginContractKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "module", "modules", "contract_kind_module":
		return "module"
	case "step", "steps", "contract_kind_step":
		return "step"
	case "trigger", "triggers", "contract_kind_trigger":
		return "trigger"
	case "service_method", "service-method", "servicemethod", "servicemethods", "service", "contract_kind_service":
		return "service_method"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func normalizePluginContractMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "strict", "strict_contract", "typed", "strict_proto", "contract_mode_strict_proto":
		return "strict"
	case "legacy", "legacy_struct", "legacy-struct", "struct", "contract_mode_legacy_struct":
		return "legacy_struct"
	case "proto_with_legacy_struct", "contract_mode_proto_with_legacy_struct":
		return "proto_with_legacy_struct"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func countPluginContractFindings(findings []planFinding) int {
	count := 0
	for _, finding := range findings {
		if finding.Level == "ERROR" && isPluginContractFinding(finding) {
			count++
		}
	}
	return count
}

func isPluginContractFinding(finding planFinding) bool {
	return strings.Contains(finding.Code, "contract_descriptor") ||
		finding.Code == "read_plugin_contract_descriptors" ||
		finding.Code == "invalid_plugin_contract_descriptors"
}

func pluginContractKindLabel(kind string) string {
	return strings.ReplaceAll(kind, "_", " ")
}

func pluginAuditFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(stringFromAny(item)); s != "" {
			values = append(values, s)
		}
	}
	return values
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}
