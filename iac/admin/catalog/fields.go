// fields.go (T7b) populates the package-level catalogEntries and
// freeformReasons tables that T7a's New() / FreeformReason() expose.
//
// Per the T7a header comment (catalog.go), Go forbids redeclaring a
// package-level var across files in the same package, so this file
// reassigns the seams from init() rather than re-declaring them.
//
// Coverage maps the 13 typed `infra.*` Configs in
// workflow-plugin-infra/internal/contracts/infra.proto to their
// form-builder FieldSpecs. The catalog_proto_parity_test.go (T9) walks
// the vendored proto and asserts every non-allowlisted *Config message
// is covered here.
//
// Selectable-over-free-text contract: every Kind=="string" or
// Kind=="array_string" entry MUST carry a matching reason in
// freeformReasons[typeName][fieldName]. The audit test in
// fields_audit_test.go (T7b) enforces this.
//
// Design source: docs/plans/2026-05-27-infra-admin-dynamic-design.md
// §FieldSpec Catalog (lines ~410-445).

package catalog

func init() {
	catalogEntries = func() map[string][]FieldSpec {
		return map[string][]FieldSpec{
			// ---- infra.vpc → VPCConfig (proto §65) ----
			"infra.vpc": {
				providerField(),
				regionField(),
				{
					Name:        "cidr",
					Label:       "CIDR block",
					Kind:        "string", // FREEFORM_OK: arbitrary RFC1918 / public-IP range
					Required:    true,
					Description: "IPv4 CIDR (e.g. 10.0.0.0/16); per-provider validation runs on infra plan",
				},
				{
					Name:           "availability_zones",
					Label:          "Availability zones",
					Kind:           "array_enum_dynamic",
					EnumSource:     "regions",
					DependsOnField: "provider",
					ElementKind:    "enum_dynamic",
					MinCount:       0,
					MaxCount:       6,
					Description:    "AZ codes valid for the chosen provider+region",
				},
			},

			// ---- infra.container_service → ContainerServiceConfig (proto §17) ----
			"infra.container_service": {
				providerField(),
				regionField(),
				{
					Name:        "image",
					Label:       "Container image",
					Kind:        "string", // FREEFORM_OK: registry tag (registry/path:tag)
					Required:    true,
					Description: "Fully-qualified image tag, e.g. registry.example.com/svc:1.4.2",
				},
				{
					Name:        "ports",
					Label:       "Container ports",
					Kind:        "array_number",
					ElementKind: "number",
					MinCount:    1,
					MaxCount:    20,
					Description: "TCP/UDP listen ports (1-65535)",
				},
				{
					Name:         "replicas",
					Label:        "Replicas",
					Kind:         "number",
					Required:     true,
					MinCount:     1,
					MaxCount:     100,
					DefaultValue: "1",
				},
			},

			// ---- infra.k8s_cluster → K8SClusterConfig (proto §29) ----
			"infra.k8s_cluster": {
				providerField(),
				regionField(),
				{
					Name:        "version",
					Label:       "Kubernetes version",
					Kind:        "enum",
					Required:    true,
					EnumValues:  []string{"1.30", "1.29", "1.28"},
					Description: "Conservative shared version list. Switch to enum_dynamic + EnumSource=k8s-versions when per-provider variance matters.",
				},
				{
					Name:         "node_count",
					Label:        "Node count",
					Kind:         "number",
					Required:     true,
					MinCount:     1,
					MaxCount:     1000,
					DefaultValue: "3",
				},
				{
					Name:           "node_size",
					Label:          "Node size",
					Kind:           "enum_dynamic",
					EnumSource:     "sizes",
					Required:       true,
					DependsOnField: "", // sizes catalog is provider-independent in v1
					DefaultValue:   "m",
				},
			},

			// ---- infra.database → DatabaseConfig (proto §40) ----
			"infra.database": {
				providerField(),
				regionField(),
				{
					Name:           "engine",
					Label:          "Database engine",
					Kind:           "enum_dynamic",
					EnumSource:     "engines",
					DependsOnField: "provider",
					Required:       true,
				},
				{
					Name:        "version",
					Label:       "Engine version",
					Kind:        "string", // FREEFORM_OK: engine-specific (e.g. 15.5 for postgres)
					Required:    true,
					Description: "Engine-specific version string (e.g. 15.5 for postgres)",
				},
				{
					Name:         "size",
					Label:        "Instance size",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"xs", "s", "m", "l", "xl"},
					DefaultValue: "m",
				},
				{
					Name:         "storage_gb",
					Label:        "Storage (GB)",
					Kind:         "number",
					Required:     true,
					MinCount:     10,
					MaxCount:     4096,
					DefaultValue: "20",
				},
				{
					Name:         "multi_az",
					Label:        "Multi-AZ",
					Kind:         "bool",
					DefaultValue: "false",
				},
			},

			// ---- infra.cache → CacheConfig (proto §53) ----
			"infra.cache": {
				providerField(),
				regionField(),
				{
					Name:         "engine",
					Label:        "Cache engine",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"redis", "memcached", "valkey"},
					DefaultValue: "redis",
				},
				{
					Name:        "version",
					Label:       "Engine version",
					Kind:        "string", // FREEFORM_OK: engine-specific version
					Required:    true,
					Description: "Engine-specific version string",
				},
				{
					Name:         "size",
					Label:        "Instance size",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"xs", "s", "m", "l", "xl"},
					DefaultValue: "m",
				},
				{
					Name:         "nodes",
					Label:        "Node count",
					Kind:         "number",
					Required:     true,
					MinCount:     1,
					MaxCount:     12,
					DefaultValue: "1",
				},
			},

			// ---- infra.load_balancer → LoadBalancerConfig (proto §75) ----
			"infra.load_balancer": {
				providerField(),
				regionField(),
				{
					Name:         "scheme",
					Label:        "Scheme",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"internet-facing", "internal"},
					DefaultValue: "internet-facing",
				},
				{
					Name:        "ports",
					Label:       "Listener ports",
					Kind:        "array_number",
					ElementKind: "number",
					MinCount:    1,
					MaxCount:    20,
					Description: "TCP listener ports (1-65535)",
				},
			},

			// ---- infra.dns → DNSConfig (proto §85) ----
			"infra.dns": {
				providerField(),
				regionField(),
				{
					Name:        "zone",
					Label:       "DNS zone",
					Kind:        "string", // FREEFORM_OK: domain name (apex)
					Required:    true,
					Description: "Apex domain (e.g. example.com)",
				},
				{
					Name:        "record",
					Label:       "Record name",
					Kind:        "string", // FREEFORM_OK: subdomain label
					Required:    true,
					Description: "Subdomain label within the zone (e.g. api). rrtype is implicit per provider driver.",
				},
				{
					Name:        "target",
					Label:       "Target",
					Kind:        "string", // FREEFORM_OK: IP address or domain
					Required:    true,
					Description: "Record target — IPv4/IPv6 address or fully-qualified domain",
				},
			},

			// ---- infra.registry → RegistryConfig (proto §96) ----
			"infra.registry": {
				providerField(),
				regionField(),
				{
					Name:        "name",
					Label:       "Registry name",
					Kind:        "string", // FREEFORM_OK: opaque provider-namespaced label
					Required:    true,
					Description: "Provider-namespaced registry name; uniqueness scoped to the account",
				},
				{
					Name:         "public",
					Label:        "Public registry",
					Kind:         "bool",
					DefaultValue: "false",
				},
			},

			// ---- infra.api_gateway → APIGatewayConfig (proto §106) ----
			"infra.api_gateway": {
				providerField(),
				regionField(),
				{
					Name:         "protocol",
					Label:        "Protocol",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"http", "https", "grpc", "websocket"},
					DefaultValue: "https",
				},
				{
					Name:        "routes",
					Label:       "Routes",
					Kind:        "array_string", // FREEFORM_OK: opaque per-provider routing DSL
					ElementKind: "string",
					MinCount:    1,
					MaxCount:    100,
					Description: "Route specs in the provider's routing DSL (e.g. `/api/* → backend:8080`)",
				},
			},

			// ---- infra.firewall → FirewallConfig (proto §116) ----
			"infra.firewall": {
				providerField(),
				regionField(),
				{
					Name:        "ingress",
					Label:       "Ingress rules",
					Kind:        "array_string", // FREEFORM_OK: per-provider rule DSL
					ElementKind: "string",
					MinCount:    0,
					MaxCount:    100,
					Description: "Ingress rule DSL (e.g. `tcp:443:0.0.0.0/0`)",
				},
				{
					Name:        "egress",
					Label:       "Egress rules",
					Kind:        "array_string", // FREEFORM_OK: per-provider rule DSL
					ElementKind: "string",
					MinCount:    0,
					MaxCount:    100,
					Description: "Egress rule DSL (e.g. `tcp:0.0.0.0/0:443`)",
				},
			},

			// ---- infra.iam_role → IAMRoleConfig (proto §126) ----
			"infra.iam_role": {
				providerField(),
				regionField(),
				{
					Name:        "name",
					Label:       "Role name",
					Kind:        "string", // FREEFORM_OK: provider-namespaced role label
					Required:    true,
					Description: "Provider-namespaced role identifier",
				},
				{
					Name:        "policies",
					Label:       "Attached policies",
					Kind:        "array_string", // FREEFORM_OK: opaque policy ARNs/IDs
					ElementKind: "string",
					MinCount:    0,
					MaxCount:    50,
					Description: "Policy ARNs (AWS) / role IDs (GCP) / built-in role names (Azure)",
				},
			},

			// ---- infra.storage → StorageConfig (proto §136) ----
			"infra.storage": {
				providerField(),
				regionField(),
				{
					Name:        "name",
					Label:       "Bucket name",
					Kind:        "string", // FREEFORM_OK: globally-unique-per-provider bucket name
					Required:    true,
					Description: "Provider-unique bucket / container name",
				},
				{
					Name:         "class",
					Label:        "Storage class",
					Kind:         "enum",
					Required:     true,
					EnumValues:   []string{"standard", "cold", "archive", "nearline", "coldline"},
					DefaultValue: "standard",
				},
				{
					Name:         "versioning",
					Label:        "Versioning enabled",
					Kind:         "bool",
					DefaultValue: "false",
				},
			},

			// ---- infra.certificate → CertificateConfig (proto §147) ----
			"infra.certificate": {
				providerField(),
				regionField(),
				{
					Name:        "domain",
					Label:       "Primary domain",
					Kind:        "string", // FREEFORM_OK: fully-qualified domain name
					Required:    true,
					Description: "Primary FQDN (e.g. example.com)",
				},
				{
					Name:        "subject_alt_names",
					Label:       "Subject alternative names",
					Kind:        "array_string", // FREEFORM_OK: SANs are arbitrary FQDNs
					ElementKind: "string",
					MinCount:    0,
					MaxCount:    100,
					Description: "Additional FQDNs covered by this certificate",
				},
			},
		}
	}

	freeformReasons = map[string]map[string]string{
		"infra.vpc": {
			"cidr": "arbitrary RFC1918 / public-IP range; per-provider validation runs on infra plan",
		},
		"infra.container_service": {
			"image": "container registry tag — opaque registry/path:tag",
		},
		"infra.database": {
			"version": "engine-specific version string (postgres 15.5, mysql 8.0, etc.)",
		},
		"infra.cache": {
			"version": "engine-specific version string (redis 7.2, memcached 1.6.x, etc.)",
		},
		"infra.dns": {
			"zone":   "apex domain — arbitrary FQDN",
			"record": "subdomain label within the zone",
			"target": "IPv4/IPv6 address or FQDN",
		},
		"infra.registry": {
			"name": "provider-namespaced registry name (no fixed enumeration)",
		},
		"infra.api_gateway": {
			"routes": "per-provider routing DSL — opaque rule strings",
		},
		"infra.firewall": {
			"ingress": "per-provider rule DSL (e.g. tcp:443:0.0.0.0/0)",
			"egress":  "per-provider rule DSL",
		},
		"infra.iam_role": {
			"name":     "provider-namespaced role identifier",
			"policies": "opaque policy identifiers (ARNs / role IDs)",
		},
		"infra.storage": {
			"name": "globally-unique-per-provider bucket / container name",
		},
		"infra.certificate": {
			"domain":            "primary FQDN — arbitrary domain",
			"subject_alt_names": "arbitrary FQDNs",
		},
	}
}

// providerField returns the common provider FieldSpec used by every
// typed `infra.*` Config. Defined as a helper so the catalog table
// stays compact and a single field shape (Name, EnumSource, etc.)
// applies consistently across all 13 entries.
func providerField() FieldSpec {
	return FieldSpec{
		Name:        "provider",
		Label:       "Provider module",
		Kind:        "enum_dynamic",
		EnumSource:  "providers",
		Required:    true,
		Description: "Name of the iac.provider module (host YAML `name:` of the iac.provider entry)",
	}
}

// regionField is the common region FieldSpec — enum_dynamic with a
// per-provider region catalog, dependent on the `provider` field.
func regionField() FieldSpec {
	return FieldSpec{
		Name:           "region",
		Label:          "Region",
		Kind:           "enum_dynamic",
		EnumSource:     "regions",
		DependsOnField: "provider",
		Required:       true,
		Description:    "Provider-specific region code (e.g. nyc1 for digitalocean, us-east-1 for aws)",
	}
}
