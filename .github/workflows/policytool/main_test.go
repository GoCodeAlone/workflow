package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

func groupedClonePolicy(t *testing.T, policy trustPolicy) trustPolicy {
	t.Helper()
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var result trustPolicy
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func groupedPolicyFixtures() (trustPolicy, trustPolicy, trustPolicy, map[string]string, map[string]string, map[string]bool) {
	base := trustPolicy{}
	oldContexts, newContexts, present := map[string]string{}, map[string]string{}, map[string]bool{}
	paths := []string{".github/workflows/ci.yml", ".github/workflows/benchmark.yml"}
	for index, workflow := range paths {
		oldContext := strings.Repeat(strconv.Itoa(index*2+1), 64)
		oldContexts[workflow], present[workflow] = oldContext, true
		newContexts[workflow] = strings.Repeat(strconv.Itoa(index*2+2), 64)
		base.Presence = append(base.Presence, trustGroup{workflow, oldContext, "active", "present"})
		base.Executables = append(base.Executables, executableEntry{"scripts/check-" + strconv.Itoa(index) + ".sh", workflow, oldContext, strings.Repeat("a", 64), "active", "Reviewed executable."})
		base.Commands = append(base.Commands, commandEntry{workflow, "go", strings.Repeat("b", 64), oldContext, "active", "Reviewed command."})
		base.Actions = append(base.Actions, actionEntry{workflow, "actions/checkout@" + strings.Repeat("c", 40), strings.Repeat("d", 64), oldContext, "active", "Reviewed action."})
	}
	staged := trustPolicy{Presence: append([]trustGroup(nil), base.Presence...), Executables: append([]executableEntry(nil), base.Executables...), Commands: append([]commandEntry(nil), base.Commands...), Actions: append([]actionEntry(nil), base.Actions...)}
	for index, workflow := range paths {
		context := newContexts[workflow]
		staged.Presence = append(staged.Presence, trustGroup{workflow, context, "staged", "present"})
		staged.Executables = append(staged.Executables, executableEntry{"scripts/check-" + strconv.Itoa(index) + ".sh", workflow, context, strings.Repeat("e", 64), "staged", "Replacement executable."})
		staged.Commands = append(staged.Commands, commandEntry{workflow, "go", strings.Repeat("f", 64), context, "staged", "Replacement command."})
		staged.Actions = append(staged.Actions, actionEntry{workflow, "actions/checkout@" + strings.Repeat("c", 40), strings.Repeat("9", 64), context, "staged", "Replacement action."})
	}
	promoted := staged
	for _, group := range staged.Presence {
		if group.State == "staged" {
			promoted = promotedTrustPolicy(promoted, group)
		}
	}
	return base, staged, promoted, oldContexts, newContexts, present
}

func TestGroupedTrustLifecycle(t *testing.T) {
	base, staged, promoted, oldContexts, newContexts, present := groupedPolicyFixtures()
	for _, tc := range []struct {
		name            string
		base, candidate trustPolicy
		before, after   map[string]string
		reject          bool
	}{
		{"complete distinct-path stage", base, staged, oldContexts, oldContexts, false},
		{"complete adoption", staged, staged, oldContexts, newContexts, false},
		{"complete realized promotion", staged, promoted, newContexts, newContexts, false},
		{"stage and adopt", base, staged, oldContexts, newContexts, true},
		{"premature promotion", staged, promoted, oldContexts, oldContexts, true},
		{"drop unused stage", staged, base, oldContexts, oldContexts, true},
		{"revert adopted bytes before promotion", staged, staged, newContexts, oldContexts, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := validateTrustTransition(tc.base, tc.candidate, tc.before, tc.after, present, present)
			if (len(findings) > 0) != tc.reject {
				t.Fatalf("findings=%v reject=%v", findings, tc.reject)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*trustPolicy)
	}{
		{"duplicate staged path", func(p *trustPolicy) { p.Presence = append(p.Presence, p.Presence[2]) }},
		{"existing unused stage", func(p *trustPolicy) {
			p.Presence = append(p.Presence, trustGroup{p.Presence[2].Path, strings.Repeat("a", 64), "staged", "present"})
		}},
		{"drops active row", func(p *trustPolicy) { p.Executables = p.Executables[1:] }},
		{"changes retained rationale", func(p *trustPolicy) { p.Actions[0].Rationale = "Different metadata." }},
		{"orphan executable", func(p *trustPolicy) { p.Executables[2].WorkflowPath = ".github/workflows/other.yml" }},
		{"mixed context", func(p *trustPolicy) { p.Commands[2].ContextSHA256 = strings.Repeat("a", 64) }},
		{"invalid executable digest", func(p *trustPolicy) { p.Executables[2].SHA256 = "bad" }},
		{"duplicate executable", func(p *trustPolicy) { p.Executables = append(p.Executables, p.Executables[2]) }},
		{"mutable action", func(p *trustPolicy) { p.Actions[2].Uses = "actions/checkout@main" }},
		{"categorical command", func(p *trustPolicy) { p.Commands[2].Command = "eval" }},
		{"nonempty secrets", func(p *trustPolicy) {
			p.Secrets = []allowEntry{{Path: p.Presence[2].Path, Secret: "TOKEN", State: "staged"}}
		}},
		{"missing group with retained rows", func(p *trustPolicy) { p.Presence = p.Presence[:3] }},
		{"already active context", func(p *trustPolicy) { p.Presence[2].ContextSHA256 = p.Presence[0].ContextSHA256 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := groupedClonePolicy(t, staged)
			tc.mutate(&candidate)
			if findings := validateTrustTransition(base, candidate, oldContexts, oldContexts, present, present); len(findings) == 0 {
				t.Fatal("malformed batch accepted")
			}
		})
	}
	t.Run("partial promotion rows", func(t *testing.T) {
		candidate := groupedClonePolicy(t, promoted)
		candidate.Executables[0].State = "staged"
		if findings := validateTrustTransition(staged, candidate, newContexts, newContexts, present, present); len(findings) == 0 {
			t.Fatal("partial row promotion accepted")
		}
	})
}

func TestUnusedStagedExecutableCorrection(t *testing.T) {
	_, staged, _, oldContexts, newContexts, present := groupedPolicyFixtures()
	corrected := groupedClonePolicy(t, staged)
	corrected.Executables[2].SHA256 = strings.Repeat("7", 64)
	t.Run("one unused context SHA only", func(t *testing.T) {
		if findings := validateTrustTransition(staged, corrected, oldContexts, oldContexts, present, present); len(findings) != 0 {
			t.Fatalf("correction rejected: %v", findings)
		}
	})
	for _, tc := range []struct {
		name   string
		mutate func(*trustPolicy)
	}{
		{"active row", func(p *trustPolicy) { p.Executables[0].SHA256 = strings.Repeat("7", 64) }},
		{"two contexts", func(p *trustPolicy) { p.Executables[3].SHA256 = strings.Repeat("7", 64) }},
		{"rationale drift", func(p *trustPolicy) { p.Executables[2].Rationale = "Drift." }},
		{"row order", func(p *trustPolicy) { p.Executables[2], p.Executables[3] = p.Executables[3], p.Executables[2] }},
		{"membership deletion", func(p *trustPolicy) { p.Executables = p.Executables[:3] }},
		{"membership addition", func(p *trustPolicy) { p.Executables = append(p.Executables, p.Executables[2]) }},
		{"path drift", func(p *trustPolicy) { p.Executables[2].Path = "scripts/new.sh" }},
		{"state drift", func(p *trustPolicy) { p.Executables[2].State = "active" }},
		{"context drift", func(p *trustPolicy) { p.Executables[2].ContextSHA256 = strings.Repeat("8", 64) }},
		{"invalid digest", func(p *trustPolicy) { p.Executables[2].SHA256 = "bad" }},
		{"action drift", func(p *trustPolicy) { p.Actions[2].NodeSHA256 = strings.Repeat("7", 64) }},
		{"command drift", func(p *trustPolicy) { p.Commands[2].StatementSHA256 = strings.Repeat("7", 64) }},
		{"presence drift", func(p *trustPolicy) { p.Presence[2].Presence = "absent"; p.Presence[2].ContextSHA256 = "" }},
		{"secret drift", func(p *trustPolicy) { p.Secrets = []allowEntry{{Secret: "TOKEN"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := groupedClonePolicy(t, corrected)
			tc.mutate(&candidate)
			if findings := validateTrustTransition(staged, candidate, oldContexts, oldContexts, present, present); len(findings) == 0 {
				t.Fatal("non-SHA-only correction accepted")
			}
		})
	}
	t.Run("selected staged context", func(t *testing.T) {
		if findings := validateTrustTransition(staged, corrected, newContexts, newContexts, present, present); len(findings) == 0 {
			t.Fatal("realized staged executable correction accepted")
		}
	})
	t.Run("simultaneous adoption", func(t *testing.T) {
		if findings := validateTrustTransition(staged, corrected, oldContexts, newContexts, present, present); len(findings) == 0 {
			t.Fatal("correction and adoption accepted")
		}
	})
}

func groupedRepositoryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	inputs := filepath.Join("..", "..", "..")
	var manifest authorityManifest
	data, err := os.ReadFile(filepath.Join(inputs, ".github/public-workflow-authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, bundle := range manifest.Bundles {
		for _, file := range bundle.Files {
			paths[file.Path] = true
		}
	}
	for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml", ".github/public-workflow-*.json"} {
		matches, err := filepath.Glob(filepath.Join(inputs, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range matches {
			relative, err := filepath.Rel(inputs, file)
			if err != nil {
				t.Fatal(err)
			}
			paths[relative] = true
		}
	}
	for name := range paths {
		data, err := os.ReadFile(filepath.Join(inputs, name))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(inputs, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := authorityInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(authorityManifest{Version: 1, Bundles: []authorityBundle{{State: "active", Files: inventory}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github/public-workflow-authority.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestTrustOnlyTransitionPreservesRawWorkflowBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(string) error
		reject bool
	}{
		{"unchanged bytes", func(string) error { return nil }, false},
		{"comment drift", func(root string) error {
			path := filepath.Join(root, ".github/workflows/ci.yml")
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(path, append(data, []byte("\n# unrelated drift\n")...), 0644)
		}, true},
		{"mode drift", func(root string) error { return os.Chmod(filepath.Join(root, ".github/workflows/ci.yml"), 0755) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, candidate := groupedRepositoryFixture(t), groupedRepositoryFixture(t)
			policy, err := loadTrustPolicy(candidate)
			if err != nil {
				t.Fatal(err)
			}
			policy.Presence = append(policy.Presence, trustGroup{Path: ".github/workflows/public-workflow-policy.yml", ContextSHA256: strings.Repeat("7", 64), State: "staged", Presence: "present"})
			data, err := json.Marshal(policy.Presence)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(candidate, ".github/public-workflow-presence-allowlist.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(candidate); err != nil {
				t.Fatal(err)
			}
			findings, err := validateRepositoryAuthority(base, candidate, false)
			if err != nil {
				t.Fatal(err)
			}
			if (len(findings) > 0) != tc.reject {
				t.Fatalf("findings=%v reject=%v", findings, tc.reject)
			}
		})
	}
}

func TestSHA256HexPatternIsCompiledOnce(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	const pattern = `^[a-f0-9]{64}$`
	compileCount := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "MustCompile" {
			return true
		}
		packageName, ok := selector.X.(*ast.Ident)
		if !ok || packageName.Name != "regexp" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("unquote regexp pattern: %v", err)
		}
		if value == pattern {
			compileCount++
		}
		return true
	})
	if compileCount != 1 {
		t.Fatalf("SHA-256 validation pattern compiled %d times, want exactly once", compileCount)
	}
}

func TestValidSHA256Hex(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{"lowercase hex", strings.Repeat("0123456789abcdef", 4), true},
		{"uppercase", strings.Repeat("A", 64), false},
		{"short", strings.Repeat("a", 63), false},
		{"nonhex", strings.Repeat("a", 63) + "g", false},
		{"long", strings.Repeat("a", 65), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validSHA256Hex(test.value); got != test.want {
				t.Fatalf("validSHA256Hex(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func parseShell(t *testing.T, source string) *syntax.File {
	t.Helper()
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "test")
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	return file
}

func firstCall(t *testing.T, file *syntax.File) *syntax.CallExpr {
	t.Helper()
	var call *syntax.CallExpr
	syntax.Walk(file, func(node syntax.Node) bool {
		if call != nil {
			return false
		}
		if candidate, ok := node.(*syntax.CallExpr); ok {
			call = candidate
			return false
		}
		return true
	})
	if call == nil {
		t.Fatal("shell source did not contain a call")
	}
	return call
}

func TestResolvedProgramUnwrapsReviewedWrappers(t *testing.T) {
	file := parseShell(t, `MODE=ci command exec env AUTH=x ./scripts/live.sh`)
	program, _, resolved := resolvedProgram(firstCall(t, file))
	if !resolved {
		t.Fatal("expected wrapped program to resolve")
	}
	value, literal := literalWord(program)
	if !literal || value != "./scripts/live.sh" {
		t.Fatalf("resolved program = %q, literal=%v", value, literal)
	}
}

func TestResolvedProgramRejectsDynamicCommand(t *testing.T) {
	file := parseShell(t, `sudo "$TOOL"`)
	_, _, resolved := resolvedProgram(firstCall(t, file))
	if resolved {
		t.Fatal("dynamic wrapped command resolved as a literal program")
	}
}

func TestPureRejectionGuardConfinesDenyPattern(t *testing.T) {
	patterns := map[string]string{"PROVIDER_DENY_PATTERN": "doctl|api.digitalocean.com"}
	safe := parseShell(t, `if rg "$PROVIDER_DENY_PATTERN" .github/workflows/; then echo blocked; exit 1; fi`)
	if !pureRejectionGuard(safe, patterns) {
		t.Fatal("expected parser-proven rejection guard")
	}
	unsafe := parseShell(t, `if rg "$PROVIDER_DENY_PATTERN" .github/workflows/; then exit 1; fi; "$PROVIDER_DENY_PATTERN"`)
	if pureRejectionGuard(unsafe, patterns) {
		t.Fatal("deny pattern escaped rejection guard")
	}
}

func TestDefaultsRunShell(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("defaults:\n  run:\n    shell: bash\n"), &doc); err != nil {
		t.Fatalf("parse workflow defaults: %v", err)
	}
	shell := defaultsRunShell(doc.Content[0])
	if shell == nil || shell.Value != "bash" {
		t.Fatalf("defaults shell = %#v", shell)
	}
}

func TestWorkflowPermissionsRejectWriteAuthority(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("permissions:\n  contents: read\n  packages: write\n"), &doc); err != nil {
		t.Fatalf("parse workflow permissions: %v", err)
	}
	findings := &findingSet{}
	checkPermissions("workflow test.yml", mappingValue(doc.Content[0], "permissions"), true, findings)
	if !strings.Contains(strings.Join(findings.items, "\n"), "workflow test.yml grants write permission packages outside a job") {
		t.Fatalf("workflow-level write permission was accepted: %v", findings.items)
	}
}

func TestMissingEffectivePermissionsAreRejected(t *testing.T) {
	findings := &findingSet{}
	checkPermissions("fixture job implicit", nil, false, findings)
	if !strings.Contains(strings.Join(findings.items, "\n"), "does not declare permissions and workflow has no explicit permissions to inherit") {
		t.Fatalf("implicit repository-default permissions were accepted: %v", findings.items)
	}
}

func TestExplicitJobPermissionsWithoutWorkflowPermissionsPass(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("permissions:\n  contents: read\n  pull-requests: write\n"), &doc); err != nil {
		t.Fatalf("parse job permissions: %v", err)
	}
	findings := &findingSet{}
	checkPermissions("fixture job explicit", mappingValue(doc.Content[0], "permissions"), false, findings)
	if len(findings.items) != 0 {
		t.Fatalf("explicit job permissions without workflow permissions were rejected: %v", findings.items)
	}
}

func TestExpressionIdentifiersIgnoresStringData(t *testing.T) {
	masked := expressionIdentifiers(`format('No secrets are used', secrets.RELEASES_TOKEN)`)
	if strings.Contains(masked, "No secrets are used") {
		t.Fatalf("expression string data was not masked: %q", masked)
	}
	if !strings.Contains(masked, "secrets.RELEASES_TOKEN") {
		t.Fatalf("secret identifier was masked: %q", masked)
	}
}

func TestStatementDigestCoversCompleteCall(t *testing.T) {
	original := parseShell(t, `GOWORK=off env MODE=ci go test -race ./...`)
	originalDigest, err := statementDigest(original.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		`GOWORK=off env MODE=ci go test -race ./... ./extra/...`,
		`GOWORK=off env MODE=ci go test ./...`,
		`GOWORK=off env MODE=prod go test -race ./...`,
		`env MODE=ci go test -race ./...`,
	} {
		file := parseShell(t, mutation)
		digest, err := statementDigest(file.Stmts[0])
		if err != nil {
			t.Fatal(err)
		}
		if digest == originalDigest {
			t.Errorf("mutation retained invocation digest: %s", mutation)
		}
	}
}

func TestGithubExpressionSourceChangesInvocationDigest(t *testing.T) {
	left, err := normalizeGithubExpressions(`gh release edit ${{ github.ref_name }} --draft=false`)
	if err != nil {
		t.Fatal(err)
	}
	right, err := normalizeGithubExpressions(`gh release edit ${{ vars.RELEASE_TAG }} --draft=false`)
	if err != nil {
		t.Fatal(err)
	}
	leftFile := parseShell(t, left)
	leftDigest, err := statementDigest(leftFile.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	rightFile := parseShell(t, right)
	rightDigest, err := statementDigest(rightFile.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest == rightDigest {
		t.Fatal("different GitHub expressions produced the same invocation digest")
	}
}

func TestActionAllowlistIsExactByWorkflowAndReference(t *testing.T) {
	entry := actionEntry{
		Path:          ".github/workflows/ci.yml",
		Uses:          "actions/checkout@ffffffffffffffffffffffffffffffffffffffff",
		NodeSHA256:    strings.Repeat("a", 64),
		ContextSHA256: strings.Repeat("b", 64),
		Rationale:     "Checkout this repository at the reviewed action commit.",
	}
	allowed := map[string]actionEntry{actionKey(entry.Path, entry.Uses, entry.NodeSHA256, entry.ContextSHA256): entry}
	if _, ok := matchAction(entry.Path, entry.Uses, entry.NodeSHA256, entry.ContextSHA256, allowed); !ok {
		t.Fatal("exact reviewed action did not match")
	}
	for _, changed := range []string{
		"actions/checkout@main",
		"actions/checkout@v5",
		"actions/checkout@0123456789012345678901234567890123456789",
		"${{ vars.ACTION_REF }}",
	} {
		if _, ok := matchAction(entry.Path, changed, entry.NodeSHA256, entry.ContextSHA256, allowed); ok {
			t.Errorf("changed action %q matched", changed)
		}
	}
	if _, ok := matchAction(".github/workflows/release.yml", entry.Uses, entry.NodeSHA256, entry.ContextSHA256, allowed); ok {
		t.Fatal("action allowlist leaked across workflow paths")
	}
}

func TestImmutableActionReferenceAcceptsOnlyCommitOrImageDigest(t *testing.T) {
	for _, reference := range []string{
		"actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10",
		"docker://ghcr.io/google/osv-scanner-action@sha256:48406c58197201fe55e56615ad9d414f85063da320e204d0b0ed460fb3908dba",
	} {
		if !immutableActionReference(reference) {
			t.Errorf("immutable action reference rejected: %s", reference)
		}
	}
	for _, reference := range []string{
		"actions/checkout@v6",
		"docker://ghcr.io/google/osv-scanner-action:v2.3.8",
		"docker://ghcr.io/google/osv-scanner-action@sha256:short",
	} {
		if immutableActionReference(reference) {
			t.Errorf("mutable action reference accepted: %s", reference)
		}
	}
}

func TestResolvedProgramUnwrapsAllowedWrappersButRejectsSudo(t *testing.T) {
	for _, source := range []string{
		`command go test ./...`,
		`exec go test ./...`,
		`env GOWORK=off go test ./...`,
	} {
		file := parseShell(t, source)
		program, args, resolved := resolvedProgram(firstCall(t, file))
		value, literal := literalWord(program)
		if !resolved || !literal || value != "go" || len(args) < 1 {
			t.Fatalf("%q resolved to %q, args=%d, resolved=%v", source, value, len(args), resolved)
		}
	}
	file := parseShell(t, `sudo go test ./...`)
	if _, _, resolved := resolvedProgram(firstCall(t, file)); resolved {
		t.Fatal("sudo unexpectedly resolved as a reviewed wrapper")
	}
}

func TestKnownCloudSecretRejectsSpacesCredentials(t *testing.T) {
	for _, name := range []string{
		"SPACES_ACCESS_KEY_ID",
		"SPACES_SECRET_ACCESS_KEY",
		"DIGITALOCEAN_SPACES_ACCESS_KEY_ID",
		"DIGITALOCEAN_SPACES_SECRET_ACCESS_KEY",
		"DO_SPACES_ACCESS_KEY_ID",
		"DO_SPACES_SECRET_ACCESS_KEY",
	} {
		if !knownCloudSecret(name) {
			t.Errorf("%s was not categorized as a cloud secret", name)
		}
	}
}

func TestDecodeJSONRequiresExactlyOneValue(t *testing.T) {
	tmp := t.TempDir()
	valid := filepath.Join(tmp, "valid.json")
	if err := os.WriteFile(valid, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	var target []allowEntry
	if err := decodeJSONFile(valid, &target); err != nil {
		t.Fatalf("valid JSON failed: %v", err)
	}
	for name, content := range map[string]string{
		"trailing-object":  `[] {}`,
		"trailing-garbage": `[] garbage`,
		"null-array":       `null`,
	} {
		path := filepath.Join(tmp, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := decodeJSONFile(path, &target); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestYAMLStructureRejectsAliasesAndDuplicateKeys(t *testing.T) {
	for name, source := range map[string]string{
		"alias":     "run: &shared echo safe\nother: *shared\n",
		"duplicate": "run: echo first\nrun: echo second\n",
		"nested":    "job:\n  env: &env\n    VALUE: safe\n  other: *env\n",
	} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
			t.Fatalf("%s parse: %v", name, err)
		}
		findings := &findingSet{}
		validateYAMLStructure("fixture", &doc, findings)
		if len(findings.items) == 0 {
			t.Errorf("%s structure was accepted", name)
		}
	}
}

func TestActionNodeDigestCoversCompleteStep(t *testing.T) {
	parseStep := func(source string) *yaml.Node {
		t.Helper()
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Content[0]
	}
	original := parseStep("name: Upload\nuses: actions/upload-artifact@0123456789012345678901234567890123456789\nwith:\n  path: evidence.json\n")
	originalDigest := actionNodeDigest(original)
	for _, mutation := range []string{
		"name: Upload\nuses: actions/upload-artifact@0123456789012345678901234567890123456789\nwith:\n  path: other.json\n",
		"name: Changed\nuses: actions/upload-artifact@0123456789012345678901234567890123456789\nwith:\n  path: evidence.json\n",
		"name: Upload\nif: always()\nuses: actions/upload-artifact@0123456789012345678901234567890123456789\nwith:\n  path: evidence.json\n",
	} {
		if actionNodeDigest(parseStep(mutation)) == originalDigest {
			t.Errorf("action step mutation retained digest: %q", mutation)
		}
	}
}

func TestStatementDigestCoversRedirectsAndAssignments(t *testing.T) {
	original := parseShell(t, `MODE=ci go test ./...`)
	originalDigest, err := statementDigest(original.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		`MODE=prod go test ./...`,
		`MODE=ci go test ./... > results.txt`,
		`MODE=ci go test ./... 2>&1`,
	} {
		file := parseShell(t, mutation)
		digest, err := statementDigest(file.Stmts[0])
		if err != nil {
			t.Fatal(err)
		}
		if digest == originalDigest {
			t.Errorf("statement mutation retained digest: %s", mutation)
		}
	}
}

func TestDangerousAssignmentsAndEnvironmentRedirects(t *testing.T) {
	for _, source := range []string{
		`PATH=/tmp/bin`,
		`BASH_ENV=./bootstrap`,
		`ENV=./profile`,
		`SHELLOPTS=xtrace`,
		`LD_PRELOAD=./hook.so command`,
		`DYLD_INSERT_LIBRARIES=./hook.dylib command`,
		`command exec env PATH=/tmp/bin go test ./...`,
		`echo value >> "$GITHUB_ENV"`,
		`echo value >> "${GITHUB_ENV:?missing}"`,
		`printf '%s\n' value >> "$GITHUB_PATH"`,
	} {
		file := parseShell(t, source)
		findings := &findingSet{}
		inspectStatementGuards("fixture", file.Stmts[0], findings)
		if len(findings.items) == 0 {
			t.Errorf("dangerous statement was accepted: %s", source)
		}
	}
}

func TestWorkingDirectoryIsCategoricallyRejected(t *testing.T) {
	for name, source := range map[string]string{
		"literal": "working-directory: ./subdir\n",
		"dynamic": "working-directory: ${{ vars.WORKDIR }}\n",
		"mapping": "working-directory:\n  path: ./subdir\n",
	} {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(source), &node); err != nil {
			t.Fatal(err)
		}
		findings := &findingSet{}
		checkWorkingDirectory("fixture "+name, node.Content[0], findings)
		if len(findings.items) == 0 {
			t.Errorf("%s working-directory was accepted", name)
		}
	}
}

func TestExecutionAffectingEnvironmentNames(t *testing.T) {
	for _, name := range []string{
		"PATH", "BASH_ENV", "ENV", "SHELLOPTS", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES",
		"NODE_OPTIONS", "PYTHONPATH", "PYTHONHOME", "RUBYOPT", "PERL5OPT", "PERL5LIB",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_COUNT", "GIT_SSH", "GIT_SSH_COMMAND", "HOME", "IFS", "CDPATH",
		"CC", "CXX", "AR", "LD", "GOROOT", "GOPATH", "GOENV", "GOFLAGS", "GOTOOLCHAIN",
		"LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH", "LIBRARY_PATH", "CPATH", "RUSTC_WRAPPER", "JAVA_TOOL_OPTIONS",
		"CFLAGS", "LDFLAGS", "GOEXPERIMENT", "GIT_EXEC_PATH", "CGO_LDFLAGS", "CARGO_HOME", "SHELL",
		"BASH_FUNC_attack%%", "bash_func_attack%%",
	} {
		if !executionAffectingEnv(name) {
			t.Errorf("%s was not rejected", name)
		}
	}
	for _, name := range []string{"GOPRIVATE", "GH_TOKEN", "GITHUB_TOKEN", "GOOS", "GOARCH", "RELEASES_TOKEN", "WFCTL_CONFORMANCE_VERSION"} {
		if executionAffectingEnv(name) {
			t.Errorf("required safe environment %s was rejected", name)
		}
	}
}

func TestNodeAuthTokenAllowsOnlyAutomaticGithubToken(t *testing.T) {
	for name, test := range map[string]struct {
		value       string
		wantFinding bool
	}{
		"automatic token": {"${{ github.token }}", false},
		"legacy secret":   {"${{ secrets.GITHUB_TOKEN }}", true},
		"named secret":    {"${{ secrets.PACKAGES_TOKEN }}", true},
		"literal":         {"token", true},
		"other context":   {"${{ vars.NODE_AUTH_TOKEN }}", true},
		"mixed value":     {"${{ github.token }}-suffix", true},
		"bracket context": {"${{ github['token'] }}", true},
		"expression":      {"${{ github.token || vars.FALLBACK }}", true},
	} {
		t.Run(name, func(t *testing.T) {
			var document yaml.Node
			source := "NODE_AUTH_TOKEN: " + test.value + "\n"
			if err := yaml.Unmarshal([]byte(source), &document); err != nil {
				t.Fatal(err)
			}
			findings := &findingSet{}
			envValues("fixture", document.Content[0], false, findings)
			if got := len(findings.items) > 0; got != test.wantFinding {
				t.Fatalf("findings = %v, wantFinding = %v", findings.items, test.wantFinding)
			}
		})
	}
}

func TestTrustGroupThreeStateLifecycle(t *testing.T) {
	active := strings.Repeat("a", 64)
	staged := strings.Repeat("b", 64)
	wf := ".github/workflows/wf.yml"
	transition := []trustGroup{{Path: wf, ContextSHA256: active, State: "active", Presence: "present"}, {Path: wf, ContextSHA256: staged, State: "staged", Presence: "present"}}
	for name, phase := range map[string]struct {
		groups  []trustGroup
		context string
	}{
		"phase1": {transition, active},
		"phase2": {transition, staged},
		"phase3": {[]trustGroup{{Path: wf, ContextSHA256: staged, State: "active", Presence: "present"}}, staged},
	} {
		selected, findings := selectTrustGroups(phase.groups, map[string]string{wf: phase.context}, map[string]bool{})
		if len(findings) != 0 || !selected[wf+"\x00"+phase.context] {
			t.Errorf("%s selection = %v, findings = %v", name, selected, findings)
		}
	}
	for _, phase := range []struct {
		name     string
		groups   []trustGroup
		contexts map[string]string
	}{
		{"add phase1", []trustGroup{{Path: ".github/workflows/new.yml", State: "active", Presence: "absent"}, {Path: ".github/workflows/new.yml", ContextSHA256: staged, State: "staged", Presence: "present"}}, map[string]string{}},
		{"add phase2", []trustGroup{{Path: ".github/workflows/new.yml", State: "active", Presence: "absent"}, {Path: ".github/workflows/new.yml", ContextSHA256: staged, State: "staged", Presence: "present"}}, map[string]string{".github/workflows/new.yml": staged}},
		{"delete phase2", []trustGroup{{Path: ".github/workflows/old.yml", ContextSHA256: active, State: "active", Presence: "present"}, {Path: ".github/workflows/old.yml", State: "staged", Presence: "absent"}}, map[string]string{}},
		{"absent tombstone", []trustGroup{{Path: ".github/workflows/old.yml", State: "active", Presence: "absent"}}, map[string]string{}},
	} {
		selected, findings := selectTrustGroups(phase.groups, phase.contexts, map[string]bool{})
		if len(findings) != 0 || len(selected) != 1 {
			t.Errorf("%s selection = %v, findings = %v", phase.name, selected, findings)
		}
	}
	for name, groups := range map[string][]trustGroup{
		"unmatched":       {{Path: wf, ContextSHA256: active, State: "active", Presence: "present"}},
		"mixed":           {{Path: wf, ContextSHA256: active, State: "active", Presence: "present"}, {Path: wf, ContextSHA256: active, State: "staged", Presence: "present"}},
		"multiple staged": {{Path: wf, ContextSHA256: active, State: "active", Presence: "present"}, {Path: wf, ContextSHA256: staged, State: "staged", Presence: "present"}, {Path: wf, ContextSHA256: strings.Repeat("c", 64), State: "staged", Presence: "present"}},
		"invalid state":   {{Path: wf, ContextSHA256: active, State: "pending", Presence: "present"}},
		"lone staged":     {{Path: wf, ContextSHA256: staged, State: "staged", Presence: "present"}},
	} {
		_, findings := selectTrustGroups(groups, map[string]string{wf: staged}, map[string]bool{})
		if len(findings) == 0 {
			t.Errorf("%s trust groups were accepted", name)
		}
	}
	duplicate := trustGroup{Path: wf, ContextSHA256: active, State: "active", Presence: "present"}
	if _, findings := selectTrustGroups([]trustGroup{duplicate, duplicate}, map[string]string{wf: active}, map[string]bool{}); len(findings) == 0 {
		t.Error("duplicate active trust groups were accepted")
	}
	for _, artifactPath := range []string{".github/conformance/cleanup.yaml", ".github/workflows/scripts/cleanup.sh", "docs/retired-runbook.md"} {
		group := trustGroup{Path: artifactPath, State: "active", Presence: "absent"}
		if _, findings := selectTrustGroups([]trustGroup{group}, map[string]string{}, map[string]bool{}); len(findings) != 0 {
			t.Errorf("artifact tombstone path %q was rejected: %v", artifactPath, findings)
		}
		if _, findings := selectTrustGroups([]trustGroup{group}, map[string]string{}, map[string]bool{artifactPath: true}); len(findings) == 0 {
			t.Errorf("artifact tombstone path %q did not reject an existing path", artifactPath)
		}
	}
	for _, invalidPath := range []string{"", "../../outside.yml", ".github\\workflows\\workflow.yml"} {
		group := trustGroup{Path: invalidPath, State: "active", Presence: "absent"}
		if _, findings := selectTrustGroups([]trustGroup{group}, map[string]string{}, map[string]bool{}); len(findings) == 0 {
			t.Errorf("invalid tombstone path %q was accepted", invalidPath)
		}
	}
}

func TestWorkflowSecretAuthorityBoundary(t *testing.T) {
	for name, test := range map[string]struct {
		secrets     map[string]bool
		wantFinding bool
	}{
		"workflow call noncloud":        {map[string]bool{"RELEASES_TOKEN": true}, true},
		"branch push noncloud":          {map[string]bool{"PUBLISH_TOKEN": true}, true},
		"tag push noncloud":             {map[string]bool{"REGISTRY_TOKEN": true}, true},
		"cloud secret":                  {map[string]bool{"DIGITALOCEAN_TOKEN": true}, true},
		"legacy automatic token syntax": {map[string]bool{"GITHUB_TOKEN": true}, true},
	} {
		findings := &findingSet{}
		validateSecretReferences("fixture.yml", "fixture "+name, test.secrets, map[string]bool{}, findings)
		if got := len(findings.items) > 0; got != test.wantFinding {
			t.Errorf("%s finding = %v, want %v: %v", name, got, test.wantFinding, findings.items)
		}
	}
}

func TestWorkflowCallEnvironmentRejectsNamedSecret(t *testing.T) {
	const workflowPath = ".github/workflows/reusable.yml"
	var doc yaml.Node
	source := "on: workflow_call\njobs:\n  deploy:\n    environment: production\n    uses: acme/platform/.github/workflows/deploy.yml@0123456789012345678901234567890123456789\n    secrets:\n      token: ${{ secrets.RELEASES_TOKEN }}\n"
	if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
		t.Fatal(err)
	}
	job := mappingValue(mappingValue(doc.Content[0], "jobs"), "deploy")
	secrets := secretReferences(job)
	findings := &findingSet{}
	validateSecretReferences(workflowPath, "fixture workflow_call", secrets, map[string]bool{}, findings)
	if len(findings.items) == 0 {
		t.Fatal("workflow_call environment accepted a named repository secret")
	}
}

func TestReusableWorkflowSecretShapesFailClosed(t *testing.T) {
	for name, test := range map[string]struct {
		source      string
		wantFinding bool
	}{
		"inherit scalar":  {"secrets: inherit\n", true},
		"inherit mapping": {"secrets:\n  token: inherit\n", true},
		"dynamic mapping": {"secrets:\n  token: ${{ secrets[vars.SECRET_NAME] }}\n", true},
		"automatic token": {"secrets:\n  token: ${{ github.token }}\n", false},
	} {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(test.source), &doc); err != nil {
			t.Fatal(err)
		}
		job := doc.Content[0]
		findings := &findingSet{}
		validateInheritedSecrets("fixture "+name, mappingValue(job, "secrets"), findings)
		validateCredentialSelectors("fixture "+name, job, findings)
		if got := len(findings.items) > 0; got != test.wantFinding {
			t.Errorf("%s finding = %v, want %v: %v", name, got, test.wantFinding, findings.items)
		}
	}
}

func TestOnlyLiteralGOWORKOffAssignmentIsSafe(t *testing.T) {
	for _, source := range []string{
		`GOWORK=off go test ./...`,
		`env GOWORK=off go test ./...`,
		`env "GOWORK=off" go test ./...`,
		`exec env GOWORK=off go test ./...`,
	} {
		findings := &findingSet{}
		inspectStatementGuards("fixture", parseShell(t, source).Stmts[0], findings)
		if len(findings.items) != 0 {
			t.Errorf("literal GOWORK=off in %q was rejected: %v", source, findings.items)
		}
	}

	for _, source := range []string{
		`GOWORK=auto go test ./...`,
		`GOWORK= go test ./...`,
		`GOWORK="$MODE" go test ./...`,
		`env GOWORK=auto go test ./...`,
		`env GOWORK="$MODE" go test ./...`,
		`env "GOWORK=$MODE" go test ./...`,
		`env "PATH=$MODE" go test ./...`,
		`env 'BASH_FUNC_attack%%=() { :; }' go test ./...`,
		`nice env 'BASH_FUNC_attack%%=() { :; }' bash -c attack`,
		`nice env "BASH_FUNC_attack%${PERCENT}=() { :; }" bash -c attack`,
	} {
		findings := &findingSet{}
		inspectStatementGuards("fixture", parseShell(t, source).Stmts[0], findings)
		if len(findings.items) == 0 {
			t.Errorf("non-literal-off GOWORK in %q was accepted", source)
		}
	}

	for name, source := range map[string]string{
		"literal off": "GOWORK: off\n",
		"auto":        "GOWORK: auto\n",
		"empty":       "GOWORK: ''\n",
		"dynamic":     "GOWORK: ${{ vars.GOWORK }}\n",
	} {
		var env yaml.Node
		if err := yaml.Unmarshal([]byte(source), &env); err != nil {
			t.Fatal(err)
		}
		findings := &findingSet{}
		envValues("fixture", env.Content[0], false, findings)
		if name == "literal off" && len(findings.items) != 0 {
			t.Errorf("literal YAML GOWORK=off was rejected: %v", findings.items)
		}
		if name != "literal off" && len(findings.items) == 0 {
			t.Errorf("%s YAML GOWORK assignment was accepted", name)
		}
	}
}

func TestOnlyFailClosedGitEnvironmentAssignmentsAreSafe(t *testing.T) {
	for _, source := range []string{
		`GIT_ASKPASS=/bin/false git fetch`,
		`GIT_CONFIG_GLOBAL=/dev/null git fetch`,
		`GIT_CONFIG_NOSYSTEM=1 git fetch`,
		`GIT_TERMINAL_PROMPT=0 git fetch`,
	} {
		findings := &findingSet{}
		inspectStatementGuards("fixture", parseShell(t, source).Stmts[0], findings)
		if len(findings.items) != 0 {
			t.Errorf("fail-closed Git environment in %q was rejected: %v", source, findings.items)
		}
	}

	for _, source := range []string{
		`GIT_ASKPASS=./candidate-helper git fetch`,
		`GIT_CONFIG_GLOBAL=./candidate-config git fetch`,
		`GIT_CONFIG_NOSYSTEM=0 git fetch`,
		`GIT_TERMINAL_PROMPT=1 git fetch`,
	} {
		findings := &findingSet{}
		inspectStatementGuards("fixture", parseShell(t, source).Stmts[0], findings)
		if len(findings.items) == 0 {
			t.Errorf("unsafe Git environment in %q was accepted", source)
		}
	}
}

func TestAuthorizationContextBindsCompleteWorkflow(t *testing.T) {
	parseMapping := func(source string) *yaml.Node {
		t.Helper()
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Content[0]
	}
	base := "on: push\nenv:\n  SAFE_MODE: one\ndefaults:\n  run:\n    shell: bash\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env:\n      JOB_MODE: one\n    steps:\n      - env:\n          STEP_MODE: one\n        run: echo safe\n"
	workflow := parseMapping(base)
	original := authorizationContextDigest(workflow, nil, nil)
	job := mappingValue(mappingValue(workflow, "jobs"), "test")
	step := mappingValue(job, "steps").Content[0]
	if got := authorizationContextDigest(workflow, job, step); got != original {
		t.Fatalf("job/step arguments narrowed workflow-wide context digest: got %s, want %s", got, original)
	}
	for name, changed := range map[string]string{
		"trigger":        strings.Replace(base, "on: push", "on: pull_request_target", 1),
		"workflow env":   strings.Replace(base, "SAFE_MODE: one", "SAFE_MODE: two", 1),
		"defaults":       strings.Replace(base, "shell: bash", "shell: sh", 1),
		"job env":        strings.Replace(base, "JOB_MODE: one", "JOB_MODE: two", 1),
		"container":      strings.Replace(base, "runs-on: ubuntu-latest", "runs-on: ubuntu-latest\n    container: attacker:latest", 1),
		"step control":   strings.Replace(base, "run: echo safe", "if: always()\n        run: echo safe", 1),
		"step statement": strings.Replace(base, "run: echo safe", "run: set -x; echo safe", 1),
	} {
		if authorizationContextDigest(parseMapping(changed), nil, nil) == original {
			t.Errorf("%s mutation retained complete workflow digest", name)
		}
	}
}

func TestNormalizePathResolvesRelativeToRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(workflowDir, "ci.yml")
	if err := os.WriteFile(workflowPath, []byte("name: CI\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := normalizePath(root, resolvedRoot, ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("normalize relative workflow path: %v", err)
	}
	if got != ".github/workflows/ci.yml" {
		t.Fatalf("normalized workflow path = %q, want .github/workflows/ci.yml", got)
	}

	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("name: outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePath(root, resolvedRoot, outside); err == nil {
		t.Fatal("absolute workflow path outside root was accepted")
	}
	if _, err := normalizePath(root, resolvedRoot, "../outside.yml"); err == nil {
		t.Fatal("relative workflow path outside root was accepted")
	}

	symlink := filepath.Join(workflowDir, "escape.yml")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePath(root, resolvedRoot, ".github/workflows/escape.yml"); err == nil {
		t.Fatal("workflow symlink escaping root was accepted")
	}
}

func TestAssignmentOnlyCallIsRecognized(t *testing.T) {
	file := parseShell(t, `SAFE_VALUE=one`)
	call := firstCall(t, file)
	if !assignmentOnlyCall(call) {
		t.Fatal("standalone safe assignment was not recognized")
	}
	if assignmentOnlyCall(firstCall(t, parseShell(t, `SAFE_VALUE=one echo safe`))) {
		t.Fatal("command with an assignment prefix was treated as assignment-only")
	}
}

func TestBuiltinRequiresExactStatementAuthority(t *testing.T) {
	file := parseShell(t, `set -x`)
	findings := &findingSet{}
	inspectShell(
		"fixture", ".github/workflows/fixture.yml", strings.Repeat("0", 64),
		"set -x", file, false, t.TempDir(),
		map[string]executableEntry{}, map[string]bool{},
		map[string]commandEntry{}, map[string]bool{}, findings,
	)
	if len(findings.items) != 1 || !strings.Contains(findings.items[0], "unreviewed exact statement containing set") {
		t.Fatalf("builtin authority findings = %v", findings.items)
	}
}

func TestStatementWithoutCallRequiresExactAuthority(t *testing.T) {
	file := parseShell(t, `(( X ))`)
	findings := &findingSet{}
	inspectShell(
		"fixture", ".github/workflows/fixture.yml", strings.Repeat("0", 64),
		"(( X ))", file, false, t.TempDir(),
		map[string]executableEntry{}, map[string]bool{},
		map[string]commandEntry{}, map[string]bool{}, findings,
	)
	if len(findings.items) != 1 || !strings.Contains(findings.items[0], "unreviewed exact shell statement") {
		t.Fatalf("call-free statement authority findings = %v", findings.items)
	}
}

func TestJobContainerAndServicesAreCategoricallyRejected(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("container: attacker:latest\nservices:\n  db:\n    image: attacker:latest\n"), &doc); err != nil {
		t.Fatal(err)
	}
	findings := &findingSet{}
	checkJobRuntime("fixture", doc.Content[0], findings)
	if len(findings.items) != 2 {
		t.Fatalf("job runtime findings = %v", findings.items)
	}
}

func TestKnownProviderCommandLimitsReviewedPackageBuildSubcommands(t *testing.T) {
	for _, subcommand := range []string{"ci", "run", "test"} {
		if knownProviderCommand("npm", []string{subcommand}) {
			t.Errorf("npm %s must remain eligible for exact Workflow UI build review", subcommand)
		}
	}
	for _, argv := range [][]string{nil, {"exec", "provider-tool"}, {"install"}} {
		if !knownProviderCommand("npm", argv) {
			t.Errorf("npm argv %q must remain categorically forbidden", argv)
		}
	}
	if categoricallyUnallowlistableCommandName("npm") {
		t.Fatal("npm exact statement rows must remain eligible for manifest validation")
	}
}

func TestContainsProviderSDKMarkerPreservesFixedSubstrings(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  bool
	}{
		"digitalocean":        {"prefix DIGITALOCEAN/GODO suffix", true},
		"aws":                 {"github.com/aws/AWS-SDK-go-v2", true},
		"azure":               {"Azure-SDK-for-go", true},
		"google go":           {"CLOUD.GOOGLE.COM/GO/storage", true},
		"google other":        {"google-cloud-storage", true},
		"digitalocean dash":   {"digitalocean-godo", false},
		"aws underscore":      {"aws_sdk", false},
		"azure slash":         {"azure/sdk", false},
		"google wildcard dot": {"cloudXgoogle.com/go", false},
		"google no dash":      {"google-cloud", false},
		"empty":               {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := containsProviderSDKMarker(test.value); got != test.want {
				t.Fatalf("containsProviderSDKMarker(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestKnownProviderCommandAllowsLocalHelmValidation(t *testing.T) {
	if !knownProviderCommand("helm", nil) {
		t.Error("bare helm invocation must remain categorically forbidden")
	}
	for _, subcommand := range []string{"lint", "template"} {
		if knownProviderCommand("helm", []string{subcommand}) {
			t.Errorf("helm %s must remain eligible for exact local validation review", subcommand)
		}
	}
	if !knownProviderCommand("helm", []string{"upgrade"}) {
		t.Fatal("helm upgrade must remain categorically forbidden")
	}
	if categoricallyUnallowlistableCommandName("helm") {
		t.Fatal("helm exact statement rows must remain eligible for manifest validation")
	}
}

func TestKnownProviderCommandRejectsMutableGoInstallVersions(t *testing.T) {
	for _, packageArg := range []string{
		"golang.org/x/perf/cmd/benchstat@latest",
		"example.com/tool@upgrade",
		"example.com/tool@patch",
		"example.com/tool@main",
		"example.com/tool@>v1.2.3",
		"example.com/tool@v1.2",
	} {
		if !knownProviderCommand("go", []string{"install", packageArg}) {
			t.Errorf("mutable go install selector remained allowlistable: %s", packageArg)
		}
	}
	for _, packageArg := range []string{
		"example.com/tool@v1.2.3",
		"golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d",
		"./cmd/wfctl",
	} {
		if knownProviderCommand("go", []string{"install", packageArg}) {
			t.Errorf("immutable or local go install target became categorical: %s", packageArg)
		}
	}
}

func TestMutableGoInstallCannotBeAuthorizedByExactStatement(t *testing.T) {
	const workflowPath = ".github/workflows/install.yml"
	const source = "go install golang.org/x/perf/cmd/benchstat@latest"
	contextSHA256 := strings.Repeat("e", 64)
	file := parseShell(t, source)
	statementSHA256, err := statementDigest(file.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	key := commandKey(workflowPath, "go", statementSHA256, contextSHA256)
	commands := map[string]commandEntry{key: {
		Path: workflowPath, Command: "go", StatementSHA256: statementSHA256,
		ContextSHA256: contextSHA256, State: "active", Rationale: "Exact mutable install mutation row.",
	}}
	commandReferenced := map[string]bool{}
	findings := &findingSet{}
	inspectShell(
		"fixture", workflowPath, contextSHA256, source, file, false, t.TempDir(),
		map[string]executableEntry{}, map[string]bool{}, commands, commandReferenced, findings,
	)
	if !strings.Contains(strings.Join(findings.items, "\n"), "executes categorically forbidden command go") {
		t.Fatalf("exact row authorized mutable go install: %v", findings.items)
	}
	if commandReferenced[key] {
		t.Fatal("categorical mutable go install rejection consumed exact authority row")
	}
}

func TestPinnedGoInstallCanUseExactStatementAuthority(t *testing.T) {
	const workflowPath = ".github/workflows/install.yml"
	const source = "go install golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d"
	contextSHA256 := strings.Repeat("f", 64)
	file := parseShell(t, source)
	statementSHA256, err := statementDigest(file.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	key := commandKey(workflowPath, "go", statementSHA256, contextSHA256)
	commands := map[string]commandEntry{key: {
		Path: workflowPath, Command: "go", StatementSHA256: statementSHA256,
		ContextSHA256: contextSHA256, State: "active", Rationale: "Exact pinned install row.",
	}}
	commandReferenced := map[string]bool{}
	findings := &findingSet{}
	inspectShell(
		"fixture", workflowPath, contextSHA256, source, file, false, t.TempDir(),
		map[string]executableEntry{}, map[string]bool{}, commands, commandReferenced, findings,
	)
	if len(findings.items) != 0 {
		t.Fatalf("pinned go install exact authority was rejected: %v", findings.items)
	}
	if !commandReferenced[key] {
		t.Fatal("pinned go install exact authority row was not consumed")
	}
}

func TestEvalIsCategoricallyUnallowlistable(t *testing.T) {
	for _, command := range []string{"eval", "EVAL", "/usr/bin/eval", "./eval"} {
		if !categoricallyUnallowlistableCommandName(command) {
			t.Errorf("eval variant remained allowlistable: %s", command)
		}
	}
	if categoricallyUnallowlistableCommandName("printf") {
		t.Fatal("ordinary printf command became categorically forbidden")
	}
}

func TestEvalCannotBeAuthorizedByExactStatement(t *testing.T) {
	const workflowPath = ".github/workflows/eval.yml"
	contextSHA256 := strings.Repeat("a", 64)
	for _, source := range []string{
		`eval "$COMMAND"`,
		`command eval "$COMMAND"`,
		`env eval "$COMMAND"`,
		`/usr/bin/eval "$COMMAND"`,
		`./eval "$COMMAND"`,
		`\eval "$COMMAND"`,
	} {
		t.Run(source, func(t *testing.T) {
			file := parseShell(t, source)
			statementSHA256, err := statementDigest(file.Stmts[0])
			if err != nil {
				t.Fatal(err)
			}
			commandKey := commandKey(workflowPath, "eval", statementSHA256, contextSHA256)
			commands := map[string]commandEntry{commandKey: {
				Path: workflowPath, Command: "eval", StatementSHA256: statementSHA256,
				ContextSHA256: contextSHA256, State: "active", Rationale: "Exact eval mutation row.",
			}}
			executableKey := workflowPath + "\x00eval"
			executables := map[string]executableEntry{executableKey: {
				Path: "eval", WorkflowPath: workflowPath, ContextSHA256: contextSHA256,
				State: "active", SHA256: strings.Repeat("b", 64), Rationale: "Local eval mutation executable.",
			}}
			commandReferenced := map[string]bool{}
			findings := &findingSet{}
			inspectShell(
				"fixture", workflowPath, contextSHA256, source, file, false, t.TempDir(),
				executables, map[string]bool{}, commands, commandReferenced, findings,
			)
			if !strings.Contains(strings.Join(findings.items, "\n"), "executes categorically forbidden command eval") {
				t.Fatalf("exact row authorized eval variant %q: %v", source, findings.items)
			}
			if commandReferenced[commandKey] {
				t.Fatalf("categorical eval rejection consumed exact authority row for %q", source)
			}
		})
	}
}

func TestBuiltinIsCategoricallyUnallowlistable(t *testing.T) {
	for _, command := range []string{"builtin", "BUILTIN", `\builtin`} {
		if !categoricallyUnallowlistableCommandName(command) {
			t.Errorf("builtin variant remained allowlistable: %s", command)
		}
	}
	if categoricallyUnallowlistableCommandName("printf") {
		t.Fatal("ordinary printf command became categorically forbidden")
	}
}

func TestBuiltinCannotBeAuthorizedByExactStatement(t *testing.T) {
	const workflowPath = ".github/workflows/builtin.yml"
	contextSHA256 := strings.Repeat("c", 64)
	for _, source := range []string{
		`builtin eval "$COMMAND"`,
		`command builtin eval "$COMMAND"`,
		`builtin source "$SCRIPT"`,
		`command builtin source "$SCRIPT"`,
	} {
		t.Run(source, func(t *testing.T) {
			file := parseShell(t, source)
			statementSHA256, err := statementDigest(file.Stmts[0])
			if err != nil {
				t.Fatal(err)
			}
			commandKey := commandKey(workflowPath, "builtin", statementSHA256, contextSHA256)
			commands := map[string]commandEntry{commandKey: {
				Path: workflowPath, Command: "builtin", StatementSHA256: statementSHA256,
				ContextSHA256: contextSHA256, State: "active", Rationale: "Exact builtin mutation row.",
			}}
			commandReferenced := map[string]bool{}
			findings := &findingSet{}
			inspectShell(
				"fixture", workflowPath, contextSHA256, source, file, false, t.TempDir(),
				map[string]executableEntry{}, map[string]bool{}, commands, commandReferenced, findings,
			)
			if !strings.Contains(strings.Join(findings.items, "\n"), "executes categorically forbidden command builtin") {
				t.Fatalf("exact row authorized builtin wrapper %q: %v", source, findings.items)
			}
			if commandReferenced[commandKey] {
				t.Fatalf("categorical builtin rejection consumed exact authority row for %q", source)
			}
		})
	}
}

func TestOrdinaryExactCommandAuthorityRemainsAvailable(t *testing.T) {
	const workflowPath = ".github/workflows/ordinary.yml"
	const source = `printf '%s\n' "$VALUE"`
	contextSHA256 := strings.Repeat("d", 64)
	file := parseShell(t, source)
	statementSHA256, err := statementDigest(file.Stmts[0])
	if err != nil {
		t.Fatal(err)
	}
	key := commandKey(workflowPath, "printf", statementSHA256, contextSHA256)
	commands := map[string]commandEntry{key: {
		Path: workflowPath, Command: "printf", StatementSHA256: statementSHA256,
		ContextSHA256: contextSHA256, State: "active", Rationale: "Exact ordinary command row.",
	}}
	commandReferenced := map[string]bool{}
	findings := &findingSet{}
	inspectShell(
		"fixture", workflowPath, contextSHA256, source, file, false, t.TempDir(),
		map[string]executableEntry{}, map[string]bool{}, commands, commandReferenced, findings,
	)
	if len(findings.items) != 0 {
		t.Fatalf("ordinary exact command authority was rejected: %v", findings.items)
	}
	if !commandReferenced[key] {
		t.Fatal("ordinary exact command authority row was not consumed")
	}
}

func TestReusableWorkflowJobIsCategoricallyRejected(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("uses: acme/repo/.github/workflows/check.yml@0123456789012345678901234567890123456789\n"), &doc); err != nil {
		t.Fatal(err)
	}
	findings := &findingSet{}
	checkJobRunnerSelector("fixture", doc.Content[0], findings)
	if len(findings.items) == 0 || !strings.Contains(findings.items[0], "reusable workflow") {
		t.Fatalf("reusable job was not categorically rejected: %v", findings.items)
	}
}

func TestTrustedPolicyExecutableUsesCoreWrapperPath(t *testing.T) {
	entry := executableEntry{
		WorkflowPath: ".github/workflows/public-workflow-policy.yml",
		Path:         "scripts/check-public-workflow-policy.sh",
	}
	if !trustedPolicyExecutable(entry) {
		t.Fatal("core policy wrapper was not recognized as trusted")
	}
	entry.Path = ".github/workflows/scripts/check-public-workflow-policy.sh"
	if trustedPolicyExecutable(entry) {
		t.Fatal("legacy plugin wrapper location remained trusted")
	}
}

func authorityFixture(state, digest string) authorityBundle {
	return authorityBundle{
		State: state,
		Files: []authorityFile{
			{Path: ".github/workflows/policytool/go.mod", SHA256: digest},
			{Path: ".github/workflows/policytool/go.sum", SHA256: digest},
			{Path: ".github/workflows/policytool/main.go", SHA256: digest},
			{Path: ".github/workflows/policytool/main_test.go", SHA256: digest},
			{Path: ".github/workflows/scripts/verify-public-workflow-branch-protection.sh", SHA256: digest},
			{Path: "scripts/check-public-workflow-policy.sh", SHA256: digest},
			{Path: "scripts/fixtures/public-workflow-policy/pass.yml", SHA256: digest},
			{Path: "scripts/test-check-public-workflow-policy.sh", SHA256: digest},
		},
	}
}

func TestAuthorityManifestRejectsMalformedOrIncompleteSurface(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := authorityManifest{Version: 1, Bundles: []authorityBundle{authorityFixture("active", digest)}}
	if findings := validateAuthorityManifest(valid); len(findings) != 0 {
		t.Fatalf("valid authority manifest findings = %v", findings)
	}

	for name, mutate := range map[string]func(*authorityManifest){
		"version": func(manifest *authorityManifest) { manifest.Version = 2 },
		"no active": func(manifest *authorityManifest) {
			manifest.Bundles[0].State = "staged"
		},
		"two staged": func(manifest *authorityManifest) {
			manifest.Bundles = append(manifest.Bundles,
				authorityFixture("staged", strings.Repeat("b", 64)),
				authorityFixture("staged", strings.Repeat("c", 64)))
		},
		"unsorted": func(manifest *authorityManifest) {
			manifest.Bundles[0].Files[0], manifest.Bundles[0].Files[1] = manifest.Bundles[0].Files[1], manifest.Bundles[0].Files[0]
		},
		"duplicate": func(manifest *authorityManifest) {
			manifest.Bundles[0].Files[1] = manifest.Bundles[0].Files[0]
		},
		"outside surface": func(manifest *authorityManifest) {
			manifest.Bundles[0].Files[0].Path = ".github/public-workflow-authority.json"
		},
		"bad digest": func(manifest *authorityManifest) {
			manifest.Bundles[0].Files[0].SHA256 = "ABC"
		},
		"missing core": func(manifest *authorityManifest) {
			manifest.Bundles[0].Files = manifest.Bundles[0].Files[1:]
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Bundles = append([]authorityBundle(nil), valid.Bundles...)
			candidate.Bundles[0].Files = append([]authorityFile(nil), valid.Bundles[0].Files...)
			mutate(&candidate)
			if findings := validateAuthorityManifest(candidate); len(findings) == 0 {
				t.Fatal("invalid authority manifest was accepted")
			}
		})
	}
}

func TestAuthorityInventoryRejectsExtraAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	for _, file := range authorityFixture("active", strings.Repeat("a", 64)).Files {
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(file.Path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if inventory, err := authorityInventory(root); err != nil || len(inventory) != 8 {
		t.Fatalf("authority inventory = %v, err = %v", inventory, err)
	}

	extra := filepath.Join(root, ".github", "workflows", "policytool", "policytool")
	if err := os.WriteFile(extra, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if inventory, err := authorityInventory(root); err != nil || len(inventory) != 9 {
		t.Fatalf("recursive authority inventory = %v, err = %v", inventory, err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "scripts", "check-public-workflow-policy.sh")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("test-check-public-workflow-policy.sh", target); err != nil {
		t.Fatal(err)
	}
	if _, err := authorityInventory(root); err == nil || !strings.Contains(err.Error(), "non-regular authority path") {
		t.Fatalf("symlink authority file error = %v", err)
	}
}

func TestAuthorityTransitionLifecycle(t *testing.T) {
	oldBundle := authorityFixture("active", strings.Repeat("a", 64))
	newBundle := authorityFixture("staged", strings.Repeat("b", 64))
	activeNew := newBundle
	activeNew.State = "active"
	baseActive := authorityManifest{Version: 1, Bundles: []authorityBundle{oldBundle}}
	baseStaged := authorityManifest{Version: 1, Bundles: []authorityBundle{oldBundle, newBundle}}
	promoted := authorityManifest{Version: 1, Bundles: []authorityBundle{activeNew}}

	for name, test := range map[string]struct {
		base, candidate             authorityManifest
		baseInventory, candidateInv []authorityFile
		bootstrap                   bool
		wantFinding                 bool
	}{
		"unchanged":       {baseActive, baseActive, oldBundle.Files, oldBundle.Files, false, false},
		"stage":           {baseActive, baseStaged, oldBundle.Files, oldBundle.Files, false, false},
		"adopt":           {baseStaged, baseStaged, oldBundle.Files, newBundle.Files, false, false},
		"promote":         {baseStaged, promoted, newBundle.Files, newBundle.Files, false, false},
		"bootstrap":       {authorityManifest{}, baseActive, nil, oldBundle.Files, true, false},
		"stage and adopt": {baseActive, baseStaged, oldBundle.Files, newBundle.Files, false, true},
		"old after adopt": {baseStaged, baseStaged, newBundle.Files, oldBundle.Files, false, true},
		"old after promote": {
			baseStaged, promoted, newBundle.Files, oldBundle.Files, false, true,
		},
		"bootstrap staged": {authorityManifest{}, baseStaged, nil, oldBundle.Files, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			findings := validateAuthorityTransition(&test.base, test.candidate, test.baseInventory, test.candidateInv, test.bootstrap)
			if got := len(findings) > 0; got != test.wantFinding {
				t.Fatalf("findings = %v, wantFinding = %v", findings, test.wantFinding)
			}
			if name == "stage and adopt" && !strings.Contains(strings.Join(findings, "\n"), "same pull request") {
				t.Fatalf("same-PR authority rejection was not explicit: %v", findings)
			}
		})
	}
}

func trustPolicyFixture(activeContext string) trustPolicy {
	workflow := ".github/workflows/ci.yml"
	return trustPolicy{
		Presence: []trustGroup{{Path: workflow, ContextSHA256: activeContext, State: "active", Presence: "present"}},
		Commands: []commandEntry{{
			Path: workflow, Command: "go", StatementSHA256: strings.Repeat("c", 64), ContextSHA256: activeContext,
			State: "active", Rationale: "Run the reviewed test command.",
		}},
	}
}

func TestTrustManifestTransitionLifecycle(t *testing.T) {
	oldContext := strings.Repeat("a", 64)
	newContext := strings.Repeat("b", 64)
	workflow := ".github/workflows/ci.yml"
	base := trustPolicyFixture(oldContext)
	staged := base
	staged.Presence = append([]trustGroup(nil), base.Presence...)
	staged.Commands = append([]commandEntry(nil), base.Commands...)
	staged.Presence = append(staged.Presence, trustGroup{Path: workflow, ContextSHA256: newContext, State: "staged", Presence: "present"})
	staged.Commands = append(staged.Commands, commandEntry{
		Path: workflow, Command: "go", StatementSHA256: strings.Repeat("d", 64), ContextSHA256: newContext,
		State: "staged", Rationale: "Run the replacement reviewed test command.",
	})
	promoted := trustPolicyFixture(newContext)
	promoted.Commands[0].StatementSHA256 = strings.Repeat("d", 64)
	promoted.Commands[0].Rationale = "Run the replacement reviewed test command."

	for name, test := range map[string]struct {
		base, candidate               trustPolicy
		baseContexts, candidateCtx    map[string]string
		basePresent, candidatePresent map[string]bool
		wantFinding                   bool
	}{
		"unchanged":               {base, base, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, false},
		"stage":                   {base, staged, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, false},
		"adopt":                   {staged, staged, map[string]string{workflow: oldContext}, map[string]string{workflow: newContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, false},
		"promote":                 {staged, promoted, map[string]string{workflow: newContext}, map[string]string{workflow: newContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, false},
		"same PR stage and adopt": {base, staged, map[string]string{workflow: oldContext}, map[string]string{workflow: newContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, true},
		"promote before adopt":    {staged, promoted, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}, true},
	} {
		t.Run(name, func(t *testing.T) {
			findings := validateTrustTransition(test.base, test.candidate, test.baseContexts, test.candidateCtx, test.basePresent, test.candidatePresent)
			if got := len(findings) > 0; got != test.wantFinding {
				t.Fatalf("findings = %v, wantFinding = %v", findings, test.wantFinding)
			}
			if name == "same PR stage and adopt" && !strings.Contains(strings.Join(findings, "\n"), "same pull request") {
				t.Fatalf("same-PR trust rejection was not explicit: %v", findings)
			}
		})
	}

	t.Run("secret manifest must remain empty", func(t *testing.T) {
		candidate := base
		candidate.Secrets = []allowEntry{{Path: workflow, Secret: "TOKEN", ContextSHA256: oldContext, State: "active", Rationale: "not allowed"}}
		if findings := validateTrustTransition(base, candidate, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}); len(findings) == 0 {
			t.Fatal("non-empty secret manifest was accepted")
		}
	})

	t.Run("mixed staged contexts", func(t *testing.T) {
		candidate := staged
		candidate.Actions = []actionEntry{{Path: workflow, Uses: "actions/checkout@" + strings.Repeat("e", 40), NodeSHA256: strings.Repeat("f", 64), ContextSHA256: strings.Repeat("9", 64), State: "staged", Rationale: "Unrelated context."}}
		if findings := validateTrustTransition(base, candidate, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}); len(findings) == 0 {
			t.Fatal("mixed staged contexts were accepted")
		}
	})

	t.Run("drops active row", func(t *testing.T) {
		candidate := staged
		candidate.Commands = candidate.Commands[1:]
		if findings := validateTrustTransition(base, candidate, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}); len(findings) == 0 {
			t.Fatal("stage transition that dropped an active row was accepted")
		}
	})

	t.Run("malformed staged presence", func(t *testing.T) {
		candidate := staged
		candidate.Presence = append([]trustGroup(nil), staged.Presence...)
		candidate.Presence[1].Presence = "pending"
		if findings := validateTrustTransition(base, candidate, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}); len(findings) == 0 {
			t.Fatal("malformed staged presence was accepted")
		}
	})

	t.Run("malformed staged action", func(t *testing.T) {
		candidate := staged
		candidate.Actions = []actionEntry{{
			Path: workflow, Uses: "actions/checkout@main", NodeSHA256: strings.Repeat("e", 64), ContextSHA256: newContext,
			State: "staged", Rationale: "Mutable action reference.",
		}}
		if findings := validateTrustTransition(base, candidate, map[string]string{workflow: oldContext}, map[string]string{workflow: oldContext}, map[string]bool{workflow: true}, map[string]bool{workflow: true}); len(findings) == 0 {
			t.Fatal("malformed staged action was accepted")
		}
	})
}

func TestValidateRepositoryAuthorityLoadsAndChecksRealizedFiles(t *testing.T) {
	root := t.TempDir()
	for _, file := range authorityFixture("active", strings.Repeat("a", 64)).Files {
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(file.Path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inventory, err := authorityInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest := authorityManifest{Version: 1, Bundles: []authorityBundle{{State: "active", Files: inventory}}}
	writeJSON := func(filePath string, value any) {
		t.Helper()
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(filePath)), append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(".github/public-workflow-authority.json", manifest)
	writeJSON(".github/public-workflow-secret-allowlist.json", []allowEntry{})
	writeJSON(".github/public-workflow-executable-allowlist.json", []executableEntry{})
	writeJSON(".github/public-workflow-command-allowlist.json", []commandEntry{})
	writeJSON(".github/public-workflow-action-allowlist.json", []actionEntry{})
	writeJSON(".github/public-workflow-presence-allowlist.json", []trustGroup{{Path: ".github/workflows/retired.yml", State: "active", Presence: "absent"}})

	if findings, err := validateRepositoryAuthority(root, root, false); err != nil || len(findings) != 0 {
		t.Fatalf("valid repository authority findings = %v, err = %v", findings, err)
	}
	authorityPath := filepath.Join(root, ".github", "public-workflow-authority.json")
	authorityData, err := os.ReadFile(authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(authorityPath); err != nil {
		t.Fatal(err)
	}
	externalAuthority := filepath.Join(t.TempDir(), "authority.json")
	if err := os.WriteFile(externalAuthority, authorityData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalAuthority, authorityPath); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRepositoryAuthority(root, root, false); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked authority manifest error = %v", err)
	}
	if err := os.Remove(authorityPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorityPath, authorityData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "policytool", "main.go"), []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if findings, err := validateRepositoryAuthority(root, root, false); err != nil || len(findings) == 0 {
		t.Fatalf("mutated realized authority findings = %v, err = %v", findings, err)
	}
}

func TestAuthorityInventoryRejectsSymlinkedFixedAncestor(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	for _, file := range authorityFixture("active", strings.Repeat("a", 64)).Files {
		if file.Path == ".github/workflows/scripts/verify-public-workflow-branch-protection.sh" {
			continue
		}
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte(file.Path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	externalVerifier := filepath.Join(external, "verify-public-workflow-branch-protection.sh")
	if err := os.WriteFile(externalVerifier, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptsPath := filepath.Join(root, ".github", "workflows", "scripts")
	if err := os.Symlink(external, scriptsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := authorityInventory(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked fixed-file ancestor error = %v", err)
	}
}

// Run only the real dependency-fixture function. Calling the full harness from
// its own policy-tool test would recurse into go test.
func TestPolicyFetchFixtureOwnsEventBoundary(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	harness, err := os.ReadFile(filepath.Join(root, "scripts", "test-check-public-workflow-policy.sh"))
	if err != nil {
		t.Fatalf("read policy mutation harness: %v", err)
	}
	const signature = "\nrun_candidate_fetch() {\n"
	if strings.Count(string(harness), signature) != 1 {
		t.Fatal("policy mutation harness must define one fetch fixture")
	}
	start := strings.Index(string(harness), signature) + 1
	end := strings.Index(string(harness)[start:], "\n}\n")
	if end < 0 {
		t.Fatal("fetch fixture closing boundary missing")
	}
	fixture := string(harness)[start : start+end+3]
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash: %v", err)
	}
	const probe = `set -euo pipefail
[[ "${EVENT_NAME}:${BASE_REF}:${EVENT_REF}" == pull_request_target:main:refs/heads/main ]] || exit 1
[[ "$CANDIDATE_REPOSITORY" == example/repo ]] || exit 1
[[ "$CANDIDATE_SHA" == 0123456789012345678901234567890123456789 ]] || exit 1
[[ "$GIT_ASKPASS" == /bin/false ]] || exit 1
[[ "$GIT_CONFIG_GLOBAL" == /dev/null ]] || exit 1
[[ "$GIT_CONFIG_NOSYSTEM" == 1 ]] || exit 1
[[ "$GIT_TERMINAL_PROMPT" == 0 ]] || exit 1
printf 'fixture-event-boundary-ok\n'
`
	for _, tc := range []struct {
		name string
		env  []string
	}{
		{name: "unset event variables"},
		{name: "hostile inherited event", env: []string{
			"EVENT_NAME=workflow_run", "BASE_REF=develop", "EVENT_REF=refs/tags/v0.0.0",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := t.TempDir()
			probePath := filepath.Join(leaf, "fetch-boundary.sh")
			if err := os.WriteFile(probePath, []byte(probe), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(leaf, "fake-bin"), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			body := "set -euo pipefail\n" + fixture + `
fake_bin="$1/fake-bin"
candidate_fetch_script="$2"
run_candidate_fetch "$1/work" example/repo 0123456789012345678901234567890123456789 "$1/archive"
`
			cmd := exec.CommandContext(ctx, bash, "-c", body, "fetch-fixture-test", leaf, probePath)
			cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, tc.env...)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != "fixture-event-boundary-ok\n" {
				t.Fatalf("fetch fixture must own its event tuple: err=%v output=%q", err, out)
			}
		})
	}
}

// These are authority-boundary tests, not native test acceptance evidence.
const equivalentBudgetFixtureCI = "name: CI\n\non:\n  push:\n    branches: [ main, develop ]\n  pull_request:\n    branches: [ main, develop ]\n\npermissions:\n  contents: read\n  packages: read\n\ndefaults:\n  run:\n    shell: bash\n\nenv:\n  # CI runners are ephemeral; the iac/diffcache filesystem cache would\n  # cold-miss every job and waste a few ms on disk I/O. Force in-memory\n  # mode per the T3.5 rev3 lifecycle constraint \u2014 no filesystem writes\n  # in containerized runners. Operators running wfctl locally see the\n  # default filesystem cache at ~/.cache/wfctl/diff/.\n  WFCTL_DIFFCACHE: \":memory:\"\n\njobs:\n  public-workflow-policy:\n    name: Public Workflow Policy\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n    - name: Enforce credential-free public workflow policy\n      run: ./scripts/check-public-workflow-policy.sh\n    - name: Exercise public workflow policy mutations\n      run: ./scripts/test-check-public-workflow-policy.sh\n\n  test:\n    name: Test (Go ${{ matrix.go-version }})\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n    strategy:\n      matrix:\n        go-version: ['1.27.2']\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: ${{ matrix.go-version }}\n        cache: false\n\n    - name: Set up Node.js\n      uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0\n      with:\n        node-version: '24'\n        registry-url: 'https://npm.pkg.github.com'\n        cache: 'npm'\n        cache-dependency-path: ui/package-lock.json\n\n    - name: Build UI assets\n      env:\n        NODE_AUTH_TOKEN: ${{ github.token }}\n      run: |\n        cd ui && npm ci && npm run build\n\n    - name: Download dependencies\n      run: |\n        go mod download\n        go mod verify\n\n    - name: Run tests\n      run: |\n        go test -v -race -coverprofile=coverage.out ./...\n\n    - name: Upload coverage reports\n      uses: codecov/codecov-action@75cd11691c0faa626561e295848008c8a7dddffe # v5.5.4\n      if: always()\n      with:\n        files: ./coverage.out\n        fail_ci_if_error: false\n\n  go-tests:\n    name: Go Tests\n    runs-on: ubuntu-latest\n    needs: [test]\n    if: always()\n    timeout-minutes: 1\n    permissions: {}\n    steps:\n    - name: Require successful native test job\n      env:\n        TEST_RESULT: ${{ needs.test.result }}\n      run: test \"${TEST_RESULT}\" = success\n\n  lint:\n    name: Lint\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Set up Node.js\n      uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0\n      with:\n        node-version: '24'\n        registry-url: 'https://npm.pkg.github.com'\n        cache: 'npm'\n        cache-dependency-path: ui/package-lock.json\n\n    - name: Build UI assets\n      env:\n        NODE_AUTH_TOKEN: ${{ github.token }}\n      run: |\n        cd ui && npm ci && npm run build\n\n    - name: Run golangci-lint\n      uses: golangci/golangci-lint-action@82606bf257cbaff209d206a39f5134f0cfbfd2ee # v9.2.1\n      with:\n        version: v2.14.0\n        args: --timeout=10m\n\n  build:\n    name: Build\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Set up Node.js\n      uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0\n      with:\n        node-version: '24'\n        registry-url: 'https://npm.pkg.github.com'\n        cache: 'npm'\n        cache-dependency-path: ui/package-lock.json\n\n    - name: Build UI assets\n      env:\n        NODE_AUTH_TOKEN: ${{ github.token }}\n      run: |\n        cd ui && npm ci && npm run build\n\n    - name: Build server binary\n      run: go build -v -o /dev/null ./cmd/server\n\n    - name: Build all packages\n      run: go build -v ./...\n\n    - name: Upload UI build artifact\n      uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1\n      with:\n        name: admin-ui-dist\n        path: ui/dist/\n        retention-days: 3\n\n  build-examples:\n    name: Build Examples\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Build examples\n      run: |\n        cd example\n        go build -v ./...\n\n  lint-examples:\n    name: Lint Examples\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Run golangci-lint on examples\n      uses: golangci/golangci-lint-action@82606bf257cbaff209d206a39f5134f0cfbfd2ee # v9.2.1\n      with:\n        version: v2.14.0\n        args: --timeout=10m\n        working-directory: example\n\n  go-mod-tidy:\n    name: Go Mod Tidy\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n      with:\n        token: ${{ github.token }}\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Run go mod tidy (workflow)\n      run: go mod tidy\n\n    - name: Run go mod tidy (examples)\n      run: |\n        cd example\n        go mod tidy\n\n    - name: Commit tidy changes\n      if: github.event_name == 'push'\n      run: |\n        git config user.name \"github-actions[bot]\"\n        git config user.email \"github-actions[bot]@users.noreply.github.com\"\n        git diff --quiet go.mod go.sum example/go.mod example/go.sum || {\n          git add go.mod go.sum example/go.mod example/go.sum\n          git commit -m \"chore: go mod tidy\"\n          git push\n        }\n\n    - name: Fail if go mod tidy produced changes (PR check)\n      if: github.event_name == 'pull_request'\n      run: |\n        git diff --exit-code go.mod go.sum\n        git diff --exit-code example/go.mod example/go.sum\n\n  ui-test:\n    name: UI Tests\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n      packages: read\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Node.js\n      uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0\n      with:\n        node-version: '24'\n        registry-url: 'https://npm.pkg.github.com'\n        cache: 'npm'\n        cache-dependency-path: ui/package-lock.json\n\n    - name: Install dependencies\n      env:\n        NODE_AUTH_TOKEN: ${{ github.token }}\n      run: cd ui && npm ci\n\n    - name: Lint\n      run: cd ui && npm run lint\n\n    - name: Unit tests\n      run: cd ui && npm test -- --run\n\n  # Validate all example workflow configs at three levels:\n  # 1. YAML parsing (can it be loaded?)\n  # 2. Schema validation (correct types, required fields, valid references?)\n  # 3. Engine loading (can BuildFromConfig produce a working engine?)\n  example-configs:\n    name: Validate Example Configs\n    runs-on: ubuntu-latest\n    needs: [go-mod-tidy]\n    permissions:\n      contents: read\n      packages: read\n\n    steps:\n    - name: Check out code\n      uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n\n    - name: Set up Go\n      uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n      with:\n        go-version: '1.27.2'\n        cache: false\n\n    - name: Set up Node.js\n      uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0\n      with:\n        node-version: '24'\n        registry-url: 'https://npm.pkg.github.com'\n        cache: 'npm'\n        cache-dependency-path: ui/package-lock.json\n\n    - name: Build UI assets\n      env:\n        NODE_AUTH_TOKEN: ${{ github.token }}\n      run: |\n        cd ui && npm ci && npm run build\n\n    - name: YAML parsing \u2014 all configs loadable\n      run: go test -v -run TestExampleConfigsLoad -timeout 60s .\n\n    - name: Schema validation \u2014 types, fields, dependencies\n      run: go test -v -run TestExampleConfigsValidate -timeout 60s .\n\n    - name: Engine loading \u2014 BuildFromConfig + start/stop cycle\n      run: go test -v -run TestExampleConfigsBuildFromConfig -timeout 120s .\n\n    - name: Build example runner binary\n      run: |\n        cd example\n        go build -o workflow-example .\n\n    - name: Smoke test \u2014 run each config with timeout\n      run: |\n        cd example\n        passed=0\n        warned=0\n        total=0\n\n        for config in $(find . -name \"*.yaml\" \\\n          -not -path \"*/configs/*\" -not -path \"*/seed/*\" -not -path \"*/observability/*\" \\\n          -not -path \"*/spa/*\" -not -path \"*/components/*\" -not -path \"*/node_modules/*\" \\\n          -not -path \"*/e2e/*\" -not -path \"*/data/*\" -not -path \"*/.*\" \\\n          -not -name \"docker-compose.yaml\" -not -name \"docker-compose.yml\" | sort); do\n          total=$((total + 1))\n          echo -n \"  $config ... \"\n\n          # Run with timeout \u2014 exit 124 means timeout (expected for servers)\n          if timeout 10s ./workflow-example -config \"$config\" >/tmp/workflow-example.out 2>&1; then\n            echo \"OK (exited cleanly)\"\n            passed=$((passed + 1))\n          else\n            rc=$?\n            if [ \"$rc\" -eq 124 ]; then\n              echo \"OK (server timeout \u2014 expected)\"\n              passed=$((passed + 1))\n            else\n              echo \"WARN (exit $rc \u2014 may need runtime deps)\"\n              warned=$((warned + 1))\n            fi\n          fi\n        done\n\n        echo \"\"\n        echo \"Results: $total configs, $passed passed, $warned warnings\"\n\n  godo-banned:\n    name: Verify godo is not imported (issue #617)\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n        with:\n          go-version-file: go.mod\n          cache: false\n      - name: Semantically reject godo imports and module declarations\n        run: go test -mod=readonly ./module -run '^TestNoDigitalOceanGodoReferences$' -count=1\n\n  cloud-sdk-audit:\n    name: Cloud-SDK inventory + Kubernetes backend boundary + asymmetric graph audit\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0\n        with:\n          go-version-file: go.mod\n          cache: false\n      - name: Download module deps (needed for `go list -deps`)\n        env:\n          GOWORK: \"off\"\n        run: go mod download\n      - name: Test cloud/Kubernetes boundary audit mutations\n        run: ./scripts/test-audit-cloud-symbols.sh\n      - name: Audit cloud-SDK imports + Kubernetes backend boundary + Phase C asymmetric gate\n        # The script enforces (once .phase-c-complete is armed):\n        #   - module/ zero gcp/azure SDK real imports\n        #   - only core kind/k3s and temporary eks/aks factory registrations;\n        #     deleted provider-specific GKE implementation stays absent\n        #   - build graph: zero Azure/azure-sdk-for-go, zero google.golang.org/api,\n        #     zero cloud.google.com/go except compute/metadata (OAuth2 ADC helper\n        #     legitimately pulled by provider/gcp's service-account auth).\n        run: ./scripts/audit-cloud-symbols.sh --check\n      - name: Standalone asymmetric gate \u2014 `go list -deps` (mirrors audit script)\n        # Independent assertion of the same invariant for clarity in CI failure logs.\n        # Any cloud.google.com/go path other than compute/metadata fails the build.\n        env:\n          GOWORK: \"off\"\n        run: |\n          set +e\n          DEPS=$(go list -deps ./... 2>&1)\n          LIST_EXIT=$?\n          set -e\n          if [ \"$LIST_EXIT\" -ne 0 ]; then\n            echo \"FAIL: \\`go list -deps ./...\\` exited $LIST_EXIT (gate cannot enforce):\"\n            echo \"$DEPS\" | head -10 | sed 's/^/  /'\n            exit 1\n          fi\n          # `|| true` on grep is fine: exit 1 from grep means \"no matches\" = success.\n          UNEXPECTED=$(echo \"$DEPS\" \\\n            | grep -E '^(cloud\\.google\\.com/go|google\\.golang\\.org/api|github\\.com/Azure/azure-sdk-for-go)' \\\n            | grep -v '^cloud\\.google\\.com/go/compute/metadata$' \\\n            || true)\n          if [ -n \"$UNEXPECTED\" ]; then\n            echo \"FAIL: unexpected gcp/azure/api transitive deps in workflow-core build graph:\"\n            while read -r dep; do\n              printf '  %s\\n' \"$dep\"\n            done <<< \"$UNEXPECTED\"\n            echo\n            echo \"Only cloud.google.com/go/compute/metadata is allowlisted (OAuth2 ADC helper).\"\n            echo \"Other gcp/azure SDK packages belong in a plugin, not workflow core.\"\n            echo \"See docs/migrations/2026-05-15-plugin-modules-on-iac.md (Phase C).\"\n            exit 1\n          fi\n\n  aws-sdk-banned:\n    name: Verify removed AWS SDK packages are not imported (issue #653)\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3\n      - name: Grep gate \u2014 no *.go file (repo-wide) may import fully-removed AWS service packages\n        # Scans the whole repo. service/eks is allowed only in provider/ (ECS/EKS deploy pipeline).\n        # platform/providers/aws/ was deleted in Phase 3; provider/aws/ (deploy pipeline) is kept.\n        run: |\n          ! grep -rn --include=\"*.go\" \\\n              --exclude-dir=_worktrees \\\n              --exclude-dir=.worktrees \\\n              --exclude-dir=.claude \\\n              --exclude=\"aws_absent_test.go\" \\\n              --exclude=\"nosql_dynamodb.go\" \\\n              -e \"aws-sdk-go-v2/service/apigatewayv2\" \\\n              -e \"aws-sdk-go-v2/service/applicationautoscaling\" \\\n              -e \"aws-sdk-go-v2/service/route53\" \\\n              -e \"aws-sdk-go-v2/service/codebuild\" \\\n              -e \"aws-sdk-go-v2/service/ec2\" \\\n              -e \"aws-sdk-go-v2/service/dynamodb\" \\\n              -e \"aws-sdk-go-v2/service/elasticloadbalancingv2\" \\\n              -e \"aws-sdk-go-v2/service/rds\" \\\n              -e \"aws-sdk-go-v2/service/sqs\" \\\n              .\n      - name: Grep gate \u2014 service/eks must only appear in provider/ (deploy pipeline)\n        # platform/providers/aws/ was deleted in Phase 3; only provider/aws/ legitimately uses\n        # service/eks for ECS/EKS deploy pipeline. Any new import outside provider/ is a regression.\n        run: |\n          ! grep -rn --include=\"*.go\" \\\n              --exclude-dir=_worktrees \\\n              --exclude-dir=.worktrees \\\n              --exclude-dir=.claude \\\n              --exclude=\"aws_absent_test.go\" \\\n              --exclude-dir=\"provider\" \\\n              -e \"aws-sdk-go-v2/service/eks\" \\\n              .\n      - name: Grep gate \u2014 go.mod files must not list removed AWS SDK packages\n        # service/eks is intentionally omitted from this go.mod gate: provider/aws/ (deploy\n        # pipeline) has a legitimate caller. ec2/dynamodb/elb/rds/sqs are added here because\n        # platform/providers/aws/ (their only consumer) was deleted in Phase 3.\n        run: |\n          ! grep -qH \\\n              -e \"aws-sdk-go-v2/service/apigatewayv2\" \\\n              -e \"aws-sdk-go-v2/service/applicationautoscaling\" \\\n              -e \"aws-sdk-go-v2/service/route53\" \\\n              -e \"aws-sdk-go-v2/service/ec2\" \\\n              -e \"aws-sdk-go-v2/service/dynamodb\" \\\n              -e \"aws-sdk-go-v2/service/elasticloadbalancingv2\" \\\n              -e \"aws-sdk-go-v2/service/rds\" \\\n              -e \"aws-sdk-go-v2/service/sqs\" \\\n              go.mod example/go.mod\n"

func equivalentBudgetFixture(t *testing.T) (string, string) {
	t.Helper()
	const workflow = ".github/workflows/ci.yml"
	const active = "cd51e6225760668a51b92c71f4b40d362fe0a957b7aae04d7421a1c1282ee386"
	const prior = "447b640269534fa2108222bfa4e77c885318f92ba206b5aac070646ac910d598"
	const next = "a6e499f0785275675c67d7d8705484e2a1fed72e333c40e01301f161581ce6f5"
	policy := trustPolicy{Secrets: []allowEntry{}, Presence: []trustGroup{
		{workflow, active, "active", "present"}, {workflow, prior, "staged", "present"},
		{".github/workflows/benchmark.yml", strings.Repeat("8", 64), "active", "present"},
		{".github/workflows/benchmark.yml", strings.Repeat("9", 64), "staged", "present"},
	}}
	policy.Executables = []executableEntry{{"scripts/check-public-workflow-policy.sh", workflow, active, strings.Repeat("a", 64), "active", "Active executable."}}
	policy.Commands = []commandEntry{{workflow, "go", strings.Repeat("b", 64), active, "active", "Active command."}}
	policy.Actions = []actionEntry{{workflow, "actions/checkout@" + strings.Repeat("c", 40), strings.Repeat("d", 64), active, "active", "Active action."}}
	for i := 0; i < 4; i++ {
		policy.Executables = append(policy.Executables, executableEntry{fmt.Sprintf("scripts/staged-%d.sh", i), workflow, prior, strings.Repeat("e", 64), "staged", "Reviewed executable."})
	}
	for i := 0; i < 63; i++ {
		statement := fmt.Sprintf("%064x", i+1)
		if i == 0 {
			statement = "fa9dfdc59736a4c3cfa84b50ab1dbb642e3acdf8597adbc4f08c16c31139853d"
		}
		policy.Commands = append(policy.Commands, commandEntry{workflow, "go", statement, prior, "staged", "Reviewed command."})
	}
	for i := 0; i < 11; i++ {
		policy.Actions = append(policy.Actions, actionEntry{workflow, "actions/checkout@" + strings.Repeat("c", 40), fmt.Sprintf("%064x", i+100), prior, "staged", "Reviewed action."})
	}
	candidatePolicy := groupedClonePolicy(t, policy)
	candidatePolicy.Presence[1].ContextSHA256 = next
	for i := 1; i < len(candidatePolicy.Executables); i++ {
		candidatePolicy.Executables[i].ContextSHA256 = next
	}
	for i := 1; i < len(candidatePolicy.Commands); i++ {
		candidatePolicy.Commands[i].ContextSHA256 = next
	}
	candidatePolicy.Commands[1].StatementSHA256 = "ff839b699b98d3bc674dfc9c04ee51600dec68f4b3dff7292589be885cbe6813"
	for i := 1; i < len(candidatePolicy.Actions); i++ {
		candidatePolicy.Actions[i].ContextSHA256 = next
	}
	makeRoot := func(candidate bool) string {
		root := t.TempDir()
		write := func(name string, data []byte, mode os.FileMode) {
			t.Helper()
			path := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range append(append([]string(nil), authorityFixedPaths...), "scripts/fixtures/public-workflow-policy/fixture.yml") {
			write(name, []byte("authority fixture\n"), 0644)
		}
		ci := equivalentBudgetFixtureCI
		selected := policy
		if candidate {
			ci = strings.Replace(ci, "go test -v -race -coverprofile=coverage.out ./...", "go test -v -race -coverprofile=coverage.out ./... -timeout=15m", 1)
			selected = candidatePolicy
		}
		write(workflow, []byte(ci), 0644)
		write(".github/workflows/benchmark.yml", []byte("name: benchmark\non: push\njobs: {}\n"), 0644)
		writePolicy := func(kind string, value any) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			write(".github/public-workflow-"+kind+"-allowlist.json", data, 0644)
		}
		writePolicy("secret", selected.Secrets)
		writePolicy("presence", selected.Presence)
		writePolicy("executable", selected.Executables)
		writePolicy("command", selected.Commands)
		writePolicy("action", selected.Actions)
		inventory, err := authorityInventory(root)
		if err != nil {
			t.Fatal(err)
		}
		staged := append([]authorityFile(nil), inventory...)
		staged[0].SHA256 = strings.Repeat("f", 64)
		data, err := json.Marshal(authorityManifest{Version: 1, Bundles: []authorityBundle{{State: "active", Files: inventory}, {State: "staged", Files: staged}}})
		if err != nil {
			t.Fatal(err)
		}
		write(".github/public-workflow-authority.json", data, 0644)
		return root
	}
	return makeRoot(false), makeRoot(true)
}

func TestEquivalentStagedNativeBudgetBoundary(t *testing.T) {
	mutatePolicy := func(root string, edit func(*trustPolicy)) {
		t.Helper()
		p, err := loadTrustPolicy(root)
		if err != nil {
			t.Fatal(err)
		}
		edit(&p)
		for _, item := range []struct {
			name  string
			value any
		}{{"secret", p.Secrets}, {"presence", p.Presence}, {"executable", p.Executables}, {"command", p.Commands}, {"action", p.Actions}} {
			data, err := json.Marshal(item.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".github/public-workflow-"+item.name+"-allowlist.json"), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name   string
		edit   func(string, string)
		reject bool
	}{
		{"exact reviewed budget equivalent", func(string, string) {}, false},
		{"partial context relabel", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Executables[1].ContextSHA256 = strings.Repeat("1", 64) })
		}, true},
		{"wrong statement", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Commands[1].StatementSHA256 = strings.Repeat("1", 64) })
		}, true},
		{"added command", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Commands = append(p.Commands, p.Commands[1]) })
		}, true},
		{"deleted staged row", func(_, c string) { mutatePolicy(c, func(p *trustPolicy) { p.Actions = p.Actions[:len(p.Actions)-1] }) }, true},
		{"active pin change", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Executables[0].SHA256 = strings.Repeat("1", 64) })
		}, true},
		{"staged executable pin change", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Executables[1].SHA256 = strings.Repeat("1", 64) })
		}, true},
		{"action node change", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Actions[1].NodeSHA256 = strings.Repeat("1", 64) })
		}, true},
		{"rationale change", func(_, c string) { mutatePolicy(c, func(p *trustPolicy) { p.Commands[1].Rationale = "Drift" }) }, true},
		{"row reordering", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Actions[1], p.Actions[2] = p.Actions[2], p.Actions[1] })
		}, true},
		{"promotion", func(_, c string) { mutatePolicy(c, func(p *trustPolicy) { p.Presence[1].State = "active" }) }, true},
		{"second context change", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Presence[3].ContextSHA256 = strings.Repeat("1", 64) })
		}, true},
		{"nonempty secrets", func(_, c string) {
			mutatePolicy(c, func(p *trustPolicy) { p.Secrets = []allowEntry{{Secret: "TOKEN"}} })
		}, true},
		{"null secrets", func(_, c string) { mutatePolicy(c, func(p *trustPolicy) { p.Secrets = nil }) }, true},
		{"missing previous stage", func(b, _ string) {
			mutatePolicy(b, func(p *trustPolicy) { p.Presence = append(p.Presence[:1], p.Presence[2:]...) })
		}, true},
		{"malformed previous stage", func(b, _ string) { mutatePolicy(b, func(p *trustPolicy) { p.Commands[1].Command = "eval" }) }, true},
		{"base already realizes stage", func(b, _ string) {
			budgetEditFixtureFile(t, b, ".github/workflows/ci.yml", "go test -v -race -coverprofile=coverage.out ./...", "go test -v -race -timeout=15m -coverprofile=coverage.out ./...")
		}, true},
		{"extra target", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/workflows/ci.yml", "./... -timeout=15m", "./... ./config/... -timeout=15m")
		}, true},
		{"different timeout", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/workflows/ci.yml", "-timeout=15m", "-timeout=16m")
		}, true},
		{"malformed YAML", func(_, c string) { budgetEditFixtureFile(t, c, ".github/workflows/ci.yml", "name: CI", "name: [") }, true},
		{"CI comment drift", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/workflows/ci.yml", "name: CI", "# extra\nname: CI")
		}, true},
		{"other workflow drift", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/workflows/benchmark.yml", "name: benchmark", "name: changed")
		}, true},
		{"workflow mode", func(_, c string) {
			if err := os.Chmod(filepath.Join(c, ".github/workflows/ci.yml"), 0755); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"authority source drift", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/workflows/policytool/main.go", "authority fixture", "unreviewed source")
		}, true},
		{"authority mode", func(_, c string) {
			if err := os.Chmod(filepath.Join(c, ".github/workflows/policytool/main.go"), 0755); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"H staged pin change", func(_, c string) {
			budgetEditFixtureFile(t, c, ".github/public-workflow-authority.json", strings.Repeat("f", 64), strings.Repeat("e", 64))
		}, true},
		{"H mode", func(_, c string) {
			if err := os.Chmod(filepath.Join(c, ".github/public-workflow-authority.json"), 0600); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, candidate := equivalentBudgetFixture(t)
			tc.edit(base, candidate)
			findings, err := validateRepositoryAuthority(base, candidate, false)
			if (err != nil || len(findings) != 0) != tc.reject {
				t.Fatalf("findings=%v err=%v reject=%v", findings, err, tc.reject)
			}
		})
	}
}

