package config

import "log"

// CIBuildSecurity configures supply-chain hardening for the build phase.
type CIBuildSecurity struct {
	Hardened        bool               `json:"hardened" yaml:"hardened"`
	SBOM            bool               `json:"sbom" yaml:"sbom"`
	Provenance      string             `json:"provenance,omitempty" yaml:"provenance,omitempty"`
	Sign            bool               `json:"sign,omitempty" yaml:"sign,omitempty"`
	NonRoot         bool               `json:"non_root" yaml:"non_root"`
	BaseImagePolicy *CIBaseImagePolicy `json:"base_image_policy,omitempty" yaml:"base_image_policy,omitempty"`
}

// CIBaseImagePolicy restricts which base images may be used in container builds.
type CIBaseImagePolicy struct {
	AllowPrefixes []string `json:"allow_prefixes,omitempty" yaml:"allow_prefixes,omitempty"`
	DenyPrefixes  []string `json:"deny_prefixes,omitempty" yaml:"deny_prefixes,omitempty"`
}

// ApplyDefaults returns a CIBuildSecurity with opinionated secure defaults applied.
// If the receiver is nil, a fully-hardened default struct is returned.
// If the receiver is non-nil, only the Provenance field is defaulted when empty;
// all other fields are honored as-is (including explicit false values).
func (s *CIBuildSecurity) ApplyDefaults() *CIBuildSecurity {
	if s == nil {
		return &CIBuildSecurity{
			Hardened:   true,
			SBOM:       true,
			Provenance: "slsa-3",
			NonRoot:    true,
		}
	}
	out := *s
	if !out.Hardened {
		log.Printf("warning: ci.build.security.hardened=false — supply-chain hardening disabled")
	}
	if out.Provenance == "" {
		out.Provenance = "slsa-3"
	}
	return &out
}
