package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/ticket"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type dispatchPhase int

const (
	phaseTask dispatchPhase = iota
	phaseInput
	phaseSelect
	phaseSending
	phaseDone
	phaseDispatchError
)

type workerInfo struct {
	name        string
	cliType     string
	paneID      string
	worktreeDir string
	status      string // "idle" | "in-progress"
	ticketTitle string
}

// tea.Msg types
type workersLoadedMsg []workerInfo
type ticketsLoadedMsg struct {
	tickets []*ticket.Ticket
	err     error
}
type dispatchSentMsg string
type dispatchErrMsg struct{ err error }
type aiRoutedMsg struct{ idx int }

// ── Model ─────────────────────────────────────────────────────────────────────

type dispatchModel struct {
	phase        dispatchPhase
	input        textinput.Model
	tickets      []*ticket.Ticket
	ticketsReady bool
	ticketErr    error
	taskCursor   int // 0 = "new quick task", 1..N = ticket index
	selected     *ticket.Ticket
	taskText     string
	workers      []workerInfo
	workersReady bool
	cursor       int // 0 = auto, 1..N = worker index
	spinner      spinner.Model
	result       string
	err          error
	width        int
	session      string
	ticketsDir   string
}

func newDispatchModel(session, ticketsDir string) dispatchModel {
	ti := textinput.New()
	ti.Placeholder = "Describe the task…"
	ti.Focus()
	ti.CharLimit = 500

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	return dispatchModel{
		phase:      phaseTask,
		input:      ti,
		spinner:    sp,
		session:    session,
		ticketsDir: ticketsDir,
		width:      72,
	}
}

func (m dispatchModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		loadWorkersCmd(m.session),
		loadTicketsCmd(m.ticketsDir),
	)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m dispatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width > 82 {
			m.width = 82
		}
		return m, nil
	case workersLoadedMsg:
		m.workers = []workerInfo(msg)
		m.workersReady = true
		return m, nil
	case ticketsLoadedMsg:
		m.ticketErr = msg.err
		m.tickets = msg.tickets
		m.ticketsReady = true
		total := len(m.tickets) + 1 // +1 for "new quick task"
		if m.taskCursor >= total {
			m.taskCursor = total - 1
		}
		if m.taskCursor < 0 {
			m.taskCursor = 0
		}
		return m, nil
	}

	switch m.phase {
	case phaseTask:
		return m.updateTask(msg)
	case phaseInput:
		return m.updateInput(msg)
	case phaseSelect:
		return m.updateSelect(msg)
	case phaseSending:
		return m.updateSending(msg)
	case phaseDone, phaseDispatchError:
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "enter", "esc", "q", "ctrl+c":
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m dispatchModel) updateTask(msg tea.Msg) (tea.Model, tea.Cmd) {
	total := len(m.tickets) + 1 // +1 for "new quick task"
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, tea.Quit
		case "r":
			return m, tea.Batch(
				loadWorkersCmd(m.session),
				loadTicketsCmd(m.ticketsDir),
			)
		case "up", "k":
			if m.taskCursor > 0 {
				m.taskCursor--
			}
		case "down", "j":
			if m.taskCursor < total-1 {
				m.taskCursor++
			}
		case "n":
			m.selected = nil
			m.taskText = ""
			m.input.SetValue("")
			m.input.Focus()
			m.phase = phaseInput
			return m, nil
		case "enter":
			if m.taskCursor == 0 {
				m.selected = nil
				m.taskText = ""
				m.input.SetValue("")
				m.input.Focus()
				m.phase = phaseInput
				return m, nil
			}
			if !m.ticketsReady || m.ticketErr != nil || len(m.tickets) == 0 {
				return m, nil
			}
			m.selected = m.tickets[m.taskCursor-1]
			m.taskText = m.selected.Title
			m.cursor = 0
			m.phase = phaseSelect
			return m, nil
		}
	}
	return m, nil
}

