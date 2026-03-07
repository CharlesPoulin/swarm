package cmd

import (
	"testing"
	"time"
)

func TestUsageModel_Medium(t *testing.T) {
	m := newUsageModel("test-session", 1*time.Second)

	m.collect = func(session string) ([]usageRow, error) {
		return []usageRow{
			{Worker: 1, Status: "active"},
			{Worker: 2, Status: "usage-limited"},
		}, nil
	}

	res, _ := m.Update(refreshMsg{})
	updated := res.(usageModel)

	if len(updated.rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(updated.rows))
	}

	active, limited := summarizeRows(updated.rows)
	if active != 1 || limited != 1 {
		t.Errorf("active=%d limited=%d, want 1/1", active, limited)
	}
}
