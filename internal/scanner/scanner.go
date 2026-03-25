package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/tmux"
)

type Repo struct {
	Name          string
	Path          string
	Group         string
	Workspace     string
	HasSession    bool
	SavedWindows  int
	BranchCount   int
	LastActivity  string
	CurrentBranch string
}

func Scan(cfg *config.Config) []Repo {
	seen := map[string]bool{}
	var repos []Repo
	sessions := activeSessions()

	for _, ws := range cfg.Workspaces {
		for _, dir := range ws.Dirs {
			scanDir(dir, ws.Name, cfg, seen, sessions, &repos)
		}
	}

	for _, dir := range cfg.Search.Dirs {
		scanDir(dir, "", cfg, seen, sessions, &repos)
	}

	sort.Slice(repos, func(i, j int) bool {
		if repos[i].HasSession != repos[j].HasSession {
			return repos[i].HasSession
		}
		return repos[i].Name < repos[j].Name
	})

	return repos
}

func scanDir(dir, workspace string, cfg *config.Config, seen map[string]bool, sessions map[string]bool, repos *[]Repo) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	group := groupName(dir)
	if workspace != "" {
		group = workspace
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		real, err := filepath.EvalSymlinks(full)
		if err != nil {
			continue
		}

		if !isGitRepo(real) {
			if cfg.Search.MaxDepth > 1 {
				scanNested(real, group, workspace, cfg.Search.MaxDepth-1, seen, sessions, cfg, repos)
			}
			continue
		}

		if seen[real] {
			continue
		}
		seen[real] = true

		name := sessionName(entry.Name())
		branch, bc, la := repoGitInfo(real)
		*repos = append(*repos, Repo{
			Name:          name,
			Path:          real,
			Group:         group,
			Workspace:     workspace,
			HasSession:    sessions[name],
			SavedWindows:  countSavedWindows(cfg, name),
			BranchCount:   bc,
			LastActivity:  la,
			CurrentBranch: branch,
		})
	}
}

func scanNested(dir, group, workspace string, depth int, seen map[string]bool, sessions map[string]bool, cfg *config.Config, repos *[]Repo) {
	if depth <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		real, err := filepath.EvalSymlinks(full)
		if err != nil {
			continue
		}
		if !isGitRepo(real) {
			continue
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		name := sessionName(entry.Name())
		branch, bc, la := repoGitInfo(real)
		*repos = append(*repos, Repo{
			Name:          name,
			Path:          real,
			Group:         group,
			Workspace:     workspace,
			HasSession:    sessions[name],
			SavedWindows:  countSavedWindows(cfg, name),
			BranchCount:   bc,
			LastActivity:  la,
			CurrentBranch: branch,
		})
	}
}

func isGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func sessionName(basename string) string {
	return strings.ReplaceAll(basename, ".", "_")
}

func activeSessions() map[string]bool {
	out, err := tmux.Run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil
	}
	m := map[string]bool{}
	for _, s := range strings.Split(out, "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			m[s] = true
		}
	}
	return m
}

func countSavedWindows(cfg *config.Config, name string) int {
	path := filepath.Join(cfg.Paths.Projects, name, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), `"branch"`) + strings.Count(string(data), `"type"`)
}

func groupName(dir string) string {
	home := os.Getenv("HOME")
	rel := strings.TrimPrefix(dir, home+"/")
	rel = strings.TrimSuffix(rel, "/")

	parts := strings.Split(rel, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return rel
}

func repoGitInfo(path string) (branch string, branchCount int, lastActivity string) {
	if out, err := exec.Command("git", "-C", path, "branch", "--show-current").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	if branch == "" {
		branch = "main"
	}

	branchCount = 1
	wtDir := filepath.Join(path, ".worktrees")
	if entries, err := os.ReadDir(wtDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				branchCount++
			}
		}
	}

	if out, err := exec.Command("git", "-C", path, "log", "-1", "--all", "--format=%cr").Output(); err == nil {
		lastActivity = strings.TrimSpace(string(out))
	}

	return
}

func WorkspaceNames(cfg *config.Config) []string {
	var names []string
	for _, ws := range cfg.Workspaces {
		names = append(names, ws.Name)
	}
	return names
}
