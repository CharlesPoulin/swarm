package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/ghcli"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
)

type reviewClient interface {
	ListOpenPullRequests(ctx context.Context, repo string) ([]ghcli.PullRequest, error)
	GetPullRequestDetail(ctx context.Context, repo string, number int) (ghcli.PullRequestDetail, error)
	ApprovePullRequest(ctx context.Context, repo string, number int) error
	MergePullRequestSquash(ctx context.Context, repo string, number int) error
}

type reviewRow struct {
	Number      int
	Title       string
	Author      string
	UpdatedAt   string
	BaseRefName string
	HeadRefName string
	URL         string
	MergeState  string
	CheckStatus ghcli.CheckStatus
	IsDraft     bool
}

type reviewRefreshMsg struct {
	rows []reviewRow
	err  error
}

type reviewDetailMsg struct {
	number int
	detail ghcli.PullRequestDetail
	err    error
}

type reviewActionMsg struct {
	number int
	text   string
	err    error
}

type reviewTickMsg struct{}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review open PRs with CI status, description, diff, and merge actions",
	RunE:  runReview,
}

func init() {
	f := reviewCmd.Flags()
	f.String("session", "", "tmux session name (defaults to config session)")
	f.Int("refresh-secs", 0, "refresh interval in seconds (defaults to review_refresh_secs)")
	f.String("repo", "", "GitHub repo in owner/name format (defaults to current repository)")
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found — install it from https://cli.github.com and run `gh auth login`")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	session, _ := cmd.Flags().GetString("session")
	if strings.TrimSpace(session) == "" {
		session = cfg.Session
	}
	// Validate only when user explicitly targets a session.
	if cmd.Flags().Changed("session") && session != "" && tmux.HasSession(session) == false {
		return fmt.Errorf("session %q not found", session)
	}

	refreshSecs, _ := cmd.Flags().GetInt("refresh-secs")
	if refreshSecs <= 0 {
		refreshSecs = cfg.ReviewRefreshSecs
	}
	if refreshSecs <= 0 {
		refreshSecs = 30
	}

	repo, _ := cmd.Flags().GetString("repo")
	m := newReviewModel(session, strings.TrimSpace(repo), time.Duration(refreshSecs)*time.Second, ghcli.NewClient())
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

type reviewModel struct {
	session string
	repo    string
	refresh time.Duration
	client  reviewClient

	table table.Model
	rows  []reviewRow

	detailMap map[int]ghcli.PullRequestDetail
	detailErr map[int]string
	tab       int
	scroll    int

	confirmOverrideNumber int
	actionInFlight        bool

	lastErr   string
	statusMsg string

	width  int
	height int
}

func newReviewModel(session, repo string, refresh time.Duration, client reviewClient) reviewModel {
	cols := []table.Column{
		{Title: "PR", Width: 6},
		{Title: "Checks", Width: 10},
		{Title: "Merge", Width: 12},
		{Title: "Author", Width: 14},
		{Title: "Title", Width: 46},
		{Title: "Updated", Width: 17},
	}
	t := table.New(table.WithColumns(cols), table.WithRows(nil), table.WithFocused(true), table.WithHeight(12))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("33")).Bold(true)
	t.SetStyles(styles)

	return reviewModel{
		session:   session,
		repo:      repo,
		refresh:   refresh,
		client:    client,
		table:     t,
		detailMap: make(map[int]ghcli.PullRequestDetail),
		detailErr: make(map[int]string),
	}
}

func (m reviewModel) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), m.loadSelectedDetailCmd())
}

