package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

func runConfigValidate(args []string) error {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml project manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to .wfctl-lock.yaml")
	skipLock := fs.Bool("skip-lock", false, "Skip lockfile validation")
	locked := fs.Bool("locked", false, "Require .wfctl-lock.yaml to match wfctl.yaml without updates")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl config validate [options] [wfctl.yaml]

Validate wfctl project configuration files. Use "wfctl validate" for workflow
runtime configs such as workflow.yaml.

Options:
`)
		fs.PrintDefaults()
	}
	args = reorderFlags(args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("expected at most one positional manifest path")
	}
	if fs.NArg() == 1 {
		*manifestPath = fs.Arg(0)
	}
	lockFlagExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "lock-file" {
			lockFlagExplicit = true
		}
	})

	if err := validateWfctlManifestFile(*manifestPath); err != nil {
		return err
	}
	fmt.Printf("  PASS %s (wfctl project manifest)\n", *manifestPath)

	if *skipLock {
		if *locked {
			return fmt.Errorf("--skip-lock and --locked are contradictory")
		}
		return nil
	}
	if err := validateWfctlLockfile(*lockPath); err != nil {
		if errors.Is(err, os.ErrNotExist) && !lockFlagExplicit {
			fmt.Fprintf(os.Stderr, "  WARN %s: lockfile not found; run 'wfctl plugin lock' when plugins are declared\n", *lockPath)
			return nil
		}
		return err
	}
	if *locked {
		if err := validateLockfileProvenanceForManifest(*manifestPath, *lockPath); err != nil {
			return err
		}
		fmt.Printf("  PASS %s matches %s (locked)\n", *lockPath, *manifestPath)
	}
	fmt.Printf("  PASS %s (wfctl plugin lockfile)\n", *lockPath)
	return nil
}

func validateWfctlManifestFile(path string) error {
	raw, err := readYAMLMap(path)
	if err != nil {
		return err
	}
	if _, hasVersion := raw["version"]; !hasVersion {
		if _, hasPlugins := raw["plugins"]; !hasPlugins {
			return fmt.Errorf("%s is not a wfctl project manifest; use 'wfctl validate' for workflow runtime configs", path)
		}
	}
	manifest, err := config.LoadWfctlManifest(path)
	if err != nil {
		return err
	}
	var errs []error
	if manifest.Version != 1 {
		errs = append(errs, fmt.Errorf("version: got %d want 1", manifest.Version))
	}
	seen := map[string]struct{}{}
	for i, plugin := range manifest.Plugins {
		name := strings.TrimSpace(plugin.Name)
		if name == "" {
			errs = append(errs, fmt.Errorf("plugins[%d].name is required", i))
		} else if _, ok := seen[name]; ok {
			errs = append(errs, fmt.Errorf("plugins[%d].name %q is duplicated", i, name))
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(plugin.Version) == "" {
			errs = append(errs, fmt.Errorf("plugins[%d].version is required", i))
		}
		if plugin.Auth != nil && strings.TrimSpace(plugin.Auth.Env) == "" {
			errs = append(errs, fmt.Errorf("plugins[%d].auth.env is required when auth is declared", i))
		}
		if plugin.Verify != nil && strings.TrimSpace(plugin.Verify.Identity) == "" {
			errs = append(errs, fmt.Errorf("plugins[%d].verify.identity is required when verify is declared", i))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid wfctl manifest %s: %w", path, errors.Join(errs...))
	}
	return nil
}

func validateWfctlLockfile(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return fmt.Errorf("stat lockfile %s: %w", path, err)
	}
	lockfile, err := config.LoadWfctlLockfile(path)
	if err != nil {
		return err
	}
	var errs []error
	if lockfile.Version != 1 {
		errs = append(errs, fmt.Errorf("version: got %d want 1", lockfile.Version))
	}
	for name, plugin := range lockfile.Plugins {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, fmt.Errorf("plugins contains an empty plugin key"))
		}
		if strings.TrimSpace(plugin.Version) == "" {
			errs = append(errs, fmt.Errorf("plugins[%s].version is required", name))
		}
		for platform, artifact := range plugin.Platforms {
			if strings.TrimSpace(platform) == "" {
				errs = append(errs, fmt.Errorf("plugins[%s].platforms contains an empty platform key", name))
			}
			if _, err := url.ParseRequestURI(artifact.URL); err != nil {
				errs = append(errs, fmt.Errorf("plugins[%s].platforms[%s].url is invalid: %w", name, platform, err))
			}
			if err := validateSHA256Hex(artifact.SHA256); err != nil {
				errs = append(errs, fmt.Errorf("plugins[%s].platforms[%s].sha256: %w", name, platform, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid wfctl lockfile %s: %w", path, errors.Join(errs...))
	}
	return nil
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

func isLikelyWfctlProjectManifest(path string) bool {
	switch filepath.Base(path) {
	case wfctlManifestPath, ".wfctl.yaml":
	default:
		return false
	}
	raw, err := readYAMLMap(path)
	if err != nil {
		return false
	}
	if _, hasPlugins := raw["plugins"]; !hasPlugins {
		return false
	}
	for _, runtimeKey := range []string{"modules", "workflows", "triggers", "pipelines", "services"} {
		if _, ok := raw[runtimeKey]; ok {
			return false
		}
	}
	return true
}

func validateSHA256Hex(value string) error {
	if len(value) != 64 {
		return fmt.Errorf("got length %d want 64", len(value))
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return err
	}
	if len(decoded) != 32 {
		return fmt.Errorf("decoded length got %d want 32", len(decoded))
	}
	return nil
}
