package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/cpoulin/claude-swarm/internal/monitor"
	"github.com/cpoulin/claude-swarm/internal/ticket"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	Version = "dev"
	Repo    = "github.com/cpoulin/claude-swarm"
)

var rootCmd = &cobra.Command{
	Use:     "claude-swarm",
	Short:   "Spawn N AI CLI instances in git worktrees inside tmux",
	Version: Version,
	Long: `claude-swarm creates a tmux session with:
  - Window 1 "swarm": 2x3 agents by default
  - Window 2 "hub":   editor (left) + review/git view (right)`,
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

// ExecuteWithArgs is used for testing the command output.
func ExecuteWithArgs(args []string, out, err io.Writer) error {
	rootCmd.SetArgs(args)
	rootCmd.SetOut(out)
	rootCmd.SetErr(err)
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	f := rootCmd.Flags()
	f.IntP("num", "n", 0, "Number of AI instances (default: 6)")
	f.StringP("session", "s", "", "tmux session name (default: claude-swarm)")
	f.StringP("base-branch", "b", "", "Base branch for worktrees (default: current branch)")
	f.StringP("type", "t", "", "AI CLI(s) to use: claude|gemini|codex|pm|spare (comma list; pm opens its own tab)")
	f.String("cli-flags", "", "Extra flags passed to each AI CLI command")
	f.BoolP("add", "a", false, "Add workers to an existing session instead of restarting")
	f.String("assign-mode", "", "Ticket assignment mode: parallel|sequential|manual")

	_ = viper.BindPFlag("num", f.Lookup("num"))
	_ = viper.BindPFlag("session", f.Lookup("session"))
	_ = viper.BindPFlag("base_branch", f.Lookup("base-branch"))
	_ = viper.BindPFlag("cli_type", f.Lookup("type"))
	_ = viper.BindPFlag("cli_flags", f.Lookup("cli-flags"))
	_ = viper.BindPFlag("add_mode", f.Lookup("add"))
	_ = viper.BindPFlag("assignment_mode", f.Lookup("assign-mode"))

	rootCmd.AddCommand(swapCmd)
}

var swapCmd = &cobra.Command{
	Use:   "swap <pane-id> <new-type>",
	Short: "Swap an existing worker's model (e.g. swap %1 gemini)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		paneID := args[0]
		newType := args[1]

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Capture current working directory of the pane
		out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_current_path}").Output()
		if err != nil {
			return fmt.Errorf("getting pane path: %w", err)
		}
		dir := strings.TrimSpace(string(out))

		fmt.Printf("🔄 Swapping pane %s to %s in %s\n", paneID, newType, dir)

		// Send /exit or similar to stop current process safely if it's responsive
		_ = tmux.SendKeys(paneID, "/exit")
		time.Sleep(1 * time.Second)

		// Relaunch with new CLI
		newCmd := cliCmdFor(cfg, newType, dir, "")
		_ = tmux.SendKeys(paneID, fmt.Sprintf("cd '%s' && %s --continue", dir, newCmd))
		_ = tmux.SetPaneTitle(paneID, paneTitle(0, newType)) // 0 as dummy index

		return nil
	},
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

// ── Naming helpers ─────────────────────────────────────────────────────────────

func wtDir(repoRoot, prefix string, i int) string {
	return filepath.Join(repoRoot, fmt.Sprintf("%s-%d", prefix, i))
}

func wtBranch(baseBranch string, i int) string {
	return fmt.Sprintf("swarm/%s/worker-%d", baseBranch, i)
}

func paneTitle(i int, cliType string) string {
	return fmt.Sprintf("worker-%d (%s)", i, cliType)
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
			return fmt.Errorf("unknown CLI type %q — use claude, gemini, codex, pm, or spare", cliName)
		}
		cliName, _ := parseWorker(cliType)
		if cliName == "spare" || cliName == "pm" {
			continue
		}
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

	// Resolve TicketsDir to absolute so monitors and assignment helpers share one path.
	if !filepath.IsAbs(cfg.TicketsDir) {
		cfg.TicketsDir = filepath.Join(repoRoot, cfg.TicketsDir)
	}

	logPath := fmt.Sprintf("/tmp/claude-swarm-%s.log", cfg.Session)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if logFile != nil {
		defer func() {
			_ = logFile.Close()
		}()
	}

	fmt.Printf("🌳  Repo    : %s\n", repoRoot)
	fmt.Printf("🌿  Branch  : %s\n", cfg.BaseBranch)
	fmt.Printf("🤖  Instances: %d total (%d swarm + %d pm)  (CLI mix: %s)\n",
		len(workers), len(nonPMWorkerIndices(workers)), countCLIType(workers, "pm"), strings.Join(uniqueWorkerTypes(workers), ","))
	fmt.Printf("📺  Session : %s\n", cfg.Session)
	fmt.Printf("📋  Log     : %s\n", logPath)
	fmt.Printf("📦  Version : %s\n\n", Version)

	var w io.Writer = os.Stdout
	if logFile != nil {
		w = io.MultiWriter(os.Stdout, logFile)
	}

	if cfg.AddMode {
		return addWorkers(cfg, repoRoot, workers)
	}
	return startSwarm(cfg, repoRoot, workers, w)
}

