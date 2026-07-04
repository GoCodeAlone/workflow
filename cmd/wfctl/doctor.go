package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

type doctorStatus string

const (
	doctorStatusOK    doctorStatus = "OK"
	doctorStatusWarn  doctorStatus = "WARN"
	doctorStatusError doctorStatus = "ERROR"
)

type doctorReport struct {
	Status   doctorStatus    `json:"status"`
	Sections []doctorSection `json:"sections"`
}

type doctorSection struct {
	Name   string        `json:"name"`
	Checks []doctorCheck `json:"checks"`
}

type doctorCheck struct {
	Status  doctorStatus `json:"status"`
	Message string       `json:"message"`
	Fix     string       `json:"fix,omitempty"`
}

func runDoctor(args []string) error {
	return runDoctorWithOutput(args, os.Stdout)
}

func runDoctorWithOutput(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(out)
	workflowPath := fs.String("workflow", "workflow.yaml", "Workflow config path")
	manifestPath := fs.String("manifest", wfctlManifestPath, "wfctl plugin manifest path")
	lockPath := fs.String("lock-file", wfctlLockPath, "wfctl plugin lockfile path")
	pluginDir := fs.String("plugin-dir", defaultDataDir, "Project plugin install directory")
	includeGlobal := fs.Bool("include-global", false, "Include global plugin directory summary")
	online := fs.Bool("online", false, "Check latest GitHub release")
	format := fs.String("format", "text", "Output format: text or json")
	strict := fs.Bool("strict", false, "Exit non-zero when warnings or errors are found")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl doctor [options]

Check wfctl binary, project config, plugin manifest, lockfile, and installed plugin state.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	report := buildDoctorReport(doctorOptions{
		WorkflowPath:  *workflowPath,
		ManifestPath:  *manifestPath,
		LockPath:      *lockPath,
		PluginDir:     *pluginDir,
		IncludeGlobal: *includeGlobal,
		Online:        *online,
	})

	switch *format {
	case "text":
		renderDoctorText(out, report)
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode doctor report: %w", err)
		}
	default:
		return fmt.Errorf("--format must be text or json, got %q", *format)
	}

	if *strict && report.Status != doctorStatusOK {
		return fmt.Errorf("wfctl doctor found %s diagnostics", report.Status)
	}
	return nil
}

type doctorOptions struct {
	WorkflowPath  string
	ManifestPath  string
	LockPath      string
	PluginDir     string
	IncludeGlobal bool
	Online        bool
}

func buildDoctorReport(opts doctorOptions) doctorReport {
	sections := []doctorSection{
		doctorBinarySection(opts.Online),
		doctorProjectSection(opts.WorkflowPath),
		doctorPluginSection(opts.ManifestPath, opts.LockPath, opts.PluginDir),
	}
	if opts.IncludeGlobal {
		sections = append(sections, doctorGlobalPluginSection())
	}
	report := doctorReport{Sections: sections}
	report.Status = doctorOverallStatus(sections)
	return report
}

func doctorBinarySection(online bool) doctorSection {
	section := doctorSection{Name: "Binary"}
	execPath, err := os.Executable()
	if err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("could not resolve current executable: %v", err),
		})
	} else {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("executable: %s", execPath),
		})
	}
	if version == "dev" {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: "running a development build",
			Fix:     "install a released wfctl binary or run 'wfctl update --check'",
		})
	} else {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("version: %s", version),
		})
	}
	if online {
		section.Checks = append(section.Checks, doctorOnlineUpdateCheck())
	} else {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusOK,
			Message: "latest release check skipped; pass --online to check GitHub",
		})
	}
	return section
}

func doctorOnlineUpdateCheck() doctorCheck {
	rel, err := fetchLatestRelease()
	if err != nil {
		return doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("latest release check failed: %v", err),
			Fix:     "run 'wfctl update --check' later",
		}
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(version, "v")
	if current == "dev" || isNewerVersion(latest, current) {
		return doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("Update available: %s -> %s", version, rel.TagName),
			Fix:     "run 'wfctl update --check' or update through your package manager",
		}
	}
	return doctorCheck{
		Status:  doctorStatusOK,
		Message: fmt.Sprintf("latest release: %s", rel.TagName),
	}
}

func doctorProjectSection(workflowPath string) doctorSection {
	section := doctorSection{Name: "Project"}
	if _, err := os.Stat(workflowPath); errors.Is(err, os.ErrNotExist) {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("workflow config %s not found", workflowPath),
			Fix:     "run 'wfctl init <project>' or 'wfctl wizard'",
		})
		return section
	} else if err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("stat workflow config %s: %v", workflowPath, err),
		})
		return section
	}
	if _, err := config.LoadFromFile(workflowPath); err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("workflow config parse failed: %v", err),
			Fix:     fmt.Sprintf("run 'wfctl validate %s'", workflowPath),
		})
		return section
	}
	section.Checks = append(section.Checks, doctorCheck{
		Status:  doctorStatusOK,
		Message: fmt.Sprintf("workflow config parsed: %s", workflowPath),
	})
	return section
}

