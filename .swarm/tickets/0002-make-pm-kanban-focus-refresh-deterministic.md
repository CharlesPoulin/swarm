---
id: "0002"
title: Make PM Kanban/Focus refresh deterministic
status: done
priority: 3
created_by: human
---
## Description
Define and implement a single deterministic refresh model so `.swarm/PM_KANBAN.md` and `.swarm/PM_FOCUS.md` consistently reflect ticket metadata after both CLI actions and direct markdown edits.

## Dependencies
- Depends on `0004` for final policy decisions on refresh ownership and trigger model.

## Acceptance Criteria
- [ ] `0004` decisions for refresh ownership and trigger policy are reflected in the implemented refresh flow.
- [ ] Refresh behavior is explicitly defined for `ticket add`, `ticket assign`, `ticket done`, and direct markdown edits.
- [x] PM views are generated with deterministic ordering: `status bucket -> priority asc -> ticket id asc`.
- [x] Focus selection is deterministic: first `in-progress`, then first `todo`, then first `blocked`, then first `done`.
- [ ] A repeatable drift-recovery workflow is documented with concrete before/after validation steps.

## PM Audit Notes
- `writePMBoard()` and `pickPMFocusTicket()` already enforce deterministic board and focus ordering.
- Ticket lifecycle commands still do not refresh PM mirrors after `ticket add`, `ticket assign`, and `ticket done`, so this ticket remains open.
