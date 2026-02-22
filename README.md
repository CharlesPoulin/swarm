# claude-swarm

Run N Claude instances in parallel, each in its own git worktree, inside a tmux session.

## Install

```bash
git clone https://github.com/cpoulin/claude-swarm
cd claude-swarm
task install
source ~/.bashrc
```

> Requires: `go`, `tmux`, `task` — and `claude` (or `gemini`/`codex`)

## Use

```bash
# inside any git repo
claude-swarm
```

That's it. You get:
- Window `0` — hub: nvim on the left, lazygit on the right
- Windows `1–N` — one Claude per worktree on a fresh branch

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `4` | Number of workers |
| `-s` | `claude-swarm` | tmux session name |
| `-b` | current branch | Base branch for worktrees |
| `-t` | `claude` | CLI: `claude`, `gemini`, or `codex` (or comma list like `claude,gemini,codex`) |
| `--cli-flags` | `` | Extra flags passed to each worker CLI command |
| `-a` | — | Add workers to a running session |

## Config file

Put defaults in `~/.claude-swarm.yaml` so you don't have to retype flags:

```yaml
num: 3
cli_type: claude
cli_flags: ""
session: myswarm
resume_buffer_secs: 120   # extra wait after usage-limit expires
monitor_interval: 30       # how often to check for usage-limit errors (secs)
```

## Keybindings (inside the session)

| Key | Action |
|-----|--------|
| `Alt+0` | Hub window |
| `Alt+1–9` | Worker windows |
| `Ctrl+b e` | Jump to editor (nvim) |
| `Ctrl+b g` | Jump to git (lazygit) |
| `Ctrl+b +` | Add a new worker on the fly |
| `Ctrl+b d` | Detach (stops monitors, prompts cleanup) |

## Edit / hack

Everything is in `internal/`:

```
internal/config/config.go      ← defaults & config struct
internal/monitor/monitor.go    ← usage-limit auto-resume logic
internal/usagelimit/parser.go  ← regex for detecting/parsing limit messages
internal/tmux/session.go       ← tmux wrappers
internal/git/worktree.go       ← git worktree helpers
```

Rebuild after changes:
```bash
task build   # or: go build -o claude-swarm .
task install # rebuild + copy to ~/bin
```
