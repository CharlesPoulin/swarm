package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running swarm session quickly",
	RunE:  runStop,
}

func init() {
	stopCmd.Flags().Bool("cleanup", false, "Also remove swarm worktrees/branches without prompts")
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if tmux.HasSession(cfg.Session) {
		if err := tmux.KillSession(cfg.Session); err != nil {
			return fmt.Errorf("killing session %q: %w", cfg.Session, err)
		}
		fmt.Printf("✅  Stopped session %q.\n", cfg.Session)
	} else {
		fmt.Printf("ℹ️   Session %q is not running.\n", cfg.Session)
	}

	cleanup, _ := cmd.Flags().GetBool("cleanup")
	if !cleanup {
		return nil
	}

	repoRoot, err := git.RepoRoot()
	if err != nil {
		return fmt.Errorf("cleanup requested, but not inside a git repository")
	}

	_ = git.Prune()

	removedWorktrees := 0
	worktreeGlob := filepath.Join(repoRoot, fmt.Sprintf("%s-*", cfg.WorktreePrefix))
	worktreeDirs, _ := filepath.Glob(worktreeGlob)
	for _, dir := range worktreeDirs {
		if err := git.RemoveWorktree(dir); err == nil {
			removedWorktrees++
		}
	}

	deletedBranches := 0
	out, err := exec.Command("git", "branch", "--list", "swarm/*/worker-*").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "*"))
			if branch == "" {
				continue
			}
			if err := git.DeleteBranch(branch); err == nil {
				deletedBranches++
			}
		}
	}

	_ = git.Prune()
	fmt.Printf("🧹  Cleanup done. Removed %d worktree(s), deleted %d branch(es).\n", removedWorktrees, deletedBranches)
	return nil
}
