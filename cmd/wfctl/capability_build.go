package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/recommend"
)

// buildSelection is the pure, tea-free selection state for `wfctl capability build`.
// It is separated from the Bubbletea model (buildModel, Task 5) so the selection
// logic is unit-testable without a terminal.
type buildSelection struct {
	inv    *inventory.Inventory
	chosen map[string]bool // capability IDs
}

func newBuildSelection(inv *inventory.Inventory) *buildSelection {
	return &buildSelection{inv: inv, chosen: map[string]bool{}}
}

// toggleCapability flips membership of id in the chosen set.
func (s *buildSelection) toggleCapability(id string) { s.chosen[id] = !s.chosen[id] }

// recommendation delegates to recommend.Recommend over the chosen capability set.
// An empty selection yields an empty recommendation (nothing requested), rather
// than the unfiltered inventory that recommend.Recommend returns for empty input.
func (s *buildSelection) recommendation() *recommend.Recommendation {
	var ids []string
	for id, on := range s.chosen {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return &recommend.Recommendation{}
	}
	return recommend.Recommend(s.inv, recommend.Options{Capabilities: ids})
}

// capabilities returns the sorted inventory capabilities presented on the
// selection screen. It is a thin view over the underlying inventory so the
// model does not reach into inventory internals.
func (s *buildSelection) capabilities() []inventory.Capability {
	caps := make([]inventory.Capability, len(s.inv.Capabilities))
	copy(caps, s.inv.Capabilities)
	sort.Slice(caps, func(i, j int) bool { return caps[i].ID < caps[j].ID })
	return caps
}

// isChosen reports whether id is currently selected.
func (s *buildSelection) isChosen(id string) bool { return s.chosen[id] }

// printRecommendation renders the recommendation markdown to w.
func (s *buildSelection) printRecommendation(w io.Writer) {
	renderRecommendMarkdown(w, s.recommendation())
}

// ── Bubbletea model ───────────────────────────────────────────────────────────

// buildModel is the Bubbletea wrapper around buildSelection for
// `wfctl capability build`. It mirrors wizardModel minimally: a screen enum,
// a cursor over the capability list, terminal dimensions, and a cancelled flag.
type buildModel struct {
	selection *buildSelection
	screen    screenID
	cursor    int // index into selection.capabilities() on screenCapability
	cancelled bool
	width     int
	height    int
	err       string
}

// newBuildModel constructs a buildModel rooted at the capability-selection screen.
func newBuildModel(sel *buildSelection) buildModel {
	return buildModel{selection: sel, screen: screenCapability}
}

// Init implements tea.Model.
func (m buildModel) Init() tea.Cmd { return nil }

// Update implements tea.Model. It mirrors wizard.go's Update structure:
// WindowSizeMsg sizes the view; ctrl+c cancels; Esc steps back or cancels;
// Enter advances through the screens; j/k/Up/Down move the list cursor; Space
// toggles the focused capability.
func (m buildModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			return m, tea.Quit
		}
		switch msg.Code {
		case tea.KeyEscape:
			// Explicit per-screen back-navigation: the build flow's screen enum
			// uses a distinct value range (screenCapability=100, screenReview=10),
			// so numeric screen--/comparisons would be wrong. Only review goes
			// back (to capability); capability cancels.
			if m.screen == screenReview {
				m.screen = screenCapability
				m.err = ""
			} else {
				m.cancelled = true
				return m, tea.Quit
			}
		case tea.KeyEnter:
			switch m.screen {
			case screenCapability:
				m.screen = screenReview
			case screenReview:
				m.screen = screenDone
				return m, tea.Quit
			}
		case tea.KeyUp, 'k':
			if m.screen == screenCapability && m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown, 'j':
			if m.screen == screenCapability {
				if n := len(m.selection.capabilities()); m.cursor < n-1 {
					m.cursor++
				}
			}
		case ' ':
			if m.screen == screenCapability {
				caps := m.selection.capabilities()
				if m.cursor >= 0 && m.cursor < len(caps) {
					m.selection.toggleCapability(caps[m.cursor].ID)
				}
			}
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m buildModel) View() tea.View {
	return tea.NewView(m.render())
}

// render produces the string for the current screen.
func (m buildModel) render() string {
	switch m.screen {
	case screenCapability:
		return m.viewCapability()
	case screenReview:
		return m.viewReview()
	case screenDone:
		return titleStyle.Render("Build complete.") + "\n"
	default:
		return ""
	}
}

// viewCapability renders the capability checkbox list.
func (m buildModel) viewCapability() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("wfctl capability build"))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("Space: toggle  Enter: review  Esc: back  ctrl+c: cancel"))
	b.WriteString("\n\n")
	caps := m.selection.capabilities()
	for i := range caps {
		cap := &caps[i]
		marker := " "
		if m.selection.isChosen(cap.ID) {
			marker = "x"
		}
		line := fmt.Sprintf("[%s] %s — %s", marker, cap.ID, cap.Name)
		if i == m.cursor {
			b.WriteString(activeStyle.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// viewReview renders the recommendation preview.
func (m buildModel) viewReview() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Review recommendation"))
	b.WriteString("\n\n")
	var preview strings.Builder
	m.selection.printRecommendation(&preview)
	b.WriteString(dimStyle.Render(preview.String()))
	b.WriteString(hintStyle.Render("Enter: confirm & emit  Esc: back  ctrl+c: cancel"))
	b.WriteString("\n")
	return b.String()
}

// err holds a transient error message surfaced in the view; a field on the
// model so Update can clear it on navigation.

// ── CLI entrypoint ────────────────────────────────────────────────────────────

// runCapabilityBuild implements `wfctl capability build` — an interactive
// Bubbletea wizard that walks the operator from capability selection to a
// recommendation, mirroring runWizard.
func runCapabilityBuild(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("capability build", flag.ContinueOnError)
	fs.SetOutput(out)
	var registryDir, repoRoot, taxonomyPath string
	fs.StringVar(&registryDir, "registry", defaultCapabilityRegistryPath(), "registry directory")
	fs.StringVar(&repoRoot, "repo-root", "..", "workspace root containing workflow-plugin-* repos")
	fs.StringVar(&taxonomyPath, "taxonomy", defaultCapabilityTaxonomyPath(), "capability taxonomy YAML")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inv, err := inventory.CollectEcosystem(inventory.EcosystemOptions{
		RegistryDir:     registryDir,
		RepoRoot:        repoRoot,
		TaxonomyPath:    taxonomyPath,
		GeneratedAt:     time.Now().UTC(),
		WorkflowVersion: version,
	})
	if err != nil {
		return err
	}
	// Render the TUI to stderr so a redirected stdout (e.g. `> rec.md`) stays
	// clean for the emitted recommendation.
	p := tea.NewProgram(newBuildModel(newBuildSelection(inv)), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("capability build: %w", err)
	}
	m, ok := final.(buildModel)
	if !ok || m.cancelled || m.screen != screenDone {
		fmt.Fprintln(os.Stderr, "build cancelled — nothing emitted")
		return nil
	}
	m.selection.printRecommendation(out)
	return nil
}
