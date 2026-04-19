package config

// CIRegistry describes a container registry used by the build pipeline.
type CIRegistry struct {
	Name      string               `json:"name" yaml:"name"`
	Type      string               `json:"type" yaml:"type"`
	Path      string               `json:"path" yaml:"path"`
	Auth      *CIRegistryAuth      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Retention *CIRegistryRetention `json:"retention,omitempty" yaml:"retention,omitempty"`
}

// CIRegistryAuth holds credentials for pushing/pulling from a registry.
type CIRegistryAuth struct {
	Env        string               `json:"env,omitempty" yaml:"env,omitempty"`
	File       string               `json:"file,omitempty" yaml:"file,omitempty"`
	AWSProfile string               `json:"aws_profile,omitempty" yaml:"aws_profile,omitempty"`
	Vault      *CIRegistryVaultAuth `json:"vault,omitempty" yaml:"vault,omitempty"`
}

// CIRegistryVaultAuth specifies a HashiCorp Vault path for registry credentials.
type CIRegistryVaultAuth struct {
	Address string `json:"address" yaml:"address"`
	Path    string `json:"path" yaml:"path"`
}

// CIRegistryRetention configures automatic tag pruning for a registry.
type CIRegistryRetention struct {
	KeepLatest  int    `json:"keep_latest,omitempty" yaml:"keep_latest,omitempty"`
	UntaggedTTL string `json:"untagged_ttl,omitempty" yaml:"untagged_ttl,omitempty"`
	Schedule    string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
}
