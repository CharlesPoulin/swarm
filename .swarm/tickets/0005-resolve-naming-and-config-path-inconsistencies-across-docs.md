---
id: "0005"
title: Resolve naming and config-path inconsistencies across docs
status: in-progress
priority: 4
created_by: human
assigned_to: worker-5 ⏳ stalled
---
## Description
Audit README, AGENTS, PM bootstrap/prompt docs, and examples for one canonical binary name, config path, and CLI casing so setup instructions are copy/paste reliable.

## Dependencies
- Depends on `0004` for final policy on canonical naming and compatibility language.

## Acceptance Criteria
- [ ] Canonical binary name and config file path are explicitly defined in one source location.
- [ ] README, AGENTS, PM bootstrap/prompt docs, and command examples use the same naming and path conventions.
- [ ] Any intentional aliases or backward-compatible variants are called out explicitly.
- [ ] Quick smoke-check confirms documented commands work verbatim on a clean shell.
