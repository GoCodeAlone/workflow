package sdk

import (
	"strings"
	"testing"
)

func TestResolveBuildVersion_ReleaseDeclaredPassThrough(t *testing.T) {
	got := ResolveBuildVersion("v1.2.3")
	if got != "v1.2.3" {
		t.Errorf("got %q, want v1.2.3", got)
	}
}

func TestResolveBuildVersion_EmptyFallsToBuildInfo(t *testing.T) {
	got := ResolveBuildVersion("")
	if !strings.HasPrefix(got, "(devel)") {
		t.Errorf("got %q, want prefix (devel) for empty declared", got)
	}
}

func TestResolveBuildVersion_DevSentinelFallsToBuildInfo(t *testing.T) {
	got := ResolveBuildVersion("dev")
	if !strings.HasPrefix(got, "(devel)") {
		t.Errorf("got %q, want prefix (devel) for 'dev' sentinel", got)
	}
}

func TestResolveBuildVersion_DevelSentinelFallsToBuildInfo(t *testing.T) {
	got := ResolveBuildVersion("(devel)")
	if !strings.HasPrefix(got, "(devel)") {
		t.Errorf("got %q, want prefix (devel) for '(devel)' sentinel", got)
	}
}

func TestResolveBuildVersion_NonStandardDeclaredPassThrough(t *testing.T) {
	got := ResolveBuildVersion("v0.0.0-rc.1+build.42")
	if got != "v0.0.0-rc.1+build.42" {
		t.Errorf("got %q, want pass-through of non-sentinel declared", got)
	}
}
