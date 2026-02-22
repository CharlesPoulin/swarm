package monitor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/cpoulin/claude-swarm/internal/usagelimit"
)

// Watch polls a tmux window for API usage-limit errors and automatically resumes.
// windowID is the stable tmux @N identifier (does not change on rename).
// It runs until ctx is cancelled.
func Watch(ctx context.Context, cfg *config.Config, session, windowID string, workerNum int, log *os.File) {
	interval := time.Duration(cfg.MonitorInterval) * time.Second
	detected := false

	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(time.Now().UTC().Format("2006-01-02T15:04:05Z")+" "+format+"\n", args...)
		fmt.Print(msg)
		if log != nil {
			fmt.Fprint(log, msg)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		target := fmt.Sprintf("%s:%s", session, windowID)
		content, err := tmux.CapturePane(target)
		if err != nil {
			// Session or window gone — exit silently.
			return
		}

		if !detected && usagelimit.HasError(content) {
			detected = true

			waitSecs := usagelimit.ExtractWaitSecs(content)
			totalSecs := waitSecs + cfg.ResumeBufferSec

			displayH := totalSecs / 3600
			displayM := (totalSecs % 3600) / 60

			logf("[worker-%d] API usage limit hit. Resuming in %dh %dm.", workerNum, displayH, displayM)

			windowName := fmt.Sprintf("w%d[wait %dh%dm]", workerNum, displayH, displayM)
			_ = tmux.RenameWindow(target, windowName)

			// Sleep in small increments so we can respond to cancellation.
			deadline := time.Now().Add(time.Duration(totalSecs) * time.Second)
			for time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}

			// Check session still alive before resuming.
			if !tmux.HasSession(session) {
				return
			}

			logf("[worker-%d] Resuming with %s --continue.", workerNum, cfg.CLIType)
			_ = tmux.SendKeys(target, cfg.CLIType+" --continue")
			_ = tmux.RenameWindow(target, fmt.Sprintf("worker-%d", workerNum))
			detected = false // reset so future limits are caught
		}
	}
}
