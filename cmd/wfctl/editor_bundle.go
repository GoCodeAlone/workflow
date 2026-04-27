package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/schema"
)

var (
	listEditorBundleRegistryPluginNames = ListPluginNames
	fetchEditorBundleRegistryManifest   = FetchManifest
)

func runEditorBundle(args []string) error {
	fs := flag.NewFlagSet("editor-bundle", flag.ExitOnError)
	output := fs.String("output", "", "Write bundle to file instead of stdout")
	format := fs.String("format", "json", "Output format: json")
	pluginDir := fs.String("plugin-dir", "", "Load plugin contract descriptors from a plugin repo or plugin root")
	includeRegistry := fs.Bool("registry", true, "Include contract descriptors from the configured plugin registry")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl editor-bundle [options]\n\nExport the canonical editor contract bundle.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "json" {
		return fmt.Errorf("unsupported format %q", *format)
	}

	sources, err := editorBundleContractSources(*pluginDir, *includeRegistry)
	if err != nil {
		return err
	}
	bundle, err := schema.ExportEditorBundle(schema.EditorBundleOptions{
		WorkflowVersion:    version,
		ContractRegistries: sources,
	})
	if err != nil {
		return err
	}

	if ref, refErr := loadEditorBundleDSLReference(); refErr == nil {
		bundle.DSLReference = ref
	}

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bundle); err != nil {
		return fmt.Errorf("encode editor bundle: %w", err)
	}
	if *output != "" {
		fmt.Fprintf(os.Stderr, "Editor bundle written to %s\n", *output)
	}
	return nil
}

func loadEditorBundleDSLReference() (*DSLReferenceOutput, error) {
	md, err := findDSLReferenceMarkdown()
	if err != nil {
		return nil, err
	}
	return parseDSLReference(string(md))
}

func editorBundleContractSources(pluginDir string, includeRegistry bool) ([]schema.EditorContractRegistrySource, error) {
	var sources []schema.EditorContractRegistrySource
	if pluginDir != "" {
		pluginSources, err := editorBundlePluginDirSources(pluginDir)
		if err != nil {
			return nil, err
		}
		sources = append(sources, pluginSources...)
	}
	if includeRegistry {
		registrySources, err := editorBundleRegistrySources()
		if err != nil {
			return nil, err
		}
		sources = append(sources, registrySources...)
	}
	return sources, nil
}

func editorBundlePluginDirSources(path string) ([]schema.EditorContractRegistrySource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat plugin dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin-dir must be a directory")
	}
	if pluginAuditFileExists(filepath.Join(path, "plugin.json")) || pluginAuditFileExists(filepath.Join(path, "plugin.contracts.json")) {
		source, err := editorBundlePluginRepoSource(path)
		if err != nil {
			return nil, err
		}
		if source.Registry == nil || len(source.Registry.Contracts) == 0 {
			return nil, nil
		}
		return []schema.EditorContractRegistrySource{source}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin root: %w", err)
	}
	var sources []schema.EditorContractRegistrySource
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoPath := filepath.Join(path, entry.Name())
		if !pluginAuditFileExists(filepath.Join(repoPath, "plugin.json")) && !pluginAuditFileExists(filepath.Join(repoPath, "plugin.contracts.json")) {
			continue
		}
		source, err := editorBundlePluginRepoSource(repoPath)
		if err != nil {
			return nil, err
		}
		if source.Registry != nil && len(source.Registry.Contracts) > 0 {
			sources = append(sources, source)
		}
	}
	return sources, nil
}

func editorBundlePluginRepoSource(path string) (schema.EditorContractRegistrySource, error) {
	manifest, err := readPluginManifestMap(filepath.Join(path, "plugin.json"))
	if err != nil && !os.IsNotExist(err) {
		return schema.EditorContractRegistrySource{}, err
	}
	pluginName := stringFromAny(manifest["name"])
	if pluginName == "" {
		pluginName = filepath.Base(path)
	}

	descriptors, _, _, findings := loadPluginContractDescriptors(path, manifest, pluginAuditOptions{})
	for _, finding := range findings {
		if finding.Level == "ERROR" {
			return schema.EditorContractRegistrySource{}, fmt.Errorf("%s: %s", finding.Code, finding.Message)
		}
	}
	return schema.EditorContractRegistrySource{
		Plugin:                    pluginName,
		Source:                    schema.EditorContractSourcePluginContractsJSON,
		DescriptorSetRef:          firstDescriptorSetRef(descriptors),
		ContractDescriptorSetRefs: contractDescriptorSetRefsFromPluginDescriptors(descriptors),
		Registry:                  contractRegistryFromPluginDescriptors(descriptors),
	}, nil
}

func readPluginManifestMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}, err
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	return manifest, nil
}

