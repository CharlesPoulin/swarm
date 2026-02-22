package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	f.StringP("type", "t", "", "AI CLI(s) to use: claude|gemini|codex (or comma list, e.g. claude,gemini,codex)")
	f.String("cli-flags", "", "Extra flags passed to each AI CLI command")
	f.BoolP("add", "a", false, "Add workers to an existing session instead of restarting")

	_ = viper.BindPFlag("num", f.Lookup("num"))
	_ = viper.BindPFlag("session", f.Lookup("session"))
	_ = viper.BindPFlag("base_branch", f.Lookup("base-branch"))
	_ = viper.BindPFlag("cli_type", f.Lookup("type"))
	_ = viper.BindPFlag("cli_flags", f.Lookup("cli-flags"))
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
	cliTypes := parseCLITypes(cfg.CLIType)
	if len(cliTypes) == 0 {
		return fmt.Errorf("no valid CLI types provided")
	}
	for _, cliType := range cliTypes {
		if !isSupportedCLIType(cliType) {
			cliName, _ := parseWorker(cliType)
			return fmt.Errorf("unknown CLI type %q — use claude, gemini, or codex", cliName)
		}
		cliName, _ := parseWorker(cliType)
		if _, err := exec.LookPath(cliName); err != nil {
			return fmt.Errorf("%s not found — install it first", cliName)
		}
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
	workers := buildWorkers(cfg)
	workers = normalizeWorkers(workers)

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
	fmt.Printf("🤖  Instances: %d  (CLI mix: %s)\n", len(workers), strings.Join(uniqueWorkerTypes(workers), ","))
	fmt.Printf("📺  Session : %s\n", cfg.Session)
	fmt.Printf("📋  Log     : %s\n\n", logPath)

	if cfg.AddMode {
		return addWorkers(cfg, repoRoot, workers)
	}
	return startSwarm(cfg, repoRoot, workers, logFile)
}

// ── Start swarm ───────────────────────────────────────────────────────────────

