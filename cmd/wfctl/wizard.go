package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#06B6D4"))

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#374151")).
			Padding(0, 1)

	checkboxOnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981"))

	checkboxOffStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444"))

	codeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FCD34D")).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)
)

// ── wizard model ──────────────────────────────────────────────────────────────

// wizardModel is the top-level Bubbletea model for the interactive setup wizard.
type wizardModel struct {
	screen screenID
	data   wizardData
	err    string

	// Screen 1: project info fields (indexed: 0=name, 1=description).
	infoFields    [2]inputField
	infoFocused   int

	// Screen 2: services.
	serviceInput inputField // comma-separated names

	// Screen 3: infrastructure checkboxes.
	infraItems [3]checkboxItem
	infraCursor int

	// Screen 4: environments checkboxes.
	envItems  [3]checkboxItem
	envCursor int

	// Screen 5: deployment provider dropdown.
	deployItems  []dropdownItem
	deployCursor int

	// Screen 6: secrets provider dropdown.
	secretsItems  []dropdownItem
	secretsCursor int

	// Screen 7: CI/CD
	ciItems  [2]checkboxItem // generate CI?, platform selection cursor
	ciPlatformItems []dropdownItem
	ciPlatformCursor int
	ciGenerate bool

	// Screen 8: review — generated YAML preview.
	reviewYAML string

	// Terminal dimensions.
	width  int
	height int
}

// newWizardModel creates a fresh wizard model with sensible defaults.
func newWizardModel() wizardModel {
	m := wizardModel{
		screen: screenProjectInfo,
		infoFields: [2]inputField{
			{label: "Project name", placeholder: "my-app"},
			{label: "Description", placeholder: "A workflow-powered application"},
		},
		serviceInput: inputField{label: "Service names", placeholder: "api, worker, scheduler"},
		infraItems: [3]checkboxItem{
			{label: "PostgreSQL database (database.postgres)", checked: true},
			{label: "Redis cache (cache.redis)", checked: false},
			{label: "NATS message queue (messaging.nats)", checked: false},
		},
		envItems: [3]checkboxItem{
			{label: "local", checked: true},
			{label: "staging", checked: true},
			{label: "production", checked: true},
		},
		deployItems: []dropdownItem{
			{label: "Docker Compose (local / CI)", value: "docker"},
			{label: "Kubernetes", value: "kubernetes"},
			{label: "AWS ECS", value: "aws-ecs"},
		},
		secretsItems: []dropdownItem{
			{label: "Environment variables (default)", value: "env"},
			{label: "HashiCorp Vault", value: "vault"},
			{label: "AWS Secrets Manager", value: "aws-secrets-manager"},
			{label: "GCP Secret Manager", value: "gcp-secret-manager"},
		},
		ciGenerate: true,
		ciPlatformItems: []dropdownItem{
			{label: "GitHub Actions", value: "github-actions"},
			{label: "GitLab CI", value: "gitlab-ci"},
		},
	}
	return m
}

// Init implements tea.Model.
func (m wizardModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyEscape:
			if m.screen > screenProjectInfo {
				m.screen--
				m.err = ""
			}
		case tea.KeyEnter:
			return m.advance()
		case tea.KeyTab:
			return m.tabCycle(), nil
		default:
			return m.handleScreenKey(msg), nil
		}
	}
	return m, nil
}

// advance validates the current screen and moves to the next.
func (m wizardModel) advance() (tea.Model, tea.Cmd) {
	m.err = ""
	switch m.screen {
	case screenProjectInfo:
		name := strings.TrimSpace(m.infoFields[0].value)
		if name == "" {
			m.err = "project name is required"
			return m, nil
		}
		m.data.ProjectName = name
		m.data.Description = strings.TrimSpace(m.infoFields[1].value)
		if m.data.Description == "" {
			m.data.Description = "A workflow-powered application"
		}

	case screenServices:
		raw := strings.TrimSpace(m.serviceInput.value)
		if raw == "" {
			m.data.MultiService = false
			m.data.ServiceNames = nil
		} else {
			parts := strings.Split(raw, ",")
			var names []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					names = append(names, p)
				}
			}
			m.data.MultiService = len(names) > 1
			m.data.ServiceNames = names
		}

	case screenInfrastructure:
		m.data.HasDatabase = m.infraItems[0].checked
		m.data.HasCache = m.infraItems[1].checked
		m.data.HasMQ = m.infraItems[2].checked

	case screenEnvironments:
		m.data.EnvLocal = m.envItems[0].checked
		m.data.EnvStaging = m.envItems[1].checked
		m.data.EnvProduction = m.envItems[2].checked
		if !m.data.EnvLocal && !m.data.EnvStaging && !m.data.EnvProduction {
			m.err = "select at least one environment"
			return m, nil
		}

	case screenDeployment:
		m.data.DeployProvider = m.deployItems[m.deployCursor].value

	case screenSecrets:
		m.data.SecretsProvider = m.secretsItems[m.secretsCursor].value

	case screenCICD:
		m.data.GenerateCI = m.ciGenerate
		if m.ciGenerate {
			m.data.CIPlatform = m.ciPlatformItems[m.ciPlatformCursor].value
		}

	case screenReview:
		// User confirmed — write the file.
		if err := m.writeOutput(); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.screen = screenDone
		return m, nil

	case screenDone:
		return m, tea.Quit
	}

	m.screen++
	if m.screen == screenReview {
		m.reviewYAML = buildWizardYAML(&m.data)
	}
	return m, nil
}

