package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/schema"
)

type editorSchemasOutput struct {
	ModuleSchemas map[string]*schema.ModuleSchema `json:"moduleSchemas"`
	StepSchemas   map[string]*schema.StepSchema   `json:"stepSchemas"`
	CoercionRules map[string][]string             `json:"coercionRules"`
}

func runEditorSchemas(args []string) error {
	fs := flag.NewFlagSet("editor-schemas", flag.ExitOnError)
	output := fs.String("output", "", "Write schemas to file instead of stdout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl editor-schemas [options]\n\nExport module schemas and type coercion rules for the visual editor.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	moduleReg := schema.NewModuleSchemaRegistry()
	stepReg := schema.NewStepSchemaRegistry()
	coercionReg := schema.NewTypeCoercionRegistry()

	data := editorSchemasOutput{
		ModuleSchemas: moduleReg.AllMap(),
		StepSchemas:   stepReg.AllMap(),
		CoercionRules: coercionReg.Rules(),
	}

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("encode schemas: %w", err)
	}

	if *output != "" {
		fmt.Fprintf(os.Stderr, "Editor schemas written to %s\n", *output)
	}
	return nil
}
