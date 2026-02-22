package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/spf13/cobra"
)

var shipCmd = &cobra.Command{
	Use:   "ship",
	Short: "Create a PR from the current worktree branch, then clean up",
	Long: `Run from inside a swarm worktree when your task is finished.
Opens an interactive gh pr create, then removes the worktree and branch.`,
	RunE: runShip,
}

func init() {
	f := shipCmd.Flags()
	f.StringP("base", "b", "main", "Base branch for the pull request")
	f.Bool("no-cleanup", false, "Skip worktree cleanup after PR creation")
	rootCmd.AddCommand(shipCmd)
}

func runShip(cmd *cobra.Command, args []string) error {
	base, _ := cmd.Flags().GetString("base")
	noCleanup, _ := cmd.Flags().GetBool("no-cleanup")

	repoRoot, err := git.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("getting current branch: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Warn if not in a worktree (branch doesn't look like swarm/*)
	if !strings.HasPrefix(branch, "swarm/") {
		fmt.Printf("⚠️   Current branch %q doesn't look like a swarm worktree branch.\n", branch)
		fmt.Print("    Continue anyway? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			return nil
		}
	}

	fmt.Printf("🌿  Branch : %s\n", branch)
	fmt.Printf("🎯  Base   : %s\n", base)
	fmt.Println()

	// Check gh is available
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found — install it from https://cli.github.com")
	}

	// Push branch first
	fmt.Println("📤  Pushing branch…")
	pushCmd := exec.Command("git", "push", "-u", "origin", branch)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	// Create PR interactively
	fmt.Println("\n📝  Creating pull request…")
	prCmd := exec.Command("gh", "pr", "create", "--base", base, "--head", branch)
	prCmd.Stdin = os.Stdin
	prCmd.Stdout = os.Stdout
	prCmd.Stderr = os.Stderr
	if err := prCmd.Run(); err != nil {
		return fmt.Errorf("gh pr create failed: %w", err)
	}

	if noCleanup {
		fmt.Println("\nℹ️   Skipping cleanup (--no-cleanup).")
		return nil
	}

	// Cleanup
	fmt.Print("\n🧹  Remove worktree and branch? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "Y"
	}
	if strings.EqualFold(answer, "y") {
		_ = os.Chdir(repoRoot)
		_ = git.RemoveWorktree(cwd)
		_ = git.DeleteBranch(branch)
		_ = git.Prune()
		fmt.Println("✅  Cleaned up.")
	} else {
		fmt.Printf("ℹ️   Kept. Remove manually: git worktree remove %s\n", cwd)
	}

	return nil
}
