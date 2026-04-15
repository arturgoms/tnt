package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arturgoms/tnt/internal/recents"
)

func runClose(args []string) {
	cfg := app.Config

	session := ""
	if out, err := exec.Command("tmux", "display-message", "-p", "#S").Output(); err == nil {
		session = strings.TrimSpace(string(out))
	}
	if session == "" {
		fmt.Fprintln(os.Stderr, "not in a tmux session")
		os.Exit(1)
	}

	branch := ""
	if len(args) > 0 {
		branch = args[0]
	} else {
		if out, err := exec.Command("tmux", "display-message", "-p", "#{@worktree}").Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}
	}

	if branch == "" {
		exec.Command("tmux", "display-message", "No worktree tag on current window").Run()
		os.Exit(1)
	}

	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id} #{@worktree}").Output()
	if err != nil {
		exec.Command("tmux", "display-message", fmt.Sprintf("No windows found for worktree: %s", branch)).Run()
		return
	}

	var worktreeWindows []string
	totalWindows := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		totalWindows++
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && parts[1] == branch {
			worktreeWindows = append(worktreeWindows, parts[0])
		}
	}

	if len(worktreeWindows) == 0 {
		exec.Command("tmux", "display-message", fmt.Sprintf("No windows found for worktree: %s", branch)).Run()
		return
	}

	remainingAfter := totalWindows - len(worktreeWindows)

	if remainingAfter == 0 {
		recentList := recents.Load(cfg.Paths.State)
		sessions, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
		activeSet := map[string]bool{}
		for _, s := range strings.Split(strings.TrimSpace(string(sessions)), "\n") {
			if s != "" && s != session {
				activeSet[s] = true
			}
		}
		next := ""
		for _, name := range recentList.Repos {
			if activeSet[name] {
				next = name
				break
			}
		}
		if next == "" {
			for name := range activeSet {
				next = name
				break
			}
		}
		if next != "" {
			exec.Command("tmux", "switch-client", "-t", next).Run()
		}
		exec.Command("tmux", "kill-session", "-t", session).Run()
	} else {
		for i := len(worktreeWindows) - 1; i >= 0; i-- {
			exec.Command("tmux", "kill-window", "-t", worktreeWindows[i]).Run()
		}
		exec.Command("tmux", "display-message", fmt.Sprintf("Closed %d window(s) for %s", len(worktreeWindows), branch)).Run()
	}

	short := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		short = branch[idx+1:]
	}
	socketName := strings.ReplaceAll(fmt.Sprintf("%s_%s", session, short), "/", "_")
	socketPath := filepath.Join(cfg.Paths.Projects, session, "sockets", socketName+".sock")
	os.Remove(socketPath)

	nvimSession := filepath.Join(cfg.Paths.Projects, session, "nvim-sessions", socketName+".vim")
	os.Remove(nvimSession)

	gitRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return
	}
	root := strings.TrimSpace(string(gitRoot))
	repoName := filepath.Base(root)
	projCfg := loadProjectConfig(app.Config.Paths.Projects, repoName)
	wtPath := filepath.Join(root, ".worktrees", branch)
	if _, err := os.Stat(wtPath); err == nil {
		runHooks(projCfg.Hooks.PreDelete, wtPath)
		postDeleteCmds := ""
		for _, cmd := range projCfg.Hooks.PostDelete {
			escaped := strings.ReplaceAll(cmd, "'", "'\\''")
			postDeleteCmds += fmt.Sprintf("; sh -c '%s'", escaped)
		}
		confirmCmd := fmt.Sprintf(
			"run-shell \"git -C '%s' worktree remove '%s' --force 2>/dev/null; git -C '%s' branch -d '%s' 2>/dev/null%s; tmux display-message 'Removed worktree: %s'\"",
			root, wtPath, root, branch, postDeleteCmds, branch,
		)
		exec.Command("tmux", "confirm-before", "-p",
			fmt.Sprintf("Remove worktree '%s' from disk? (y/n)", branch),
			confirmCmd,
		).Run()
	}
}