// ── Start swarm ───────────────────────────────────────────────────────────────

func startSwarm(cfg *config.Config, repoRoot string, workers []string, w io.Writer) error {
	if tmux.HasSession(cfg.Session) {
		fmt.Printf("⚠️   Session %q already exists — killing it.\n", cfg.Session)
		_ = tmux.KillSession(cfg.Session)
	}

	if containsCLIType(workers, "pm") {
		if err := writePMArtifacts(repoRoot); err != nil {
			fmt.Printf("⚠️   Could not prepare PM artifacts: %v\n", err)
		}
	}

	worktreeDirs, err := createWorktrees(cfg, repoRoot, workers)
	if err != nil {
		return err
	}

	if cfg.AssignmentMode != "manual" {
		assignTicketsToWorkers(cfg, workers, worktreeDirs)
	}

	fmt.Println("\n🚀  Launching tmux session…")

	if err := tmux.NewSession(cfg.Session, worktreeDirs[0], 220, 50, "swarm"); err != nil {
		return err
	}

	applyStatusBar(cfg, workers)

	paneMappings, err := setupSwarmWindow(cfg, workers, worktreeDirs)
	if err != nil {
		return err
	}

	nvimID, reviewID, err := setupHubWindow(cfg, repoRoot)
	if err != nil {
		return err
	}

	if err := setupUsageWindow(cfg); err != nil {
		return err
	}

	pmWindowName, err := setupPMWindow(cfg, workers, worktreeDirs)
	if err != nil {
		return err
	}

	bindKeybindings(cfg, nvimID, reviewID, pmWindowName)

	return runAndMonitor(cfg, repoRoot, workers, worktreeDirs, paneMappings, pmWindowName, w)
}

// createWorktrees creates git worktrees for all workers and returns their dirs.
// PM workers are not given a worktree; their dir is set to repoRoot.
func createWorktrees(cfg *config.Config, repoRoot string, workers []string) ([]string, error) {
	worktreeDirs := make([]string, len(workers))
	// Clean stale administrative entries up front so branch deletion is accurate.
	_ = git.Prune()
	for i := 1; i <= len(workers); i++ {
		cliName, _ := parseWorker(workers[i-1])
		if cliName == "pm" {
			worktreeDirs[i-1] = repoRoot
			fmt.Printf("🤖  Worker %d → %s  (PM mode, runs in repo root)\n", i, repoRoot)
			continue
		}
		dir := wtDir(repoRoot, cfg.WorktreePrefix, i)
		branch := wtBranch(cfg.BaseBranch, i)
		_ = git.RemoveWorktree(dir)
		_ = git.Prune()
		// If a stale directory remains but is no longer a registered worktree, remove it.
		if _, err := os.Stat(dir); err == nil {
			_ = os.RemoveAll(dir)
		}
		_ = git.DeleteBranch(branch)
		if err := git.AddWorktree(dir, branch, cfg.BaseBranch); err != nil {
			// Retry once after another prune/delete pass for stale branch/worktree state.
			_ = git.Prune()
			if _, statErr := os.Stat(dir); statErr == nil {
				_ = os.RemoveAll(dir)
			}
			_ = git.DeleteBranch(branch)
			if retryErr := git.AddWorktree(dir, branch, cfg.BaseBranch); retryErr != nil {
				return nil, retryErr
			}
		}
		worktreeDirs[i-1] = dir
		fmt.Printf("✅  Worktree %d → %s  (branch: %s, CLI: %s)\n", i, dir, branch, workers[i-1])
	}
	return worktreeDirs, nil
}

