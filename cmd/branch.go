package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arturgomes/tnt/internal/theme"
	"github.com/arturgomes/tnt/internal/worktree"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type branchState int

const (
	branchList branchState = iota
	branchCreate
)

type branchModel struct {
	ctx        worktree.RepoContext
	entries    []worktree.Entry
	cursor     int
	state      branchState
	theme      *theme.Theme
	input      textinput.Model
	embedded   bool
	wantsBack  bool
	quitting   bool
	action     string
	actionArg  string
	width      int
	height     int
	filterText string
	filtering  bool
}

type branchEntriesMsg struct {
	entries []worktree.Entry
}

func loadBranchEntriesCmd(ctx worktree.RepoContext) tea.Cmd {
	return func() tea.Msg {
		return branchEntriesMsg{entries: worktree.ListEntries(ctx)}
	}
}

func newBranchModel(t *theme.Theme, ctx worktree.RepoContext) branchModel {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 50

	m := branchModel{
		ctx:   ctx,
		theme: t,
		input: ti,
	}
	return m
}

func newBranchModelWithEntries(t *theme.Theme, ctx worktree.RepoContext, entries []worktree.Entry) branchModel {
	m := newBranchModel(t, ctx)
	m.entries = entries
	return m
}

func (m branchModel) filteredEntries() []worktree.Entry {
	if !m.filtering && m.filterText == "" {
		return m.entries
	}
	ft := strings.ToLower(m.filterText)
	if ft == "" {
		return m.entries
	}
	var result []worktree.Entry
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Branch), ft) {
			result = append(result, e)
		}
	}
	return result
}

func (m branchModel) Init() tea.Cmd {
	if len(m.entries) == 0 {
		return loadBranchEntriesCmd(m.ctx)
	}
	return nil
}

func (m branchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 20
		if m.input.Width < 30 {
			m.input.Width = 30
		}
		return m, nil

	case branchEntriesMsg:
		m.entries = msg.entries
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case branchList:
			return m.updateList(msg)
		case branchCreate:
			return m.updateCreate(msg)
		}

	default:
		if m.state == branchCreate || m.filtering {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m branchModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.updateFilter(msg)
	}

	filtered := m.filteredEntries()

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if m.cursor < len(filtered)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if m.cursor < 0 || m.cursor >= len(filtered) {
			return m, nil
		}
		e := filtered[m.cursor]
		switch e.Kind {
		case worktree.KindJump, worktree.KindMain:
			m.action = "jump"
			m.actionArg = e.Branch
		case worktree.KindOpen:
			m.action = "open"
			m.actionArg = e.Branch
		case worktree.KindCheckout, worktree.KindLocal:
			m.action = "create"
			m.actionArg = e.Branch
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
		m.state = branchCreate
		m.input.SetValue("")
		m.input.Placeholder = "new branch name"
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		worktree.FetchAsync(m.ctx.GitRoot)
		return m, loadBranchEntriesCmd(m.ctx)

	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		m.filtering = true
		m.filterText = ""
		m.input.SetValue("")
		m.input.Placeholder = "filter branches..."
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.filterText != "" {
			m.filterText = ""
			return m, nil
		}
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

func (m branchModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.filtering = false
		m.filterText = ""
		m.cursor = 0
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		m.filtering = false
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterText = m.input.Value()
		if m.cursor >= len(m.filteredEntries()) {
			m.cursor = len(m.filteredEntries()) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, cmd
	}
}

func (m branchModel) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = branchList
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.state = branchList
			return m, nil
		}
		m.action = "create"
		m.actionArg = name
		m.quitting = true
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// --- view ---

func (m branchModel) View() string {
	if m.quitting {
		return ""
	}
	switch m.state {
	case branchCreate:
		return m.viewInput()
	}
	return m.viewList()
}

