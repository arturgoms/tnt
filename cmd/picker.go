package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/recents"
	"github.com/arturgomes/tnt/internal/scanner"
	"github.com/arturgomes/tnt/internal/session"
	"github.com/arturgomes/tnt/internal/theme"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type repoItem struct {
	repo    scanner.Repo
	divider bool
}

func (r repoItem) Title() string {
	if r.divider {
		return "──────────"
	}
	badge := ""
	if r.repo.HasSession {
		badge = "  ●"
	}
	if r.repo.SavedWindows > 0 {
		badge += fmt.Sprintf("  [%d saved]", r.repo.SavedWindows)
	}
	return r.repo.Name + badge
}

func (r repoItem) Description() string {
	if r.divider {
		return ""
	}
	return r.repo.Group
}

func (r repoItem) FilterValue() string {
	if r.divider {
		return ""
	}
	return r.repo.Name
}

type pickerState int

const (
	stateBrowse pickerState = iota
	stateRestore
	stateDetail
)

type detailItem struct {
	name   string
	desc   string
	action string
	paneID string
}

func (d detailItem) Title() string       { return d.name }
func (d detailItem) Description() string { return d.desc }
func (d detailItem) FilterValue() string { return d.name }

type panePreviewMsg struct {
	content string
}

func capturePane(paneID string) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-e").Output()
		if err != nil {
			return panePreviewMsg{content: ""}
		}
		content := string(out)
		// Trim trailing empty lines (compare stripped version)
		lines := strings.Split(content, "\n")
		for len(lines) > 0 {
			plain := stripAnsi(lines[len(lines)-1])
			if strings.TrimSpace(plain) != "" {
				break
			}
			lines = lines[:len(lines)-1]
		}
		return panePreviewMsg{content: strings.Join(lines, "\n")}
	}
}

func stripAnsi(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

func visibleLen(s string) int {
	return len(stripAnsi(s))
}

func truncateAnsi(s string, maxW int) string {
	visible := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			if i < len(s) {
				i++
			}
		} else {
			visible++
			if visible > maxW {
				return s[:i] + "\x1b[0m"
			}
			i++
		}
	}
	return s
}

type pickerModel struct {
	list       list.Model
	detailList list.Model
	detailRepo *scanner.Repo
	preview    string
	lastPane   string
	theme      *theme.Theme
	state      pickerState
	selected   *scanner.Repo
	action     string
	quitting   bool
	width      int
	height     int
}

type pickerKeys struct {
	Branch key.Binding
	Layout key.Binding
	New    key.Binding
	Delete key.Binding
}

var extraKeys = pickerKeys{
	Branch: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch")),
	Layout: key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "layout")),
	New:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
}

func newPicker(repos []scanner.Repo, t *theme.Theme, recentList *recents.List) pickerModel {
	var activeItems, inactiveItems []list.Item

	for _, r := range repos {
		if r.HasSession || recentList.Index(r.Name) >= 0 {
			activeItems = append(activeItems, repoItem{repo: r})
		} else {
			inactiveItems = append(inactiveItems, repoItem{repo: r})
		}
	}

	var items []list.Item
	items = append(items, activeItems...)
	if len(activeItems) > 0 && len(inactiveItems) > 0 {
		items = append(items, repoItem{divider: true})
	}
	items = append(items, inactiveItems...)

	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Blue)).Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(t.Blue)).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Gray)).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(t.Blue)).
		Padding(0, 0, 0, 1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.FG)).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Gray)).
		Padding(0, 0, 0, 2)
	delegate.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Gray)).
		Padding(0, 0, 0, 2)
	delegate.Styles.DimmedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Border)).
		Padding(0, 0, 0, 2)

	l := list.New(items, delegate, 0, 0)
	l.Title = "tnt"
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(t.Blue)).
		Padding(0, 1)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Yellow))
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Yellow))
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.KeyMap.NextPage.SetKeys("pgdown")
	l.KeyMap.PrevPage.SetKeys("pgup")
	l.KeyMap.GoToEnd.SetKeys("end")
	l.KeyMap.GoToStart.SetKeys("home")
	l.KeyMap.CursorUp.SetKeys("up")
	l.KeyMap.CursorDown.SetKeys("down")
	l.KeyMap.Quit.SetKeys("ctrl+c")

	return pickerModel{
		list:  l,
		theme: t,
		state: stateBrowse,
	}
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) selectedRepo() *scanner.Repo {
	item, ok := m.list.SelectedItem().(repoItem)
	if !ok || item.divider {
		return nil
	}
	r := item.repo
	return &r
}

