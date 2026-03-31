package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/agents"
	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/linear"
	"github.com/arturgomes/tnt/internal/plans"
	"github.com/arturgomes/tnt/internal/prs"
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
	stateRepoContext
	stateOverview
	stateBranch
	stateLayout
	stateNewSession
	stateNewSessionGit
)

const prCacheTTL = 60 * time.Second
const reviewPRCacheTTL = 60 * time.Second
const linearCacheTTL = 120 * time.Second

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
	list              list.Model
	detailList        list.Model
	detailRepo        *scanner.Repo
	preview           string
	lastPane          string
	tmux              tmuxContext
	allRepos          []scanner.Repo
	recentList        *recents.List
	currentSession    string
	workspaceNames    []string
	workspace         string
	todoGroups        []todos.RepoGroup
	agentList         []agents.Agent
	planList          []plans.BranchProgress
	planRepo          string
	planCache         map[string][]plans.BranchProgress
	pr                prModel
	prList            []prs.PR
	prReviewList      []prs.PR
	prRepo            string
	prLoading         bool
	prLoadedAt        time.Time
	prCache           map[string][]prs.PR
	prLoadingSet      map[string]bool
	linearIssues      []linear.IssueWithWorktrees
	linearLoaded      bool
	linearLoading     bool
	linearLoadedAt    time.Time
	linearAPIKey      string
	linearCursor      int
	reviewPRs         []prs.PR
	reviewPRsLoaded   bool
	reviewPRsCursor   int
	reviewPRsLoading  bool
	reviewPRsLoadedAt time.Time
	tasksDir          string
	todo              todoModel
	agent             agentModel
	plan              planModel
	repoContextFocus  int
	overviewFocus     int
	branch            branchModel
	layout            layoutModel
	theme             *theme.Theme
	state             pickerState
	selected          *scanner.Repo
	action            string
	quitting          bool
	newSessionName    string
	newSessionInput   textinput.Model
	width             int
	height            int
}

type pickerKeys struct {
	Branch    key.Binding
	Layout    key.Binding
	New       key.Binding
	Delete    key.Binding
	Todo      key.Binding
	Agents    key.Binding
	Plans     key.Binding
	Workspace key.Binding
}

var extraKeys = pickerKeys{
	Branch:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branch")),
	Layout:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "layout")),
	New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Todo:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todo")),
	Agents:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "agents")),
	Plans:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "plans")),
	Workspace: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workspace")),
}

