package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arturgomes/tnt/internal/config"
	"github.com/arturgomes/tnt/internal/tmux"
)

type Repo struct {
	Name         string
	Path         string
	Group        string
	HasSession   bool
	SavedWindows int
}

func Scan(cfg *config.Config) []Repo {
	seen := map[string]bool{}
	var repos []Repo

	sessions := activeSessions()

	for _, dir := range cfg.Search.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		group := groupName(dir)

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
					scanNested(real, group, cfg.Search.MaxDepth-1, seen, sessions, cfg, &repos)
				}
				continue
			}

			if seen[real] {
				continue
			}
			seen[real] = true

			name := sessionName(entry.Name())
			repos = append(repos, Repo{
				Name:         name,
				Path:         real,
				Group:        group,
				HasSession:   sessions[name],
				SavedWindows: countSavedWindows(cfg, name),
			})
		}
	}

	sort.Slice(repos, func(i, j int) bool {
		if repos[i].HasSession != repos[j].HasSession {
			return repos[i].HasSession
		}
		return repos[i].Name < repos[j].Name
	})

	return repos
}

func scanNested(dir, group string, depth int, seen map[string]bool, sessions map[string]bool, cfg *config.Config, repos *[]Repo) {
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
		*repos = append(*repos, Repo{
			Name:         name,
			Path:         real,
			Group:        group,
			HasSession:   sessions[name],
			SavedWindows: countSavedWindows(cfg, name),
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
