package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arturgoms/tnt/internal/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type layoutEntry struct {
	name string
	desc string
	path string
}

type layoutModel struct {
	entries   []layoutEntry
	cursor    int
	theme     *theme.Theme
	workdir   string
	session   string
	branch    string
	embedded  bool
	wantsBack bool
	quitting  bool
	selected  *layoutEntry
	width     int
	height    int
}

func scanLayouts(layoutsDir string) []layoutEntry {
	pattern := filepath.Join(layoutsDir, "*.sh")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	var entries []layoutEntry
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".sh")
		desc := extractLayoutDesc(path)
		entries = append(entries, layoutEntry{name: name, desc: desc, path: path})
	}
	return entries
}

func extractLayoutDesc(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum == 2 {
			line := scanner.Text()
			if after, ok := strings.CutPrefix(line, "# Layout: "); ok {
				if idx := strings.Index(after, " — "); idx >= 0 {
					return after[idx+len(" — "):]
				}
				if idx := strings.Index(after, " - "); idx >= 0 {
					return after[idx+3:]
				}
			}
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func newLayoutModel(t *theme.Theme, layoutsDir, workdir, session, branch string) layoutModel {
	return layoutModel{
		entries: scanLayouts(layoutsDir),
		theme:   t,
		workdir: workdir,
		session: session,
		branch:  branch,
	}
}

func (m layoutModel) Init() tea.Cmd {
	return nil
}

func (m layoutModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m layoutModel) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if m.cursor >= 0 && m.cursor < len(m.entries) {
			e := m.entries[m.cursor]
			m.selected = &e
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

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

func (m layoutModel) View() string {
	if m.quitting {
		return ""
	}
	return m.viewList()
}

func (m layoutModel) viewList() string {
	t := m.theme

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Blue).
		Padding(0, 1).
		Render(fmt.Sprintf("layouts [%d]", len(m.entries)))

	var lines []string
	for i, e := range m.entries {
		isCursor := i == m.cursor

		icon := lipgloss.NewStyle().Foreground(t.Cyan).Render("▸")
		name := lipgloss.NewStyle().Foreground(t.FG).Bold(true).Width(12).Render(e.name)
		desc := lipgloss.NewStyle().Foreground(t.Gray).Render(e.desc)

		line := fmt.Sprintf("    %s %s %s", icon, name, desc)

		if isCursor {
			pointer := lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			name = lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Width(12).Render(e.name)
			desc = lipgloss.NewStyle().Foreground(t.Blue).Render(e.desc)
			line = fmt.Sprintf("  %s%s %s %s", pointer, icon, name, desc)
		}

		lines = append(lines, line)
	}

	if len(m.entries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("No layouts found."))
	}

	content := title + "\n\n" + strings.Join(lines, "\n")

	help := lipgloss.NewStyle().
		Foreground(t.Gray).
		Padding(0, 2).
		Render("↵ create window  esc back")

	return content + "\n\n" + help
}

func handleLayoutDeferred(m layoutModel) {
	if m.selected == nil {
		return
	}
	exec.Command(m.selected.path, m.workdir, m.session, m.branch).Run()
	exec.Command("tmux", "switch-client", "-t", m.session).Run()
}

func runLayoutPicker() {
	cfg := app.Config
	t := app.Theme

	ctx := detectTmuxContext()
	workdir := ctx.workdir
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		gitRoot := strings.TrimSpace(string(out))
		workdir = ctx.resolveWorkdir(gitRoot)
	}

	m := newLayoutModel(t, cfg.Paths.Layouts, workdir, ctx.session, ctx.branch)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	final := result.(layoutModel)
	handleLayoutDeferred(final)
}
