package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/config/yamledit"
	"github.com/GoCodeAlone/workflow/iac/derive"
	"github.com/GoCodeAlone/workflow/iac/requirements"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"gopkg.in/yaml.v3"
)

var infraDeriveMapperFactory = defaultInfraDeriveMapperFactory

func runInfraDerive(args []string) error {
	fs := flag.NewFlagSet("infra derive", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: wfctl infra derive --config workflow.yaml [options]

Derive provider-specific IaC modules from Workflow requirements.

Options:
  --config <file>       Config file
  --provider <name>     IaC provider mapper to use
  --runtime <runtime>   Target runtime
  --env <name>          Environment name
  --dry-run             Print expanded YAML without mutating the config file
  --write               Update the config file in place
  --non-interactive     Fail instead of prompting for ambiguous choices
  --format yaml         Output format
`)
	}
	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var provider string
	fs.StringVar(&provider, "provider", "", "IaC provider mapper to use")
	var runtimeFlag string
	fs.StringVar(&runtimeFlag, "runtime", "", "Target runtime (kubernetes, ecs, cloud_run, azure_container_apps, digitalocean_app_platform)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name")
	var write bool
	fs.BoolVar(&write, "write", false, "Update the config file in place")
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "Print expanded YAML without mutating the config file")
	var nonInteractive bool
	fs.BoolVar(&nonInteractive, "non-interactive", false, "Fail instead of prompting when derivation choices are ambiguous")
	var format string
	fs.StringVar(&format, "format", "yaml", "Output format (yaml)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if write && dryRun {
		return fmt.Errorf("--write and --dry-run are mutually exclusive")
	}
	if format != "yaml" {
		return fmt.Errorf("unsupported derive output format %q", format)
	}
	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}
	runtime, err := parseDeriveRuntime(runtimeFlag)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load %s: %w", cfgFile, err)
	}
	resolvedProvider, err := resolveDeriveProviderInteractive(cfg, provider, envName, nonInteractive)
	if err != nil {
		return err
	}
	providerCfg := providerConfigForType(cfg, resolvedProvider, envName)
	mapper, closeMapper, err := infraDeriveMapperFactory(context.Background(), resolvedProvider, providerCfg)
	if err != nil {
		return err
	}
	if closeMapper != nil {
		defer closeMapper()
	}
	result, err := derive.Derive(context.Background(), cfg, nil, mapper, derive.Options{
		Provider:       resolvedProvider,
		Runtime:        runtime,
		Environment:    envName,
		NonInteractive: nonInteractive,
	})
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", cfgFile, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", cfgFile, err)
	}
	changed, err := yamledit.AppendGeneratedModules(&doc, yamleditModules(result.Modules))
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("No derived IaC changes")
		return nil
	}
	out, err := encodeYAMLDoc(&doc)
	if err != nil {
		return err
	}
	if !write {
		_, err = os.Stdout.Write(out)
		return err
	}
	if err := writeFileAtomic(cfgFile, out, 0o600); err != nil {
		return err
	}
	fmt.Printf("Updated %s with %d derived module(s)\n", cfgFile, len(result.Modules))
	return nil
}

func defaultInfraDeriveMapperFactory(ctx context.Context, provider string, providerCfg map[string]any) (derive.ProviderMapper, func(), error) {
	if provider == "" {
		return nil, nil, fmt.Errorf("derive requires --provider or exactly one iac.provider module")
	}
	loaded, closer, err := resolveIaCProvider(ctx, provider, providerCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("load provider %q: %w", provider, err)
	}
	adapter, ok := loaded.(*typedIaCAdapter)
	if !ok {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, fmt.Errorf("provider %q does not expose typed IaC services", provider)
	}
	client := adapter.RequirementMapper()
	if client == nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, fmt.Errorf("provider %q does not support IaC requirement mapping", provider)
	}
	var closeFn func()
	if closer != nil {
		closeFn = func() { _ = closer.Close() }
	}
	return derive.ExternalProviderMapper{Client: client}, closeFn, nil
}

func resolveDeriveProviderInteractive(cfg *config.WorkflowConfig, provider, envName string, nonInteractive bool) (string, error) {
	if provider != "" {
		return provider, nil
	}
	choices := derive.ProviderChoices(cfg, envName)
	switch len(choices) {
	case 0:
		return "", nil
	case 1:
		return choices[0], nil
	}
	if nonInteractive || !stdinIsTerminal() {
		return "", fmt.Errorf("multiple iac providers available %v; pass --provider", choices)
	}
	fmt.Fprintln(os.Stderr, "Select IaC provider for derived modules:")
	for i, choice := range choices {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, choice)
	}
	fmt.Fprint(os.Stderr, "Provider: ")
	var selected int
	if _, err := fmt.Fscan(os.Stdin, &selected); err != nil {
		return "", fmt.Errorf("read provider selection: %w", err)
	}
	if selected < 1 || selected > len(choices) {
		return "", fmt.Errorf("invalid provider selection %d", selected)
	}
	return choices[selected-1], nil
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (a *typedIaCAdapter) RequirementMapper() pb.IaCProviderRequirementMapperClient {
	if a == nil {
		return nil
	}
	return a.reqMapper
}

func parseDeriveRuntime(raw string) (requirements.Runtime, error) {
	switch raw {
	case "":
		return "", nil
	case "kubernetes":
		return requirements.RuntimeKubernetes, nil
	case "ecs":
		return requirements.RuntimeECS, nil
	case "cloud_run", "cloud-run":
		return requirements.RuntimeCloudRun, nil
	case "azure_container_apps", "azure-container-apps":
		return requirements.RuntimeAzureContainerApps, nil
	case "digitalocean_app_platform", "do-app-platform", "digitalocean-app-platform":
		return requirements.RuntimeDigitalOceanAppPlatform, nil
	default:
		return "", fmt.Errorf("unsupported runtime %q", raw)
	}
}

func providerConfigForType(cfg *config.WorkflowConfig, provider, envName string) map[string]any {
	if cfg == nil || provider == "" {
		return nil
	}
	for i := range cfg.Modules {
		mod := &cfg.Modules[i]
		if mod.Type != "iac.provider" {
			continue
		}
		modCfg := mod.Config
		if envName != "" {
			if resolved, ok := mod.ResolveForEnv(envName); ok {
				modCfg = resolved.Config
			}
		}
		expanded := config.ExpandEnvInMap(modCfg)
		if got, _ := expanded["provider"].(string); got == provider {
			return expanded
		}
	}
	return nil
}

func yamleditModules(modules []derive.GeneratedModule) []yamledit.GeneratedModule {
	out := make([]yamledit.GeneratedModule, 0, len(modules))
	for i := range modules {
		out = append(out, yamledit.GeneratedModule{
			Name:      modules[i].Name,
			Type:      modules[i].Type,
			Satisfies: append([]string(nil), modules[i].Satisfies...),
			Config:    cloneAnyMap(modules[i].Config),
			DependsOn: append([]string(nil), modules[i].DependsOn...),
		})
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func encodeYAMLDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close YAML encoder: %w", err)
	}
	return buf.Bytes(), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