func newPicker(repos []scanner.Repo, t *theme.Theme, recentList *recents.List) pickerModel {
	var activeRepos, inactiveRepos []scanner.Repo
	var currentRepo *scanner.Repo

	currentSession, _ := exec.Command("tmux", "display-message", "-p", "#S").Output()
	current := strings.TrimSpace(string(currentSession))

	for _, r := range repos {
		if r.Name == current {
			rc := r
			currentRepo = &rc
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
	if len(activeRepos) > 0 {
		items = append(items, repoItem{repo: activeRepos[0]})
	}
	if currentRepo != nil {
		items = append(items, repoItem{repo: *currentRepo})
	}
	for i := 1; i < len(activeRepos); i++ {
		items = append(items, repoItem{repo: activeRepos[i]})
	}
	if (currentRepo != nil || len(activeRepos) > 0) && len(inactiveRepos) > 0 {
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

type prsLoadedMsg struct {
	mine     []prs.PR
	review   []prs.PR
	repoName string
}

type reviewPRsLoadedMsg struct {
	list []prs.PR
}

type linearLoadedMsg struct {
	issues []linear.IssueWithWorktrees
}

func loadAgentsCmd() tea.Msg {
	return agentsLoadedMsg{list: agents.Detect("")}
}

func loadPRsCmd(repoName, repoPath string) tea.Cmd {
	return func() tea.Msg {
		mine, _ := prs.LoadForRepo(repoPath)
		review, _ := prs.LoadReviewRequested(repoPath)
		return prsLoadedMsg{mine: mine, review: review, repoName: repoName}
	}
}

func loadReviewPRsCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		list, _ := prs.LoadReviewRequested(repoPath)
		return reviewPRsLoadedMsg{list: list}
	}
}

func loadLinearCmd(apiKey string, repos []scanner.Repo) tea.Cmd {
	return func() tea.Msg {
		issues, err := linear.LoadMyIssues(apiKey)
		if err != nil || len(issues) == 0 {
			return linearLoadedMsg{issues: nil}
		}
		matched := linear.MatchWorktrees(issues, buildLinearRepoInfos(repos))
		return linearLoadedMsg{issues: matched}
	}
}

func buildLinearRepoInfos(repos []scanner.Repo) []linear.RepoInfo {
	infos := make([]linear.RepoInfo, 0, len(repos))
	for _, r := range repos {
		if r.Path == "" {
			continue
		}
		infos = append(infos, linear.RepoInfo{
			Name:     r.Name,
			Path:     r.Path,
			Branches: listWorktreeBranches(r.Path),
		})
	}
	return infos
}

func listWorktreeBranches(repoPath string) []string {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	branches := []string{}
	s := bufio.NewScanner(strings.NewReader(string(out)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "branch refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(line, "branch refs/heads/")
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		branches = append(branches, branch)
	}
	return branches
}

func (m pickerModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, loadAgentsCmd)
	for _, r := range m.allRepos {
		if r.Path != "" {
			cmds = append(cmds, loadReviewPRsCmd(r.Path))
			break
		}
	}
	if repo := m.selectedRepo(); repo != nil && repo.Path != "" {
		cmds = append(cmds, loadPRsCmd(repo.Name, repo.Path))
	}
	if m.linearAPIKey != "" {
		m.linearLoading = true
		cmds = append(cmds, loadLinearCmd(m.linearAPIKey, m.allRepos))
	}
	return tea.Batch(cmds...)
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
		} else {
			m = m.applyMainLayoutSizing()
		}
		return m, nil

	case agentsLoadedMsg:
		m.agentList = msg.list
		m = m.applyMainLayoutSizing()
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

	case planRefreshMsg:
		m.planList = msg.list
		m.plan.plans = msg.list
		if m.plan.cursor >= len(m.plan.plans) {
			m.plan.cursor = len(m.plan.plans) - 1
		}
		if m.plan.cursor < 0 {
			m.plan.cursor = 0
		}
		return m, nil

	case prsLoadedMsg:
		if m.prCache == nil {
			m.prCache = map[string][]prs.PR{}
		}
		if m.prLoadingSet == nil {
			m.prLoadingSet = map[string]bool{}
		}
		m.prCache[msg.repoName] = msg.mine
		delete(m.prLoadingSet, msg.repoName)

		repo := m.selectedRepo()
		if repo != nil && repo.Name == msg.repoName {
			m.prRepo = msg.repoName
			m.prList = msg.mine
			m.prReviewList = msg.review
			m.pr.prs = msg.mine
			m.pr.reviewPRs = msg.review
			m.pr.rebuildRows()
			if m.pr.cursor >= len(m.pr.allRows) {
				m.pr.cursor = len(m.pr.allRows) - 1
			}
			if m.pr.cursor < 0 {
				m.pr.cursor = 0
			}
		}
		return m, nil

	case reviewPRsLoadedMsg:
		m.reviewPRs = msg.list
		m.reviewPRsLoaded = true
		m.reviewPRsLoading = false
		m.reviewPRsLoadedAt = time.Now()
		if m.reviewPRsCursor >= len(m.reviewPRs) {
			m.reviewPRsCursor = len(m.reviewPRs) - 1
		}
		if m.reviewPRsCursor < 0 {
			m.reviewPRsCursor = 0
		}
		m.reviewPRsCursor = m.clampGithubCursor(m.reviewPRsCursor)
		if m.reviewPRsCursor >= len(m.reviewPRs) {
			m.linearCursor = m.reviewPRsCursor - len(m.reviewPRs)
		}
		return m, nil

	case linearLoadedMsg:
		m.linearIssues = msg.issues
		m.linearLoaded = true
		m.linearLoading = false
		m.linearLoadedAt = time.Now()
		m.reviewPRsCursor = m.clampGithubCursor(m.reviewPRsCursor)
		if m.reviewPRsCursor >= len(m.reviewPRs) {
			m.linearCursor = m.reviewPRsCursor - len(m.reviewPRs)
		}
		if m.linearCursor >= len(m.linearIssues) {
			m.linearCursor = len(m.linearIssues) - 1
		}
		if m.linearCursor < 0 {
			m.linearCursor = 0
		}
		return m, nil

	case prChecksLoadedMsg:
		for i := range m.pr.prs {
			if m.pr.prs[i].Number == msg.number {
				m.pr.prs[i].Checks = msg.checks
				break
			}
		}
		for i := range m.prList {
			if m.prList[i].Number == msg.number {
				m.prList[i].Checks = msg.checks
				break
			}
		}
		m.pr.rebuildRows()
		return m, nil

	case prRefreshMsg:
		m.prList = msg.mine
		m.prReviewList = msg.review
		m.pr.prs = msg.mine
		m.pr.reviewPRs = msg.review
		m.pr.rebuildRows()
		if repo := m.selectedRepo(); repo != nil {
			m.prRepo = repo.Name
		}
		m.prLoadedAt = time.Now()
		m.prLoading = false
		if m.pr.cursor >= len(m.pr.prs) {
			m.pr.cursor = len(m.pr.prs) - 1
		}
		if m.pr.cursor < 0 {
			m.pr.cursor = 0
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
				return m.enterRepoContextState(0)
			case stateRepoContext:
				return m.enterOverviewState(0)
			case stateOverview:
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
		case stateRepoContext:
			return m.updateRepoContext(msg)
		case stateOverview:
			return m.updateOverview(msg)
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
	var currentRepo *scanner.Repo
	for _, r := range m.allRepos {
		if r.Name == m.currentSession {
			rc := r
			currentRepo = &rc
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
	if len(activeRepos) > 0 {
		items = append(items, repoItem{repo: activeRepos[0]})
	}
	if currentRepo != nil {
		items = append(items, repoItem{repo: *currentRepo})
	}
	for i := 1; i < len(activeRepos); i++ {
		items = append(items, repoItem{repo: activeRepos[i]})
	}
	if (currentRepo != nil || len(activeRepos) > 0) && len(inactiveRepos) > 0 {
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

func (m pickerModel) computeMainColumnWidths() (int, int, int) {
	if m.width < 80 {
		return m.width, 0, 0
	}
	if m.width <= 120 {
		col1 := m.width / 3
		if col1 < 20 {
			col1 = 20
		}
		col3 := m.width - col1 - 2
		if col3 < 20 {
			col3 = 20
		}
		return col1, 0, col3
	}
	col1 := m.width / 4
	if col1 < 20 {
		col1 = 20
	}
	remaining := m.width - col1 - 4
	if remaining < 40 {
		remaining = 40
	}
	col2 := (remaining * 37) / 75
	col3 := remaining - col2
	if col2 < 20 {
		col2 = 20
		col3 = remaining - col2
	}
	if col3 < 20 {
		col3 = 20
		col2 = remaining - col3
	}
	return col1, col2, col3
}

func (m pickerModel) applyMainLayoutSizing() pickerModel {
	col1, _, _ := m.computeMainColumnWidths()
	m.list.SetSize(col1, m.height-2)
	if m.state == stateBrowse {
		m.list.SetDelegate(m.makeActiveDelegate())
	} else {
		m.list.SetDelegate(m.makeDimDelegate())
	}
	return m
}

func (m pickerModel) enterRepoContextState(focus int) (tea.Model, tea.Cmd) {
	m = m.ensurePlanCacheForSelected()
	updated, prCmd := m.ensurePRCacheForSelected()
	m = updated
	if focus < 0 || focus > 2 {
		focus = 0
	}
	m.repoContextFocus = focus
	repo := m.selectedRepo()
	repoName := ""
	repoPath := ""
	if repo != nil {
		repoName = repo.Name
		repoPath = repo.Path
	}
	m.plan = newPlanModelWithList(m.theme, m.planList, m.tasksDir, repoName)
	m.plan.embedded = true
	m.pr = newPRModelWithList(m.theme, m.prList, nil, repoPath)
	m.pr.embedded = true
	m.state = stateRepoContext
	m = m.applyMainLayoutSizing()
	return m, tea.Batch(m.plan.Init(), m.pr.Init(), prCmd)
}

func (m pickerModel) enterOverviewState(focus int) (tea.Model, tea.Cmd) {
	m = m.ensurePlanCacheForSelected()
	updated, prCmd := m.ensurePRCacheForSelected()
	m = updated
	if focus < 0 || focus > 2 {
		focus = 0
	}
	m.overviewFocus = focus
	m.todo = newTodoModel(m.theme, app.Config.Paths.State)
	m.todo.embedded = true
	m.agent = newAgentModelWithList(m.theme, m.agentList)
	m.agent.embedded = true
	m.state = stateOverview
	m = m.applyMainLayoutSizing()
	cmds := []tea.Cmd{prCmd}
	if cmd := m.ensureReviewPRCache(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.ensureLinearCache(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m pickerModel) enterBrowseState() (tea.Model, tea.Cmd) {
	m = m.ensurePlanCacheForSelected()
	updated, prCmd := m.ensurePRCacheForSelected()
	m = updated
	m.state = stateBrowse
	m = m.applyMainLayoutSizing()
	m.todoGroups = todos.Load(app.Config.Paths.State)
	if cmd := m.ensureReviewPRCache(); cmd != nil {
		return m, tea.Batch(prCmd, cmd)
	}
	return m, prCmd
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
		m = m.ensurePlanCacheForSelected()
		updated, prCmd := m.ensurePRCacheForSelected()
		m = updated
		return m, prCmd

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
		m = m.ensurePlanCacheForSelected()
		updated, prCmd := m.ensurePRCacheForSelected()
		m = updated
		return m, prCmd

	case key.Matches(msg, extraKeys.Todo):
		return m.enterOverviewState(0)

	case key.Matches(msg, extraKeys.Agents):
		return m.enterOverviewState(1)

	case key.Matches(msg, extraKeys.Plans):
		return m.enterRepoContextState(0)

	case key.Matches(msg, key.NewBinding(key.WithKeys("right"))):
		repo := m.selectedRepo()
		if repo == nil {
			return m, nil
		}
		m.openDetail(repo)
		return m, m.captureSelectedPane()

	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		if m.list.FilterState() == list.FilterApplied {
			m = m.applyMainLayoutSizing()
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
	m = m.ensurePlanCacheForSelected()
	updated, prCmd := m.ensurePRCacheForSelected()
	m = updated
	if prCmd != nil {
		return m, tea.Batch(cmd, prCmd)
	}
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

func (m pickerModel) updatePlan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.plan.Update(msg)
	m.plan = updated.(planModel)

	if m.plan.wantsBack {
		m.plan.wantsBack = false
		return m.enterBrowseState()
	}

	if m.plan.jumpTo != "" {
		repo := m.selectedRepo()
		if repo != nil {
			m.selected = repo
			m.action = "jump-window:" + m.plan.jumpTo
			m.quitting = true
			return m, tea.Quit
		}
	}

	if m.plan.quitting {
		m.quitting = true
		return m, tea.Quit
	}

	return m, cmd
}

func (m pickerModel) updatePR(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.pr.Update(msg)
	m.pr = updated.(prModel)

	if m.pr.wantsBack {
		m.pr.wantsBack = false
		return m.enterBrowseState()
	}

	if m.pr.jumpTo != "" {
		repo := m.selectedRepo()
		if repo != nil {
			m.selected = repo
			m.action = "jump-window:" + m.pr.jumpTo
			m.quitting = true
			return m, tea.Quit
		}
	}

	if m.pr.quitting {
		m.quitting = true
		return m, tea.Quit
	}

	return m, cmd
}

func (m *pickerModel) ensureReviewPRCache() tea.Cmd {
	if len(m.allRepos) == 0 {
		return nil
	}
	if m.reviewPRsLoaded && !m.reviewPRsLoadedAt.IsZero() && time.Since(m.reviewPRsLoadedAt) < reviewPRCacheTTL {
		return nil
	}
	if m.reviewPRsLoading {
		return nil
	}
	for _, r := range m.allRepos {
		if r.Path != "" {
			m.reviewPRsLoading = true
			return loadReviewPRsCmd(r.Path)
		}
	}
	return nil
}

func (m *pickerModel) ensureLinearCache() tea.Cmd {
	if m.linearAPIKey == "" {
		return nil
	}
	if m.linearLoaded && !m.linearLoadedAt.IsZero() && time.Since(m.linearLoadedAt) < linearCacheTTL {
		return nil
	}
	if m.linearLoading {
		return nil
	}
	m.linearLoading = true
	return loadLinearCmd(m.linearAPIKey, m.allRepos)
}

func (m pickerModel) updateRepoContext(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nextFocus := func() {
		m.repoContextFocus = (m.repoContextFocus + 1) % 3
	}
	prevFocus := func() {
		m.repoContextFocus = (m.repoContextFocus + 2) % 3
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
		return m.enterBrowseState()
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("tab"))) {
		nextFocus()
		return m, nil
	}

	switch m.repoContextFocus {
	case 0:
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) && (len(m.plan.plans) == 0 || m.plan.cursor >= len(m.plan.plans)-1) {
			nextFocus()
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) && (len(m.plan.plans) == 0 || m.plan.cursor == 0) {
			prevFocus()
			return m, nil
		}
		updated, cmd := m.plan.Update(msg)
		m.plan = updated.(planModel)
		if m.plan.wantsBack {
			m.plan.wantsBack = false
			nextFocus()
			return m, nil
		}
		if m.plan.jumpTo != "" {
			repo := m.selectedRepo()
			if repo != nil {
				m.selected = repo
				m.action = "jump-window:" + m.plan.jumpTo
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, cmd
	case 1:
		lastPRIdx := len(m.pr.allRows) - 1
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) && (len(m.pr.allRows) == 0 || m.pr.cursor >= lastPRIdx) {
			nextFocus()
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) && (len(m.pr.allRows) == 0 || m.pr.cursor == 0) {
			prevFocus()
			return m, nil
		}
		updated, cmd := m.pr.Update(msg)
		m.pr = updated.(prModel)
		if m.pr.wantsBack {
			m.pr.wantsBack = false
			nextFocus()
			return m, nil
		}
		if m.pr.openPR > 0 {
			repo := m.selectedRepo()
			if repo != nil {
				m.selected = repo
				m.action = fmt.Sprintf("open-pr:%d", m.pr.openPR)
				m.quitting = true
				return m, tea.Quit
			}
		}
		if m.pr.jumpTo != "" {
			repo := m.selectedRepo()
			if repo != nil {
				m.selected = repo
				m.action = "jump-window:" + m.pr.jumpTo
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, cmd
	default:
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) {
			nextFocus()
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) {
			prevFocus()
			return m, nil
		}
		return m, nil
	}
}

func (m pickerModel) updateOverview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nextFocus := func() {
		m.overviewFocus = (m.overviewFocus + 1) % 3
	}
	prevFocus := func() {
		m.overviewFocus = (m.overviewFocus + 2) % 3
	}

	if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
		return m.enterBrowseState()
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("tab"))) {
		nextFocus()
		return m, nil
	}

	switch m.overviewFocus {
	case 0:
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) && (len(m.todo.rows) == 0 || m.todo.cursor >= len(m.todo.rows)-1) {
			nextFocus()
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) && (len(m.todo.rows) == 0 || m.todo.cursor == 0) {
			prevFocus()
			return m, nil
		}
		updated, cmd := m.todo.Update(msg)
		m.todo = updated.(todoModel)
		if m.todo.wantsBack {
			m.todo.wantsBack = false
			nextFocus()
			return m, nil
		}
		if m.todo.quitting {
			m.quitting = true
			return m, tea.Quit
		}
		return m, cmd
	case 1:
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) && (len(m.agent.agents) == 0 || m.agent.cursor >= len(m.agent.agents)-1) {
			nextFocus()
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) && (len(m.agent.agents) == 0 || m.agent.cursor == 0) {
			prevFocus()
			return m, nil
		}
		updated, cmd := m.agent.Update(msg)
		m.agent = updated.(agentModel)
		if m.agent.wantsBack {
			m.agent.wantsBack = false
			nextFocus()
			return m, nil
		}
		if m.agent.quitting {
			m.quitting = true
			m.action = "agent-jump"
			return m, tea.Quit
		}
		return m, cmd
	default:
		if key.Matches(msg, key.NewBinding(key.WithKeys("down"))) {
			total := len(m.reviewPRs) + len(m.linearIssues)
			if total == 0 || m.reviewPRsCursor >= total-1 {
				nextFocus()
				return m, nil
			}
			m.reviewPRsCursor = m.clampGithubCursor(m.reviewPRsCursor + 1)
			if m.reviewPRsCursor >= len(m.reviewPRs) {
				m.linearCursor = m.reviewPRsCursor - len(m.reviewPRs)
			}
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("up"))) {
			total := len(m.reviewPRs) + len(m.linearIssues)
			if total == 0 || m.reviewPRsCursor <= 0 {
				prevFocus()
				return m, nil
			}
			m.reviewPRsCursor = m.clampGithubCursor(m.reviewPRsCursor - 1)
			if m.reviewPRsCursor >= len(m.reviewPRs) {
				m.linearCursor = m.reviewPRsCursor - len(m.reviewPRs)
			}
			return m, nil
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("enter"))) {
			kind, idx := m.selectedGithubItem()
			switch kind {
			case "review":
				selected := m.reviewPRs[idx]
				if selected.Branch != "" {
					repo := m.selectedRepo()
					if repo != nil {
						m.selected = repo
						m.action = "jump-window:" + selected.Branch
						m.quitting = true
						return m, tea.Quit
					}
				}
			case "linear":
				selected := m.linearIssues[idx]
				if selected.URL != "" {
					m.action = "open-linear:" + selected.URL
					m.quitting = true
					return m, tea.Quit
				}
			}
		}
		if key.Matches(msg, key.NewBinding(key.WithKeys("w"))) {
			kind, idx := m.selectedGithubItem()
			if kind == "linear" {
				m.action = "linear-work:" + m.linearIssues[idx].Identifier
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, nil
	}
}

func (m pickerModel) renderRepoColumn(width, height int, focused bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	sectionH := height / 3
	if sectionH < 5 {
		sectionH = 5
	}
	dimmed := m.state != stateBrowse && !focused
	box := lipgloss.NewStyle().Width(width).Height(sectionH)

	var parts []string
	if focused && m.repoContextFocus == 0 {
		plan := m.plan
		plan.width = width
		plan.height = sectionH
		parts = append(parts, box.Render(plan.View()))
	} else {
		parts = append(parts, box.Render(m.renderPlanSection(sectionH, dimmed)))
	}

	if focused && m.repoContextFocus == 1 {
		pr := m.pr
		pr.width = width
		pr.height = sectionH
		parts = append(parts, box.Render(pr.View()))
	} else {
		parts = append(parts, box.Render(m.renderPRSection(sectionH, dimmed)))
	}

	parts = append(parts, box.Render(m.renderRepoGitSection(width, sectionH, focused && m.repoContextFocus == 2)))
	return strings.Join(parts, "\n")
}

func (m pickerModel) renderRepoGitSection(width, maxH int, active bool) string {
	if maxH < 3 {
		maxH = 3
	}
	dimmed := m.state != stateBrowse && !active
	titleColor := m.theme.Blue
	textColor := m.theme.FG
	if dimmed {
		titleColor = m.theme.Gray
		textColor = m.theme.Border
	}
	if active {
		titleColor = m.theme.Blue
		textColor = m.theme.FG
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).Padding(0, 1).Render("git"))
	lines = append(lines, "")

	repo := m.selectedRepo()
	if repo == nil {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("no repo selected"))
		return strings.Join(lines, "\n")
	}

	branch := repo.CurrentBranch
	if branch == "" {
		branch = "unknown"
	}
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("branch: "+branch))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render(fmt.Sprintf("worktrees: %d", repo.BranchCount)))

	if len(lines) > maxH {
		lines = lines[:maxH]
	}
	return strings.Join(lines, "\n")
}