func (m dispatchModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.phase = phaseTask
			return m, nil
		case "enter":
			task := strings.TrimSpace(m.input.Value())
			if task != "" {
				m.selected = nil
				m.taskText = task
				m.cursor = 0
				m.phase = phaseSelect
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m dispatchModel) updateSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	total := len(m.workers) + 1 // +1 for "auto"
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.selected != nil {
				m.phase = phaseTask
			} else {
				m.phase = phaseInput
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < total-1 {
				m.cursor++
			}
		case "enter":
			if m.workersReady && len(m.workers) > 0 {
				return m.doSend()
			}
		}
	}
	return m, nil
}

func (m dispatchModel) doSend() (tea.Model, tea.Cmd) {
	if len(m.workers) == 0 {
		m.phase = phaseDispatchError
		m.err = fmt.Errorf("no workers found — is the swarm running?")
		return m, nil
	}
	m.phase = phaseSending
	task := m.currentTask()
	selected := m.selected
	cursor := m.cursor
	workers := m.workers
	ticketsDir := m.ticketsDir

	return m, tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			if cursor == 0 {
				// AI picks the worker
				idx, err := aiPickWorker(task, workers)
				if err != nil || idx < 0 {
					idx = firstIdleWorker(workers)
				}
				if idx < 0 {
					idx = 0
				}
				return aiRoutedMsg{idx: idx}
			}
			w := workers[cursor-1]
			if err := sendTaskToWorker(task, selected, &w, ticketsDir); err != nil {
				return dispatchErrMsg{err}
			}
			if selected != nil {
				return dispatchSentMsg(fmt.Sprintf("Ticket %s sent to %s (%s)", selected.ID, w.name, w.cliType))
			}
			return dispatchSentMsg(fmt.Sprintf("Task sent to %s (%s)", w.name, w.cliType))
		},
	)
}