func budgetEditFixtureFile(t *testing.T, root, relative, before, after string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), before) {
		t.Fatalf("fixture target missing: %s", relative)
	}
	if err := os.WriteFile(name, []byte(strings.ReplaceAll(string(data), before, after)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestEquivalentBudgetRetainsIndependentAuthorityFindings(t *testing.T) {
	base, candidate := equivalentBudgetFixture(t)
	for _, root := range []string{base, candidate} {
		budgetEditFixtureFile(t, root, ".github/public-workflow-authority.json", `"version":1`, `"version":2`)
	}
	bp, err := loadTrustPolicy(base)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := loadTrustPolicy(candidate)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := authorityInventory(base)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := authorityInventory(candidate)
	if err != nil {
		t.Fatal(err)
	}
	bc, err := repositoryWorkflowContexts(base)
	if err != nil {
		t.Fatal(err)
	}
	cc, err := repositoryWorkflowContexts(candidate)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := equivalentStagedNativeBudget(base, candidate, bp, cp, bi, ci, bc, cc)
	if err != nil || !accepted {
		t.Fatalf("control did not exercise accepted predicate: accepted=%v err=%v", accepted, err)
	}
	findings, effective, err := validateRepositoryAuthorityPolicy(base, candidate, false)
	if err != nil || len(findings) == 0 || effective != nil {
		t.Fatalf("independent invalid authority was lost: findings=%v effective=%v err=%v", findings, effective != nil, err)
	}
}

// Use the actual frozen repository workflow and trust rows for the real CLI
// crossing. The synthetic boundary fixture above cannot prove scanner wiring.
func equivalentBudgetRealRepositoryFixture(t *testing.T) (string, string) {
	t.Helper()
	const prior = "447b640269534fa2108222bfa4e77c885318f92ba206b5aac070646ac910d598"
	const next = "a6e499f0785275675c67d7d8705484e2a1fed72e333c40e01301f161581ce6f5"
	roots := []string{groupedRepositoryFixture(t), groupedRepositoryFixture(t)}
	for index, root := range roots {
		policy, err := loadTrustPolicy(root)
		if err != nil {
			t.Fatal(err)
		}
		// The tests can run on either side of the proposed root migration.
		for i := range policy.Presence {
			if policy.Presence[i].ContextSHA256 == next {
				policy.Presence[i].ContextSHA256 = prior
			}
		}
		for i := range policy.Executables {
			if policy.Executables[i].ContextSHA256 == next {
				policy.Executables[i].ContextSHA256 = prior
			}
		}
		for i := range policy.Commands {
			if policy.Commands[i].ContextSHA256 == next {
				policy.Commands[i].ContextSHA256 = prior
			}
			if policy.Commands[i].StatementSHA256 == "ff839b699b98d3bc674dfc9c04ee51600dec68f4b3dff7292589be885cbe6813" {
				policy.Commands[i].StatementSHA256 = "fa9dfdc59736a4c3cfa84b50ab1dbb642e3acdf8597adbc4f08c16c31139853d"
			}
		}
		for i := range policy.Actions {
			if policy.Actions[i].ContextSHA256 == next {
				policy.Actions[i].ContextSHA256 = prior
			}
		}
		ci := equivalentBudgetFixtureCI
		if index == 1 {
			ci = strings.Replace(ci, "go test -v -race -coverprofile=coverage.out ./...", "go test -v -race -coverprofile=coverage.out ./... -timeout=15m", 1)
			for i := range policy.Presence {
				if policy.Presence[i].ContextSHA256 == prior {
					policy.Presence[i].ContextSHA256 = next
				}
			}
			for i := range policy.Executables {
				if policy.Executables[i].ContextSHA256 == prior {
					policy.Executables[i].ContextSHA256 = next
				}
			}
			for i := range policy.Commands {
				if policy.Commands[i].ContextSHA256 == prior {
					policy.Commands[i].ContextSHA256 = next
					if policy.Commands[i].StatementSHA256 == "fa9dfdc59736a4c3cfa84b50ab1dbb642e3acdf8597adbc4f08c16c31139853d" {
						policy.Commands[i].StatementSHA256 = "ff839b699b98d3bc674dfc9c04ee51600dec68f4b3dff7292589be885cbe6813"
					}
				}
			}
			for i := range policy.Actions {
				if policy.Actions[i].ContextSHA256 == prior {
					policy.Actions[i].ContextSHA256 = next
				}
			}
		}
		for _, item := range []struct {
			name  string
			value any
		}{{"secret", policy.Secrets}, {"presence", policy.Presence}, {"executable", policy.Executables}, {"command", policy.Commands}, {"action", policy.Actions}} {
			data, err := json.Marshal(item.value)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(root, ".github/public-workflow-"+item.name+"-allowlist.json")
			if err := os.WriteFile(name, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(name, 0644); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(root, ".github/workflows/ci.yml")
		if err := os.WriteFile(path, []byte(ci), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		contexts, err := repositoryWorkflowContexts(root)
		if err != nil {
			t.Fatal(err)
		}
		present, err := trustPresence(root, policy)
		if err != nil {
			t.Fatal(err)
		}
		selected, findings := selectTrustGroups(policy.Presence, contexts, present)
		if len(findings) != 0 {
			t.Fatalf("real fixture trust groups: %v", findings)
		}
		// Workflow scanning hashes selected executables even outside H. Copy the
		// full selected dependency closure; do not special-case missing scripts.
		copied := make(map[string]bool)
		for _, entry := range policy.Executables {
			if !selected[entry.WorkflowPath+"\x00"+entry.ContextSHA256] || copied[entry.Path] {
				continue
			}
			relative := filepath.Clean(filepath.FromSlash(entry.Path))
			if filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				t.Fatalf("invalid selected fixture path: %s", entry.Path)
			}
			source := filepath.Join("..", "..", "..", relative)
			info, err := os.Lstat(source)
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("selected executable %s: info=%v err=%v", entry.Path, info, err)
			}
			actual, err := hashAuthorityFile(source)
			if err != nil || actual != entry.SHA256 {
				t.Fatalf("selected executable %s: sha=%s want=%s err=%v", entry.Path, actual, entry.SHA256, err)
			}
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			copied[entry.Path] = true
		}
	}
	return roots[0], roots[1]
}

func TestEquivalentStagedBudgetCLICrossing(t *testing.T) {
	const marker = "WF_POLICY_EQUIVALENT_BUDGET_CLI_CHILD"
	if os.Getenv(marker) == "1" {
		os.Args = []string{"policytool", "--repo", os.Getenv("WF_POLICY_EQUIVALENT_BASE"), "--scan-root", os.Getenv("WF_POLICY_EQUIVALENT_CANDIDATE")}
		main()
		return
	}
	for _, tc := range []struct {
		name               string
		unrelated          bool
		authorityDrift     bool
		tamperedExecutable bool
		pass               bool
	}{
		{"actual full scanner accepts equivalent policy", false, false, false, true},
		{"accepted predicate still runs unrelated policy checks", true, false, false, false},
		{"invalid authority cannot select candidate policy", false, true, false, false},
		{"accepted predicate still hashes selected executables", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, candidate := equivalentBudgetRealRepositoryFixture(t)
			if tc.unrelated {
				for _, root := range []string{base, candidate} {
					name := filepath.Join(root, ".github/workflows/unreviewed.yml")
					if err := os.WriteFile(name, []byte("name: unreviewed\non: push\njobs: {}\n"), 0644); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(name, 0644); err != nil {
						t.Fatal(err)
					}
				}
				findings, effective, err := validateRepositoryAuthorityPolicy(base, candidate, false)
				if err != nil || len(findings) != 0 || effective == nil {
					t.Fatalf("control missed accepted predicate: findings=%v effective=%v err=%v", findings, effective != nil, err)
				}
			}
			if tc.tamperedExecutable {
				audit := filepath.Join(candidate, "scripts/audit-cloud-symbols.sh")
				info, err := os.Stat(audit)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(audit)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(audit, append(data, []byte("\n# test control: executable byte tamper\n")...), info.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(audit, info.Mode().Perm()); err != nil {
					t.Fatal(err)
				}
				findings, effective, err := validateRepositoryAuthorityPolicy(base, candidate, false)
				if err != nil || len(findings) != 0 || effective == nil {
					t.Fatalf("control missed accepted predicate: findings=%v effective=%v err=%v", findings, effective != nil, err)
				}
			}
			if tc.authorityDrift {
				budgetEditFixtureFile(t, candidate, ".github/workflows/policytool/main.go", "package main", "// unexpected source drift\npackage main")
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestEquivalentStagedBudgetCLICrossing$")
			command.Env = append(os.Environ(), marker+"=1", "WF_POLICY_EQUIVALENT_BASE="+base, "WF_POLICY_EQUIVALENT_CANDIDATE="+candidate)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("CLI crossing deadline: %v", ctx.Err())
			}
			if (err == nil) != tc.pass {
				t.Fatalf("pass=%v err=%v output=%s", tc.pass, err, output)
			}
			if tc.pass && !strings.Contains(string(output), "public workflow policy passed") {
				t.Fatalf("real scanner success missing: %s", output)
			}
			if tc.unrelated && !strings.Contains(string(output), "no trust group matches workflow") {
				t.Fatalf("unrelated scanner rejection missing: %s", output)
			}
			if tc.tamperedExecutable && !strings.Contains(string(output), "executable hash mismatch for scripts/audit-cloud-symbols.sh") {
				t.Fatalf("executable scanner rejection missing: %s", output)
			}
		})
	}
}
