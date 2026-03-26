package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arturgomes/tnt/internal/agents"
	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/recents"
	"github.com/arturgomes/tnt/internal/scanner"
	"github.com/arturgomes/tnt/internal/session"
	"github.com/arturgomes/tnt/internal/theme"
	"github.com/arturgomes/tnt/internal/todos"
	"github.com/arturgomes/tnt/internal/worktree"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type repoItem struct {
	repo         scanner.Repo
	divider      bool
	dividerWidth int
}

func (r repoItem) Title() string {
	if r.divider {
		w := r.dividerWidth
		if w <= 0 {
			w = 40
		}
		return strings.Repeat("─", w)
	}
	if r.repo.HasSession {
		return r.repo.Name + "  ●"
	}
	return r.repo.Name
}

func (r repoItem) Description() string {
	if r.divider {
		return " "
	}
	if r.repo.HasSession {
		sessionType := "git"
		if r.repo.Lite {
			sessionType = "lite"
		}
		return sessionType + " · " + r.repo.Group
	}

	var parts []string
	if r.repo.SavedWindows > 0 {
		parts = append(parts, fmt.Sprintf("%d saved", r.repo.SavedWindows))
	}
	if r.repo.BranchCount > 1 {
		parts = append(parts, fmt.Sprintf("%d branches", r.repo.BranchCount))
	} else if r.repo.CurrentBranch != "" {
		parts = append(parts, r.repo.CurrentBranch)
	}
	if r.repo.LastActivity != "" {
		parts = append(parts, r.repo.LastActivity)
	}
	return strings.Join(parts, " · ")
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
	stateTodo
	stateAgent
	stateBranch
	stateLayout
	stateNewSession
	stateNewSessionGit
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

type tmuxContext struct {
	session  string
	worktree string
	workdir  string
	branch   string
}

func detectTmuxContext() tmuxContext {
	ctx := tmuxContext{}

	if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
		ctx.session = strings.TrimSpace(string(out))
	}

	if out, err := exec.Command("tmux", "display-message", "-p", "#{@worktree}").Output(); err == nil {
		ctx.worktree = strings.TrimSpace(string(out))
	}

	if out, err := exec.Command("tmux", "display-message", "-p", "#{pane_current_path}").Output(); err == nil {
		ctx.workdir = strings.TrimSpace(string(out))
	}

	if ctx.worktree != "" {
		ctx.branch = ctx.worktree
	} else if out, err := exec.Command("git", "-C", ctx.workdir, "branch", "--show-current").Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			ctx.branch = b
		}
	}
	if ctx.branch == "" {
		ctx.branch = "workspace"
	}

	return ctx
}

func (c tmuxContext) resolveWorkdir(repoPath string) string {
	if c.worktree != "" {
		wtPath := filepath.Join(repoPath, ".worktrees", c.worktree)
		if info, err := os.Stat(wtPath); err == nil && info.IsDir() {
			return wtPath
		}
	}
	return repoPath
}

type pickerModel struct {
	list            list.Model
	detailList      list.Model
	detailRepo      *scanner.Repo
	preview         string
	lastPane        string
	tmux            tmuxContext
	allRepos        []scanner.Repo
	recentList      *recents.List
	currentSession  string
	workspaceNames  []string
	workspace       string
	todoGroups      []todos.RepoGroup
	agentList       []agents.Agent
	todo            todoModel
	agent           agentModel
	branch          branchModel
	layout          layoutModel
	theme           *theme.Theme
	state           pickerState
	selected        *scanner.Repo
	action          string
	quitting        bool
	newSessionName  string
	newSessionInput textinput.Model
	width           int
	height          int
}

type pickerKeys struct {
	Branch    key.Binding
	Layout    key.Binding
	New       key.Binding
	Delete    key.Binding
	Todo      key.Binding
	Agents    key.Binding
	Workspace key.Binding
}

var extraKeys = pickerKeys{
	Branch:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch")),
	Layout:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "layout")),
	New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Todo:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todo")),
	Agents:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "agents")),
	Workspace: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workspace")),
}

