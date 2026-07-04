// Package lockio provides a go/analysis-style checker that flags a lock (or
// lease) held across a store I/O round trip. It generalizes a checker
// originally written directly inside a Workflow host app's test suite to
// enforce "no lock held across store I/O" for one specific set of method
// names; this package makes the same detection logic reusable by any host
// app, parameterized entirely by method name.
//
// Background: a staging incident traced to a lock held while a slow store
// write ran, starving unrelated requests behind it, and was made worse
// because the two symptom shapes it produced — a platform edge proxy's own
// HTML error page, and the app's own structured error once contention
// resolved — looked identical from the outside for hours. See
// https://github.com/GoCodeAlone/workflow-compute
// (docs/plans/2026-07-04-durable-mutation-lifecycle-design.md, "Upstream
// Guards" U2) for the full incident writeup that motivated this package.
//
// The checker enforces two independent rules, both driven purely by method
// name — no lock-state data-flow tracking, no type information:
//
//  1. Restricted I/O (Class ClassRestrictedIO): a call to one of
//     Config.RestrictedIOMethods is flagged unless the enclosing function is
//     listed in Config.PermanentIOCallers. These methods are assumed to do
//     I/O directly under the assumption that the caller already holds the
//     lock; only sanctioned wrapper functions (or storage backends with no
//     separable lock/I-O split) should call them.
//  2. Uncovered return paths (Class ClassUncoveredReturnPath): once a
//     function calls one of Config.AcquireMethods using the idiom
//     `v, err := recv.Acquire(...); if err != nil { return ... }`, every
//     return path reachable afterward must pass through a call to one of
//     Config.ReleaseMethods before returning, unless the enclosing function
//     is listed in Config.PermanentReturnPathFunctions. A release call
//     inside an unawaited `go func(){ ... }()` does not count: it returns
//     with no guarantee of running before (or ever, relative to) the
//     enclosing function's return.
package lockio

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"

	"golang.org/x/tools/go/analysis"
)

// Class identifies which rule a Violation broke.
type Class string

const (
	// ClassRestrictedIO marks a call to a RestrictedIOMethods entry from a
	// function not listed in PermanentIOCallers.
	ClassRestrictedIO Class = "restricted-io"
	// ClassUncoveredReturnPath marks a return path reachable after an
	// AcquireMethods call that is not covered by a synchronous
	// ReleaseMethods call.
	ClassUncoveredReturnPath Class = "uncovered-return-path"
)

// Config parameterizes the checker for one host codebase's lock/lease and
// store-I/O method names. All four method-name sets are matched against the
// selector name of a call expression (e.g. "Save" for `a.Save()`); receiver
// type is not checked, so pick names specific enough to a single codebase's
// convention to avoid false matches on unrelated types.
type Config struct {
	// AcquireMethods are method names whose call, recognized only via the
	// idiom `v, err := recv.Method(...); if err != nil { return ... }`,
	// begins a critical section that must be closed by a ReleaseMethods call
	// on every path reachable afterward.
	AcquireMethods map[string]bool

	// ReleaseMethods are method names that legitimately end a critical
	// section opened by an AcquireMethods call — by committing, aborting, or
	// otherwise releasing the lock/lease.
	ReleaseMethods map[string]bool

	// RestrictedIOMethods are method names that perform store I/O directly,
	// assuming a lock/lease is already held. A RestrictedIOMethods entry
	// that should also satisfy the ReleaseMethods requirement (i.e. calling
	// it does complete the critical section, even when the call site itself
	// is separately flagged as unsanctioned) must be listed in both maps;
	// the two sets are independent by design.
	RestrictedIOMethods map[string]bool

	// PermanentIOCallers are function/method names allowed to call
	// RestrictedIOMethods directly: structural exceptions such as the
	// sanctioned release helper itself, or a storage backend with no
	// separate lock/I-O split — not migration debt, not expected to shrink.
	PermanentIOCallers map[string]bool

	// PermanentReturnPathFunctions are function names exempt from the
	// uncovered-return-path check: verified safe by means outside this
	// checker's single-function, block-scoped reach (for example, a boolean
	// flag set immediately before an unconditional release call two
	// statements earlier that the checker cannot correlate back).
	PermanentReturnPathFunctions map[string]bool
}

func (c Config) isAcquire(name string) bool        { return name != "" && c.AcquireMethods[name] }
func (c Config) isRelease(name string) bool        { return name != "" && c.ReleaseMethods[name] }
func (c Config) isRestrictedIO(name string) bool   { return name != "" && c.RestrictedIOMethods[name] }
func (c Config) permanentIOCaller(n string) bool   { return c.PermanentIOCallers[n] }
func (c Config) permanentReturnPath(n string) bool { return c.PermanentReturnPathFunctions[n] }

// Violation is one finding from FindViolations (or an Analyzer built with
// NewAnalyzer).
type Violation struct {
	Pos   token.Pos
	File  string // base name, e.g. "server.go"
	Func  string // enclosing function/method name
	Class Class
}

// Key returns a stable identity for the violation independent of line
// number, in the form "file|class|func". Host apps can use this to build a
// shrink-only allowlist test the way workflow-compute's original guard test
// did: fail loudly both on an undocumented new violation and on a stale
// allowlist entry that no longer violates.
func (v Violation) Key() string {
	return v.File + "|" + string(v.Class) + "|" + v.Func
}

// FindViolations scans already-parsed files sharing fset for both violation
// classes using cfg. Files may come from a single package or be scanned
// independently; each file is analyzed on its own (no cross-file call
// resolution), matching the original checker's per-file design.
func FindViolations(fset *token.FileSet, files []*ast.File, cfg Config) []Violation {
	var violations []Violation
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			fileName := filepath.Base(fset.Position(fn.Pos()).Filename)

			if callsRestrictedIO(fn.Body, cfg) && !cfg.permanentIOCaller(name) {
				violations = append(violations, Violation{
					Pos: fn.Pos(), File: fileName, Func: name, Class: ClassRestrictedIO,
				})
			}

			if cfg.isAcquire(name) {
				continue // the acquire method's own definition is not a call site
			}
			if cfg.permanentReturnPath(name) {
				continue
			}
			if acquireReturnPathUncovered(fn, cfg) {
				violations = append(violations, Violation{
					Pos: fn.Pos(), File: fileName, Func: name, Class: ClassUncoveredReturnPath,
				})
			}
		}
	}
	return violations
}

// NewAnalyzer builds a go/analysis.Analyzer named name that reports both
// violation classes from cfg via pass.Reportf at each violation's function
// declaration position.
func NewAnalyzer(name, doc string, cfg Config) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: name,
		Doc:  doc,
		Run: func(pass *analysis.Pass) (interface{}, error) {
			for _, v := range FindViolations(pass.Fset, pass.Files, cfg) {
				pass.Reportf(v.Pos, "%s", violationMessage(v))
			}
			return nil, nil
		},
	}
}

func violationMessage(v Violation) string {
	switch v.Class {
	case ClassRestrictedIO:
		return fmt.Sprintf("%s: %s calls a restricted store I/O method directly; only a permanently-allowlisted caller may do this", v.Class, v.Func)
	case ClassUncoveredReturnPath:
		return fmt.Sprintf("%s: %s has a return path after acquiring a lock/lease that is not covered by a release call", v.Class, v.Func)
	default:
		return fmt.Sprintf("%s: %s", v.Class, v.Func)
	}
}
