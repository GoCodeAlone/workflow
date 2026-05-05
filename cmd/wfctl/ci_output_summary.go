package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// SummaryInput is the data bundled into a step-summary Markdown block.
type SummaryInput struct {
	Operation   string // "deploy" | "apply" | "destroy"
	Env         string // e.g. "staging"
	Resource    string // resource display name
	Outcome     string // "SUCCESS" | "FAILED"
	ConsoleURL  string // direct link to provider dashboard
	Diagnostics []interfaces.Diagnostic
	Phases      []PhaseTiming
	RootCause   string
}

// PhaseTiming records the name, status, and duration of a deployment phase.
type PhaseTiming struct {
	Name     string
	Status   string
	Duration time.Duration
}

// WriteStepSummary appends Markdown to the CI provider's summary destination.
// No-ops when the provider has no summary destination (all non-GHA for now).
func WriteStepSummary(emitter CIGroupEmitter, in SummaryInput) (err error) {
	path := emitter.SummaryPath()
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // summary path comes from GITHUB_STEP_SUMMARY env var, not user input
	if err != nil {
		return fmt.Errorf("open summary: %w", err)
	}
	defer func() {
		// Capture the close error (may flush buffered writes) and surface it
		// only when there is no earlier write error to preserve.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return renderSummary(f, in)
}

func renderSummary(w io.Writer, in SummaryInput) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## wfctl: %s %s — %s\n\n", in.Operation, in.Env, in.Outcome)
	if in.Resource != "" {
		fmt.Fprintf(&b, "**Resource:** %s\n", in.Resource)
	}
	if in.RootCause != "" {
		fmt.Fprintf(&b, "**Root cause:** `%s`\n", in.RootCause)
	}
	if in.ConsoleURL != "" {
		fmt.Fprintf(&b, "**Console:** %s\n", in.ConsoleURL)
	}
	if len(in.Phases) > 0 {
		b.WriteString("\n### Phase timings\n\n| Phase | Status | Duration |\n|---|---|---|\n")
		for _, p := range in.Phases {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", p.Name, p.Status, p.Duration.Round(time.Second))
		}
	}
	if len(in.Diagnostics) > 0 {
		b.WriteString("\n### Diagnostics\n\n")
		for _, d := range in.Diagnostics {
			fmt.Fprintf(&b, "- **[%s]** `%s` — %s\n", d.Phase, d.ID, d.Cause)
			if d.Detail != "" {
				fmt.Fprintf(&b, "  <details><summary>log tail</summary>\n\n  ```\n  %s\n  ```\n\n  </details>\n",
					strings.ReplaceAll(d.Detail, "\n", "\n  "))
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
