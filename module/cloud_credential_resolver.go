package module

// CloudCredentialResolver resolves credentials for a specific cloud provider and credential type.
type CloudCredentialResolver interface {
	// Provider returns the cloud provider name (e.g., "aws", "gcp", "azure", "digitalocean", "kubernetes").
	Provider() string
	// CredentialType returns the credential type this resolver handles (e.g., "static", "env", "profile", "role_arn").
	CredentialType() string
	// Resolve resolves credentials from the given config and stores them in the CloudAccount.
	Resolve(m *CloudAccount) error
}

// credentialResolvers is the global registry: provider -> credType -> resolver.
var credentialResolvers = map[string]map[string]CloudCredentialResolver{}

// RegisterCredentialResolver registers a CloudCredentialResolver in the global registry.
// It is safe to call from init() functions.
func RegisterCredentialResolver(r CloudCredentialResolver) {
	p := r.Provider()
	if credentialResolvers[p] == nil {
		credentialResolvers[p] = map[string]CloudCredentialResolver{}
	}
	credentialResolvers[p][r.CredentialType()] = r
}
