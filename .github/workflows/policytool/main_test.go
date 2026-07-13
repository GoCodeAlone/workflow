package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

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
