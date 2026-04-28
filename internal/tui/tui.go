// Package tui implements the Yaad terminal UI using Bubbletea.
// Screens: Dashboard → Search → Node Detail
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/GrayCodeAI/yaad/internal/engine"
	intentpkg "github.com/GrayCodeAI/yaad/internal/intent"
	"github.com/GrayCodeAI/yaad/internal/storage"
)

// Screen constants
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenSearch
	ScreenDetail
)

// Styles
var (
	purple    = lipgloss.Color("#a78bfa")
	green     = lipgloss.Color("#68d391")
	red       = lipgloss.Color("#fc8181")
	yellow    = lipgloss.Color("#f6ad55")
	blue      = lipgloss.Color("#63b3ed")
	gray      = lipgloss.Color("#718096")
	darkGray  = lipgloss.Color("#2d3748")
	white     = lipgloss.Color("#e2e8f0")

	titleStyle = lipgloss.NewStyle().Foreground(purple).Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(gray)
	nodeStyle  = lipgloss.NewStyle().Foreground(white)
	selectedStyle = lipgloss.NewStyle().Background(darkGray).Foreground(white)
	borderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).Padding(0, 1)

	typeColors = map[string]lipgloss.Color{
		"convention": green, "decision": blue, "bug": red,
		"spec": yellow, "task": purple, "skill": lipgloss.Color("#f687b3"),
		"preference": lipgloss.Color("#76e4f7"), "session": gray,
		"file": gray, "entity": darkGray,
	}
)

// Model is the TUI state.
type Model struct {
	screen   Screen
	eng      *engine.Engine
	width    int
	height   int

	// Dashboard
	stats    *engine.Status
	hotNodes []*storage.Node

	// Search
	input    textinput.Model
	results  []*storage.Node
	cursor   int
	searched bool

	// Detail
	detail   *storage.Node
	edges    []*storage.Edge
	scroll   int

	err error
}

// New creates a new TUI model.
func New(eng *engine.Engine) Model {
	ti := textinput.New()
	ti.Placeholder = "Search memories..."
	ti.CharLimit = 100
	ti.Width = 50

	return Model{eng: eng, input: ti}
}

// Init loads initial data.
func (m Model) Init() tea.Cmd {
	return loadDashboard(m.eng)
}

// --- Messages ---

type dashboardMsg struct {
	stats    *engine.Status
	hotNodes []*storage.Node
}
type searchMsg struct{ nodes []*storage.Node }
type errMsg struct{ err error }

func loadDashboard(eng *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		stats, _ := eng.Status("")
		ctx, _ := eng.Context("")
		return dashboardMsg{stats: stats, hotNodes: ctx.Nodes}
	}
}

func doSearch(eng *engine.Engine, query string) tea.Cmd {
	return func() tea.Msg {
		result, err := eng.Recall(engine.RecallOpts{Query: query, Depth: 2, Limit: 20})
		if err != nil {
			return errMsg{err}
		}
		return searchMsg{result.Nodes}
	}
}

// Update handles messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case dashboardMsg:
		m.stats = msg.stats
		m.hotNodes = msg.hotNodes

	case searchMsg:
		m.results = msg.nodes
		m.cursor = 0
		m.searched = true

	case errMsg:
		m.err = msg.err

	case tea.KeyMsg:
		switch m.screen {
		case ScreenDashboard:
			return m.updateDashboard(msg)
		case ScreenSearch:
			return m.updateSearch(msg)
		case ScreenDetail:
			return m.updateDetail(msg)
		}
	}

	if m.screen == ScreenSearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/", "s":
		m.screen = ScreenSearch
		m.input.Focus()
		return m, textinput.Blink
	case "r":
		return m, loadDashboard(m.eng)
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = ScreenDashboard
		m.input.Blur()
		return m, nil
	case "enter":
		// If input has text, search; if results exist, open detail
		if q := m.input.Value(); q != "" && !m.searched {
			return m, doSearch(m.eng, q)
		}
		if len(m.results) > 0 {
			m.detail = m.results[m.cursor]
			edges, _ := m.eng.Store().GetEdgesFrom(m.detail.ID)
			m.edges = edges
			m.scroll = 0
			m.screen = ScreenDetail
			return m, nil
		}
		if q := m.input.Value(); q != "" {
			m.searched = false
			return m, doSearch(m.eng, q)
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = ScreenSearch
		return m, nil
	case "up", "k":
		if m.scroll > 0 {
			m.scroll--
		}
	case "down", "j":
		m.scroll++
	}
	return m, nil
}

// View renders the current screen.
func (m Model) View() string {
	switch m.screen {
	case ScreenDashboard:
		return m.viewDashboard()
	case ScreenSearch:
		return m.viewSearch()
	case ScreenDetail:
		return m.viewDetail()
	}
	return ""
}

