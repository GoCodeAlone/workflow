package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

const logCaptureFollowCompletionGrace = 30 * time.Second

func runLogs(args []string) error {
	return runLogsWithOutput(args, os.Stdout)
}

func runLogsWithOutput(args []string, out io.Writer) error {
	if len(args) < 1 {
		return logsUsage()
	}
	switch args[0] {
	case "capture":
		return runLogsCapture(args[1:], out)
	default:
		return logsUsage()
	}
}

func runInfraLogs(args []string) error {
	if len(args) > 0 && args[0] == "capture" {
		args = args[1:]
	}
	return runLogsCapture(args, os.Stdout)
}

func logsUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl logs <action> [options]

Actions:
  capture    Capture provider logs for an infrastructure resource

Options:
  --config <file>       Config file (default: infra.yaml or config/infra.yaml)
  --env <name>          Environment name for provider config resolution
  --resource <name>     infra.container_service resource name
  --component <name>    Provider component name (for example App Platform service)
  --type <type>         Log type: BUILD, DEPLOY, RUN, RUN_RESTARTED (default RUN)
  --tail <n>            Tail line count (default 300)
  --follow              Follow live logs until --duration expires
  --duration <d>        Max follow duration (default 2m)
  --deployment <id>     Provider deployment ID when supported
  --plugin-dir <dir>    External plugin directory
`)
	return fmt.Errorf("missing or unknown logs action")
}

func runLogsCapture(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("logs capture", flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	var configFile, envName, resourceName, componentName, logType, deploymentID, pluginDir string
	var tailLines int
	var follow bool
	var duration time.Duration
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file")
	fs.StringVar(&envName, "env", "", "Environment name")
	fs.StringVar(&resourceName, "resource", "", "infra.container_service resource name")
	fs.StringVar(&componentName, "component", "", "Provider component name")
	fs.StringVar(&logType, "type", "RUN", "Log type")
	fs.IntVar(&tailLines, "tail", 300, "Tail line count")
	fs.BoolVar(&follow, "follow", false, "Follow live logs")
	fs.DurationVar(&duration, "duration", 2*time.Minute, "Max follow duration")
	fs.StringVar(&deploymentID, "deployment", "", "Provider deployment ID")
	fs.StringVar(&pluginDir, "plugin-dir", "", "External plugin directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if resourceName == "" {
		return fmt.Errorf("logs capture: --resource is required")
	}
	normalizedLogType, err := normalizeLogCaptureType(logType)
	if err != nil {
		return err
	}
	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	spec, providerRef, err := resolveLogCaptureResource(cfg, envName, resourceName)
	if err != nil {
		return err
	}
	providerDefs, _, disabled := resolveProviderDefs(cfg, envName)
	if _, ok := disabled[providerRef]; ok {
		return fmt.Errorf("logs capture: provider %q is disabled for environment %q", providerRef, envName)
	}
	def, ok := providerDefs[providerRef]
	if !ok || def.provType == "" {
		return fmt.Errorf("logs capture: resource %q references unknown iac.provider %q", resourceName, providerRef)
	}

	prevPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDir
	defer func() { currentInfraPluginDir = prevPluginDir }()

	ctx := context.Background()
	if follow && duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration+logCaptureFollowCompletionGrace)
		defer cancel()
	}
	durationSeconds := int64(0)
	if follow {
		durationSeconds = int64(duration / time.Second)
	}
	provider, closer, err := resolveIaCProvider(ctx, def.provType, def.provCfg)
	if err != nil {
		return fmt.Errorf("load provider %q: %w", def.provType, err)
	}
	if closer != nil {
		defer closer.Close()
	}
	capturer, ok := provider.(interfaces.LogCaptureProvider)
	if !ok {
		return fmt.Errorf("provider %q does not support log capture", def.provType)
	}
	req := interfaces.LogCaptureRequest{
		ResourceName:    logCaptureResourceCloudName(spec),
		ResourceType:    spec.Type,
		ProviderID:      logCaptureString(spec.Config["provider_id"]),
		ComponentName:   componentName,
		LogType:         normalizedLogType,
		TailLines:       tailLines,
		Follow:          follow,
		DurationSeconds: durationSeconds,
		DeploymentID:    deploymentID,
	}
	return capturer.CaptureLogs(ctx, req, writerLogSink{out: out})
}

func normalizeLogCaptureType(s string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "RUN":
		return "RUN", nil
	case "BUILD":
		return "BUILD", nil
	case "DEPLOY":
		return "DEPLOY", nil
	case "RUN_RESTARTED":
		return "RUN_RESTARTED", nil
	default:
		return "", fmt.Errorf("logs capture: unsupported --type %q (want BUILD, DEPLOY, RUN, or RUN_RESTARTED)", s)
	}
}

func resolveLogCaptureResource(cfg *config.WorkflowConfig, envName, name string) (interfaces.ResourceSpec, string, error) {
	for i := range cfg.Modules {
		m := cfg.Modules[i]
		if m.Name != name {
			continue
		}
		resolvedName := m.Name
		resolved := m.Config
		if envName != "" {
			envResolved, ok := m.ResolveForEnv(envName)
			if !ok {
				return interfaces.ResourceSpec{}, "", fmt.Errorf("logs capture: resource %q is disabled for environment %q", name, envName)
			}
			resolvedName = envResolved.Name
			resolved = envResolved.Config
		}
		cfgMap := config.ExpandEnvInMapPreservingKeys(resolved, infraPreserveKeys)
		providerRef := resolveIaCProviderRef(cfgMap)
		if providerRef == "" {
			return interfaces.ResourceSpec{}, "", fmt.Errorf("logs capture: resource %q missing iac_provider/provider", name)
		}
		return interfaces.ResourceSpec{Name: resolvedName, Type: m.Type, Config: cfgMap}, providerRef, nil
	}
	return interfaces.ResourceSpec{}, "", fmt.Errorf("logs capture: resource %q not found", name)
}

func logCaptureResourceCloudName(spec interfaces.ResourceSpec) string {
	for _, key := range []string{"app_name", "name"} {
		if v := logCaptureString(spec.Config[key]); v != "" {
			return v
		}
	}
	return spec.Name
}

func logCaptureString(v any) string {
	s, _ := v.(string)
	return s
}

type writerLogSink struct {
	out io.Writer
}

func (s writerLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	if len(chunk.Data) == 0 {
		return nil
	}
	_, err := s.out.Write(chunk.Data)
	return err
}
