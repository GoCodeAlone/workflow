package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

type planAuditReport struct {
	Docs     []planDoc     `json:"docs"`
	Findings []planFinding `json:"findings"`
	Summary  auditSummary  `json:"summary"`
}

type auditSummary struct {
	Status   string `json:"status"`
	Docs     int    `json:"docs"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

func runAudit(args []string) error {
	return runAuditWithOutput(args, os.Stdout)
}

func runAuditWithOutput(args []string, out io.Writer) error {
	if len(args) < 1 {
		return auditUsage(out)
	}
	switch args[0] {
	case "plans":
		return runAuditPlansWithOutput(args[1:], out)
	case "plugins":
		return fmt.Errorf("audit plugins is not implemented yet")
	default:
		return fmt.Errorf("unknown audit subcommand %q (try: plans, plugins)", args[0])
	}
}

func auditUsage(out io.Writer) error {
	fmt.Fprintln(out, `Usage: wfctl audit <subject> [options]

Audit Workflow project metadata.

Subjects:
  plans    Audit docs/plans metadata and implementation evidence
  plugins  Audit workflow-plugin-* manifest shape`)
	return fmt.Errorf("missing audit subcommand")
}

func runAuditPlansWithOutput(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("audit plans", flag.ContinueOnError)
	dir := fs.String("dir", "docs/plans", "Directory containing plan docs")
	jsonOut := fs.Bool("json", false, "Write JSON output")
	staleAfter := fs.String("stale-after", "30d", "Warn when verification is older than this duration")
	fixIndex := fs.Bool("fix-index", false, "Regenerate docs/plans/INDEX.md")
	if err := fs.Parse(args); err != nil {
		return err
	}

	staleDuration, err := parsePlanAuditDuration(*staleAfter)
	if err != nil {
		return err
	}
	docs, findings, err := collectPlanDocs(*dir, planAuditNow(), staleDuration)
	if err != nil {
		return err
	}
	report := planAuditReport{
		Docs:     docs,
		Findings: findings,
		Summary:  summarizePlanAudit(docs, findings),
	}

	if *fixIndex {
		if err := os.WriteFile(filepath.Join(*dir, "INDEX.md"), []byte(renderPlanIndex(docs)), 0o644); err != nil {
			return fmt.Errorf("write plan index: %w", err)
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		renderPlanAuditReport(out, report)
	}

	if report.Summary.Errors > 0 {
		return fmt.Errorf("%d plan audit error(s) found", report.Summary.Errors)
	}
	return nil
}

func summarizePlanAudit(docs []planDoc, findings []planFinding) auditSummary {
	summary := auditSummary{Status: "PASS", Docs: len(docs)}
	for _, finding := range findings {
		switch finding.Level {
		case "ERROR":
			summary.Errors++
		case "WARN":
			summary.Warnings++
		}
	}
	if summary.Errors > 0 {
		summary.Status = "FAIL"
	} else if summary.Warnings > 0 {
		summary.Status = "WARN"
	}
	return summary
}

func renderPlanAuditReport(out io.Writer, report planAuditReport) {
	fmt.Fprintf(out, "%s plans audit: %d doc(s), %d warning(s), %d error(s)\n",
		report.Summary.Status, report.Summary.Docs, report.Summary.Warnings, report.Summary.Errors)
	if len(report.Findings) == 0 {
		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LEVEL\tCODE\tPATH\tMESSAGE")
	fmt.Fprintln(tw, "-----\t----\t----\t-------")
	for _, finding := range report.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", finding.Level, finding.Code, finding.Path, finding.Message)
	}
	_ = tw.Flush()
}

func parsePlanAuditDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		parsed, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, fmt.Errorf("parse stale-after %q: %w", value, err)
		}
		return parsed * 24, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse stale-after %q: %w", value, err)
	}
	return duration, nil
}
