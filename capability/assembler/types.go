// Package assembler composes a structurally-valid workflow scaffold from a
// chosen capability set (+ optional explicit modules). Pure + deterministic.
package assembler

import "github.com/GoCodeAlone/workflow/config"

// AssembledApp is the assembler output: module instances + a workflows.http
// section (so http.server gets AddRouter at boot via ConfigureWorkflow — P1),
// required external plugins, findings (NEXT_STEPS), + unmatched requested caps.
type AssembledApp struct {
	Modules   []config.ModuleConfig
	Workflows map[string]any // {http: {server: <name>, router: <name>}} — P1
	Requires  config.RequiresConfig
	Findings  []Finding
	Unmatched []string
}

// Finding is a NEXT_STEPS item (V8 honest wiring).
type Finding struct {
	Level   string // "info" | "warn"
	Code    string
	Message string
}

// AssemblyInput is the parsed --set payload.
type AssemblyInput struct {
	Capabilities []string         `json:"capabilities"`
	Modules      []ExplicitModule `json:"modules"`
	Goal         string           `json:"goal"`
}

// ExplicitModule is an agent-pinned/injected module instance.
type ExplicitModule struct {
	Type   string         `json:"type"`
	Name   string         `json:"name,omitempty"`
	Config map[string]any `json:"config,omitempty"`
}
