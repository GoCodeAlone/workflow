package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/manifest"
	"gopkg.in/yaml.v3"
)

func runManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	format := fs.String("format", "json", "Output format: json or yaml")
	name := fs.String("name", "", "Override the workflow name in the manifest")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl manifest [options] <config.yaml>

Analyze a workflow configuration and report its infrastructure requirements.

Examples:
  wfctl manifest config.yaml
  wfctl manifest -format yaml config.yaml
  wfctl manifest -name my-service config.yaml

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	cfg, err := config.LoadFromFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	m := manifest.AnalyzeWithName(cfg, *name)

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("failed to encode YAML: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format: %s (use json or yaml)", *format)
	}

	return nil
}
