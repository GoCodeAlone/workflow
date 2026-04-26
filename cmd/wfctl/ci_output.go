package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// CIGroupEmitter wraps output with CI-provider-specific group markers so
// long outputs (like diagnostics) render as collapsible sections in the UI.
type CIGroupEmitter interface {
	GroupStart(w io.Writer, name string)
	GroupEnd(w io.Writer)
	// SummaryPath returns the path to append Markdown summary content, or
	// "" if this CI provider has no step-summary concept.
	SummaryPath() string
}

// detectCIProvider inspects env vars and returns the appropriate emitter.
func detectCIProvider() CIGroupEmitter {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		return githubEmitter{summaryPath: githubStepSummaryPath()}
	case os.Getenv("GITLAB_CI") == "true":
		return &gitlabEmitter{}
	case os.Getenv("JENKINS_HOME") != "":
		return jenkinsEmitter{}
	case os.Getenv("CIRCLECI") == "true":
		return circleCIEmitter{}
	default:
		return plainEmitter{}
	}
}

func githubStepSummaryPath() string {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return ""
	}
	if runningAsGoTest() && os.Getenv("WFCTL_ALLOW_TEST_STEP_SUMMARY") != "true" {
		return ""
	}
	return path
}

func runningAsGoTest() bool {
	return flag.Lookup("test.v") != nil
}

// --- GitHub Actions ---

type githubEmitter struct{ summaryPath string }

func (g githubEmitter) GroupStart(w io.Writer, name string) {
	fmt.Fprintf(w, "::group::%s\n", name)
}
func (g githubEmitter) GroupEnd(w io.Writer) { fmt.Fprintln(w, "::endgroup::") }
func (g githubEmitter) SummaryPath() string  { return g.summaryPath }

// --- GitLab CI ---

// gitlabEmitter stores the section ID generated in GroupStart so that
// GroupEnd can emit a matching ID — GitLab requires identical IDs on both
// markers to close the fold correctly.
type gitlabEmitter struct {
	sectionID string
}

func (g *gitlabEmitter) GroupStart(w io.Writer, name string) {
	g.sectionID = fmt.Sprintf("wfctl_%d", time.Now().UnixNano())
	fmt.Fprintf(w, "\x1b[0Ksection_start:%d:%s\r\x1b[0K%s\n", time.Now().Unix(), g.sectionID, name)
}
func (g *gitlabEmitter) GroupEnd(w io.Writer) {
	fmt.Fprintf(w, "\x1b[0Ksection_end:%d:%s\r\x1b[0K\n", time.Now().Unix(), g.sectionID)
}
func (g *gitlabEmitter) SummaryPath() string { return "" }

// --- Jenkins ---

type jenkinsEmitter struct{}

func (j jenkinsEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (j jenkinsEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (j jenkinsEmitter) SummaryPath() string                 { return "" }

// --- CircleCI ---

type circleCIEmitter struct{}

func (c circleCIEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (c circleCIEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (c circleCIEmitter) SummaryPath() string                 { return "" }

// --- Plain (default) ---

type plainEmitter struct{}

func (p plainEmitter) GroupStart(w io.Writer, name string) { fmt.Fprintf(w, "\n--- %s ---\n", name) }
func (p plainEmitter) GroupEnd(w io.Writer)                { fmt.Fprintln(w, "--- end ---") }
func (p plainEmitter) SummaryPath() string                 { return "" }
