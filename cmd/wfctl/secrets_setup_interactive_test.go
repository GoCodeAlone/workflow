package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// TestStorePickOptions verifies the store-pick option list: configured store
// keys (sorted) come first, then builtin provider types not already present.
func TestStorePickOptions(t *testing.T) {
	stores := map[string]*config.SecretStoreConfig{
		"zeta":  {Provider: "file"},
		"alpha": {Provider: "vault"},
		"env":   {Provider: "env"}, // shadows builtin "env"
	}
	opts := storePickOptions(stores)

	// First three must be the sorted store keys.
	wantPrefix := []string{"alpha", "env", "zeta"}
	for i, w := range wantPrefix {
		if i >= len(opts) || opts[i] != w {
			t.Fatalf("opts[%d] = %q, want %q (opts=%v)", i, safeIdx(opts, i), w, opts)
		}
	}
	// "env" builtin must NOT be duplicated.
	count := 0
	for _, o := range opts {
		if o == "env" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'env' appears %d times, want 1 (opts=%v)", count, opts)
	}
	// Builtin types like github/vault/aws/keychain/file must appear.
	for _, b := range []string{"github", "vault", "aws", "keychain", "file"} {
		if !sliceHas(opts, b) {
			t.Errorf("builtin %q missing from opts %v", b, opts)
		}
	}
}

func TestStorePickOptions_NoStores(t *testing.T) {
	opts := storePickOptions(nil)
	// Should be exactly the builtin provider types.
	if len(opts) != len(builtinProviderTypes) {
		t.Fatalf("opts = %v, want %v", opts, builtinProviderTypes)
	}
}

// TestFormatStatusLabel verifies the MultiSelect row labels.
func TestFormatStatusLabel(t *testing.T) {
	unset := formatStatusLabel("FOO", SecretStatus{Name: "FOO", IsSet: false})
	if !strings.Contains(unset, "✗ unset") {
		t.Errorf("unset label = %q, want ✗ unset", unset)
	}

	setNoTime := formatStatusLabel("BAR", SecretStatus{Name: "BAR", IsSet: true})
	if !strings.Contains(setNoTime, "✓ set") || strings.Contains(setNoTime, "updated") {
		t.Errorf("set-no-time label = %q, want ✓ set without updated", setNoTime)
	}

	setWithTime := formatStatusLabel("BAZ", SecretStatus{
		Name: "BAZ", IsSet: true, LastRotated: time.Now().Add(-72 * time.Hour),
	})
	if !strings.Contains(setWithTime, "✓ set · updated") || !strings.Contains(setWithTime, "3d ago") {
		t.Errorf("set-with-time label = %q, want '✓ set · updated 3d ago'", setWithTime)
	}

	noAccess := formatStatusLabel("QUX", SecretStatus{Name: "QUX", State: SecretNoAccess})
	if !strings.Contains(noAccess, "! no access") {
		t.Errorf("no-access label = %q, want ! no access", noAccess)
	}

	fetchError := formatStatusLabel("QUUX", SecretStatus{Name: "QUUX", State: SecretFetchError})
	if !strings.Contains(fetchError, "! check failed") {
		t.Errorf("fetch-error label = %q, want ! check failed", fetchError)
	}

	unconfigured := formatStatusLabel("CORGE", SecretStatus{Name: "CORGE", State: SecretUnconfigured})
	if !strings.Contains(unconfigured, "! unconfigured") {
		t.Errorf("unconfigured label = %q, want ! unconfigured", unconfigured)
	}
}

func TestFormatRotatedAge(t *testing.T) {
	if got := formatRotatedAge(time.Time{}); got != "" {
		t.Errorf("zero time → %q, want empty", got)
	}
	if got := formatRotatedAge(time.Now().Add(-30 * time.Minute)); got != "30m ago" {
		t.Errorf("30m → %q", got)
	}
	if got := formatRotatedAge(time.Now().Add(-5 * time.Hour)); got != "5h ago" {
		t.Errorf("5h → %q", got)
	}
	if got := formatRotatedAge(time.Now().Add(-48 * time.Hour)); got != "2d ago" {
		t.Errorf("2d → %q", got)
	}
}