func newPicker(repos []scanner.Repo, t *theme.Theme, recentList *recents.List) pickerModel {
	var activeRepos, inactiveRepos []scanner.Repo

	// Current session name — exclude from list so position 0 is the switch target
	currentSession, _ := exec.Command("tmux", "display-message", "-p", "#S").Output()
	current := strings.TrimSpace(string(currentSession))

	for _, r := range repos {
		if r.Name == current {
			continue
		}
		if r.HasSession {
			activeRepos = append(activeRepos, r)
		} else {
			inactiveRepos = append(inactiveRepos, r)
		}
	}

	// Sort active by recency (most recent first), unknown recents at end
	sort.SliceStable(activeRepos, func(i, j int) bool {
		ri := recentList.Index(activeRepos[i].Name)
		rj := recentList.Index(activeRepos[j].Name)
		if ri == -1 {
			ri = 9999
		}
		if rj == -1 {
			rj = 9999
		}
		return ri < rj
	})

	var items []list.Item
	for _, r := range activeRepos {
		items = append(items, repoItem{repo: r})
	}
	if len(activeRepos) > 0 && len(inactiveRepos) > 0 {
		items = append(items, repoItem{divider: true})
	}
	for _, r := range inactiveRepos {
		items = append(items, repoItem{repo: r})
	}

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

type agentsLoadedMsg struct {
	list []agents.Agent
}

func loadAgentsCmd() tea.Msg {
	return agentsLoadedMsg{list: agents.Detect("")}
}

func (m pickerModel) Init() tea.Cmd {
	return loadAgentsCmd
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
		} else if m.state == stateTodo {
			colW := msg.Width / 3
			if colW < 20 {
				colW = 20
			}
			m.list.SetSize(colW, msg.Height-2)
			m.todo.width = msg.Width - colW - 2
			m.todo.height = msg.Height - 2
		} else if m.state == stateAgent {
			colW := msg.Width / 3
			if colW < 20 {
				colW = 20
			}
			m.list.SetSize(colW, msg.Height-2)
			m.agent.width = msg.Width - colW - 2
			m.agent.height = msg.Height - 2
		} else if m.state == stateBranch {
			colW := msg.Width / 3
			if colW < 20 {
				colW = 20
			}
			m.list.SetSize(colW, msg.Height-2)
			m.branch.width = msg.Width - colW - 2
			m.branch.height = msg.Height - 2
		} else if m.state == stateLayout {
			colW := msg.Width / 3
			if colW < 20 {
				colW = 20
			}
			m.list.SetSize(colW, msg.Height-2)
			m.layout.width = msg.Width - colW - 2
			m.layout.height = msg.Height - 2
		} else if (len(m.todoGroups) > 0 || len(m.agentList) > 0) && msg.Width >= 60 {
			m.list.SetSize(msg.Width/4, msg.Height-2)
		} else {
			m.list.SetSize(msg.Width, msg.Height-2)
		}
		return m, nil

	case agentsLoadedMsg:
		m.agentList = msg.list
		if m.state == stateBrowse && len(m.agentList) > 0 && m.width >= 60 {
			m.list.SetSize(m.width/4, m.height-2)
		}
		return m, nil

	case agentRefreshMsg:
		m.agentList = msg.list
		m.agent.agents = msg.list
		if m.agent.cursor >= len(m.agent.agents) {
			m.agent.cursor = len(m.agent.agents) - 1
		}
		if m.agent.cursor < 0 {
			m.agent.cursor = 0
		}
		return m, nil

	case panePreviewMsg:
		m.preview = msg.content
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			m.list.SetSize(m.width, m.height-2)
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		if key.Matches(msg, key.NewBinding(key.WithKeys("tab"))) && m.state != stateRestore && m.state != stateDetail {
			switch m.state {
			case stateBrowse:
				return m.enterTodoState()
			case stateTodo:
				if m.todo.state != todoList {
					break
				}
				return m.enterAgentState()
			case stateAgent:
				return m.enterBrowseState()
			}
		}

		switch m.state {
		case stateBrowse:
			return m.updateBrowse(msg)
		case stateRestore:
			return m.updateRestore(msg)
		case stateDetail:
			return m.updateDetail(msg)
		case stateTodo:
			return m.updateTodo(msg)
		case stateAgent:
			return m.updateAgent(msg)
		case stateBranch:
			return m.updateBranch(msg)
		case stateLayout:
			return m.updateLayout(msg)
		case stateNewSession:
			return m.updateNewSession(msg)
		case stateNewSessionGit:
			return m.updateNewSessionGit(msg)
		}
	}

	var cmd tea.Cmd
	switch m.state {
	case stateDetail:
		m.detailList, cmd = m.detailList.Update(msg)
	case stateTodo:
		updated, c := m.todo.Update(msg)
		m.todo = updated.(todoModel)
		cmd = c
	case stateAgent:
		updated, c := m.agent.Update(msg)
		m.agent = updated.(agentModel)
		cmd = c
	case stateBranch:
		updated, c := m.branch.Update(msg)
		m.branch = updated.(branchModel)
		cmd = c
	case stateLayout:
		updated, c := m.layout.Update(msg)
		m.layout = updated.(layoutModel)
		cmd = c
	case stateNewSession:
		m.newSessionInput, cmd = m.newSessionInput.Update(msg)
	default:
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m pickerModel) nextSession() string {
	for _, name := range m.recentList.Repos {
		if name != m.currentSession {
			for _, r := range m.allRepos {
				if r.Name == name && r.HasSession {
					return name
				}
			}
		}
	}
	for _, r := range m.allRepos {
		if r.HasSession && r.Name != m.currentSession {
			return r.Name
		}
	}
	return ""
}

