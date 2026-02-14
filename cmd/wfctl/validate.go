package main

import (
	"flag"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	strict := fs.Bool("strict", false, "Enable strict validation (no empty modules allowed)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl validate [options] <config.yaml>\n\nValidate a workflow configuration file.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	cfgPath := fs.Arg(0)
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var opts []schema.ValidationOption
	if !*strict {
		opts = append(opts, schema.WithAllowEmptyModules())
	}
	opts = append(opts, schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())

	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		return fmt.Errorf("validation failed:\n%v", err)
	}

	fmt.Printf("config %s is valid (%d modules, %d workflows, %d triggers)\n",
		cfgPath, len(cfg.Modules), len(cfg.Workflows), len(cfg.Triggers))
	return nil
}
