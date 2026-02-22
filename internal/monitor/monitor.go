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

// Watch polls a pane for API usage-limit errors and automatically resumes.
// paneID is the stable %N tmux pane identifier.
func Watch(ctx context.Context, cfg *config.Config, session, paneID string, workerNum int, log *os.File) {
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

		content, err := tmux.CapturePane(paneID)
		if err != nil {
			return // pane gone
		}

		if !detected && usagelimit.HasError(content) {
			detected = true

			waitSecs := usagelimit.ExtractWaitSecs(content)
			totalSecs := waitSecs + cfg.ResumeBufferSec
			displayH := totalSecs / 3600
			displayM := (totalSecs % 3600) / 60

			logf("[worker-%d] API usage limit hit. Resuming in %dh %dm.", workerNum, displayH, displayM)

			title := fmt.Sprintf("worker-%d [wait %dh%dm]", workerNum, displayH, displayM)
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

			logf("[worker-%d] Resuming with %s --continue.", workerNum, cfg.CLIType)
			_ = tmux.SendKeys(paneID, cfg.CLIType+" --continue")
			_ = tmux.SetPaneTitle(paneID, fmt.Sprintf("worker-%d", workerNum))
			detected = false
		}
	}
}
