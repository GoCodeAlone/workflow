package main

import (
	"crypto/rand"
	"encoding/hex"
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
	infoFields  [2]inputField
	infoFocused int

	// Screen 2: services.
	serviceInput inputField // comma-separated names

	// Screen 3: infrastructure checkboxes.
	infraItems  [3]checkboxItem
	infraCursor int

	// Screen 4: per-environment infra resolution.
	infraResList   []infraResolutionItem
	infraResCursor int

	// Screen 5: environments checkboxes.
	envItems  [3]checkboxItem
	envCursor int

	// Screen 6: deployment provider dropdown.
	deployItems  []dropdownItem
	deployCursor int

	// Screen 7: secret stores.
	storeProviders []dropdownItem // available provider choices
	storeEntries   []wizardStoreRow
	storeCursor    int
	storeNameInput inputField
	storeAddMode   bool // true while editing a new store name

	// Screen 8: secret routing.
	routingItems  []wizardRouteRow
	routingCursor int

	// Screen 9: bulk secret input.
	bulkItems  []wizardBulkRow
	bulkCursor int
	bulkInput  inputField

	// Screen 10: CI/CD
	ciPlatformItems  []dropdownItem
	ciPlatformCursor int
	ciGenerate       bool

	// Screen 11: review — generated YAML preview.
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
		storeProviders: []dropdownItem{
			{label: "Environment variables", value: "env"},
			{label: "HashiCorp Vault", value: "vault"},
			{label: "AWS Secrets Manager", value: "aws-secrets-manager"},
			{label: "GCP Secret Manager", value: "gcp-secret-manager"},
		},
		storeEntries: []wizardStoreRow{
			{name: "primary", provider: "env", isDefault: true},
		},
		storeNameInput: inputField{label: "Store name", placeholder: "primary"},
		ciGenerate:     true,
		ciPlatformItems: []dropdownItem{
			{label: "GitHub Actions", value: "github-actions"},
			{label: "GitLab CI", value: "gitlab-ci"},
		},
	}
	return m
}

// inferRequiredSecrets returns the secret names needed based on selected infra.
func (m *wizardModel) inferRequiredSecrets() []string {
	var secrets []string
	if m.data.HasDatabase {
		secrets = append(secrets, "DATABASE_URL")
	}
	if m.data.HasCache {
		secrets = append(secrets, "REDIS_URL")
	}
	if m.data.HasMQ {
		secrets = append(secrets, "NATS_URL")
	}
	secrets = append(secrets, "JWT_SECRET")
	return secrets
}

// buildInfraResList constructs the per-env resolution list from selected infra + envs.
func (m *wizardModel) buildInfraResList() []infraResolutionItem {
	strategies := []dropdownItem{
		{label: "Container (run locally)", value: "container"},
		{label: "Provision (IaC)", value: "provision"},
		{label: "Existing (connect to running instance)", value: "existing"},
	}
	type infraEntry struct {
		name    string
		checked bool
	}
	infra := []infraEntry{
		{"postgresql", m.data.HasDatabase},
		{"redis", m.data.HasCache},
		{"nats", m.data.HasMQ},
	}
	envs := envList(&m.data)
	var items []infraResolutionItem
	for _, ie := range infra {
		if !ie.checked {
			continue
		}
		for _, env := range envs {
			item := infraResolutionItem{
				resource:   ie.name,
				env:        env,
				strategies: strategies,
				cursor:     0,
				connInput:  inputField{label: "host:port", placeholder: "localhost:5432"},
			}
			if env == "production" {
				item.cursor = 1
			}
			items = append(items, item)
		}
	}
	return items
}

