package main

// Plan §Task 20 parity test for the wfctl infra admin CLI surface.
//
// What's covered here (in-process, no exec):
//   - The CLI's JSON output round-trips through the matching adminpb
//     proto-message via encoding/json (the CLI uses encoding/json with
//     proto-generated snake_case tags; see emitJSON godoc on the
//     "we intentionally don't use protojson" rationale).
//   - The subcommand dispatcher rejects unknown subcommands without
//     panicking and emits a stable usage on `--help`.
//   - --field flag parses repeated KEY=VALUE pairs with last-write-wins
//     semantics.
//
// What's NOT covered here (intentionally — exec-driven full smoke
// lives in workflow-scenarios/scenarios/92-infra-admin-demo/test/run.sh
// per plan §CLI end-to-end smoke):
//   - Live state-store + provider resolution (requires a real workflow
//     config + filesystem state). The scenario harness exercises that
//     path against the docker-compose stack.
//   - audit-tail HTTP round-trip (scenario harness via curl).
//
// The CLI's JSON output uses snake_case field names (the proto-
// generated `json:"snake_case,omitempty"` tags on adminpb structs).
// json.Unmarshal of the bytes back into the same struct must produce
// an equivalent value — that's the parity assertion.

import (
	"bytes"
	"encoding/json"
	"flag"
	"strings"
	"testing"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
)

// captureEmit invokes emitJSON's path by marshaling the same way the
// CLI does to a buffer instead of stdout. We mirror the body of
// emitJSON here to keep the test hermetic (no os.Stdout redirection
// race in parallel-test mode).
func captureEmit(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestInfraAdminCLI_ListResourcesOutput_RoundTrip(t *testing.T) {
	original := &adminpb.AdminListResourcesOutput{
		Resources: []*adminpb.AdminResourceSummary{
			{
				Name:           "demo-vpc",
				Type:           "infra.vpc",
				ProviderModule: "do-provider",
				ProviderType:   "digitalocean",
				ProviderId:     "vpc-abc123",
				Status:         "applied",
				UpdatedAtUnix:  1717000000,
				Dependencies:   []string{"infra.firewall"},
				AppContext:     "production",
			},
		},
	}
	payload := captureEmit(t, original)

	var decoded adminpb.AdminListResourcesOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal CLI output: %v\npayload=%s", err, payload)
	}
	if len(decoded.Resources) != 1 {
		t.Fatalf("decoded.Resources len=%d, want 1", len(decoded.Resources))
	}
	got := decoded.Resources[0]
	want := original.Resources[0]
	if got.Name != want.Name || got.Type != want.Type ||
		got.ProviderModule != want.ProviderModule ||
		got.ProviderType != want.ProviderType ||
		got.ProviderId != want.ProviderId ||
		got.Status != want.Status ||
		got.UpdatedAtUnix != want.UpdatedAtUnix ||
		got.AppContext != want.AppContext {
		t.Errorf("AdminResourceSummary round-trip lost fields.\n got=%+v\nwant=%+v", got, want)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0] != "infra.firewall" {
		t.Errorf("dependencies lost: got=%v", got.Dependencies)
	}
}

func TestInfraAdminCLI_GetResourceOutput_RoundTrip(t *testing.T) {
	original := &adminpb.AdminGetResourceOutput{
		Resource: &adminpb.AdminResourceDetail{
			Summary:                  &adminpb.AdminResourceSummary{Name: "demo-db", Type: "infra.database"},
			AppliedConfigJson:        []byte(`{"engine":"postgres"}`),
			OutputsJson:              []byte(`{"endpoint":"db.example.com"}`),
			ConfigHash:               "sha256:abc",
			LastDriftCheckUnix:       1717000123,
			SensitiveOutputsRedacted: []string{"password"},
		},
	}
	payload := captureEmit(t, original)

	var decoded adminpb.AdminGetResourceOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal CLI output: %v\npayload=%s", err, payload)
	}
	if decoded.Resource == nil {
		t.Fatal("decoded.Resource is nil")
	}
	if decoded.Resource.ConfigHash != original.Resource.ConfigHash {
		t.Errorf("ConfigHash: got %q, want %q", decoded.Resource.ConfigHash, original.Resource.ConfigHash)
	}
	if string(decoded.Resource.AppliedConfigJson) != string(original.Resource.AppliedConfigJson) {
		t.Errorf("AppliedConfigJson round-trip mismatch: got=%s want=%s",
			decoded.Resource.AppliedConfigJson, original.Resource.AppliedConfigJson)
	}
	if len(decoded.Resource.SensitiveOutputsRedacted) != 1 ||
		decoded.Resource.SensitiveOutputsRedacted[0] != "password" {
		t.Errorf("SensitiveOutputsRedacted round-trip mismatch: got=%v", decoded.Resource.SensitiveOutputsRedacted)
	}
}

func TestInfraAdminCLI_ListTypesOutput_RoundTrip(t *testing.T) {
	original := &adminpb.AdminListResourceTypesOutput{
		Types: []*adminpb.AdminResourceTypeMetadata{
			{
				Type:             "infra.vpc",
				ConfigMessageFqn: "workflow.plugin.infra.v1.VPCConfig",
				Fields: []*adminpb.AdminFieldSpec{
					{Name: "provider", Kind: "enum_dynamic", EnumSource: "providers", Required: true},
					{Name: "cidr", Kind: "string", Required: true},
				},
			},
		},
	}
	payload := captureEmit(t, original)

	var decoded adminpb.AdminListResourceTypesOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal CLI output: %v\npayload=%s", err, payload)
	}
	if len(decoded.Types) != 1 || decoded.Types[0].Type != "infra.vpc" {
		t.Errorf("types round-trip mismatch: got=%+v", decoded.Types)
	}
	if len(decoded.Types[0].Fields) != 2 {
		t.Errorf("fields lost: got=%d, want 2", len(decoded.Types[0].Fields))
	}
	if decoded.Types[0].Fields[0].EnumSource != "providers" {
		t.Errorf("EnumSource lost: got=%q", decoded.Types[0].Fields[0].EnumSource)
	}
}