func doctorPluginSection(manifestPath, lockPath, pluginDir string) doctorSection {
	section := doctorSection{Name: "Plugins"}
	manifest, err := config.LoadWfctlManifest(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("plugin manifest %s not found", manifestPath),
			Fix:     "run 'wfctl plugin add <plugin>' when this project needs external plugins",
		})
		return section
	}
	if err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("load plugin manifest: %v", err),
		})
		return section
	}
	if len(manifest.Plugins) == 0 {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("plugin manifest has no project plugins: %s", manifestPath),
		})
		return section
	}

	lock, err := config.LoadWfctlLockfile(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("lockfile %s not found", lockPath),
			Fix:     "run 'wfctl plugin lock'",
		})
		return section
	}
	if err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("load lockfile: %v", err),
		})
		return section
	}
	if err := config.ValidateWfctlLockfileProvenance(manifest, lock); err != nil {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusWarn,
			Message: err.Error(),
			Fix:     "run 'wfctl plugin lock'",
		})
	} else {
		section.Checks = append(section.Checks, doctorCheck{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("lockfile matches manifest: %s", lockPath),
		})
	}

	for _, pluginEntry := range manifest.Plugins {
		lockEntry, ok := findLockedPlugin(lock, pluginEntry.Name)
		if !ok {
			section.Checks = append(section.Checks, doctorCheck{
				Status:  doctorStatusWarn,
				Message: fmt.Sprintf("%s is declared in manifest but missing from lockfile", pluginEntry.Name),
				Fix:     "run 'wfctl plugin lock'",
			})
			continue
		}
		section.Checks = append(section.Checks, doctorInstalledPluginCheck(pluginDir, pluginEntry.Name, lockEntry.Version))
	}
	return section
}

func findLockedPlugin(lock *config.WfctlLockfile, name string) (config.WfctlLockPluginEntry, bool) {
	if lock == nil {
		return config.WfctlLockPluginEntry{}, false
	}
	if entry, ok := lock.Plugins[name]; ok {
		return entry, true
	}
	normalized := normalizePluginName(name)
	for key, entry := range lock.Plugins {
		if normalizePluginName(key) == normalized {
			return entry, true
		}
	}
	return config.WfctlLockPluginEntry{}, false
}

func doctorInstalledPluginCheck(pluginDir, name, expectedVersion string) doctorCheck {
	installName := normalizePluginName(name)
	installDir := filepath.Join(pluginDir, installName)
	if _, err := os.Stat(installDir); errors.Is(err, os.ErrNotExist) {
		return doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%s is not installed in %s", name, installDir),
			Fix:     "run 'wfctl plugin install'",
		}
	} else if err != nil {
		return doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("stat installed plugin %s: %v", installDir, err),
		}
	}
	installedVersion := readInstalledVersion(installDir)
	if installedVersion == "unknown" {
		return doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%s is installed but plugin.json version could not be read", name),
			Fix:     "run 'wfctl plugin install'",
		}
	}
	if !samePluginVersion(installedVersion, expectedVersion) {
		return doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%s installed version %s does not match lockfile version %s", name, installedVersion, expectedVersion),
			Fix:     "run 'wfctl plugin install'",
		}
	}
	return doctorCheck{
		Status:  doctorStatusOK,
		Message: fmt.Sprintf("%s installed at %s", name, installedVersion),
	}
}

func doctorGlobalPluginSection() doctorSection {
	dir := defaultGlobalPluginDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return doctorSection{
			Name: "Global Plugins",
			Checks: []doctorCheck{{
				Status:  doctorStatusOK,
				Message: fmt.Sprintf("no global plugin directory: %s", dir),
			}},
		}
	}
	if err != nil {
		return doctorSection{
			Name: "Global Plugins",
			Checks: []doctorCheck{{
				Status:  doctorStatusWarn,
				Message: fmt.Sprintf("could not read global plugin directory %s: %v", dir, err),
			}},
		}
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return doctorSection{
		Name: "Global Plugins",
		Checks: []doctorCheck{{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("%d global plugin(s) in %s", count, dir),
		}},
	}
}

func doctorOverallStatus(sections []doctorSection) doctorStatus {
	status := doctorStatusOK
	for _, section := range sections {
		for _, check := range section.Checks {
			status = worseDoctorStatus(status, check.Status)
		}
	}
	return status
}

func worseDoctorStatus(a, b doctorStatus) doctorStatus {
	if a == doctorStatusError || b == doctorStatusError {
		return doctorStatusError
	}
	if a == doctorStatusWarn || b == doctorStatusWarn {
		return doctorStatusWarn
	}
	return doctorStatusOK
}

func renderDoctorText(out io.Writer, report doctorReport) {
	fmt.Fprintln(out, "wfctl doctor")
	fmt.Fprintf(out, "Overall: %s\n", report.Status)
	for _, section := range report.Sections {
		fmt.Fprintf(out, "\n[%s]\n", section.Name)
		for _, check := range section.Checks {
			fmt.Fprintf(out, "- %s %s\n", check.Status, check.Message)
			if check.Fix != "" {
				fmt.Fprintf(out, "  Fix: %s\n", check.Fix)
			}
		}
	}
}