// buildRoutingRows constructs per-secret routing rows based on declared stores and secrets.
func (m *wizardModel) buildRoutingRows(secrets []string) []wizardRouteRow {
	var storeOpts []dropdownItem
	for _, s := range m.storeEntries {
		label := s.name + " (" + s.provider + ")"
		if s.isDefault {
			label += " [default]"
		}
		storeOpts = append(storeOpts, dropdownItem{label: label, value: s.name})
	}
	rows := make([]wizardRouteRow, 0, len(secrets))
	for _, sec := range secrets {
		rows = append(rows, wizardRouteRow{
			secretName: sec,
			storeItems: storeOpts,
			cursor:     0,
		})
	}
	return rows
}

// buildBulkRows constructs bulk-input rows for each required secret.
func (m *wizardModel) buildBulkRows(secrets []string) []wizardBulkRow {
	rows := make([]wizardBulkRow, 0, len(secrets))
	for _, sec := range secrets {
		rows = append(rows, wizardBulkRow{name: sec})
	}
	return rows
}

// generateSecretValue returns a random 32-byte hex string suitable for use as a secret.
func generateSecretValue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
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
				if m.screen == screenSecretStores && m.storeAddMode {
					m.storeAddMode = false
					m.storeNameInput.value = ""
					return m, nil
				}
				m.screen--
				m.err = ""
			}
		case tea.KeyEnter:
			if m.screen == screenSecretStores && m.storeAddMode {
				return m.confirmAddStore()
			}
			return m.advance()
		case tea.KeyTab:
			return m.tabCycle(), nil
		default:
			return m.handleScreenKey(msg), nil
		}
	}
	return m, nil
}

// confirmAddStore adds the new store entry from the add-mode input.
func (m wizardModel) confirmAddStore() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.storeNameInput.value)
	if name == "" {
		name = m.storeNameInput.placeholder
	}
	provider := "env"
	if m.storeCursor < len(m.storeProviders) {
		provider = m.storeProviders[m.storeCursor].value
	}
	isDefault := len(m.storeEntries) == 0
	m.storeEntries = append(m.storeEntries, wizardStoreRow{
		name:      name,
		provider:  provider,
		isDefault: isDefault,
	})
	m.storeAddMode = false
	m.storeNameInput.value = ""
	return m, nil
}

// advance validates the current screen and moves to the next.
func (m wizardModel) advance() (tea.Model, tea.Cmd) { //nolint:cyclop,gocognit
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
		m.infraResList = m.buildInfraResList()
		if len(m.infraResList) == 0 {
			m.screen = screenInfraResolution
		}

	case screenInfraResolution:
		// Selections stored in infraResList; nothing to validate.

	case screenEnvironments:
		m.data.EnvLocal = m.envItems[0].checked
		m.data.EnvStaging = m.envItems[1].checked
		m.data.EnvProduction = m.envItems[2].checked
		if !m.data.EnvLocal && !m.data.EnvStaging && !m.data.EnvProduction {
			m.err = "select at least one environment"
			return m, nil
		}
		m.infraResList = m.buildInfraResList()

	case screenDeployment:
		m.data.DeployProvider = m.deployItems[m.deployCursor].value

	case screenSecretStores:
		m.data.SecretStores = make([]secretStoreEntry, 0, len(m.storeEntries))
		for _, row := range m.storeEntries {
			m.data.SecretStores = append(m.data.SecretStores, secretStoreEntry{
				Name:      row.name,
				Provider:  row.provider,
				IsDefault: row.isDefault,
			})
			if row.isDefault {
				m.data.DefaultSecretStore = row.name
			}
		}
		if m.data.DefaultSecretStore == "" && len(m.storeEntries) > 0 {
			m.data.DefaultSecretStore = m.storeEntries[0].name
		}
		if len(m.storeEntries) > 0 {
			m.data.SecretsProvider = m.storeEntries[0].provider
		}
		secrets := m.inferRequiredSecrets()
		m.routingItems = m.buildRoutingRows(secrets)

	case screenSecretRouting:
		m.data.SecretRoutes = make(map[string]string, len(m.routingItems))
		for _, row := range m.routingItems {
			if row.cursor < len(row.storeItems) {
				m.data.SecretRoutes[row.secretName] = row.storeItems[row.cursor].value
			}
		}
		secrets := m.inferRequiredSecrets()
		m.bulkItems = m.buildBulkRows(secrets)
		m.bulkCursor = 0
		m.bulkInput = inputField{}

	case screenBulkSecrets:
		if m.bulkCursor < len(m.bulkItems) {
			m.bulkItems[m.bulkCursor].value = m.bulkInput.value
		}
		m.data.BulkSecrets = make(map[string]string, len(m.bulkItems))
		for _, row := range m.bulkItems {
			if row.value != "" {
				m.data.BulkSecrets[row.name] = row.value
			}
		}

	case screenCICD:
		m.data.GenerateCI = m.ciGenerate
		if m.ciGenerate {
			m.data.CIPlatform = m.ciPlatformItems[m.ciPlatformCursor].value
		}

	case screenReview:
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
	case screenInfraResolution:
		if len(m.infraResList) > 0 {
			m.infraResCursor = (m.infraResCursor + 1) % len(m.infraResList)
		}
	case screenEnvironments:
		m.envCursor = (m.envCursor + 1) % len(m.envItems)
	case screenDeployment:
		m.deployCursor = (m.deployCursor + 1) % len(m.deployItems)
	case screenSecretStores:
		m.storeCursor = (m.storeCursor + 1) % len(m.storeProviders)
	case screenSecretRouting:
		if len(m.routingItems) > 0 {
			m.routingCursor = (m.routingCursor + 1) % len(m.routingItems)
		}
	case screenCICD:
		if m.ciGenerate {
			m.ciPlatformCursor = (m.ciPlatformCursor + 1) % len(m.ciPlatformItems)
		}
	}
	return m
}