func (m pickerModel) rebuildListItems() []list.Item {
	var activeRepos, inactiveRepos []scanner.Repo
	for _, r := range m.allRepos {
		if r.Name == m.currentSession {
			continue
		}
		if r.HasSession {
			activeRepos = append(activeRepos, r)
		} else if m.workspace == "" || r.Workspace == m.workspace {
			inactiveRepos = append(inactiveRepos, r)
		}
	}

	sort.SliceStable(activeRepos, func(i, j int) bool {
		ri := m.recentList.Index(activeRepos[i].Name)
		rj := m.recentList.Index(activeRepos[j].Name)
		if ri == -1 {
			ri = 9999
		}
		if rj == -1 {
			rj = 9999
		}
		return ri < rj
	})

	var items []list.Item
	for _, r := range activeRepos {
		items = append(items, repoItem{repo: r})
	}
	if len(activeRepos) > 0 && len(inactiveRepos) > 0 {
		items = append(items, repoItem{divider: true, dividerWidth: m.width/4 - 4})
	}
	for _, r := range inactiveRepos {
		items = append(items, repoItem{repo: r})
	}
	return items
}

func (m pickerModel) makeDimDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 1)
	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 1)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Padding(0, 0, 0, 2)
	return d
}

func (m pickerModel) makeActiveDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 0, 0, 1)
	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(m.theme.Blue)).
		Padding(0, 0, 0, 1)
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.FG)).
		Padding(0, 0, 0, 2)
	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 2)
	d.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 0, 0, 2)
	d.Styles.DimmedDesc = lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Padding(0, 0, 0, 2)
	return d
}

func (m pickerModel) enterTodoState() (tea.Model, tea.Cmd) {
	m.todo = newTodoModel(m.theme, app.Config.Paths.State)
	m.todo.embedded = true
	colW := m.width / 3
	if colW < 20 {
		colW = 20
	}
	agentH := len(m.agentList) + 3
	m.todo.width = m.width - colW - 2
	m.todo.height = m.height - 2 - agentH
	m.list.SetSize(colW, m.height-2)
	m.list.SetDelegate(m.makeDimDelegate())
	m.state = stateTodo
	return m, nil
}

func (m pickerModel) enterAgentState() (tea.Model, tea.Cmd) {
	m.agent = newAgentModelWithList(m.theme, m.agentList)
	m.agent.embedded = true
	colW := m.width / 3
	if colW < 20 {
		colW = 20
	}
	todoH := strings.Count(m.renderTodoSection(m.height/3), "\n") + 1
	m.agent.width = m.width - colW - 2
	m.agent.height = m.height - 2 - todoH
	m.list.SetSize(colW, m.height-2)
	m.list.SetDelegate(m.makeDimDelegate())
	m.state = stateAgent
	return m, nil
}

