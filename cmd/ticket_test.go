package cmd

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/ticket"
	"github.com/spf13/viper"
)

func TestTicketLifecycleCommandsRefreshPMArtifacts(t *testing.T) {
	repoRoot := setupTicketCommandRepo(t)

	if err := ExecuteWithArgs([]string{"ticket", "add", "--title", "Alpha", "--desc", "First ticket"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("ticket add: %v", err)
	}

	kanbanPath := filepath.Join(repoRoot, ".swarm", "PM_KANBAN.md")
	focusPath := filepath.Join(repoRoot, ".swarm", "PM_FOCUS.md")

	kanban := mustReadFile(t, kanbanPath)
	if !strings.Contains(kanban, "## Todo\n- [0001] Alpha (p10, assigned: -)") {
		t.Fatalf("expected todo ticket in PM_KANBAN after add, got:\n%s", kanban)
	}

	focus := mustReadFile(t, focusPath)
	if !strings.Contains(focus, "## [0001] Alpha") || !strings.Contains(focus, "Status: `todo`") {
		t.Fatalf("expected todo ticket in PM_FOCUS after add, got:\n%s", focus)
	}

	worktreeDir := filepath.Join(repoRoot, ".wt-1")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	if err := ExecuteWithArgs([]string{"ticket", "assign", "0001", "worker-1"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("ticket assign: %v", err)
	}

	currentTicket := mustReadFile(t, filepath.Join(worktreeDir, "CURRENT_TICKET.md"))
	if !strings.Contains(currentTicket, "status: in-progress") || !strings.Contains(currentTicket, "assigned_to: worker-1") {
		t.Fatalf("expected CURRENT_TICKET to include updated assignment metadata, got:\n%s", currentTicket)
	}

	kanban = mustReadFile(t, kanbanPath)
	if !strings.Contains(kanban, "## In Progress\n- [0001] Alpha (p10, assigned: worker-1)") {
		t.Fatalf("expected in-progress ticket in PM_KANBAN after assign, got:\n%s", kanban)
	}

	focus = mustReadFile(t, focusPath)
	if !strings.Contains(focus, "Status: `in-progress`") {
		t.Fatalf("expected in-progress focus after assign, got:\n%s", focus)
	}

	if err := ExecuteWithArgs([]string{"ticket", "done", "0001"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("ticket done: %v", err)
	}

	kanban = mustReadFile(t, kanbanPath)
	if !strings.Contains(kanban, "## Done\n- [0001] Alpha (p10, assigned: -)") {
		t.Fatalf("expected done ticket in PM_KANBAN after done, got:\n%s", kanban)
	}

	focus = mustReadFile(t, focusPath)
	if !strings.Contains(focus, "Status: `done`") {
		t.Fatalf("expected done focus after done, got:\n%s", focus)
	}
}

func TestTicketRefreshCommandRecoversDirectMarkdownEdits(t *testing.T) {
	repoRoot := setupTicketCommandRepo(t)
	store := ticket.NewStore(filepath.Join(repoRoot, ".swarm", "tickets"))

	alpha, err := store.Add("Alpha", "First ticket", "human")
	if err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	beta, err := store.Add("Beta", "Second ticket", "human")
	if err != nil {
		t.Fatalf("add beta: %v", err)
	}

	if err := writePMArtifacts(repoRoot); err != nil {
		t.Fatalf("initial PM artifact write: %v", err)
	}

	rewriteTicketFile(t, store.Dir(), alpha.ID, func(content string) string {
		content = strings.Replace(content, "status: todo", "status: blocked", 1)
		content = strings.Replace(content, "priority: 10", "priority: 5", 1)
		content = strings.Replace(content, "assigned_to:", "assigned_to: worker-2", 1)
		if !strings.Contains(content, "assigned_to: worker-2") {
			content = strings.Replace(content, "created_by: human\n", "created_by: human\nassigned_to: worker-2\n", 1)
		}
		return content
	})
	rewriteTicketFile(t, store.Dir(), beta.ID, func(content string) string {
		content = strings.Replace(content, "status: todo", "status: in-progress", 1)
		content = strings.Replace(content, "priority: 10", "priority: 1", 1)
		content = strings.Replace(content, "assigned_to:", "assigned_to: worker-1", 1)
		if !strings.Contains(content, "assigned_to: worker-1") {
			content = strings.Replace(content, "created_by: human\n", "created_by: human\nassigned_to: worker-1\n", 1)
		}
		return content
	})

	staleKanban := mustReadFile(t, filepath.Join(repoRoot, ".swarm", "PM_KANBAN.md"))
	if strings.Contains(staleKanban, "## In Progress\n- [0002] Beta (p1, assigned: worker-1)") {
		t.Fatalf("expected PM_KANBAN to be stale before refresh, got:\n%s", staleKanban)
	}

	if err := ExecuteWithArgs([]string{"ticket", "refresh"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("ticket refresh: %v", err)
	}

	kanban := mustReadFile(t, filepath.Join(repoRoot, ".swarm", "PM_KANBAN.md"))
	if !strings.Contains(kanban, "## In Progress\n- [0002] Beta (p1, assigned: worker-1)") {
		t.Fatalf("expected refreshed in-progress ticket in PM_KANBAN, got:\n%s", kanban)
	}
	if !strings.Contains(kanban, "## Blocked\n- [0001] Alpha (p5, assigned: worker-2)") {
		t.Fatalf("expected refreshed blocked ticket in PM_KANBAN, got:\n%s", kanban)
	}

	focus := mustReadFile(t, filepath.Join(repoRoot, ".swarm", "PM_FOCUS.md"))
	if !strings.Contains(focus, "## [0002] Beta") || !strings.Contains(focus, "Status: `in-progress`") {
		t.Fatalf("expected PM_FOCUS to pick refreshed in-progress ticket, got:\n%s", focus)
	}
}

func setupTicketCommandRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	runGitCmd(t, repoRoot, "init", "-q")

	swarmDir := filepath.Join(repoRoot, ".swarm")
	if err := os.MkdirAll(filepath.Join(swarmDir, "tickets"), 0o755); err != nil {
		t.Fatalf("mkdir tickets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(swarmDir, "PM_TASK.md"), []byte("# PM Task\n"), 0o644); err != nil {
		t.Fatalf("write PM_TASK.md: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	viper.Reset()
	config.SetDefaults()

	return repoRoot
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func rewriteTicketFile(t *testing.T, ticketsDir, id string, fn func(string) string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(ticketsDir, id+"-*.md"))
	if err != nil {
		t.Fatalf("glob ticket %s: %v", id, err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one ticket file for %s, got %v", id, matches)
	}
	path := matches[0]
	content := mustReadFile(t, path)
	if err := os.WriteFile(path, []byte(fn(content)), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", path, err)
	}
}
