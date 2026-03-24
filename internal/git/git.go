package git

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func RepoRoot(dir string) (string, error) {
	return run(dir, "rev-parse", "--show-toplevel")
}

func Branch(dir string) (string, error) {
	return run(dir, "branch", "--show-current")
}

func RepoName(dir string) (string, error) {
	root, err := RepoRoot(dir)
	if err != nil {
		return "", err
	}

	commonDir, err := run(dir, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == ".git" {
		return filepath.Base(root), nil
	}

	return filepath.Base(filepath.Dir(commonDir)), nil
}

func MainRoot(dir string) (string, error) {
	root, err := RepoRoot(dir)
	if err != nil {
		return "", err
	}

	commonDir, err := run(dir, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == ".git" {
		return root, nil
	}

	return filepath.Dir(commonDir), nil
}

func CommitsAhead(dir, base string) (string, error) {
	return run(dir, "rev-list", "--count", base+"..HEAD")
}

func IsDirty(dir string) (bool, error) {
	out, err := run(dir, "status", "--short")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func WorktreeList(dir string) ([]string, error) {
	out, err := run(dir, "worktree", "list")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func Fetch(dir string) error {
	_, err := run(dir, "fetch", "--prune", "--quiet")
	return err
}

func BranchList(dir string) ([]string, error) {
	out, err := run(dir, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func RemoteBranchList(dir string) ([]string, error) {
	out, err := run(dir, "branch", "-r", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var branches []string
	for _, b := range strings.Split(out, "\n") {
		b = strings.TrimPrefix(b, "origin/")
		if b != "HEAD" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}
