package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// runBuild is the top-level `wfctl build` dispatcher.
// Accepts subcommands (go, ui, image, push, custom) or runs all target types
// present in the config when invoked without a subcommand.
func runBuild(args []string) error {
	// Explicit subcommand dispatch.
	if len(args) > 0 && !isFlag(args[0]) {
		sub := args[0]
		rest := args[1:]
		switch sub {
		case "go":
			return runBuildGo(rest)
		case "ui":
			return runBuildUIPlugin(rest)
		case "image":
			return runBuildImage(rest)
		case "push":
			return runBuildPush(rest)
		case "custom":
			return runBuildCustom(rest)
		case "audit":
			return runBuildSecurityAudit(rest)
		default:
			return fmt.Errorf("unknown build subcommand %q — valid: go, ui, image, push, custom, audit", sub)
		}
	}

	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		cfgPath string
		dryRun  bool
		only    string
		skip    string
		tag     string
		format  string
		noPush  bool
		envName string
	)
	fs.StringVar(&cfgPath, "config", "workflow.yaml", "Path to workflow config file")
	fs.StringVar(&cfgPath, "c", "workflow.yaml", "Path to workflow config file (short)")
	fs.BoolVar(&dryRun, "dry-run", false, "Print planned actions without executing")
	fs.StringVar(&only, "only", "", "Build only targets matching this name (comma-separated)")
	fs.StringVar(&skip, "skip", "", "Skip targets matching this name (comma-separated)")
	fs.StringVar(&tag, "tag", "", "Override image tag for all container targets")
	fs.StringVar(&format, "format", "table", "Output format: table | json | yaml")
	fs.BoolVar(&noPush, "no-push", false, "Build but do not push images to registries")
	fs.StringVar(&envName, "env", "", "Environment name for per-env config overrides")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if dryRun {
		os.Setenv("WFCTL_BUILD_DRY_RUN", "1")    //nolint:errcheck
		defer os.Unsetenv("WFCTL_BUILD_DRY_RUN") //nolint:errcheck
	}

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("wfctl build: load config: %w", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil {
		fmt.Println("No build configuration, skipping build phase")
		return nil
	}

	return runBuildOrchestrate(cfg, buildOpts{
		dryRun:  dryRun,
		only:    splitCSV(only),
		skip:    splitCSV(skip),
		tag:     tag,
		format:  format,
		noPush:  noPush,
		envName: envName,
		cfgPath: cfgPath,
	})
}

// buildOpts carries parsed build flags for use across subcommands.
type buildOpts struct {
	dryRun  bool
	only    []string
	skip    []string
	tag     string
	format  string
	noPush  bool
	envName string
	cfgPath string
}

// runBuildOrchestrate chains all target-type sub-handlers: go → ui → image → push.
// Honors --only, --skip, --no-push. Behavior is wired fully in T19.
func runBuildOrchestrate(cfg *config.WorkflowConfig, opts buildOpts) error {
	build := cfg.CI.Build

	// Go targets.
	for i := range build.Targets {
		t := &build.Targets[i]
		if t.Type != "go" || !shouldInclude(t.Name, opts) {
			continue
		}
		b, ok := builder.Get("go")
		if !ok {
			return fmt.Errorf("go builder not registered")
		}
		if opts.dryRun {
			fmt.Printf("[dry-run] go build: %s (path: %s)\n", t.Name, t.Path)
			continue
		}
		out := &builder.Outputs{}
		if err := b.Build(context.Background(), builderCfgFromTarget(t), out); err != nil {
			return fmt.Errorf("go build %q: %w", t.Name, err)
		}
	}

	// NodeJS/UI targets.
	for i := range build.Targets {
		t := &build.Targets[i]
		if t.Type != "nodejs" || !shouldInclude(t.Name, opts) {
			continue
		}
		b, ok := builder.Get("nodejs")
		if !ok {
			return fmt.Errorf("nodejs builder not registered")
		}
		if opts.dryRun {
			fmt.Printf("[dry-run] nodejs build: %s (path: %s)\n", t.Name, t.Path)
			continue
		}
		out := &builder.Outputs{}
		if err := b.Build(context.Background(), builderCfgFromTarget(t), out); err != nil {
			return fmt.Errorf("nodejs build %q: %w", t.Name, err)
		}
	}

	// Container image targets.
	if len(build.Containers) > 0 {
		imgArgs := []string{}
		if opts.cfgPath != "" {
			imgArgs = append(imgArgs, "--config", opts.cfgPath)
		}
		if opts.dryRun {
			imgArgs = append(imgArgs, "--dry-run")
		}
		if opts.tag != "" {
			imgArgs = append(imgArgs, "--tag", opts.tag)
		}
		if err := runBuildImage(imgArgs); err != nil {
			return err
		}
	}

	// Push step (unless --no-push).
	if !opts.noPush && !opts.dryRun {
		pushArgs := []string{}
		if opts.cfgPath != "" {
			pushArgs = append(pushArgs, "--config", opts.cfgPath)
		}
		if opts.tag != "" {
			pushArgs = append(pushArgs, "--tag", opts.tag)
		}
		if err := runBuildPush(pushArgs); err != nil {
			return fmt.Errorf("push: %w", err)
		}
	}

	if opts.dryRun {
		fmt.Println("[dry-run] build plan complete")
	}
	return nil
}

func shouldInclude(name string, opts buildOpts) bool {
	for _, s := range opts.skip {
		if s == name {
			return false
		}
	}
	if len(opts.only) > 0 {
		for _, o := range opts.only {
			if o == name {
				return true
			}
		}
		return false
	}
	return true
}

func builderCfgFromTarget(t *config.CITarget) builder.Config {
	return builder.Config{
		TargetName: t.Name,
		Path:       t.Path,
		Fields:     t.Config,
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}
