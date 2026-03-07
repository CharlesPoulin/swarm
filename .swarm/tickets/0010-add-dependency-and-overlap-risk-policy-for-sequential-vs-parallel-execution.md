---
id: "0010"
title: Add dependency and overlap-risk policy for sequential vs parallel execution
status: todo
priority: 3
created_by: human
assigned_to: worker-1
---
## Description
Define PM metadata and workflow for: branch parent chaining, ticket dependencies, overlap risk, and explicit sequential-vs-parallel routing so long unattended runs avoid branch/file collisions.

## Scope Notes
- Add canonical ticket metadata for dependency and collision planning (for example `depends_on`, `branch_parent`, `overlap_risk`, `touches`, `run_mode`).
- Define how queued sequential tasks branch from prior tickets when they are intended to build on each other.
- Define where PM records conflict likelihood and execution recommendation (`sequential` vs `parallel-ok`).

## Acceptance Criteria
- [ ] Ticket schema/policy defines dependency and branching fields for chained execution (`depends_on`, `branch_parent` at minimum).
- [ ] Ticket schema/policy defines overlap-risk and scope-surface fields (`overlap_risk`, `touches`) to evaluate collision likelihood.
- [ ] PM decision rule is documented: when to run sequentially vs parallel based on dependencies and overlap risk.
- [ ] Branching rule is documented: for sequential chains, downstream ticket branch is based on the upstream ticket branch; otherwise use base branch.
- [ ] A shared PM artifact exists (for example `.swarm/PM_CONCURRENCY.md`) that summarizes open-ticket collision risk and execution mode recommendations.
- [ ] Queue/autopilot flow consumes this metadata so blocked/dependent tickets are not started early.
- [ ] Documentation includes two worked examples: one sequential chain and one safe-parallel pair.
- [ ] Deterministic tests/validation cover dependency blocking, branch-parent resolution, and conflict-mode routing outcomes.
