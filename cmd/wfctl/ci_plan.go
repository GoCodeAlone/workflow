package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/cigen"
)

func runCIPlan(args []string) error {
	fs := flag.NewFlagSet("ci plan", flag.ContinueOnError)
	configFile := fs.String("config", "", "Workflow config file (default: app.yaml or infra.yaml)")
	configFileShort := fs.String("c", "", "Workflow config file (shorthand for --config)")
	out := fs.String("out", "-", "Output file for the CIPlan JSON ('-' for stdout)")
	phaseConfig := fs.String("phase-config", "", "Prerequisite phase config path (creates a 2-phase plan)")
	configPathAlias := fs.String("config-path-alias", "", "Logical repo-relative path for the primary config in generated CI (default: relativized real path)")
	phaseConfigAlias := fs.String("phase-config-alias", "", "Logical repo-relative path for the prereq config in generated CI")
	wfctlVer := fs.String("wfctl-version", "", "Pin wfctl version in plan (default: latest)")
	branch := fs.String("branch", "", "Default branch name (default: main)")
	runner := fs.String("runner", "", "GitHub Actions runner label (default: ubuntu-latest)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl ci plan [options]

Analyze a workflow config and emit a platform-neutral CIPlan as JSON.
The plan can be passed to 'wfctl ci generate --from-plan' to render
CI files without re-analyzing.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve -c shorthand
	if *configFile == "" && *configFileShort != "" {
		*configFile = *configFileShort
	}

	configPath, err := resolveCIConfig(*configFile)
	if err != nil {
		return err
	}

	opts := cigen.Options{
		WfctlVersion:     *wfctlVer,
		DefaultBranch:    *branch,
		Runner:           *runner,
		PhaseConfig:      *phaseConfig,
		ConfigPathAlias:  *configPathAlias,
		PhaseConfigAlias: *phaseConfigAlias,
	}

	plan, err := cigen.Analyze([]string{configPath}, opts)
	if err != nil {
		return fmt.Errorf("ci plan: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("ci plan: marshal: %w", err)
	}

	if *out == "-" {
		_, err = fmt.Fprintln(os.Stdout, string(data))
		return err
	}

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("ci plan: create %s: %w", *out, err)
	}
	defer f.Close() //nolint:errcheck
	_, err = f.WriteString(string(data) + "\n")
	return err
}