func (m pickerModel) renderOverviewColumn(width, height int, focused bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	sectionH := height / 3
	if sectionH < 5 {
		sectionH = 5
	}
	dimmed := m.state != stateBrowse && !focused
	box := lipgloss.NewStyle().Width(width).Height(sectionH)

	var parts []string
	if focused && m.overviewFocus == 0 {
		todo := m.todo
		todo.width = width
		todo.height = sectionH
		parts = append(parts, box.Render(todo.View()))
	} else {
		parts = append(parts, box.Render(m.renderTodoSection(sectionH, dimmed)))
	}

	if focused && m.overviewFocus == 1 {
		agent := m.agent
		agent.width = width
		agent.height = sectionH
		parts = append(parts, box.Render(agent.View()))
	} else {
		parts = append(parts, box.Render(m.renderAgentSection(sectionH, dimmed)))
	}

	parts = append(parts, box.Render(m.renderGithubSection(width, sectionH, focused && m.overviewFocus == 2)))
	return strings.Join(parts, "\n")
}

func (m pickerModel) renderGithubSection(width, maxH int, active bool) string {
	dimmed := m.state != stateBrowse && !active
	titleColor := m.theme.Blue
	textColor := m.theme.FG
	if dimmed {
		titleColor = m.theme.Gray
		textColor = m.theme.Border
	}
	if active {
		titleColor = m.theme.Blue
		textColor = m.theme.FG
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).Padding(0, 1).Render("github"))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(titleColor)).Render("review requested"))

	if !m.reviewPRsLoaded && m.reviewPRsLoading {
		lines = append(lines, "    "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("loading..."))
		return strings.Join(lines, "\n")
	}
	if len(m.reviewPRs) == 0 {
		lines = append(lines, "    "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("none"))
	} else {
		limit := maxH - len(lines)
		if limit < 1 {
			limit = 1
		}
		for i, pr := range m.reviewPRs {
			if i >= limit {
				break
			}
			author := pr.Author.Login
			if author == "" {
				author = pr.Author.Name
			}
			label := fmt.Sprintf("#%d %s", pr.Number, pr.Branch)
			if len(label) > width-10 {
				label = label[:width-13] + "..."
			}
			line := fmt.Sprintf("    %-24s %s", label, author)
			if active && m.reviewPRsCursor == i {
				line = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render("> "+line)
			} else {
				line = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("  "+line)
			}
			lines = append(lines, line)
		}
	}

	if m.linearAPIKey != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(titleColor)).Render("linear"))
		if m.linearLoading && !m.linearLoaded {
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("loading..."))
			return strings.Join(lines, "\n")
		}
		if len(m.linearIssues) == 0 {
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render("none"))
			return strings.Join(lines, "\n")
		}

		for i, issue := range m.linearIssues {
			if len(lines) >= maxH {
				break
			}

			icon := "○"
			iconColor := m.theme.Gray
			stateLabel := ""
			switch {
			case strings.EqualFold(issue.StateName, "In Progress"):
				icon = "◑"
				iconColor = m.theme.Yellow
				stateLabel = "progress"
			case strings.EqualFold(issue.StateName, "In Review"):
				icon = "◎"
				iconColor = m.theme.Cyan
				stateLabel = "review"
			case strings.EqualFold(issue.StateType, "started"):
				icon = "◑"
				iconColor = m.theme.Yellow
				stateLabel = strings.ToLower(issue.StateName)
			default:
				stateLabel = "todo"
			}

			titleStr := issue.Title
			maxTitle := width - 30
			if maxTitle < 10 {
				maxTitle = 10
			}
			if len(titleStr) > maxTitle {
				titleStr = titleStr[:maxTitle-3] + "..."
			}

			stateTag := lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor)).Render(icon + " " + stateLabel)
			idStr := lipgloss.NewStyle().Foreground(lipgloss.Color(iconColor)).Bold(true).Render(issue.Identifier)
			titleRender := lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)).Render(titleStr)

			styled := fmt.Sprintf("    %s  %s  %s", stateTag, idStr, titleRender)
			if active && m.reviewPRsCursor == len(m.reviewPRs)+i {
				styled = "    " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Blue)).Bold(true).Render(fmt.Sprintf("> %s  %s  %s", stateLabel, issue.Identifier, titleStr))
			}
			lines = append(lines, styled)

			for _, wt := range issue.Worktrees {
				if len(lines) >= maxH {
					break
				}
				progress := ""
				if wt.HasTasks {
					progress = fmt.Sprintf(" %s %d/%d", progressBar(wt.Done, wt.Total, 4), wt.Done, wt.Total)
				}
				wtLine := fmt.Sprintf("● %s%s", wt.Repo, progress)
				lines = append(lines, "        "+lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Green)).Render(wtLine))

				linkedPR := m.findPRForRepo(issue.Identifier, wt.Repo)
				if linkedPR != nil && len(lines) < maxH {
					prLine := m.formatPRInline(linkedPR)
					lines = append(lines, "            "+prLine)
				}
			}

			if len(issue.Worktrees) == 0 {
				linkedPR := m.findPRForTicket(issue.Identifier)
				if linkedPR != nil && len(lines) < maxH {
					prLine := m.formatPRInline(linkedPR)
					lines = append(lines, "        "+prLine)
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

func (m pickerModel) findPRForTicket(ticketID string) *prs.PR {
	lower := strings.ToLower(ticketID)
	for _, cached := range m.prCache {
		for i := range cached {
			if strings.Contains(strings.ToLower(cached[i].Branch), lower) {
				return &cached[i]
			}
		}
	}
	if m.prRepo != "" {
		for i := range m.prList {
			if strings.Contains(strings.ToLower(m.prList[i].Branch), lower) {
				return &m.prList[i]
			}
		}
	}
	return nil
}

func (m pickerModel) findPRForRepo(ticketID, repoName string) *prs.PR {
	lower := strings.ToLower(ticketID)
	if cached, ok := m.prCache[repoName]; ok {
		for i := range cached {
			if strings.Contains(strings.ToLower(cached[i].Branch), lower) {
				return &cached[i]
			}
		}
	}
	if m.prRepo == repoName {
		for i := range m.prList {
			if strings.Contains(strings.ToLower(m.prList[i].Branch), lower) {
				return &m.prList[i]
			}
		}
	}
	return nil
}

func (m pickerModel) formatPRInline(pr *prs.PR) string {
	prIcon, prColor := prs.ChecksIcon(prs.ChecksSummary(*pr))
	reviewIcon, reviewLabel, reviewColor := prs.ReviewIcon(pr.ReviewDecision, pr.IsDraft)

	prLine := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Purple)).Render(fmt.Sprintf("PR #%d", pr.Number))
	if prIcon != "" {
		c := m.theme.Gray
		switch prColor {
		case "green":
			c = m.theme.Green
		case "red":
			c = m.theme.Red
		case "yellow":
			c = m.theme.Yellow
		}
		prLine += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(prIcon)
	}
	if reviewIcon != "" {
		c := m.theme.Gray
		switch reviewColor {
		case "green":
			c = m.theme.Green
		case "orange":
			c = m.theme.Orange
		}
		prLine += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(reviewIcon+" "+reviewLabel)
	}
	return prLine
}

