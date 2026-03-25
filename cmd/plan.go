package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arturgomes/tnt/internal/agents"
)

type planFlags struct {
	status      string
	changed     string
	next        string
	questionFor string
	question    string
	answer      string
	decision    string
	blockedOn   string
	blocked     string
	handoff     string
	stepNum     string
	stepCommit  string
}

func parsePlanFlags(args []string) planFlags {
	f := planFlags{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 < len(args) {
				f.status = args[i+1]
				i++
			}
		case "--changed":
			if i+1 < len(args) {
				f.changed = args[i+1]
				i++
			}
		case "--next":
			if i+1 < len(args) {
				f.next = args[i+1]
				i++
			}
		case "--question":
			if i+2 < len(args) {
				f.questionFor = args[i+1]
				f.question = args[i+2]
				i += 2
			}
		case "--answer":
			if i+1 < len(args) {
				f.answer = args[i+1]
				i++
			}
		case "--decision":
			if i+1 < len(args) {
				f.decision = args[i+1]
				i++
			}
		case "--blocked":
			if i+2 < len(args) {
				f.blockedOn = args[i+1]
				f.blocked = args[i+2]
				i += 2
			}
		case "--handoff":
			if i+1 < len(args) {
				f.handoff = args[i+1]
				i++
			}
		case "--step":
			if i+1 < len(args) {
				f.stepNum = args[i+1]
				i++
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
					f.stepCommit = args[i+1]
					i++
				}
			}
		}
	}
	return f
}

func (f planFlags) hasContent() bool {
	return f.status != "" || f.changed != "" || f.next != "" ||
		f.question != "" || f.answer != "" || f.decision != "" ||
		f.blocked != "" || f.handoff != "" || f.stepNum != ""
}

func detectRepoContext() (repo, worktree, gitRoot string, err error) {
	out, e := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if e != nil {
		return "", "", "", fmt.Errorf("not in a git repo")
	}
	gitRoot = strings.TrimSpace(string(out))

	gc, e := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if e == nil {
		gcStr := strings.TrimSpace(string(gc))
		if gcStr != ".git" {
			repo = filepath.Base(filepath.Dir(gcStr))
		} else {
			repo = filepath.Base(gitRoot)
		}
	} else {
		repo = filepath.Base(gitRoot)
	}

	out, e = exec.Command("git", "branch", "--show-current").Output()
	if e != nil {
		return "", "", "", fmt.Errorf("no branch")
	}
	worktree = strings.TrimSpace(string(out))
	if worktree == "" {
		return "", "", "", fmt.Errorf("no branch")
	}

	return repo, worktree, gitRoot, nil
}

