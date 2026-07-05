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

// The following three fixtures are modeled on a cross-repo sweep (team-lead,
// 2026-07-04; full inventory to land at workflow-compute's
// docs/validation/2026-07-04-ecosystem-lock-io-scan.md) that found the same
// lock-across-I/O shape workflow-compute hit in at least nine instances
// across the ecosystem beyond workflow-compute itself. Each fixture uses a
// plain sync.Mutex Lock()/defer Unlock() — not the error-returning
// AcquireLease idiom the Class B fixtures above exercise — to prove Class A
// (a direct call to a restricted I/O method outside a sanctioned caller)
// generalizes to that shape too: Class A never reasoned about lock state in
// the first place, so it flags these the same way it flags workflow-compute's
// saveLocked, independent of whether Lock()/Unlock() or an error-returning
// acquire surrounds the call. Method/field names are anonymized generics,
// not the real identifiers from the swept repos.

func TestRestrictedIOEcosystemShapeSchedulerShutdownHoldsLockAcrossSave(t *testing.T) {
	// A scheduler's Stop() holds its lock across a slow persistence save.
	fset, f := parseFixture(t, `
func (s *Scheduler) Stop() error {
	s.schedulerLock.Lock()
	defer s.schedulerLock.Unlock()
	return s.persistenceHandler.rawWrite()
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "Stop", ClassRestrictedIO) {
		t.Fatalf("checker bug: ecosystem-shaped Stop() was not flagged; violations=%+v", violations)
	}
}

func TestRestrictedIOEcosystemShapePolicySaveContendsWithHotPathRLock(t *testing.T) {
	// An authz module holds its mutex across a policy save while every
	// request-path Enforce() call takes the same mutex to read — contention
	// on every request during a save.
	fset, f := parseFixture(t, `
func (m *Module) SavePolicy() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enforcer.rawWrite()
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "SavePolicy", ClassRestrictedIO) {
		t.Fatalf("checker bug: ecosystem-shaped SavePolicy() was not flagged; violations=%+v", violations)
	}
}

func TestRestrictedIOEcosystemShapeProviderDriverListContendsWithHealthCheck(t *testing.T) {
	// A cloud provider driver holds its deployment mutex across a slow list
	// API call while a health check contends for the same mutex on every
	// tick.
	fset, f := parseFixture(t, `
func (d *Driver) refreshDeployments() error {
	d.deploymentMu.Lock()
	defer d.deploymentMu.Unlock()
	return d.rawWrite()
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "refreshDeployments", ClassRestrictedIO) {
		t.Fatalf("checker bug: ecosystem-shaped refreshDeployments() was not flagged; violations=%+v", violations)
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

// The following two fixtures are regressions for a Copilot review finding on
// the upstream PR: walkBlock previously didn't inspect a for-loop's or
// switch's header expressions (Init/Cond/Post, Tag, Assign) for acquire/
// release calls before recursing into the loop/switch body, so a return
// inside that body could go unflagged even though the critical section began
// in the header. Both fixtures put the AcquireLease call in the header and a
// bare return in the body to prove the header now extends the running state
// into the body, matching how IfStmt's Init/Cond already worked.

func TestForLoopHeaderAcquireIsDetectedInBody(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badForInitAcquireLeaksInBody() error {
	for a.AcquireLease(); true; {
		return errors.New("nope")
	}
	return nil
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badForInitAcquireLeaksInBody", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: acquire call in a for-loop's Init was not detected inside the loop body; violations=%+v", violations)
	}
}

// TestForLoopPostReleaseDoesNotMaskFirstIterationLeak is a regression guard
// for a real gap found in quality review of a downstream backport of this
// exact ForStmt case (workflow-compute's local mutation-lifecycle guard
// test): unlike Init/Cond (and RangeStmt's X, SwitchStmt's Tag,
// TypeSwitchStmt's Assign — all of which execute unconditionally before any
// entry into their body), a for-loop's Post clause only runs after a
// completed, non-returning iteration — never before the body's first entry.
// Folding a release call found in Post into releaseSeen before recursing
// into Body made the checker believe the body's first-iteration return path
// was already covered when it never is on that iteration — a demonstrated
// false negative in the one direction this checker's design explicitly
// favors avoiding (over-flagging is acceptable, under-flagging is not).
func TestForLoopPostReleaseDoesNotMaskFirstIterationLeak(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badForPostReleaseMasksFirstIterationLeak() error {
	for i := 0; i < 3; a.Commit(0) {
		a.AcquireLease()
		if somethingWrong() {
			return errors.New("nope")
		}
		i++
	}
	return nil
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badForPostReleaseMasksFirstIterationLeak", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: a release call in a for-loop's Post clause masked a genuine leak on the body's first-iteration return path; violations=%+v", violations)
	}
}

func TestSwitchHeaderAcquireIsDetectedInBody(t *testing.T) {
	fset, f := parseFixture(t, `
func (a *App) badSwitchInitAcquireLeaksInBody() error {
	switch a.AcquireLease(); {
	case somethingWrong():
		return errors.New("nope")
	}
	return a.Commit(nil)
}
`)
	violations := FindViolations(fset, []*ast.File{f}, testConfig())
	if !hasViolation(violations, "badSwitchInitAcquireLeaksInBody", ClassUncoveredReturnPath) {
		t.Fatalf("checker bug: acquire call in a switch's Init was not detected inside a case body; violations=%+v", violations)
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
