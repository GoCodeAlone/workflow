package lockio

import (
	"go/ast"
	"go/token"
)

// selectorCallName returns a call expression's method/function name, e.g.
// "Save" for a.Save(). Bare identifier calls (returning "") never match any
// configured method name, since Config sets only ever contain method-style
// names.
func selectorCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
}

// nodeContainsCall reports whether any call expression within node matches
// isMatch. node may be nil (e.g. an absent else-branch), in which case it
// reports false.
func nodeContainsCall(node ast.Node, isMatch func(string) bool) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isMatch(selectorCallName(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

// nodeContainsSyncCall is like nodeContainsCall but treats *ast.GoStmt as
// opaque, not descending into it. A call launched via `go func(){...}()`
// returns immediately with no guarantee it runs before (or even after) the
// enclosing function's return, so it must not count as covering a return
// path — unlike nodeContainsCall, which is still used unmodified for the
// restricted-I/O check (a direct call is unsafe regardless of sync/async
// context) and for acquire-call detection.
func nodeContainsSyncCall(node ast.Node, isMatch func(string) bool) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.GoStmt); ok {
			return false // don't descend into an async launch
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isMatch(selectorCallName(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callsRestrictedIO(node ast.Node, cfg Config) bool {
	return nodeContainsCall(node, cfg.isRestrictedIO)
}

func containsAcquireCall(node ast.Node, cfg Config) bool {
	return nodeContainsCall(node, cfg.isAcquire)
}

// acquireCallErrVar reports whether stmt is `x, err := a.Acquire()` for some
// Acquire in cfg.AcquireMethods, returning the name bound to the call's error
// result.
func acquireCallErrVar(stmt ast.Stmt, cfg Config) (string, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
		return "", false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || !cfg.isAcquire(selectorCallName(call)) {
		return "", false
	}
	ident, ok := assign.Lhs[len(assign.Lhs)-1].(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// isErrNilCheck reports whether cond is `errVar != nil` (or `nil != errVar`).
func isErrNilCheck(cond ast.Expr, errVar string) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	isVar := func(e ast.Expr) bool {
		ident, ok := e.(*ast.Ident)
		return ok && ident.Name == errVar
	}
	isNil := func(e ast.Expr) bool {
		ident, ok := e.(*ast.Ident)
		return ok && ident.Name == "nil"
	}
	return (isVar(bin.X) && isNil(bin.Y)) || (isNil(bin.X) && isVar(bin.Y))
}

// returnPathWalker walks a function body in source order, tracking whether a
// critical section has been successfully opened (sawAcquire) and whether a
// sanctioned release call has fired since then on the current path
// (releaseSeen). Any return statement reached with sawAcquire && !releaseSeen,
// and whose own result expressions don't themselves contain a release call,
// is flagged.
//
// Design choice (favors false positives over false negatives, appropriate
// for a lock-safety guard): nested blocks (if/for/switch/select bodies)
// inherit the incoming state but do not leak their own progress back out to
// the parent block or sibling branches. A shared-tail pattern needs its
// release call restated per branch or hoisted before the fork.
//
// Special case: `x, err := a.Acquire(); if err != nil { ... return ... }` is
// exempt from the release requirement on that branch — no critical section
// was ever opened when acquisition itself fails.
type returnPathWalker struct {
	cfg     Config
	flagged bool
}

func (w *returnPathWalker) walkBlock(stmts []ast.Stmt, sawAcquire, releaseSeen bool) {
	for i := 0; i < len(stmts); i++ {
		stmt := stmts[i]
		if errVar, ok := acquireCallErrVar(stmt, w.cfg); ok {
			sawAcquire = true
			// A fresh acquisition resets whether this path is "covered":
			// some functions open and fully close more than one critical
			// section in sequence, and a release call that closed an
			// earlier, already-finished section must not be mistaken for
			// covering a later one.
			releaseSeen = false
			if i+1 < len(stmts) {
				if ifStmt, ok2 := stmts[i+1].(*ast.IfStmt); ok2 && ifStmt.Init == nil && isErrNilCheck(ifStmt.Cond, errVar) {
					i++ // the paired acquire-failure check is exempt; skip it
					continue
				}
			}
			continue
		}
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			hasRelease := nodeContainsSyncCall(s, w.cfg.isRelease)
			if sawAcquire && !releaseSeen && !hasRelease {
				w.flagged = true
			}
			if nodeContainsCall(s, w.cfg.isAcquire) {
				sawAcquire = true
			}
			if hasRelease {
				releaseSeen = true
			}
		case *ast.IfStmt:
			// Init/Cond execute unconditionally on reaching this statement,
			// so they extend the running state for the branches and for
			// code after the if.
			if s.Init != nil {
				sawAcquire = sawAcquire || nodeContainsCall(s.Init, w.cfg.isAcquire)
				releaseSeen = releaseSeen || nodeContainsSyncCall(s.Init, w.cfg.isRelease)
			}
			sawAcquire = sawAcquire || nodeContainsCall(s.Cond, w.cfg.isAcquire)
			releaseSeen = releaseSeen || nodeContainsSyncCall(s.Cond, w.cfg.isRelease)
			if s.Body != nil {
				w.walkBlock(s.Body.List, sawAcquire, releaseSeen)
			}
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				w.walkBlock(e.List, sawAcquire, releaseSeen)
			case *ast.IfStmt:
				w.walkBlock([]ast.Stmt{e}, sawAcquire, releaseSeen)
			}
		case *ast.BlockStmt:
			w.walkBlock(s.List, sawAcquire, releaseSeen)
		case *ast.ForStmt:
			// Init/Cond/Post execute unconditionally on reaching this
			// statement (Init and Cond at least once; Post only after a
			// completed iteration, but a call there is still reachable
			// before any return inside Body), so — like IfStmt's Init/Cond —
			// they extend the running state for the loop body.
			if s.Init != nil {
				sawAcquire = sawAcquire || nodeContainsCall(s.Init, w.cfg.isAcquire)
				releaseSeen = releaseSeen || nodeContainsSyncCall(s.Init, w.cfg.isRelease)
			}
			sawAcquire = sawAcquire || nodeContainsCall(s.Cond, w.cfg.isAcquire)
			releaseSeen = releaseSeen || nodeContainsSyncCall(s.Cond, w.cfg.isRelease)
			if s.Post != nil {
				sawAcquire = sawAcquire || nodeContainsCall(s.Post, w.cfg.isAcquire)
				releaseSeen = releaseSeen || nodeContainsSyncCall(s.Post, w.cfg.isRelease)
			}
			if s.Body != nil {
				w.walkBlock(s.Body.List, sawAcquire, releaseSeen)
			}
		case *ast.RangeStmt:
			sawAcquire = sawAcquire || nodeContainsCall(s.X, w.cfg.isAcquire)
			releaseSeen = releaseSeen || nodeContainsSyncCall(s.X, w.cfg.isRelease)
			if s.Body != nil {
				w.walkBlock(s.Body.List, sawAcquire, releaseSeen)
			}
		case *ast.SwitchStmt:
			if s.Init != nil {
				sawAcquire = sawAcquire || nodeContainsCall(s.Init, w.cfg.isAcquire)
				releaseSeen = releaseSeen || nodeContainsSyncCall(s.Init, w.cfg.isRelease)
			}
			sawAcquire = sawAcquire || nodeContainsCall(s.Tag, w.cfg.isAcquire)
			releaseSeen = releaseSeen || nodeContainsSyncCall(s.Tag, w.cfg.isRelease)
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					w.walkBlock(cc.Body, sawAcquire, releaseSeen)
				}
			}
		case *ast.TypeSwitchStmt:
			if s.Init != nil {
				sawAcquire = sawAcquire || nodeContainsCall(s.Init, w.cfg.isAcquire)
				releaseSeen = releaseSeen || nodeContainsSyncCall(s.Init, w.cfg.isRelease)
			}
			sawAcquire = sawAcquire || nodeContainsCall(s.Assign, w.cfg.isAcquire)
			releaseSeen = releaseSeen || nodeContainsSyncCall(s.Assign, w.cfg.isRelease)
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					w.walkBlock(cc.Body, sawAcquire, releaseSeen)
				}
			}
		case *ast.SelectStmt:
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					w.walkBlock(cc.Body, sawAcquire, releaseSeen)
				}
			}
		default:
			if nodeContainsCall(stmt, w.cfg.isAcquire) {
				sawAcquire = true
			}
			if nodeContainsSyncCall(stmt, w.cfg.isRelease) {
				releaseSeen = true
			}
		}
	}
}

// acquireReturnPathUncovered reports whether fn has any return path reachable
// after a successful AcquireMethods call that lacks a sanctioned release
// call.
func acquireReturnPathUncovered(fn *ast.FuncDecl, cfg Config) bool {
	if fn.Body == nil || !containsAcquireCall(fn.Body, cfg) {
		return false
	}
	w := &returnPathWalker{cfg: cfg}
	w.walkBlock(fn.Body.List, false, false)
	return w.flagged
}