// applyStatusBar sets session-scoped tmux status bar options in a deterministic order.
func applyStatusBar(cfg *config.Config, workers []string) {
	cliLabel := cfg.CLIType
	if len(uniqueWorkerTypes(workers)) > 1 {
		cliLabel = strings.Join(uniqueWorkerTypes(workers), ",")
	}
	statusLeft := fmt.Sprintf(
		"#[bg=colour33,fg=colour15,bold] 🤖 SWARM (%s) #[bg=colour235] ", cliLabel)
	pmHint := ""
	if containsCLIType(workers, "pm") {
		pmHint = "  #[fg=colour39]Alt+4#[fg=colour245]:pm"
	}
	statusRight := fmt.Sprintf(
		"#[bg=colour235,fg=colour245] %d agents  "+
			"#[fg=colour33]v%s#[fg=colour245]  "+
			"#[fg=colour39]Alt+1#[fg=colour245]:agents  "+
			"#[fg=colour39]Alt+2#[fg=colour245]:hub  "+
			"#[fg=colour39]Alt+3#[fg=colour245]:usage  "+
			"%s"+
			"#[fg=colour39]Ctrl+b p#[fg=colour245]:review  "+
			"#[fg=colour39]Ctrl+b g#[fg=colour245]:git  "+
			"#[fg=colour39]Ctrl+b e#[fg=colour245]:editor  "+
			"#[fg=colour39]Ctrl+b d#[fg=colour245]:detach  "+
			"#[fg=colour196]Ctrl+Q#[fg=colour245]:quit  "+
			"#[fg=colour39]%s",
		len(workers), Version, pmHint, Repo)

	statusOpts := [][2]string{
		{"status", "on"},
		{"status-position", "top"},
		{"status-style", "bg=colour235,fg=colour245"},
		{"status-left", statusLeft},
		{"status-left-length", "30"},
		{"status-right", statusRight},
		{"status-right-length", "200"},
		{"window-status-format", "#[fg=colour245] #I:#W "},
		{"window-status-current-format", "#[bg=colour33,fg=colour15,bold] #I:#W "},
		{"pane-border-style", "fg=colour238"},
		{"pane-active-border-style", "fg=colour39"},
		{"pane-border-status", "top"},
		{"pane-border-format", " #{pane_title} "},
	}
	for _, opt := range statusOpts {
		_ = tmux.SetOption(cfg.Session, opt[0], opt[1])
	}
}

func setupUsageWindow(cfg *config.Config) error {
	if err := tmux.NewWindowNoIndex(cfg.Session, ".", "usage"); err != nil {
		return fmt.Errorf("creating usage window: %w", err)
	}
	refreshSecs := cfg.MonitorInterval
	if refreshSecs <= 0 {
		refreshSecs = 30
	}
	return tmux.SendKeys(fmt.Sprintf("%s:usage", cfg.Session), usageWindowCommand(cfg.Session, refreshSecs))
}

type paneMapping struct {
	PaneID      string
	WorkerIndex int
}

// setupSwarmWindow creates worker panes in the "swarm" window and launches non-PM AI CLIs.
func setupSwarmWindow(cfg *config.Config, workers, worktreeDirs []string) ([]paneMapping, error) {
	topLeft, err := tmux.GetPaneID(fmt.Sprintf("%s:swarm", cfg.Session))
	if err != nil {
		return nil, fmt.Errorf("getting initial pane ID: %w", err)
	}

	workerIdxs := nonPMWorkerIndices(workers)
	if len(workerIdxs) == 0 {
		_ = tmux.SetPaneTitle(topLeft, "swarm (no non-PM workers)")
		_ = tmux.SendKeys(topLeft, "echo 'No non-PM workers configured.' && exec bash")
		return nil, nil
	}

	var workerPaneIDs []string
	if len(workerIdxs) == 6 {
		// Fixed 2x3 grid:
		//  ┌─────────────┬─────────────┐
		//  │   worker-1  │   worker-2  │
		//  ├─────────────┼─────────────┤
		//  │   worker-3  │   worker-4  │
		//  ├─────────────┼─────────────┤
		//  │   worker-5  │   worker-6  │
		//  └─────────────┴─────────────┘
		topRight, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[workerIdxs[1]], 50, true)
		if err != nil {
			return nil, fmt.Errorf("creating top-right pane: %w", err)
		}
		middleLeft, err := tmux.SplitWindowGetPaneID(topLeft, worktreeDirs[workerIdxs[2]], 66, false)
		if err != nil {
			return nil, fmt.Errorf("creating middle-left pane: %w", err)
		}
		bottomLeft, err := tmux.SplitWindowGetPaneID(middleLeft, worktreeDirs[workerIdxs[4]], 50, false)
		if err != nil {
			return nil, fmt.Errorf("creating bottom-left pane: %w", err)
		}
		middleRight, err := tmux.SplitWindowGetPaneID(topRight, worktreeDirs[workerIdxs[3]], 66, false)
		if err != nil {
			return nil, fmt.Errorf("creating middle-right pane: %w", err)
		}
		bottomRight, err := tmux.SplitWindowGetPaneID(middleRight, worktreeDirs[workerIdxs[5]], 50, false)
		if err != nil {
			return nil, fmt.Errorf("creating bottom-right pane: %w", err)
		}

		workerPaneIDs = []string{topLeft, topRight, middleLeft, middleRight, bottomLeft, bottomRight}
	} else {
		// Fallback for non-6 worker counts.
		workerPaneIDs = []string{topLeft}
		for i := 1; i < len(workerIdxs); i++ {
			newPane, splitErr := tmux.SplitWindowGetPaneID(fmt.Sprintf("%s:swarm", cfg.Session), worktreeDirs[workerIdxs[i]], 50, false)
			if splitErr != nil {
				return nil, fmt.Errorf("creating pane for worker %d: %w", i+1, splitErr)
			}
			workerPaneIDs = append(workerPaneIDs, newPane)
			_ = tmux.SelectLayout(fmt.Sprintf("%s:swarm", cfg.Session), "tiled")
		}
	}

	mappings := make([]paneMapping, 0, len(workerPaneIDs))
	for i, paneID := range workerPaneIDs {
		idx := workerIdxs[i]
		_ = tmux.SetPaneTitle(paneID, paneTitle(idx+1, workers[idx]))
		ticketFile := ""
		if _, err := os.Stat(filepath.Join(worktreeDirs[idx], "CURRENT_TICKET.md")); err == nil {
			ticketFile = "CURRENT_TICKET.md"
		}
		_ = tmux.SendKeys(paneID, fmt.Sprintf("cd '%s' && %s", worktreeDirs[idx], cliCmdFor(cfg, workers[idx], worktreeDirs[idx], ticketFile)))
		mappings = append(mappings, paneMapping{PaneID: paneID, WorkerIndex: idx})
	}
	_ = tmux.SelectPane(topLeft)

	return mappings, nil
}

