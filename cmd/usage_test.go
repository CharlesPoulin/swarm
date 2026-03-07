package cmd

import (
	"reflect"
	"testing"

	"github.com/cpoulin/claude-swarm/internal/tmux"
)

func TestRowFromPaneUsageLimited(t *testing.T) {
	p := tmux.PaneInfo{ID: "%11", Index: 2, Title: "worker-2 (codex)", CurrentCommand: "codex"}
	row := rowFromPane(p, "You have exceeded your usage limit for today. Retry in 1m30s.", "12:00:00")

	if row.Worker != 2 {
		t.Fatalf("worker=%d want 2", row.Worker)
	}
	if row.CLI != "codex" {
		t.Fatalf("cli=%q want codex", row.CLI)
	}
	if row.Status != "usage-limited" {
		t.Fatalf("status=%q want usage-limited", row.Status)
	}
	if row.ResumeIn != "1m30s" {
		t.Fatalf("resume_in=%q want 1m30s", row.ResumeIn)
	}
}

func TestRowFromPaneActive(t *testing.T) {
	p := tmux.PaneInfo{ID: "%3", Index: 0, Title: "worker-1", CurrentCommand: "claude"}
	row := rowFromPane(p, "everything is normal", "12:00:00")

	if row.Status != "active" {
		t.Fatalf("status=%q want active", row.Status)
	}
	if row.ResumeIn != "-" {
		t.Fatalf("resume_in=%q want -", row.ResumeIn)
	}
	if row.CLI != "claude" {
		t.Fatalf("cli=%q want claude", row.CLI)
	}
}

func TestSortUsageRows(t *testing.T) {
	rows := []usageRow{
		{Worker: 3, PaneID: "%9", SortKey: 2, HasSortKey: true},
		{Worker: 1, PaneID: "%8", SortKey: 1, HasSortKey: true},
		{Worker: 2, PaneID: "%7", SortKey: 0, HasSortKey: true},
	}
	sortUsageRows(rows)

	got := []int{rows[0].Worker, rows[1].Worker, rows[2].Worker}
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("order=%v want [1 2 3]", got)
	}
}

func TestSummarizeRows(t *testing.T) {
	rows := []usageRow{
		{Status: "active"},
		{Status: "usage-limited"},
		{Status: "active"},
	}
	active, limited := summarizeRows(rows)
	if active != 2 || limited != 1 {
		t.Fatalf("active=%d limited=%d want 2/1", active, limited)
	}
}

func TestRenderSparkline(t *testing.T) {
	got := renderSparkline([]int{0, 1, 2, 3})
	if got == "" || got == "-" {
		t.Fatalf("sparkline=%q expected non-empty graph", got)
	}
}
