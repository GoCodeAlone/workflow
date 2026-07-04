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

type repairStatus string

const (
	repairStatusNoop    repairStatus = "noop"
	repairStatusPlanned repairStatus = "planned"
	repairStatusApplied repairStatus = "applied"
	repairStatusManual  repairStatus = "manual"
)

type repairReport struct {
	Mode        string         `json:"mode"`
	Status      repairStatus   `json:"status"`
	Doctor      doctorStatus   `json:"doctorStatus"`
	Actions     []repairAction `json:"actions"`
	Suggestions []repairAction `json:"suggestions,omitempty"`
}

type repairAction struct {
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
}

type repairRunner interface {
	PluginLock(args []string) error
	PluginInstall(args []string) error
}

type defaultRepairRunner struct{}

func (defaultRepairRunner) PluginLock(args []string) error {
	return runPluginLock(args)
}

func (defaultRepairRunner) PluginInstall(args []string) error {
	return runPluginInstall(args)
}

func runRepair(args []string) error {
	return runRepairWithOutput(args, os.Stdout)
}

func runRepairWithOutput(args []string, out io.Writer) error {
	return runRepairWithRunner(args, out, defaultRepairRunner{})
}

func runRepairWithRunner(args []string, out io.Writer, runner repairRunner) error {
	fs := flag.NewFlagSet("repair", flag.ContinueOnError)
	fs.SetOutput(out)
	workflowPath := fs.String("workflow", "workflow.yaml", "Workflow config path")
	manifestPath := fs.String("manifest", wfctlManifestPath, "wfctl plugin manifest path")
	lockPath := fs.String("lock-file", wfctlLockPath, "wfctl plugin lockfile path")
	pluginDir := fs.String("plugin-dir", defaultDataDir, "Project plugin install directory")
	includeGlobal := fs.Bool("include-global", false, "Include global plugin diagnostics as suggestions")
	online := fs.Bool("online", false, "Check latest GitHub release for suggestions")
	format := fs.String("format", "text", "Output format: text or json")
	apply := fs.Bool("apply", false, "Apply automatic repairs")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl repair [options]

Plan or apply safe wfctl lifecycle repairs. By default this command is a dry-run.
Use --apply to regenerate a stale plugin lockfile and install locked project plugins.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	report := buildRepairReport(repairOptions{
		Doctor: doctorOptions{
			WorkflowPath:  *workflowPath,
			ManifestPath:  *manifestPath,
			LockPath:      *lockPath,
			PluginDir:     *pluginDir,
			IncludeGlobal: *includeGlobal,
			Online:        *online,
		},
		Apply: *apply,
	})

	if *apply {
		if err := applyRepairActions(report.Actions, runner); err != nil {
			return err
		}
		if len(report.Actions) > 0 {
			report.Status = repairStatusApplied
		}
	}

	switch *format {
	case "text":
		renderRepairText(out, report)
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode repair report: %w", err)
		}
	default:
		return fmt.Errorf("--format must be text or json, got %q", *format)
	}
	return nil
}

type repairOptions struct {
	Doctor doctorOptions
	Apply  bool
}

func buildRepairReport(opts repairOptions) repairReport {
	doctor := buildDoctorReport(opts.Doctor)
	actions, automatedKinds := planAutomaticRepairActions(opts.Doctor)
	suggestions := planManualRepairSuggestions(doctor, automatedKinds)
	status := repairStatusNoop
	if len(actions) > 0 {
		status = repairStatusPlanned
	} else if len(suggestions) > 0 {
		status = repairStatusManual
	}
	mode := "dry-run"
	if opts.Apply {
		mode = "apply"
	}
	return repairReport{
		Mode:        mode,
		Status:      status,
		Doctor:      doctor.Status,
		Actions:     actions,
		Suggestions: suggestions,
	}
}