func (m *pickerModel) openDetail(repo *scanner.Repo) {
	var items []list.Item

	if repo.HasSession {
		windows, err := exec.Command("tmux", "list-windows", "-t", repo.Name, "-F", "#{window_name}\t#{pane_current_path}\t#{pane_id}").Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(windows)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 3)
				name := parts[0]
				desc := ""
				paneID := ""
				if len(parts) > 1 {
					desc = filepath.Base(parts[1])
				}
				if len(parts) > 2 {
					paneID = parts[2]
				}
				items = append(items, detailItem{name: name, desc: desc, action: "window", paneID: paneID})
			}
		}
	} else {
		branches, err := exec.Command("git", "-C", repo.Path, "branch", "--format=%(refname:short)").Output()
		if err == nil {
			for _, b := range strings.Split(strings.TrimSpace(string(branches)), "\n") {
				if b == "" {
					continue
				}
				items = append(items, detailItem{name: b, desc: "", action: "branch"})
			}
		}
		remoteBranches, err := exec.Command("git", "-C", repo.Path, "branch", "-r", "--format=%(refname:short)").Output()
		if err == nil {
			for _, b := range strings.Split(strings.TrimSpace(string(remoteBranches)), "\n") {
				b = strings.TrimPrefix(b, "origin/")
				if b == "" || b == "HEAD" {
					continue
				}
				already := false
				for _, item := range items {
					if item.(detailItem).name == b {
						already = true
						break
					}
				}
				if !already {
					items = append(items, detailItem{name: b, desc: "remote", action: "branch"})
				}
			}
		}
	}

	title := repo.Name + " — windows"
	if !repo.HasSession {
		title = repo.Name + " — branches"
	}

	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 0, 0, 1)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.FG)).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 2)

	colW := m.width / 4
	if colW < 20 {
		colW = 20
	}
	dl := list.New(items, delegate, colW, m.height-2)
	dl.Title = title
	dl.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 1)
	dl.SetShowStatusBar(false)
	dl.SetShowHelp(false)
	dl.SetFilteringEnabled(false)
	dl.KeyMap.CursorUp.SetKeys("up")
	dl.KeyMap.CursorDown.SetKeys("down")
	dl.KeyMap.NextPage.SetKeys("pgdown")
	dl.KeyMap.PrevPage.SetKeys("pgup")
	dl.KeyMap.Quit.SetKeys("ctrl+c")

	m.list.SetSize(colW, m.height-2)

	dimDelegate := list.NewDefaultDelegate()
	dimDelegate.SetSpacing(0)
	dimDelegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 1)
	dimDelegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 1)
	dimDelegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 2)
	dimDelegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Padding(0, 0, 0, 2)
	m.list.SetDelegate(dimDelegate)

	m.detailList = dl
	m.detailRepo = repo
	m.preview = ""
	m.lastPane = ""
	m.state = stateDetail
}

