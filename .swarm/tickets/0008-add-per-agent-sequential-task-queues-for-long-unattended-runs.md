---
id: "0008"
title: Add per-agent sequential task queues for long unattended runs
status: blocked
priority: 3
created_by: human
assigned_to: worker-1
---
## Description
Enable each worker to process a queue/list of tasks assigned to that worker, one after another, without manual intervention, for multi-hour unattended operation.

## Dependencies
- Depends on `0007` for direct Plan-mode dispatch entry behavior.
- Depends on `0009` for safety/permission policy and loop-guard enforcement required for unattended execution.
- Depends on `0010` for dependency/overlap policy and branch-parent chaining behavior in sequential lists.

## Blocked By
- `0007`, `0009`, and `0010` must complete before queue execution can begin safely.

## Scope Notes
- Queue model is per-agent: each worker drains its own ordered task list sequentially.
- Intended use case is unattended execution windows (4-5 hours) with minimal/no operator interaction.
- Include usage-limit behavior so workers can pause and resume without losing queue position.

## Acceptance Criteria
- [ ] Per-agent queue/list behavior exists: workers execute assigned tasks one-by-one in deterministic order.
- [ ] On task completion, worker auto-picks the next queued task for that same worker without manual dispatch.
- [ ] Before starting each next task, worker context is auto-cleared to reduce cross-task contamination.
- [ ] If worker hits usage/token limits, queue state transitions to `waiting`, automatically resumes when limits clear, and continues from the same queue position.
- [ ] If no queued tasks remain, worker stays idle and continues polling for newly queued tasks.
- [ ] Operator-visible status surface shows at minimum: `active`, `waiting (rate-limited)`, `idle (no tasks)`, and current queue depth per worker.
- [ ] Documentation describes how to create per-agent queues, expected unattended behavior, and recovery steps after long runs.
- [ ] Deterministic tests cover queue ordering, completion handoff, context clear trigger, wait/resume behavior, and empty-queue idle behavior.
