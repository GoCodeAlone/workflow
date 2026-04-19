package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/GoCodeAlone/workflow/config"
)

// buildAuditFinding is a single finding from the build security audit.
type buildAuditFinding struct {
	Severity string // WARN | NOTE
	Check    string
	Message  string
}

func (f buildAuditFinding) String() string {
	return fmt.Sprintf("[%s] %s: %s", f.Severity, f.Check, f.Message)
}

// runBuildSecurityAudit implements `wfctl build audit` and `wfctl build --security-audit`.
func runBuildSecurityAudit(args []string) error {
	fs := flag.NewFlagSet("build audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "", "Path to workflow config file")
	strict := fs.Bool("strict", false, "Exit 1 if any warnings are found")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *cfgPath == "" {
		for _, c := range []string{"workflow.yaml", "app.yaml", "ci.yaml"} {
			if _, err := os.Stat(c); err == nil {
				*cfgPath = c
				break
			}
		}
	}
	if *cfgPath == "" {
		return fmt.Errorf("wfctl build audit: no config file found")
	}

	workDir := filepath.Dir(*cfgPath)
	findings := runBuildAuditChecks(*cfgPath, workDir)

	if len(findings) == 0 {
		fmt.Println("No build security issues found.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tCHECK\tFINDING")
	fmt.Fprintln(tw, "--------\t-----\t-------")
	for _, f := range findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", f.Severity, f.Check, f.Message)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if *strict {
		for _, f := range findings {
			if f.Severity == "WARN" {
				return fmt.Errorf("%d build security issue(s) found", len(findings))
			}
		}
	}
	return nil
}

// runBuildAuditChecks runs all audit checks and returns the findings.
// workDir is the directory used to locate the plugins lockfile.
func runBuildAuditChecks(cfgPath, workDir string) []buildAuditFinding {
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return []buildAuditFinding{{Severity: "WARN", Check: "config", Message: fmt.Sprintf("failed to load config: %v", err)}}
	}
	return auditBuildSecurity(cfg, workDir)
}

// auditBuildSecurity performs all six T34 audit checks against cfg.
func auditBuildSecurity(cfg *config.WorkflowConfig, workDir string) []buildAuditFinding {
	var findings []buildAuditFinding

	add := func(severity, check, message string) {
		findings = append(findings, buildAuditFinding{Severity: severity, Check: check, Message: message})
	}

	var build *config.CIBuildConfig
	var registries []config.CIRegistry
	if cfg.CI != nil {
		build = cfg.CI.Build
		registries = cfg.CI.Registries
	}

	// Check 1: ci.build.security.hardened=false.
	if build != nil && build.Security != nil && !build.Security.Hardened {
		add("WARN", "hardened", "ci.build.security.hardened=false — supply-chain hardening is disabled")
	}

	// Check 2: dockerfile containers without sbom or provenance.
	if build != nil {
		for i := range build.Containers {
			ctr := &build.Containers[i]
			method := ctr.Method
			if method == "" {
				method = "dockerfile"
			}
			if method != "dockerfile" {
				continue
			}
			sec := build.Security
			if sec == nil || !sec.SBOM {
				add("WARN", "sbom", fmt.Sprintf("container %q uses dockerfile but ci.build.security.sbom is not enabled", ctr.Name))
			}
			if sec == nil || sec.Provenance == "" {
				add("WARN", "provenance", fmt.Sprintf("container %q uses dockerfile but ci.build.security.provenance is not set", ctr.Name))
			}
		}
	}

	// Check 3: registries without retention.
	for _, reg := range registries {
		if reg.Retention == nil {
			add("WARN", "retention", fmt.Sprintf("ci.registries[%q] has no retention policy defined", reg.Name))
		}
	}

	// Check 4: plugins declared without a plugins lockfile.
	hasPlugins := (cfg.Requires != nil && len(cfg.Requires.Plugins) > 0) ||
		(cfg.Plugins != nil && len(cfg.Plugins.External) > 0)
	if hasPlugins {
		lockPath := filepath.Join(workDir, wfctlYAMLPath)
		lf, err := loadPluginLockfile(lockPath)
		if err != nil || len(lf.Plugins) == 0 {
			add("WARN", "lockfile", fmt.Sprintf("plugins are declared in config but no plugins lockfile found at %s", lockPath))
		}
	}

	// Check 5: registries with auth.env where the env var is not set.
	for _, reg := range registries {
		if reg.Auth == nil || reg.Auth.Env == "" {
			continue
		}
		if os.Getenv(reg.Auth.Env) == "" {
			add("WARN", "auth-env", fmt.Sprintf("ci.registries[%q] auth.env=%q is not set in the current environment", reg.Name, reg.Auth.Env))
		}
	}

	// Check 6: environments.local.build overrides that disable hardening — NOTE only.
	if cfg.Environments != nil {
		if localEnv, ok := cfg.Environments["local"]; ok && localEnv != nil && localEnv.Build != nil {
			sec := localEnv.Build.Security
			if sec != nil && !sec.Hardened {
				add("NOTE", "local-hardening", "environments.local.build.security.hardened=false — expected for local dev, not a security issue")
			}
		}
	}

	return findings
}
