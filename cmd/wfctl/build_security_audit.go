package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// buildAuditFinding is a single finding from the build security audit.
type buildAuditFinding struct {
	Severity string // CRITICAL | WARN | NOTE
	Check    string
	Message  string
	File     string // non-empty for Dockerfile findings
	Line     int    // 1-based line number for Dockerfile findings
}

func (f buildAuditFinding) String() string {
	if f.File != "" {
		return fmt.Sprintf("[%s] %s: %s (%s:%d)", f.Severity, f.Check, f.Message, f.File, f.Line)
	}
	return fmt.Sprintf("[%s] %s: %s", f.Severity, f.Check, f.Message)
}

// runBuildSecurityAudit implements `wfctl build audit` and `wfctl build --security-audit`.
func runBuildSecurityAudit(args []string) error {
	fs := flag.NewFlagSet("build audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "", "Path to workflow config file")
	strict := fs.Bool("strict", false, "Exit 1 if any warnings are found (critical always exits 1)")
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
		loc := ""
		if f.File != "" {
			loc = fmt.Sprintf(" (%s:%d)", f.File, f.Line)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s%s\n", f.Severity, f.Check, f.Message, loc)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Critical always exits 1.
	criticalCount := 0
	for _, f := range findings {
		if f.Severity == "CRITICAL" {
			criticalCount++
		}
	}
	if criticalCount > 0 {
		return fmt.Errorf("%d critical build security issue(s) found", criticalCount)
	}

	// --strict exits 1 on any WARN (NOTE does not count).
	if *strict {
		warnCount := 0
		for _, f := range findings {
			if f.Severity == "WARN" {
				warnCount++
			}
		}
		if warnCount > 0 {
			return fmt.Errorf("%d build security warning(s) found", warnCount)
		}
	}
	return nil
}

// runBuildAuditChecks runs all audit checks and returns the findings.
// workDir is the directory used to locate the plugins lockfile and Dockerfiles.
func runBuildAuditChecks(cfgPath, workDir string) []buildAuditFinding {
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return []buildAuditFinding{{Severity: "WARN", Check: "config", Message: fmt.Sprintf("failed to load config: %v", err)}}
	}
	return auditBuildSecurity(cfg, workDir)
}

// auditBuildSecurity performs all audit checks against cfg.
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
		lockPath := filepath.Join(workDir, wfctlLockPath)
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

	if build == nil {
		return findings
	}

	// Target-level audits: builder.SecurityLint() for each typed target.
	for _, target := range build.Targets {
		b, ok := builder.Get(target.Type)
		if !ok {
			continue
		}
		var sec *builder.SecurityConfig
		if build.Security != nil {
			sec = &builder.SecurityConfig{
				Hardened:   build.Security.Hardened,
				SBOM:       build.Security.SBOM,
				Provenance: build.Security.Provenance,
				NonRoot:    build.Security.NonRoot,
			}
		}
		lintFindings := b.SecurityLint(builder.Config{
			TargetName: target.Name,
			Path:       target.Path,
			Fields:     target.Config,
			Security:   sec,
		})
		for _, lf := range lintFindings {
			severity := strings.ToUpper(lf.Severity)
			findings = append(findings, buildAuditFinding{
				Severity: severity,
				Check:    fmt.Sprintf("target:%s", target.Name),
				Message:  lf.Message,
				File:     lf.File,
				Line:     lf.Line,
			})
		}
	}

	// Dockerfile linting for each container target with method=dockerfile.
	var allowPrefixes []string
	if build.Security != nil && build.Security.BaseImagePolicy != nil {
		allowPrefixes = build.Security.BaseImagePolicy.AllowPrefixes
	}
	for i := range build.Containers {
		ctr := &build.Containers[i]
		method := ctr.Method
		if method == "" {
			method = "dockerfile"
		}
		if method != "dockerfile" {
			continue
		}
		dfPath := ctr.Dockerfile
		if dfPath == "" {
			dfPath = "Dockerfile"
		}
		if !filepath.IsAbs(dfPath) {
			dfPath = filepath.Join(workDir, dfPath)
		}
		dfFindings := lintDockerfile(dfPath, ctr.Name, allowPrefixes)
		findings = append(findings, dfFindings...)
	}

	return findings
}

var (
	reUserRoot       = regexp.MustCompile(`(?i)^USER\s+root\s*$`)
	reUserAny        = regexp.MustCompile(`(?i)^USER\s+\S`)
	reFromLatest     = regexp.MustCompile(`(?i)^FROM\s+[^:\s]+:latest(\s|$)`)
	reFromImage      = regexp.MustCompile(`(?i)^FROM\s+(\S+)`)
	reAddURL         = regexp.MustCompile(`(?i)^ADD\s+https?://`)
	reEmbeddedSecret = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key)\s*=\s*["']?[A-Za-z0-9]`)
)

// lintDockerfile scans a Dockerfile and returns security findings.
func lintDockerfile(dfPath, containerName string, allowPrefixes []string) []buildAuditFinding {
	var findings []buildAuditFinding

	data, err := os.ReadFile(dfPath)
	if err != nil {
		// Dockerfile not present — skip silently (may not exist in audit-only context).
		return findings
	}

	checkName := "dockerfile:" + containerName
	addDF := func(severity, msg string, lineNum int) {
		findings = append(findings, buildAuditFinding{
			Severity: severity,
			Check:    checkName,
			Message:  msg,
			File:     dfPath,
			Line:     lineNum,
		})
	}

	hasUser := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if reUserRoot.MatchString(line) {
			addDF("CRITICAL", "USER root detected — container will run as root", lineNum)
		}
		if reUserAny.MatchString(line) {
			hasUser = true
		}
		if reFromLatest.MatchString(line) {
			addDF("WARN", "FROM uses :latest tag — pin to a digest or explicit version for reproducibility", lineNum)
		}
		if reAddURL.MatchString(line) {
			addDF("WARN", "ADD with URL is untrusted — use RUN curl/wget with checksum verification instead", lineNum)
		}
		if reEmbeddedSecret.MatchString(line) {
			addDF("CRITICAL", fmt.Sprintf("possible embedded secret in line %d — use BuildKit secrets (--secret) instead", lineNum), lineNum)
		}

		// Base image policy.
		if len(allowPrefixes) > 0 && reFromImage.MatchString(line) {
			m := reFromImage.FindStringSubmatch(line)
			if len(m) > 1 && m[1] != "scratch" {
				img := m[1]
				if !matchesAnyPrefix(img, allowPrefixes) {
					addDF("WARN", fmt.Sprintf("base image %q does not match allow_prefixes policy %v", img, allowPrefixes), lineNum)
				}
			}
		}
	}

	if !hasUser {
		findings = append(findings, buildAuditFinding{
			Severity: "CRITICAL",
			Check:    checkName,
			Message:  "no USER directive found — container will run as root by default",
			File:     dfPath,
			Line:     0,
		})
	}

	return findings
}

func matchesAnyPrefix(image string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(image, p) {
			return true
		}
	}
	return false
}
