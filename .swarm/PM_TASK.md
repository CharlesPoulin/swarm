# PM Task

## Outcome
- Maintain a deterministic PM operating loop where any contributor can identify the next executable ticket and owner within 5 minutes using `.swarm/` artifacts only.

## Scope
### In Scope
- Keep `.swarm/PM_TASK.md` as the single planning source for goals, scope, sequence, acceptance criteria, and open decisions.
- Keep `.swarm/tickets/*.md` as the operational source for ticket metadata (`id`, `title`, `status`, `priority`, `assigned_to`) and acceptance criteria.
- Keep `.swarm/PM_KANBAN.md` and `.swarm/PM_FOCUS.md` synchronized with ticket files after metadata changes.
- Enforce dependency-aware sequencing across worker tickets.

### Out of Scope
- Product code implementation.
- Non-PM tmux UX redesign.
- Multi-cycle roadmap planning beyond current swarm run.

## Current State (2026-03-07)
- Startup context was refreshed from `README.md`, `AGENTS.md`, `.swarm/PM_KANBAN.md`, and `.swarm/PM_FOCUS.md`.
- PM audit reconciled ticket files against repo behavior after finding stale mirror data and false `done` states.
- `0004` is now the completed policy baseline; its decisions are recorded below and propagated to dependent tickets.
- `0002` is the active focus ticket because deterministic ordering/focus logic exists, but lifecycle refresh is still incomplete for `ticket add`, `ticket assign`, and `ticket done`.
- `0005` was moved back to `todo` to preserve one active focus ticket and avoid parallel doc churn while `0002` remains unresolved.
- `0003` is blocked on `0005`; `0008` is blocked on `0007`, `0009`, and `0010`.
- `0006` and `0007` were returned to `todo` after PM audit found no evidence yet that their acceptance criteria are met.
- Global config still pins the shared ticket store via `tickets_dir: /home/cpoulin/sidejob/swarm/.swarm/tickets`.

## Sequencing (Updated 2026-03-07)
1. `0002` (`p3`, `in-progress`, `worker-2`, depends on `0004`): finish deterministic PM mirror refresh for ticket lifecycle commands and document drift recovery.
2. `0007` (`p3`, `todo`, `worker-1`): direct Plan-mode dispatch, required before unattended queueing.
3. `0009` (`p3`, `todo`, `worker-1`): safe broad permission profile and loop guards for unattended runs.
4. `0010` (`p3`, `todo`, `worker-1`): dependency and overlap-risk policy for sequential vs parallel routing.
5. `0008` (`p3`, `blocked`, `worker-1`, depends on `0007`, `0009`, `0010`): per-agent sequential task queues for unattended runs.
6. `0006` (`p3`, `todo`, `worker-1`): tmux settings tab and editable config UX.
7. `0005` (`p4`, `todo`, `worker-4`, depends on `0004`): canonical naming and config path consistency across docs.
8. `0003` (`p5`, `blocked`, `worker-3`, depends on `0004` and `0005`): first-run onboarding path.
9. `0001` (`p1`, `done`, `pm`): PM workflow and quality bar baseline.
10. `0004` (`p2`, `done`, `worker-1`): PM policy baseline.

## Operator Preferences (User-Defined)
- Ticket default priority: `p3` unless explicitly overridden by the user.
- Ticket default assignee: `worker-1` unless explicitly overridden by the user.
- PM should not ask the user for priority/assignee during intake.

## PM Operating Loop
1. Startup context load: read `README.md`, `AGENTS.md`, `.swarm/PM_KANBAN.md`, `.swarm/PM_FOCUS.md`.
2. Intake gate (mandatory): ask 1-5 concise clarification questions for new feature/task requests before ticket creation or metadata changes, unless scope is fully specified.
3. Backlog hygiene: verify ticket frontmatter/body quality and dependency correctness.
4. Focus selection: keep exactly one active focus ticket (`in-progress`, owned).
5. Routing: ensure `assigned_to` is explicit for each open ticket and aligned with sequencing.
6. Mirror sync: after ticket edits, refresh `.swarm/PM_KANBAN.md` and `.swarm/PM_FOCUS.md` to match ticket files.
7. Closure: mark done tickets, capture decision deltas in this file, and queue follow-on tickets when scope expands.

