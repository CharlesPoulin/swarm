package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// RepoRoot returns the absolute path of the git repository root.
func RepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the short name of the current branch (or commit hash on detached HEAD).
func CurrentBranch() (string, error) {
	out, err := exec.Command("git", "symbolic-ref", "--short", "HEAD").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	// detached HEAD — return commit hash
	out, err = exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// AddWorktree creates a new worktree at dir on a fresh branch based on base.
func AddWorktree(dir, branch, base string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branch, dir, base, "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return nil
}

// RemoveWorktree force-removes the worktree at dir.
func RemoveWorktree(dir string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove %s: %w\n%s", dir, err, out)
	}
	return nil
}

// Prune cleans up stale worktree administrative files.
func Prune() error {
	if out, err := exec.Command("git", "worktree", "prune").CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune: %w\n%s", err, out)
	}
	return nil
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(branch string) error {
	if out, err := exec.Command("git", "branch", "-D", branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D %s: %w\n%s", branch, err, out)
	}
	return nil
}

// BranchOfWorktree returns the branch checked out in the given worktree directory.
func BranchOfWorktree(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s symbolic-ref --short HEAD: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}
