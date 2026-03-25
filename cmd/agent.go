package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/arturgomes/tnt/internal/agents"
	"github.com/arturgomes/tnt/internal/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type agentModel struct {
	agents    []agents.Agent
	cursor    int
	theme     *theme.Theme
	embedded  bool
	wantsBack bool
	quitting  bool
	jumpTo    string
	width     int
	height    int
}

func newAgentModel(t *theme.Theme) agentModel {
	return agentModel{theme: t}
}

func newAgentModelWithList(t *theme.Theme, list []agents.Agent) agentModel {
	return agentModel{theme: t, agents: list}
}

type agentRefreshMsg struct {
	list []agents.Agent
}

func loadAgentRefreshCmd() tea.Msg {
	return agentRefreshMsg{list: agents.Detect("")}
}

func (m *agentModel) reload() {
	m.agents = agents.Detect("")
}

func (m agentModel) Init() tea.Cmd {
	if len(m.agents) == 0 {
		return loadAgentRefreshCmd
	}
	return nil
}

func (m agentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case agentRefreshMsg:
		m.agents = msg.list
		if m.cursor >= len(m.agents) {
			m.cursor = len(m.agents) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m agentModel) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if m.cursor < len(m.agents)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if m.cursor >= 0 && m.cursor < len(m.agents) {
			m.jumpTo = m.agents[m.cursor].Target
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		return m, loadAgentRefreshCmd

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.embedded {
			m.wantsBack = true
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// --- view ---

func (m agentModel) View() string {
	if m.quitting {
		return ""
	}
	return m.viewList()
}

func (m agentModel) viewList() string {
	t := m.theme

	running, waiting, idle := agents.CountByStatus(m.agents)
	var counts []string
	if running > 0 {
		counts = append(counts, fmt.Sprintf("%d running", running))
	}
	if waiting > 0 {
		counts = append(counts, fmt.Sprintf("%d waiting", waiting))
	}
	if idle > 0 {
		counts = append(counts, fmt.Sprintf("%d idle", idle))
	}
	summary := strings.Join(counts, ", ")
	if summary == "" {
		summary = "none"
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(0, 1).
		Render(fmt.Sprintf("agents (%s)", summary))

	maxH := m.height - 4
	if maxH < 1 {
		maxH = 1
	}

	visStart := 0
	if m.cursor >= maxH {
		visStart = m.cursor - maxH + 1
	}

	var lines []string
	for i, a := range m.agents {
		if i < visStart {
			continue
		}
		if len(lines) >= maxH {
			break
		}

		isCursor := i == m.cursor

		var icon string
		var iconStyle lipgloss.Style
		switch a.Status {
		case agents.StatusRunning:
			icon = "◑"
			iconStyle = lipgloss.NewStyle().Foreground(t.Green)
		case agents.StatusWaiting:
			icon = "●"
			iconStyle = lipgloss.NewStyle().Foreground(t.Yellow)
		case agents.StatusIdle:
			icon = "○"
			iconStyle = lipgloss.NewStyle().Foreground(t.Gray)
		}

		statusLabel := lipgloss.NewStyle().Foreground(t.Gray).Width(8).Render(string(a.Status))
		switch a.Status {
		case agents.StatusRunning:
			statusLabel = lipgloss.NewStyle().Foreground(t.Green).Width(8).Render("running")
		case agents.StatusWaiting:
			statusLabel = lipgloss.NewStyle().Foreground(t.Yellow).Width(8).Render("waiting")
		}

		branch := a.Branch
		if len(branch) > 25 {
			branch = branch[:22] + "..."
		}
		branchStr := lipgloss.NewStyle().Foreground(t.FG).Width(25).Render(branch)

		sessionWin := fmt.Sprintf("%s/%s", a.Session, a.WindowName)
		if len(sessionWin) > 25 {
			sessionWin = sessionWin[:22] + "..."
		}
		sessionStr := lipgloss.NewStyle().Foreground(t.Purple).Render(sessionWin)

		line := fmt.Sprintf("    %s %s %s  %s", iconStyle.Render(icon), statusLabel, branchStr, sessionStr)

		if isCursor {
			pointer := lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			line = fmt.Sprintf("  %s%s %s %s  %s",
				pointer,
				iconStyle.Render(icon),
				statusLabel,
				lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Width(25).Render(branch),
				lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render(sessionWin),
			)
		}

		lines = append(lines, line)
	}

	if len(m.agents) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("No agents running."))
	}

	content := title + "\n\n" + strings.Join(lines, "\n")

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(0, 2).
		Render("↵ jump  r refresh  esc back")

	return content + "\n\n" + help
}

// --- deferred actions ---

func handleAgentDeferred(m agentModel) {
	if m.jumpTo == "" {
		return
	}
	exec.Command("tmux", "switch-client", "-t", m.jumpTo).Run()
}

func runAgentJump() {
	detected := agents.Detect("")
	for _, a := range detected {
		if a.Status == agents.StatusWaiting {
			exec.Command("tmux", "switch-client", "-t", a.Target).Run()
			exec.Command("tmux", "select-pane", "-t", a.Target).Run()
			return
		}
	}
	exec.Command("tmux", "display-message", "No agents waiting for input").Run()
}

func runAgentCycle() {
	current := ""
	if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
		current = strings.TrimSpace(string(out))
	}

	detected := agents.Detect("")
	seen := map[string]bool{}
	var activeSessions []string
	for _, a := range detected {
		if a.Status == agents.StatusRunning || a.Status == agents.StatusWaiting {
			if !seen[a.Session] {
				seen[a.Session] = true
				activeSessions = append(activeSessions, a.Session)
			}
		}
	}

	if len(activeSessions) == 0 {
		exec.Command("tmux", "display-message", "No active agents").Run()
		return
	}

	nextIdx := 0
	for i, s := range activeSessions {
		if s == current {
			nextIdx = (i + 1) % len(activeSessions)
			break
		}
	}

	next := activeSessions[nextIdx]
	if next == current && len(activeSessions) == 1 {
		exec.Command("tmux", "display-message", fmt.Sprintf("Only 1 active agent (%s)", next)).Run()
		return
	}
	exec.Command("tmux", "switch-client", "-t", next).Run()
}

func runAgentRoster() {
	t := app.Theme
	m := newAgentModel(t)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	final := result.(agentModel)
	handleAgentDeferred(final)
}
