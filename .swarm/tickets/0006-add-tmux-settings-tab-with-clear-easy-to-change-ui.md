---
id: "0006"
title: Add tmux settings tab with clear, easy-to-change UI
status: todo
priority: 3
created_by: human
assigned_to: worker-1
---
## Description
Add a dedicated `settings` tab in the tmux session with a small terminal UI that makes swarm settings easier to understand and change than raw manual file edits. The first release should prioritize clarity, safe edits, and fast access to commonly changed configuration values.

## Scope Notes
- Target surface: tmux session workflow (`claude-swarm` runtime windows/keybinding/docs/tests as needed).
- Non-goal: broad product refactors unrelated to settings workflow.

## Acceptance Criteria
- [ ] A dedicated tmux `settings` tab/window is available during swarm sessions and is reachable through a documented, stable navigation path.
- [ ] The tab presents a clear, minimal settings UI (not just raw file dump) showing key configurable fields with short labels/help text.
- [ ] Users can change at least the most common settings from this UI and persist changes to the canonical config path used by the project.
- [ ] Validation/error handling prevents silent bad writes (invalid input yields explicit guidance and no unclear state).
- [ ] README and/or relevant PM docs include a concise “how to use settings tab” section with one example flow.
- [ ] Automated coverage (or equivalent deterministic checks) is added/updated for the settings-tab workflow and key failure paths.