func TestInfraAdminCLI_ListProvidersOutput_RoundTrip(t *testing.T) {
	original := &adminpb.AdminListProvidersOutput{
		Providers: []*adminpb.AdminProviderSummary{
			{
				ModuleName:       "do-provider",
				ProviderType:     "digitalocean",
				Capabilities:     []string{"plan", "apply"},
				SupportedRegions: []string{"nyc1", "nyc3"},
				SupportedTypes:   []string{"infra.vpc", "infra.database"},
				SupportedEngines: []string{"postgres", "mysql"},
				RegionsSource:    "local-catalog",
			},
		},
	}
	payload := captureEmit(t, original)

	var decoded adminpb.AdminListProvidersOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal CLI output: %v\npayload=%s", err, payload)
	}
	if len(decoded.Providers) != 1 {
		t.Fatalf("decoded.Providers len=%d, want 1", len(decoded.Providers))
	}
	p := decoded.Providers[0]
	if p.ModuleName != "do-provider" || p.ProviderType != "digitalocean" ||
		p.RegionsSource != "local-catalog" {
		t.Errorf("provider summary fields lost: got=%+v", p)
	}
	if len(p.SupportedRegions) != 2 || p.SupportedRegions[0] != "nyc1" {
		t.Errorf("SupportedRegions lost: got=%v", p.SupportedRegions)
	}
	if len(p.SupportedEngines) != 2 {
		t.Errorf("SupportedEngines lost: got=%v", p.SupportedEngines)
	}
}

func TestInfraAdminCLI_GenerateConfigOutput_RoundTrip(t *testing.T) {
	original := &adminpb.AdminGenerateConfigOutput{
		YamlSnippet: "name: demo-vpc\ntype: infra.vpc\nconfig:\n  cidr: 10.0.0.0/16\n",
	}
	payload := captureEmit(t, original)

	var decoded adminpb.AdminGenerateConfigOutput
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal CLI output: %v\npayload=%s", err, payload)
	}
	if decoded.YamlSnippet != original.YamlSnippet {
		t.Errorf("YamlSnippet round-trip mismatch.\n got=%q\nwant=%q",
			decoded.YamlSnippet, original.YamlSnippet)
	}
}

func TestInfraAdminCLI_UnknownSubcommand(t *testing.T) {
	// Cases that should not panic and should return without crashing.
	// Output goes to os.Stderr / flag.CommandLine — we only assert
	// the dispatcher behavior.
	cases := [][]string{
		{},                   // no args → usage
		{"--help"},           // help → usage
		{"-h"},               // -h → usage
		{"help"},             // help → usage
		{"completely-bogus"}, // unknown → usage
	}
	for _, args := range cases {
		// Discard errors: --help and unknown both return nil (usage
		// path) per the dispatcher implementation, and the no-arg
		// case returns nil too. Just confirm no panic.
		_ = runInfraAdmin(args)
	}
}

func TestInfraAdminCLI_FieldFlag_RepeatableAndLastWriteWins(t *testing.T) {
	f := newFieldFlag()
	if err := f.Set("provider=do-provider"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Set("region=nyc1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := f.Set("region=nyc3"); err != nil { // overwrite
		t.Fatalf("Set: %v", err)
	}
	if got := f.values["provider"]; got != "do-provider" {
		t.Errorf("provider: got %q, want do-provider", got)
	}
	if got := f.values["region"]; got != "nyc3" {
		t.Errorf("region last-write-wins: got %q, want nyc3", got)
	}
	if !strings.Contains(f.String(), "provider=do-provider") {
		t.Errorf("String() missing provider entry: %q", f.String())
	}
	// Bad input: missing `=` should error rather than panic.
	if err := f.Set("no-equal"); err == nil {
		t.Error("Set(\"no-equal\") returned nil, want error")
	}
	// Empty key (leading `=`) should also error — the index check is
	// `<= 0`, so `=val` (idx=0) is rejected.
	if err := f.Set("=val"); err == nil {
		t.Error("Set(\"=val\") returned nil, want error")
	}
}

// TestInfraAdminCLI_HelpListsAllSubcommands ensures the dispatcher's
// usage block stays in sync with the 6 documented subcommands. The
// usage text is the source of truth users see when typing `wfctl infra
// admin --help`; missing entries here surface as quiet UX failures.
func TestInfraAdminCLI_HelpListsAllSubcommands(t *testing.T) {
	// Capture flag.CommandLine output into a buffer so we can grep.
	var buf bytes.Buffer
	origOut := flag.CommandLine.Output()
	flag.CommandLine.SetOutput(&buf)
	defer flag.CommandLine.SetOutput(origOut)

	if err := infraAdminUsage(); err != nil {
		t.Fatalf("infraAdminUsage: %v", err)
	}
	out := buf.String()

	expected := []string{
		"list-resources",
		"get-resource",
		"list-types",
		"list-providers",
		"generate-config",
		"audit-tail",
	}
	for _, name := range expected {
		if !strings.Contains(out, name) {
			t.Errorf("usage missing subcommand %q in output:\n%s", name, out)
		}
	}
}