## Change Control
- PM may edit open ticket metadata/body to preserve sequencing clarity, ownership clarity, and executable acceptance criteria.
- New tickets are created only when current scope cannot be represented by existing ticket IDs.
- PM must ask 1-5 clarification questions for new requests before creating/updating tickets unless scope is fully specified.
- PM must not implement product code; code requests are translated into executable tickets.
- PM must not ask for priority/assignee; apply user-defined defaults from this file unless user provides explicit overrides.
- Worker assignment choice is user-owned; PM applies declared user defaults rather than autonomous routing decisions.
- Done tickets are immutable except for factual corrections tied to auditability.
- Every ticket edit session must end with mirror sync (`PM_KANBAN`, `PM_FOCUS`) and a `Current State` note.

## Recovery Runbook
- If PM mirrors drift from ticket files, treat `.swarm/tickets/*.md` as authoritative, then rewrite `PM_KANBAN` and `PM_FOCUS` from ticket metadata.
- If ticket metadata is incomplete, normalize frontmatter to include `id`, `title`, `status`, `priority`, `created_by`, `assigned_to` before dispatch.
- If sequencing conflict exists (priority vs dependency), dependency order wins; adjust status/priority/description to remove ambiguity.

## Decision Log
- `2026-03-07` | Priority policy: use `p1`-`p5`, lower number means higher priority, default new-ticket priority is `p3`, tie-break is `ticket id asc`. Rationale: matches current backlog shape and current ticket sorting behavior. Impacted tickets: `0002`, `0005`, `0003`.
- `2026-03-07` | Refresh ownership model: `hybrid`. CLI/startup-generated PM artifacts may refresh automatically, but direct ticket markdown edits require PM mirror sync before closing the edit session. Rationale: matches current implementation while keeping ticket files authoritative. Impacted tickets: `0002`, `0001`.
- `2026-03-07` | Onboarding policy: `hybrid`. Canonical onboarding should combine quick-start docs plus PM artifacts rather than rely on either docs-only or command-only flow. Rationale: README handles install/use while PM docs handle live backlog workflow. Impacted tickets: `0005`, `0003`.

## Acceptance Criteria
- [x] `.swarm/PM_TASK.md` defines outcome, scope, sequence, acceptance criteria, and open questions with owners.
- [x] All open tickets have explicit `assigned_to`, `status`, `priority`, and checkable acceptance criteria.
- [x] Ticket dependencies or blockers are explicit where sequencing requires them.
- [x] `.swarm/PM_KANBAN.md` and `.swarm/PM_FOCUS.md` match ticket metadata (status/priority/assignee).
- [x] PM change-control rules are defined and compatible with ongoing ticket hygiene.
- [x] `0004` policy decisions are documented and propagated to dependent tickets (`0002`, `0005`, `0003`).

## Open Questions
- Q1: Should PM mirror refresh happen automatically after every `ticket` subcommand, or only for a defined subset of commands? Owner: `worker-2` via `0002`.
- Q2: Which unattended-run status fields must be first-class ticket metadata versus derived runtime status? Owner: `worker-1` via `0009` and `0010`.
- Q3: Which single doc should become the canonical naming/config reference once `0005` lands: `README.md` or `.swarm/PM_TASK.md`? Owner: `worker-4` via `0005`.

## PM Ticket Quality Bar
- Frontmatter required: `id`, `title`, `status`, `priority`, `created_by`, `assigned_to`.
- Body required: `## Description`, `## Acceptance Criteria`.
- Conditional body section: `## Dependencies` when sequencing depends on upstream work.
- Conditional body section: `## Blocked By` when status is `blocked`.
- Acceptance criteria must be externally verifiable outcomes, not implementation details.
