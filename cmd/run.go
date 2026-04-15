package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturgoms/tnt/internal/theme"
	"github.com/arturgoms/tnt/internal/worktree"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type hooks struct {
	PostCreate []string `json:"post_create"`
	PreDelete  []string `json:"pre_delete"`
	PostDelete []string `json:"post_delete"`
}

type service struct {
	Name  string   `json:"name"`
	Run   string   `json:"run"`
	Cwd   string   `json:"cwd"`
	Setup []string `json:"setup"`
}

type projectConfig struct {
	DefaultLayout string    `json:"default_layout"`
	Env           string    `json:"env"`
	Hooks         hooks     `json:"hooks"`
	Services      []service `json:"services"`
}

func runHooks(commands []string, cwd string) {
	for _, cmd := range commands {
		c := exec.Command("sh", "-c", cmd)
		c.Dir = cwd
		c.Run()
	}
}

func loadProjectConfig(projectsDir, repoName string) projectConfig {
	path := filepath.Join(projectsDir, repoName, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return projectConfig{}
	}
	var cfg projectConfig
	json.Unmarshal(data, &cfg)
	return cfg
}

func moveRunWindowFirst(session, wid string) {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id}").Output()
	if err != nil {
		return
	}
	windows := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(windows) == 0 || windows[0] == wid {
		return
	}
	exec.Command("tmux", "move-window", "-b", "-s", wid, "-t", windows[0]).Run()
}

func findRunWindow(session string) string {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id} #{@run}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(line, " 1") {
			return strings.Fields(line)[0]
		}
	}
	return ""
}

func ensureRunWindow(session, workdir, branch string) string {
	wid := findRunWindow(session)
	if wid != "" {
		return wid
	}
	short := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		short = branch[idx+1:]
	}
	out, err := exec.Command("tmux", "new-window", "-P", "-F", "#{window_id}",
		"-t", session, "-n", short+":run", "-d", "-c", workdir).Output()
	if err != nil {
		return ""
	}
	wid = strings.TrimSpace(string(out))
	exec.Command("tmux", "set-option", "-w", "-t", wid, "@run", "1").Run()
	exec.Command("tmux", "set-option", "-w", "-t", wid, "@worktree", branch).Run()
	exec.Command("tmux", "set-option", "-w", "-t", wid, "@worktree_color", worktree.WorktreeColor(branch)).Run()
	moveRunWindowFirst(session, wid)
	return wid
}

func waitForPane(paneID string) {
	for i := 0; i < 10; i++ {
		out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p").Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func firstPane(windowID string) string {
	out, err := exec.Command("tmux", "list-panes", "-t", windowID, "-F", "#{pane_id}").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return ""
}

func runStartServices(session, workdir, branch, projectsDir, repoName, mainRoot string) {
	cfg := loadProjectConfig(projectsDir, repoName)
	if len(cfg.Services) == 0 {
		exec.Command("tmux", "display-message", "No services in config").Run()
		return
	}

	wid := ensureRunWindow(session, workdir, branch)
	if wid == "" {
		return
	}

	count := 0
	for i, svc := range cfg.Services {
		if svc.Run == "" {
			continue
		}
		svcDir := filepath.Join(workdir, svc.Cwd)
		if svc.Cwd == "" || svc.Cwd == "." {
			svcDir = workdir
		}

		if i == 0 {
			pane := firstPane(wid)
			waitForPane(pane)
			exec.Command("tmux", "send-keys", "-t", pane, "cd '"+svcDir+"'", "Enter").Run()
			exec.Command("tmux", "send-keys", "-t", pane, "export TNT_MAIN_ROOT='"+mainRoot+"'", "Enter").Run()
			if cfg.Env != "" {
				exec.Command("tmux", "send-keys", "-t", pane, cfg.Env, "Enter").Run()
			}
			for _, setupCmd := range svc.Setup {
				exec.Command("tmux", "send-keys", "-t", pane, setupCmd, "Enter").Run()
			}
			exec.Command("tmux", "send-keys", "-t", pane, svc.Run, "Enter").Run()
		} else {
			out, err := exec.Command("tmux", "split-window", "-t", wid, "-h", "-c", svcDir, "-P", "-F", "#{pane_id}").Output()
			if err != nil {
				continue
			}
			paneID := strings.TrimSpace(string(out))
			time.Sleep(300 * time.Millisecond)
			exec.Command("tmux", "send-keys", "-t", paneID, "export TNT_MAIN_ROOT='"+mainRoot+"'", "Enter").Run()
			if cfg.Env != "" {
				exec.Command("tmux", "send-keys", "-t", paneID, cfg.Env, "Enter").Run()
			}
			for _, setupCmd := range svc.Setup {
				exec.Command("tmux", "send-keys", "-t", paneID, setupCmd, "Enter").Run()
			}
			exec.Command("tmux", "send-keys", "-t", paneID, svc.Run, "Enter").Run()
		}
		count++
	}

	exec.Command("tmux", "select-layout", "-t", wid, "even-horizontal").Run()

	short := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		short = branch[idx+1:]
	}
	exec.Command("tmux", "display-message", fmt.Sprintf("Started %d service(s) in %s:run", count, short)).Run()
}