// tabCycle moves focus between fields on the current screen.
func (m wizardModel) tabCycle() wizardModel {
	switch m.screen {
	case screenProjectInfo:
		m.infoFocused = (m.infoFocused + 1) % 2
	case screenInfrastructure:
		m.infraCursor = (m.infraCursor + 1) % len(m.infraItems)
	case screenEnvironments:
		m.envCursor = (m.envCursor + 1) % len(m.envItems)
	case screenDeployment:
		m.deployCursor = (m.deployCursor + 1) % len(m.deployItems)
	case screenSecrets:
		m.secretsCursor = (m.secretsCursor + 1) % len(m.secretsItems)
	case screenCICD:
		if m.ciGenerate {
			m.ciPlatformCursor = (m.ciPlatformCursor + 1) % len(m.ciPlatformItems)
		}
	}
	return m
}

// handleScreenKey routes key events to the focused field on the current screen.
func (m wizardModel) handleScreenKey(msg tea.KeyPressMsg) wizardModel {
	switch m.screen {
	case screenProjectInfo:
		switch msg.Code {
		case tea.KeyBackspace:
			m.infoFields[m.infoFocused].deleteBack()
		default:
			if msg.Text != "" {
				for _, r := range msg.Text {
					m.infoFields[m.infoFocused].insertChar(r)
				}
			}
		}

	case screenServices:
		switch msg.Code {
		case tea.KeyBackspace:
			m.serviceInput.deleteBack()
		default:
			if msg.Text != "" {
				for _, r := range msg.Text {
					m.serviceInput.insertChar(r)
				}
			}
		}

	case screenInfrastructure:
		switch msg.Code {
		case tea.KeyUp:
			if m.infraCursor > 0 {
				m.infraCursor--
			}
		case tea.KeyDown:
			if m.infraCursor < len(m.infraItems)-1 {
				m.infraCursor++
			}
		case tea.KeySpace:
			m.infraItems[m.infraCursor].checked = !m.infraItems[m.infraCursor].checked
		}

	case screenEnvironments:
		switch msg.Code {
		case tea.KeyUp:
			if m.envCursor > 0 {
				m.envCursor--
			}
		case tea.KeyDown:
			if m.envCursor < len(m.envItems)-1 {
				m.envCursor++
			}
		case tea.KeySpace:
			m.envItems[m.envCursor].checked = !m.envItems[m.envCursor].checked
		}

	case screenDeployment:
		switch msg.Code {
		case tea.KeyUp:
			if m.deployCursor > 0 {
				m.deployCursor--
			}
		case tea.KeyDown:
			if m.deployCursor < len(m.deployItems)-1 {
				m.deployCursor++
			}
		}

	case screenSecrets:
		switch msg.Code {
		case tea.KeyUp:
			if m.secretsCursor > 0 {
				m.secretsCursor--
			}
		case tea.KeyDown:
			if m.secretsCursor < len(m.secretsItems)-1 {
				m.secretsCursor++
			}
		}

	case screenCICD:
		switch msg.Code {
		case tea.KeySpace:
			m.ciGenerate = !m.ciGenerate
		case tea.KeyUp:
			if m.ciGenerate && m.ciPlatformCursor > 0 {
				m.ciPlatformCursor--
			}
		case tea.KeyDown:
			if m.ciGenerate && m.ciPlatformCursor < len(m.ciPlatformItems)-1 {
				m.ciPlatformCursor++
			}
		}
	}
	return m
}

