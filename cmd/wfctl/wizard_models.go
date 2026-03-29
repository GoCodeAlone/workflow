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

	// Screen 6: secrets
	SecretsProvider string // env, vault, aws-secrets-manager

	// Screen 7: CI/CD
	GenerateCI bool
	CIPlatform string // github-actions, gitlab-ci
}

// screenID identifies a wizard screen.
type screenID int

const (
	screenProjectInfo screenID = iota
	screenServices
	screenInfrastructure
	screenEnvironments
	screenDeployment
	screenSecrets
	screenCICD
	screenReview
	screenDone
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
