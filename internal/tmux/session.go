package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

func run(args ...string) error {
	cmd := exec.Command("tmux", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// HasSession reports whether a tmux session with the given name exists.
func HasSession(session string) bool {
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// KillSession kills the named tmux session.
func KillSession(session string) error {
	return run("kill-session", "-t", session)
}

// NewSession creates a detached session; the first window starts in cwd at the given dimensions.
// windowName is applied to the initial window at creation time (avoids index assumptions).
func NewSession(session, cwd string, width, height int, windowName string) error {
	args := []string{"new-session", "-d", "-s", session, "-c", cwd,
		"-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height)}
	if windowName != "" {
		args = append(args, "-n", windowName)
	}
	return run(args...)
}

// GetWindowID returns the stable @N window ID for a target (e.g. "session:worker-1").
// The @ID does not change when the window is renamed, so it is safe to use as a long-lived target.
func GetWindowID(target string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-t", target, "-p", "#{window_id}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux display-message -t %s: %w", target, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// NewWindow creates a new named window at index idx inside session, starting in cwd.
func NewWindow(session, cwd, name string, idx int) error {
	args := []string{"new-window", "-t", fmt.Sprintf("%s:%d", session, idx), "-c", cwd}
	if name != "" {
		args = append(args, "-n", name)
	}
	return run(args...)
}

// NewWindowNoIndex creates a new named window at the end of the window list.
func NewWindowNoIndex(session, cwd, name string) error {
	args := []string{"new-window", "-t", session, "-c", cwd}
	if name != "" {
		args = append(args, "-n", name)
	}
	return run(args...)
}

// SendKeys sends keystrokes to a pane (target can be "session:window" or "session:window.pane").
func SendKeys(target, keys string) error {
	return run("send-keys", "-t", target, keys, "Enter")
}

// RenameWindow renames a window identified by target.
func RenameWindow(target, name string) error {
	return run("rename-window", "-t", target, name)
}

// CapturePane returns the visible content of a pane.
func CapturePane(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p").Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane -t %s: %w", target, err)
	}
	return string(out), nil
}

// SetOption sets a tmux option on a session.
func SetOption(session, key, value string) error {
	return run("set-option", "-t", session, key, value)
}

// BindKey binds a key in a session's key table.
// flags may be e.g. "-n" (no prefix) or "" (use prefix).
func BindKey(session, flags, key, command string) error {
	args := []string{"bind-key"}
	if flags != "" {
		args = append(args, flags)
	}
	args = append(args, key, command)
	return run(args...)
}

// SelectWindow selects (focuses) a window by target.
func SelectWindow(target string) error {
	return run("select-window", "-t", target)
}

// SelectPane selects a pane by target.
func SelectPane(target string) error {
	return run("select-pane", "-t", target)
}

// SplitWindow splits a pane. horizontal=true means side-by-side (-h).
func SplitWindow(target, cwd string, percent int, horizontal bool) error {
	args := []string{"split-window", "-t", target}
	if horizontal {
		args = append(args, "-h")
	}
	args = append(args, "-p", fmt.Sprintf("%d", percent), "-c", cwd)
	return run(args...)
}

// ListWindowIndices returns all window indices in the session, sorted ascending.
func ListWindowIndices(session string) ([]int, error) {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_index}").Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows -t %s: %w", session, err)
	}
	var indices []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(line, "%d", &idx); err == nil {
			indices = append(indices, idx)
		}
	}
	return indices, nil
}

// MaxWindowIndex returns the highest window index in the session.
func MaxWindowIndex(session string) (int, error) {
	indices, err := ListWindowIndices(session)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, idx := range indices {
		if idx > max {
			max = idx
		}
	}
	return max, nil
}