func runStopServices(session string) {
	wid := findRunWindow(session)
	if wid == "" {
		exec.Command("tmux", "display-message", "No run window found").Run()
		return
	}

	out, err := exec.Command("tmux", "list-panes", "-t", wid, "-F", "#{pane_id}").Output()
	if err != nil {
		return
	}
	panes := strings.Split(strings.TrimSpace(string(out)), "\n")

	for _, p := range panes {
		exec.Command("tmux", "send-keys", "-t", p, "C-c").Run()
	}

	for {
		out, err := exec.Command("tmux", "list-panes", "-t", wid, "-F", "#{pane_id}").Output()
		if err != nil {
			break
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) <= 1 {
			break
		}
		last := lines[len(lines)-1]
		if exec.Command("tmux", "kill-pane", "-t", last).Run() != nil {
			break
		}
	}

	exec.Command("tmux", "display-message", "Stopped services").Run()
}

func runSwitchWorktree(session, workdir, branch, mainRoot string) {
	wid := findRunWindow(session)
	if wid == "" {
		repoName := filepath.Base(mainRoot)
		runStartServices(session, workdir, branch, app.Config.Paths.Projects, repoName, mainRoot)
		return
	}

	for {
		out, err := exec.Command("tmux", "list-panes", "-t", wid, "-F", "#{pane_id}").Output()
		if err != nil {
			break
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) <= 1 {
			break
		}
		last := lines[len(lines)-1]
		if exec.Command("tmux", "kill-pane", "-t", last).Run() != nil {
			break
		}
	}

	pane := firstPane(wid)
	exec.Command("tmux", "respawn-pane", "-k", "-t", pane, "-c", workdir).Run()

	short := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		short = branch[idx+1:]
	}
	exec.Command("tmux", "set-option", "-w", "-t", wid, "@worktree", branch).Run()
	exec.Command("tmux", "set-option", "-w", "-t", wid, "@worktree_color", worktree.WorktreeColor(branch)).Run()
	exec.Command("tmux", "rename-window", "-t", wid, short+":run").Run()
	moveRunWindowFirst(session, wid)

	time.Sleep(300 * time.Millisecond)
	repoName := filepath.Base(mainRoot)
	runStartServices(session, workdir, branch, app.Config.Paths.Projects, repoName, mainRoot)
}

// --- pick TUI ---

type runPickEntry struct {
	short    string
	path     string
	branch   string
	isActive bool
}

type runPickModel struct {
	entries  []runPickEntry
	cursor   int
	theme    *theme.Theme
	session  string
	mainRoot string
	quitting bool
	selected *runPickEntry
	width    int
	height   int
}

func (m runPickModel) Init() tea.Cmd { return nil }

func (m runPickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if m.cursor >= 0 && m.cursor < len(m.entries) {
				e := m.entries[m.cursor]
				m.selected = &e
				m.quitting = true
				return m, tea.Quit
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "ctrl+c"))):
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m runPickModel) View() string {
	if m.quitting {
		return ""
	}
	t := m.theme
	title := lipgloss.NewStyle().Bold(true).Foreground(t.Blue).Padding(0, 1).
		Render("Switch run window to worktree")

	var lines []string
	for i, e := range m.entries {
		label := e.short
		if e.isActive {
			label += lipgloss.NewStyle().Foreground(t.Green).Render(" (active)")
		}
		if i == m.cursor {
			pointer := lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render("> ")
			label = pointer + lipgloss.NewStyle().Foreground(t.Blue).Bold(true).Render(e.short)
			if e.isActive {
				label += lipgloss.NewStyle().Foreground(t.Green).Render(" (active)")
			}
		} else {
			label = "  " + lipgloss.NewStyle().Foreground(t.FG).Render(label)
		}
		lines = append(lines, label)
	}

	if len(m.entries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Gray).Padding(1, 2).Render("No worktrees open."))
	}

	help := lipgloss.NewStyle().Foreground(t.Gray).Padding(0, 2).Render("↵ switch  esc cancel")
	return title + "\n\n" + strings.Join(lines, "\n") + "\n\n" + help
}

