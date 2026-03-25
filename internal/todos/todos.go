package todos

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Source struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	LineEnd  *int   `json:"line_end,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Worktree string `json:"worktree,omitempty"`
}

type Todo struct {
	ID          string  `json:"id"`
	Text        string  `json:"text"`
	Description string  `json:"description"`
	Project     string  `json:"project"`
	Done        bool    `json:"done"`
	Created     string  `json:"created"`
	RemindAt    *string `json:"remind_at"`
	Source      *Source `json:"source"`
}

type TodoFile struct {
	Todos []Todo `json:"todos"`
}

type ProjectGroup struct {
	Name   string
	Active []Todo
	Done   []Todo
}

func DoneCount(g ProjectGroup) int {
	return len(g.Done)
}

func RepoActiveCount(groups []RepoGroup) int {
	count := 0
	for _, rg := range groups {
		for _, wt := range rg.Worktrees {
			count += len(wt.Active)
		}
	}
	return count
}

// LoadRaw reads the raw TodoFile for mutation.
func LoadRaw(stateDir string) (*TodoFile, error) {
	path := filepath.Join(stateDir, "todos.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TodoFile{}, nil
		}
		return nil, err
	}

	var file TodoFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return &file, nil
}

// Save writes the TodoFile atomically.
func Save(stateDir string, file *TodoFile) error {
	dir := stateDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	tmp := filepath.Join(dir, "todos.json.tmp")
	target := filepath.Join(dir, "todos.json")

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// GenID generates an 8-char hex ID from current timestamp.
func GenID() string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}

// DefaultProject detects project name from tmux session + worktree.
func DefaultProject() string {
	session, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return "inbox"
	}
	sess := strings.TrimSpace(string(session))
	if sess == "" {
		return "inbox"
	}

	worktree, err := exec.Command("tmux", "display-message", "-p", "#{@worktree}").Output()
	if err != nil {
		return sess
	}
	wt := strings.TrimSpace(string(worktree))
	if wt == "" {
		return sess
	}
	short := wt
	if idx := strings.LastIndex(wt, "/"); idx >= 0 {
		short = wt[idx+1:]
	}
	return sess + "/" + short
}

// Add creates a new todo and appends it to the file.
func Add(file *TodoFile, text, project, description string) *Todo {
	if project == "" {
		project = "inbox"
	}
	t := Todo{
		ID:          GenID(),
		Text:        text,
		Description: description,
		Project:     project,
		Done:        false,
		Created:     time.Now().Format(time.RFC3339),
	}
	file.Todos = append(file.Todos, t)
	return &t
}

// Toggle flips the done state of a todo.
func Toggle(file *TodoFile, id string) (*Todo, bool) {
	for i := range file.Todos {
		if file.Todos[i].ID == id {
			file.Todos[i].Done = !file.Todos[i].Done
			return &file.Todos[i], true
		}
	}
	return nil, false
}

// Delete removes a todo by ID.
func Delete(file *TodoFile, id string) bool {
	for i, t := range file.Todos {
		if t.ID == id {
			file.Todos = append(file.Todos[:i], file.Todos[i+1:]...)
			return true
		}
	}
	return false
}

// FindByID returns a pointer to the todo with the given ID.
func FindByID(file *TodoFile, id string) *Todo {
	for i := range file.Todos {
		if file.Todos[i].ID == id {
			return &file.Todos[i]
		}
	}
	return nil
}

// Group organizes todos into ProjectGroups (including done items).
func Group(file *TodoFile) []ProjectGroup {
	groups := map[string]*ProjectGroup{}
	var order []string

	for _, t := range file.Todos {
		proj := t.Project
		if proj == "" {
			proj = "inbox"
		}

		g, ok := groups[proj]
		if !ok {
			g = &ProjectGroup{Name: proj}
			groups[proj] = g
			order = append(order, proj)
		}

		if t.Done {
			g.Done = append(g.Done, t)
		} else {
			g.Active = append(g.Active, t)
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		ai := len(groups[order[i]].Active)
		aj := len(groups[order[j]].Active)
		if ai > 0 && aj == 0 {
			return true
		}
		if ai == 0 && aj > 0 {
			return false
		}
		return strings.ToLower(order[i]) < strings.ToLower(order[j])
	})

	var result []ProjectGroup
	for _, name := range order {
		result = append(result, *groups[name])
	}
	return result
}

type WorktreeGroup struct {
	Name    string
	Project string
	Active  []Todo
	Done    []Todo
}

type RepoGroup struct {
	Name      string
	Worktrees []WorktreeGroup
	DoneTotal int
}

func SplitProject(project string) (repo, worktree string) {
	if idx := strings.Index(project, "/"); idx >= 0 {
		return project[:idx], project[idx+1:]
	}
	return project, ""
}

func GroupByRepo(file *TodoFile) []RepoGroup {
	groups := Group(file)

	repoMap := map[string]*RepoGroup{}
	var repoOrder []string

	for _, g := range groups {
		if len(g.Active) == 0 && len(g.Done) == 0 {
			continue
		}
		repo, wt := SplitProject(g.Name)
		rg, ok := repoMap[repo]
		if !ok {
			rg = &RepoGroup{Name: repo}
			repoMap[repo] = rg
			repoOrder = append(repoOrder, repo)
		}
		rg.Worktrees = append(rg.Worktrees, WorktreeGroup{
			Name:    wt,
			Project: g.Name,
			Active:  g.Active,
			Done:    g.Done,
		})
		rg.DoneTotal += len(g.Done)
	}

	var result []RepoGroup
	for _, name := range repoOrder {
		result = append(result, *repoMap[name])
	}
	return result
}

func Load(stateDir string) []RepoGroup {
	file, err := LoadRaw(stateDir)
	if err != nil {
		return nil
	}
	return GroupByRepo(file)
}

// ActiveCount returns the total active (non-done) todo count.
func ActiveCount(groups []ProjectGroup) int {
	count := 0
	for _, g := range groups {
		count += len(g.Active)
	}
	return count
}
