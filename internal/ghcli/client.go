package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type CheckStatus string

const (
	CheckStatusPass    CheckStatus = "pass"
	CheckStatusPending CheckStatus = "pending"
	CheckStatusFail    CheckStatus = "fail"
	CheckStatusNone    CheckStatus = "none"
)

type Check struct {
	Name   string
	State  string
	Link   string
	Bucket string
}

type PullRequest struct {
	Number      int
	Title       string
	Author      string
	BaseRefName string
	HeadRefName string
	URL         string
	UpdatedAt   string
	IsDraft     bool
	MergeState  string
	CheckStatus CheckStatus
}

type PullRequestDetail struct {
	Number      int
	Title       string
	Body        string
	Author      string
	BaseRefName string
	HeadRefName string
	URL         string
	MergeState  string
	CheckStatus CheckStatus
	Checks      []Check
	Diff        string
}

type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

type Client struct {
	runner Runner
}

func NewClient() *Client {
	return &Client{runner: execRunner{}}
}

func NewClientWithRunner(r Runner) *Client {
	return &Client{runner: r}
}

func (c *Client) ListOpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error) {
	args := []string{"pr", "list", "--state", "open", "--limit", "50", "--json", "number,title,author,isDraft,updatedAt,headRefName,baseRefName,url,mergeStateStatus,statusCheckRollup"}
	args = appendRepoArg(args, repo)
	out, err := c.runner.Run(ctx, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no checks") {
			return nil, nil
		}
		return nil, err
	}

	type authorJSON struct {
		Login string `json:"login"`
	}
	type prJSON struct {
		Number          int               `json:"number"`
		Title           string            `json:"title"`
		Author          authorJSON        `json:"author"`
		IsDraft         bool              `json:"isDraft"`
		UpdatedAt       string            `json:"updatedAt"`
		HeadRefName     string            `json:"headRefName"`
		BaseRefName     string            `json:"baseRefName"`
		URL             string            `json:"url"`
		MergeState      string            `json:"mergeStateStatus"`
		StatusCheckList []json.RawMessage `json:"statusCheckRollup"`
	}
	var payload []prJSON
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parsing gh pr list output: %w", err)
	}

	prs := make([]PullRequest, 0, len(payload))
	for _, row := range payload {
		prs = append(prs, PullRequest{
			Number:      row.Number,
			Title:       row.Title,
			Author:      row.Author.Login,
			BaseRefName: row.BaseRefName,
			HeadRefName: row.HeadRefName,
			URL:         row.URL,
			UpdatedAt:   row.UpdatedAt,
			IsDraft:     row.IsDraft,
			MergeState:  strings.ToUpper(strings.TrimSpace(row.MergeState)),
			CheckStatus: summarizeRollupStatus(row.StatusCheckList),
		})
	}
	return prs, nil
}