// handleScreenKey routes key events to the focused field on the current screen.
func (m wizardModel) handleScreenKey(msg tea.KeyPressMsg) wizardModel { //nolint:cyclop,gocognit
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

	case screenInfraResolution:
		if len(m.infraResList) == 0 {
			break
		}
		row := &m.infraResList[m.infraResCursor]
		if row.showConn {
			switch msg.Code {
			case tea.KeyBackspace:
				row.connInput.deleteBack()
			default:
				if msg.Text != "" {
					for _, r := range msg.Text {
						row.connInput.insertChar(r)
					}
				}
			}
		} else {
			switch msg.Code {
			case tea.KeyUp:
				if m.infraResCursor > 0 {
					m.infraResCursor--
				}
			case tea.KeyDown:
				if m.infraResCursor < len(m.infraResList)-1 {
					m.infraResCursor++
				}
			case tea.KeyLeft:
				if row.cursor > 0 {
					row.cursor--
					row.showConn = false
				}
			case tea.KeyRight:
				if row.cursor < len(row.strategies)-1 {
					row.cursor++
					row.showConn = row.strategies[row.cursor].value == "existing"
				}
			}
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

	case screenSecretStores:
		if m.storeAddMode {
			switch msg.Code {
			case tea.KeyBackspace:
				m.storeNameInput.deleteBack()
			default:
				if msg.Text != "" {
					for _, r := range msg.Text {
						m.storeNameInput.insertChar(r)
					}
				}
			}
		} else {
			switch msg.Code {
			case tea.KeyUp:
				if m.storeCursor > 0 {
					m.storeCursor--
				}
			case tea.KeyDown:
				if m.storeCursor < len(m.storeProviders)-1 {
					m.storeCursor++
				}
			case tea.KeySpace:
				m.storeAddMode = true
				m.storeNameInput = inputField{label: "Store name", placeholder: "primary"}
			case tea.KeyDelete:
				for i := len(m.storeEntries) - 1; i >= 0; i-- {
					if !m.storeEntries[i].isDefault {
						m.storeEntries = append(m.storeEntries[:i], m.storeEntries[i+1:]...)
						break
					}
				}
			}
		}

	case screenSecretRouting:
		if len(m.routingItems) == 0 {
			break
		}
		row := &m.routingItems[m.routingCursor]
		switch msg.Code {
		case tea.KeyUp:
			if m.routingCursor > 0 {
				m.routingCursor--
			}
		case tea.KeyDown:
			if m.routingCursor < len(m.routingItems)-1 {
				m.routingCursor++
			}
		case tea.KeyLeft:
			if row.cursor > 0 {
				row.cursor--
			}
		case tea.KeyRight:
			if row.cursor < len(row.storeItems)-1 {
				row.cursor++
			}
		}

	case screenBulkSecrets:
		if len(m.bulkItems) == 0 {
			break
		}
		switch msg.Code {
		case tea.KeyUp:
			if m.bulkCursor > 0 {
				m.bulkItems[m.bulkCursor].value = m.bulkInput.value
				m.bulkCursor--
				m.bulkInput = inputField{value: m.bulkItems[m.bulkCursor].value}
			}
		case tea.KeyDown:
			if m.bulkCursor < len(m.bulkItems)-1 {
				m.bulkItems[m.bulkCursor].value = m.bulkInput.value
				m.bulkCursor++
				m.bulkInput = inputField{value: m.bulkItems[m.bulkCursor].value}
			}
		case tea.KeyBackspace:
			m.bulkInput.deleteBack()
		case tea.KeyCtrlG:
			m.bulkInput.value = generateSecretValue()
			if m.bulkCursor < len(m.bulkItems) {
				m.bulkItems[m.bulkCursor].autoGen = true
			}
		default:
			if msg.Text != "" {
				for _, r := range msg.Text {
					m.bulkInput.insertChar(r)
				}
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
	case screenInfraResolution:
		b.WriteString(m.viewInfraResolution())
	case screenEnvironments:
		b.WriteString(m.viewEnvironments())
	case screenDeployment:
		b.WriteString(m.viewDeployment())
	case screenSecretStores:
		b.WriteString(m.viewSecretStores())
	case screenSecretRouting:
		b.WriteString(m.viewSecretRouting())
	case screenBulkSecrets:
		b.WriteString(m.viewBulkSecrets())
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
	case screenInfraResolution:
		return hintStyle.Render("Enter: next  Esc: back  ↑↓: select resource  ←→: change strategy  Ctrl+C: quit")
	case screenBulkSecrets:
		return hintStyle.Render("Enter: next  ↑↓: move  type: set value  Ctrl+G: auto-generate  Ctrl+C: quit")
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
	b.WriteString(headerStyle.Render("1 / 11  Project Info") + "\n\n")
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
	b.WriteString(headerStyle.Render("2 / 11  Services") + "\n\n")
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
	b.WriteString(headerStyle.Render("3 / 11  Infrastructure") + "\n\n")
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

func (m wizardModel) viewInfraResolution() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("4 / 11  Infra Resolution (per environment)") + "\n\n")
	if len(m.infraResList) == 0 {
		b.WriteString(dimStyle.Render("No infrastructure selected — skipping.") + "\n")
		return b.String()
	}
	b.WriteString("Set how each infrastructure resource is resolved in each environment.\n")
	b.WriteString(dimStyle.Render("Use ← → to change strategy, ↑ ↓ to move between rows.") + "\n\n")

	for i, row := range m.infraResList {
		cursor := "  "
		if i == m.infraResCursor {
			cursor = activeStyle.Render("▶ ")
		}
		stratLabel := ""
		if row.cursor < len(row.strategies) {
			stratLabel = row.strategies[row.cursor].label
		}
		b.WriteString(fmt.Sprintf("%s%-12s  %-10s  %s\n", cursor, row.resource, row.env, activeStyle.Render(stratLabel)))
		if i == m.infraResCursor && row.showConn {
			conn := row.connInput.value
			if conn == "" {
				conn = dimStyle.Render(row.connInput.placeholder)
			}
			b.WriteString("         Connection: " + inputStyle.Render(conn+" ") + "\n")
		}
	}
	return b.String()
}

func (m wizardModel) viewEnvironments() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("5 / 11  Environments") + "\n\n")
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
	b.WriteString(headerStyle.Render("6 / 11  Deployment Provider") + "\n\n")
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

func (m wizardModel) viewSecretStores() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("7 / 11  Secret Stores") + "\n\n")
	b.WriteString("Define named secret stores for your application.\n")
	b.WriteString(dimStyle.Render("Space: add store  Delete: remove last  Enter: continue") + "\n\n")

	if len(m.storeEntries) > 0 {
		b.WriteString(headerStyle.Render("Configured stores:") + "\n")
		for _, row := range m.storeEntries {
			def := ""
			if row.isDefault {
				def = activeStyle.Render(" [default]")
			}
			b.WriteString(fmt.Sprintf("  %-12s  (%s)%s\n", row.name, row.provider, def))
		}
		b.WriteString("\n")
	}

	if m.storeAddMode {
		b.WriteString(headerStyle.Render("New store — provider:") + "\n")
		for i, item := range m.storeProviders {
			cursor := "  "
			if i == m.storeCursor {
				cursor = activeStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
		}
		b.WriteString("\n" + headerStyle.Render("Store name: "))
		display := m.storeNameInput.value
		if display == "" {
			display = dimStyle.Render(m.storeNameInput.placeholder)
		}
		b.WriteString(inputStyle.Render(display+" ") + "\n")
		b.WriteString(hintStyle.Render("Enter: confirm  Esc: cancel") + "\n")
	} else {
		b.WriteString(dimStyle.Render("Select provider to add (Space):") + "\n")
		for i, item := range m.storeProviders {
			cursor := "  "
			if i == m.storeCursor {
				cursor = activeStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
		}
	}
	return b.String()
}

func (m wizardModel) viewSecretRouting() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("8 / 11  Secret Routing") + "\n\n")
	b.WriteString("Assign each secret to a store. Use ← → to change the store.\n\n")

	if len(m.routingItems) == 0 {
		b.WriteString(dimStyle.Render("No secrets detected.") + "\n")
		return b.String()
	}

	for i, row := range m.routingItems {
		cursor := "  "
		if i == m.routingCursor {
			cursor = activeStyle.Render("▶ ")
		}
		storeLabel := dimStyle.Render("(default)")
		if row.cursor < len(row.storeItems) {
			storeLabel = activeStyle.Render(row.storeItems[row.cursor].label)
		}
		b.WriteString(fmt.Sprintf("%s%-20s  →  %s\n", cursor, row.secretName, storeLabel))
	}
	return b.String()
}