func startSwarm(cfg *config.Config, repoRoot string, workers []string, logFile *os.File) error {
	if tmux.HasSession(cfg.Session) {
		fmt.Printf("⚠️   Session %q already exists — killing it.\n", cfg.Session)
		_ = tmux.KillSession(cfg.Session)
	}

	// Create worktrees.
	worktreeDirs := make([]string, len(workers))
	for i := 1; i <= len(workers); i++ {
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		wtBranch := fmt.Sprintf("swarm/%s/worker-%d", cfg.BaseBranch, i)
		_ = git.RemoveWorktree(wtDir)
		_ = git.DeleteBranch(wtBranch)
		if err := git.AddWorktree(wtDir, wtBranch, cfg.BaseBranch); err != nil {
			return err
		}
		worktreeDirs[i-1] = wtDir
		fmt.Printf("✅  Worktree %d → %s  (branch: %s, CLI: %s)\n", i, wtDir, wtBranch, workers[i-1])
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
	cliLabel := cfg.CLIType
	if len(uniqueWorkerTypes(workers)) > 1 {
		cliLabel = strings.Join(uniqueWorkerTypes(workers), ",")
	}
	statusLeft := fmt.Sprintf(
		"#[bg=colour33,fg=colour15,bold] 🤖 SWARM (%s) #[bg=colour235] ", cliLabel)
	statusRight := fmt.Sprintf(
		"#[bg=colour235,fg=colour245] %d agents  "+
			"#[fg=colour39]Alt+1#[fg=colour245]:agents  "+
			"#[fg=colour39]Alt+2#[fg=colour245]:hub  "+
			"#[fg=colour39]Ctrl+b g#[fg=colour245]:git  "+
			"#[fg=colour39]Ctrl+b e#[fg=colour245]:editor  "+
			"#[fg=colour39]Ctrl+b d#[fg=colour245]:detach  "+
			"#[fg=colour196]Ctrl+Q#[fg=colour245]:quit",
		len(workers))

	for k, v := range map[string]string{
		"status":                       "on",
		"status-position":              "bottom",
		"status-style":                 "bg=colour235,fg=colour245",
		"status-left":                  statusLeft,
		"status-left-length":           "30",
		"status-right":                 statusRight,
		"status-right-length":          "140",
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
	topRight, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[1%len(workers)], 50, true)
	if err != nil {
		return fmt.Errorf("creating top-right pane: %w", err)
	}
	bottomLeft, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[2%len(workers)], 50, false)
	if err != nil {
		return fmt.Errorf("creating bottom-left pane: %w", err)
	}
	bottomRight, err := tmux.SplitWindowGetPaneID(topRight, worktreeDirs[3%len(workers)], 50, false)
	if err != nil {
		return fmt.Errorf("creating bottom-right pane: %w", err)
	}

	workerPaneIDs := []string{topLeft, topRight, bottomLeft, bottomRight}

	for i, paneID := range workerPaneIDs {
		idx := i % len(workers)
		_ = tmux.SetPaneTitle(paneID, fmt.Sprintf("worker-%d (%s)", i+1, workers[idx]))
		_ = tmux.SendKeys(paneID, fmt.Sprintf("cd '%s' && %s", worktreeDirs[idx], cliCmdFor(cfg, workers[idx])))
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

	// Ctrl+b S → confirm then ship: open PR + cleanup for current worktree
	_ = tmux.BindKey(cfg.Session, "", "S",
		"confirm-before -p \"Ship this worktree as a PR? (y/n)\" "+
			"\"new-window -c '#{pane_current_path}' 'claude-swarm ship; echo; read -p \\\"Press Enter to close…\\\"'\"")
  
	// Ctrl+Q → kill session (no prefix)
	_ = tmux.BindKey(cfg.Session, "-n", "C-q",
		fmt.Sprintf("kill-session -t '%s'", cfg.Session))

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

	fmt.Printf("✅  All %d instances launched!\n", len(workers))
	fmt.Printf("🔍  Monitors active (log: /tmp/claude-swarm-%s.log)\n", cfg.Session)
	fmt.Printf("📎  Attaching to session %q…\n", cfg.Session)
	fmt.Println("    Detach: Ctrl+b d  |  Hub: Alt+2  |  Agents: Alt+1")
	fmt.Println()

	// ── Start monitors ────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i, paneID := range workerPaneIDs {
		idx := i % len(workers)
		go monitor.Watch(ctx, cfg, cfg.Session, paneID, i+1, cliCmdFor(cfg, workers[idx]), logFile)
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

func addWorkers(cfg *config.Config, repoRoot string, workers []string) error {
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

	for j, cliType := range workers {
		i := startIdx + j
		wtDir := filepath.Join(repoRoot, fmt.Sprintf("%s-%d", cfg.WorktreePrefix, i))
		wtBranch := fmt.Sprintf("swarm/%s/worker-%d", cfg.BaseBranch, i)
		_ = git.RemoveWorktree(wtDir)
		_ = git.DeleteBranch(wtBranch)
		if err := git.AddWorktree(wtDir, wtBranch, cfg.BaseBranch); err != nil {
			return err
		}
		fmt.Printf("✅  Worktree %d → %s  (branch: %s, CLI: %s)\n", i, wtDir, wtBranch, cliType)

		// Find the last pane in swarm window and split it.
		newPane, err := tmux.SplitWindowGetPaneID(fmt.Sprintf("%s:swarm", cfg.Session), wtDir, 50, false)
		if err != nil {
			return fmt.Errorf("creating pane for worker %d: %w", i, err)
		}
		_ = tmux.SetPaneTitle(newPane, fmt.Sprintf("worker-%d (%s)", i, cliType))
		_ = tmux.SendKeys(newPane, fmt.Sprintf("cd '%s' && %s", wtDir, cliCmdFor(cfg, cliType)))
	}

	fmt.Printf("✅  Added %d worker(s) to session %q.\n", len(workers), cfg.Session)
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

// parseWorker splits "gemini:gemini-2.0-flash" into ("gemini", "gemini-2.0-flash").
// A plain "claude" returns ("claude", "").
func parseWorker(s string) (cliName, model string) {
	if idx := strings.Index(s, ":"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func isSupportedCLIType(cliType string) bool {
	cliName, _ := parseWorker(cliType)
	switch cliName {
	case "claude", "gemini", "codex":
		return true
	default:
		return false
	}
}

func parseCLITypes(raw string) []string {
	parts := strings.Split(raw, ",")
	cliTypes := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			cliTypes = append(cliTypes, trimmed)
		}
	}
	return cliTypes
}

func buildWorkers(cfg *config.Config) []string {
	cliTypes := parseCLITypes(cfg.CLIType)
	workers := make([]string, cfg.Num)
	for i := 0; i < cfg.Num; i++ {
		workers[i] = cliTypes[i%len(cliTypes)]
	}
	return workers
}

func normalizeWorkers(workers []string) []string {
	if !containsCLIType(workers, "gemini") {
		return workers
	}
	if geminiHealthCheck() {
		return workers
	}
	fallback, ok := firstAvailableCLI("claude", "codex")
	if !ok {
		fmt.Println("⚠️   Gemini is installed but fails to start (likely Node.js runtime mismatch).")
		fmt.Println("⚠️   No fallback CLI (claude/codex) was found, keeping gemini workers as-is.")
		return workers
	}
	replaced := make([]string, len(workers))
	replacedCount := 0
	for i, cliType := range workers {
		if cliType == "gemini" {
			replaced[i] = fallback
			replacedCount++
		} else {
			replaced[i] = cliType
		}
	}
	fmt.Printf("⚠️   Gemini failed health check; replaced %d worker(s) with %s.\n", replacedCount, fallback)
	fmt.Println("⚠️   Fix locally by upgrading Node.js and reinstalling @google/gemini-cli.")
	return replaced
}

func containsCLIType(workers []string, cliType string) bool {
	for _, worker := range workers {
		cliName, _ := parseWorker(worker)
		if cliName == cliType {
			return true
		}
	}
	return false
}

func firstAvailableCLI(cliTypes ...string) (string, bool) {
	for _, cliType := range cliTypes {
		if commandExists(cliType) {
			return cliType, true
		}
	}
	return "", false
}

func geminiHealthCheck() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gemini", "--version")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}
	output := string(out)
	if strings.Contains(output, "ReferenceError: File is not defined") {
		return false
	}
	if ctx.Err() == context.DeadlineExceeded {
		return false
	}
	return false
}

func uniqueWorkerTypes(workers []string) []string {
	seen := make(map[string]bool, len(workers))
	ordered := make([]string, 0, len(workers))
	for _, worker := range workers {
		if !seen[worker] {
			seen[worker] = true
			ordered = append(ordered, worker)
		}
	}
	return ordered
}

// cliCmdFor returns the full CLI invocation for a worker, including model and extra flags.
// Worker may be "gemini:gemini-2.0-flash" or plain "claude".
func cliCmdFor(cfg *config.Config, worker string) string {
	cliName, model := parseWorker(worker)
	cmd := cliName
	if model != "" {
		cmd += " --model " + model
	}
	if cfg.CLIFlags != "" {
		cmd += " " + cfg.CLIFlags
	}
	return cmd
}
