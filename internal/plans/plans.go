package plans

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type Task struct {
	ID       string                 `json:"id"`
	Subject  string                 `json:"subject"`
	Status   string                 `json:"status"`
	Metadata map[string]interface{} `json:"metadata"`
}

type BranchProgress struct {
	Branch string
	Tasks  []Task
	Done   int
	Total  int
}

func LoadForRepo(tasksDir, repo string) []BranchProgress {
	if tasksDir == "" || repo == "" {
		return []BranchProgress{}
	}

	repoDir := filepath.Join(tasksDir, repo)
	if info, err := os.Stat(repoDir); err != nil || !info.IsDir() {
		return []BranchProgress{}
	}

	files, err := filepath.Glob(filepath.Join(repoDir, "T-*.json"))
	if err != nil || len(files) == 0 {
		return []BranchProgress{}
	}

	byBranch := map[string][]Task{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}

		branch := "unknown"
		if task.Metadata != nil {
			if raw, ok := task.Metadata["branch"]; ok {
				if value, ok := raw.(string); ok && value != "" {
					branch = value
				}
			}
		}

		byBranch[branch] = append(byBranch[branch], task)
	}

	if len(byBranch) == 0 {
		return []BranchProgress{}
	}

	branches := make([]string, 0, len(byBranch))
	for branch := range byBranch {
		branches = append(branches, branch)
	}
	sort.Strings(branches)

	result := make([]BranchProgress, 0, len(branches))
	for _, branch := range branches {
		tasks := byBranch[branch]
		sort.SliceStable(tasks, func(i, j int) bool {
			return tasks[i].Subject < tasks[j].Subject
		})

		done := 0
		total := 0
		for _, task := range tasks {
			if task.Status == "deleted" {
				continue
			}
			total++
			if task.Status == "completed" {
				done++
			}
		}

		result = append(result, BranchProgress{
			Branch: branch,
			Tasks:  tasks,
			Done:   done,
			Total:  total,
		})
	}

	return result
}
