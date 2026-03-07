package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Status values for a ticket.
type Status string

const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in-progress"
	StatusDone       Status = "done"
	StatusBlocked    Status = "blocked"
)

// Ticket represents a work item.
type Ticket struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Status     Status `yaml:"status"`
	Priority   int    `yaml:"priority"`
	CreatedBy  string `yaml:"created_by"`
	AssignedTo string `yaml:"assigned_to,omitempty"`
	Body       string `yaml:"-"`
}

// Store is a file-based ticket store backed by a directory of markdown files.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the store's ticket directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) ensureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

// slug converts a title to a filename-safe lowercase slug.
func slug(title string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return strings.Trim(re.ReplaceAllString(strings.ToLower(title), "-"), "-")
}

// nextID returns the next zero-padded 4-digit ID.
func (s *Store) nextID() (string, error) {
	tickets, err := s.List()
	if err != nil {
		return "", err
	}
	max := 0
	for _, t := range tickets {
		var n int
		if _, err := fmt.Sscanf(t.ID, "%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%04d", max+1), nil
}

// fileName returns the expected file path for a given id+title pair.
func (s *Store) fileName(id, title string) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s-%s.md", id, slug(title)))
}

// write serializes a ticket to disk.
func (s *Store) write(t *Ticket) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	frontmatter, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("---\n%s---\n%s", string(frontmatter), t.Body)
	return os.WriteFile(s.fileName(t.ID, t.Title), []byte(content), 0o644)
}

// parseFile reads and parses a ticket markdown file with YAML frontmatter.
func parseFile(path string) (*Ticket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Expect: "---\n<yaml>\n---\n<body>"
	parts := strings.SplitN(string(data), "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid ticket file %s: missing frontmatter delimiters", path)
	}
	var t Ticket
	if err := yaml.Unmarshal([]byte(parts[1]), &t); err != nil {
		return nil, fmt.Errorf("parsing frontmatter in %s: %w", path, err)
	}
	t.Body = strings.TrimLeft(parts[2], "\n")
	return &t, nil
}

// filePath locates the file path for a ticket ID by scanning the directory.
func (s *Store) filePath(id string) (string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id+"-") && strings.HasSuffix(e.Name(), ".md") {
			return filepath.Join(s.dir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no file found for ticket %s", id)
}

// List returns all tickets sorted by priority (ascending) then ID.
func (s *Store) List() ([]*Ticket, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tickets []*Ticket
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, parseErr := parseFile(filepath.Join(s.dir, e.Name()))
		if parseErr != nil {
			continue
		}
		tickets = append(tickets, t)
	}
	sort.Slice(tickets, func(i, j int) bool {
		if tickets[i].Priority != tickets[j].Priority {
			return tickets[i].Priority < tickets[j].Priority
		}
		return tickets[i].ID < tickets[j].ID
	})
	return tickets, nil
}

// Get returns a ticket by ID, or an error if not found.
func (s *Store) Get(id string) (*Ticket, error) {
	tickets, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, t := range tickets {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("ticket %s not found", id)
}

// updateTicket applies fn to a ticket and writes it back to disk.
func (s *Store) updateTicket(id string, fn func(*Ticket)) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	oldPath, err := s.filePath(id)
	if err != nil {
		return err
	}
	fn(t)
	newPath := s.fileName(t.ID, t.Title)
	if oldPath != newPath {
		_ = os.Remove(oldPath)
	}
	return s.write(t)
}

// Add creates a new ticket and persists it.
func (s *Store) Add(title, desc, createdBy string) (*Ticket, error) {
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	t := &Ticket{
		ID:        id,
		Title:     title,
		Status:    StatusTodo,
		Priority:  10,
		CreatedBy: createdBy,
		Body:      fmt.Sprintf("## Description\n%s\n\n## Acceptance Criteria\n- [ ] \n", desc),
	}
	return t, s.write(t)
}

// MarkDone marks a ticket as done.
func (s *Store) MarkDone(id string) error {
	return s.updateTicket(id, func(t *Ticket) {
		t.Status = StatusDone
		t.AssignedTo = ""
	})
}

// Assign marks a ticket as in-progress and assigned to a worker.
func (s *Store) Assign(id, worker string) error {
	return s.updateTicket(id, func(t *Ticket) {
		t.AssignedTo = worker
		t.Status = StatusInProgress
	})
}

// NextTodo returns the highest-priority todo ticket, or nil if the backlog is empty.
func (s *Store) NextTodo() (*Ticket, error) {
	tickets, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, t := range tickets {
		if t.Status == StatusTodo {
			return t, nil
		}
	}
	return nil, nil
}

// WriteCurrentTicket writes a CURRENT_TICKET.md to worktreeDir with ticket content
// and footer instructions for the agent.
func WriteCurrentTicket(worktreeDir string, t *Ticket) error {
	frontmatter, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	footer := fmt.Sprintf(
		"\n---\nWhen done: run `claude-swarm ticket done %s`\n"+
			"To create a sub-task: run `claude-swarm ticket add --title \"...\" --desc \"...\"`\n",
		t.ID,
	)
	content := fmt.Sprintf("---\n%s---\n%s%s", string(frontmatter), t.Body, footer)
	return os.WriteFile(filepath.Join(worktreeDir, "CURRENT_TICKET.md"), []byte(content), 0o644)
}
