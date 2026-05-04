package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// warnIfPlanNotGitignored writes a stderr warning to w when planPath is not
// covered by any .gitignore reachable from the directory containing planPath
// up to the filesystem root.
//
// Why: plan.json carries semi-sensitive content (env-var fingerprints,
// resolved configs, sometimes provider IDs); committing it to source control
// is almost always accidental. We don't promise full gitignore semantics —
// the heuristic catches the common cases (literal basename, simple
// extension/path globs) and stays silent when no .gitignore exists at all
// (likely not a tracked repo).
//
// No warning is emitted when:
//   - No .gitignore is found between planDir and the filesystem root.
//   - At least one reachable .gitignore contains a line matching the plan's
//     basename, the literal plan path, "*.json", "*<ext>", or a "**/" pattern
//     ending with the basename.
func warnIfPlanNotGitignored(w io.Writer, planPath string) {
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return
	}
	base := filepath.Base(abs)
	dir := filepath.Dir(abs)

	foundAny := false
	covered := false
	scanFailed := false
	for {
		gitignore := filepath.Join(dir, ".gitignore")
		if data, err := os.ReadFile(gitignore); err == nil {
			foundAny = true
			ok, scanErr := gitignoreCovers(data, base, abs, dir)
			if scanErr != nil {
				// Surface parse failure to the operator (line over
				// bufio.MaxScanTokenSize, etc.) rather than silently
				// pretending the file is/isn't covered.
				fmt.Fprintf(w, "warning: could not scan %s for plan.json coverage: %v\n", gitignore, scanErr)
				scanFailed = true
			}
			if ok {
				covered = true
				break
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	if foundAny && !covered && !scanFailed {
		fmt.Fprintf(w, "warning: %s is not covered by .gitignore — plan.json may contain semi-sensitive data; add %q to .gitignore before committing.\n", planPath, base)
	}
}

// gitignoreCovers performs a pragmatic match against a .gitignore content for
// patterns that would exclude planAbs (basename = base, found at gitignoreDir).
// This is intentionally a heuristic, not full gitignore semantics: it covers
// the common cases (literal basename, "*.ext", "**/<base>", and a path
// relative to the gitignore directory) and ignores negation rules.
//
// Returns (covered, scanErr). scanErr is non-nil only when the underlying
// bufio.Scanner failed (e.g. a line over bufio.MaxScanTokenSize); the caller
// surfaces that to the operator via stderr instead of silently treating
// scan-failure as either covered or not-covered.
func gitignoreCovers(data []byte, base, planAbs, gitignoreDir string) (bool, error) {
	ext := filepath.Ext(base)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			// Negation rules are skipped entirely. Combined with the early-return
			// on first positive match below, this means a pattern like "*.json"
			// followed by "!plan.json" (re-include) is incorrectly treated as
			// covered — producing a false-NEGATIVE warning (operator sees no
			// warning when one was warranted). Acceptable for a heuristic whose
			// purpose is to nudge, not enforce; full last-match-wins gitignore
			// semantics are out of scope. If false negatives become a problem,
			// either implement last-matching-rule-wins for the supported
			// pattern set, or shell out to `git check-ignore`.
			continue
		}
		// Strip a leading "/" — gitignore-relative anchor; we treat both
		// "/foo" and "foo" as candidates against the basename or relative path.
		anchored := strings.TrimPrefix(line, "/")

		if anchored == base {
			return true, nil
		}
		if ext != "" && (anchored == "*"+ext || anchored == "**/*"+ext) {
			return true, nil
		}
		// "**/<base>" matches at any depth.
		if anchored == "**/"+base {
			return true, nil
		}
		// Relative path from .gitignore dir, e.g. "cmd/wfctl/plan.json".
		if rel, err := filepath.Rel(gitignoreDir, planAbs); err == nil {
			if anchored == rel || anchored == filepath.ToSlash(rel) {
				return true, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}