// setupPMWindow creates dedicated PM window(s) with ticket pane (left) and chat pane (right).
// Returns first PM window name for Alt+4 binding.
func setupPMWindow(cfg *config.Config, workers, worktreeDirs []string) (string, error) {
	pmCount := 0
	firstWindow := ""
	for i, worker := range workers {
		cliName, _ := parseWorker(worker)
		if cliName != "pm" {
			continue
		}
		pmCount++
		windowName := "pm"
		if pmCount > 1 {
			windowName = fmt.Sprintf("pm-%d", pmCount)
		}
		if err := tmux.NewWindowNoIndex(cfg.Session, worktreeDirs[i], windowName); err != nil {
			return "", fmt.Errorf("creating PM window %q: %w", windowName, err)
		}
		leftPaneID, err := tmux.GetPaneID(fmt.Sprintf("%s:%s", cfg.Session, windowName))
		if err != nil {
			return "", fmt.Errorf("getting PM pane for %q: %w", windowName, err)
		}
		rightPaneID, err := tmux.SplitWindowGetPaneID(leftPaneID, worktreeDirs[i], 55, true)
		if err != nil {
			return "", fmt.Errorf("splitting PM window %q: %w", windowName, err)
		}

		_ = tmux.SetPaneTitle(leftPaneID, "tickets")
		_ = tmux.SetPaneTitle(rightPaneID, fmt.Sprintf("worker-%d (pm)", i+1))
		_ = tmux.SendKeys(leftPaneID, pmTicketsWorkbenchCmd(worktreeDirs[i]))
		_ = tmux.SendKeys(rightPaneID, fmt.Sprintf("cd '%s' && %s", worktreeDirs[i], cliCmdFor(cfg, worker, worktreeDirs[i], "")))
		_ = tmux.SelectPane(rightPaneID)
		if firstWindow == "" {
			firstWindow = windowName
		}
	}
	return firstWindow, nil
}

// setupHubWindow creates the "hub" window with editor (left) and review/git view (right).
// Returns (editorPaneID, rightPaneID, error).
func setupHubWindow(cfg *config.Config, repoRoot string) (editorPaneID, rightPaneID string, err error) {
	if err = tmux.NewWindowNoIndex(cfg.Session, repoRoot, "hub"); err != nil {
		return
	}
	editorPaneID, err = tmux.GetPaneID(fmt.Sprintf("%s:hub", cfg.Session))
	if err != nil {
		err = fmt.Errorf("getting hub pane ID: %w", err)
		return
	}

	rightPaneID, err = tmux.SplitWindowGetPaneID(editorPaneID, repoRoot, 50, true)
	if err != nil {
		err = fmt.Errorf("splitting hub window: %w", err)
		return
	}

	_ = tmux.SetPaneTitle(editorPaneID, "editor")
	_ = tmux.SetPaneTitle(rightPaneID, "review")
	_ = tmux.SendKeys(editorPaneID, hubEditorCmd())
	_ = tmux.SendKeys(rightPaneID, hubRightPaneCmd(cfg))

	_ = tmux.SelectPane(editorPaneID)
	return
}

func hubEditorCmd() string {
	if commandExists("nvim") {
		return "nvim ."
	}
	if commandExists("code") {
		return "code . && exec bash"
	}
	return "echo 'nvim/code not found; editor pane is shell.' && exec bash"
}

