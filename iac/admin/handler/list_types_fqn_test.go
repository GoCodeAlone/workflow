package handler_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
)

// TestListResourceTypes_ConfigMessageFQNMatchesVendoredProto is the
// F2 regression guard (spec-reviewer T6 finding on commit 1ea231fdd).
// It walks every AdminResourceTypeMetadata returned by the handler,
// parses out the short message name from each `config_message_fqn`,
// and asserts (a) the package prefix matches the vendored proto's
// `package workflow.plugins.infra.v1;` declaration AND (b) the
// short name exists as a `message <Name>` line in the vendored
// proto. Without this test, the earlier T6 bug (wrong "plugin"
// singular prefix + missing acronym preservation producing
// "VpcConfig"/"DnsConfig"/etc.) would have shipped silently —
// the original AllFieldsMatchProto test only checked non-emptiness.
func TestListResourceTypes_ConfigMessageFQNMatchesVendoredProto(t *testing.T) {
	const vendoredPath = "../catalog/../testdata/infra.proto"
	// Walk up to iac/admin/testdata/infra.proto regardless of test
	// cwd. The relative path from iac/admin/handler/ is
	// ../testdata/infra.proto.
	protoBytes, err := os.ReadFile("../testdata/infra.proto")
	if err != nil {
		t.Fatalf("read vendored proto (%s): %v", vendoredPath, err)
	}
	src := string(protoBytes)

	// Confirm the vendored proto declares the package we expect — if
	// upstream ever renames, this catches it before the FQN parity
	// check yields a confusing failure.
	pkgRe := regexp.MustCompile(`(?m)^package\s+([A-Za-z0-9_.]+)\s*;`)
	pkgMatch := pkgRe.FindStringSubmatch(src)
	if len(pkgMatch) < 2 {
		t.Fatal("vendored proto missing `package` declaration")
	}
	gotPkg := pkgMatch[1]
	if gotPkg != catalog.ConfigProtoPackage {
		t.Fatalf("vendored proto package = %q but catalog.ConfigProtoPackage = %q — update one to match", gotPkg, catalog.ConfigProtoPackage)
	}

	// Collect the message names declared in the vendored proto.
	msgRe := regexp.MustCompile(`(?m)^\s*message\s+([A-Za-z0-9_]+Config)\s*\{`)
	matches := msgRe.FindAllStringSubmatch(src, -1)
	vendoredMessages := map[string]bool{}
	for _, m := range matches {
		vendoredMessages[m[1]] = true
	}
	if len(vendoredMessages) == 0 {
		t.Fatal("regex found 0 *Config messages in vendored proto")
	}

	// Now exercise the handler and validate every emitted FQN.
	in := &adminpb.AdminListResourceTypesInput{Evidence: authzOK()}
	out, err := handler.ListResourceTypes(context.Background(), catalog.New(), nil, in)
	if err != nil {
		t.Fatalf("ListResourceTypes: %v", err)
	}
	if len(out.Types) == 0 {
		t.Fatal("no types in output; T7b should have populated 13")
	}
	wantPrefix := catalog.ConfigProtoPackage + "."
	for _, ty := range out.Types {
		if ty.ConfigMessageFqn == "" {
			t.Errorf("type %q: config_message_fqn empty", ty.Type)
			continue
		}
		if !strings.HasPrefix(ty.ConfigMessageFqn, wantPrefix) {
			t.Errorf("type %q: config_message_fqn = %q, want prefix %q", ty.Type, ty.ConfigMessageFqn, wantPrefix)
			continue
		}
		shortName := strings.TrimPrefix(ty.ConfigMessageFqn, wantPrefix)
		if !vendoredMessages[shortName] {
			t.Errorf("type %q: emitted FQN %q references a *Config message NOT present in vendored proto (got short=%q; available=%v)",
				ty.Type, ty.ConfigMessageFqn, shortName, sortedKeys(vendoredMessages))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Don't pull in sort here; just stringify for the error message.
	return out
}