func runPickWorktrees(session, mainRoot string) {
	currentWT := ""
	wid := findRunWindow(session)
	if wid != "" {
		if out, err := exec.Command("tmux", "show-option", "-wv", "-t", wid, "@worktree").Output(); err == nil {
			currentWT = strings.TrimSpace(string(out))
		}
	}

	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id}\t#{@worktree}").Output()
	if err != nil {
		exec.Command("tmux", "display-message", "No worktrees open").Run()
		return
	}

	mainBranch := ""
	if out, err := exec.Command("git", "-C", mainRoot, "branch", "--show-current").Output(); err == nil {
		mainBranch = strings.TrimSpace(string(out))
	}

	seen := map[string]bool{}
	var entries []runPickEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		wt := parts[1]
		if seen[wt] {
			continue
		}
		seen[wt] = true

		short := wt
		if idx := strings.LastIndex(wt, "/"); idx >= 0 {
			short = wt[idx+1:]
		}
		wtPath := mainRoot
		if wt != mainBranch {
			candidate := filepath.Join(mainRoot, ".worktrees", wt)
			if _, err := os.Stat(candidate); err == nil {
				wtPath = candidate
			}
		}
		entries = append(entries, runPickEntry{
			short:    short,
			path:     wtPath,
			branch:   wt,
			isActive: wt == currentWT,
		})
	}

	if len(entries) == 0 {
		exec.Command("tmux", "display-message", "No worktrees open").Run()
		return
	}

	m := runPickModel{
		entries:  entries,
		theme:    app.Theme,
		session:  session,
		mainRoot: mainRoot,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return
	}

	final := result.(runPickModel)
	if final.selected == nil {
		return
	}
	if final.selected.branch == currentWT {
		exec.Command("tmux", "display-message", fmt.Sprintf("Already running on %s", final.selected.short)).Run()
		return
	}
	runSwitchWorktree(session, final.selected.path, final.selected.branch, mainRoot)
}

// --- entry point ---

func runRunWindow(args []string) {
	ctx := detectTmuxContext()

	gitRoot := ""
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		gitRoot = strings.TrimSpace(string(out))
	}
	if gitRoot == "" {
		gitRoot = ctx.workdir
	}

	mainRoot := gitRoot
	if out, err := exec.Command("git", "-C", gitRoot, "rev-parse", "--git-common-dir").Output(); err == nil {
		gc := strings.TrimSpace(string(out))
		if gc != ".git" {
			mainRoot = filepath.Dir(gc)
		}
	}
	repoName := filepath.Base(mainRoot)

	workdir := ctx.resolveWorkdir(mainRoot)
	branch := ctx.branch

	action := ""
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "start":
		wd := workdir
		if len(args) > 1 {
			wd = args[1]
		}
		runStartServices(ctx.session, wd, branch, app.Config.Paths.Projects, repoName, mainRoot)
	case "stop":
		runStopServices(ctx.session)
	case "restart":
		runStopServices(ctx.session)
		time.Sleep(time.Second)
		wd := workdir
		if len(args) > 1 {
			wd = args[1]
		}
		runStartServices(ctx.session, wd, branch, app.Config.Paths.Projects, repoName, mainRoot)
	case "switch":
		wd := workdir
		if len(args) > 1 {
			wd = args[1]
		}
		runSwitchWorktree(ctx.session, wd, branch, mainRoot)
	case "pick":
		runPickWorktrees(ctx.session, mainRoot)
	default:
		wid := ensureRunWindow(ctx.session, workdir, branch)
		if wid != "" {
			exec.Command("tmux", "select-window", "-t", wid).Run()
		}
	}
}