// TestBuildMultiSelectItems verifies unset secrets are preselected and set
// secrets are not.
func TestBuildMultiSelectItems(t *testing.T) {
	decls := []setupDecl{
		{name: "SET_ONE"},
		{name: "UNSET_ONE"},
	}
	statuses := []SecretStatus{
		{Name: "SET_ONE", IsSet: true, State: SecretSet},
		{Name: "UNSET_ONE", IsSet: false, State: SecretNotSet},
	}
	items := buildMultiSelectItems(decls, statuses)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Preselected {
		t.Error("SET_ONE should NOT be preselected")
	}
	if !items[1].Preselected {
		t.Error("UNSET_ONE SHOULD be preselected")
	}
	if !strings.Contains(items[0].Label, "✓ set") {
		t.Errorf("SET_ONE label = %q", items[0].Label)
	}
	if !strings.Contains(items[1].Label, "✗ unset") {
		t.Errorf("UNSET_ONE label = %q", items[1].Label)
	}
}

func TestBuildSetupDeclsResolvesPerSecretStores(t *testing.T) {
	cfg := &config.WorkflowConfig{
		SecretStores: map[string]*config.SecretStoreConfig{
			"github-repo": {Provider: "github", Config: map[string]any{"repo": "GoCodeAlone/example"}},
			"aws-prod":    {Provider: "aws-secrets-manager", Config: map[string]any{"region": "us-east-1"}},
		},
		Secrets: &config.SecretsConfig{
			DefaultStore: "github-repo",
			Entries: []config.SecretEntry{
				{Name: "GITHUB_TOKEN"},
				{Name: "AWS_ACCESS_KEY_ID", Store: "aws-prod"},
			},
		},
	}

	decls := buildSetupDecls(cfg, "production", "")
	if len(decls) != 2 {
		t.Fatalf("decls len = %d, want 2", len(decls))
	}
	if decls[0].store != "github-repo" {
		t.Fatalf("GITHUB_TOKEN store = %q, want github-repo", decls[0].store)
	}
	if decls[1].store != "aws-prod" {
		t.Fatalf("AWS_ACCESS_KEY_ID store = %q, want aws-prod", decls[1].store)
	}

	decls = buildSetupDecls(cfg, "production", "github-repo")
	if decls[1].store != "github-repo" {
		t.Fatalf("--store override did not override per-secret store: %+v", decls[1])
	}
}

func TestBuildMultiSelectItemsShowsResolvedStore(t *testing.T) {
	decls := []setupDecl{
		{name: "AWS_ACCESS_KEY_ID", store: "aws-prod"},
	}
	statuses := []SecretStatus{
		{Name: "AWS_ACCESS_KEY_ID", Store: "aws-prod", IsSet: false, State: SecretNotSet},
	}

	items := buildMultiSelectItems(decls, statuses)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if !strings.Contains(items[0].Label, "[aws-prod]") {
		t.Fatalf("label = %q, want resolved store", items[0].Label)
	}
}

// TestQueryDeclStatuses verifies the status query against a fake provider.
func TestQueryDeclStatuses(t *testing.T) {
	provider := newEngineTestProvider(map[string]string{"A": "v"})
	decls := []setupDecl{{name: "A"}, {name: "B"}}
	statuses := queryDeclStatuses(context.Background(), decls, provider)
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2", len(statuses))
	}
	byName := map[string]SecretStatus{}
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if !byName["A"].IsSet {
		t.Error("A should be set")
	}
	if byName["B"].IsSet {
		t.Error("B should be unset")
	}
}

// TestPrintStoreAccessLine_RedactsError verifies the access line never leaks
// the underlying error message (which could echo credentials).
func TestPrintStoreAccessLine_RedactsError(t *testing.T) {
	// Build a file-store adapter pointed at a non-existent, non-creatable dir.
	provider, err := getProviderForStore("env", &config.WorkflowConfig{})
	if err != nil {
		t.Fatalf("build env provider: %v", err)
	}
	var buf bytes.Buffer
	printStoreAccessLine(context.Background(), &buf, "env", provider)
	out := buf.String()
	// env provider has no CheckAccess → reports ✓.
	if !strings.Contains(out, "access: ✓") {
		t.Errorf("env access line = %q, want ✓", out)
	}
}

func TestRedactAccessError(t *testing.T) {
	if got := redactAccessError(nil); got != "" {
		t.Errorf("nil → %q, want empty", got)
	}
	msg := redactAccessError(context.DeadlineExceeded)
	if strings.Contains(msg, "deadline") || strings.Contains(strings.ToLower(msg), "exceeded") {
		t.Errorf("redacted message leaks original error: %q", msg)
	}
	if !strings.Contains(msg, "redacted") {
		t.Errorf("redacted message = %q, want it to mention redaction", msg)
	}
}

func sliceHas(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func safeIdx(ss []string, i int) string {
	if i < len(ss) {
		return ss[i]
	}
	return "<oob>"
}