func pmTicketsWorkbenchCmd(repoRoot string) string {
	taskPath := filepath.Join(repoRoot, ".swarm", "PM_TASK.md")
	ticketsDir := filepath.Join(repoRoot, ".swarm", "tickets")
	if commandExists("nvim") {
		return fmt.Sprintf("mkdir -p '%s' && nvim '%s' '+Lexplore %s'", ticketsDir, taskPath, ticketsDir)
	}
	if commandExists("vim") {
		return fmt.Sprintf("mkdir -p '%s' && vim '%s'", ticketsDir, taskPath)
	}
	if commandExists("nano") {
		return fmt.Sprintf("mkdir -p '%s' && nano '%s'", ticketsDir, taskPath)
	}
	return fmt.Sprintf("mkdir -p '%s' && ls -la '%s' && echo 'Edit ticket files under %s manually.' && exec bash", ticketsDir, ticketsDir, ticketsDir)
}

func hubRightPaneCmd(cfg *config.Config) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.HubMode))
	if mode == "git" {
		if commandExists("lazygit") {
			return "lazygit"
		}
		return "git status -sb && echo && git log --graph --oneline --decorate -20 && exec bash"
	}

	refreshSecs := cfg.ReviewRefreshSecs
	if refreshSecs <= 0 {
		refreshSecs = cfg.MonitorInterval
	}
	if refreshSecs <= 0 {
		refreshSecs = 30
	}
	if commandExists("gh") {
		return reviewWindowCommand(cfg.Session, refreshSecs)
	}
	if commandExists("lazygit") {
		return "echo 'gh CLI not found; falling back to lazygit.' && lazygit"
	}
	return "echo 'gh CLI not found. Install gh and run: gh auth login' && git status -sb && exec bash"
}

// bindKeybindings sets session-scoped tmux keybindings.
func bindKeybindings(cfg *config.Config, hubPaneID, rightPaneID, pmWindowName string) {
	// Alt+1 → swarm (agents), Alt+2 → hub
	_ = tmux.BindKey(cfg.Session, "-n", "M-1",
		fmt.Sprintf("select-window -t '%s:swarm'", cfg.Session))
	_ = tmux.BindKey(cfg.Session, "-n", "M-2",
		fmt.Sprintf("select-window -t '%s:hub'", cfg.Session))
	_ = tmux.BindKey(cfg.Session, "-n", "M-3",
		fmt.Sprintf("select-window -t '%s:usage'", cfg.Session))
	if pmWindowName != "" {
		_ = tmux.BindKey(cfg.Session, "-n", "M-4",
			fmt.Sprintf("select-window -t '%s:%s'", cfg.Session, pmWindowName))
	}

	// Ctrl+b v → nvim basics quick reference
	_ = tmux.BindKey(cfg.Session, "", "v", nvimBasicsPopupCommand())

	// Ctrl+b S → confirm then ship: open PR + cleanup for current worktree
	_ = tmux.BindKey(cfg.Session, "", "S",
		"confirm-before -p \"Ship this worktree as a PR? (y/n)\" "+
			"\"new-window -c '#{pane_current_path}' 'claude-swarm ship; echo; read -p \\\"Press Enter to close…\\\"'\"")

	// Ctrl+b R → reset current worktree to origin/main + clear pane context
	_ = tmux.BindKey(cfg.Session, "", "R",
		"confirm-before -p \"Reset this worktree to origin/main and clear context? (y/n)\" "+
			"\"new-window -c '#{pane_current_path}' 'claude-swarm reset -y --pane \\\"#{pane_id}\\\"; echo; read -p \\\"Press Enter to close…\\\"'\"")

	// Ctrl+Q → kill session (no prefix)
	_ = tmux.BindKey(cfg.Session, "-n", "C-q",
		fmt.Sprintf("kill-session -t '%s'", cfg.Session))

	// Ctrl+b e → editor, Ctrl+b g/p → right pane (review/git)
	_ = tmux.BindKey(cfg.Session, "", "e",
		fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"",
			cfg.Session, hubPaneID))
	_ = tmux.BindKey(cfg.Session, "", "g",
		fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"",
			cfg.Session, rightPaneID))
	_ = tmux.BindKey(cfg.Session, "", "p",
		fmt.Sprintf("run-shell \"tmux select-window -t '%s:hub' && tmux select-pane -t '%s'\"",
			cfg.Session, rightPaneID))
}

