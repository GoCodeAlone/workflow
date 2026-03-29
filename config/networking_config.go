package config

// NetworkingConfig defines network exposure and policies.
type NetworkingConfig struct {
	Ingress  []IngressConfig `json:"ingress,omitempty" yaml:"ingress,omitempty"`
	Policies []NetworkPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	DNS      *DNSConfig      `json:"dns,omitempty" yaml:"dns,omitempty"`
}

// IngressConfig defines an externally-accessible endpoint.
type IngressConfig struct {
	Service      string     `json:"service,omitempty" yaml:"service,omitempty"`
	Port         int        `json:"port" yaml:"port"`
	ExternalPort int        `json:"externalPort,omitempty" yaml:"externalPort,omitempty"`
	Protocol     string     `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Path         string     `json:"path,omitempty" yaml:"path,omitempty"`
	TLS          *TLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// TLSConfig defines TLS termination.
type TLSConfig struct {
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Domain     string `json:"domain,omitempty" yaml:"domain,omitempty"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}

// NetworkPolicy defines allowed communication between services.
type NetworkPolicy struct {
	From string   `json:"from" yaml:"from"`
	To   []string `json:"to" yaml:"to"`
}

// DNSConfig defines DNS management.
type DNSConfig struct {
	Provider string      `json:"provider,omitempty" yaml:"provider,omitempty"`
	Zone     string      `json:"zone,omitempty" yaml:"zone,omitempty"`
	Records  []DNSRecord `json:"records,omitempty" yaml:"records,omitempty"`
}

// DNSRecord is a single DNS record.
type DNSRecord struct {
	Name   string `json:"name" yaml:"name"`
	Type   string `json:"type" yaml:"type"`
	Target string `json:"target" yaml:"target"`
}
