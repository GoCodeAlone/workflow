package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/capability/assembler"
	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
	"gopkg.in/yaml.v3"
)

// runCapabilityAssemble implements `wfctl capability assemble --set <f|@f|-> --out <dir> [--force]`.
// It parses a capability set, validates canonical taxonomy IDs (D3), resolves a
// path-safe output dir (D14), runs inventory.CollectEcosystem + assembler.Assemble
// (V3 fail-closed), emits workflow.yaml + app.yaml + NEXT_STEPS.md, and appends an
// audit record (V9). Unmatched capabilities are surfaced as a warning, not an error.
func runCapabilityAssemble(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("capability assemble", flag.ContinueOnError)
	fs.SetOutput(out)
	var setSpec, outDir string
	var registryDir, repoRoot, taxonomyPath string
	var force bool
	fs.StringVar(&setSpec, "set", "", "capability set JSON: file path, @file, - (stdin), or inline JSON")
	fs.StringVar(&outDir, "out", "", "output directory (must be within cwd unless --force)")
	fs.StringVar(&registryDir, "registry", defaultCapabilityRegistryPath(), "registry directory")
	fs.StringVar(&repoRoot, "repo-root", "..", "workspace root containing workflow-plugin-* repos")
	fs.StringVar(&taxonomyPath, "taxonomy", defaultCapabilityTaxonomyPath(), "capability taxonomy YAML")
	fs.BoolVar(&force, "force", false, "overwrite existing --out + allow absolute/out-of-cwd paths")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if setSpec == "" || outDir == "" {
		return errors.New("capability assemble: --set and --out are required")
	}

	in, err := parseAssemblySet(setSpec, fs) // file / @file / - / inline
	if err != nil {
		return err
	}

	tax, err := inventory.LoadTaxonomy(taxonomyPath)
	if err != nil {
		return fmt.Errorf("load taxonomy: %w", err)
	}
	if err := validateCanonicalIDs(in.Capabilities, tax); err != nil {
		return err // includes did-you-mean (D3)
	}

	// --out path-safety (D14): resolve + reject out-of-cwd/system unless --force
	resolved, err := resolveOutDir(outDir, force)
	if err != nil {
		return err
	}

	inv, err := inventory.CollectEcosystem(inventory.EcosystemOptions{
		RegistryDir:     registryDir,
		RepoRoot:        repoRoot,
		TaxonomyPath:    taxonomyPath,
		GeneratedAt:     nowUTC(),
		WorkflowVersion: version,
	})
	if err != nil {
		return err
	}

	app, err := assembler.Assemble(inv, in, schema.NewModuleSchemaRegistry())
	if err != nil {
		return err // V3 fail-closed already fired inside Assemble
	}
	if !force {
		if _, err := os.Stat(resolved); err == nil {
			return fmt.Errorf("capability assemble: %s exists (use --force to overwrite)", resolved)
		}
	}
	if err := emit(resolved, app, in); err != nil {
		return err
	}
	if err := writeAssembleAudit(app, in); err != nil {
		return err // V9
	}
	if len(app.Unmatched) > 0 {
		fmt.Fprintf(out, "WARN unmatched capabilities (see NEXT_STEPS.md): %s\n", strings.Join(app.Unmatched, ", "))
	}
	fmt.Fprintf(out, "assembled %d module(s) into %s\n", len(app.Modules), resolved)
	return nil
}

// parseAssemblySet resolves the --set argument to an AssemblyInput. Supported
// shapes: file path, "@file", "-" (stdin), or an inline JSON literal beginning
// with '{'. Rejects an empty capabilities∪modules set (D7).
func parseAssemblySet(spec string, fs *flag.FlagSet) (assembler.AssemblyInput, error) {
	var data []byte
	var err error
	switch {
	case spec == "-":
		data, err = io.ReadAll(os.Stdin)
	case strings.HasPrefix(spec, "@"):
		data, err = os.ReadFile(strings.TrimPrefix(spec, "@"))
	case strings.HasPrefix(spec, "{"):
		data = []byte(spec) // inline JSON literal
	default:
		data, err = os.ReadFile(spec) // file path
	}
	if err != nil {
		return assembler.AssemblyInput{}, fmt.Errorf("read --set: %w", err)
	}
	var in assembler.AssemblyInput
	if err := json.Unmarshal(data, &in); err != nil {
		return assembler.AssemblyInput{}, fmt.Errorf("parse --set JSON: %w", err)
	}
	if len(in.Capabilities) == 0 && len(in.Modules) == 0 {
		return assembler.AssemblyInput{}, errors.New("--set: at least one capability or module is required (D7)")
	}
	return in, nil
}