func nvimBasicsPopupCommand() string {
	return `display-popup -E "sh -lc 'printf \"%s\n\" \"NVIM BASICS (normal usage)\" \"\" \"Modes\" \"  i        insert mode\" \"  Esc      normal mode\" \"  :        command mode\" \"\" \"Movement\" \"  h j k l  left/down/up/right\" \"  w / b    next/previous word\" \"  0 / $    line start/end\" \"  gg / G   file top/bottom\" \"\" \"Editing\" \"  x        delete char\" \"  dd       delete line\" \"  yy       yank (copy) line\" \"  p        paste\" \"  u        undo\" \"  Ctrl+r   redo\" \"\" \"Search\" \"  /text    search forward\" \"  n / N    next/prev match\" \"\" \"Files\" \"  :w       save\" \"  :q       quit\" \"  :wq      save + quit\" \"  :q!      quit without save\" \"\" \"Press Enter to close...\"; read _'"` + ` || new-window -n nvim-help "sh -lc 'printf \"%s\n\" \"NVIM BASICS (normal usage)\" \"\" \"Modes\" \"  i        insert mode\" \"  Esc      normal mode\" \"  :        command mode\" \"\" \"Movement\" \"  h j k l  left/down/up/right\" \"  w / b    next/previous word\" \"  0 / $    line start/end\" \"  gg / G   file top/bottom\" \"\" \"Editing\" \"  x        delete char\" \"  dd       delete line\" \"  yy       yank (copy) line\" \"  p        paste\" \"  u        undo\" \"  Ctrl+r   redo\" \"\" \"Search\" \"  /text    search forward\" \"  n / N    next/prev match\" \"\" \"Files\" \"  :w       save\" \"  :q       quit\" \"  :wq      save + quit\" \"  :q!      quit without save\" \"\" \"Press Enter to close...\"; read _'"` + `"`
}

// runAndMonitor attaches the tmux session, starts worker monitors, and handles post-detach cleanup.
func runAndMonitor(cfg *config.Config, repoRoot string, workers, worktreeDirs []string, paneMappings []paneMapping, pmWindowName string, w io.Writer) error {
	_ = tmux.SelectWindow(fmt.Sprintf("%s:swarm", cfg.Session))

	fmt.Printf("✅  All %d instances launched!\n", len(workers))
	fmt.Printf("🔍  Monitors active (log: /tmp/claude-swarm-%s.log)\n", cfg.Session)
	fmt.Printf("📎  Attaching to session %q…\n", cfg.Session)
	pmHelp := ""
	if pmWindowName != "" {
		pmHelp = "  |  PM: Alt+4"
	}
	fmt.Printf("    Detach: Ctrl+b d  |  Usage: Alt+3  |  Hub: Alt+2  |  Review: Ctrl+b p  |  Agents: Alt+1%s\n", pmHelp)
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, mapping := range paneMappings {
		idx := mapping.WorkerIndex
		cliName, _ := parseWorker(workers[idx])
		if cliName == "spare" || cliName == "pm" {
			continue
		}
		go monitor.Watch(ctx, cfg, cfg.Session, mapping.PaneID, idx+1, cliCmdFor(cfg, workers[idx], worktreeDirs[idx], ""), worktreeDirs[idx], w)
	}

	attachCmd := exec.Command("tmux", "attach-session", "-t", cfg.Session)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	_ = attachCmd.Run()

	fmt.Println("\n🔴  Stopping monitors…")
	cancel()

	postDetachCleanup(repoRoot, worktreeDirs)
	return nil
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
		if _, err := os.Stat(wtDir(repoRoot, cfg.WorktreePrefix, i)); os.IsNotExist(err) {
			break
		}
		i++
	}
	startIdx := i

	for j, cliType := range workers {
		i := startIdx + j
		cliName, _ := parseWorker(cliType)
		if cliName == "pm" {
			if err := writePMArtifacts(repoRoot); err != nil {
				fmt.Printf("⚠️   Could not prepare PM artifacts: %v\n", err)
			}
			windowName := fmt.Sprintf("pm-%d", i)
			if err := tmux.NewWindowNoIndex(cfg.Session, repoRoot, windowName); err != nil {
				return fmt.Errorf("creating PM window %q: %w", windowName, err)
			}
			leftPaneID, err := tmux.GetPaneID(fmt.Sprintf("%s:%s", cfg.Session, windowName))
			if err != nil {
				return fmt.Errorf("getting PM pane %q: %w", windowName, err)
			}
			rightPaneID, err := tmux.SplitWindowGetPaneID(leftPaneID, repoRoot, 55, true)
			if err != nil {
				return fmt.Errorf("splitting PM window %q: %w", windowName, err)
			}
			_ = tmux.SetPaneTitle(leftPaneID, "tickets")
			_ = tmux.SetPaneTitle(rightPaneID, paneTitle(i, cliType))
			_ = tmux.SendKeys(leftPaneID, pmTicketsWorkbenchCmd(repoRoot))
			_ = tmux.SendKeys(rightPaneID, fmt.Sprintf("cd '%s' && %s", repoRoot, cliCmdFor(cfg, cliType, repoRoot, "")))
			_ = tmux.SelectPane(rightPaneID)
			fmt.Printf("✅  PM window %q launched (worker-%d).\n", windowName, i)
			continue
		}

		dir := wtDir(repoRoot, cfg.WorktreePrefix, i)
		branch := wtBranch(cfg.BaseBranch, i)
		_ = git.RemoveWorktree(dir)
		_ = git.Prune()
		if _, err := os.Stat(dir); err == nil {
			_ = os.RemoveAll(dir)
		}
		_ = git.DeleteBranch(branch)
		if err := git.AddWorktree(dir, branch, cfg.BaseBranch); err != nil {
			return err
		}
		fmt.Printf("✅  Worktree %d → %s  (branch: %s, CLI: %s)\n", i, dir, branch, cliType)

		// Find the last pane in swarm window and split it.
		newPane, err := tmux.SplitWindowGetPaneID(fmt.Sprintf("%s:swarm", cfg.Session), dir, 50, false)
		if err != nil {
			return fmt.Errorf("creating pane for worker %d: %w", i, err)
		}
		_ = tmux.SetPaneTitle(newPane, paneTitle(i, cliType))
		_ = tmux.SendKeys(newPane, fmt.Sprintf("cd '%s' && %s", dir, cliCmdFor(cfg, cliType, dir, "")))
	}

	fmt.Printf("✅  Added %d worker(s) to session %q.\n", len(workers), cfg.Session)
	return nil
}