func (m pickerModel) selectedGithubItem() (kind string, index int) {
	total := len(m.reviewPRs) + len(m.linearIssues)
	if total == 0 {
		return "", -1
	}
	cursor := m.clampGithubCursor(m.reviewPRsCursor)
	if cursor < len(m.reviewPRs) {
		return "review", cursor
	}
	idx := cursor - len(m.reviewPRs)
	if idx >= 0 && idx < len(m.linearIssues) {
		return "linear", idx
	}
	return "", -1
}

func (m pickerModel) clampGithubCursor(cursor int) int {
	total := len(m.reviewPRs) + len(m.linearIssues)
	if total <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= total {
		return total - 1
	}
	return cursor
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

	if m.state == stateBrowse || m.state == stateRepoContext || m.state == stateOverview {
		m = m.ensurePlanCacheForSelected()

		help := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Gray)).
			Padding(0, 2).
			Render("↵ open  → details  b branch  l layout  t todo  g agents  p plans  w workspace  n new  d delete  / filter  tab cycle  esc quit")

		col1W, col2W, col3W := m.computeMainColumnWidths()
		sepHeight := m.height - 3
		if sepHeight < 1 {
			sepHeight = 1
		}
		sep := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Border)).
			Render(strings.Repeat("│\n", sepHeight))

		m.list.SetSize(col1W, m.height-2)
		if m.state == stateBrowse {
			m.list.SetDelegate(m.makeActiveDelegate())
		} else {
			m.list.SetDelegate(m.makeDimDelegate())
		}
		left := lipgloss.NewStyle().Width(col1W).Render(m.list.View())

		if m.width < 80 {
			return left + "\n" + help
		}

		if m.width <= 120 {
			col3 := lipgloss.NewStyle().Width(col3W).Render(m.renderOverviewColumn(col3W, m.height-2, m.state == stateOverview))
			columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, col3)
			return columns + "\n" + help
		}

		col2 := lipgloss.NewStyle().Width(col2W).Render(m.renderRepoColumn(col2W, m.height-2, m.state == stateRepoContext))
		col3 := lipgloss.NewStyle().Width(col3W).Render(m.renderOverviewColumn(col3W, m.height-2, m.state == stateOverview))
		columns := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, col2, sep, col3)
		return columns + "\n" + help
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

	return m.list.View()
}