func (m *pickerModel) captureSelectedPane() tea.Cmd {
	item, ok := m.detailList.SelectedItem().(detailItem)
	if !ok || item.paneID == "" {
		m.preview = ""
		m.lastPane = ""
		return nil
	}
	if item.paneID == m.lastPane {
		return nil
	}
	m.lastPane = item.paneID
	return capturePane(item.paneID)
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.state == stateDetail {
			colW := msg.Width / 4
			if colW < 20 {
				colW = 20
			}
			m.list.SetSize(colW, msg.Height-2)
			m.detailList.SetSize(colW, msg.Height-2)
		} else {
			m.list.SetSize(msg.Width, msg.Height-2)
		}
		return m, nil

	case panePreviewMsg:
		m.preview = msg.content
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch m.state {
		case stateBrowse:
			return m.updateBrowse(msg)
		case stateRestore:
			return m.updateRestore(msg)
		case stateDetail:
			return m.updateDetail(msg)
		}
	}

	var cmd tea.Cmd
	switch m.state {
	case stateDetail:
		m.detailList, cmd = m.detailList.Update(msg)
	default:
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m pickerModel) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.selected = repo
		if repo.HasSession {
			m.quitting = true
			return m, tea.Quit
		}
		if repo.SavedWindows > 0 {
			m.state = stateRestore
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, extraKeys.New):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		repo.SavedWindows = 0
		m.selected = repo
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, extraKeys.Branch):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.selected = repo
		m.action = "branch"
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, extraKeys.Layout):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.selected = repo
		m.action = "layout"
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, extraKeys.Delete):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		if repo.HasSession {
			exec.Command("tmux", "kill-session", "-t", repo.Name).Run()
		}
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("right"))):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.openDetail(repo)
		return m, m.captureSelectedPane()

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.list.FilterState() == list.FilterApplied {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("left", "esc"))):
		m.state = stateBrowse
		m.detailRepo = nil
		m.list.SetSize(m.width, m.height-2)
		restoreDelegate := list.NewDefaultDelegate()
		restoreDelegate.SetSpacing(0)
		restoreDelegate.Styles.SelectedTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color(m.theme.Blue)).
			Padding(0, 0, 0, 1)
		restoreDelegate.Styles.SelectedDesc = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color(m.theme.Blue)).
			Padding(0, 0, 0, 1)
		restoreDelegate.Styles.NormalTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.FG)).
			Padding(0, 0, 0, 2)
		restoreDelegate.Styles.NormalDesc = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 0, 0, 2)
		restoreDelegate.Styles.DimmedTitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 0, 0, 2)
		restoreDelegate.Styles.DimmedDesc = lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Padding(0, 0, 0, 2)
		m.list.SetDelegate(restoreDelegate)
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		item, ok := m.detailList.SelectedItem().(detailItem)
		if !ok {
			return m, nil
		}
		m.selected = m.detailRepo
		if item.action == "window" {
			m.action = "jump-window:" + item.name
		} else {
			m.action = "create-worktree:" + item.name
		}
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+c"))):
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.detailList, cmd = m.detailList.Update(msg)
	previewCmd := m.captureSelectedPane()
	if previewCmd != nil {
		return m, tea.Batch(cmd, previewCmd)
	}
	return m, cmd
}

func (m pickerModel) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.quitting = true
		return m, tea.Quit
	case "n":
		m.selected.SavedWindows = 0
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.state = stateBrowse
		m.selected = nil
		return m, nil
	}
	return m, nil
}

func (m pickerModel) View() string {
	if m.quitting {
		return ""
	}

	if m.state == stateRestore && m.selected != nil {
		prompt := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.theme.Yellow)).
			Render(fmt.Sprintf("\n  %s — %d windows saved\n", m.selected.Name, m.selected.SavedWindows))

		keys := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Render("  [r]estore  [n]ew session  [esc] cancel\n")

		return prompt + keys
	}

	if m.state == stateDetail {
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		left := m.list.View()
		middle := m.detailList.View()

		help := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 2).
			Render("↵ select  ← back  esc back")

		showPreview := m.width >= 80 && m.preview != "" && m.detailRepo != nil && m.detailRepo.HasSession
		if !showPreview {
			columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, middle)
			return columns + "\n" + help
		}

		colW := m.width / 4
		if colW < 20 {
			colW = 20
		}
		previewW := m.width - colW*2 - 4
		if previewW < 15 {
			previewW = 15
		}
		previewH := m.height - 4
		if previewH < 1 {
			previewH = 1
		}

		lines := strings.Split(m.preview, "\n")
		if len(lines) > previewH {
			lines = lines[:previewH]
		}
		for i, line := range lines {
			lines[i] = truncateAnsi(line, previewW)
		}

		previewTitle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.theme.Blue)).
			Padding(0, 1).
			Render("preview")

		right := previewTitle + "\n" + strings.Join(lines, "\n")

		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, middle, sep, right)
		return columns + "\n" + help
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 2).
		Render("↵ open  → details  b branch  l layout  n new  d delete  / filter  esc quit")

	return m.list.View() + "\n" + help
}

