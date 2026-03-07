package cmd

import (
	"reflect"
	"testing"

	"github.com/cpoulin/claude-swarm/internal/tmux"
)

func TestRowFromPane(t *testing.T) {
	cases := []struct {
		name      string
		pane      tmux.PaneInfo
		content   string
		checkedAt string
		want      usageRow
	}{
		{
			name:      "Active",
			pane:      tmux.PaneInfo{ID: "%3", Index: 0, Title: "worker-1", CurrentCommand: "claude"},
			content:   "ok",
			checkedAt: "12:00:00",
			want: usageRow{
				Worker: 1, PaneID: "%3", CLI: "claude", Status: "active", StatusDetail: "⚡ active", LastLine: "ok", ResumeIn: "-", CheckedAt: "12:00:00", SortKey: 0, HasSortKey: true,
			},
		},
		{
			name:      "Limited",
			pane:      tmux.PaneInfo{ID: "%11", Index: 2, Title: "worker-2 (codex)", CurrentCommand: "codex"},
			content:   "exceeded your usage limit, retry in 1m30s",
			checkedAt: "12:00:00",
			want: usageRow{
				Worker: 2, PaneID: "%11", CLI: "codex", Status: "usage-limited", StatusDetail: "⚡ active", LastLine: "exceeded your usage limit, retry in 1m30s", ResumeIn: "1m30s", CheckedAt: "12:00:00", SortKey: 2, HasSortKey: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rowFromPane(tc.pane, tc.content, tc.checkedAt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSortUsageRows(t *testing.T) {
	rows := []usageRow{
		{Worker: 3, PaneID: "%9", SortKey: 2, HasSortKey: true},
		{Worker: 1, PaneID: "%8", SortKey: 1, HasSortKey: true},
		{Worker: 2, PaneID: "%7", SortKey: 0, HasSortKey: true},
	}
	sortUsageRows(rows)

	got := make([]int, len(rows))
	for i, r := range rows {
		got[i] = r.Worker
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
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
		t.Errorf("active=%d limited=%d, want 2/1", active, limited)
	}
}

func TestRenderSparkline(t *testing.T) {
	cases := []struct {
		name   string
		values []int
		want   bool
	}{
		{"Empty", []int{}, false},
		{"Single", []int{1}, true},
		{"Multiple", []int{0, 1, 2, 3}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderSparkline(tc.values)
			if (got != "" && got != "-") != tc.want {
				t.Errorf("renderSparkline(%v) = %q, expected non-empty=%v", tc.values, got, tc.want)
			}
		})
	}
}
