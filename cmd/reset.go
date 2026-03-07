package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset current worktree branch to origin/<base> and optionally clear CLI context",
	Long: `Use this inside a worker worktree after a PR is merged.
It fetches origin/<base>, hard-resets the current branch to that commit, and cleans untracked files.`,
	RunE: runReset,
}

func init() {
	f := resetCmd.Flags()
	f.StringP("base", "b", "main", "Base branch to reset from (remote: origin/<base>)")
	f.BoolP("yes", "y", false, "Skip confirmation prompt")
	f.String("pane", "", "tmux pane ID to send /clear to (example: %5)")
	f.Bool("no-clear", false, "Do not send /clear to the target pane after reset")
	rootCmd.AddCommand(resetCmd)
}

func runReset(cmd *cobra.Command, args []string) error {
	base, _ := cmd.Flags().GetString("base")
	yes, _ := cmd.Flags().GetBool("yes")
	paneID, _ := cmd.Flags().GetString("pane")
	noClear, _ := cmd.Flags().GetBool("no-clear")

	repoRoot, err := git.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("getting current branch: %w", err)
	}

	fmt.Printf("🌳  Repo    : %s\n", repoRoot)
	fmt.Printf("📁  Worktree: %s\n", cwd)
	fmt.Printf("🌿  Branch  : %s\n", branch)
	fmt.Printf("🎯  Reset to: origin/%s\n\n", base)

	if !yes {
		fmt.Print("⚠️   This will discard local changes and untracked files in this worktree. Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("ℹ️   Reset cancelled.")
			return nil
		}
	}

	fmt.Println("📥  Fetching latest base branch…")
	if err := runGit(cwd, "fetch", "origin", base); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	fmt.Println("♻️   Hard-resetting current branch…")
	if err := runGit(cwd, "reset", "--hard", fmt.Sprintf("origin/%s", base)); err != nil {
		return fmt.Errorf("git reset failed: %w", err)
	}

	fmt.Println("🧹  Cleaning untracked files…")
	if err := runGit(cwd, "clean", "-fd"); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	if !noClear {
		if paneID == "" {
			fmt.Println("ℹ️   Skipped context clear (no --pane provided).")
		} else if err := tmux.SendKeys(paneID, "/clear"); err != nil {
			fmt.Printf("⚠️   Could not send /clear to pane %s: %v\n", paneID, err)
		} else {
			fmt.Printf("🧠  Sent /clear to pane %s.\n", paneID)
		}
	}

	fmt.Println("✅  Worktree reset complete.")
	return nil
}

func runGit(dir string, args ...string) error {
	gitArgs := append([]string{"-C", dir}, args...)
	c := exec.Command("git", gitArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