func (m pickerModel) dashW() int {
	_, col2, col3 := m.computeMainColumnWidths()
	if m.state == stateRepoContext && col2 > 0 {
		return col2
	}
	if col3 > 0 {
		return col3
	}
	if m.width > 0 {
		return m.width
	}
	return 40
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
	isDimmed := len(dimmed) > 0 && dimmed[0]
	if len(m.agentList) == 0 {
		titleColor := m.theme.Blue
		if isDimmed {
			titleColor = m.theme.Gray
		}
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).Padding(0, 1).Render("agents")
		loading := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render("  loading...")
		return title + "\n" + loading
	}
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

func (m pickerModel) renderPlanSection(maxH int, dimmed ...bool) string {
	if maxH < 2 {
		return ""
	}
	isDimmed := len(dimmed) > 0 && dimmed[0]
	var lines []string

	titleColor := m.theme.Blue
	if isDimmed {
		titleColor = m.theme.Gray
	}
	lines = append(lines, lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(titleColor)).
		Padding(0, 1).
		Render("plans"))
	lines = append(lines, "")

	if len(m.planList) == 0 {
		noData := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render("  no tasks")
		lines = append(lines, noData)
		lines = append(lines, "")
		return strings.Join(lines, "\n")
	}

	for _, p := range m.planList {
		if len(lines) >= maxH {
			break
		}
		status := fmt.Sprintf("%s %d/%d", progressBar(p.Done, p.Total, 9), p.Done, p.Total)
		if p.Total > 0 && p.Done == p.Total {
			status += " done"
		}
		branchColor := m.theme.Purple
		statusColor := m.theme.Gray
		if isDimmed {
			branchColor = m.theme.Gray
			statusColor = m.theme.Border
		}
		branch := lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor)).Render(p.Branch)
		line := fmt.Sprintf("  %-12s %s", branch, lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(status))
		lines = append(lines, line)
	}
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

