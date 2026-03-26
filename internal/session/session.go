package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/tmux"
)

type PaneType string

const (
	PaneNvim     PaneType = "nvim"
	PaneOpencode PaneType = "opencode"
	PaneShell    PaneType = "shell"
	PaneService  PaneType = "service"
)

type Pane struct {
	Type            PaneType `json:"type"`
	Cwd             string   `json:"cwd"`
	Command         string   `json:"command,omitempty"`
	Socket          string   `json:"socket,omitempty"`
	NvimSocket      string   `json:"nvim_socket,omitempty"`
	SessionFile     string   `json:"session_file,omitempty"`
	OpencodeSession string   `json:"opencode_session,omitempty"`
	OpencodeExport  string   `json:"opencode_export,omitempty"`
}

type Window struct {
	Branch string `json:"branch,omitempty"`
	Layout string `json:"layout,omitempty"`
	Type   string `json:"type,omitempty"`
	Panes  []Pane `json:"panes,omitempty"`
}

type State struct {
	SavedAt time.Time `json:"saved_at"`
	Windows []Window  `json:"windows"`
}

func Load(cfg *config.Config, repoName string) (*State, error) {
	path := filepath.Join(cfg.Paths.Projects, repoName, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func Save(cfg *config.Config, sessionName string) error {
	windows, err := tmux.ListWindows(sessionName, "#{window_id}\t#{window_name}\t#{@worktree}")
	if err != nil {
		return fmt.Errorf("list windows: %w", err)
	}

	var state State
	state.SavedAt = time.Now()

	for _, line := range windows {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}

		windowID := parts[0]
		name := parts[1]
		worktree := ""
		if len(parts) >= 3 {
			worktree = strings.TrimSpace(parts[2])
		}

		if strings.Contains(name, ":run") || name == "run" {
			state.Windows = append(state.Windows, Window{Type: "run"})
			continue
		}

		branch := worktree
		if branch == "" {
			branch = strings.Split(name, ":")[0]
		}

		layout := "dev"
		if strings.HasSuffix(name, ":ai") {
			layout = "ai"
		} else if strings.HasSuffix(name, ":term") {
			layout = "terminal"
		}

		panes := capturePanes(windowID, cfg, sessionName)

		state.Windows = append(state.Windows, Window{
			Branch: branch,
			Layout: layout,
			Panes:  panes,
		})
	}

	dir := filepath.Join(cfg.Paths.Projects, sessionName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "session.json"), data, 0644)
}

func capturePanes(windowID string, cfg *config.Config, sessionName string) []Pane {
	format := "#{pane_id}\t#{pane_current_command}\t#{pane_pid}\t#{pane_current_path}"
	lines, err := tmux.ListPanes(windowID, format)
	if err != nil {
		return nil
	}

	var panes []Pane
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		paneID, cmd, pid, cwd := parts[0], parts[1], parts[2], parts[3]

		pane := Pane{Cwd: cwd}

		switch {
		case cmd == "nvim" || cmd == "vim":
			pane.Type = PaneNvim
			pane.Socket = detectNvimSocket(pid)
			if pane.Socket != "" {
				sf := saveNvimSession(paneID, pane.Socket, cfg, sessionName)
				if sf != "" {
					pane.SessionFile = sf
				}
			}

		case cmd == "opencode":
			pane.Type = PaneOpencode
			pane.NvimSocket = detectEnvVar(pid, "NVIM_SOCKET_PATH")
			pane.OpencodeSession = detectOpencodeSession(pid, cwd)
			if pane.OpencodeSession != "" {
				pane.OpencodeExport = exportOpencodeSession(pane.OpencodeSession, cfg, sessionName)
			}

		default:
			if isOpencode(pid) {
				pane.Type = PaneOpencode
				pane.NvimSocket = detectEnvVar(pid, "NVIM_SOCKET_PATH")
				pane.OpencodeSession = detectOpencodeSession(pid, cwd)
				if pane.OpencodeSession != "" {
					pane.OpencodeExport = exportOpencodeSession(pane.OpencodeSession, cfg, sessionName)
				}
			} else {
				pane.Type = PaneShell
			}
		}

		panes = append(panes, pane)
	}
	return panes
}

