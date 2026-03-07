---
id: "0003"
title: Improve first-run onboarding
status: blocked
priority: 5
created_by: human
assigned_to: worker-3
---
## Description
Reduce first-run friction by publishing one canonical quick-start flow that validates prerequisites, provides a working PM-enabled config, and leads to a usable board/focus/ticket workflow.

## Dependencies
- Depends on `0004` (policy decisions) and `0005` (canonical naming/path conventions).

## Blocked By
- `0005` must land first so onboarding only documents one canonical binary/config naming path.

## Acceptance Criteria
- [ ] Quick-start documents required tools (`go`, `tmux`, `task`, agent CLIs, optional `gh`) and includes verification commands.
- [ ] Recommended config example includes `pm` usage, practical defaults, and canonical naming/path decisions from `0005`.
- [ ] New users can produce a non-empty PM board/focus and assignable ticket backlog in one guided flow with no undocumented steps.
- [ ] Troubleshooting guidance covers missing dependencies plus at least three concrete setup failures with fix commands.