func (m pickerModel) renderPRSection(maxH int, dimmed ...bool) string {
	isDimmed := len(dimmed) > 0 && dimmed[0]
	repo := m.selectedRepo()
	if repo == nil || maxH < 2 {
		titleColor := m.theme.Blue
		if isDimmed {
			titleColor = m.theme.Gray
		}
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).Padding(0, 1).Render("pull requests")
		return title
	}
	if m.prRepo != repo.Name || len(m.prList) == 0 {
		titleColor := m.theme.Blue
		if isDimmed {
			titleColor = m.theme.Gray
		}
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).Padding(0, 1).Render("pull requests")
		msg := "  loading..."
		if m.prRepo == repo.Name && !m.prLoading {
			msg = "  no PRs"
		}
		loading := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Gray)).Render(msg)
		return title + "\n" + loading
	}
	var lines []string

	titleColor := m.theme.Blue
	if isDimmed {
		titleColor = m.theme.Gray
	}
	lines = append(lines, lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(titleColor)).
		Padding(0, 1).
		Render("pull requests"))
	lines = append(lines, "")

	for _, pr := range m.prList {
		if len(lines) >= maxH {
			break
		}
		numColor := m.theme.Purple
		branchColor := m.theme.Purple
		metaColor := m.theme.Gray
		if isDimmed {
			numColor = m.theme.Gray
			branchColor = m.theme.Gray
			metaColor = m.theme.Border
		}

		number := lipgloss.NewStyle().Foreground(lipgloss.Color(numColor)).Render(fmt.Sprintf("#%d", pr.Number))
		branch := lipgloss.NewStyle().Foreground(lipgloss.Color(branchColor)).Render(pr.Branch)

		if pr.State == "MERGED" {
			stateColor := m.theme.Purple
			if isDimmed {
				stateColor = m.theme.Border
			}
			lines = append(lines, fmt.Sprintf("  %s %-14s  %s", number, branch, lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor)).Render("◆ merged")))
			continue
		}
		if pr.State == "CLOSED" {
			stateColor := m.theme.Red
			if isDimmed {
				stateColor = m.theme.Border
			}
			lines = append(lines, fmt.Sprintf("  %s %-14s  %s", number, branch, lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor)).Render("✗ closed")))
			continue
		}

		passed, failed, pending := prs.ChecksSummary(pr)
		checkIcon, checkColor := prs.ChecksIcon(passed, failed, pending)
		total := passed + failed + pending
		checkPart := ""
		if checkIcon != "" && total > 0 {
			c := m.theme.Gray
			switch checkColor {
			case "green":
				c = m.theme.Green
			case "red":
				c = m.theme.Red
			case "yellow":
				c = m.theme.Yellow
			}
			if isDimmed {
				c = m.theme.Border
			}
			checkPart = fmt.Sprintf("%s %d/%d", lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(checkIcon), passed, total)
		}

		reviewIcon, reviewLabel, reviewColor := prs.ReviewIcon(pr.ReviewDecision, pr.IsDraft)
		reviewPart := ""
		if reviewIcon != "" {
			c := m.theme.Gray
			switch reviewColor {
			case "green":
				c = m.theme.Green
			case "orange":
				c = m.theme.Orange
			case "gray":
				c = m.theme.Gray
			}
			if isDimmed {
				c = m.theme.Border
			}
			reviewPart = fmt.Sprintf("%s %s", lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(reviewIcon), lipgloss.NewStyle().Foreground(lipgloss.Color(metaColor)).Render(reviewLabel))
		}

		parts := []string{fmt.Sprintf("%s %-14s", number, branch)}
		if checkPart != "" {
			parts = append(parts, checkPart)
		}
		if reviewPart != "" {
			parts = append(parts, reviewPart)
		}
		lines = append(lines, "  "+strings.Join(parts, "  "))
	}
	lines = append(lines, "")

	return strings.Join(lines, "\n")
}