// View implements tea.Model.
func (m wizardModel) View() tea.View {
	return tea.NewView(m.render())
}

func (m wizardModel) render() string {
	var b strings.Builder

	// Title bar.
	b.WriteString(titleStyle.Render("wfctl wizard") + "  ")
	b.WriteString(dimStyle.Render(m.progressBar()))
	b.WriteString("\n\n")

	switch m.screen {
	case screenProjectInfo:
		b.WriteString(m.viewProjectInfo())
	case screenServices:
		b.WriteString(m.viewServices())
	case screenInfrastructure:
		b.WriteString(m.viewInfrastructure())
	case screenEnvironments:
		b.WriteString(m.viewEnvironments())
	case screenDeployment:
		b.WriteString(m.viewDeployment())
	case screenSecrets:
		b.WriteString(m.viewSecrets())
	case screenCICD:
		b.WriteString(m.viewCICD())
	case screenReview:
		b.WriteString(m.viewReview())
	case screenDone:
		b.WriteString(m.viewDone())
	}

	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("! "+m.err) + "\n")
	}

	b.WriteString("\n" + m.navHint())
	return b.String()
}

func (m wizardModel) progressBar() string {
	total := int(screenDone)
	current := int(m.screen)
	var parts []string
	for i := 0; i < total; i++ {
		if i < current {
			parts = append(parts, "●")
		} else if i == current {
			parts = append(parts, activeStyle.Render("●"))
		} else {
			parts = append(parts, dimStyle.Render("○"))
		}
	}
	return strings.Join(parts, " ")
}

func (m wizardModel) navHint() string {
	switch m.screen {
	case screenReview:
		return hintStyle.Render("Enter: write app.yaml  Esc: back  Ctrl+C: quit")
	case screenDone:
		return hintStyle.Render("Enter: exit")
	default:
		return hintStyle.Render("Enter: next  Esc: back  Tab: next field  Space: toggle  ↑↓: move  Ctrl+C: quit")
	}
}

// ── screen renderers ──────────────────────────────────────────────────────────

func (m wizardModel) viewProjectInfo() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("1 / 8  Project Info") + "\n\n")
	for i, f := range m.infoFields {
		focused := i == m.infoFocused
		cursor := " "
		if focused {
			cursor = activeStyle.Render(">")
		}
		display := f.value
		if display == "" {
			display = dimStyle.Render(f.placeholder)
		}
		if focused {
			b.WriteString(fmt.Sprintf("%s %s: %s\n", cursor, headerStyle.Render(f.label), inputStyle.Render(display+" ")))
		} else {
			b.WriteString(fmt.Sprintf("%s %s: %s\n", cursor, dimStyle.Render(f.label), display))
		}
	}
	return b.String()
}

func (m wizardModel) viewServices() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("2 / 8  Services") + "\n\n")
	b.WriteString("Leave blank for a single-service app, or enter comma-separated service names.\n\n")
	display := m.serviceInput.value
	if display == "" {
		display = dimStyle.Render(m.serviceInput.placeholder)
	}
	b.WriteString(activeStyle.Render("> ") + headerStyle.Render("Service names") + ": " + inputStyle.Render(display+" ") + "\n")
	return b.String()
}

func (m wizardModel) viewInfrastructure() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("3 / 8  Infrastructure") + "\n\n")
	b.WriteString("Select infrastructure modules to include:\n\n")
	for i, item := range m.infraItems {
		cursor := "  "
		if i == m.infraCursor {
			cursor = activeStyle.Render("▶ ")
		}
		box := checkboxOffStyle.Render("[ ]")
		if item.checked {
			box = checkboxOnStyle.Render("[x]")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, item.label))
	}
	return b.String()
}

func (m wizardModel) viewEnvironments() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("4 / 8  Environments") + "\n\n")
	b.WriteString("Select deployment environments:\n\n")
	for i, item := range m.envItems {
		cursor := "  "
		if i == m.envCursor {
			cursor = activeStyle.Render("▶ ")
		}
		box := checkboxOffStyle.Render("[ ]")
		if item.checked {
			box = checkboxOnStyle.Render("[x]")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, item.label))
	}
	return b.String()
}