func (m pickerModel) enterBrowseState() (tea.Model, tea.Cmd) {
	m.state = stateBrowse
	if (len(m.todoGroups) > 0 || len(m.agentList) > 0) && m.width >= 60 {
		m.list.SetSize(m.width/4, m.height-2)
	} else {
		m.list.SetSize(m.width, m.height-2)
	}
	m.list.SetDelegate(m.makeActiveDelegate())
	m.todoGroups = todos.Load(app.Config.Paths.State)
	return m, nil
}

func (m pickerModel) enterBranchState(repo *scanner.Repo) (tea.Model, tea.Cmd) {
	tntDir := filepath.Dir(app.Config.Paths.Layouts)
	ctx := worktree.NewContext(repo.Path, repo.Name, tntDir)
	worktree.FetchAsync(ctx.GitRoot)
	m.branch = newBranchModel(m.theme, ctx)
	m.branch.embedded = true
	m.branch.width = m.width - m.width/3 - 2
	m.branch.height = m.height - 2
	m.list.SetSize(m.width/3, m.height-2)
	m.list.SetDelegate(m.makeDimDelegate())
	m.state = stateBranch
	return m, loadBranchEntriesCmd(ctx)
}

func (m pickerModel) enterLayoutState(repo *scanner.Repo) (tea.Model, tea.Cmd) {
	session := repo.Name
	workdir := m.tmux.resolveWorkdir(repo.Path)
	branch := m.tmux.branch

	m.layout = newLayoutModel(m.theme, app.Config.Paths.Layouts, workdir, session, branch)
	m.layout.embedded = true
	colW := m.width / 3
	if colW < 20 {
		colW = 20
	}
	m.layout.width = m.width - colW - 2
	m.layout.height = m.height - 2
	m.list.SetSize(colW, m.height-2)
	m.list.SetDelegate(m.makeDimDelegate())
	m.state = stateLayout
	return m, nil
}

func (m pickerModel) updateLayout(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.layout.Update(msg)
	m.layout = updated.(layoutModel)

	if m.layout.wantsBack {
		m.layout.wantsBack = false
		return m.enterBrowseState()
	}

	if m.layout.quitting {
		m.quitting = true
		m.action = "layout-action"
		return m, tea.Quit
	}

	return m, cmd
}

func (m pickerModel) updateBranch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.branch.Update(msg)
	m.branch = updated.(branchModel)

	if m.branch.wantsBack {
		m.branch.wantsBack = false
		return m.enterBrowseState()
	}

	if m.branch.quitting {
		m.quitting = true
		m.action = "branch-action"
		return m, tea.Quit
	}

	return m, cmd
}

func (m pickerModel) updateNewSession(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.state = stateBrowse
		return m, nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		name := strings.TrimSpace(m.newSessionInput.Value())
		if name == "" {
			m.state = stateBrowse
			return m, nil
		}
		m.newSessionName = name
		m.state = stateNewSessionGit
		return m, nil
	default:
		var cmd tea.Cmd
		m.newSessionInput, cmd = m.newSessionInput.Update(msg)
		return m, cmd
	}
}

func (m pickerModel) updateNewSessionGit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.action = "new-session-git"
		m.quitting = true
		return m, tea.Quit
	case "n":
		m.action = "new-session-lite"
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.state = stateBrowse
		return m, nil
	}
	return m, nil
}