func (m pickerModel) ensurePlanCacheForSelected() pickerModel {
	if m.tasksDir == "" {
		return m
	}
	repo := m.selectedRepo()
	if repo == nil {
		m.planRepo = ""
		m.planList = nil
		return m
	}
	if m.planCache == nil {
		m.planCache = map[string][]plans.BranchProgress{}
	}
	if cached, ok := m.planCache[repo.Name]; ok {
		m.planRepo = repo.Name
		m.planList = cached
		return m
	}
	loaded := plans.LoadForRepo(m.tasksDir, repo.Name)
	m.planCache[repo.Name] = loaded
	m.planRepo = repo.Name
	m.planList = loaded
	return m
}

func (m pickerModel) ensurePRCacheForSelected() (pickerModel, tea.Cmd) {
	repo := m.selectedRepo()
	if repo == nil || !repo.HasSession {
		m.prRepo = ""
		m.prList = nil
		return m, nil
	}

	if m.prCache == nil {
		m.prCache = map[string][]prs.PR{}
	}
	if m.prLoadingSet == nil {
		m.prLoadingSet = map[string]bool{}
	}

	if cached, ok := m.prCache[repo.Name]; ok {
		m.prRepo = repo.Name
		m.prList = cached
		return m, nil
	}

	if m.prLoadingSet[repo.Name] {
		return m, nil
	}

	m.prLoadingSet[repo.Name] = true
	return m, loadPRsCmd(repo.Name, repo.Path)
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
	homeDir, err := os.UserHomeDir()
	if err == nil {
		m.tasksDir = filepath.Join(homeDir, ".config", "opencode", "tasks")
	}
	m.workspaceNames = scanner.WorkspaceNames(cfg)
	m.workspace = cfg.Search.DefaultWorkspace
	if m.workspace != "" {
		m.list.Title = "tnt [" + lipgloss.NewStyle().Foreground(lipgloss.Color(t.Green)).Render(m.workspace) + "]"
		m.list.SetItems(m.rebuildListItems())
	}
	m.todoGroups = todoGroups
	m.linearAPIKey = linear.LoadAPIKey()
	m.linearLoading = m.linearAPIKey != ""
	m = m.ensurePlanCacheForSelected()
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	final := result.(pickerModel)

	if strings.HasPrefix(final.action, "open-pr:") {
		numStr := strings.TrimPrefix(final.action, "open-pr:")
		if final.selected != nil {
			cmd := exec.Command("gh", "pr", "view", numStr, "--json", "url", "-q", ".url")
			cmd.Dir = final.selected.Path
			if out, err := cmd.Output(); err == nil {
				url := strings.TrimSpace(string(out))
				if url != "" {
					exec.Command("open", url).Run()
				}
			}
		}
		return
	}

	if strings.HasPrefix(final.action, "open-linear:") {
		url := strings.TrimPrefix(final.action, "open-linear:")
		if url != "" {
			exec.Command("open", url).Run()
		}
		return
	}

	if strings.HasPrefix(final.action, "linear-work:") {
		ticketID := strings.TrimPrefix(final.action, "linear-work:")
		handleLinearWork(final, ticketID, cfg)
		return
	}

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
		cmd := exec.Command("tmux", "-u", "-2", "attach-session", "-t="+name)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
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

func handleLinearWork(m pickerModel, ticketID string, cfg *config.Config) {
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return
	}

	for _, issue := range m.linearIssues {
		if !strings.EqualFold(issue.Identifier, ticketID) {
			continue
		}
		if len(issue.Worktrees) > 0 {
			jumpOrCreateLinearWorktree(issue.Worktrees[0], m, cfg)
			return
		}
		break
	}

	repo := m.selected
	if repo == nil && len(m.allRepos) > 0 {
		r := m.allRepos[0]
		repo = &r
	}
	if repo == nil {
		return
	}
	branchName := strings.ToLower(ticketID)
	jumpOrCreateLinearWorktree(linear.TicketWorktree{Repo: repo.Name, Branch: branchName}, m, cfg)
}