func (m branchModel) viewList() string {
	t := m.theme
	filtered := m.filteredEntries()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(0, 1).
		Render(fmt.Sprintf("branches (%s) [%d]", m.ctx.RepoName, len(filtered)))

	maxH := m.height - 4
	if maxH < 1 {
		maxH = 1
	}

	visStart := 0
	if m.cursor >= maxH {
		visStart = m.cursor - maxH + 1
	}

	var lines []string
	for i, e := range filtered {
		if i < visStart {
			continue
		}
		if len(lines) >= maxH {
			break
		}

		isCursor := i == m.cursor

		var kindIcon string
		var kindStyle lipgloss.Style
		switch e.Kind {
		case worktree.KindJump:
			kindIcon = "●"
			kindStyle = lipgloss.NewStyle().Foreground(t.Green)
		case worktree.KindOpen:
			kindIcon = "○"
			kindStyle = lipgloss.NewStyle().Foreground(t.Cyan)
		case worktree.KindMain:
			kindIcon = "◆"
			kindStyle = lipgloss.NewStyle().Foreground(t.Yellow)
		case worktree.KindCheckout:
			kindIcon = "↓"
			kindStyle = lipgloss.NewStyle().Foreground(t.Purple)
		case worktree.KindLocal:
			kindIcon = "·"
			kindStyle = lipgloss.NewStyle().Foreground(t.Gray)
		}

		label := e.Branch
		maxW := m.width - 16
		if maxW < 20 {
			maxW = 20
		}
		if len(label) > maxW {
			label = label[:maxW-3] + "..."
		}

		kindLabel := lipgloss.NewStyle().Foreground(t.Gray).Width(9).Render(e.Label())

		line := fmt.Sprintf("    %s %s %s",
			kindStyle.Render(kindIcon),
			kindLabel,
			lipgloss.NewStyle().Foreground(t.FG).Render(label))

		if isCursor {
			pointer := lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			line = fmt.Sprintf("  %s%s %s %s",
				pointer,
				kindStyle.Render(kindIcon),
				kindLabel,
				lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render(label))
		}

		lines = append(lines, line)
	}

	if len(filtered) == 0 {
		if len(m.entries) == 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("Loading branches..."))
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("No matching branches."))
		}
	}

	content := title + "\n\n" + strings.Join(lines, "\n")

	if m.filtering {
		filterLine := lipgloss.NewStyle().Foreground(t.Yellow).Render("/ ") + m.input.View()
		content += "\n\n" + filterLine
	} else if m.filterText != "" {
		filterLine := lipgloss.NewStyle().Foreground(t.Yellow).
			Render(fmt.Sprintf("filter: %s  (/ to change, esc to clear)", m.filterText))
		content += "\n\n  " + filterLine
	}

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(0, 2).
		Render("↵ open  n new branch  r fetch  / filter  esc back")

	return content + "\n\n" + help
}

func (m branchModel) viewInput() string {
	t := m.theme
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(1, 2).
		Render("New branch")

	input := lipgloss.NewStyle().
		Padding(0, 2).
		Render(m.input.View())

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(1, 2).
		Render("enter confirm  esc cancel")

	return header + "\n" + input + "\n" + help
}

// --- deferred actions ---

func handleBranchDeferred(m branchModel) {
	if m.action == "" {
		return
	}
	switch m.action {
	case "jump":
		isMain := false
		for _, e := range m.entries {
			if e.Branch == m.actionArg && e.Kind == worktree.KindMain {
				isMain = true
				break
			}
		}
		worktree.JumpToWorktree(m.ctx, m.actionArg, isMain)
	case "open":
		for _, e := range m.entries {
			if e.Branch == m.actionArg {
				worktree.OpenWorktreeWindow(m.ctx, e.Path, e.Branch)
				return
			}
		}
	case "create":
		wtPath, err := worktree.CreateWorktree(m.ctx, m.actionArg)
		if err != nil {
			return
		}
		worktree.OpenWorktreeWindow(m.ctx, wtPath, m.actionArg)
	}
}

func runBranchPicker() {
	cfg := app.Config
	t := app.Theme

	tmx := detectTmuxContext()

	gitRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "not in a git repository")
		os.Exit(1)
	}

	tntDir := filepath.Dir(cfg.Paths.Layouts)
	ctx := worktree.NewContext(strings.TrimSpace(string(gitRoot)), tmx.session, tntDir)
	worktree.FetchAsync(ctx.GitRoot)

	m := newBranchModel(t, ctx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	final := result.(branchModel)
	handleBranchDeferred(final)
}