func detectNvimSocket(pid string) string {
	if sock := findListenArg(pid); sock != "" {
		return sock
	}
	children, err := exec.Command("pgrep", "-P", pid).Output()
	if err != nil {
		return ""
	}
	for _, cp := range strings.Fields(string(children)) {
		if sock := findListenArg(cp); sock != "" {
			return sock
		}
		grandchildren, err := exec.Command("pgrep", "-P", cp).Output()
		if err != nil {
			continue
		}
		for _, gcp := range strings.Fields(string(grandchildren)) {
			if sock := findListenArg(gcp); sock != "" {
				return sock
			}
		}
	}
	return ""
}

func findListenArg(pid string) string {
	out, err := exec.Command("ps", "-o", "args=", "-p", pid).Output()
	if err != nil {
		return ""
	}
	args := string(out)
	if idx := strings.Index(args, "--listen"); idx >= 0 {
		rest := strings.TrimSpace(args[idx+len("--listen"):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

func detectEnvVar(pid, name string) string {
	prefix := name + "="
	pids := []string{pid}

	children, err := exec.Command("pgrep", "-P", pid).Output()
	if err == nil {
		pids = append(pids, strings.Fields(string(children))...)
	}
	for _, cp := range strings.Fields(string(children)) {
		grandchildren, err := exec.Command("pgrep", "-P", cp).Output()
		if err == nil {
			pids = append(pids, strings.Fields(string(grandchildren))...)
		}
	}

	for _, p := range pids {
		out, err := exec.Command("ps", "-o", "command=", "-p", p).Output()
		if err != nil {
			continue
		}
		for _, part := range strings.Fields(string(out)) {
			if strings.HasPrefix(part, prefix) {
				return strings.Trim(strings.TrimPrefix(part, prefix), "'\"")
			}
		}
	}
	return ""
}

func saveNvimSession(paneID, socket string, cfg *config.Config, sessionName string) string {
	sessDir := filepath.Join(cfg.Paths.Projects, sessionName, "nvim-sessions")
	os.MkdirAll(sessDir, 0755)

	socketBase := filepath.Base(socket)
	sessFile := filepath.Join(sessDir, strings.TrimSuffix(socketBase, ".sock")+".vim")

	exec.Command("tmux", "send-keys", "-t", paneID, "Escape", "Escape").Run()
	exec.Command("tmux", "send-keys", "-t", paneID,
		fmt.Sprintf(":mksession! %s", sessFile), "Enter").Run()

	time.Sleep(200 * time.Millisecond)

	if _, err := os.Stat(sessFile); err == nil {
		return sessFile
	}
	return ""
}

func detectOpencodeSession(panePid string, cwd string) string {
	pids := []string{panePid}
	children, err := exec.Command("pgrep", "-P", panePid).Output()
	if err == nil {
		pids = append(pids, strings.Fields(string(children))...)
	}
	for _, cp := range strings.Fields(string(children)) {
		grandchildren, err := exec.Command("pgrep", "-P", cp).Output()
		if err == nil {
			pids = append(pids, strings.Fields(string(grandchildren))...)
		}
	}

	for _, p := range pids {
		out, err := exec.Command("ps", "-o", "args=", "-p", p).Output()
		if err != nil {
			continue
		}
		args := strings.Fields(string(out))
		for i, arg := range args {
			if (arg == "-s" || arg == "--session") && i+1 < len(args) {
				return args[i+1]
			}
		}
	}

	if cwd != "" {
		dbPath := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "opencode.db")
		dirs := []string{cwd}
		if gitRoot, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
			root := strings.TrimSpace(string(gitRoot))
			if root != cwd {
				dirs = append(dirs, root)
			}
		}
		if gc, err := exec.Command("git", "-C", cwd, "rev-parse", "--git-common-dir").Output(); err == nil {
			gcStr := strings.TrimSpace(string(gc))
			if gcStr != ".git" {
				mainRoot := filepath.Dir(gcStr)
				dirs = append(dirs, mainRoot)
			}
		}
		for _, dir := range dirs {
			out, err := exec.Command("sqlite3", dbPath,
				fmt.Sprintf("SELECT id FROM session WHERE directory='%s' ORDER BY time_updated DESC LIMIT 1", dir)).Output()
			if err == nil {
				sid := strings.TrimSpace(string(out))
				if sid != "" {
					return sid
				}
			}
		}
	}

	return ""
}

func exportOpencodeSession(sessionID string, cfg *config.Config, tmuxSession string) string {
	exportDir := filepath.Join(cfg.Paths.Projects, tmuxSession, "opencode-sessions")
	os.MkdirAll(exportDir, 0755)

	exportPath := filepath.Join(exportDir, sessionID+".json")

	out, err := exec.Command("opencode", "export", sessionID).Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	if err := os.WriteFile(exportPath, out, 0644); err != nil {
		return ""
	}
	return exportPath
}

func isOpencode(pid string) bool {
	out, err := exec.Command("pgrep", "-P", pid, "-f", "opencode").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func Restore(cfg *config.Config, sessionName, repoPath string) error {
	state, err := Load(cfg, sessionName)
	if err != nil {
		return err
	}

	for _, w := range state.Windows {
		if w.Type == "run" {
			continue
		}

		workdir := repoPath
		if w.Branch != "" && w.Branch != "main" && w.Branch != "master" {
			wtDir := filepath.Join(repoPath, cfg.Branch.WorktreeDir, w.Branch)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				workdir = wtDir
			}
		}

		if len(w.Panes) > 0 {
			restoreWithPanes(cfg, sessionName, w, workdir)
		} else {
			layoutScript := filepath.Join(cfg.Paths.Layouts, w.Layout+".sh")
			if _, err := os.Stat(layoutScript); err != nil {
				continue
			}
			cmd := fmt.Sprintf("%s %q %q %q", layoutScript, workdir, sessionName, w.Branch)
			tmux.Run("run-shell", cmd)
		}
	}

	return nil
}

func restoreWithPanes(cfg *config.Config, sessionName string, w Window, workdir string) {
	short := w.Branch
	if idx := strings.LastIndex(w.Branch, "/"); idx >= 0 {
		short = w.Branch[idx+1:]
	}

	windowName := short + ":" + w.Layout
	wid, err := tmux.NewWindow(sessionName, windowName, workdir)
	if err != nil {
		return
	}
	tmux.SetWindowOption(wid, "@worktree", w.Branch)

	socketDir := filepath.Join(cfg.Paths.Projects, sessionName, "sockets")
	os.MkdirAll(socketDir, 0755)

	socketName := strings.ReplaceAll(fmt.Sprintf("%s_%s", sessionName, short), "/", "_")
	socketPath := filepath.Join(socketDir, socketName+".sock")

	for i, p := range w.Panes {
		var paneID string
		if i == 0 {
			out, _ := exec.Command("tmux", "list-panes", "-t", wid, "-F", "#{pane_id}").Output()
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 0 {
				paneID = lines[0]
			}
		} else {
			dir := workdir
			if p.Cwd != "" {
				dir = p.Cwd
			}
			var splitDir string
			if i%2 == 1 {
				splitDir = "-h"
			} else {
				splitDir = "-v"
			}
			out, err := exec.Command("tmux", "split-window", "-t", wid, splitDir, "-c", dir, "-P", "-F", "#{pane_id}").Output()
			if err != nil {
				continue
			}
			paneID = strings.TrimSpace(string(out))
			time.Sleep(300 * time.Millisecond)
		}

		if paneID == "" {
			continue
		}

		switch p.Type {
		case PaneNvim:
			nvimCmd := fmt.Sprintf("nvim --listen '%s'", socketPath)
			if p.SessionFile != "" {
				if _, err := os.Stat(p.SessionFile); err == nil {
					nvimCmd += fmt.Sprintf(" -S '%s'", p.SessionFile)
				}
			} else {
				nvimCmd += " ."
			}
			exec.Command("tmux", "send-keys", "-t", paneID, nvimCmd, "Enter").Run()

		case PaneOpencode:
			ocCmd := fmt.Sprintf("NVIM_SOCKET_PATH='%s' opencode --port", socketPath)
			if p.OpencodeSession != "" {
				ocCmd = fmt.Sprintf("NVIM_SOCKET_PATH='%s' opencode --port -s %s", socketPath, p.OpencodeSession)
			}
			exec.Command("tmux", "send-keys", "-t", paneID, ocCmd, "Enter").Run()

		case PaneShell:
			if p.Cwd != "" && i == 0 {
				exec.Command("tmux", "send-keys", "-t", paneID, "cd '"+p.Cwd+"'", "Enter").Run()
			}
		}
	}

	if w.Layout == "dev" {
		exec.Command("tmux", "select-pane", "-t", wid+".1").Run()
	}
}

func TimeSince(s *State) string {
	d := time.Since(s.SavedAt)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
