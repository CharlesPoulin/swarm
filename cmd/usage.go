package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/monitor"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/cpoulin/claude-swarm/internal/usagelimit"
	"github.com/spf13/cobra"
)

type usageRow struct {
	Worker       int
	PaneID       string
	CLI          string
	Status       string
	StatusDetail string
	LastLine     string
	ResumeIn     string
	CheckedAt    string
	SortKey      int
	HasSortKey   bool
}

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show per-agent usage state dashboard for a running session",
	RunE:  runUsage,
}

func init() {
	f := usageCmd.Flags()
	f.String("session", "", "tmux session name (defaults to config session)")
	f.Int("refresh-secs", 0, "refresh interval in seconds (defaults to monitor_interval)")
	rootCmd.AddCommand(usageCmd)
}

func runUsage(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	session, _ := cmd.Flags().GetString("session")
	if strings.TrimSpace(session) == "" {
		session = cfg.Session
	}
	if !tmux.HasSession(session) {
		return fmt.Errorf("session %q not found", session)
	}

	refreshSecs, _ := cmd.Flags().GetInt("refresh-secs")
	if refreshSecs <= 0 {
		refreshSecs = cfg.MonitorInterval
	}
	if refreshSecs <= 0 {
		refreshSecs = 30
	}

	m := newUsageModel(session, time.Duration(refreshSecs)*time.Second)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	if err != nil {
		return err
	}
	return nil
}

type refreshMsg struct{}

type usageModel struct {
	session        string
	refresh        time.Duration
	table          table.Model
	rows           []usageRow
	lastErr        string
	limitedHistory []int
	collect        func(string) ([]usageRow, error)
	viewMode       string // "table" or "cards"
}

func newUsageModel(session string, refresh time.Duration) usageModel {
	cols := []table.Column{
		{Title: "Worker", Width: 8},
		{Title: "Pane", Width: 8},
		{Title: "CLI", Width: 14},
		{Title: "Status", Width: 18},
		{Title: "Resume In", Width: 12},
		{Title: "Last Checked", Width: 12},
	}

	t := table.New(table.WithColumns(cols), table.WithRows(nil), table.WithFocused(true), table.WithHeight(14))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("33")).Bold(true)
	t.SetStyles(styles)

	return usageModel{
		session:  session,
		refresh:  refresh,
		table:    t,
		collect:  collectUsageRows,
		viewMode: "cards",
	}
}

func (m usageModel) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m usageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "t":
			if m.viewMode == "table" {
				m.viewMode = "cards"
			} else {
				m.viewMode = "table"
			}
		}
	case tea.WindowSizeMsg:
		if msg.Height > 8 {
			m.table.SetHeight(msg.Height - 8)
		}
	case refreshMsg:
		rows, err := m.collect(m.session)
		if err != nil {
			m.lastErr = err.Error()
		} else {
			m.lastErr = ""
			m.rows = rows
			m.table.SetRows(toTableRows(rows))
			_, limited := summarizeRows(rows)
			m.limitedHistory = append(m.limitedHistory, limited)
			if len(m.limitedHistory) > 30 {
				m.limitedHistory = m.limitedHistory[len(m.limitedHistory)-30:]
			}
		}
		return m, m.refreshCmd()
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m usageModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("33")).
		Padding(0, 1).
		MarginBottom(1)

	title := titleStyle.Render("🤖 SWARM DASHBOARD")
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
		fmt.Sprintf(" session=%s | refresh=%s | q:quit", m.session, m.refresh.String()),
	)

	active, limited := summarizeRows(m.rows)
	topCLI, topCount := topCLI(m.rows)

	summaryStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		MarginBottom(1)

	summaryContent := fmt.Sprintf("✅ Active: %d  |  ⏳ Limited: %d  |  🤖 Total: %d  |  🏆 Top: %s (%d)",
		active, limited, len(m.rows), topCLI, topCount)
	summary := summaryStyle.Render(summaryContent)

	graphLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Limited Trend: ")
	graph := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true).Render(renderSparkline(m.limitedHistory))
	header := lipgloss.JoinVertical(lipgloss.Left, title+sub, summary, graphLabel+graph)

	var body string
	if len(m.rows) == 0 {
		body = "\n  No workers found in swarm window."
	} else if m.viewMode == "table" {
		body = m.table.View()
	} else {
		// Create cards for each worker
		var cards []string
		cardStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Width(45).
			Height(6).
			Padding(0, 1).
			Margin(0, 1, 1, 0)

		for _, r := range m.rows {
			workerTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render(fmt.Sprintf("Worker %d", r.Worker))
			cliInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(fmt.Sprintf("[%s] (%s)", r.CLI, r.PaneID))

			statusColor := "34" // green
			if r.Status == "usage-limited" {
				statusColor = "196" // red
			}
			status := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true).Render(r.StatusDetail)

			resume := ""
			if r.Status == "usage-limited" {
				resume = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(fmt.Sprintf(" (Wait %s)", r.ResumeIn))
			}

			lastLine := r.LastLine
			if len(lastLine) > 40 {
				lastLine = lastLine[:37] + "..."
			}
			lastLineRender := lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("242")).Render(lastLine)

			cardContent := lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.JoinHorizontal(lipgloss.Top, workerTitle, " ", cliInfo),
				"Status: "+status+resume,
				"",
				"Last activity:",
				lastLineRender,
			)
			cards = append(cards, cardStyle.Render(cardContent))
		}

		// Arrange cards in a grid (2 columns)
		var rows []string
		for i := 0; i < len(cards); i += 2 {
			if i+1 < len(cards) {
				rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards[i], cards[i+1]))
			} else {
				rows = append(rows, cards[i])
			}
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	help := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("  ↑/↓: scroll table | q: quit | t: toggle layout | refresh: " + m.refresh.String())

	footer := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).MarginTop(1).Render(
			fmt.Sprintf("  Last updated: %s", time.Now().Format("15:04:05")),
		),
		lipgloss.NewStyle().MarginTop(1).MarginLeft(4).Render(help),
	)

	if m.lastErr != "" {
		footer += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(" | Error: " + m.lastErr)
	}

	return lipgloss.NewStyle().Margin(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

func (m usageModel) refreshCmd() tea.Cmd {
	return tea.Tick(m.refresh, func(time.Time) tea.Msg { return refreshMsg{} })
}

func toTableRows(rows []usageRow) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		worker := "?"
		if r.Worker > 0 {
			worker = strconv.Itoa(r.Worker)
		}
		status := r.Status
		if r.StatusDetail != "" {
			status = r.StatusDetail
		}
		out = append(out, table.Row{worker, r.PaneID, r.CLI, status, r.ResumeIn, r.CheckedAt})
	}
	return out
}

