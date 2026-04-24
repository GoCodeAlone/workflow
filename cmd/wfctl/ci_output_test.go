package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectCIProvider_GitHub(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITLAB_CI", "")
	t.Setenv("JENKINS_HOME", "")
	t.Setenv("CIRCLECI", "")
	e := detectCIProvider()
	if _, ok := e.(githubEmitter); !ok {
		t.Fatalf("expected githubEmitter, got %T", e)
	}
}

func TestDetectCIProvider_GitLab(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITLAB_CI", "true")
	e := detectCIProvider()
	if _, ok := e.(gitlabEmitter); !ok {
		t.Fatalf("expected gitlabEmitter, got %T", e)
	}
}

func TestDetectCIProvider_Default(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITLAB_CI", "")
	t.Setenv("JENKINS_HOME", "")
	t.Setenv("CIRCLECI", "")
	e := detectCIProvider()
	if _, ok := e.(plainEmitter); !ok {
		t.Fatalf("expected plainEmitter, got %T", e)
	}
}

func TestGithubEmitter_GroupMarkers(t *testing.T) {
	var buf bytes.Buffer
	e := githubEmitter{}
	e.GroupStart(&buf, "Troubleshoot: bmw-staging")
	buf.WriteString("hello\n")
	e.GroupEnd(&buf)
	out := buf.String()
	if !strings.Contains(out, "::group::Troubleshoot: bmw-staging\n") {
		t.Errorf("missing ::group:: marker: %q", out)
	}
	if !strings.Contains(out, "::endgroup::\n") {
		t.Errorf("missing ::endgroup:: marker: %q", out)
	}
}

func TestGitlabEmitter_GroupMarkers(t *testing.T) {
	var buf bytes.Buffer
	e := gitlabEmitter{}
	e.GroupStart(&buf, "my-section")
	e.GroupEnd(&buf)
	out := buf.String()
	if !strings.Contains(out, "section_start") {
		t.Errorf("missing section_start: %q", out)
	}
	if !strings.Contains(out, "section_end") {
		t.Errorf("missing section_end: %q", out)
	}
}

func TestPlainEmitter_UsesDashSeparators(t *testing.T) {
	var buf bytes.Buffer
	e := plainEmitter{}
	e.GroupStart(&buf, "section")
	e.GroupEnd(&buf)
	out := buf.String()
	if !strings.Contains(out, "--- section ---") {
		t.Errorf("expected --- section --- header, got %q", out)
	}
}
