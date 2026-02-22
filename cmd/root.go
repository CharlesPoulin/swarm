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
	Long: `claude-swarm creates a tmux session with a hub window (nvim + lazygit)
and N worker windows, each running an AI CLI (claude, gemini, or codex)
in its own git worktree on a fresh branch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return orchestrate(cfg)
	},
}

// Execute is the entry point called from main.
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

	// Bind flags to viper keys (only non-zero values override).
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
	viper.AddConfigPath("$HOME")
	home, _ := os.UserHomeDir()
	viper.AddConfigPath(home)
	viper.AutomaticEnv()

	// Ignore "file not found" — config file is optional.
	_ = viper.ReadInConfig()
}

// ── Validation helpers ────────────────────────────────────────────────────────

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

// ── Main orchestration ────────────────────────────────────────────────────────

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
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		logFile = nil // non-fatal; just skip file logging
	} else {
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

// ── Add-mode: append workers to a running session ────────────────────────────

func addWorkers(cfg *config.Config, repoRoot string) error {
	if !tmux.HasSession(cfg.Session) {
		return fmt.Errorf("session %q not found — start a swarm first (without -a)", cfg.Session)
	}

	maxWin, err := tmux.MaxWindowIndex(cfg.Session)
	if err != nil {
		return err
	}
	startIdx := maxWin + 1

	var newDirs []string
	for i := startIdx; i < startIdx+cfg.Num; i++ {
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		wtBranch := fmt.Sprintf("swarm/%s/worker-%d", cfg.BaseBranch, i)

		// Remove stale worktree/branch if present.
		_ = git.RemoveWorktree(wtDir)
		_ = git.DeleteBranch(wtBranch)

		if err := git.AddWorktree(wtDir, wtBranch, cfg.BaseBranch); err != nil {
			return err
		}
		newDirs = append(newDirs, wtDir)
		fmt.Printf("✅  Worktree %d → %s  (branch: %s)\n", i, wtDir, wtBranch)
	}

	fmt.Println()
	for i := startIdx; i < startIdx+cfg.Num; i++ {
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		name := fmt.Sprintf("worker-%d", i)
		if err := tmux.NewWindowNoIndex(cfg.Session, wtDir, name); err != nil {
			return err
		}
		target := fmt.Sprintf("%s:%s", cfg.Session, name)
		if err := tmux.SendKeys(target, cfg.CLIType); err != nil {
			return err
		}
	}

	if err := tmux.SelectWindow(fmt.Sprintf("%s:worker-%d", cfg.Session, startIdx)); err != nil {
		return err
	}

	fmt.Printf("✅  Added %d %s worker(s) to session %q.\n", cfg.Num, cfg.CLIType, cfg.Session)
	fmt.Printf("    Attach with: tmux attach -t %q\n", cfg.Session)
	_ = newDirs // worktrees persist in the running session
	return nil
}

// ── Full swarm start ──────────────────────────────────────────────────────────

func startSwarm(cfg *config.Config, repoRoot string, logFile *os.File) error {
	// Kill existing session.
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

	// Detect optional tools.
	hasLazygit := commandExists("lazygit")
	hasNvim := commandExists("nvim")

	// ── Create session (initial window named "hub" at creation to avoid base-index issues) ──
	if err := tmux.NewSession(cfg.Session, repoRoot, 220, 50, "hub"); err != nil {
		return err
	}

	// ── Status bar ────────────────────────────────────────────────────────────
	statusRight := fmt.Sprintf(
		"#[bg=colour235,fg=colour245] Swarm: %d workers  "+
			"#[fg=colour39]Alt+0#[fg=colour245]:hub  "+
			"#[fg=colour39]Alt+1-9#[fg=colour245]:worker  "+
			"#[fg=colour39]Ctrl+b+#[fg=colour245]:add  "+
			"#[fg=colour39]Ctrl+b g#[fg=colour245]:git  "+
			"#[fg=colour39]Ctrl+b e#[fg=colour245]:editor  "+
			"#[fg=colour39]Ctrl+b d#[fg=colour245]:detach",
		cfg.Num,
	)
	statusLeft := fmt.Sprintf(
		"#[bg=colour33,fg=colour15,bold] 🤖 SWARM (%s) #[bg=colour235] ",
		cfg.CLIType,
	)

	opts := map[string]string{
		"status":                       "on",
		"status-position":              "bottom",
		"status-style":                 "bg=colour235,fg=colour245",
		"status-left":                  statusLeft,
		"status-left-length":           "30",
		"status-right":                 statusRight,
		"status-right-length":          "140",
		"window-status-format":         "#[fg=colour245] #I:#W ",
		"window-status-current-format": "#[bg=colour33,fg=colour15,bold] #I:#W ",
	}
	for k, v := range opts {
		_ = tmux.SetOption(cfg.Session, k, v)
	}

	// ── Hub window — use stable %N pane IDs to survive any pane-base-index setting ──
	hub := fmt.Sprintf("%s:hub", cfg.Session)

	// Capture the initial pane's ID before anything else touches it.
	leftPaneID, err := tmux.GetPaneID(hub)
	if err != nil {
		return fmt.Errorf("getting hub pane ID: %w", err)
	}

	if hasNvim {
		_ = tmux.SendKeys(leftPaneID, "nvim .")
	} else {
		_ = tmux.SendKeys(leftPaneID, "echo 'nvim not found — install it for editor support'")
	}

	var rightPaneID string
	if hasLazygit {
		// Split right (40%) and capture the new pane's ID.
		rightPaneID, err = tmux.SplitWindowGetPaneID(leftPaneID, repoRoot, 40, true)
		if err != nil {
			return fmt.Errorf("splitting hub for lazygit: %w", err)
		}
		_ = tmux.SendKeys(rightPaneID, "lazygit")
		_ = tmux.SetOption(cfg.Session, "pane-border-style", "fg=colour238")
		_ = tmux.SetOption(cfg.Session, "pane-active-border-style", "fg=colour39")
		// Give focus back to nvim (left pane).
		_ = tmux.SelectPane(leftPaneID)
	} else {
		fmt.Println("⚠️   lazygit not found — hub will open without git pane.")
	}

	// ── Keybindings ───────────────────────────────────────────────────────────
	if hasLazygit && rightPaneID != "" {
		_ = tmux.BindKey(cfg.Session, "",
			"g",
			fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"", cfg.Session, rightPaneID),
		)
	}
	_ = tmux.BindKey(cfg.Session, "",
		"e",
		fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"", cfg.Session, leftPaneID),
	)

	// Alt+0 → hub (by name, not index)
	_ = tmux.BindKey(cfg.Session, "-n", "M-0",
		fmt.Sprintf("select-window -t '%s:hub'", cfg.Session),
	)

	// Alt+1-9 → worker windows (by name, not index)
	for n := 1; n <= 9; n++ {
		_ = tmux.BindKey(cfg.Session, "-n", fmt.Sprintf("M-%d", n),
			fmt.Sprintf("select-window -t '%s:worker-%d'", cfg.Session, n),
		)
	}

	// Ctrl+b + → add a new worker on the fly
	addOneScript := strings.Join([]string{
		fmt.Sprintf(`idx=$(tmux list-windows -t '%s' -F '#{window_index}' | sort -n | tail -1)`, cfg.Session),
		`idx=$((idx+1))`,
		fmt.Sprintf(`wt='%s/%s-'$idx`, repoRoot, cfg.WorktreePrefix),
		fmt.Sprintf(`br='swarm/%s/worker-'$idx`, cfg.BaseBranch),
		fmt.Sprintf(`git -C '%s' worktree add -b "$br" "$wt" '%s' -q`, repoRoot, cfg.BaseBranch),
		fmt.Sprintf(`tmux new-window -t '%s' -c "$wt" -n "worker-$idx"`, cfg.Session),
		fmt.Sprintf(`tmux send-keys -t '%s:worker-'$idx '%s' Enter`, cfg.Session, cfg.CLIType),
		fmt.Sprintf(`tmux display-message "✅ Worker $idx added"`),
	}, " && ")
	_ = tmux.BindKey(cfg.Session, "", "+", fmt.Sprintf("run-shell \"%s\"", addOneScript))

	// ── Worker windows — created by name; @ID captured for stable monitor targeting ──
	windowIDs := make([]string, cfg.Num)
	for i := 1; i <= cfg.Num; i++ {
		name := fmt.Sprintf("worker-%d", i)
		if err := tmux.NewWindowNoIndex(cfg.Session, worktreeDirs[i-1], name); err != nil {
			return err
		}
		target := fmt.Sprintf("%s:%s", cfg.Session, name)
		if err := tmux.SendKeys(target, cfg.CLIType); err != nil {
			return err
		}
		// Capture stable @ID so monitor targeting survives window renames.
		id, err := tmux.GetWindowID(target)
		if err != nil {
			return fmt.Errorf("getting window ID for %s: %w", name, err)
		}
		windowIDs[i-1] = id
	}

	// Select worker-1 on attach (by name).
	_ = tmux.SelectWindow(fmt.Sprintf("%s:worker-1", cfg.Session))

	// ── Start monitors ────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 1; i <= cfg.Num; i++ {
		go monitor.Watch(ctx, cfg, cfg.Session, windowIDs[i-1], i, logFile)
	}

	// ── Attach (blocks until detach) ──────────────────────────────────────────
	fmt.Printf("✅  All %d %s instances launched!\n", cfg.Num, cfg.CLIType)
	fmt.Printf("🔍  Usage-limit monitors active (log: /tmp/claude-swarm-%s.log)\n", cfg.Session)
	fmt.Printf("📎  Attaching to session %q…\n", cfg.Session)
	fmt.Println("    Detach anytime with: Ctrl+b  d")
	fmt.Println()

	attachCmd := exec.Command("tmux", "attach-session", "-t", cfg.Session)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	_ = attachCmd.Run() // ignore error on detach

	// ── Post-detach ───────────────────────────────────────────────────────────
	fmt.Println("\n🔴  Stopping pane monitors…")
	cancel()

	return postDetachCleanup(cfg, repoRoot, worktreeDirs)
}

// ── Post-detach cleanup prompt ────────────────────────────────────────────────

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

	_ = repoRoot // used above for context
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
