# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`claude-swarm` is a Go CLI tool that spawns N AI CLI instances (claude, gemini, codex) in parallel, each in its own git worktree, inside a tmux session with a 2×2 grid layout.

## Commands

```bash
task build      # go build -o claude-swarm .
task test       # go test ./...
task vet        # go vet ./...
task install    # build + copy to ~/bin + configure statusline
```

Run a single test package:
```bash
go test ./internal/usagelimit/...
```

## Architecture

The entry point is `main.go` → `cmd.Execute()` (cobra). All logic lives in `cmd/root.go` and `internal/`.

**Startup flow** (`cmd/root.go`):
1. `config.Load()` — merges CLI flags, `~/.claude-swarm.yaml`, and defaults via viper
2. `validate()` — checks tmux/git/CLI binaries are available
3. `buildWorkers()` — expands the comma-separated `-t` list into a `[]string` of length `-n`, cycling through the list
4. `normalizeWorkers()` — runs a Gemini health check; falls back to claude/codex if Gemini's Node.js runtime is broken
5. `startSwarm()` — creates git worktrees (`.wt-1`, `.wt-2`, …), starts the tmux session with a 2×2 pane grid, launches an AI CLI in each pane, starts goroutine monitors, then blocks on `tmux attach-session`
6. After detach: prompts to clean up worktrees and branches

**Key packages:**
- `internal/config/config.go` — `Config` struct and viper defaults; worktree dirs use the `worktree_prefix` key (default `.wt`)
- `internal/tmux/session.go` — thin wrappers around `exec.Command("tmux", ...)`, all pane references use stable `%N` pane IDs
- `internal/git/worktree.go` — git worktree add/remove/prune helpers
- `internal/monitor/monitor.go` — `Watch()` goroutine: polls `tmux capture-pane` every `monitor_interval` seconds; on usage-limit detection, waits `wait_secs + resume_buffer_secs`, then sends `<cli> --continue` to the pane
- `internal/usagelimit/parser.go` — regex detection and wait-time extraction from pane text; only file with tests (`parser_test.go`)

**Tmux session layout:**
- Window `swarm` — 2×2 grid of agent panes (top-left, top-right, bottom-left, bottom-right mapped to workers 1–4 cycling)
- Window `hub` — nvim (left pane) + lazygit (right pane, 40% width), opened at repo root
- Keybindings are session-scoped (not global): `Alt+1`/`Alt+2` switch windows; `Ctrl+b e`/`Ctrl+b g` jump to nvim/lazygit panes by stable `%N` ID

**Config file** (`~/.claude-swarm.yaml`):
```yaml
num: 4
cli_type: claude,claude,claude,gemini
cli_flags: ""
session: claude-swarm
resume_buffer_secs: 120
monitor_interval: 30
```
Viper merges file → env → CLI flags in that priority order.

**Branch naming:** worktrees are placed at `<repo-root>/.wt-<N>` on branches named `swarm/<base-branch>/worker-<N>`. Both are deleted on cleanup.
