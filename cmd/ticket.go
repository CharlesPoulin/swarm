package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/cpoulin/claude-swarm/internal/config"
	"github.com/cpoulin/claude-swarm/internal/git"
	"github.com/cpoulin/claude-swarm/internal/ticket"
	"github.com/cpoulin/claude-swarm/internal/tmux"
	"github.com/spf13/cobra"
)

var ticketCmd = &cobra.Command{
	Use:   "ticket",
	Short: "Manage the swarm ticket backlog",
}

var ticketAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new ticket to the backlog",
	RunE: func(cmd *cobra.Command, args []string) error {
		title, _ := cmd.Flags().GetString("title")
		desc, _ := cmd.Flags().GetString("desc")

		reader := bufio.NewReader(os.Stdin)
		if title == "" {
			fmt.Print("Title: ")
			t, _ := reader.ReadString('\n')
			title = strings.TrimSpace(t)
		}
		if title == "" {
			return fmt.Errorf("title is required")
		}
		if desc == "" {
			fmt.Print("Description (press Enter twice to finish):\n")
			var lines []string
			for {
				line, _ := reader.ReadString('\n')
				line = strings.TrimRight(line, "\n\r")
				if line == "" && len(lines) > 0 {
					break
				}
				lines = append(lines, line)
			}
			desc = strings.Join(lines, "\n")
		}

		store, err := ticketStore()
		if err != nil {
			return err
		}
		t, err := store.Add(title, desc, "human")
		if err != nil {
			return err
		}
		if err := refreshPMArtifacts(); err != nil {
			return err
		}
		fmt.Printf("Created ticket %s: %s\n", t.ID, t.Title)
		return nil
	},
}

var ticketListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tickets",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := ticketStore()
		if err != nil {
			return err
		}
		tickets, err := store.List()
		if err != nil {
			return err
		}
		if len(tickets) == 0 {
			fmt.Println("No tickets found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "ID\tSTATUS\tPRI\tASSIGNED\tTITLE"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "----\t-----------\t---\t--------\t-----"); err != nil {
			return err
		}
		for _, t := range tickets {
			assigned := t.AssignedTo
			if assigned == "" {
				assigned = "-"
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", t.ID, t.Status, t.Priority, assigned, t.Title); err != nil {
				return err
			}
		}
		return w.Flush()
	},
}

var ticketDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a ticket as done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := ticketStore()
		if err != nil {
			return err
		}
		id := args[0]
		if err := store.MarkDone(id); err != nil {
			return err
		}
		if err := refreshPMArtifacts(); err != nil {
			return err
		}
		fmt.Printf("Ticket %s marked as done.\n", id)
		return nil
	},
}

var ticketAssignCmd = &cobra.Command{
	Use:   "assign <id> <worker-N>",
	Short: "Manually assign a ticket to a worker pane",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		workerName := args[1] // e.g. "worker-2"

		store, err := ticketStore()
		if err != nil {
			return err
		}
		t, err := store.Get(id)
		if err != nil {
			return err
		}

		repoRoot, err := git.RepoRoot()
		if err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Determine the worktree dir for this worker (worker-N → .wt-N).
		var workerNum int
		if _, scanErr := fmt.Sscanf(workerName, "worker-%d", &workerNum); scanErr != nil || workerNum < 1 {
			return fmt.Errorf("invalid worker name %q — expected worker-N", workerName)
		}
		worktreeDir := wtDir(repoRoot, cfg.WorktreePrefix, workerNum)
		if _, statErr := os.Stat(worktreeDir); statErr != nil {
			return fmt.Errorf("worktree %s not found — is a swarm running?", worktreeDir)
		}
		if err := store.Assign(id, workerName); err != nil {
			return err
		}
		t, err = store.Get(id)
		if err != nil {
			return err
		}
		if err := ticket.WriteCurrentTicket(worktreeDir, t); err != nil {
			return fmt.Errorf("writing ticket to worktree: %w", err)
		}
		if err := refreshPMArtifacts(); err != nil {
			return err
		}

		// If a tmux session is running, find the pane and send the command.
		if tmux.HasSession(cfg.Session) {
			panes, listErr := tmux.ListPanes(cfg.Session + ":swarm")
			if listErr == nil {
				for _, p := range panes {
					if strings.HasPrefix(p.Title, workerName+" ") || p.Title == workerName {
						cliType := extractCLIFromPaneTitle(p.Title)
						workerType := cliType
						ticketPath := filepath.Join(worktreeDir, "CURRENT_TICKET.md")
						_ = tmux.SendKeys(p.ID, workerType+fmt.Sprintf(` --message "$(cat '%s')"`, ticketPath))
						fmt.Printf("Sent ticket %s to %s (pane %s).\n", id, workerName, p.ID)
						return nil
					}
				}
			}
		}

		fmt.Printf("Ticket %s assigned to %s (worktree updated; no live pane found).\n", id, workerName)
		return nil
	},
}

var ticketRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Regenerate PM Kanban and Focus from ticket markdown files",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := refreshPMArtifacts(); err != nil {
			return err
		}
		fmt.Println("PM artifacts refreshed from .swarm/tickets.")
		return nil
	},
}

var ticketNextCmd = &cobra.Command{
	Use:   "next",
	Short: "Print the next todo ticket",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := ticketStore()
		if err != nil {
			return err
		}
		t, err := store.NextTodo()
		if err != nil {
			return err
		}
		if t == nil {
			fmt.Println("No todo tickets.")
			return nil
		}
		fmt.Printf("[%s] (priority %d) %s\n", t.ID, t.Priority, t.Title)
		return nil
	},
}

func init() {
	ticketAddCmd.Flags().String("title", "", "Ticket title")
	ticketAddCmd.Flags().String("desc", "", "Ticket description")

	ticketCmd.AddCommand(ticketAddCmd, ticketListCmd, ticketDoneCmd, ticketAssignCmd, ticketRefreshCmd, ticketNextCmd)
	rootCmd.AddCommand(ticketCmd)
}

// ticketStore opens the ticket store at the configured (or default) tickets dir.
func ticketStore() (*ticket.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	dir := cfg.TicketsDir
	if !filepath.IsAbs(dir) {
		repoRoot, err := git.RepoRoot()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(repoRoot, dir)
	}
	return ticket.NewStore(dir), nil
}

// extractCLIFromPaneTitle parses "worker-2 (claude)" → "claude".
// Falls back to "claude" if parsing fails.
func extractCLIFromPaneTitle(title string) string {
	start := strings.Index(title, "(")
	end := strings.Index(title, ")")
	if start != -1 && end != -1 && end > start {
		return title[start+1 : end]
	}
	return "claude"
}
