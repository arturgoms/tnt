package todos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Todo struct {
	ID          string  `json:"id"`
	Text        string  `json:"text"`
	Description string  `json:"description"`
	Project     string  `json:"project"`
	Done        bool    `json:"done"`
	Created     string  `json:"created"`
	RemindAt    *string `json:"remind_at"`
}

type TodoFile struct {
	Todos []Todo `json:"todos"`
}

type ProjectGroup struct {
	Name   string
	Active []Todo
	Done   int
}

func Load(stateDir string) []ProjectGroup {
	path := filepath.Join(stateDir, "todos.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var file TodoFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}

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
			g.Done++
		} else {
			g.Active = append(g.Active, t)
		}
	}

	// Sort: projects with active todos first, then by name
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

func ActiveCount(groups []ProjectGroup) int {
	count := 0
	for _, g := range groups {
		count += len(g.Active)
	}
	return count
}
