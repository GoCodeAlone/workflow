// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// captureMode swaps modes[name] for a recorder that captures the Options
// it was invoked with. Returns a teardown func and a pointer to the
// captured Options (nil until the mode actually runs).
func captureMode(t *testing.T, name string) (*Options, func()) {
	t.Helper()
	orig, ok := modes[name]
	if !ok {
		t.Fatalf("captureMode: unknown mode %q", name)
	}
	captured := &Options{}
	called := false
	modes[name] = func(args []string, opts *Options, stdout, stderr io.Writer) int {
		*captured = *opts
		called = true
		_ = args
		_ = stdout
		_ = stderr
		return 0
	}
	return captured, func() {
		modes[name] = orig
		if !called {
			t.Errorf("captureMode(%q): mode never invoked", name)
		}
	}
}

func TestRun_NoArgs_ExitsWithUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "usage:") {
		t.Errorf("expected usage in output; got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for _, mode := range []string{"refactor-plan", "refactor-apply", "add-validate-plan", "lint"} {
		if !strings.Contains(combined, mode) {
			t.Errorf("usage should list mode %q; got %q", mode, combined)
		}
	}
}

func TestRun_HelpFlag_ExitsZero(t *testing.T) {
	for _, flag := range []string{"-h", "--help", "help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{flag}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if !strings.Contains(stdout.String()+stderr.String(), "usage:") {
				t.Errorf("expected usage in output; got stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRun_UnknownMode_Exits2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"frobnicate"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown mode") {
		t.Errorf("expected 'unknown mode' in stderr; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "frobnicate") {
		t.Errorf("expected unknown mode name in stderr; got %q", stderr.String())
	}
}

func TestRun_KnownModes_DispatchToHandlers(t *testing.T) {
	for _, mode := range []string{"refactor-plan", "refactor-apply", "add-validate-plan", "lint"} {
		t.Run(mode, func(t *testing.T) {
			opts, teardown := captureMode(t, mode)
			defer teardown()
			var stdout, stderr bytes.Buffer
			code := run([]string{mode}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr.String())
			}
			if !opts.DryRun {
				t.Errorf("DryRun should default to true")
			}
			if opts.Fix {
				t.Errorf("Fix should default to false")
			}
		})
	}
}

func TestRun_DryRunDefaultsTrue(t *testing.T) {
	opts, teardown := captureMode(t, "lint")
	defer teardown()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"lint"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !opts.DryRun {
		t.Errorf("DryRun should default to true; got false")
	}
}

func TestRun_FixOptsIntoMutation(t *testing.T) {
	opts, teardown := captureMode(t, "refactor-plan")
	defer teardown()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"refactor-plan", "-fix"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !opts.Fix {
		t.Errorf("Fix should be true when -fix passed")
	}
	if opts.DryRun {
		t.Errorf("DryRun should be false when -fix passed (mutation opt-in)")
	}
}

// TestSkipMarker_LiteralPinned guards against drift in the canonical marker
// string. Plan rev2 (line 2400) unifies all four modes on a single marker
// specifically to prevent mismatched-marker silent-no-op surfaces. T8.3-T8.5
// will import SkipMarker for actual parsing; pinning the literal here means
// any rename or typo trips this test before it reaches a mode parser.
func TestSkipMarker_LiteralPinned(t *testing.T) {
	const want = "// wfctl:skip-iac-codemod"
	if SkipMarker != want {
		t.Errorf("canonical marker drift: SkipMarker = %q, want %q", SkipMarker, want)
	}
}

func TestUsage_MentionsSkipMarker(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	if !strings.Contains(buf.String(), SkipMarker) {
		t.Errorf("usage must mention canonical marker %q; got:\n%s", SkipMarker, buf.String())
	}
}

func TestUsage_DocumentsFlagOrdering(t *testing.T) {
	// Reviewer finding #2: stdlib flag stops at the first non-flag arg, so
	// `iac-codemod refactor-plan /path -fix` silently drops -fix. The
	// constraint is documented in usage so future maintainers know the
	// flag-after-path failure is intentional, not a parser bug.
	var buf bytes.Buffer
	usage(&buf)
	if !strings.Contains(buf.String(), "Flags must precede paths") {
		t.Errorf("usage must document flag-ordering constraint; got:\n%s", buf.String())
	}
}

func TestRun_FlagAfterPath_SilentlyTreatedAsPositional(t *testing.T) {
	// Documents (and pins) the stdlib flag-pkg behavior: once a non-flag arg
	// appears, every subsequent token — including what looks like a flag —
	// is forwarded to the mode as a positional. The mode receives the raw
	// token; -fix does NOT take effect on the run's Options.
	var capturedOpts Options
	var capturedArgs []string
	orig := modes["refactor-plan"]
	modes["refactor-plan"] = func(args []string, opts *Options, stdout, stderr io.Writer) int {
		capturedOpts = *opts
		capturedArgs = append([]string{}, args...)
		return 0
	}
	defer func() { modes["refactor-plan"] = orig }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"refactor-plan", "/path", "-fix"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if capturedOpts.Fix {
		t.Errorf("Fix should be false when -fix appears AFTER positional (stdlib flag stops at first non-flag); got Fix=true")
	}
	if capturedOpts.DryRun != true {
		t.Errorf("DryRun should remain default true when -fix is silently dropped; got %v", capturedOpts.DryRun)
	}
	wantArgs := []string{"/path", "-fix"}
	if len(capturedArgs) != len(wantArgs) {
		t.Fatalf("got args %v, want %v", capturedArgs, wantArgs)
	}
	for i := range wantArgs {
		if capturedArgs[i] != wantArgs[i] {
			t.Errorf("arg[%d] = %q, want %q", i, capturedArgs[i], wantArgs[i])
		}
	}
}

func TestRun_PositionalArgsForwardedToMode(t *testing.T) {
	var gotArgs []string
	orig := modes["lint"]
	modes["lint"] = func(args []string, opts *Options, stdout, stderr io.Writer) int {
		gotArgs = append([]string{}, args...)
		return 0
	}
	defer func() { modes["lint"] = orig }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"lint", "-dry-run", "/path/to/plugin", "/another/path"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, stderr.String())
	}
	wantArgs := []string{"/path/to/plugin", "/another/path"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("got args %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Errorf("arg[%d] = %q, want %q", i, gotArgs[i], wantArgs[i])
		}
	}
}
