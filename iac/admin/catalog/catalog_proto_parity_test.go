package catalog_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
)

// Allowlist of vendored *Config message names that are NOT directly
// instantiated as form-rendered types — InfraResourceConfig is the
// abstract base per the design's §FieldSpec Catalog note.
var protoConfigAllowlist = map[string]bool{
	"InfraResourceConfig": true,
}

// configMessagePattern matches `message <NAME>Config {` at line start,
// optionally preceded by indentation. The vendored proto is small and
// structurally regular (no nested message types share the *Config
// suffix in v1.0.0), so a regex suffices over a full protoparse.
//
// If a future contract introduces nested *Config messages or a more
// complex shape, swap to google.golang.org/protobuf/types/descriptorpb
// + a protoparse driver.
var configMessagePattern = regexp.MustCompile(`(?m)^\s*message\s+([A-Za-z0-9_]+Config)\s*\{`)

// typeToConfigMessage maps an "infra.<snake>" type name to its proto
// CamelCase Config message. The mapping mirrors the per-driver naming
// convention shared between catalog/fields.go and the vendored proto.
//
// Special-case acronym preservations (VPC, K8S, DNS, IAM, API) avoid
// degenerate `Vpc` → `VPC` toggling. The set is closed at 13 entries
// (the design's typed-Config inventory).
func typeToConfigMessage(typeName string) string {
	tail := strings.TrimPrefix(typeName, "infra.")
	switch tail {
	case "vpc":
		return "VPCConfig"
	case "k8s_cluster":
		return "K8SClusterConfig"
	case "dns":
		return "DNSConfig"
	case "iam_role":
		return "IAMRoleConfig"
	case "api_gateway":
		return "APIGatewayConfig"
	}
	// Default: camelize snake-case tail (e.g. "container_service" →
	// "ContainerService") then append "Config". Words are joined
	// without separators per protobuf convention.
	parts := strings.Split(tail, "_")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "") + "Config"
}

func extractConfigMessages(t *testing.T, src string) []string {
	t.Helper()
	matches := configMessagePattern.FindAllStringSubmatch(src, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// TestCatalog_CoversAllTypedConfigs is the drift-detection backbone
// for the vendored proto. It walks every *Config message in the
// vendored workflow-plugin-infra/internal/contracts/infra.proto and
// asserts the catalog (T7b) has an entry for each (except the
// allowlisted abstract base).
//
// Failure modes this test catches:
//   - Upstream adds a new typed Config (e.g. ServerlessFunctionConfig)
//     and the vendored copy is refreshed without a paired catalog
//     entry → test fails.
//   - Catalog loses an entry → AllExpectedTypesRegistered already
//     catches that, but this is the cross-repo guard.
func TestCatalog_CoversAllTypedConfigs(t *testing.T) {
	data, err := os.ReadFile("../testdata/infra.proto")
	if err != nil {
		t.Fatalf("read vendored proto: %v", err)
	}

	messages := extractConfigMessages(t, string(data))
	if len(messages) == 0 {
		t.Fatal("regex extracted zero *Config messages — pattern or vendored file likely broken")
	}

	cat := catalog.New()
	coveredMessages := map[string]bool{}
	for _, typeName := range cat.AllTypes() {
		coveredMessages[typeToConfigMessage(typeName)] = true
	}

	for _, msg := range messages {
		if protoConfigAllowlist[msg] {
			continue
		}
		if !coveredMessages[msg] {
			t.Errorf("typed message %s is in vendored infra.proto but missing from FieldSpec catalog (typeToConfigMessage map; T7b)", msg)
		}
	}
}

// TestCatalog_NoUncatalogedTypes is the reverse-direction guard: every
// catalog entry's Config message MUST exist in the vendored proto.
// This catches the case where a catalog entry is added pointing at a
// renamed / removed upstream Config.
func TestCatalog_NoUncatalogedTypes(t *testing.T) {
	data, err := os.ReadFile("../testdata/infra.proto")
	if err != nil {
		t.Fatalf("read vendored proto: %v", err)
	}
	messages := extractConfigMessages(t, string(data))
	protoSet := map[string]bool{}
	for _, m := range messages {
		protoSet[m] = true
	}

	cat := catalog.New()
	for _, typeName := range cat.AllTypes() {
		msg := typeToConfigMessage(typeName)
		if !protoSet[msg] {
			t.Errorf("catalog entry %q maps to %q which is NOT in vendored proto — upstream may have renamed/removed it",
				typeName, msg)
		}
	}
}
