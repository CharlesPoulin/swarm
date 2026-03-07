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
- Window `swarm` — worker pane grid
- Window `hub` — nvim on the left, PR review on the right (`gh` required; falls back to lazygit/git view)
- Window `usage` — live per-agent usage/limit dashboard
- Window `dispatch` — route tickets/tasks to workers
- Window `settings` — interactive editor for common swarm settings (`~/.claude-swarm.yaml`)
- PM windows (`pm`, `pm-2`, ...) when configured

After merging a PR from a worker, reset that worktree back to `main` state:
```bash
claude-swarm reset
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-n` | `4` | Number of workers |
| `-s` | `claude-swarm` | tmux session name |
| `-b` | current branch | Base branch for worktrees |
| `-t` | `claude` | CLI: `claude`, `gemini`, or `codex` (or comma list like `claude,gemini,codex`) |
| `--cli-flags` | `` | Extra flags passed to each worker CLI command |
| `-a` | — | Add workers to a running session |

`pm` can be included in `-t` / `cli_type`; it opens in a dedicated PM tab (`Alt+4`) and does not appear in the swarm grid.

## Config file

Put defaults in `~/.claude-swarm.yaml` so you don't have to retype flags:

```yaml
num: 3
cli_type: codex,codex,claude,gemini:gemini-3-flash-preview,gemini:gemini-3-flash-preview,spare,pm
cli_flags: ""
pm_bootstrap_mode: prompt    # prompt (default), full, or none
dispatch_plan_mode: true     # dispatch sends /plan + task by default
session: myswarm
resume_buffer_secs: 120   # extra wait after usage-limit expires
monitor_interval: 30       # how often to check for usage-limit errors (secs)
hub_mode: review           # review (default) or git for the hub right pane
review_refresh_secs: 30    # PR review auto-refresh cadence
```

## Dispatch behavior

- By default, dispatch sends two commands to workers: `/plan` then the task prompt.
- This applies to both quick tasks and ticket dispatch, so workers start directly in Plan mode without extra keypresses.
- Set `dispatch_plan_mode: false` in `~/.claude-swarm.yaml` to disable Plan-mode dispatch.
- If a worker CLI does not support Plan mode, dispatch falls back to normal prompt delivery and shows an explicit notice in the dispatch result.

## Keybindings (inside the session)

| Key | Action |
|-----|--------|
| `Alt+1` | Swarm window |
| `Alt+2` | Hub window |
| `Alt+3` | Usage window |
| `Alt+4` | PM window (when configured) |
| `Alt+5` | Dispatch window |
| `Alt+6` | Settings window |
| `Ctrl+b e` | Jump to editor (nvim) |
| `Ctrl+b g` | Jump to hub right pane (review/git) |
| `Ctrl+b p` | Jump to PR review pane |
| `Ctrl+b v` | Show nvim basics (quick help) |
| `Ctrl+b R` | Reset current worktree to `origin/main` and send `/clear` |
| `Ctrl+b +` | Add a new worker on the fly |
| `Ctrl+b d` | Detach (stops monitors, prompts cleanup) |

## Settings tab

Open the settings UI with `Alt+6` (or run `claude-swarm settings` directly).

- Use `↑/↓` to pick a setting, `Enter` to edit.
- Use `s` to save all values to `~/.claude-swarm.yaml`.
- Use `r` to reload from disk.
- Invalid input is rejected with an explicit message and no config write.

Example flow:
1. Press `Alt+6`.
2. Select `Monitor Interval`, press `Enter`, type `20`, press `Enter`.
3. Press `s` to persist.
4. Re-open the tab later and press `r` to confirm the saved value.

When `pm` is enabled, PM window has:
- `tickets` pane: opens `.swarm/PM_KANBAN.md` plus `.swarm/PM_FOCUS.md` (bottom split), with ticket files under `.swarm/tickets/`
- `worker-N (pm)` pane: Codex PM chat, bootstrapped from `.swarm/PM_PROMPT.md` by default (`pm_bootstrap_mode` controls this)
- PM can pre-route tickets by setting `assigned_to: worker-N`; startup assignment now honors these first, then fills remaining workers from unassigned TODOs.

## PM Mirror Refresh

`.swarm/tickets/*.md` is the source of truth for ticket metadata. `.swarm/PM_KANBAN.md` and `.swarm/PM_FOCUS.md` are derived mirrors.

`claude-swarm ticket add`, `claude-swarm ticket assign`, and `claude-swarm ticket done` refresh PM mirrors automatically after updating ticket metadata.

If you edit ticket markdown directly, run:

```bash
claude-swarm ticket refresh
```

Drift-recovery workflow:
1. Edit the authoritative ticket files under `.swarm/tickets/`.
2. Before refresh, confirm drift if needed with `cat .swarm/PM_KANBAN.md` and `cat .swarm/PM_FOCUS.md`.
3. Run `claude-swarm ticket refresh`.
4. Validate recovery by checking that `PM_KANBAN` ordering is `status bucket -> priority asc -> ticket id asc` and that `PM_FOCUS` selects the first `in-progress`, then `todo`, then `blocked`, then `done` ticket.

## PR Review

Open interactive PR review dashboard:

```bash
claude-swarm review
```

You can see open PRs, CI status, mergeability, description, checks, and diff in one window.
Press `a` to approve + squash-merge (warns if CI is not green, but still allows override).

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

## CI/CD

This repository uses GitHub Actions for:
- **CI**: Linting (golangci-lint), testing, and cross-platform build verification on every PR and push to `main`.
- **Release**: Automated releases using [GoReleaser](https://goreleaser.com/). To create a new release, push a version tag:
  ```bash
  git tag -a v1.0.0 -m "Release v1.0.0"
  git push origin v1.0.0
  ```
  This will trigger a workflow to build binaries for Linux, macOS, and Windows and upload them to a new GitHub Release.