func (c *Client) GetPullRequestDetail(ctx context.Context, repo string, number int) (PullRequestDetail, error) {
	args := []string{"pr", "view", fmt.Sprintf("%d", number), "--json", "number,title,body,author,headRefName,baseRefName,url,mergeStateStatus,statusCheckRollup"}
	args = appendRepoArg(args, repo)
	out, err := c.runner.Run(ctx, args...)
	if err != nil {
		return PullRequestDetail{}, err
	}

	type authorJSON struct {
		Login string `json:"login"`
	}
	type prJSON struct {
		Number          int               `json:"number"`
		Title           string            `json:"title"`
		Body            string            `json:"body"`
		Author          authorJSON        `json:"author"`
		HeadRefName     string            `json:"headRefName"`
		BaseRefName     string            `json:"baseRefName"`
		URL             string            `json:"url"`
		MergeState      string            `json:"mergeStateStatus"`
		StatusCheckList []json.RawMessage `json:"statusCheckRollup"`
	}
	var pr prJSON
	if err := json.Unmarshal(out, &pr); err != nil {
		return PullRequestDetail{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}

	checks, err := c.pullRequestChecks(ctx, repo, number)
	if err != nil {
		return PullRequestDetail{}, err
	}

	diff, err := c.pullRequestDiff(ctx, repo, number)
	if err != nil {
		return PullRequestDetail{}, err
	}

	return PullRequestDetail{
		Number:      pr.Number,
		Title:       pr.Title,
		Body:        pr.Body,
		Author:      pr.Author.Login,
		BaseRefName: pr.BaseRefName,
		HeadRefName: pr.HeadRefName,
		URL:         pr.URL,
		MergeState:  strings.ToUpper(strings.TrimSpace(pr.MergeState)),
		CheckStatus: summarizeRollupStatus(pr.StatusCheckList),
		Checks:      checks,
		Diff:        diff,
	}, nil
}

func (c *Client) ApprovePullRequest(ctx context.Context, repo string, number int) error {
	args := []string{"pr", "review", fmt.Sprintf("%d", number), "--approve"}
	args = appendRepoArg(args, repo)
	_, err := c.runner.Run(ctx, args...)
	return err
}

func (c *Client) MergePullRequestSquash(ctx context.Context, repo string, number int) error {
	args := []string{"pr", "merge", fmt.Sprintf("%d", number), "--squash", "--delete-branch"}
	args = appendRepoArg(args, repo)
	_, err := c.runner.Run(ctx, args...)
	return err
}

func (c *Client) pullRequestChecks(ctx context.Context, repo string, number int) ([]Check, error) {
	args := []string{"pr", "checks", fmt.Sprintf("%d", number), "--json", "name,state,link,bucket"}
	args = appendRepoArg(args, repo)
	out, err := c.runner.Run(ctx, args...)
	if err != nil {
		return nil, err
	}

	type checkJSON struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Link   string `json:"link"`
		Bucket string `json:"bucket"`
	}
	var payload []checkJSON
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parsing gh pr checks output: %w", err)
	}
	checks := make([]Check, 0, len(payload))
	for _, c := range payload {
		checks = append(checks, Check{
			Name:   c.Name,
			State:  strings.ToUpper(strings.TrimSpace(c.State)),
			Link:   strings.TrimSpace(c.Link),
			Bucket: strings.ToUpper(strings.TrimSpace(c.Bucket)),
		})
	}
	return checks, nil
}

func (c *Client) pullRequestDiff(ctx context.Context, repo string, number int) (string, error) {
	args := []string{"pr", "diff", fmt.Sprintf("%d", number), "--color=never"}
	args = appendRepoArg(args, repo)
	out, err := c.runner.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func appendRepoArg(args []string, repo string) []string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return args
	}
	return append(args, "--repo", repo)
}

func summarizeRollupStatus(items []json.RawMessage) CheckStatus {
	if len(items) == 0 {
		return CheckStatusNone
	}
	anyPending := false
	anyPass := false

	for _, item := range items {
		var v map[string]any
		if err := json.Unmarshal(item, &v); err != nil {
			continue
		}
		state := normalizedState(v)
		if isFailureState(state) {
			return CheckStatusFail
		}
		if isPendingState(state) {
			anyPending = true
			continue
		}
		if isPassingState(state) {
			anyPass = true
		}
	}

	if anyPending {
		return CheckStatusPending
	}
	if anyPass {
		return CheckStatusPass
	}
	return CheckStatusNone
}

func normalizedState(v map[string]any) string {
	candidates := []string{
		extractString(v, "conclusion"),
		extractString(v, "state"),
		extractString(v, "status"),
	}
	for _, raw := range candidates {
		raw = strings.ToUpper(strings.TrimSpace(raw))
		if raw != "" {
			return raw
		}
	}
	return ""
}

func extractString(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return s
}

func isFailureState(s string) bool {
	switch s {
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE", "ERROR":
		return true
	default:
		return false
	}
}

func isPendingState(s string) bool {
	switch s {
	case "PENDING", "IN_PROGRESS", "QUEUED", "REQUESTED", "WAITING", "EXPECTED":
		return true
	default:
		return false
	}
}

func isPassingState(s string) bool {
	switch s {
	case "SUCCESS", "PASS", "PASSED", "NEUTRAL", "SKIPPED":
		return true
	default:
		return false
	}
}
