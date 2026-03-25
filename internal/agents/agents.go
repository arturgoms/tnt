package agents

import (
	"os/exec"
	"strings"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusWaiting Status = "waiting"
	StatusIdle    Status = "idle"
)

type Agent struct {
	Session    string
	WindowName string
	Target     string
	Status     Status
	Branch     string
	Workdir    string
}

func Detect(sessionFilter string) []Agent {
	format := "#{session_name}:#{window_index}.#{pane_index}\t#{pane_pid}\t#{pane_current_command}\t#{session_name}\t#{window_name}\t#{pane_current_path}"
	args := []string{"list-panes", "-a", "-F", format}
	if sessionFilter != "" {
		args = []string{"list-panes", "-t", sessionFilter, "-F", format}
	}

	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return nil
	}

	var result []Agent
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 6 {
			continue
		}
		target, pid, cmd, session, winName, panePath := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]

		if !isOpencode(cmd, pid) {
			continue
		}

		status := detectPaneStatus(target)
		branch := detectBranch(panePath)
		workdir := panePath
		if idx := strings.LastIndex(panePath, "/"); idx >= 0 {
			workdir = panePath[idx+1:]
		}

		result = append(result, Agent{
			Session:    session,
			WindowName: winName,
			Target:     target,
			Status:     status,
			Branch:     branch,
			Workdir:    workdir,
		})
	}
	return result
}

func isOpencode(cmd, pid string) bool {
	if cmd == "opencode" {
		return true
	}
	out, err := exec.Command("pgrep", "-P", pid, "-f", "opencode").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func detectPaneStatus(target string) Status {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-30").Output()
	if err != nil {
		return StatusIdle
	}
	content := string(out)

	if strings.Contains(content, "esc to interrupt") ||
		strings.Contains(content, "Esc to interrupt") ||
		containsBrailleSpinners(content) {
		return StatusRunning
	}

	lines := strings.Split(content, "\n")
	start := len(lines) - 15
	if start < 0 {
		start = 0
	}
	tail := strings.Join(lines[start:], "\n")
	lower := strings.ToLower(tail)

	if strings.Contains(lower, "(y/n)") ||
		strings.Contains(lower, "[y/n]") ||
		strings.Contains(lower, "continue?") ||
		strings.Contains(lower, "proceed?") ||
		strings.Contains(lower, "approve") ||
		strings.Contains(lower, "allow") ||
		strings.Contains(lower, "enter to select") ||
		strings.Contains(lower, "esc to cancel") {
		return StatusWaiting
	}

	if strings.Contains(tail, "❯") {
		for _, l := range lines[start:] {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "❯") && len(trimmed) > 2 {
				c := trimmed[len("❯ "):]
				if len(c) > 0 && c[0] >= '1' && c[0] <= '3' && (len(c) == 1 || c[1] == '.') {
					return StatusWaiting
				}
			}
		}
	}

	return StatusIdle
}

func containsBrailleSpinners(s string) bool {
	for _, r := range s {
		if r >= '⠋' && r <= '⠿' {
			return true
		}
	}
	return false
}

func detectBranch(path string) string {
	if path == "" {
		return "?"
	}
	out, err := exec.Command("git", "-C", path, "branch", "--show-current").Output()
	if err != nil {
		return "?"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "?"
	}
	return branch
}

func CountByStatus(agents []Agent) (running, waiting, idle int) {
	for _, a := range agents {
		switch a.Status {
		case StatusRunning:
			running++
		case StatusWaiting:
			waiting++
		case StatusIdle:
			idle++
		}
	}
	return
}
