package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/schema"
)

func runSnippets(args []string) error {
	fs := flag.NewFlagSet("snippets", flag.ExitOnError)
	format := fs.String("format", "json", "Output format: json, vscode, jetbrains")
	output := fs.String("output", "", "Write output to file instead of stdout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl snippets [options]

Export workflow configuration snippets for IDE support.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
Formats:
  json        Raw snippet list as JSON (default)
  vscode      VSCode .code-snippets JSON format
  jetbrains   JetBrains live templates XML format

Examples:
  wfctl snippets --format vscode --output workflow.code-snippets
  wfctl snippets --format jetbrains --output workflow.xml
`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	var data []byte
	var err error

	switch *format {
	case "vscode":
		data, err = schema.ExportSnippetsVSCode()
		if err != nil {
			return fmt.Errorf("vscode export failed: %w", err)
		}
	case "jetbrains":
		data, err = schema.ExportSnippetsJetBrains()
		if err != nil {
			return fmt.Errorf("jetbrains export failed: %w", err)
		}
	case "json", "":
		snips := schema.GetSnippets()
		data, err = json.MarshalIndent(snips, "", "  ")
		if err != nil {
			return fmt.Errorf("json export failed: %w", err)
		}
	default:
		return fmt.Errorf("unknown format %q; choose json, vscode, or jetbrains", *format)
	}

	w := os.Stdout
	if *output != "" {
		f, ferr := os.Create(*output)
		if ferr != nil {
			return fmt.Errorf("failed to create output file: %w", ferr)
		}
		defer f.Close()
		w = f
	}

	if _, err = w.Write(data); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}
	if *output != "" {
		fmt.Fprintf(os.Stderr, "Snippets written to %s\n", *output)
	}
	return nil
}
