package monitor

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/ticket"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/cpoulin/claude-swarm/internal/usagelimit"
)

var shellPromptRe = regexp.MustCompile(`(?m)[\$>]\s*$`)

// looksLikeShellPrompt returns true when the last non-empty line of content
// ends with a shell prompt indicator ($ or >).
func looksLikeShellPrompt(content string) bool {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return shellPromptRe.MatchString(line)
		}
	}
	return false
}

// Watch polls a pane for API usage-limit errors and automatically resumes.
// In sequential mode it also detects shell prompts (agent exited) and assigns
// the next ticket from the backlog.
// paneID is the stable %N tmux pane identifier.
func Watch(ctx context.Context, cfg *config.Config, session, paneID string, workerNum int, cliCmd, worktreeDir string, w io.Writer) {
	interval := time.Duration(cfg.MonitorInterval) * time.Second
	lastContent := ""
	stallCount := 0
	failureCount := 0

	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(time.Now().UTC().Format("2006-01-02T15:04:05Z")+" "+format+"\n", args...)
		_, _ = fmt.Fprint(w, msg)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		content, err := tmux.CapturePane(paneID)
		if err != nil {
			return // pane gone
		}

		// Logic for detecting frustration/stalls
		if content == lastContent && content != "" {
			stallCount++
		} else {
			stallCount = 0
		}
		lastContent = content

		// Simple check for common error markers in the last few lines
		if IsStruggling(content) {
			failureCount++
		} else {
			if failureCount > 0 {
				failureCount--
			}
		}

		if usagelimit.HasError(content) {
			waitSecs := usagelimit.ExtractWaitSecs(content)
			totalSecs := waitSecs + cfg.ResumeBufferSec
			displayH := totalSecs / 3600
			displayM := (totalSecs % 3600) / 60

			logf("[worker-%d] API usage limit hit. Resuming in %dh %dm.", workerNum, displayH, displayM)

			title := fmt.Sprintf("worker-%d ⏳ [wait %dh%dm]", workerNum, displayH, displayM)
			_ = tmux.SetPaneTitle(paneID, title)

			deadline := time.Now().Add(time.Duration(totalSecs) * time.Second)
			for time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}

			if !tmux.HasSession(session) {
				return
			}

			logf("[worker-%d] Resuming with %s --continue.", workerNum, cliCmd)
			_ = tmux.SendKeys(paneID, cliCmd+" --continue")
			_ = tmux.SetPaneTitle(paneID, fmt.Sprintf("worker-%d ⚡", workerNum))
			continue
		}

		if cfg.AssignmentMode == "sequential" && looksLikeShellPrompt(content) {
			store := ticket.NewStore(cfg.TicketsDir)
			t, nextErr := store.NextTodo()
			if nextErr == nil && t != nil {
				worker := fmt.Sprintf("worker-%d", workerNum)
				_ = store.Assign(t.ID, worker)
				if writeErr := ticket.WriteCurrentTicket(worktreeDir, t); writeErr == nil {
					logf("[worker-%d] Assigning next ticket %s: %s", workerNum, t.ID, t.Title)
					_ = tmux.SendKeys(paneID, cliCmd+fmt.Sprintf(` --message "$(cat '%s/CURRENT_TICKET.md')"`, worktreeDir))
				}
			}
			continue
		}

		// Update status in title if not in wait state or assignment state
		status := ExtractStatus(content)
		if failureCount >= 3 {
			status = "🤯 struggling"
		} else if stallCount >= 10 { // approx 5 mins if interval is 30s
			status = "⏳ stalled"
		} else if usagelimit.HasWarning(content) {
			status = usagelimit.ExtractWarningLabel(content)
		}
		_ = tmux.SetPaneTitle(paneID, fmt.Sprintf("worker-%d %s", workerNum, status))
	}
}

func IsStruggling(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	// Check last 5 lines for error markers
	start := len(lines) - 5
	if start < 0 {
		start = 0
	}
	for i := start; i < len(lines); i++ {
		line := strings.ToLower(lines[i])
		if strings.Contains(line, "failed") ||
			strings.Contains(line, "error") ||
			strings.Contains(line, "not found") ||
			strings.Contains(line, "timed out") ||
			strings.Contains(line, "try again") {
			return true
		}
	}
	return false
}

func ExtractStatus(content string) string {
	// Simple heuristic to find what the agent is doing
	// This can be refined based on specific CLI output patterns
	if content == "" {
		return "💤 idle"
	}
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Look for activity markers
		if strings.Contains(line, "Thinking") || strings.Contains(line, "thinking") {
			return "🧠 thinking"
		}
		if strings.Contains(line, "Reading") || strings.Contains(line, "reading") {
			return "📖 reading"
		}
		if strings.Contains(line, "Editing") || strings.Contains(line, "editing") {
			return "📝 editing"
		}
		if strings.Contains(line, "Executing") || strings.Contains(line, "executing") {
			return "⚙️ executing"
		}
		if strings.Contains(line, "Searching") || strings.Contains(line, "searching") {
			return "🔍 searching"
		}
		if strings.Contains(line, "Done") || strings.Contains(line, "done") {
			return "✅ done"
		}
	}
	return "⚡ active"
}