func collectUsageRows(session string) ([]usageRow, error) {
	panes, err := tmux.ListPanes(fmt.Sprintf("%s:swarm", session))
	if err != nil {
		return nil, err
	}

	rows := make([]usageRow, 0, len(panes))
	now := time.Now().Format("15:04:05")
	for _, pane := range panes {
		content, err := tmux.CapturePane(pane.ID)
		if err != nil {
			continue
		}
		rows = append(rows, rowFromPane(pane, content, now))
	}

	sortUsageRows(rows)

	return rows, nil
}

func rowFromPane(pane tmux.PaneInfo, content, checkedAt string) usageRow {
	status := "active"
	resumeIn := "-"
	if usagelimit.HasError(content) {
		wait := usagelimit.ExtractWaitSecs(content)
		status = "usage-limited"
		resumeIn = formatWait(wait)
	}

	detail := monitor.ExtractStatus(content)
	lastLine := ""
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			lastLine = l
			break
		}
	}

	return usageRow{
		Worker:       workerFromPaneTitle(pane.Title),
		PaneID:       pane.ID,
		CLI:          deriveCLI(pane),
		Status:       status,
		StatusDetail: detail,
		LastLine:     lastLine,
		ResumeIn:     resumeIn,
		CheckedAt:    checkedAt,
		SortKey:      pane.Index,
		HasSortKey:   true,
	}
}

func sortUsageRows(rows []usageRow) {
	sort.Slice(rows, func(i, j int) bool {
		ri, rj := rows[i], rows[j]
		if ri.Worker > 0 && rj.Worker > 0 && ri.Worker != rj.Worker {
			return ri.Worker < rj.Worker
		}
		if ri.HasSortKey && rj.HasSortKey && ri.SortKey != rj.SortKey {
			return ri.SortKey < rj.SortKey
		}
		return ri.PaneID < rj.PaneID
	})
}

func deriveCLI(p tmux.PaneInfo) string {
	if cli := cliFromPaneTitle(p.Title); cli != "" {
		return cli
	}
	if p.CurrentCommand != "" {
		switch p.CurrentCommand {
		case "claude", "codex", "gemini":
			return p.CurrentCommand
		}
	}
	return "unknown"
}

func cliFromPaneTitle(title string) string {
	// Expected: worker-1 (codex)
	open := strings.LastIndex(title, "(")
	close := strings.LastIndex(title, ")")
	if open == -1 || close == -1 || close <= open+1 {
		return ""
	}
	return strings.TrimSpace(title[open+1 : close])
}

func workerFromPaneTitle(title string) int {
	title = strings.ToLower(strings.TrimSpace(title))
	if !strings.HasPrefix(title, "worker-") {
		return 0
	}
	n := strings.TrimPrefix(title, "worker-")
	for i, r := range n {
		if r < '0' || r > '9' {
			n = n[:i]
			break
		}
	}
	v, _ := strconv.Atoi(n)
	if v < 0 {
		return 0
	}
	return v
}

func formatWait(secs int) string {
	if secs <= 0 {
		return "-"
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func usageWindowCommand(session string, refreshSecs int) string {
	return fmt.Sprintf("claude-swarm usage --session %s --refresh-secs %d", shellQuote(session), refreshSecs)
}

func summarizeRows(rows []usageRow) (active, limited int) {
	for _, r := range rows {
		if r.Status == "usage-limited" {
			limited++
			continue
		}
		active++
	}
	return active, limited
}

func topCLI(rows []usageRow) (string, int) {
	if len(rows) == 0 {
		return "-", 0
	}
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		cli := r.CLI
		if cli == "" {
			cli = "unknown"
		}
		counts[cli]++
	}
	bestCLI := "-"
	bestCount := 0
	for cli, n := range counts {
		if n > bestCount || (n == bestCount && cli < bestCLI) {
			bestCLI = cli
			bestCount = n
		}
	}
	return bestCLI, bestCount
}

func renderSparkline(values []int) string {
	if len(values) == 0 {
		return "-"
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	maxV := 0
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		return strings.Repeat("▁", len(values))
	}
	var b strings.Builder
	b.Grow(len(values))
	for _, v := range values {
		if v < 0 {
			v = 0
		}
		idx := v * (len(levels) - 1) / maxV
		b.WriteRune(levels[idx])
	}
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
