package worktree

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type EntryKind int

const (
	KindJump     EntryKind = iota // existing worktree with tmux window
	KindOpen                      // existing worktree, no tmux window
	KindMain                      // main branch (always first)
	KindCheckout                  // remote branch, can checkout
	KindLocal                     // local branch, no worktree
)

type Entry struct {
	Kind   EntryKind
	Branch string
	Path   string
}

func (e Entry) Label() string {
	switch e.Kind {
	case KindJump:
		return "jump"
	case KindOpen:
		return "open"
	case KindMain:
		return "main"
	case KindCheckout:
		return "checkout"
	case KindLocal:
		return "local"
	}
	return ""
}

func (e Entry) ShortBranch() string {
	if idx := strings.LastIndex(e.Branch, "/"); idx >= 0 {
		return e.Branch[idx+1:]
	}
	return e.Branch
}

type RepoContext struct {
	GitRoot     string
	MainRoot    string
	RepoName    string
	SessionName string
	WorktreeDir string
	LayoutsDir  string
	ProjectsDir string
	PlansDir    string
}

func NewContext(gitRoot, sessionName, tntDir string) RepoContext {
	mainRoot := gitRoot
	gitCommon, err := exec.Command("git", "-C", gitRoot, "rev-parse", "--git-common-dir").Output()
	if err == nil {
		gc := strings.TrimSpace(string(gitCommon))
		if gc != ".git" {
			mainRoot = filepath.Dir(gc)
		}
	}

	return RepoContext{
		GitRoot:     gitRoot,
		MainRoot:    mainRoot,
		RepoName:    filepath.Base(mainRoot),
		SessionName: sessionName,
		WorktreeDir: filepath.Join(mainRoot, ".worktrees"),
		LayoutsDir:  filepath.Join(tntDir, "layouts"),
		ProjectsDir: filepath.Join(tntDir, "projects"),
		PlansDir:    filepath.Join(tntDir, "plans"),
	}
}

func ListEntries(ctx RepoContext) []Entry {
	mainBranch := gitCurrentBranch(ctx.GitRoot)

	openWindows := tmuxWorktreeWindows(ctx.SessionName)

	existing := map[string]bool{mainBranch: true}
	var entries []Entry

	wtList := gitWorktreeList(ctx.GitRoot)
	for _, wt := range wtList {
		if wt.path == ctx.GitRoot || wt.path == ctx.MainRoot {
			continue
		}
		existing[wt.branch] = true
		if _, ok := openWindows[wt.branch]; ok {
			entries = append(entries, Entry{Kind: KindJump, Branch: wt.branch, Path: wt.path})
		} else {
			entries = append(entries, Entry{Kind: KindOpen, Branch: wt.branch, Path: wt.path})
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Branch < entries[j].Branch
	})

	mainEntry := Entry{Kind: KindMain, Branch: mainBranch, Path: ctx.GitRoot}
	entries = append([]Entry{mainEntry}, entries...)

	remoteBranches := gitRemoteBranches(ctx.GitRoot)
	localBranches := gitLocalBranches(ctx.GitRoot)

	all := map[string]bool{}
	for _, b := range localBranches {
		all[b] = true
	}
	for _, b := range remoteBranches {
		all[b] = true
	}

	remoteSet := map[string]bool{}
	for _, b := range remoteBranches {
		remoteSet[b] = true
	}

	var branchNames []string
	for b := range all {
		branchNames = append(branchNames, b)
	}
	sort.Strings(branchNames)

	for _, b := range branchNames {
		if existing[b] {
			continue
		}
		if remoteSet[b] {
			entries = append(entries, Entry{Kind: KindCheckout, Branch: b})
		} else {
			entries = append(entries, Entry{Kind: KindLocal, Branch: b})
		}
	}

	return entries
}

