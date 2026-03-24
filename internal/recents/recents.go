package recents

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const maxRecents = 20

type List struct {
	Repos []string `json:"repos"`
}

func Load(stateDir string) *List {
	path := filepath.Join(stateDir, "recents.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &List{}
	}
	var l List
	if err := json.Unmarshal(data, &l); err != nil {
		return &List{}
	}
	return &l
}

func (l *List) Add(name string, stateDir string) {
	filtered := []string{name}
	for _, r := range l.Repos {
		if r != name {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) > maxRecents {
		filtered = filtered[:maxRecents]
	}
	l.Repos = filtered

	path := filepath.Join(stateDir, "recents.json")
	data, _ := json.MarshalIndent(l, "", "  ")
	os.MkdirAll(stateDir, 0755)
	os.WriteFile(path, data, 0644)
}

func (l *List) Index(name string) int {
	for i, r := range l.Repos {
		if r == name {
			return i
		}
	}
	return -1
}
