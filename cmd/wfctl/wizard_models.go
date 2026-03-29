package main

// wizardData holds all data collected across wizard screens.
type wizardData struct {
	// Screen 1: project info
	ProjectName string
	Description string

	// Screen 2: services
	MultiService bool
	ServiceNames []string // parsed from comma-separated input

	// Screen 3: infrastructure
	HasDatabase bool
	HasCache    bool
	HasMQ       bool

	// Screen 4: environments
	EnvLocal      bool
	EnvStaging    bool
	EnvProduction bool

	// Screen 5: deployment
	DeployProvider string // docker, kubernetes, aws-ecs

	// Screen 6: secret stores
	SecretStores       []secretStoreEntry // named stores configured
	DefaultSecretStore string

	// Screen 7: secret routing — per-secret store override
	SecretRoutes map[string]string // secret name → store name

	// Screen 8: bulk secret input
	BulkSecrets map[string]string // secret name → value

	// Screen 9: CI/CD
	GenerateCI bool
	CIPlatform string // github-actions, gitlab-ci

	// Legacy single-provider field (used in YAML generation for simple cases)
	SecretsProvider string // env, vault, aws-secrets-manager
}

// secretStoreEntry is a named secret store configured in the wizard.
type secretStoreEntry struct {
	Name      string // e.g. "primary", "github", "aws"
	Provider  string // env, vault, aws-secrets-manager, gcp-secret-manager
	IsDefault bool
}

// infraResolutionEntry stores per-environment resolution strategy for one resource.
type infraResolutionEntry struct {
	ResourceName string
	EnvName      string
	Strategy     string // container, provision, existing
	Connection   string // host:port for existing
}

// screenID identifies a wizard screen.
type screenID int

const (
	screenProjectInfo    screenID = iota
	screenServices                // 1
	screenInfrastructure          // 2
	screenInfraResolution         // 3: per-env strategy for each detected infra resource
	screenEnvironments            // 4
	screenDeployment              // 5
	screenSecretStores            // 6: define named secret stores + default
	screenSecretRouting           // 7: per-secret store override
	screenBulkSecrets             // 8: hidden input for each required secret
	screenCICD                    // 9
	screenReview                  // 10
	screenDone                    // 11
)

// inputField holds a named text input.
type inputField struct {
	label       string
	placeholder string
	value       string
	cursor      int // byte cursor within value
}

// insertChar inserts a rune at the cursor position and advances the cursor.
func (f *inputField) insertChar(r rune) {
	s := []rune(f.value)
	if f.cursor > len(s) {
		f.cursor = len(s)
	}
	s = append(s[:f.cursor], append([]rune{r}, s[f.cursor:]...)...)
	f.value = string(s)
	f.cursor++
}

// deleteBack deletes the character before the cursor.
func (f *inputField) deleteBack() {
	s := []rune(f.value)
	if f.cursor <= 0 || len(s) == 0 {
		return
	}
	s = append(s[:f.cursor-1], s[f.cursor:]...)
	f.value = string(s)
	f.cursor--
}

// checkboxItem is a toggleable option.
type checkboxItem struct {
	label   string
	checked bool
}

// dropdownItem is a selectable option.
type dropdownItem struct {
	label string
	value string
}

// infraResolutionItem is one (resource × environment) row in the infra resolution screen.
type infraResolutionItem struct {
	resource   string
	env        string
	strategies []dropdownItem
	cursor     int // index into strategies
	connInput  inputField
	showConn   bool // true when strategy == "existing"
}

// wizardStoreRow is a named secret store configured in the wizard.
type wizardStoreRow struct {
	name      string
	provider  string
	isDefault bool
}

// wizardRouteRow is a per-secret store override row.
type wizardRouteRow struct {
	secretName string
	storeItems []dropdownItem // available store names
	cursor     int
}

// wizardBulkRow is a single secret in the bulk-input screen.
type wizardBulkRow struct {
	name    string
	value   string
	autoGen bool // true when the secret was auto-generated
	skip    bool // true for no-access stores
}