func runPicker() {
	cfg := app.Config
	t := app.Theme

	repos := scanner.Scan(cfg)
	recentList := recents.Load(cfg.Paths.State)

	m := newPicker(repos, t, recentList)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	final := result.(pickerModel)
	if final.selected == nil {
		return
	}

	repo := final.selected
	recentList.Add(repo.Name, cfg.Paths.State)

	switch {
	case final.action == "branch":
		if !repo.HasSession {
			createSession(repo.Name, repo.Path)
			loadLayout(cfg, repo.Name, repo.Path)
		}
		switchToSession(repo.Name)
		exec.Command("tmux", "neww", filepath.Join(cfg.Paths.Scripts, "tnt-branch")).Run()
		return

	case final.action == "layout":
		if !repo.HasSession {
			createSession(repo.Name, repo.Path)
		}
		switchToSession(repo.Name)
		exec.Command("tmux", "neww", fmt.Sprintf("%s %q %q",
			filepath.Join(cfg.Paths.Scripts, "tnt-layout-picker"),
			repo.Path, "main")).Run()
		return

	case strings.HasPrefix(final.action, "jump-window:"):
		windowName := strings.TrimPrefix(final.action, "jump-window:")
		switchToSession(repo.Name)
		exec.Command("tmux", "select-window", "-t", repo.Name+":"+windowName).Run()
		return

	case strings.HasPrefix(final.action, "create-worktree:"):
		branchName := strings.TrimPrefix(final.action, "create-worktree:")
		if !repo.HasSession {
			createSession(repo.Name, repo.Path)
		}
		switchToSession(repo.Name)
		layoutScript := filepath.Join(cfg.Paths.Layouts, cfg.Layout.Default+".sh")
		worktreeDir := filepath.Join(repo.Path, cfg.Branch.WorktreeDir, branchName)
		if _, err := os.Stat(worktreeDir); err != nil {
			exec.Command("git", "-C", repo.Path, "worktree", "add", worktreeDir, branchName).Run()
		}
		exec.Command(layoutScript, worktreeDir, repo.Name, branchName).Run()
		return
	}

	if repo.HasSession {
		switchToSession(repo.Name)
		return
	}

	createSession(repo.Name, repo.Path)

	if repo.SavedWindows > 0 {
		if err := session.Restore(cfg, repo.Name, repo.Path); err != nil {
			fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		}
		cleanupInitialWindow(repo.Name)
	} else {
		loadLayout(cfg, repo.Name, repo.Path)
	}

	switchToSession(repo.Name)
}

func createSession(name, path string) {
	exec.Command("tmux", "-u", "-2", "new-session", "-s", name, "-c", path, "-d").Run()
}

func switchToSession(name string) {
	if os.Getenv("TMUX") == "" {
		exec.Command("tmux", "-u", "-2", "attach-session", "-t="+name).Run()
	} else {
		exec.Command("tmux", "switch-client", "-t", name).Run()
	}
}

func loadLayout(cfg *config.Config, name, path string) {
	layout := cfg.Layout.Default

	cfgFile := filepath.Join(cfg.Paths.Projects, name, "config.json")
	if data, err := os.ReadFile(cfgFile); err == nil {
		if strings.Contains(string(data), `"default_layout"`) {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, `"default_layout"`) {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						v := strings.Trim(strings.TrimSpace(parts[1]), `",`)
						if v != "" {
							layout = v
						}
					}
				}
			}
		}
	}

	layoutScript := filepath.Join(cfg.Paths.Layouts, layout+".sh")
	if _, err := os.Stat(layoutScript); err != nil {
		return
	}

	branch := "main"
	if b, err := exec.Command("git", "-C", path, "branch", "--show-current").Output(); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			branch = s
		}
	}

	exec.Command(layoutScript, path, name, branch).Run()
	cleanupInitialWindow(name)
}

func cleanupInitialWindow(name string) {
	out, _ := exec.Command("tmux", "list-windows", "-t", name).Output()
	count := len(strings.Split(strings.TrimSpace(string(out)), "\n"))
	if count > 1 {
		exec.Command("tmux", "kill-window", "-t", name+":1").Run()
		exec.Command("tmux", "move-window", "-r", "-t", name).Run()
	}
}
