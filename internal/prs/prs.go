package prs

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type CheckRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Name       string `json:"name"`
}

type Author struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type Notification struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	URL   string `json:"url"`
}

type PR struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	Branch         string     `json:"headRefName"`
	State          string     `json:"state"`
	IsDraft        bool       `json:"isDraft"`
	ReviewDecision string     `json:"reviewDecision"`
	Checks         []CheckRun `json:"statusCheckRollup"`
	Author         Author     `json:"author"`
}

func LoadForRepo(repoPath string) ([]PR, error) {
	cmd := exec.Command("gh", "pr", "list", "--author", "@me", "--json", "number,title,headRefName,state,isDraft,reviewDecision,author", "--limit", "20")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return []PR{}, err
	}

	var list []PR
	if err := json.Unmarshal(out, &list); err != nil {
		return []PR{}, err
	}

	return list, nil
}

func LoadChecksForPR(repoPath string, number int) ([]CheckRun, error) {
	cmd := exec.Command("gh", "pr", "view", fmt.Sprintf("%d", number), "--json", "statusCheckRollup")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result struct {
		Checks []CheckRun `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	return result.Checks, nil
}

func LoadReviewRequested(repoPath string) ([]PR, error) {
	cmd := exec.Command("gh", "pr", "list", "--search", "review-requested:@me", "--json", "number,title,headRefName,state,author", "--limit", "20")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return []PR{}, err
	}

	var list []PR
	if err := json.Unmarshal(out, &list); err != nil {
		return []PR{}, err
	}

	return list, nil
}

func LoadNotifications() ([]Notification, error) {
	cmd := exec.Command("gh", "api", "notifications", "--jq", ".[].subject | {title, type, url}")
	out, err := cmd.Output()
	if err != nil {
		return []Notification{}, err
	}

	var list []Notification
	if len(out) == 0 {
		return []Notification{}, nil
	}

	trimmed := string(out)
	if trimmed == "" {
		return []Notification{}, nil
	}

	if trimmed[0] == '{' {
		lines := []string{}
		for _, line := range splitLines(trimmed) {
			if line != "" {
				lines = append(lines, line)
			}
		}
		for _, line := range lines {
			var n Notification
			if err := json.Unmarshal([]byte(line), &n); err != nil {
				return []Notification{}, err
			}
			list = append(list, n)
		}
		return list, nil
	}

	if err := json.Unmarshal(out, &list); err != nil {
		return []Notification{}, err
	}

	return list, nil
}

func splitLines(s string) []string {
	start := 0
	var lines []string
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s)-1 {
		lines = append(lines, s[start:])
	}
	return lines
}

func ChecksSummary(pr PR) (passed, failed, pending int) {
	for _, check := range pr.Checks {
		if check.Status == "COMPLETED" {
			if check.Conclusion == "SUCCESS" {
				passed++
			} else {
				failed++
			}
			continue
		}
		pending++
	}
	return passed, failed, pending
}

func ChecksIcon(passed, failed, pending int) (icon string, color string) {
	if passed == 0 && failed == 0 && pending == 0 {
		return "", ""
	}
	if failed > 0 {
		return "✗", "red"
	}
	if pending > 0 {
		return "◑", "yellow"
	}
	return "✓", "green"
}

func ReviewIcon(decision string, isDraft bool) (icon string, label string, color string) {
	if isDraft {
		return "◌", "draft", "gray"
	}

	switch decision {
	case "APPROVED":
		return "✓", "approved", "green"
	case "CHANGES_REQUESTED":
		return "●", "changes", "orange"
	case "REVIEW_REQUIRED":
		return "⏳", "review", "gray"
	default:
		return "", "", ""
	}
}