func (m reviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Height > 12 {
			m.table.SetHeight(msg.Height - 12)
		}
	case tea.KeyMsg:
		if m.confirmOverrideNumber > 0 {
			switch msg.String() {
			case "y", "Y":
				prNumber := m.confirmOverrideNumber
				m.confirmOverrideNumber = 0
				m.actionInFlight = true
				return m, m.acceptCmd(prNumber)
			default:
				m.confirmOverrideNumber = 0
				m.statusMsg = "Merge cancelled."
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			return m, m.refreshCmd()
		case "1":
			m.tab = 0
			m.scroll = 0
			return m, nil
		case "2":
			m.tab = 1
			m.scroll = 0
			return m, nil
		case "3":
			m.tab = 2
			m.scroll = 0
			return m, nil
		case "J":
			m.scroll++
			return m, nil
		case "K":
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		case "a":
			if m.actionInFlight {
				return m, nil
			}
			row, ok := m.selectedRow()
			if !ok {
				return m, nil
			}
			if row.CheckStatus != ghcli.CheckStatusPass {
				m.confirmOverrideNumber = row.Number
				m.statusMsg = "Checks are not green. Press y to approve+merge anyway, any other key to cancel."
				return m, nil
			}
			m.actionInFlight = true
			return m, m.acceptCmd(row.Number)
		}
	}

	switch msg := msg.(type) {
	case reviewRefreshMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
			selectedBefore, _ := m.selectedRow()
			m.rows = msg.rows
			m.table.SetRows(toReviewTableRows(m.rows))
			if selectedBefore.Number > 0 {
				for i, r := range m.rows {
					if r.Number == selectedBefore.Number {
						m.table.SetCursor(i)
						break
					}
				}
			}
		}
		return m, tea.Batch(tea.Tick(m.refresh, func(time.Time) tea.Msg {
			return reviewTickMsg{}
		}), m.loadSelectedDetailCmd())
	case reviewDetailMsg:
		if msg.err != nil {
			m.detailErr[msg.number] = msg.err.Error()
		} else {
			delete(m.detailErr, msg.number)
			m.detailMap[msg.number] = msg.detail
		}
		return m, nil
	case reviewActionMsg:
		m.actionInFlight = false
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("PR #%d: %v", msg.number, msg.err)
			return m, nil
		}
		m.statusMsg = msg.text
		return m, tea.Batch(m.refreshCmd(), m.loadSelectedDetailCmd())
	case reviewTickMsg:
		return m, m.refreshCmd()
	}

	prev := m.table.Cursor()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if m.table.Cursor() != prev {
		m.scroll = 0
		return m, tea.Batch(cmd, m.loadSelectedDetailCmd())
	}
	return m, cmd
}

func (m reviewModel) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("Pull Request Review")
	sub := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
		fmt.Sprintf("session=%s | repo=%s | refresh=%s | q:quit r:refresh a:accept 1/2/3:tabs J/K:scroll", valueOrDash(m.session), valueOrDash(m.repo), m.refresh),
	)

	left := m.table.View()
	right := m.detailView()

	layout := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(maxInt(45, m.width/2)).Render(left),
		lipgloss.NewStyle().PaddingLeft(1).Width(maxInt(45, m.width/2-1)).Render(right),
	)

	status := fmt.Sprintf("open=%d", len(m.rows))
	if m.statusMsg != "" {
		status = m.statusMsg
	}
	if m.lastErr != "" {
		status = "error: " + m.lastErr
	}
	if m.actionInFlight {
		status = "running approve+merge..."
	}

	statusLine := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(status)
	if m.lastErr != "" {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(status)
	}
	if m.confirmOverrideNumber > 0 {
		statusLine = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(m.statusMsg)
	}

	return title + "\n" + sub + "\n\n" + layout + "\n\n" + statusLine
}

func (m reviewModel) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		prs, err := m.client.ListOpenPullRequests(ctx, m.repo)
		if err != nil {
			return reviewRefreshMsg{err: err}
		}
		rows := make([]reviewRow, 0, len(prs))
		for _, pr := range prs {
			rows = append(rows, reviewRow{
				Number:      pr.Number,
				Title:       pr.Title,
				Author:      pr.Author,
				UpdatedAt:   formatReviewUpdated(pr.UpdatedAt),
				BaseRefName: pr.BaseRefName,
				HeadRefName: pr.HeadRefName,
				URL:         pr.URL,
				MergeState:  pr.MergeState,
				CheckStatus: pr.CheckStatus,
				IsDraft:     pr.IsDraft,
			})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Number > rows[j].Number
		})
		return reviewRefreshMsg{rows: rows}
	}
}