func CreateWorktree(ctx RepoContext, branchName string) (string, error) {
	wtPath := filepath.Join(ctx.WorktreeDir, branchName)

	if _, err := os.Stat(filepath.Join(wtPath, ".git")); err == nil {
		return wtPath, nil
	}

	os.MkdirAll(ctx.WorktreeDir, 0755)
	ensureGitignore(ctx.MainRoot)

	if branchExists(ctx.GitRoot, branchName) {
		err := exec.Command("git", "-C", ctx.GitRoot, "worktree", "add", wtPath, branchName).Run()
		if err != nil {
			err = exec.Command("git", "-C", ctx.GitRoot, "worktree", "add", "--track", "-b", branchName, wtPath, "origin/"+branchName).Run()
		}
		if err != nil {
			return "", fmt.Errorf("failed to create worktree: %w", err)
		}
	} else {
		baseBranch := "main"
		if !refExists(ctx.GitRoot, "refs/heads/main") {
			baseBranch = "master"
		}
		exec.Command("git", "-C", ctx.GitRoot, "fetch", "origin", baseBranch, "--quiet").Run()
		err := exec.Command("git", "-C", ctx.GitRoot, "worktree", "add", "-b", branchName, wtPath, "origin/"+baseBranch).Run()
		if err != nil {
			return "", fmt.Errorf("failed to create worktree: %w", err)
		}
	}

	copyEnvFiles(ctx.MainRoot, wtPath)
	scaffoldPlanDir(ctx, branchName)
	scaffoldProjectConfig(ctx)
	return wtPath, nil
}

func OpenWorktreeWindow(ctx RepoContext, wtPath, branchName string) error {
	layout := defaultLayout(ctx)
	script := filepath.Join(ctx.LayoutsDir, layout+".sh")

	if info, err := os.Stat(script); err == nil && info.Mode()&0111 != 0 {
		return exec.Command(script, wtPath, ctx.SessionName, branchName).Run()
	}

	short := branchName
	if idx := strings.LastIndex(branchName, "/"); idx >= 0 {
		short = branchName[idx+1:]
	}
	wid, err := exec.Command("tmux", "new-window", "-P", "-F", "#{window_id}", "-t", ctx.SessionName, "-n", short+":"+layout, "-c", wtPath).Output()
	if err != nil {
		return err
	}
	return exec.Command("tmux", "set-option", "-w", "-t", strings.TrimSpace(string(wid)), "@worktree", branchName).Run()
}

func JumpToWorktree(ctx RepoContext, branch string, isMain bool) error {
	windows := tmuxWorktreeWindows(ctx.SessionName)

	lookupBranch := branch
	if isMain {
		lookupBranch = gitCurrentBranch(ctx.MainRoot)
	}

	if wid, ok := windows[lookupBranch]; ok {
		exec.Command("tmux", "select-window", "-t", wid).Run()
		exec.Command("tmux", "switch-client", "-t", ctx.SessionName).Run()
		return nil
	}

	if isMain {
		if err := OpenWorktreeWindow(ctx, ctx.MainRoot, lookupBranch); err != nil {
			fmt.Fprintf(os.Stderr, "tnt: layout failed for main (%v), opening plain window\n", err)
			wid, err2 := exec.Command("tmux", "new-window", "-P", "-F", "#{window_id}",
				"-t", ctx.SessionName, "-n", lookupBranch+":dev", "-c", ctx.MainRoot).Output()
			if err2 != nil {
				return fmt.Errorf("open main window: %w", err2)
			}
			exec.Command("tmux", "set-option", "-w", "-t", strings.TrimSpace(string(wid)), "@worktree", lookupBranch).Run()
		}
		exec.Command("tmux", "switch-client", "-t", ctx.SessionName).Run()
		return nil
	}

	wtPath := filepath.Join(ctx.WorktreeDir, branch)
	if _, err := os.Stat(wtPath); err != nil {
		return fmt.Errorf("worktree path not found: %s", wtPath)
	}
	err := OpenWorktreeWindow(ctx, wtPath, branch)
	if err != nil {
		return err
	}
	exec.Command("tmux", "switch-client", "-t", ctx.SessionName).Run()
	return nil
}