func jumpOrCreateLinearWorktree(wt linear.TicketWorktree, m pickerModel, cfg *config.Config) {
	var repo *scanner.Repo
	for i := range m.allRepos {
		if m.allRepos[i].Name == wt.Repo {
			repo = &m.allRepos[i]
			break
		}
	}
	if repo == nil {
		return
	}

	if !repo.HasSession {
		createSession(repo.Name, repo.Path)
	}
	switchToSession(repo.Name)

	if wid := findWorktreeWindowID(repo.Name, wt.Branch); wid != "" {
		exec.Command("tmux", "select-window", "-t", wid).Run()
		return
	}

	worktreeDir := filepath.Join(repo.Path, cfg.Branch.WorktreeDir, wt.Branch)
	if _, err := os.Stat(worktreeDir); err != nil {
		exec.Command("git", "-C", repo.Path, "worktree", "add", worktreeDir, wt.Branch).Run()
	}
	layoutScript := filepath.Join(cfg.Paths.Layouts, cfg.Layout.Default+".sh")
	exec.Command(layoutScript, worktreeDir, repo.Name, wt.Branch).Run()
}

func findWorktreeWindowID(sessionName, branch string) string {
	out, err := exec.Command("tmux", "list-windows", "-t", sessionName, "-F", "#{window_id}\t#{@worktree}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[1], branch) {
			return strings.TrimSpace(parts[0])
		}
	}
	return ""
}
