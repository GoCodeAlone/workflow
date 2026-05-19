package main

import (
	"strings"
	"testing"
)

func TestFormatPluginSearchResults_HeaderColumns(t *testing.T) {
	out := formatPluginSearchResults(nil)
	for _, want := range []string{"NAME", "VERSION", "TIER", "STATUS", "SOURCE", "DESCRIPTION"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q in output:\n%s", want, out)
		}
	}
}

func TestFormatPluginSearchResults_StatusFallback(t *testing.T) {
	rows := []PluginSearchResult{
		{PluginSummary: PluginSummary{Name: "a", Version: "v1", Tier: "core", Status: "", Description: "no status"}, Source: "src"},
		{PluginSummary: PluginSummary{Name: "b", Version: "v2", Tier: "community", Status: "verified", Description: "with status"}, Source: "src"},
	}
	out := formatPluginSearchResults(rows)
	if !strings.Contains(out, " - ") {
		t.Errorf("expected '-' fallback for empty Status, got:\n%s", out)
	}
	if !strings.Contains(out, "verified") {
		t.Errorf("expected 'verified' in output, got:\n%s", out)
	}
}

func TestFormatPluginSearchResults_DescriptionTruncation(t *testing.T) {
	long := strings.Repeat("x", 80)
	rows := []PluginSearchResult{{
		PluginSummary: PluginSummary{Name: "p", Version: "v", Tier: "core", Status: "verified", Description: long},
		Source:        "src",
	}}
	out := formatPluginSearchResults(rows)
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncated description ending with '...', got:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Errorf("expected description to be truncated, but full string present:\n%s", out)
	}
}

func TestFormatPluginSearchResults_OrderAndFields(t *testing.T) {
	rows := []PluginSearchResult{
		{PluginSummary: PluginSummary{Name: "first-plugin", Version: "1.0.0", Tier: "core", Status: "verified", Description: "first"}, Source: "main"},
		{PluginSummary: PluginSummary{Name: "second-plugin", Version: "2.0.0", Tier: "community", Status: "experimental", Description: "second"}, Source: "custom"},
	}
	out := formatPluginSearchResults(rows)
	firstIdx := strings.Index(out, "first-plugin")
	secondIdx := strings.Index(out, "second-plugin")
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("expected both rows in output, got:\n%s", out)
	}
	if firstIdx >= secondIdx {
		t.Errorf("expected first-plugin before second-plugin, got firstIdx=%d secondIdx=%d", firstIdx, secondIdx)
	}
	for _, want := range []string{"first-plugin", "1.0.0", "core", "verified", "main", "first",
		"second-plugin", "2.0.0", "community", "experimental", "custom", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, missing. Output:\n%s", want, out)
		}
	}
}