func (m wizardModel) viewBulkSecrets() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("9 / 11  Secret Values") + "\n\n")
	b.WriteString("Enter a value for each required secret.\n")
	b.WriteString(dimStyle.Render("Input is hidden. Ctrl+G to auto-generate a random value.") + "\n\n")

	for i, row := range m.bulkItems {
		cursor := "  "
		if i == m.bulkCursor {
			cursor = activeStyle.Render("▶ ")
		}
		var valueDisplay string
		if i == m.bulkCursor {
			masked := strings.Repeat("*", len(m.bulkInput.value))
			if m.bulkInput.value == "" {
				masked = dimStyle.Render("(empty)")
			}
			if m.bulkCursor < len(m.bulkItems) && m.bulkItems[m.bulkCursor].autoGen {
				masked = activeStyle.Render("(auto-generated)")
			}
			valueDisplay = inputStyle.Render(masked + " ")
		} else {
			if row.autoGen {
				valueDisplay = activeStyle.Render("(auto-generated)")
			} else if row.value != "" {
				valueDisplay = activeStyle.Render(strings.Repeat("*", len(row.value)))
			} else {
				valueDisplay = dimStyle.Render("(not set)")
			}
		}
		b.WriteString(fmt.Sprintf("%s%-20s  %s\n", cursor, row.name, valueDisplay))
	}
	return b.String()
}