func handleNewSession(m pickerModel, cfg *config.Config) {
	name := m.newSessionName
	isGit := m.action == "new-session-git"

	wsDir := ""
	for _, ws := range cfg.Workspaces {
		if ws.Name == m.workspace {
			if len(ws.Dirs) > 0 {
				wsDir = ws.Dirs[0]
			}
			break
		}
	}
	if wsDir == "" && len(cfg.Workspaces) > 0 {
		wsDir = cfg.Workspaces[0].Dirs[0]
	}
	if wsDir == "" {
		wsDir = os.Getenv("HOME")
	}

	projectDir := filepath.Join(wsDir, name)
	os.MkdirAll(projectDir, 0755)

	if isGit {
		exec.Command("git", "-C", projectDir, "init").Run()
	}

	sessionName := strings.ReplaceAll(name, ".", "_")
	exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", projectDir).Run()

	layout := "terminal"
	if isGit {
		layout = "dev"
	}
	layoutScript := filepath.Join(cfg.Paths.Layouts, layout+".sh")
	if info, err := os.Stat(layoutScript); err == nil && info.Mode()&0111 != 0 {
		exec.Command(layoutScript, projectDir, sessionName, "main").Run()
		exec.Command("tmux", "kill-window", "-t", sessionName+":1").Run()
	}

	exec.Command("tmux", "switch-client", "-t", sessionName).Run()
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
		ti := textinput.New()
		ti.CharLimit = 60
		ti.Width = 40
		wsLabel := m.workspace
		if wsLabel == "" {
			wsLabel = "default"
		}
		ti.Placeholder = fmt.Sprintf("session name (%s)", wsLabel)
		ti.Focus()
		m.newSessionInput = ti
		m.state = stateNewSession
		return m, textinput.Blink

	case key.Matches(msg, extraKeys.Branch):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		return m.enterBranchState(repo)

	case key.Matches(msg, extraKeys.Layout):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		return m.enterLayoutState(repo)

	case key.Matches(msg, extraKeys.Delete):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		if !repo.HasSession {
			return m, nil
		}
		session.Save(app.Config, repo.Name)
		if repo.Name == m.currentSession {
			next := m.nextSession()
			if next != "" {
				exec.Command("tmux", "switch-client", "-t", next).Run()
				m.currentSession = next
			}
		}
		exec.Command("tmux", "kill-session", "-t", repo.Name).Run()
		for i := range m.allRepos {
			if m.allRepos[i].Name == repo.Name {
				m.allRepos[i].HasSession = false
				break
			}
		}
		m.list.SetItems(m.rebuildListItems())
		return m, nil

	case key.Matches(msg, extraKeys.Workspace):
		if len(m.workspaceNames) == 0 {
			return m, nil
		}
		idx := -1
		for i, name := range m.workspaceNames {
			if name == m.workspace {
				idx = i
				break
			}
		}
		idx = (idx + 1) % (len(m.workspaceNames) + 1)
		if idx == len(m.workspaceNames) {
			m.workspace = ""
			m.list.Title = "tnt"
		} else {
			m.workspace = m.workspaceNames[idx]
			m.list.Title = "tnt [" + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Green)).Render(m.workspace) + "]"
		}
		m.list.SetItems(m.rebuildListItems())
		return m, nil

	case key.Matches(msg, extraKeys.Todo):
		return m.enterTodoState()

	case key.Matches(msg, extraKeys.Agents):
		return m.enterAgentState()

	case key.Matches(msg, key.NewBinding(key.WithKeys("right"))):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.openDetail(repo)
		return m, m.captureSelectedPane()

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.list.FilterState() == list.FilterApplied {
			if len(m.todoGroups) > 0 && m.width >= 60 {
				m.list.SetSize(m.width/4, m.height-2)
			}
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
		m.detailRepo = nil
		return m.enterBrowseState()

	case key.Matches(msg, key.NewBinding(key.WithKeys("b"))):
		if m.detailRepo != nil {
			return m.enterBranchState(m.detailRepo)
		}
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

func (m pickerModel) updateTodo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.todo.Update(msg)
	m.todo = updated.(todoModel)

	if m.todo.wantsBack {
		m.todo.wantsBack = false
		return m.enterBrowseState()
	}

	if m.todo.quitting {
		m.quitting = true
		return m, tea.Quit
	}

	return m, cmd
}

