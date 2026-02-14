package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/schema"
)

func runSchema(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ExitOnError)
	output := fs.String("output", "", "Write schema to file instead of stdout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl schema [options]\n\nGenerate JSON Schema for workflow configuration files.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	s := schema.GenerateWorkflowSchema()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		enc = json.NewEncoder(f)
		enc.SetIndent("", "  ")
	}

	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}

	if *output != "" {
		fmt.Fprintf(os.Stderr, "Schema written to %s\n", *output)
	}
	return nil
}