func (m dispatchModel) updateSending(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case aiRoutedMsg:
		idx := msg.idx
		task := m.currentTask()
		selected := m.selected
		workers := m.workers
		ticketsDir := m.ticketsDir
		if idx < 0 || idx >= len(workers) {
			m.phase = phaseDispatchError
			m.err = fmt.Errorf("no eligible workers found")
			return m, nil
		}
		return m, func() tea.Msg {
			w := workers[idx]
			if err := sendTaskToWorker(task, selected, &w, ticketsDir); err != nil {
				return dispatchErrMsg{err}
			}
			if selected != nil {
				return dispatchSentMsg(fmt.Sprintf("AI routed ticket %s to %s (%s)", selected.ID, w.name, w.cliType))
			}
			return dispatchSentMsg(fmt.Sprintf("AI routed task to %s (%s)", w.name, w.cliType))
		}
	case dispatchSentMsg:
		m.phase = phaseDone
		m.result = string(msg)
		return m, nil
	case dispatchErrMsg:
		m.phase = phaseDispatchError
		m.err = msg.err
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

var (
	dTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	dLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	dMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	dCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	dAutoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	dTodoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	dProgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	dDoneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	dBlockStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dNameStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	dCLIStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	dIdleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	dBusyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	dOkStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
	dErrStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

func (m dispatchModel) View() string {
	innerWidth := m.width - 8
	if innerWidth < 44 {
		innerWidth = 44
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33")).
		Padding(1, 2).
		Width(innerWidth)

	header := dTitleStyle.Render("claude-swarm dispatch") +
		"  " + dHintStyle.Render("Ctrl+b D or Alt+5 to jump here")

	switch m.phase {
	case phaseTask:
		var sb strings.Builder
		sb.WriteString(dLabelStyle.Render("Backlog") + "\n")
		cur := "  "
		if m.taskCursor == 0 {
			cur = dCursorStyle.Render("▶ ")
		}
		sb.WriteString(cur + dAutoStyle.Render("+ New quick task") +
			dMutedStyle.Render("  type and dispatch ad-hoc work") + "\n")

		switch {
		case !m.ticketsReady:
			sb.WriteString(dMutedStyle.Render("  Loading tickets…") + "\n")
		case m.ticketErr != nil:
			sb.WriteString("  " + dErrStyle.Render("Failed to load tickets: "+truncate(m.ticketErr.Error(), innerWidth-10)) + "\n")
		case len(m.tickets) == 0:
			sb.WriteString(dMutedStyle.Render("  No tickets found.") + "\n")
		default:
			maxTitle := innerWidth - 44
			if maxTitle < 12 {
				maxTitle = 12
			}
			for i, t := range m.tickets {
				cur = "  "
				if m.taskCursor == i+1 {
					cur = dCursorStyle.Render("▶ ")
				}
				assigned := t.AssignedTo
				if assigned == "" {
					assigned = "-"
				}
				sb.WriteString(fmt.Sprintf("%s[%s] p%-2d %-11s %-10s %s\n",
					cur,
					t.ID,
					t.Priority,
					renderTicketStatus(t.Status),
					truncate(assigned, 10),
					truncate(t.Title, maxTitle),
				))
			}
		}

		sb.WriteString("\n" + dMutedStyle.Render("↑↓/jk navigate · Enter select · n new task · r reload · Esc quit"))
		return header + "\n" + box.Render(sb.String()) + "\n"

	case phaseInput:
		m.input.Width = innerWidth - 4
		body := dLabelStyle.Render("New quick task") + "\n" +
			m.input.View() + "\n\n" +
			dMutedStyle.Render("Enter to choose agent · Esc back")
		return header + "\n" + box.Render(body) + "\n"

	case phaseSelect:
		var sb strings.Builder
		if m.selected != nil {
			sb.WriteString(dLabelStyle.Render("Ticket") + "\n")
			sb.WriteString(dTodoStyle.Render(fmt.Sprintf("[%s] %s", m.selected.ID, m.selected.Title)) + "\n\n")
		}
		sb.WriteString(dLabelStyle.Render("Task") + "\n")
		sb.WriteString(dNameStyle.Render(truncate(m.currentTask(), innerWidth-6)) + "\n\n")
		sb.WriteString(dLabelStyle.Render("Route to") + "\n")

		cur := "  "
		if m.cursor == 0 {
			cur = dCursorStyle.Render("▶ ")
		}
		sb.WriteString(cur + dAutoStyle.Render("✨ Auto") +
			dMutedStyle.Render("  AI picks best available") + "\n")

		if !m.workersReady {
			sb.WriteString(dMutedStyle.Render("  Loading workers…") + "\n")
		} else if len(m.workers) == 0 {
			sb.WriteString(dMutedStyle.Render("  No workers found — is the swarm running?") + "\n")
		} else {
			for i, w := range m.workers {
				cur = "  "
				if m.cursor == i+1 {
					cur = dCursorStyle.Render("▶ ")
				}
				statusDot := dIdleStyle.Render("●")
				statusText := dMutedStyle.Render("idle")
				if w.status == "in-progress" {
					statusDot = dBusyStyle.Render("◉")
					statusText = dBusyStyle.Render(truncate(w.ticketTitle, 30))
				}
				sb.WriteString(fmt.Sprintf("%s%s  %s  %s %s\n",
					cur,
					dNameStyle.Render(fmt.Sprintf("%-10s", w.name)),
					dCLIStyle.Render(fmt.Sprintf("%-8s", w.cliType)),
					statusDot,
					statusText,
				))
			}
		}
		sb.WriteString("\n" + dMutedStyle.Render("↑↓/jk navigate · Enter send · Esc back"))
		return header + "\n" + box.Render(sb.String()) + "\n"

	case phaseSending:
		body := m.spinner.View() + " " + dNameStyle.Render("Dispatching task…")
		return header + "\n" + box.Render(body) + "\n"

	case phaseDone:
		body := dOkStyle.Render("✓ ") + dNameStyle.Render(m.result) + "\n\n" +
			dMutedStyle.Render("Press Enter to exit")
		return header + "\n" + box.Render(body) + "\n"

	case phaseDispatchError:
		body := dErrStyle.Render("✗ ") + dNameStyle.Render(m.err.Error()) + "\n\n" +
			dMutedStyle.Render("Press Enter to exit")
		return header + "\n" + box.Render(body) + "\n"
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func (m dispatchModel) currentTask() string {
	if strings.TrimSpace(m.taskText) != "" {
		return m.taskText
	}
	if m.selected != nil {
		return m.selected.Title
	}
	return strings.TrimSpace(m.input.Value())
}

func renderTicketStatus(status ticket.Status) string {
	s := string(status)
	switch status {
	case ticket.StatusTodo:
		return dTodoStyle.Render(s)
	case ticket.StatusInProgress:
		return dProgStyle.Render(s)
	case ticket.StatusDone:
		return dDoneStyle.Render(s)
	case ticket.StatusBlocked:
		return dBlockStyle.Render(s)
	default:
		return dMutedStyle.Render(s)
	}
}

// ── Worker discovery ──────────────────────────────────────────────────────────

func loadWorkersCmd(session string) tea.Cmd {
	return func() tea.Msg {
		workers, _ := discoverWorkers(session)
		return workersLoadedMsg(workers)
	}
}

func loadTicketsCmd(ticketsDir string) tea.Cmd {
	return func() tea.Msg {
		if ticketsDir == "" {
			return ticketsLoadedMsg{tickets: nil}
		}
		store := ticket.NewStore(ticketsDir)
		tickets, err := store.List()
		return ticketsLoadedMsg{tickets: tickets, err: err}
	}
}

func discoverWorkers(session string) ([]workerInfo, error) {
	if !tmux.HasSession(session) {
		return nil, fmt.Errorf("session %q not found", session)
	}
	panes, err := tmux.ListPanes(fmt.Sprintf("%s:swarm", session))
	if err != nil {
		return nil, err
	}
	var workers []workerInfo
	for _, pane := range panes {
		title := pane.Title
		if !strings.HasPrefix(title, "worker-") {
			continue
		}
		name := title
		cliType := ""
		if idx := strings.Index(title, " ("); idx != -1 {
			name = title[:idx]
			cliType = strings.Trim(title[idx+2:], ")")
		}

		out, err := exec.Command("tmux", "display-message", "-t", pane.ID, "-p", "#{pane_current_path}").Output()
		worktreeDir := ""
		if err == nil {
			worktreeDir = strings.TrimSpace(string(out))
		}

		status := "idle"
		ticketTitle := ""
		if worktreeDir != "" {
			ticketPath := filepath.Join(worktreeDir, "CURRENT_TICKET.md")
			if _, err := os.Stat(ticketPath); err == nil {
				status = "in-progress"
				if t, err := ticket.ParseFile(ticketPath); err == nil && t != nil {
					ticketTitle = t.Title
				}
			}
		}
		workers = append(workers, workerInfo{
			name:        name,
			cliType:     cliType,
			paneID:      pane.ID,
			worktreeDir: worktreeDir,
			status:      status,
			ticketTitle: ticketTitle,
		})
	}
	return workers, nil
}

// ── AI routing ────────────────────────────────────────────────────────────────

// aiPickWorker calls claude (or gemini as fallback) to pick the best worker.
// Returns the 0-based index into workers, or -1 on failure.
func aiPickWorker(task string, workers []workerInfo) (int, error) {
	var sb strings.Builder
	sb.WriteString("You are a task router for an AI coding swarm.\n")
	sb.WriteString("Pick the best agent for the task below. Prefer idle agents.\n\n")
	sb.WriteString("Agents (index | name | type | status):\n")
	for i, w := range workers {
		sb.WriteString(fmt.Sprintf("  %d | %s | %s | %s", i, w.name, w.cliType, w.status))
		if w.ticketTitle != "" {
			sb.WriteString(fmt.Sprintf(" (working on: %q)", w.ticketTitle))
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\nTask: %q\n\n", task))
	sb.WriteString("Reply with ONLY the index number of the chosen agent (e.g. 0, 1, 2).")

	prompt := sb.String()
	var out []byte
	var err error
	switch {
	case commandExists("claude"):
		out, err = exec.Command("claude", "-p", prompt).Output()
	case commandExists("gemini"):
		out, err = exec.Command("gemini", "-p", prompt).Output()
	default:
		return -1, fmt.Errorf("no AI CLI available for auto-routing")
	}
	if err != nil {
		return -1, fmt.Errorf("AI routing failed: %w", err)
	}

	response := strings.TrimSpace(string(out))
	var idx int
	if n, scanErr := fmt.Sscanf(response, "%d", &idx); n == 1 && scanErr == nil {
		if idx >= 0 && idx < len(workers) {
			return idx, nil
		}
	}
	// Fallback: look for a worker name mentioned in the response
	for i, w := range workers {
		if strings.Contains(strings.ToLower(response), strings.ToLower(w.name)) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("could not parse AI response: %q", response)
}

func firstIdleWorker(workers []workerInfo) int {
	for i, w := range workers {
		if w.status == "idle" {
			return i
		}
	}
	if len(workers) > 0 {
		return 0
	}
	return -1
}

// ── Send ──────────────────────────────────────────────────────────────────────

// sendTaskToWorker assigns an existing ticket (if provided) or creates a new one, then prompts the worker.
func sendTaskToWorker(task string, selected *ticket.Ticket, w *workerInfo, ticketsDir string) error {
	if selected != nil {
		t := selected
		if ticketsDir != "" {
			store := ticket.NewStore(ticketsDir)
			if err := store.Assign(t.ID, w.name); err != nil {
				return err
			}
			if fresh, err := store.Get(t.ID); err == nil && fresh != nil {
				t = fresh
			}
		}
		if w.worktreeDir != "" {
			if err := ticket.WriteCurrentTicket(w.worktreeDir, t); err == nil {
				return tmux.SendKeys(w.paneID, "Read CURRENT_TICKET.md and implement the task")
			}
		}
		return tmux.SendKeys(w.paneID, fmt.Sprintf("Ticket %s: %s", t.ID, task))
	}

	if ticketsDir != "" && w.worktreeDir != "" {
		store := ticket.NewStore(ticketsDir)
		t, err := store.Add(task, task, "dispatch")
		if err == nil {
			_ = store.Assign(t.ID, w.name)
			_ = ticket.WriteCurrentTicket(w.worktreeDir, t)
			return tmux.SendKeys(w.paneID, "Read CURRENT_TICKET.md and implement the task")
		}
	}
	return tmux.SendKeys(w.paneID, task)
}

// ── Cobra command ─────────────────────────────────────────────────────────────

func init() {
	dispatchCmd.Flags().StringP("session", "s", "", "tmux session name (overrides config)")
}

var dispatchCmd = &cobra.Command{
	Use:   "dispatch",
	Short: "Open the dispatch TUI to route backlog tickets to agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		config.SetDefaults()
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		// Allow -s flag to override session from config
		if s, _ := cmd.Flags().GetString("session"); s != "" {
			cfg.Session = s
		}
		ticketsDir := cfg.TicketsDir
		if ticketsDir != "" && !filepath.IsAbs(ticketsDir) {
			if repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
				ticketsDir = filepath.Join(strings.TrimSpace(string(repoRoot)), ticketsDir)
			}
		}
		m := newDispatchModel(cfg.Session, ticketsDir)
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err = p.Run()
		return err
	},
}

func dispatchWindowCommand(session string) string {
	return fmt.Sprintf("claude-swarm dispatch --session %s", shellQuote(session))
}