func (m pickerModel) updateAgent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.agent.Update(msg)
	m.agent = updated.(agentModel)

	if m.agent.wantsBack {
		m.agent.wantsBack = false
		return m.enterBrowseState()
	}

	if m.agent.quitting {
		m.quitting = true
		m.action = "agent-jump"
		return m, tea.Quit
	}

	return m, cmd
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

	if m.state == stateNewSession {
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.theme.Blue)).
			Padding(1, 2).
			Render("New session")

		wsLabel := m.workspace
		if wsLabel == "" {
			wsLabel = "all workspaces"
		}
		sub := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 2).
			Render(fmt.Sprintf("workspace: %s", wsLabel))

		input := lipgloss.NewStyle().
			Padding(1, 2).
			Render(m.newSessionInput.View())

		help := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 2).
			Render("enter confirm  esc cancel")

		return header + "\n" + sub + "\n" + input + "\n" + help
	}

	if m.state == stateNewSessionGit {
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.theme.Blue)).
			Padding(1, 2).
			Render(fmt.Sprintf("Create session: %s", m.newSessionName))

		prompt := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.theme.Yellow)).
			Padding(1, 2).
			Render("Initialize git repository?")

		help := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 2).
			Render("[y]es (dev layout)  [n]o (terminal layout)  esc cancel")

		return header + "\n" + prompt + "\n" + help
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
			Render("↵ select  b branch  ← back  esc back")

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

	if m.state == stateTodo {
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		left := m.list.View()
		agentSection := m.renderAgentSection(m.height/3, true)
		right := m.todo.View()
		if agentSection != "" {
			right += "\n" + agentSection
		}

		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		return columns
	}

	if m.state == stateAgent {
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		left := m.list.View()
		todoSection := m.renderTodoSection(m.height/3, true)
		right := ""
		if todoSection != "" {
			right = todoSection + "\n"
		}
		right += m.agent.View()

		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		return columns
	}

	if m.state == stateBranch {
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		left := m.list.View()
		right := m.branch.View()

		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		return columns
	}

	if m.state == stateLayout {
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		left := m.list.View()
		right := m.layout.View()

		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		return columns
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Gray)).
		Padding(0, 2).
		Render("↵ open  → details  b branch  l layout  t todo  g agents  w workspace  n new  d delete  / filter  esc quit")

	isFiltering := m.list.FilterState() == list.Filtering || m.list.FilterState() == list.FilterApplied
	dashboard := m.renderDashboard()
	if dashboard == "" || isFiltering {
		return m.list.View() + "\n" + help
	}

	sepHeight := m.height - 3
	if sepHeight < 1 {
		sepHeight = 1
	}
	sep := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.theme.Border)).
		Render(strings.Repeat("│\n", sepHeight))

	columns := lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), sep, dashboard)
	return columns + "\n" + help
}

func (m pickerModel) dashW() int {
	return m.width - m.width/4 - 2
}

