package main

import (
	"bufio"
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
	for {
		gitignore := filepath.Join(dir, ".gitignore")
		if data, err := os.ReadFile(gitignore); err == nil {
			foundAny = true
			if gitignoreCovers(data, base, abs, dir) {
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
	if foundAny && !covered {
		fmt.Fprintf(w, "warning: %s is not covered by .gitignore — plan.json may contain semi-sensitive data; add %q to .gitignore before committing.\n", planPath, base)
	}
}

// gitignoreCovers performs a pragmatic match against a .gitignore content for
// patterns that would exclude planAbs (basename = base, found at gitignoreDir).
// This is intentionally a heuristic, not full gitignore semantics: it covers
// the common cases (literal basename, "*.ext", "**/<base>", and a path
// relative to the gitignore directory) and ignores negation rules.
func gitignoreCovers(data []byte, base, planAbs, gitignoreDir string) bool {
	ext := filepath.Ext(base)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			continue // negation rules — skip; conservative (warn even if a later rule re-includes)
		}
		// Strip a leading "/" — gitignore-relative anchor; we treat both
		// "/foo" and "foo" as candidates against the basename or relative path.
		anchored := strings.TrimPrefix(line, "/")

		if anchored == base {
			return true
		}
		if ext != "" && (anchored == "*"+ext || anchored == "**/*"+ext) {
			return true
		}
		// "**/<base>" matches at any depth.
		if anchored == "**/"+base {
			return true
		}
		// Relative path from .gitignore dir, e.g. "cmd/wfctl/plan.json".
		if rel, err := filepath.Rel(gitignoreDir, planAbs); err == nil {
			if anchored == rel || anchored == filepath.ToSlash(rel) {
				return true
			}
		}
	}
	return false
}