// validateCanonicalIDs requires every requested ID to be a canonical taxonomy id
// (taxonomy.ByID, D3). On the first unknown id it returns an error with up to 3
// nearest canonical candidates (did-you-mean). Bare aliases / keywords are rejected
// so the assembler never silently no-ops on a mis-spelled capability.
func validateCanonicalIDs(ids []string, tax *inventory.Taxonomy) error {
	var known []string
	for _, id := range ids {
		if _, ok := tax.ByID(id); ok {
			continue
		}
		if known == nil {
			for i := range tax.Capabilities { // index → avoid 488B per-iter copy (gocritic)
				known = append(known, tax.Capabilities[i].ID)
			}
			sort.Strings(known)
		}
		return fmt.Errorf("capability %q is not a canonical taxonomy id; nearest: %s", id, strings.Join(nearest(id, known, 3), ", "))
	}
	return nil
}

// resolveOutDir hardens --out against symlink/path traversal (D14). It resolves
// to an absolute path, evaluates symlinks (falling back to abs when the target
// does not yet exist), and rejects paths outside cwd unless --force is set.
func resolveOutDir(outDir string, force bool) (string, error) {
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if resolved == "" {
		resolved = abs // target doesn't exist yet
	}
	cwd, _ := os.Getwd()
	// Containment uses cwd + separator so "/tmp/app2" is NOT accepted as inside
	// cwd "/tmp/app" (Copilot: bare HasPrefix(resolved, cwd) was bypassable).
	if resolved != cwd && !strings.HasPrefix(resolved, cwd+string(filepath.Separator)) && !force {
		return "", fmt.Errorf("--out %q resolves outside cwd (%s); use --force to allow", resolved, cwd)
	}
	return resolved, nil
}

// emit writes workflow.yaml + app.yaml + NEXT_STEPS.md into outDir. workflow.yaml
// is rendered via assembler.MarshalConfig (P4: shared pure fn — also used by the
// MC2-bis boot test, avoiding a cmd/wfctl <-> capability/assembler import cycle).
// Permissions: dir 0750, files 0600 (repo convention; stricter than the plan's
// 0640, which gosec G306 rejects — scaffold files may contain ${SECRET} env refs).
func emit(outDir string, app *assembler.AssembledApp, in assembler.AssemblyInput) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	wfYAML, err := assembler.MarshalConfig(app)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "workflow.yaml"), wfYAML, 0o600); err != nil {
		return err
	}
	appYAML, err := yaml.Marshal(map[string]any{
		"application": map[string]any{"name": "assembled-app", "version": "0.1.0"},
		"requires":    app.Requires,
	})
	if err != nil {
		return fmt.Errorf("marshal app.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "app.yaml"), appYAML, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "NEXT_STEPS.md"), []byte(renderNextSteps(app, in)), 0o600)
}

// renderNextSteps emits the markdown manual-wiring guide (V8): external-plugin
// install cmds, ${SECRET} env exports, manual-wiring findings, and a DB note
// when a database module is present. Pure; deterministic.
func renderNextSteps(app *assembler.AssembledApp, in assembler.AssemblyInput) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# NEXT STEPS")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Assembled scaffold generated by `wfctl capability assemble`. Complete the wiring before boot:")
	fmt.Fprintln(&b)

	if len(app.Requires.Plugins) > 0 {
		fmt.Fprintln(&b, "## Install external plugins")
		for _, p := range app.Requires.Plugins {
			fmt.Fprintf(&b, "- `wfctl plugin install %s", p.Name)
			if p.Version != "" {
				fmt.Fprintf(&b, "@%s", p.Version)
			}
			fmt.Fprintln(&b, "`")
		}
		fmt.Fprintln(&b)
	}

	if refs := secretRefs(app); len(refs) > 0 {
		fmt.Fprintln(&b, "## Provide secrets")
		for _, name := range refs {
			fmt.Fprintf(&b, "- `export %s=...`\n", name)
		}
		fmt.Fprintln(&b)
	}

	var manualWiring []assembler.Finding
	for _, f := range app.Findings {
		if f.Code == "no-schema" {
			manualWiring = append(manualWiring, f)
		}
	}
	if len(manualWiring) > 0 {
		fmt.Fprintln(&b, "## Manual wiring required")
		for _, f := range manualWiring {
			fmt.Fprintf(&b, "- [%s] %s\n", f.Level, f.Message)
		}
		fmt.Fprintln(&b)
	}

	for _, m := range app.Modules {
		if m.Type == "database.workflow" || strings.HasPrefix(m.Type, "database.") {
			fmt.Fprintln(&b, "## Database setup")
			fmt.Fprintln(&b, "- The scaffold includes a database module. Create the DB file / DSN before boot.")
			fmt.Fprintln(&b, "  For local sqlite: `touch ./app.db` and set the `dsn` field accordingly.")
			fmt.Fprintln(&b)
			break
		}
	}

	if len(app.Unmatched) > 0 {
		fmt.Fprintln(&b, "## Unmatched requested capabilities")
		for _, id := range app.Unmatched {
			fmt.Fprintf(&b, "- %s (no module candidate in inventory; add a provider plugin or an explicit `modules` entry in --set)\n", id)
		}
		fmt.Fprintln(&b)
	}

	if in.Goal != "" { // print only when a goal was provided (Copilot: hashGoal never returns bare "sha256:")
		fmt.Fprintf(&b, "_goal hash: %s_\n", hashGoal(in.Goal))
	}
	return b.String()
}