func (m reviewModel) loadSelectedDetailCmd() tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	if _, ok := m.detailMap[row.Number]; ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		detail, err := m.client.GetPullRequestDetail(ctx, m.repo, row.Number)
		return reviewDetailMsg{number: row.Number, detail: detail, err: err}
	}
}

func (m reviewModel) acceptCmd(number int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := m.client.ApprovePullRequest(ctx, m.repo, number); err != nil {
			return reviewActionMsg{number: number, err: fmt.Errorf("approve failed: %w", err)}
		}
		if err := m.client.MergePullRequestSquash(ctx, m.repo, number); err != nil {
			return reviewActionMsg{number: number, err: fmt.Errorf("merge failed: %w", err)}
		}
		return reviewActionMsg{number: number, text: fmt.Sprintf("PR #%d approved and merged with squash.", number)}
	}
}

func (m reviewModel) selectedRow() (reviewRow, bool) {
	if len(m.rows) == 0 {
		return reviewRow{}, false
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rows) {
		return reviewRow{}, false
	}
	return m.rows[idx], true
}

func (m reviewModel) detailView() string {
	row, ok := m.selectedRow()
	if !ok {
		return "No open pull requests."
	}

	head := fmt.Sprintf("PR #%d  %s", row.Number, row.Title)
	meta := fmt.Sprintf("author=%s  %s -> %s  checks=%s  merge=%s", row.Author, row.HeadRefName, row.BaseRefName, row.CheckStatus, valueOrDash(row.MergeState))
	tabs := "[1] Description  [2] Checks  [3] Diff"

	if err, ok := m.detailErr[row.Number]; ok {
		return head + "\n" + meta + "\n" + tabs + "\n\nerror: " + err
	}

	detail, ok := m.detailMap[row.Number]
	if !ok {
		return head + "\n" + meta + "\n" + tabs + "\n\nLoading..."
	}

	var body string
	switch m.tab {
	case 0:
		body = strings.TrimSpace(detail.Body)
		if body == "" {
			body = "(No description)"
		}
	case 1:
		body = renderChecks(detail.Checks)
	case 2:
		body = strings.TrimSpace(detail.Diff)
		if body == "" {
			body = "(No diff)"
		}
	}

	lines := strings.Split(body, "\n")
	viewHeight := 10
	if m.height > 14 {
		viewHeight = m.height - 14
	}
	scroll := m.scroll
	if scroll > len(lines)-1 {
		scroll = maxInt(0, len(lines)-1)
	}
	start := scroll
	end := minInt(len(lines), start+viewHeight)
	visible := strings.Join(lines[start:end], "\n")
	footer := fmt.Sprintf("[scroll %d/%d] %s", end, len(lines), strings.TrimSpace(row.URL))

	return head + "\n" + meta + "\n" + tabs + "\n\n" + visible + "\n\n" + footer
}

func toReviewTableRows(rows []reviewRow) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		title := r.Title
		if r.IsDraft {
			title = "[DRAFT] " + title
		}
		out = append(out, table.Row{
			"#" + strconv.Itoa(r.Number),
			string(r.CheckStatus),
			valueOrDash(r.MergeState),
			r.Author,
			title,
			r.UpdatedAt,
		})
	}
	return out
}

func renderChecks(checks []ghcli.Check) string {
	if len(checks) == 0 {
		return "(No check runs)"
	}
	lines := make([]string, 0, len(checks))
	for _, c := range checks {
		line := fmt.Sprintf("[%s] %s", valueOrDash(c.State), c.Name)
		if c.Bucket != "" {
			line += " (" + c.Bucket + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func reviewWindowCommand(session string, refreshSecs int) string {
	return fmt.Sprintf("claude-swarm review --session %s --refresh-secs %d", shellQuote(session), refreshSecs)
}

func valueOrDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}

func formatReviewUpdated(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
