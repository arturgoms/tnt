package linear

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/plans"
)

type Issue struct {
	Identifier string
	Title      string
	StateType  string
	StateName  string
	Priority   int
	URL        string
	TeamKey    string
}

type TicketWorktree struct {
	Repo     string
	Branch   string
	Done     int
	Total    int
	HasTasks bool
}

type IssueWithWorktrees struct {
	Issue
	Worktrees []TicketWorktree
}

type RepoInfo struct {
	Name     string
	Path     string
	Branches []string
}

func LoadAPIKey() string {
	home, err := os.UserHomeDir()
	if err == nil {
		envPath := filepath.Join(home, ".config", "tnt", ".env")
		if key := loadAPIKeyFromEnvFile(envPath); key != "" {
			return key
		}
	}
	return strings.TrimSpace(os.Getenv("LINEAR_API_KEY"))
}

func loadAPIKeyFromEnvFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if !strings.HasPrefix(line, "LINEAR_KEY=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "LINEAR_KEY="))
		value = strings.Trim(value, `"'`)
		if value != "" {
			return value
		}
	}
	return ""
}

func LoadMyIssues(apiKey string) ([]Issue, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return []Issue{}, nil
	}

	payload := map[string]string{
		"query": `{
			viewer {
				assignedIssues(
					filter: { state: { type: { in: ["started", "unstarted"] } } }
					orderBy: updatedAt
					first: 30
				) {
					nodes {
						identifier
						title
						url
						priority
						state { name type }
						team { key }
					}
				}
			}
		}`,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, "https://api.linear.app/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Data struct {
			Viewer struct {
				AssignedIssues struct {
					Nodes []struct {
						Identifier string `json:"identifier"`
						Title      string `json:"title"`
						URL        string `json:"url"`
						Priority   int    `json:"priority"`
						State      struct {
							Name string `json:"name"`
							Type string `json:"type"`
						} `json:"state"`
						Team struct {
							Key string `json:"key"`
						} `json:"team"`
					} `json:"nodes"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Errors) > 0 {
		return []Issue{}, nil
	}

	issues := make([]Issue, 0, len(parsed.Data.Viewer.AssignedIssues.Nodes))
	for _, node := range parsed.Data.Viewer.AssignedIssues.Nodes {
		issues = append(issues, Issue{
			Identifier: node.Identifier,
			Title:      node.Title,
			StateType:  node.State.Type,
			StateName:  node.State.Name,
			Priority:   node.Priority,
			URL:        node.URL,
			TeamKey:    node.Team.Key,
		})
	}

	sort.SliceStable(issues, func(i, j int) bool {
		si := stateNameRank(issues[i].StateName)
		sj := stateNameRank(issues[j].StateName)
		if si != sj {
			return si < sj
		}
		pi := priorityRank(issues[i].Priority)
		pj := priorityRank(issues[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return issues[i].Identifier < issues[j].Identifier
	})

	return issues, nil
}

func MatchWorktrees(issues []Issue, allRepos []RepoInfo) []IssueWithWorktrees {
	home, _ := os.UserHomeDir()
	tasksDir := filepath.Join(home, ".config", "opencode", "tasks")

	result := make([]IssueWithWorktrees, 0, len(issues))
	for _, issue := range issues {
		identifier := strings.ToLower(issue.Identifier)
		matched := []TicketWorktree{}

		for _, repo := range allRepos {
			branchProgress := plans.LoadForRepo(tasksDir, repo.Name)
			progressByBranch := map[string]plans.BranchProgress{}
			for _, p := range branchProgress {
				progressByBranch[strings.ToLower(p.Branch)] = p
			}

			for _, branch := range repo.Branches {
				if !strings.Contains(strings.ToLower(branch), identifier) {
					continue
				}

				wt := TicketWorktree{Repo: repo.Name, Branch: branch}
				if p, ok := progressByBranch[strings.ToLower(branch)]; ok {
					wt.Done = p.Done
					wt.Total = p.Total
					wt.HasTasks = p.Total > 0
				}
				matched = append(matched, wt)
			}
		}

		sort.SliceStable(matched, func(i, j int) bool {
			if matched[i].Repo != matched[j].Repo {
				return matched[i].Repo < matched[j].Repo
			}
			return matched[i].Branch < matched[j].Branch
		})

		result = append(result, IssueWithWorktrees{Issue: issue, Worktrees: matched})
	}

	return result
}

func stateRank(stateType string) int {
	switch strings.ToLower(stateType) {
	case "started":
		return 0
	case "unstarted":
		return 1
	default:
		return 2
	}
}

func stateNameRank(stateName string) int {
	switch strings.ToLower(stateName) {
	case "in progress":
		return 0
	case "in review":
		return 1
	case "todo":
		return 2
	default:
		return 3
	}
}

func priorityRank(priority int) int {
	if priority <= 0 {
		return 99
	}
	return priority
}