func scaffoldPlanDir(ctx RepoContext, branchName string) {
	planDir := filepath.Join(ctx.PlansDir, branchName, ctx.RepoName)
	os.MkdirAll(planDir, 0755)

	commsFile := filepath.Join(ctx.PlansDir, branchName, "comms.md")
	if _, err := os.Stat(commsFile); err != nil {
		os.WriteFile(commsFile, []byte(fmt.Sprintf("# Comms: %s\n\n---\n\n", branchName)), 0644)
	}
}

func scaffoldProjectConfig(ctx RepoContext) {
	configDir := filepath.Join(ctx.ProjectsDir, ctx.RepoName)
	configPath := filepath.Join(configDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return
	}
	os.MkdirAll(configDir, 0755)
	defaultConfig := `{
  "default_layout": "dev",
  "services": []
}
`
	os.WriteFile(configPath, []byte(defaultConfig), 0644)
}

func FetchAsync(gitRoot string) {
	cmd := exec.Command("git", "-C", gitRoot, "fetch", "--prune", "--quiet")
	cmd.Start()
}

// --- helpers ---

type worktreeEntry struct {
	path   string
	branch string
}

func gitWorktreeList(gitRoot string) []worktreeEntry {
	out, err := exec.Command("git", "-C", gitRoot, "worktree", "list").Output()
	if err != nil {
		return nil
	}
	var result []worktreeEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		path := parts[0]
		branch := parts[len(parts)-1]
		branch = strings.TrimPrefix(branch, "[")
		branch = strings.TrimSuffix(branch, "]")
		result = append(result, worktreeEntry{path: path, branch: branch})
	}
	return result
}

func gitCurrentBranch(gitRoot string) string {
	out, err := exec.Command("git", "-C", gitRoot, "branch", "--show-current").Output()
	if err != nil {
		return "main"
	}
	b := strings.TrimSpace(string(out))
	if b == "" {
		return "main"
	}
	return b
}

func gitLocalBranches(gitRoot string) []string {
	out, err := exec.Command("git", "-C", gitRoot, "branch", "--format=%(refname:short)").Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func gitRemoteBranches(gitRoot string) []string {
	out, err := exec.Command("git", "-C", gitRoot, "branch", "-r", "--format=%(refname:short)").Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "origin/") {
			continue
		}
		branch := strings.TrimPrefix(line, "origin/")
		if branch == "HEAD" {
			continue
		}
		result = append(result, branch)
	}
	return result
}

func tmuxWorktreeWindows(session string) map[string]string {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_id}\t#{@worktree}").Output()
	if err != nil {
		return nil
	}
	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] != "" {
			result[parts[1]] = parts[0]
		}
	}
	return result
}

func branchExists(gitRoot, branch string) bool {
	if refExists(gitRoot, "refs/heads/"+branch) {
		return true
	}
	return refExists(gitRoot, "refs/remotes/origin/"+branch)
}

func refExists(gitRoot, ref string) bool {
	return exec.Command("git", "-C", gitRoot, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

func defaultLayout(ctx RepoContext) string {
	cfgPath := filepath.Join(ctx.ProjectsDir, ctx.RepoName, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "dev"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "default_layout") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				val := strings.Trim(strings.TrimSpace(parts[1]), `",`)
				if val != "" {
					return val
				}
			}
		}
	}
	return "dev"
}

func ensureGitignore(mainRoot string) {
	gitignorePath := filepath.Join(mainRoot, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err == nil {
		if strings.Contains(string(data), ".worktrees") {
			return
		}
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString("\n.worktrees\n")
}

func copyEnvFiles(mainRoot, wtPath string) {
	entries, err := os.ReadDir(mainRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, ".env") {
			continue
		}
		if name == ".env.example" {
			continue
		}
		src := filepath.Join(mainRoot, name)
		dst := filepath.Join(wtPath, name)
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		os.WriteFile(dst, data, 0644)
	}
}
