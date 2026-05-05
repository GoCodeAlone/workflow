package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestResolveTag_EnvVar(t *testing.T) {
	t.Setenv("MY_IMAGE_TAG", "v1.2.3")
	entries := []config.TagFromEntry{{Env: "MY_IMAGE_TAG"}}
	got := ResolveTag(entries, "fallback")
	if got != "v1.2.3" {
		t.Errorf("want v1.2.3, got %q", got)
	}
}

func TestResolveTag_EnvVarEmpty_FallsThrough(t *testing.T) {
	// EMPTY_TAG_VAR is not set → should fall through to fallback.
	entries := []config.TagFromEntry{{Env: "EMPTY_TAG_VAR_XYZ_NOTSET"}}
	got := ResolveTag(entries, "default-tag")
	if got != "default-tag" {
		t.Errorf("want default-tag, got %q", got)
	}
}

func TestResolveTag_Command(t *testing.T) {
	entries := []config.TagFromEntry{{Command: "echo fixed-tag"}}
	got := ResolveTag(entries, "fallback")
	if got != "fixed-tag" {
		t.Errorf("want fixed-tag, got %q", got)
	}
}

func TestResolveTag_EnvBeforeCommand(t *testing.T) {
	t.Setenv("PRIORITY_TAG", "env-wins")
	entries := []config.TagFromEntry{
		{Env: "PRIORITY_TAG"},
		{Command: "echo cmd-tag"},
	}
	got := ResolveTag(entries, "fallback")
	if got != "env-wins" {
		t.Errorf("want env-wins (env takes priority), got %q", got)
	}
}

func TestResolveTag_NilEntries_ReturnsFallback(t *testing.T) {
	got := ResolveTag(nil, "my-fallback")
	if got != "my-fallback" {
		t.Errorf("want my-fallback, got %q", got)
	}
}

func TestResolveTag_CommandFails_FallsThrough(t *testing.T) {
	entries := []config.TagFromEntry{
		{Command: "exit 1"},
		{Env: "NONEXISTENT_RESOLVE_TAG_ENV"},
	}
	got := ResolveTag(entries, "safe-fallback")
	if got != "safe-fallback" {
		t.Errorf("want safe-fallback, got %q", got)
	}
}
