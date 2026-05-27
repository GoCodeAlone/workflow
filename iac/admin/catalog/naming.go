package catalog

import "strings"

// ConfigProtoPackage is the fully-qualified proto package the vendored
// workflow-plugin-infra/internal/contracts/infra.proto declares. Used
// to build `config_message_fqn` references in AdminResourceTypeMetadata
// responses so cross-language consumers can correlate against the
// vendored proto descriptor.
//
// Note the **plural** "plugins" — earlier draft code (T6 commit
// 1ea231fdd) used the singular "plugin" which produced FQNs that
// nothing on the wire matched. Per spec-reviewer T6 F2 (commit
// 1ea231fdd). The vendored proto at iac/admin/testdata/infra.proto:8
// is authoritative for this string.
const ConfigProtoPackage = "workflow.plugins.infra.v1"

// ConfigMessageShortName maps an "infra.<snake>" type name to its
// proto CamelCase Config message short name (e.g. "infra.vpc" →
// "VPCConfig"). Single-sourced here so the T9 vendored-proto parity
// test and the T6 handler library cannot drift on acronym
// preservation — earlier T6 code reimplemented snake→PascalCase
// without the acronym table and produced "VpcConfig", which doesn't
// exist in the proto. Per spec-reviewer T6 F2.
//
// Special-case acronym preservations (VPC, K8S, DNS, IAM, API) avoid
// degenerate `Vpc` ⇆ `VPC` toggling. The set is closed at 13 entries
// today (the design's typed-Config inventory); new acronyms in
// future Configs require both extending this switch AND updating
// the catalog. The vendored-proto parity test detects misses.
func ConfigMessageShortName(typeName string) string {
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

// ConfigMessageFQN returns the fully-qualified proto message name for
// a given catalog type. Composition of ConfigProtoPackage + "." +
// ConfigMessageShortName so both halves can be tested independently
// and the FQN is always consistent between catalog handler usage
// and the vendored-proto parity test.
//
// Returns the empty string when typeName lacks the "infra." prefix —
// callers treat empty as "no FQN known" rather than emitting a
// malformed reference.
func ConfigMessageFQN(typeName string) string {
	if !strings.HasPrefix(typeName, "infra.") {
		return ""
	}
	return ConfigProtoPackage + "." + ConfigMessageShortName(typeName)
}
