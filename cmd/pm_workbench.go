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
	return []string{
		"-n",
		shellQuote(paths.kanbanPath),
		"-c",
		shellQuote(pmVimExCommand(paths)),
	}
}

func pmVimExCommand(paths pmWorkbenchPaths) string {
	// Layout:
	// - Top: PM_KANBAN.md
	// - Bottom-left: ticket tree
	// - Bottom-right: editable file (starts at PM_FOCUS.md)
	openTicketMapping := "nnoremap <silent> <buffer> <CR> :wincmd l<CR>:execute 'edit ' . fnameescape(" +
		vimSingleQuote(paths.ticketsDir+"/") + " . getline('.'))<CR>"
	refreshTicketList := "nnoremap <silent> <buffer> r :execute '0read !ls -1 ' . shellescape(" +
		vimSingleQuote(paths.ticketsDir) + ")<Bar>1delete _<CR>"

	commands := []string{
		"set mouse=a",
		"let g:netrw_banner=0",
		"let g:netrw_liststyle=3",
		"let g:netrw_browse_split=4",
		fmt.Sprintf("let g:netrw_winsize=%d", pmNetrwTreeWidth),
		"execute 'belowright split ' . fnameescape(" + vimSingleQuote(paths.focusPath) + ")",
		"if exists(':Lexplore')",
		"execute 'Lexplore ' . fnameescape(" + vimSingleQuote(paths.ticketsDir) + ")",
		"else",
		fmt.Sprintf("execute 'leftabove vertical %dnew'", pmNetrwTreeWidth),
		"file PM_TICKETS",
		"setlocal buftype=nofile bufhidden=wipe noswapfile nobuflisted nowrap nonumber norelativenumber",
		"execute '0read !ls -1 ' . shellescape(" + vimSingleQuote(paths.ticketsDir) + ")",
		"1delete _",
		openTicketMapping,
		refreshTicketList,
		"endif",
		"wincmd l",
		"let g:netrw_chgwin=winnr()",
		"nnoremap <silent> <leader>pt :wincmd h<CR>",
		"nnoremap <silent> <leader>pe :wincmd l<CR>",
		"nnoremap <silent> <leader>pk :wincmd k<CR>",
	}
	return strings.Join(commands, " | ")
}

func vimSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