// ── Ticket helpers ────────────────────────────────────────────────────────────

const pmPrompt = `You are the Project Manager for this swarm.
Your job:
1) Maintain ` + "`.swarm/PM_TASK.md`" + ` as the source of truth for goals, scope, acceptance criteria, and open questions.
2) Review and improve ` + "`.swarm/tickets/`" + `.
   - Create new tickets with ` + "`claude-swarm ticket add`" + `.
   - Update existing ticket title/status/priority/assignee/description by editing ticket markdown files directly.
3) Keep tasks aligned with product intent and sequencing.

Do NOT write product code. Focus on clear, executable tickets and scope clarity.`

const pmTaskTemplate = `# PM Task

## Outcome
- What product/user outcome are we driving?

## Scope
- In scope:
- Out of scope:

## Acceptance Criteria
- [ ] Criterion 1
- [ ] Criterion 2

## Open Questions
- Question:
`

// writePMArtifacts ensures PM prompt/task files and ticket directory exist under repoRoot/.swarm.
func writePMArtifacts(repoRoot string) error {
	dir := filepath.Join(repoRoot, ".swarm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "tickets"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "PM_PROMPT.md"), []byte(pmPrompt), 0o644); err != nil {
		return err
	}
	taskPath := filepath.Join(dir, "PM_TASK.md")
	if _, err := os.Stat(taskPath); os.IsNotExist(err) {
		return os.WriteFile(taskPath, []byte(pmTaskTemplate), 0o644)
	}
	return nil
}

// assignTicketsToWorkers assigns the next N todo tickets to non-PM workers
// and writes CURRENT_TICKET.md into each worktree.
func assignTicketsToWorkers(cfg *config.Config, workers, worktreeDirs []string) {
	store := ticket.NewStore(cfg.TicketsDir)

	// Track which IDs we've already handed out this round to avoid double-assign.
	assigned := make(map[string]bool)

	for i, worker := range workers {
		cliName, _ := parseWorker(worker)
		if cliName == "spare" || cliName == "pm" {
			continue
		}
		t, err := nextUnassigned(store, assigned)
		if err != nil || t == nil {
			break
		}
		workerName := fmt.Sprintf("worker-%d", i+1)
		assigned[t.ID] = true
		if err := store.Assign(t.ID, workerName); err != nil {
			fmt.Printf("⚠️   Could not assign ticket %s to %s: %v\n", t.ID, workerName, err)
			continue
		}
		if err := ticket.WriteCurrentTicket(worktreeDirs[i], t); err != nil {
			fmt.Printf("⚠️   Could not write ticket %s to %s: %v\n", t.ID, worktreeDirs[i], err)
			continue
		}
		fmt.Printf("🎫  Ticket %s → %s: %s\n", t.ID, workerName, t.Title)
	}
}

// nextUnassigned returns the next todo ticket whose ID is not in the skip set.
func nextUnassigned(store *ticket.Store, skip map[string]bool) (*ticket.Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, t := range tickets {
		if t.Status == ticket.StatusTodo && !skip[t.ID] {
			return t, nil
		}
	}
	return nil, nil
}

// ── Cleanup ───────────────────────────────────────────────────────────────────

