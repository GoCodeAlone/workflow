package lockio

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// testConfig deliberately uses method names that differ from the
// workflow-compute checker this package generalizes (which used
// beginMutationLocked/saveLocked/saveMutationAndUnlockLocked/abort*). Using a
// distinct vocabulary here proves the detection logic is driven by Config,
// not by any hardcoded name.
func testConfig() Config {
	return Config{
		AcquireMethods:      map[string]bool{"AcquireLease": true},
		ReleaseMethods:      map[string]bool{"Commit": true, "Abort": true, "rawWrite": true},
		RestrictedIOMethods: map[string]bool{"rawWrite": true},
		PermanentIOCallers:  map[string]bool{"Commit": true},
	}
}

func parseFixture(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", "package fixture\n\n"+src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fset, f
}

func TestRestrictedIOOutsidePermanentCallerIsFlagged(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badRawWriteCaller() error {
	return a.rawWrite()
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badRawWriteCaller", ClassRestrictedIO) {
		t.Fatalf("checker bug: known-bad restricted-IO caller was not flagged; violations=%+v", violations)
	}
}

func TestRestrictedIOFromPermanentCallerIsNotFlagged(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) Commit(before int) error {
	return a.rawWrite()
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if hasViolation(violations, "Commit", ClassRestrictedIO) {
		t.Fatalf("checker false positive: permanently-allowlisted caller was flagged; violations=%+v", violations)
	}
}

func TestAcquireReturnPathMissingReleaseIsFlagged(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badAcquireReturnPath() error {
	before, err := a.AcquireLease()
	if err != nil {
		return err
	}
	if somethingWrong() {
		a.mu.Unlock()
		return errors.New("nope")
	}
	return a.Commit(before)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badAcquireReturnPath", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: known-bad uncovered return path was not flagged; violations=%+v", violations)
	}
}

func TestAcquireFullyCoveredPathsAreNotFlagged(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) goodAcquireReturnPath() error {
	before, err := a.AcquireLease()
	if err != nil {
		return err
	}
	if somethingWrong() {
		return a.Abort(errors.New("nope"))
	}
	return a.Commit(before)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if hasViolation(violations, "goodAcquireReturnPath", ClassUncoveredReturnPath) {
		t.Fatalf("checker false positive: known-good return path was flagged; violations=%+v", violations)
	}
}

func TestSecondCriticalSectionNotCoveredByFirstSectionsRelease(t *testing.T) {
	// Regression fixture ported from the original workflow-compute checker:
	// a function that opens and fully closes one critical section, then
	// opens a second one that leaks on its own no-work exit, must still be
	// flagged — a release call that closed the first section must not be
	// mistaken for covering the second.
	fset, f := parseFixture(t, `
func (a *App) badSecondSectionLeak(now int) error {
	before, err := a.AcquireLease()
	if err != nil {
		return err
	}
	if err := a.rawWrite(); err != nil {
		return err
	}
	before2, err2 := a.AcquireLease()
	if err2 != nil {
		return err2
	}
	if !somethingChanged(before2) {
		return nil
	}
	return a.Commit(before)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badSecondSectionLeak", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: second-critical-section no-work leak was not flagged; violations=%+v", violations)
	}
}

func TestAsyncReleaseCallDoesNotCoverReturnPath(t *testing.T) {
	// Regression fixture for the goroutine-opaque marker-traversal bug found
	// in the original checker's quality review: a release call fired only
	// inside an unawaited `go func(){...}()` returns no guarantee it runs
	// before (or even after) the enclosing function's return, so it must not
	// be mistaken for covering that return path. This is a plausible real
	// mistake ("clean up in the background, don't block the response"), not
	// a hypothetical one.
	fset, f := parseFixture(t, `
func (a *App) badAsyncAbort() error {
	before, err := a.AcquireLease()
	if err != nil {
		return err
	}
	if somethingWrong() {
		go func() { a.Abort(nil) }()
		return errors.New("nope")
	}
	return a.Commit(before)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badAsyncAbort", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: return path covered only by an async (go func) release call was not flagged; violations=%+v", violations)
	}
}

func TestPermanentReturnPathFunctionIsExempt(t *testing.T) {
	cfg := testConfig()
	cfg.PermanentReturnPathFunctions = map[string]bool{"exemptFunc": true}
	fset, f := parseFixture(t, `
func (a *App) exemptFunc() error {
	before, err := a.AcquireLease()
	if err != nil {
		return err
	}
	if changed {
		return nil
	}
	return a.Commit(before)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, cfg)
	if hasViolation(violations, "exemptFunc", ClassUncoveredReturnPath) {
		t.Fatalf("checker false positive: permanent-return-path function was flagged; violations=%+v", violations)
	}
}

func TestViolationKeyFormat(t *testing.T) {
	v := Violation{File: "server.go", Class: ClassRestrictedIO, Func: "badFn"}
	if got, want := v.Key(), "server.go|restricted-io|badFn"; got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func TestFindViolationsScansMultipleFilesIndependently(t *testing.T) {
	fset := token.NewFileSet()
	f1, err := parser.ParseFile(fset, "a.go", `package fixture

func (a *App) badInFileA() error {
	return a.rawWrite()
}
`, 0)
	if err != nil {
		t.Fatalf("parse a.go: %v", err)
	}
	f2, err := parser.ParseFile(fset, "b.go", `package fixture

func (a *App) badInFileB() error {
	return a.rawWrite()
}
`, 0)
	if err != nil {
		t.Fatalf("parse b.go: %v", err)
	}
	violations := FindViolations(fset, []*ast.File{f1, f2}, testConfig())
	if !hasViolation(violations, "badInFileA", ClassRestrictedIO) || !hasViolation(violations, "badInFileB", ClassRestrictedIO) {
		t.Fatalf("expected violations in both files; got %+v", violations)
	}
	for _, v := range violations {
		wantFile := map[string]string{"badInFileA": "a.go", "badInFileB": "b.go"}[v.Func]
		if v.File != wantFile {
			t.Errorf("violation for %s has File=%q, want %q", v.Func, v.File, wantFile)
		}
	}
}

// TestAnalyzerReportsViolations proves the go/analysis.Analyzer wiring
// itself: NewAnalyzer's Run function must invoke pass.Reportf (via
// pass.Report) for a known-bad fixture, so any host app can plug this
// checker into golangci-lint, go vet, or a standalone multichecker binary,
// not just call FindViolations directly from a test.
func TestAnalyzerReportsViolations(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badRawWriteCaller() error {
	return a.rawWrite()
}
`)
	var diags []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{f},
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d)
		},
	}
	analyzer := NewAnalyzer("lockio", "test analyzer", testConfig())
	if _, err := analyzer.Run(pass); err != nil {
		t.Fatalf("analyzer.Run: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic from a known-bad fixture")
	}
	found := false
	for _, d := range diags {
		if d.Message != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("diagnostics were reported with empty messages")
	}
}

func hasViolation(violations []Violation, fn string, class Class) bool {
	for _, v := range violations {
		if v.Func == fn && v.Class == class {
			return true
		}
	}
	return false
}
