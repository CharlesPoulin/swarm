package cmd_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cpoulin/claude-swarm/cmd"
)

func TestUsageHelp_Large(t *testing.T) {
	var out, err bytes.Buffer
	args := []string{"usage", "--help"}

	if errCmd := cmd.ExecuteWithArgs(args, &out, &err); errCmd != nil {
		t.Fatalf("usage --help failed: %v", errCmd)
	}

	got := out.String()
	want := "Show per-agent usage state dashboard for a running session"
	if !strings.Contains(got, want) {
		t.Errorf("usage --help output missing %q, got:\n%s", want, got)
	}
}