// writeAssembleAudit appends one JSONL record to the assemble audit log (V9).
// Path + perms mirror secrets_audit.go (D4). The record stores the goal HASH
// (⊥ raw goal, D11) — never the raw goal text, which may be sensitive.
func writeAssembleAudit(app *assembler.AssembledApp, in assembler.AssemblyInput) error {
	path, err := assembleAuditPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("audit: create dir: %w", err)
	}
	moduleTypes := make([]string, 0, len(app.Modules))
	for _, m := range app.Modules {
		moduleTypes = append(moduleTypes, m.Type)
	}
	sort.Strings(moduleTypes)
	rec := map[string]any{
		"ts":           nowUTC().Format(time.RFC3339),
		"capabilities": in.Capabilities,
		"moduleTypes":  moduleTypes,
		"goalHash":     hashGoal(in.Goal),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	if cerr := f.Close(); cerr != nil && err == nil { // check Close (code-quality: was unhandled defer)
		err = cerr
	}
	return err
}

// assembleAuditPath returns the audit JSONL path, honouring $XDG_STATE_HOME with
// a $HOME/.local/state fallback (mirrors secrets_audit.go).
func assembleAuditPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		u, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(u.HomeDir, ".local", "state")
	}
	return filepath.Join(base, "wfctl", "plugins", "wfctl", "assemble-audit.jsonl"), nil
}

// nearest returns up to n candidates closest to s by Levenshtein distance
// (did-you-mean, D3). Ties broken by candidate name for determinism.
func nearest(s string, cands []string, n int) []string {
	if len(cands) == 0 {
		return nil
	}
	type sc struct {
		c string
		d int
	}
	ss := make([]sc, 0, len(cands))
	for _, c := range cands {
		ss = append(ss, sc{c, levenshtein(strings.ToLower(s), strings.ToLower(c))})
	}
	sort.Slice(ss, func(i, j int) bool {
		if ss[i].d != ss[j].d {
			return ss[i].d < ss[j].d
		}
		return ss[i].c < ss[j].c
	})
	out := make([]string, 0, n)
	for i := 0; i < n && i < len(ss); i++ {
		out = append(out, ss[i].c)
	}
	return out
}

// levenshtein is the standard O(m*n) DP edit distance (stdlib-only).
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// minInt returns the smallest of its args. Caller MUST pass ≥1 arg.
func minInt(a ...int) int {
	m := a[0]
	for _, x := range a[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// secretRefs lists "${NAME}" env-var refs across module configs (V4: NEXT_STEPS
// export list). Top-level-only scan (P15: complete for flat genConfig output;
// agent-injected nested configs are out of scope).
var secretRefRe = regexp.MustCompile(`^\$\{([A-Z0-9_]+)\}$`)

func secretRefs(app *assembler.AssembledApp) []string {
	seen := map[string]bool{}
	for i := range app.Modules {
		for _, v := range app.Modules[i].Config {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if m := secretRefRe.FindStringSubmatch(s); m != nil {
				seen[m[1]] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hashGoal returns "sha256:<hex>" of the goal (⊥ raw goal in audit, D11).
// Returns "sha256:" for an empty goal so callers can elide the line.
func hashGoal(goal string) string {
	sum := sha256.Sum256([]byte(goal))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// nowUTC returns the current UTC time. Wrapped for testability + single seam.
func nowUTC() time.Time {
	return time.Now().UTC()
}
