package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// CLICommandDef defines a single CLI command in a workflow config.
type CLICommandDef struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Handler is the name of the registered Go function handler for this command.
	// If empty the command name itself is used as the handler key.
	Handler string `json:"handler,omitempty" yaml:"handler,omitempty"`
}

// CLIWorkflowConfig is the configuration structure for a "cli" workflow type.
type CLIWorkflowConfig struct {
	Name        string          `json:"name,omitempty" yaml:"name,omitempty"`
	Version     string          `json:"version,omitempty" yaml:"version,omitempty"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Commands    []CLICommandDef `json:"commands,omitempty" yaml:"commands,omitempty"`
}

// CLICommandFunc is the signature for CLI command handler functions.
type CLICommandFunc func(args []string) error

// CLIWorkflowHandler handles "cli" workflow types. It registers Go function
// handlers for CLI commands, configures them from a YAML workflow config, and
// dispatches os.Args to the correct handler at runtime.
type CLIWorkflowHandler struct {
	config   *CLIWorkflowConfig
	commands map[string]*CLICommandDef // keyed by command name
	runners  map[string]CLICommandFunc // keyed by handler name (or command name)
	output   io.Writer                 // for usage output; defaults to os.Stderr
}

// NewCLIWorkflowHandler creates a new CLIWorkflowHandler with no registered commands.
func NewCLIWorkflowHandler() *CLIWorkflowHandler {
	return &CLIWorkflowHandler{
		commands: make(map[string]*CLICommandDef),
		runners:  make(map[string]CLICommandFunc),
		output:   os.Stderr,
	}
}

// SetOutput overrides the writer used for usage/error messages (default os.Stderr).
// Useful in tests to capture output.
func (h *CLIWorkflowHandler) SetOutput(w io.Writer) {
	h.output = w
}

// RegisterCommand registers a Go function as the handler for a CLI command.
// The key must match either the command's Handler field (if set) or its Name.
func (h *CLIWorkflowHandler) RegisterCommand(key string, fn CLICommandFunc) {
	h.runners[key] = fn
}

// CanHandle returns true for the "cli" workflow type.
func (h *CLIWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "cli"
}

// ConfigureWorkflow stores the CLI workflow config and indexes commands by name.
func (h *CLIWorkflowHandler) ConfigureWorkflow(_ modular.Application, workflowConfig any) error {
	cfg, err := parseCLIWorkflowConfig(workflowConfig)
	if err != nil {
		return fmt.Errorf("cli workflow: %w", err)
	}
	h.config = cfg
	for i := range cfg.Commands {
		cmd := &cfg.Commands[i]
		h.commands[cmd.Name] = cmd
	}
	return nil
}

// ExecuteWorkflow implements WorkflowHandler. The action is the command name;
// data["args"] may hold a []string of additional arguments.
func (h *CLIWorkflowHandler) ExecuteWorkflow(_ context.Context, _ string, action string, data map[string]any) (map[string]any, error) {
	args, _ := data["args"].([]string)
	if err := h.runCommand(action, args); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}

// Dispatch inspects args (typically os.Args[1:]) to choose and run a command.
// A missing or unknown command prints usage and returns an error.
func (h *CLIWorkflowHandler) Dispatch(args []string) error {
	if len(args) == 0 {
		h.printUsage()
		return fmt.Errorf("no command specified")
	}

	cmd := args[0]
	switch cmd {
	case "-h", "--help", "help":
		h.printUsage()
		return nil
	case "-v", "--version", "version":
		version := "dev"
		if h.config != nil && h.config.Version != "" {
			version = h.config.Version
		}
		fmt.Fprintln(h.output, version)
		return nil
	}

	return h.runCommand(cmd, args[1:])
}

// runCommand looks up and calls the registered runner for the named command.
func (h *CLIWorkflowHandler) runCommand(name string, args []string) error {
	def, known := h.commands[name]
	if !known {
		fmt.Fprintf(h.output, "unknown command: %s\n\n", name) //nolint:gosec // G705
		h.printUsage()
		return fmt.Errorf("unknown command: %s", name)
	}

	handlerKey := def.Handler
	if handlerKey == "" {
		handlerKey = def.Name
	}

	fn, ok := h.runners[handlerKey]
	if !ok {
		return fmt.Errorf("no runner registered for command %q (handler key: %q)", name, handlerKey)
	}

	return fn(args)
}

// printUsage writes the CLI usage message to the configured output writer.
func (h *CLIWorkflowHandler) printUsage() {
	appName := "app"
	description := ""
	version := "dev"
	if h.config != nil {
		if h.config.Name != "" {
			appName = h.config.Name
		}
		if h.config.Description != "" {
			description = h.config.Description
		}
		if h.config.Version != "" {
			version = h.config.Version
		}
	}

	fmt.Fprintf(h.output, "%s - %s (version %s)\n\nUsage:\n  %s <command> [options]\n\nCommands:\n",
		appName, description, version, appName)

	// Print commands in sorted order for deterministic output.
	names := make([]string, 0, len(h.commands))
	for n := range h.commands {
		names = append(names, n)
	}
	sort.Strings(names)

	// Calculate max name width for alignment.
	maxWidth := 0
	for _, n := range names {
		if len(n) > maxWidth {
			maxWidth = len(n)
		}
	}

	for _, n := range names {
		def := h.commands[n]
		padding := strings.Repeat(" ", maxWidth-len(n))
		fmt.Fprintf(h.output, "  %s%s  %s\n", n, padding, def.Description)
	}

	fmt.Fprintf(h.output, "\nRun '%s <command> -h' for command-specific help.\n", appName)
}

// parseCLIWorkflowConfig converts the raw workflow config (map[string]any) to
// a CLIWorkflowConfig. It accepts either the map representation produced by
// YAML unmarshalling or a pre-typed *CLIWorkflowConfig.
func parseCLIWorkflowConfig(raw any) (*CLIWorkflowConfig, error) {
	if raw == nil {
		return &CLIWorkflowConfig{}, nil
	}

	if cfg, ok := raw.(*CLIWorkflowConfig); ok {
		return cfg, nil
	}

	cfgMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid cli workflow configuration type: %T", raw)
	}

	cfg := &CLIWorkflowConfig{}
	cfg.Name, _ = cfgMap["name"].(string)
	cfg.Version, _ = cfgMap["version"].(string)
	cfg.Description, _ = cfgMap["description"].(string)

	if rawCmds, ok := cfgMap["commands"].([]any); ok {
		for i, rc := range rawCmds {
			cmdMap, ok := rc.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("command at index %d is not a map", i)
			}
			def := CLICommandDef{}
			def.Name, _ = cmdMap["name"].(string)
			def.Description, _ = cmdMap["description"].(string)
			def.Handler, _ = cmdMap["handler"].(string)
			cfg.Commands = append(cfg.Commands, def)
		}
	}

	return cfg, nil
}