func (m wizardModel) viewCICD() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("10 / 11  CI/CD") + "\n\n")

	genBox := checkboxOffStyle.Render("[ ]")
	if m.ciGenerate {
		genBox = checkboxOnStyle.Render("[x]")
	}
	b.WriteString(activeStyle.Render("▶ ") + genBox + " Generate CI bootstrap\n\n")

	if m.ciGenerate {
		b.WriteString(dimStyle.Render("  Platform:\n"))
		for i, item := range m.ciPlatformItems {
			cursor := "    "
			if i == m.ciPlatformCursor {
				cursor = activeStyle.Render("  ▶ ")
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, item.label))
		}
	}
	return b.String()
}

func (m wizardModel) viewReview() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("11 / 11  Review") + "\n\n")
	b.WriteString("The following will be written to " + activeStyle.Render("app.yaml") + ":\n\n")

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
	if len(m.data.BulkSecrets) > 0 {
		b.WriteString("  " + codeStyle.Render("wfctl secrets setup --env local") + "\n")
	}
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
		modules = append(modules, "- name: db\n  type: database.postgres\n  config:\n    dsn: \"${DATABASE_URL}\"")
	}
	if d.HasCache {
		modules = append(modules, "- name: cache\n  type: cache.redis\n  config:\n    addr: \"${REDIS_URL}\"")
	}
	if d.HasMQ {
		modules = append(modules, "- name: mq\n  type: messaging.nats\n  config:\n    url: \"${NATS_URL}\"")
	}
	modules = append(modules, "- name: server\n  type: http.server\n  config:\n    port: 8080")

	b.WriteString("modules:\n")
	for _, m := range modules {
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

	// Secret stores (multi-store config).
	if len(d.SecretStores) > 1 || (len(d.SecretStores) == 1 && d.SecretStores[0].Provider != "env") {
		b.WriteString("secretStores:\n")
		for _, store := range d.SecretStores {
			b.WriteString(fmt.Sprintf("  %s:\n", store.Name))
			b.WriteString(fmt.Sprintf("    provider: %s\n", store.Provider))
		}
		b.WriteString("\n")
	}

	// Secrets.
	if d.SecretsProvider != "" {
		b.WriteString("secrets:\n")
		if d.DefaultSecretStore != "" && d.DefaultSecretStore != "primary" {
			b.WriteString(fmt.Sprintf("  defaultStore: %s\n", d.DefaultSecretStore))
		}
		if d.SecretsProvider != "env" {
			b.WriteString(fmt.Sprintf("  provider: %s\n", d.SecretsProvider))
		}
		b.WriteString("  entries:\n")
		if d.HasDatabase {
			b.WriteString("    - name: DATABASE_URL\n")
			if store, ok := d.SecretRoutes["DATABASE_URL"]; ok && store != d.DefaultSecretStore {
				b.WriteString(fmt.Sprintf("      store: %s\n", store))
			}
		}
		if d.HasCache {
			b.WriteString("    - name: REDIS_URL\n")
			if store, ok := d.SecretRoutes["REDIS_URL"]; ok && store != d.DefaultSecretStore {
				b.WriteString(fmt.Sprintf("      store: %s\n", store))
			}
		}
		if d.HasMQ {
			b.WriteString("    - name: NATS_URL\n")
			if store, ok := d.SecretRoutes["NATS_URL"]; ok && store != d.DefaultSecretStore {
				b.WriteString(fmt.Sprintf("      store: %s\n", store))
			}
		}
		b.WriteString("    - name: JWT_SECRET\n")
		if store, ok := d.SecretRoutes["JWT_SECRET"]; ok && store != d.DefaultSecretStore {
			b.WriteString(fmt.Sprintf("      store: %s\n", store))
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

	if wm, ok := finalModel.(wizardModel); ok && wm.screen != screenDone {
		fmt.Fprintf(os.Stderr, "wizard cancelled — no files written\n")
	}
	return nil
}
