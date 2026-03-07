---
id: "0009"
title: Define safe broad permissions + loop guards for 5-hour unattended runs
status: todo
priority: 3
created_by: human
assigned_to: worker-1
---
## Description
Set a permissions and guardrail policy for gemini/claude/codex so long unattended runs are productive without risking host damage or infinite action loops.

## Scope Notes
- Applies to all worker CLIs (`gemini`, `claude`, `codex`) for unattended multi-hour runs.
- Goal: broad execution autonomy with explicit boundaries that protect the host and avoid self-looping behavior.
- Destructive operations are only acceptable inside the worker's own worktree/branch context.

## Acceptance Criteria
- [ ] Permission model is documented and enforceable: write access limited to repo worktrees, `.swarm`, and `/tmp`; broader read access allowed.
- [ ] Outbound network/API access is allowed for all worker CLIs; system/package installs remain blocked unless explicitly approved.
- [ ] Safety blocks are defined and enforced: no privilege escalation, no writes outside allowed paths, and no destructive operations outside the worker's own worktree/branch.
- [ ] Branch/worktree cleanup exception is explicitly allowed: worker may run `rm`-style destructive cleanup inside its own worktree/branch scope only.
- [ ] Loop/stuck guards are defined and enforced with `4`-attempt thresholds (retry cap and repeated-identical-action cap).
- [ ] On guardrail trigger (rate limit exhaustion, loop detection, policy violation), worker stops and waits for manual intervention (no auto-skip).
- [ ] Operator-visible status includes guardrail reason and last blocked action so recovery is actionable.
- [ ] Documentation covers policy defaults, allowed exceptions, and manual override flow for trusted operations.
- [ ] Deterministic tests cover path policy, destructive-op scope checks, loop guard trigger at `4`, and stop/wait behavior.