func (m wizardModel) viewDeployment() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("5 / 8  Deployment Provider") + "\n\n")
	b.WriteString("Select the primary deployment provider:\n\n")
	for i, item := range m.deployItems {
		cursor := "  "
		if i == m.deployCursor {
			cursor = activeStyle.Render("▶ ")
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
	}
	return b.String()
}

func (m wizardModel) viewSecrets() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("6 / 8  Secrets Provider") + "\n\n")
	b.WriteString("Select how secrets are managed:\n\n")
	for i, item := range m.secretsItems {
		cursor := "  "
		if i == m.secretsCursor {
			cursor = activeStyle.Render("▶ ")
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
	}
	return b.String()
}

func (m wizardModel) viewCICD() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("7 / 8  CI/CD") + "\n\n")

	genBox := checkboxOffStyle.Render("[ ]")
	if m.ciGenerate {
		genBox = checkboxOnStyle.Render("[x]")
	}
	b.WriteString(activeStyle.Render("▶ ") + genBox + " Generate CI bootstrap\n\n")

	if m.ciGenerate {
		b.WriteString(dimStyle.Render("  Platform:\n"))
		for i, item := range m.ciPlatformItems {
			cursor := "  "
			if i == m.ciPlatformCursor {
				cursor = activeStyle.Render("  ▶ ")
			} else {
				cursor = "    "
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
		}
	}
	return b.String()
}

func (m wizardModel) viewReview() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("8 / 8  Review") + "\n\n")
	b.WriteString("The following will be written to " + activeStyle.Render("app.yaml") + ":\n\n")

	// Show first 30 lines of the YAML to avoid flooding the terminal.
	lines := strings.Split(m.reviewYAML, "\n")
	maxLines := 30
	shown := lines
	if len(shown) > maxLines {
		shown = lines[:maxLines]
	}
	for _, l := range shown {
		b.WriteString(codeStyle.Render(l) + "\n")
	}
	if len(lines) > maxLines {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... (%d more lines)", len(lines)-maxLines)) + "\n")
	}
	return b.String()
}

func (m wizardModel) viewDone() string {
	var b strings.Builder
	b.WriteString(activeStyle.Render("✓ app.yaml written successfully!") + "\n\n")
	b.WriteString("Next steps:\n")
	b.WriteString("  " + codeStyle.Render("wfctl validate app.yaml") + "\n")
	b.WriteString("  " + codeStyle.Render("wfctl dev up") + "\n")
	if m.data.GenerateCI {
		platform := m.data.CIPlatform
		if platform == "github-actions" {
			b.WriteString("  " + codeStyle.Render("wfctl ci init --platform github-actions") + "\n")
		} else {
			b.WriteString("  " + codeStyle.Render("wfctl ci init --platform gitlab-ci") + "\n")
		}
	}
	return b.String()
}

// ── YAML generation ────────────────────────────────────────────────────────────

