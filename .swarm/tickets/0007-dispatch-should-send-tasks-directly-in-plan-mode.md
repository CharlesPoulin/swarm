---
id: "0007"
title: Dispatch should send tasks directly in Plan mode
status: todo
priority: 3
created_by: human
assigned_to: worker-1
---
## Description
When dispatch routes a task, it should open in Plan mode directly and avoid requiring an extra Enter in worker panes. Behavior should cover both quick task text and ticket-based dispatch, with config support and CLI fallback behavior.

## Scope Notes
- Applies to both dispatch entry paths: ad-hoc quick tasks and existing ticket dispatch.
- Default behavior target is all worker CLIs.
- Add a config switch (for example `dispatch_plan_mode`) so operators can disable Plan-mode dispatch if needed.
- If a target CLI does not support Plan mode, fallback to normal dispatch with explicit feedback.

## Acceptance Criteria
- [ ] Dispatch sends directly without requiring an extra Enter in worker panes for both quick-task and ticket-dispatch paths.
- [ ] Plan mode is enabled by default for all worker CLIs targeted by dispatch.
- [ ] A documented config toggle exists to enable/disable Plan-mode dispatch behavior.
- [ ] Unsupported-CLI handling falls back to normal dispatch and surfaces an explicit operator-visible notice.
- [ ] README and/or dispatch help text documents default behavior, toggle usage, and fallback behavior.
- [ ] Deterministic test coverage is added/updated for: quick-task path, ticket path, config toggle off, and unsupported-CLI fallback.