func (m pickerModel) renderTodoSection(maxH int, dimmed ...bool) string {
	if len(m.todoGroups) == 0 {
		return ""
	}
	isDimmed := len(dimmed) > 0 && dimmed[0]
	dashW := m.dashW()
	var lines []string

	titleColor := m.theme.Blue
	if isDimmed {
		titleColor = m.theme.Gray
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(titleColor)).
		Padding(0, 1).
		Render(fmt.Sprintf("todos (%d)", todos.RepoActiveCount(m.todoGroups)))
	lines = append(lines, title)
	lines = append(lines, "")

	for _, rg := range m.todoGroups {
		hasActive := false
		for _, wt := range rg.Worktrees {
			if len(wt.Active) > 0 {
				hasActive = true
				break
			}
		}
		if !hasActive {
			continue
		}
		if len(lines) >= maxH {
			break
		}

		headerColor := m.theme.Purple
		doneColor := m.theme.Gray
		wtColor := m.theme.Cyan
		bulletColor := m.theme.Yellow
		textColor := m.theme.FG
		if isDimmed {
			headerColor = m.theme.Gray
			doneColor = m.theme.Border
			wtColor = m.theme.Border
			bulletColor = m.theme.Border
			textColor = m.theme.Gray
		}

		header := lipgloss.NewStyle().
			Foreground(lipgloss.Color(headerColor)).
			Bold(!isDimmed).
			Render(rg.Name)
		if rg.DoneTotal > 0 {
			header += lipgloss.NewStyle().
				Foreground(lipgloss.Color(doneColor)).
				Render(fmt.Sprintf(" +%d done", rg.DoneTotal))
		}
		lines = append(lines, "  "+header)

		multiWT := len(rg.Worktrees) > 1
		for _, wt := range rg.Worktrees {
			if len(wt.Active) == 0 {
				continue
			}
			if len(lines) >= maxH {
				break
			}
			if multiWT || wt.Name != "" {
				wtLabel := lipgloss.NewStyle().
					Foreground(lipgloss.Color(wtColor)).
					Render(wt.Name)
				lines = append(lines, "    "+wtLabel)
			}
			for _, t := range wt.Active {
				if len(lines) >= maxH {
					break
				}
				bullet := lipgloss.NewStyle().
					Foreground(lipgloss.Color(bulletColor)).
					Render("○")
				text := t.Text
				indent := "      "
				cutoff := dashW - 10
				if !multiWT && wt.Name == "" {
					indent = "    "
					cutoff = dashW - 6
				}
				if len(text) > cutoff {
					text = text[:cutoff-3] + "..."
				}
				todoLine := lipgloss.NewStyle().
					Foreground(lipgloss.Color(textColor)).
					Render(text)
				lines = append(lines, indent+bullet+" "+todoLine)
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m pickerModel) renderAgentSection(maxH int, dimmed ...bool) string {
	if len(m.agentList) == 0 {
		return ""
	}
	isDimmed := len(dimmed) > 0 && dimmed[0]
	dashW := m.dashW()
	var lines []string

	running, waiting, idle := agents.CountByStatus(m.agentList)
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
	titleColor := m.theme.Blue
	if isDimmed {
		titleColor = m.theme.Gray
	}
	agentTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(titleColor)).
		Padding(0, 1).
		Render(fmt.Sprintf("agents (%s)", strings.Join(counts, ", ")))
	lines = append(lines, agentTitle)
	lines = append(lines, "")

	for _, a := range m.agentList {
		if len(lines) >= maxH {
			break
		}
		var icon, statusLabel string
		switch a.Status {
		case agents.StatusRunning:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Green)).Render("◑")
			statusLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Green)).Width(8).Render("running")
		case agents.StatusWaiting:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Yellow)).Render("●")
			statusLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Yellow)).Width(8).Render("waiting")
		case agents.StatusIdle:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render("○")
			statusLabel = lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Width(8).Render("idle")
		}

		label := fmt.Sprintf("%s/%s", a.Session, a.WindowName)
		maxLabel := dashW - 14
		if maxLabel < 15 {
			maxLabel = 15
		}
		if len(label) > maxLabel {
			label = label[:maxLabel-3] + "..."
		}

		textColor := m.theme.FG
		if isDimmed {
			textColor = m.theme.Gray
		}
		labelStr := lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render(label)
		lines = append(lines, fmt.Sprintf("  %s %s %s", icon, statusLabel, labelStr))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func (m pickerModel) renderDashboard() string {
	hasTodos := len(m.todoGroups) > 0
	hasAgents := len(m.agentList) > 0
	if (!hasTodos && !hasAgents) || m.width < 60 || m.dashW() < 20 {
		return ""
	}
	maxH := m.height - 5
	todoSection := m.renderTodoSection(maxH)
	todoLines := strings.Count(todoSection, "\n") + 1
	agentSection := m.renderAgentSection(maxH - todoLines)

	var parts []string
	if todoSection != "" {
		parts = append(parts, todoSection)
	}
	if agentSection != "" {
		parts = append(parts, agentSection)
	}
	return strings.Join(parts, "\n")
}

func runPicker() {
	cfg := app.Config
	t := app.Theme

	repos := scanner.Scan(cfg)
	recentList := recents.Load(cfg.Paths.State)
	todoGroups := todos.Load(cfg.Paths.State)

	m := newPicker(repos, t, recentList)
	m.tmux = detectTmuxContext()
	m.allRepos = repos
	m.recentList = recentList
	m.currentSession = m.tmux.session
	m.workspaceNames = scanner.WorkspaceNames(cfg)
	m.workspace = cfg.Search.DefaultWorkspace
	if m.workspace != "" {
		m.list.Title = "tnt [" + lipgloss.NewStyle().Foreground(lipgloss.Color(t.Green)).Render(m.workspace) + "]"
		m.list.SetItems(m.rebuildListItems())
	}
	m.todoGroups = todoGroups
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	final := result.(pickerModel)

	if final.action == "agent-jump" {
		handleAgentDeferred(final.agent)
		return
	}

	if final.action == "branch-action" {
		handleBranchDeferred(final.branch)
		return
	}

	if final.action == "new-session-git" || final.action == "new-session-lite" {
		handleNewSession(final, cfg)
		return
	}

	if final.action == "layout-action" {
		handleLayoutDeferred(final.layout)
		return
	}

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