func markStepComplete(planFile, stepNum, commit string) error {
	data, err := os.ReadFile(planFile)
	if err != nil {
		return fmt.Errorf("no plan at %s", planFile)
	}

	lines := strings.Split(string(data), "\n")
	found := false
	pattern := fmt.Sprintf("- [ ] **Step %s ", stepNum)

	for i, line := range lines {
		if strings.Contains(line, pattern) {
			if commit != "" {
				lines[i] = strings.Replace(line, "- [ ] ", fmt.Sprintf("- [x] (%s) ", commit), 1)
			} else {
				lines[i] = strings.Replace(line, "- [ ] ", "- [x] ", 1)
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("step %s not found in plan", stepNum)
	}

	return os.WriteFile(planFile, []byte(strings.Join(lines, "\n")), 0644)
}

func buildCommsEntry(repo, wt string, f planFlags) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Repo**: %s\n", repo))
	b.WriteString(fmt.Sprintf("**Worktree**: %s\n", wt))
	if f.status != "" {
		b.WriteString(fmt.Sprintf("**Status**: %s\n", f.status))
	}
	if f.changed != "" {
		b.WriteString(fmt.Sprintf("**Changed**: %s\n", f.changed))
	}
	if f.next != "" {
		b.WriteString(fmt.Sprintf("**Next**: %s\n", f.next))
	}
	if f.question != "" {
		b.WriteString(fmt.Sprintf("**Question for %s**: %s\n", f.questionFor, f.question))
	}
	if f.answer != "" {
		b.WriteString(fmt.Sprintf("**Answer**: %s\n", f.answer))
	}
	if f.decision != "" {
		b.WriteString(fmt.Sprintf("**Decision**: %s\n", f.decision))
	}
	if f.blocked != "" {
		b.WriteString(fmt.Sprintf("**Blocked on %s**: %s\n", f.blockedOn, f.blocked))
	}
	if f.handoff != "" {
		b.WriteString(fmt.Sprintf("**Handoff**: %s\n", f.handoff))
	}
	return b.String()
}

func appendToComms(commsFile, repo, wt string, f planFlags) error {
	featureDir := filepath.Dir(commsFile)
	os.MkdirAll(featureDir, 0755)

	file, err := os.OpenFile(commsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	info, _ := file.Stat()
	if info.Size() == 0 {
		fmt.Fprintf(file, "# Comms: %s\n\n---\n\n", wt)
	}

	timestamp := time.Now().Format("2006-01-02 15:04 MST")
	entry := buildCommsEntry(repo, wt, f)
	fmt.Fprintf(file, "## [%s] %s\n%s\n---\n\n", timestamp, repo, entry)

	return nil
}

func alertAgent(targetSession, message string) {
	detected := agents.Detect(targetSession)
	for _, a := range detected {
		if a.Status == agents.StatusIdle || a.Status == agents.StatusWaiting {
			exec.Command("tmux", "send-keys", "-t", a.Target, message, "Enter").Run()
			return
		}
	}
	exec.Command("tmux", "display-message", "-t", targetSession, message).Run()
}

func runPlanUpdate(args []string) {
	cfg := app.Config
	f := parsePlanFlags(args)

	if !f.hasContent() {
		fmt.Fprintln(os.Stderr, "error: no content flags provided (use --help)")
		os.Exit(1)
	}

	repo, wt, _, err := detectRepoContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	plansDir := cfg.Paths.Plans
	featureDir := filepath.Join(plansDir, wt)
	planFile := filepath.Join(featureDir, repo, "plan.md")
	commsFile := filepath.Join(featureDir, "comms.md")

	if f.stepNum != "" {
		if err := markStepComplete(planFile, f.stepNum, f.stepCommit); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			fmt.Printf("Marked Step %s complete in %s\n", f.stepNum, planFile)
		}
	}

	if err := appendToComms(commsFile, repo, wt, f); err != nil {
		fmt.Fprintf(os.Stderr, "error writing comms: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Posted to %s\n", commsFile)
	notifySend(cfg.Paths.State, fmt.Sprintf("📨 %s updated %s comms", repo, wt), "#73D0FF", 120)

	if f.questionFor != "" {
		alertAgent(f.questionFor,
			fmt.Sprintf("📨 %s has a question for you. Check comms: tnt plan inbox", repo))
	}
	if f.blockedOn != "" {
		alertAgent(f.blockedOn,
			fmt.Sprintf("🚫 %s is blocked on you. Check comms: tnt plan inbox", repo))
	}
	if f.answer != "" {
		alertLastQuestioner(commsFile, repo)
	}
}

func alertLastQuestioner(commsFile, myRepo string) {
	data, err := os.ReadFile(commsFile)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.Contains(line, "**Question for "+myRepo+"**") {
			for j := i - 1; j >= 0; j-- {
				if strings.HasPrefix(lines[j], "**Repo**: ") {
					questioner := strings.TrimPrefix(lines[j], "**Repo**: ")
					alertAgent(questioner,
						fmt.Sprintf("📨 %s answered your question. Check comms: tnt plan inbox", myRepo))
					return
				}
			}
		}
	}
}

// --- inbox ---

type inboxItem struct {
	timestamp string
	from      string
	kind      string
	message   string
}

func runPlanInbox() {
	cfg := app.Config

	repo, wt, _, err := detectRepoContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	commsFile := filepath.Join(cfg.Paths.Plans, wt, "comms.md")
	data, err := os.ReadFile(commsFile)
	if err != nil {
		fmt.Println("No comms file found.")
		return
	}

	items := parseInbox(string(data), repo)
	if len(items) == 0 {
		fmt.Println("No pending items for " + repo)
		return
	}

	for _, item := range items {
		fmt.Printf("\n[%s] %s from %s:\n  %s\n", item.timestamp, item.kind, item.from, item.message)
	}
	fmt.Println()
}

func parseInbox(content, myRepo string) []inboxItem {
	var items []inboxItem
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentTimestamp, currentFrom string
	var answered = map[string]bool{}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "**Answer**:") {
			for i := len(lines) - 1; i >= 0; i-- {
				if lines[i] == line {
					for j := i - 1; j >= 0; j-- {
						if strings.HasPrefix(lines[j], "**Repo**: ") {
							answerer := strings.TrimPrefix(lines[j], "**Repo**: ")
							answered[answerer] = true
							break
						}
					}
				}
			}
		}
	}

	_ = scanner

	for _, line := range lines {
		if strings.HasPrefix(line, "## [") {
			parts := strings.SplitN(line, "] ", 2)
			if len(parts) == 2 {
				currentTimestamp = strings.TrimPrefix(parts[0], "## [")
				currentFrom = strings.TrimSpace(parts[1])
			}
			continue
		}
		if strings.HasPrefix(line, "**Repo**: ") {
			currentFrom = strings.TrimPrefix(line, "**Repo**: ")
			continue
		}

		if currentFrom == myRepo {
			continue
		}

		if strings.Contains(line, "**Question for "+myRepo+"**:") {
			msg := strings.SplitN(line, "**: ", 2)
			if len(msg) == 2 {
				items = append(items, inboxItem{
					timestamp: currentTimestamp,
					from:      currentFrom,
					kind:      "question",
					message:   msg[1],
				})
			}
		}
		if strings.Contains(line, "**Blocked on "+myRepo+"**:") {
			msg := strings.SplitN(line, "**: ", 2)
			if len(msg) == 2 {
				items = append(items, inboxItem{
					timestamp: currentTimestamp,
					from:      currentFrom,
					kind:      "blocked",
					message:   msg[1],
				})
			}
		}
		if strings.Contains(line, "**Answer**:") && answered[currentFrom] {
			msg := strings.SplitN(line, "**: ", 2)
			if len(msg) == 2 {
				items = append(items, inboxItem{
					timestamp: currentTimestamp,
					from:      currentFrom,
					kind:      "answer",
					message:   msg[1],
				})
			}
		}
	}

	return items
}
