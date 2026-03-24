package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/tmux"
)

type Window struct {
	Branch string `json:"branch,omitempty"`
	Layout string `json:"layout,omitempty"`
	Type   string `json:"type,omitempty"`
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
	windows, err := tmux.ListWindows(sessionName, "#{window_name}\t#{pane_current_path}\t#{@worktree}")
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

		name := parts[0]
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

		state.Windows = append(state.Windows, Window{
			Branch: branch,
			Layout: layout,
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

func Restore(cfg *config.Config, sessionName, repoPath string) error {
	state, err := Load(cfg, sessionName)
	if err != nil {
		return err
	}

	for _, w := range state.Windows {
		if w.Type == "run" {
			continue
		}

		layoutScript := filepath.Join(cfg.Paths.Layouts, w.Layout+".sh")
		if _, err := os.Stat(layoutScript); err != nil {
			continue
		}

		workdir := repoPath
		if w.Branch != "" && w.Branch != "main" && w.Branch != "master" {
			wtDir := filepath.Join(repoPath, cfg.Branch.WorktreeDir, w.Branch)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				workdir = wtDir
			}
		}

		cmd := fmt.Sprintf("%s %q %q %q", layoutScript, workdir, sessionName, w.Branch)
		if _, err := tmux.Run("run-shell", cmd); err != nil {
			continue
		}
	}

	return nil
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
