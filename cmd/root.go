package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/cpoulin/claude-swarm/internal/monitor"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "claude-swarm",
	Short: "Spawn N AI CLI instances in git worktrees inside tmux",
	Long: `claude-swarm creates a tmux session with:
  - Window 1 "swarm": all N agents visible as stacked panes
  - Window 2 "hub":   nvim (left) + lazygit (right)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return orchestrate(cfg)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	f := rootCmd.Flags()
	f.IntP("num", "n", 0, "Number of AI instances (default: 4)")
	f.StringP("session", "s", "", "tmux session name (default: claude-swarm)")
	f.StringP("base-branch", "b", "", "Base branch for worktrees (default: current branch)")
	f.StringP("type", "t", "", "AI CLI to use: claude|gemini|codex (default: claude)")
	f.BoolP("add", "a", false, "Add workers to an existing session instead of restarting")

	_ = viper.BindPFlag("num", f.Lookup("num"))
	_ = viper.BindPFlag("session", f.Lookup("session"))
	_ = viper.BindPFlag("base_branch", f.Lookup("base-branch"))
	_ = viper.BindPFlag("cli_type", f.Lookup("type"))
	_ = viper.BindPFlag("add_mode", f.Lookup("add"))
}

func initConfig() {
	config.SetDefaults()
	viper.SetConfigName(".claude-swarm")
	viper.SetConfigType("yaml")
	home, _ := os.UserHomeDir()
	viper.AddConfigPath(home)
	viper.AutomaticEnv()
	_ = viper.ReadInConfig()
}

// ── Validation ────────────────────────────────────────────────────────────────

func validate(cfg *config.Config) error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found — install it first")
	}
	if _, err := git.RepoRoot(); err != nil {
		return fmt.Errorf("not inside a git repository")
	}
	switch cfg.CLIType {
	case "claude", "gemini", "codex":
	default:
		return fmt.Errorf("unknown CLI type %q — use claude, gemini, or codex", cfg.CLIType)
	}
	if _, err := exec.LookPath(cfg.CLIType); err != nil {
		return fmt.Errorf("%s not found — install it first", cfg.CLIType)
	}
	if cfg.Num < 1 {
		return fmt.Errorf("-n must be a positive integer")
	}
	return nil
}

// ── Orchestrate ───────────────────────────────────────────────────────────────

func orchestrate(cfg *config.Config) error {
	if err := validate(cfg); err != nil {
		return err
	}

	repoRoot, err := git.RepoRoot()
	if err != nil {
		return err
	}

	if cfg.BaseBranch == "" {
		cfg.BaseBranch, err = git.CurrentBranch()
		if err != nil {
			return err
		}
	}

	logPath := fmt.Sprintf("/tmp/claude-swarm-%s.log", cfg.Session)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if logFile != nil {
		defer logFile.Close()
	}

	fmt.Printf("🌳  Repo    : %s\n", repoRoot)
	fmt.Printf("🌿  Branch  : %s\n", cfg.BaseBranch)
	fmt.Printf("🤖  Instances: %d  (CLI: %s)\n", cfg.Num, cfg.CLIType)
	fmt.Printf("📺  Session : %s\n", cfg.Session)
	fmt.Printf("📋  Log     : %s\n\n", logPath)

	if cfg.AddMode {
		return addWorkers(cfg, repoRoot)
	}
	return startSwarm(cfg, repoRoot, logFile)
}

// ── Start swarm ───────────────────────────────────────────────────────────────

func startSwarm(cfg *config.Config, repoRoot string, logFile *os.File) error {
	if tmux.HasSession(cfg.Session) {
		fmt.Printf("⚠️   Session %q already exists — killing it.\n", cfg.Session)
		_ = tmux.KillSession(cfg.Session)
	}

	// Create worktrees.
	worktreeDirs := make([]string, cfg.Num)
	for i := 1; i <= cfg.Num; i++ {
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		wtBranch := fmt.Sprintf("swarm/%s/worker-%d", cfg.BaseBranch, i)
		_ = git.RemoveWorktree(wtDir)
		_ = git.DeleteBranch(wtBranch)
		if err := git.AddWorktree(wtDir, wtBranch, cfg.BaseBranch); err != nil {
			return err
		}
		worktreeDirs[i-1] = wtDir
		fmt.Printf("✅  Worktree %d → %s  (branch: %s)\n", i, wtDir, wtBranch)
	}

	fmt.Println("\n🚀  Launching tmux session…")

	hasLazygit := commandExists("lazygit")
	hasNvim := commandExists("nvim")

	// ── Session ───────────────────────────────────────────────────────────────
	// First window is "swarm" — all agent panes live here.
	if err := tmux.NewSession(cfg.Session, worktreeDirs[0], 220, 50, "swarm"); err != nil {
		return err
	}

	// ── Status bar ────────────────────────────────────────────────────────────
	statusLeft := fmt.Sprintf(
		"#[bg=colour33,fg=colour15,bold] 🤖 SWARM (%s) #[bg=colour235] ", cfg.CLIType)
	statusRight := fmt.Sprintf(
		"#[bg=colour235,fg=colour245] %d agents  "+
			"#[fg=colour39]Alt+1#[fg=colour245]:agents  "+
			"#[fg=colour39]Alt+2#[fg=colour245]:hub  "+
			"#[fg=colour39]Ctrl+b g#[fg=colour245]:git  "+
			"#[fg=colour39]Ctrl+b e#[fg=colour245]:editor  "+
			"#[fg=colour39]Ctrl+b d#[fg=colour245]:detach",
		cfg.Num)

	for k, v := range map[string]string{
		"status":                       "on",
		"status-position":              "bottom",
		"status-style":                 "bg=colour235,fg=colour245",
		"status-left":                  statusLeft,
		"status-left-length":           "30",
		"status-right":                 statusRight,
		"status-right-length":          "120",
		"window-status-format":         "#[fg=colour245] #I:#W ",
		"window-status-current-format": "#[bg=colour33,fg=colour15,bold] #I:#W ",
		"pane-border-style":            "fg=colour238",
		"pane-active-border-style":     "fg=colour39",
		"pane-border-status":           "top",
		"pane-border-format":           " #{pane_title} ",
	} {
		_ = tmux.SetOption(cfg.Session, k, v)
	}

	// ── Agent panes (window "swarm") — 2×2 grid ─────────────────────────────
	//
	//  ┌─────────────┬─────────────┐
	//  │   worker-1  │   worker-2  │
	//  ├─────────────┼─────────────┤
	//  │   worker-3  │   worker-4  │
	//  └─────────────┴─────────────┘
	//
	topLeft, err := tmux.GetPaneID(fmt.Sprintf("%s:swarm", cfg.Session))
	if err != nil {
		return fmt.Errorf("getting initial pane ID: %w", err)
	}
	topRight, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[1%cfg.Num], 50, true)
	if err != nil {
		return fmt.Errorf("creating top-right pane: %w", err)
	}
	bottomLeft, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[2%cfg.Num], 50, false)
	if err != nil {
		return fmt.Errorf("creating bottom-left pane: %w", err)
	}
	bottomRight, err := tmux.SplitWindowGetPaneID(topRight, worktreeDirs[3%cfg.Num], 50, false)
	if err != nil {
		return fmt.Errorf("creating bottom-right pane: %w", err)
	}

	workerPaneIDs := []string{topLeft, topRight, bottomLeft, bottomRight}

	for i, paneID := range workerPaneIDs {
		idx := i % cfg.Num
		_ = tmux.SetPaneTitle(paneID, fmt.Sprintf("worker-%d", i+1))
		_ = tmux.SendKeys(paneID, fmt.Sprintf("cd '%s' && %s", worktreeDirs[idx], cfg.CLIType))
	}

	_ = tmux.SelectPane(topLeft)

	// ── Hub window (nvim + lazygit) ───────────────────────────────────────────
	if err := tmux.NewWindowNoIndex(cfg.Session, repoRoot, "hub"); err != nil {
		return err
	}

	hubPaneID, err := tmux.GetPaneID(fmt.Sprintf("%s:hub", cfg.Session))
	if err != nil {
		return fmt.Errorf("getting hub pane ID: %w", err)
	}

	var lazygitPaneID string
	if hasNvim {
		_ = tmux.SendKeys(hubPaneID, "nvim .")
	}
	if hasLazygit {
		lazygitPaneID, err = tmux.SplitWindowGetPaneID(hubPaneID, repoRoot, 40, true)
		if err != nil {
			return fmt.Errorf("splitting hub for lazygit: %w", err)
		}
		_ = tmux.SendKeys(lazygitPaneID, "lazygit")
		_ = tmux.SelectPane(hubPaneID) // focus nvim by default
	} else {
		fmt.Println("⚠️   lazygit not found — hub opens without git pane.")
	}

	// ── Keybindings ───────────────────────────────────────────────────────────
	// Alt+1 → swarm (agents), Alt+2 → hub
	_ = tmux.BindKey(cfg.Session, "-n", "M-1",
		fmt.Sprintf("select-window -t '%s:swarm'", cfg.Session))
	_ = tmux.BindKey(cfg.Session, "-n", "M-2",
		fmt.Sprintf("select-window -t '%s:hub'", cfg.Session))

	// Ctrl+b e → nvim, Ctrl+b g → lazygit
	_ = tmux.BindKey(cfg.Session, "", "e",
		fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"",
			cfg.Session, hubPaneID))
	if hasLazygit && lazygitPaneID != "" {
		_ = tmux.BindKey(cfg.Session, "", "g",
			fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"",
				cfg.Session, lazygitPaneID))
	}

	// ── Attach (starts on swarm window) ──────────────────────────────────────
	_ = tmux.SelectWindow(fmt.Sprintf("%s:swarm", cfg.Session))

	fmt.Printf("✅  All %d %s instances launched!\n", cfg.Num, cfg.CLIType)
	fmt.Printf("🔍  Monitors active (log: /tmp/claude-swarm-%s.log)\n", cfg.Session)
	fmt.Printf("📎  Attaching to session %q…\n", cfg.Session)
	fmt.Println("    Detach: Ctrl+b d  |  Hub: Alt+2  |  Agents: Alt+1")
	fmt.Println()

	// ── Start monitors ────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i, paneID := range workerPaneIDs {
		go monitor.Watch(ctx, cfg, cfg.Session, paneID, i+1, logFile)
	}

	attachCmd := exec.Command("tmux", "attach-session", "-t", cfg.Session)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	_ = attachCmd.Run()

	fmt.Println("\n🔴  Stopping monitors…")
	cancel()

	return postDetachCleanup(cfg, repoRoot, worktreeDirs)
}

// ── Add-mode ──────────────────────────────────────────────────────────────────

func addWorkers(cfg *config.Config, repoRoot string) error {
	if !tmux.HasSession(cfg.Session) {
		return fmt.Errorf("session %q not found — start a swarm first (without -a)", cfg.Session)
	}

	// Count existing worker panes by looking at pane titles in the swarm window.
	// Simpler: just check how many worktree dirs exist already.
	i := 1
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))); os.IsNotExist(err) {
			break
		}
		i++
	}
	startIdx := i

	for i := startIdx; i < startIdx+cfg.Num; i++ {
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		wtBranch := fmt.Sprintf("swarm/%s/worker-%d", cfg.BaseBranch, i)
		_ = git.RemoveWorktree(wtDir)
		_ = git.DeleteBranch(wtBranch)
		if err := git.AddWorktree(wtDir, wtBranch, cfg.BaseBranch); err != nil {
			return err
		}
		fmt.Printf("✅  Worktree %d → %s  (branch: %s)\n", i, wtDir, wtBranch)

		// Find the last pane in swarm window and split it.
		newPane, err := tmux.SplitWindowGetPaneID(fmt.Sprintf("%s:swarm", cfg.Session), wtDir, 50, false)
		if err != nil {
			return fmt.Errorf("creating pane for worker %d: %w", i, err)
		}
		_ = tmux.SetPaneTitle(newPane, fmt.Sprintf("worker-%d", i))
		_ = tmux.SendKeys(newPane, fmt.Sprintf("cd '%s' && %s", wtDir, cfg.CLIType))
	}

	fmt.Printf("✅  Added %d worker(s) to session %q.\n", cfg.Num, cfg.Session)
	return nil
}

// ── Cleanup ───────────────────────────────────────────────────────────────────

func postDetachCleanup(cfg *config.Config, repoRoot string, worktreeDirs []string) error {
	fmt.Print("\n🧹  Remove worktrees and swarm branches? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "Y"
	}
	if strings.EqualFold(answer, "y") {
		for _, dir := range worktreeDirs {
			branch, _ := git.BranchOfWorktree(dir)
			_ = git.RemoveWorktree(dir)
			if branch != "" {
				_ = git.DeleteBranch(branch)
			}
		}
		_ = git.Prune()
		fmt.Println("✅  Cleaned up.")
	} else {
		fmt.Println("ℹ️   Worktrees kept. Remove manually with: git worktree remove <path>")
	}
	_ = repoRoot
	return nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
