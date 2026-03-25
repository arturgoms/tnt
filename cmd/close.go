package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	var windowIDs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 && parts[1] == branch {
			windowIDs = append(windowIDs, parts[0])
		}
	}

	if len(windowIDs) == 0 {
		exec.Command("tmux", "display-message", fmt.Sprintf("No windows found for worktree: %s", branch)).Run()
		return
	}

	for i := len(windowIDs) - 1; i >= 0; i-- {
		exec.Command("tmux", "kill-window", "-t", windowIDs[i]).Run()
	}

	exec.Command("tmux", "display-message", fmt.Sprintf("Closed %d window(s) for worktree: %s", len(windowIDs), branch)).Run()

	short := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		short = branch[idx+1:]
	}
	socketName := strings.ReplaceAll(fmt.Sprintf("%s_%s", session, short), "/", "_")
	socketPath := filepath.Join(cfg.Paths.Projects, session, "sockets", socketName+".sock")
	os.Remove(socketPath)

	gitRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return
	}
	root := strings.TrimSpace(string(gitRoot))
	wtPath := filepath.Join(root, ".worktrees", branch)
	if _, err := os.Stat(wtPath); err == nil {
		confirmCmd := fmt.Sprintf(
			"run-shell \"git -C '%s' worktree remove '%s' --force 2>/dev/null; git -C '%s' branch -d '%s' 2>/dev/null; tmux display-message 'Removed worktree: %s'\"",
			root, wtPath, root, branch, branch,
		)
		exec.Command("tmux", "confirm-before", "-p",
			fmt.Sprintf("Remove worktree '%s' from disk? (y/n)", branch),
			confirmCmd,
		).Run()
	}
}
