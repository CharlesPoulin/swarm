package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cpoulin/claude-swarm/internal/ticket"
)

func TestSendTaskToWorker_QuickTaskPlanMode(t *testing.T) {
	ticketsDir := t.TempDir()
	worktreeDir := t.TempDir()
	w := &workerInfo{
		name:        "worker-1",
		cliType:     "codex",
		paneID:      "%1",
		worktreeDir: worktreeDir,
	}
	calls := captureDispatchCalls(t)

	notice, err := sendTaskToWorker("Investigate flaky test", nil, w, ticketsDir, true)
	if err != nil {
		t.Fatalf("sendTaskToWorker: %v", err)
	}
	if notice != "" {
		t.Fatalf("unexpected notice: %q", notice)
	}

	want := [][]string{{"/plan", "Read CURRENT_TICKET.md and implement the task"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("dispatch calls mismatch\n got: %#v\nwant: %#v", *calls, want)
	}

	currentTicketPath := filepath.Join(worktreeDir, "CURRENT_TICKET.md")
	if _, err := os.Stat(currentTicketPath); err != nil {
		t.Fatalf("expected CURRENT_TICKET.md to be written: %v", err)
	}
}

func TestSendTaskToWorker_TicketPlanMode(t *testing.T) {
	ticketsDir := t.TempDir()
	worktreeDir := t.TempDir()
	store := ticket.NewStore(ticketsDir)
	created, err := store.Add("Implement cache invalidation", "details", "test")
	if err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	w := &workerInfo{
		name:        "worker-2",
		cliType:     "claude",
		paneID:      "%2",
		worktreeDir: worktreeDir,
	}
	calls := captureDispatchCalls(t)

	notice, err := sendTaskToWorker(created.Title, created, w, ticketsDir, true)
	if err != nil {
		t.Fatalf("sendTaskToWorker: %v", err)
	}
	if notice != "" {
		t.Fatalf("unexpected notice: %q", notice)
	}

	want := [][]string{{"/plan", "Read CURRENT_TICKET.md and implement the task"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("dispatch calls mismatch\n got: %#v\nwant: %#v", *calls, want)
	}

	updated, err := store.Get(created.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if updated.AssignedTo != "worker-2" {
		t.Fatalf("assigned_to=%q, want %q", updated.AssignedTo, "worker-2")
	}
}

func TestSendTaskToWorker_DispatchPlanModeOff(t *testing.T) {
	ticketsDir := t.TempDir()
	worktreeDir := t.TempDir()
	w := &workerInfo{
		name:        "worker-3",
		cliType:     "codex",
		paneID:      "%3",
		worktreeDir: worktreeDir,
	}
	calls := captureDispatchCalls(t)

	notice, err := sendTaskToWorker("Refactor parser", nil, w, ticketsDir, false)
	if err != nil {
		t.Fatalf("sendTaskToWorker: %v", err)
	}
	if notice != "" {
		t.Fatalf("unexpected notice: %q", notice)
	}

	want := [][]string{{"Read CURRENT_TICKET.md and implement the task"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("dispatch calls mismatch\n got: %#v\nwant: %#v", *calls, want)
	}
}

func TestSendTaskToWorker_UnsupportedCLIFallback(t *testing.T) {
	ticketsDir := t.TempDir()
	worktreeDir := t.TempDir()
	w := &workerInfo{
		name:        "worker-4",
		cliType:     "spare",
		paneID:      "%4",
		worktreeDir: worktreeDir,
	}
	calls := captureDispatchCalls(t)

	notice, err := sendTaskToWorker("Write release notes", nil, w, ticketsDir, true)
	if err != nil {
		t.Fatalf("sendTaskToWorker: %v", err)
	}
	if !strings.Contains(notice, `Plan mode not supported by CLI "spare"`) {
		t.Fatalf("fallback notice missing unsupported-CLI message: %q", notice)
	}

	want := [][]string{{"Read CURRENT_TICKET.md and implement the task"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("dispatch calls mismatch\n got: %#v\nwant: %#v", *calls, want)
	}
}

func captureDispatchCalls(t *testing.T) *[][]string {
	t.Helper()

	origSend := dispatchSendKeys
	origSendLines := dispatchSendKeysLines

	calls := make([][]string, 0, 1)
	dispatchSendKeys = func(_ string, keys string) error {
		calls = append(calls, []string{keys})
		return nil
	}
	dispatchSendKeysLines = func(_ string, lines ...string) error {
		copied := append([]string(nil), lines...)
		calls = append(calls, copied)
		return nil
	}

	t.Cleanup(func() {
		dispatchSendKeys = origSend
		dispatchSendKeysLines = origSendLines
	})

	return &calls
}
