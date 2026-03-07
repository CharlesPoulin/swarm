package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
)

type pmWorkbenchPaths struct {
	ticketsDir string
	kanbanPath string
	focusPath  string
}

const pmNetrwTreeWidth = 30

func newPMWorkbenchPaths(repoRoot string) pmWorkbenchPaths {
	swarmDir := filepath.Join(repoRoot, ".swarm")
	return pmWorkbenchPaths{
		ticketsDir: filepath.Join(swarmDir, "tickets"),
		kanbanPath: filepath.Join(swarmDir, "PM_KANBAN.md"),
		focusPath:  filepath.Join(swarmDir, "PM_FOCUS.md"),
	}
}

func pmTicketsWorkbenchCmd(repoRoot string) string {
	paths := newPMWorkbenchPaths(repoRoot)

	switch {
	case commandExists("nvim"):
		return buildPMVimWorkbenchCmd("nvim", paths)
	case commandExists("vim"):
		return buildPMVimWorkbenchCmd("vim", paths)
	case commandExists("nano"):
		return buildPMNanoWorkbenchCmd(paths)
	default:
		return buildPMShellWorkbenchCmd(paths)
	}
}

func buildPMVimWorkbenchCmd(editor string, paths pmWorkbenchPaths) string {
	editorArgs := buildPMVimEditorArgs(paths)
	return joinWithAnd(
		ensureDirectoryCmd(paths.ticketsDir),
		fmt.Sprintf("%s %s", editor, strings.Join(editorArgs, " ")),
	)
}

func buildPMVimEditorArgs(paths pmWorkbenchPaths) []string {
	editorArgs := []string{shellQuote(paths.kanbanPath)}
	for _, cmd := range pmVimExCommands(paths) {
		editorArgs = append(editorArgs, "-c", shellQuote(cmd))
	}
	return editorArgs
}

func pmVimExCommands(paths pmWorkbenchPaths) []string {
	// Layout:
	// - Top: PM_KANBAN.md
	// - Bottom-left: ticket tree
	// - Bottom-right: editable file (starts at PM_FOCUS.md)
	return []string{
		"set mouse=a",
		"let g:netrw_banner=0",
		"let g:netrw_liststyle=3",
		"let g:netrw_browse_split=4",
		fmt.Sprintf("let g:netrw_winsize=%d", pmNetrwTreeWidth),
		"belowright split " + paths.focusPath,
		"Lexplore " + paths.ticketsDir,
		"wincmd l",
	}
}

func buildPMNanoWorkbenchCmd(paths pmWorkbenchPaths) string {
	return joinWithAnd(
		ensureDirectoryCmd(paths.ticketsDir),
		fmt.Sprintf("nano %s", shellQuote(paths.kanbanPath)),
	)
}

func buildPMShellWorkbenchCmd(paths pmWorkbenchPaths) string {
	return joinWithAnd(
		fmt.Sprintf("mkdir -p %s", shellQuote(paths.ticketsDir)),
		fmt.Sprintf("ls -la %s", shellQuote(paths.ticketsDir)),
		fmt.Sprintf("echo %s", shellQuote(fmt.Sprintf("Edit ticket files under %s manually.", paths.ticketsDir))),
		"exec bash",
	)
}

func ensureDirectoryCmd(dir string) string {
	return fmt.Sprintf("[ -d %s ] || mkdir -p %s", shellQuote(dir), shellQuote(dir))
}

func joinWithAnd(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	return strings.Join(nonEmpty, " && ")
}