func postDetachCleanup(repoRoot string, worktreeDirs []string) {
	fmt.Print("\n🧹  Remove worktrees and swarm branches? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "Y"
	}
	if strings.EqualFold(answer, "y") {
		for _, dir := range worktreeDirs {
			if dir == repoRoot {
				continue // PM worker — no worktree to remove
			}
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
	case "claude", "gemini", "codex", "spare", "pm":
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
	nonPMTypes := make([]string, 0, len(cliTypes))
	pmWorkers := make([]string, 0, len(cliTypes))
	for _, cliType := range cliTypes {
		cliName, _ := parseWorker(cliType)
		if cliName == "pm" {
			pmWorkers = append(pmWorkers, cliType)
			continue
		}
		nonPMTypes = append(nonPMTypes, cliType)
	}
	baseTypes := nonPMTypes
	if len(baseTypes) == 0 {
		baseTypes = cliTypes
	}
	workers := make([]string, 0, cfg.Num+len(pmWorkers))
	for i := 0; i < cfg.Num; i++ {
		workers = append(workers, baseTypes[i%len(baseTypes)])
	}
	workers = append(workers, pmWorkers...)
	return workers
}

func normalizeWorkers(workers []string) []string {
	workers = normalizeGemini(workers)
	workers = normalizeCodex(workers)
	return workers
}

func normalizeGemini(workers []string) []string {
	if !containsCLIType(workers, "gemini") {
		return workers
	}
	if geminiHealthCheck() {
		return workers
	}
	fmt.Println("⚠️   Gemini health check failed, but keeping gemini workers (no auto-replacement).")
	fmt.Println("⚠️   If gemini fails in-pane, fix locally by upgrading Node.js and reinstalling @google/gemini-cli.")
	return workers
}

func normalizeCodex(workers []string) []string {
	if !containsCLIType(workers, "codex") {
		return workers
	}
	if codexHealthCheck() {
		return workers
	}
	fallback, ok := firstAvailableCLI("claude", "gemini")
	if !ok {
		fmt.Println("⚠️   Codex is installed but fails to start.")
		fmt.Println("⚠️   No fallback CLI (claude/gemini) was found, keeping codex workers as-is.")
		return workers
	}
	replaced := make([]string, len(workers))
	replacedCount := 0
	for i, cliType := range workers {
		cliName, _ := parseWorker(cliType)
		if cliName == "codex" {
			replaced[i] = fallback
			replacedCount++
		} else {
			replaced[i] = cliType
		}
	}
	fmt.Printf("⚠️   Codex failed health check; replaced %d worker(s) with %s.\n", replacedCount, fallback)
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

func countCLIType(workers []string, cliType string) int {
	count := 0
	for _, worker := range workers {
		cliName, _ := parseWorker(worker)
		if cliName == cliType {
			count++
		}
	}
	return count
}

func nonPMWorkerIndices(workers []string) []int {
	indices := make([]int, 0, len(workers))
	for i, worker := range workers {
		cliName, _ := parseWorker(worker)
		if cliName == "pm" {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func firstAvailableCLI(cliTypes ...string) (string, bool) {
	for _, cliType := range cliTypes {
		if commandExists(cliType) {
			return cliType, true
		}
	}
	return "", false
}

func codexHealthCheck() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "--version")
	_, err := cmd.CombinedOutput()
	return err == nil && ctx.Err() == nil
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
// ticketFile is the filename of a ticket to load on startup (e.g. "CURRENT_TICKET.md"); empty means no ticket.
func cliCmdFor(cfg *config.Config, worker, worktreeDir, ticketFile string) string {
	cliName, model := parseWorker(worker)
	switch cliName {
	case "spare":
		return "echo 'Spare pane ready.' && exec bash"
	case "pm":
		promptPath := filepath.Join(worktreeDir, ".swarm", "PM_PROMPT.md")
		return fmt.Sprintf(`claude --message "$(cat '%s')"`, promptPath)
	}
	cmd := cliName
	if model != "" {
		cmd += " --model " + model
	}
	if cfg.CLIFlags != "" {
		cmd += " " + cfg.CLIFlags
	}
	if cliName == "gemini" {
		if home := geminiCLIHomeForWorktree(worktreeDir); home != "" {
			cmd = fmt.Sprintf("GEMINI_CLI_HOME='%s' %s", home, cmd)
		}
	}
	if ticketFile != "" {
		cmd += fmt.Sprintf(` --message "$(cat '%s')"`, filepath.Join(worktreeDir, ticketFile))
	}
	return cmd
}

func geminiCLIHomeForWorktree(worktreeDir string) string {
	base := filepath.Base(filepath.Clean(worktreeDir))
	if !strings.HasPrefix(base, ".wt-") {
		return ""
	}
	return filepath.Join(os.Getenv("HOME"), ".gemini-"+strings.TrimPrefix(base, "."))
}