func (m Model) viewDashboard() string {
	var sb strings.Builder

	// Header
	sb.WriteString(titleStyle.Render("याद Yaad") + "  " + dimStyle.Render("Memory for Coding Agents") + "\n\n")

	// Stats
	if m.stats != nil {
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n\n",
			lipgloss.NewStyle().Foreground(purple).Render(fmt.Sprintf("⬡ %d nodes", m.stats.Nodes)),
			lipgloss.NewStyle().Foreground(blue).Render(fmt.Sprintf("⟶ %d edges", m.stats.Edges)),
			lipgloss.NewStyle().Foreground(green).Render(fmt.Sprintf("◎ %d sessions", m.stats.Sessions)),
		))
	}

	// Hot tier nodes
	if len(m.hotNodes) > 0 {
		sb.WriteString(dimStyle.Render("  Active Context") + "\n")
		limit := 8
		if len(m.hotNodes) < limit {
			limit = len(m.hotNodes)
		}
		for _, n := range m.hotNodes[:limit] {
			color := typeColors[n.Type]
			if color == "" {
				color = gray
			}
			tag := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("[%s]", n.Type))
			content := truncate(n.Content, m.width-20)
			sb.WriteString(fmt.Sprintf("  %s %s\n", tag, nodeStyle.Render(content)))
		}
	} else {
		sb.WriteString(dimStyle.Render("  No memories yet. Run: yaad remember \"...\"") + "\n")
	}

	// Help
	sb.WriteString("\n" + dimStyle.Render("  / search  r refresh  q quit"))
	return sb.String()
}

func (m Model) viewSearch() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Search") + "\n\n")
	sb.WriteString("  " + m.input.View() + "\n")

	// Show detected intent
	if q := m.input.Value(); q != "" {
		i := intentpkg.Classify(q)
		intentColor := map[intentpkg.Intent]lipgloss.Color{
			intentpkg.IntentWhy:     red,
			intentpkg.IntentWhen:    blue,
			intentpkg.IntentWho:     yellow,
			intentpkg.IntentHow:     purple,
			intentpkg.IntentWhat:    green,
			intentpkg.IntentGeneral: gray,
		}[i]
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(intentColor).Render(
			fmt.Sprintf("Intent: %s", i.String()),
		) + "\n")
	}
	sb.WriteString("\n")

	if m.err != nil {
		sb.WriteString(lipgloss.NewStyle().Foreground(red).Render("  Error: "+m.err.Error()) + "\n")
	} else if m.searched && len(m.results) == 0 {
		sb.WriteString(dimStyle.Render("  No results found.") + "\n")
	}

	for i, n := range m.results {
		color := typeColors[n.Type]
		if color == "" {
			color = gray
		}
		tag := lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("[%s]", n.Type))
		conf := dimStyle.Render(fmt.Sprintf("%.0f%%", n.Confidence*100))
		content := truncate(n.Content, m.width-25)
		line := fmt.Sprintf("  %s %s %s", tag, conf, content)
		if i == m.cursor {
			sb.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			sb.WriteString(nodeStyle.Render(line) + "\n")
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  ↑↓ navigate  enter select/open  esc back"))
	return sb.String()
}

func (m Model) viewDetail() string {
	if m.detail == nil {
		return ""
	}
	var sb strings.Builder

	color := typeColors[m.detail.Type]
	if color == "" {
		color = gray
	}
	sb.WriteString(titleStyle.Render("Node Detail") + "\n\n")
	sb.WriteString(fmt.Sprintf("  %s  %s\n",
		lipgloss.NewStyle().Foreground(color).Bold(true).Render(m.detail.Type),
		dimStyle.Render("id: "+m.detail.ID[:8]),
	))
	sb.WriteString(fmt.Sprintf("  confidence: %.0f%%  tier: %d  access: %d\n\n",
		m.detail.Confidence*100, m.detail.Tier, m.detail.AccessCount))

	// Content (scrollable)
	lines := strings.Split(m.detail.Content, "\n")
	start := m.scroll
	if start >= len(lines) {
		start = 0
	}
	end := start + (m.height - 12)
	if end > len(lines) {
		end = len(lines)
	}
	for _, line := range lines[start:end] {
		sb.WriteString("  " + nodeStyle.Render(line) + "\n")
	}

	// Edges
	if len(m.edges) > 0 {
		sb.WriteString("\n" + dimStyle.Render("  Edges") + "\n")
		for _, e := range m.edges {
			sb.WriteString(fmt.Sprintf("  %s → %s\n",
				lipgloss.NewStyle().Foreground(blue).Render(e.Type),
				dimStyle.Render(e.ToID[:8]),
			))
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  ↑↓ scroll  b/esc back  q quit"))
	return sb.String()
}

// Run starts the TUI.
func Run(eng *engine.Engine) error {
	p := tea.NewProgram(New(eng), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if n <= 0 {
		n = 60
	}
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