// buildWizardYAML generates a workflow YAML config from the collected data.
func buildWizardYAML(d *wizardData) string {
	var b strings.Builder

	name := d.ProjectName
	if name == "" {
		name = "my-app"
	}

	b.WriteString(fmt.Sprintf("# %s — generated by wfctl wizard\n\n", name))

	// Modules.
	var modules []string
	if d.HasDatabase {
		modules = append(modules, `- name: db
  type: database.postgres
  config:
    dsn: "${DATABASE_URL}"`)
	}
	if d.HasCache {
		modules = append(modules, `- name: cache
  type: cache.redis
  config:
    addr: "${REDIS_URL}"`)
	}
	if d.HasMQ {
		modules = append(modules, `- name: mq
  type: messaging.nats
  config:
    url: "${NATS_URL}"`)
	}
	// Always add HTTP server module.
	modules = append(modules, `- name: server
  type: http.server
  config:
    port: 8080`)

	b.WriteString("modules:\n")
	for _, m := range modules {
		// Indent each line by 2 spaces.
		for _, l := range strings.Split(m, "\n") {
			b.WriteString("  " + l + "\n")
		}
	}
	b.WriteString("\n")

	// Workflows.
	b.WriteString("workflows:\n")
	b.WriteString("  http:\n")
	b.WriteString("    type: http\n")
	b.WriteString("    server: server\n")
	b.WriteString("    routes:\n")
	b.WriteString("      - path: /healthz\n")
	b.WriteString("        method: GET\n")
	b.WriteString("        pipeline: pipeline-healthz\n\n")

	// Pipelines.
	b.WriteString("pipelines:\n")
	b.WriteString("  pipeline-healthz:\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - name: ok\n")
	b.WriteString("        type: step.respond\n")
	b.WriteString("        config:\n")
	b.WriteString("          body: '{\"status\":\"ok\"}'\n")
	b.WriteString("          contentType: application/json\n\n")

	// Services section (multi-service).
	if d.MultiService && len(d.ServiceNames) > 1 {
		b.WriteString("services:\n")
		for _, svc := range d.ServiceNames {
			b.WriteString(fmt.Sprintf("  %s:\n", svc))
			b.WriteString(fmt.Sprintf("    binary: ./cmd/%s\n", svc))
			b.WriteString("    expose:\n")
			b.WriteString("      - port: 8080\n")
			b.WriteString("        protocol: http\n")
		}
		b.WriteString("\n")
	}

	// Environments.
	envNames := envList(d)
	if len(envNames) > 0 {
		b.WriteString("environments:\n")
		for _, env := range envNames {
			provider := wizardEnvProvider(env, d.DeployProvider)
			b.WriteString(fmt.Sprintf("  %s:\n", env))
			b.WriteString(fmt.Sprintf("    provider: %s\n", provider))
			if env == "local" {
				b.WriteString("    exposure:\n")
				b.WriteString("      method: port-forward\n")
			}
			if d.SecretsProvider != "env" && env != "local" {
				b.WriteString(fmt.Sprintf("    secretsProvider: %s\n", d.SecretsProvider))
			}
		}
		b.WriteString("\n")
	}

	// Secrets.
	if d.SecretsProvider != "" && d.SecretsProvider != "env" {
		b.WriteString("secrets:\n")
		b.WriteString(fmt.Sprintf("  provider: %s\n", d.SecretsProvider))
		b.WriteString("  entries:\n")
		if d.HasDatabase {
			b.WriteString("    - name: DATABASE_URL\n")
		}
		if d.HasCache {
			b.WriteString("    - name: REDIS_URL\n")
		}
		if d.HasMQ {
			b.WriteString("    - name: NATS_URL\n")
		}
		b.WriteString("\n")
	}

	// CI.
	if d.GenerateCI && d.CIPlatform != "" {
		b.WriteString("ci:\n")
		b.WriteString("  build:\n")
		b.WriteString("    binaries:\n")
		b.WriteString(fmt.Sprintf("      - name: %s\n", name))
		b.WriteString("        path: ./cmd/server\n")
		b.WriteString("  test:\n")
		b.WriteString("    unit:\n")
		b.WriteString("      command: go test ./...\n")
		b.WriteString("  deploy:\n")
		b.WriteString("    environments:\n")
		for _, env := range envNames {
			if env == "local" {
				continue
			}
			b.WriteString(fmt.Sprintf("      - name: %s\n", env))
			b.WriteString(fmt.Sprintf("        provider: %s\n", wizardEnvProvider(env, d.DeployProvider)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// envList returns the selected environment names in order.
func envList(d *wizardData) []string {
	var envs []string
	if d.EnvLocal {
		envs = append(envs, "local")
	}
	if d.EnvStaging {
		envs = append(envs, "staging")
	}
	if d.EnvProduction {
		envs = append(envs, "production")
	}
	return envs
}

// wizardEnvProvider maps environment + deploy provider to a concrete provider string.
func wizardEnvProvider(env, provider string) string {
	if env == "local" {
		return "docker"
	}
	if provider == "" {
		return "docker"
	}
	return provider
}

// writeOutput writes the generated YAML to app.yaml.
func (m wizardModel) writeOutput() error {
	if m.reviewYAML == "" {
		m.reviewYAML = buildWizardYAML(&m.data)
	}
	const outFile = "app.yaml"
	if err := os.WriteFile(outFile, []byte(m.reviewYAML), 0o644); err != nil { //nolint:gosec // user file
		return fmt.Errorf("write %s: %w", outFile, err)
	}
	return nil
}

// ── CLI entrypoint ─────────────────────────────────────────────────────────────

// runWizard implements `wfctl wizard`.
func runWizard(args []string) error {
	_ = args // no flags for now

	p := tea.NewProgram(newWizardModel())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("wizard: %w", err)
	}

	// If the user quit before reaching the done screen (e.g. Ctrl+C), check
	// whether anything was written.
	if wm, ok := finalModel.(wizardModel); ok && wm.screen != screenDone {
		fmt.Fprintf(os.Stderr, "wizard cancelled — no files written\n")
	}
	return nil
}
