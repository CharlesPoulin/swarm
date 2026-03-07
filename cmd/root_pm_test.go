package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpoulin/claude-swarm/internal/config"
)

func TestNormalizePMBootstrapMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "prompt", want: "prompt"},
		{in: "PROMPT", want: "prompt"},
		{in: "full", want: "full"},
		{in: "none", want: "none"},
		{in: "invalid", want: "prompt"},
		{in: "", want: "prompt"},
	}

	for _, tc := range cases {
		if got := normalizePMBootstrapMode(tc.in); got != tc.want {
			t.Fatalf("normalizePMBootstrapMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCliCmdForPMBootstrapModes(t *testing.T) {
	worktree := "/tmp/repo"
	bootstrapPath := filepath.Join(worktree, ".swarm", "PM_BOOTSTRAP.md")
	promptPath := filepath.Join(worktree, ".swarm", "PM_PROMPT.md")

	tests := []struct {
		name       string
		mode       string
		contains   string
		notContain string
	}{
		{
			name:       "full mode uses PM_BOOTSTRAP",
			mode:       "full",
			contains:   `codex "$(cat '` + bootstrapPath + `')"`,
			notContain: "PM_PROMPT.md",
		},
		{
			name:       "prompt mode uses PM_PROMPT",
			mode:       "prompt",
			contains:   `codex "$(cat '` + promptPath + `')"`,
			notContain: "PM_BOOTSTRAP.md",
		},
		{
			name:       "none mode starts codex without injected message",
			mode:       "none",
			contains:   " && codex",
			notContain: "cat '",
		},
		{
			name:       "invalid mode falls back to prompt",
			mode:       "bogus",
			contains:   `codex "$(cat '` + promptPath + `')"`,
			notContain: "PM_BOOTSTRAP.md",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{PMBootstrapMode: tc.mode}
			cmd := cliCmdFor(cfg, "pm", worktree, "")
			if !strings.Contains(cmd, tc.contains) {
				t.Fatalf("expected command to contain %q, got %q", tc.contains, cmd)
			}
			if tc.notContain != "" && strings.Contains(cmd, tc.notContain) {
				t.Fatalf("expected command to not contain %q, got %q", tc.notContain, cmd)
			}
		})
	}
}