func editorBundleRegistrySources() ([]schema.EditorContractRegistrySource, error) {
	names, err := listEditorBundleRegistryPluginNames()
	if err != nil {
		return nil, err
	}
	var sources []schema.EditorContractRegistrySource
	for _, name := range names {
		manifest, err := fetchEditorBundleRegistryManifest(name)
		if err != nil || len(manifest.Contracts) == 0 {
			continue
		}
		sources = append(sources, schema.EditorContractRegistrySource{
			Plugin:                    manifest.Name,
			Source:                    schema.EditorContractSourcePluginManifest,
			DescriptorSetRef:          firstDescriptorSetRef(manifest.Contracts),
			ContractDescriptorSetRefs: contractDescriptorSetRefsFromPluginDescriptors(manifest.Contracts),
			Registry:                  contractRegistryFromPluginDescriptors(manifest.Contracts),
		})
	}
	return sources, nil
}

func contractRegistryFromPluginDescriptors(descriptors []pluginContractDescriptor) *pb.ContractRegistry {
	registry := &pb.ContractRegistry{}
	for _, descriptor := range descriptors {
		contract := contractDescriptorFromPluginDescriptor(descriptor)
		if contract != nil {
			registry.Contracts = append(registry.Contracts, contract)
		}
	}
	return registry
}

func firstDescriptorSetRef(descriptors []pluginContractDescriptor) string {
	for _, descriptor := range descriptors {
		if descriptor.DescriptorSetRef != "" {
			return descriptor.DescriptorSetRef
		}
	}
	return ""
}

func contractDescriptorSetRefsFromPluginDescriptors(descriptors []pluginContractDescriptor) map[string]string {
	refs := map[string]string{}
	for _, descriptor := range descriptors {
		if descriptor.DescriptorSetRef == "" {
			continue
		}
		id := editorBundleContractIDFromPluginDescriptor(descriptor)
		if id == "" {
			continue
		}
		refs[id] = descriptor.DescriptorSetRef
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

func editorBundleContractIDFromPluginDescriptor(descriptor pluginContractDescriptor) string {
	kind := normalizePluginContractKind(descriptor.Kind)
	switch kind {
	case "module":
		if typ := descriptor.contractType(kind); typ != "" {
			return "module:" + typ
		}
	case "step":
		if typ := descriptor.contractType(kind); typ != "" {
			return "step:" + typ
		}
	case "trigger":
		if typ := descriptor.contractType(kind); typ != "" {
			return "trigger:" + typ
		}
	case "service_method":
		serviceName := descriptor.ServiceName
		method := descriptor.Method
		if serviceName == "" && method == "" {
			if parsedService, parsedMethod, ok := strings.Cut(descriptor.contractType(kind), "/"); ok {
				serviceName = parsedService
				method = parsedMethod
			}
		}
		if serviceName != "" {
			return "service:" + serviceName + "/" + method
		}
		if method != "" {
			return "service:" + method
		}
	}
	return ""
}

func contractDescriptorFromPluginDescriptor(descriptor pluginContractDescriptor) *pb.ContractDescriptor {
	kind := normalizePluginContractKind(descriptor.Kind)
	mode := normalizePluginContractMode(descriptor.Mode)
	contract := &pb.ContractDescriptor{
		Mode:          pbContractMode(mode),
		ConfigMessage: descriptor.Config,
		InputMessage:  descriptor.Input,
		OutputMessage: descriptor.Output,
	}
	switch kind {
	case "module":
		contract.Kind = pb.ContractKind_CONTRACT_KIND_MODULE
		contract.ModuleType = descriptor.contractType(kind)
	case "step":
		contract.Kind = pb.ContractKind_CONTRACT_KIND_STEP
		contract.StepType = descriptor.contractType(kind)
	case "trigger":
		contract.Kind = pb.ContractKind_CONTRACT_KIND_TRIGGER
		contract.TriggerType = descriptor.contractType(kind)
	case "service_method":
		contract.Kind = pb.ContractKind_CONTRACT_KIND_SERVICE
		contract.ModuleType = descriptor.ModuleType
		contract.ServiceName = descriptor.ServiceName
		contract.Method = descriptor.Method
		if contract.ServiceName == "" && contract.Method == "" {
			serviceName, method, ok := strings.Cut(descriptor.contractType(kind), "/")
			if ok {
				contract.ServiceName = serviceName
				contract.Method = method
			}
		}
	default:
		return nil
	}
	return contract
}

func pbContractMode(mode string) pb.ContractMode {
	switch mode {
	case "strict":
		return pb.ContractMode_CONTRACT_MODE_STRICT_PROTO
	case "proto_with_legacy", "proto_with_legacy_struct":
		return pb.ContractMode_CONTRACT_MODE_PROTO_WITH_LEGACY_STRUCT
	case "legacy", "legacy_struct":
		return pb.ContractMode_CONTRACT_MODE_LEGACY_STRUCT
	default:
		return pb.ContractMode_CONTRACT_MODE_UNSPECIFIED
	}
}