func planAutomaticRepairActions(opts doctorOptions) ([]repairAction, map[string]bool) {
	automatedKinds := make(map[string]bool)
	manifest, err := config.LoadWfctlManifest(opts.ManifestPath)
	if err != nil || manifest == nil || len(manifest.Plugins) == 0 {
		return nil, automatedKinds
	}

	lockNeedsRepair := false
	lock, err := config.LoadWfctlLockfile(opts.LockPath)
	if errors.Is(err, os.ErrNotExist) {
		lockNeedsRepair = true
	} else if err != nil {
		return nil, automatedKinds
	} else if err := config.ValidateWfctlLockfileProvenance(manifest, lock); err != nil {
		lockNeedsRepair = true
	} else {
		for _, pluginEntry := range manifest.Plugins {
			if _, ok := findLockedPlugin(lock, pluginEntry.Name); !ok {
				lockNeedsRepair = true
				break
			}
		}
	}

	installNeedsRepair := lockNeedsRepair
	if !installNeedsRepair && lock != nil {
		for _, pluginEntry := range manifest.Plugins {
			lockEntry, ok := findLockedPlugin(lock, pluginEntry.Name)
			if !ok {
				installNeedsRepair = true
				break
			}
			if doctorInstalledPluginCheck(opts.PluginDir, pluginEntry.Name, lockEntry.Version).Status != doctorStatusOK {
				installNeedsRepair = true
				break
			}
		}
	}

	var actions []repairAction
	if lockNeedsRepair {
		args := []string{"--config", opts.WorkflowPath, "--manifest", opts.ManifestPath, "--lock-file", opts.LockPath}
		actions = append(actions, repairAction{
			Kind:        "plugin-lock",
			Description: "Regenerate the project plugin lockfile from wfctl.yaml",
			Command:     repairCommandString("plugin lock", args),
			Args:        args,
		})
		automatedKinds["plugin-lock"] = true
	}
	if installNeedsRepair {
		args := []string{"--manifest", opts.ManifestPath, "--lock-file", opts.LockPath, "--plugin-dir", opts.PluginDir}
		actions = append(actions, repairAction{
			Kind:        "plugin-install",
			Description: "Install project plugins from the lockfile",
			Command:     repairCommandString("plugin install", args),
			Args:        args,
		})
		automatedKinds["plugin-install"] = true
	}
	return actions, automatedKinds
}

func planManualRepairSuggestions(report doctorReport, automatedKinds map[string]bool) []repairAction {
	var suggestions []repairAction
	seen := make(map[string]bool)
	for _, section := range report.Sections {
		for _, check := range section.Checks {
			if check.Fix == "" {
				continue
			}
			if automatedKinds["plugin-lock"] && strings.Contains(check.Fix, "wfctl plugin lock") {
				continue
			}
			if automatedKinds["plugin-install"] && strings.Contains(check.Fix, "wfctl plugin install") {
				continue
			}
			key := check.Message + "\x00" + check.Fix
			if seen[key] {
				continue
			}
			seen[key] = true
			suggestions = append(suggestions, repairAction{
				Kind:        "manual",
				Description: check.Message,
				Command:     check.Fix,
			})
		}
	}
	return suggestions
}

func applyRepairActions(actions []repairAction, runner repairRunner) error {
	for _, action := range actions {
		switch action.Kind {
		case "plugin-lock":
			if err := runner.PluginLock(action.Args); err != nil {
				return fmt.Errorf("repair plugin lock: %w", err)
			}
		case "plugin-install":
			if err := runner.PluginInstall(action.Args); err != nil {
				return fmt.Errorf("repair plugin install: %w", err)
			}
		default:
			return fmt.Errorf("unsupported automatic repair action %q", action.Kind)
		}
	}
	return nil
}

func renderRepairText(out io.Writer, report repairReport) {
	fmt.Fprintln(out, "wfctl repair")
	if report.Mode == "apply" {
		fmt.Fprintln(out, "Mode: APPLY")
	} else {
		fmt.Fprintln(out, "Mode: DRY-RUN")
	}
	fmt.Fprintf(out, "Doctor: %s\n", report.Doctor)
	fmt.Fprintf(out, "Status: %s\n", report.Status)
	if len(report.Actions) == 0 {
		fmt.Fprintln(out, "\nNo automatic repairs available.")
	} else {
		fmt.Fprintln(out, "\n[Automatic]")
		for _, action := range report.Actions {
			prefix := "Would run"
			if report.Mode == "apply" && report.Status == repairStatusApplied {
				prefix = "Ran"
			}
			fmt.Fprintf(out, "- %s: %s\n", action.Kind, action.Description)
			fmt.Fprintf(out, "  %s: %s\n", prefix, action.Command)
		}
	}
	if len(report.Suggestions) > 0 {
		fmt.Fprintln(out, "\n[Manual]")
		for _, suggestion := range report.Suggestions {
			fmt.Fprintf(out, "- %s\n", suggestion.Description)
			fmt.Fprintf(out, "  Try: %s\n", suggestion.Command)
		}
	}
}

func repairCommandString(command string, args []string) string {
	parts := append([]string{"wfctl"}, strings.Fields(command)...)
	for _, arg := range args {
		parts = append(parts, shellQuoteRepairArg(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuoteRepairArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\n'\"\\$`!*?[]{}()<>|&;") {
		return filepath.ToSlash(arg)
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
