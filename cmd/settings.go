package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

type settingSpec struct {
	Key       string
	Label     string
	Help      string
	Normalize func(string) string
	Validate  func(string) error
}

type settingField struct {
	Spec  settingSpec
	Value string
}

type settingsModel struct {
	configPath string
	fields     []settingField
	cursor     int
	editing    bool
	input      textinput.Model
	statusMsg  string
	errMsg     string
	width      int
}

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Open an interactive editor for common swarm settings",
	RunE:  runSettings,
}

func init() {
	rootCmd.AddCommand(settingsCmd)
}

func runSettings(cmd *cobra.Command, args []string) error {
	configPath, err := canonicalConfigPath()
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	fields, err := loadSettingsFields(configPath)
	if err != nil {
		return err
	}
	m := newSettingsModel(configPath, fields)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newSettingsModel(configPath string, fields []settingField) settingsModel {
	in := textinput.New()
	in.CharLimit = 240
	return settingsModel{
		configPath: configPath,
		fields:     fields,
		input:      in,
		width:      90,
	}
}

func (m settingsModel) Init() tea.Cmd {
	return nil
}

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width > 110 {
			m.width = 110
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	if m.editing {
		return m.updateEditing(msg)
	}
	return m.updateBrowsing(msg)
}

func (m settingsModel) updateBrowsing(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.fields)-1 {
				m.cursor++
			}
		case "enter", "e":
			if len(m.fields) == 0 {
				return m, nil
			}
			f := m.fields[m.cursor]
			m.input.SetValue(f.Value)
			m.input.Placeholder = f.Spec.Label
			m.input.Focus()
			m.editing = true
			m.errMsg = ""
		case "s":
			if err := persistSettings(m.configPath, m.fields); err != nil {
				m.errMsg = err.Error()
				m.statusMsg = ""
				return m, nil
			}
			m.errMsg = ""
			m.statusMsg = fmt.Sprintf("Saved %d setting(s) to %s at %s.", len(m.fields), m.configPath, time.Now().Format("15:04:05"))
		case "r":
			fields, err := loadSettingsFields(m.configPath)
			if err != nil {
				m.errMsg = err.Error()
				m.statusMsg = ""
				return m, nil
			}
			m.fields = fields
			if m.cursor >= len(m.fields) {
				m.cursor = len(m.fields) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.errMsg = ""
			m.statusMsg = "Reloaded settings from disk."
		}
	}
	return m, nil
}

func (m settingsModel) updateEditing(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.input.Blur()
			m.editing = false
			m.statusMsg = "Edit canceled."
			m.errMsg = ""
			return m, nil
		case "enter":
			val := m.fields[m.cursor].Spec.Normalize(m.input.Value())
			if err := m.fields[m.cursor].Spec.Validate(val); err != nil {
				m.errMsg = fmt.Sprintf("%s: %v", m.fields[m.cursor].Spec.Label, err)
				m.statusMsg = ""
				return m, nil
			}
			m.fields[m.cursor].Value = val
			m.input.Blur()
			m.editing = false
			m.errMsg = ""
			m.statusMsg = fmt.Sprintf("Updated %s.", m.fields[m.cursor].Spec.Label)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

var (
	settingsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	settingsLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	settingsHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	settingsHelpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	settingsCursor     = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	settingsValue      = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	settingsErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	settingsOKStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("40")).Bold(true)
)

func (m settingsModel) View() string {
	innerWidth := m.width - 8
	if innerWidth < 52 {
		innerWidth = 52
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("33")).
		Padding(1, 2).
		Width(innerWidth)

	var body strings.Builder
	body.WriteString(settingsTitleStyle.Render("claude-swarm settings") + "\n")
	body.WriteString(settingsHintStyle.Render("Config file: "+m.configPath) + "\n\n")

	labelWidth := 22
	if len(m.fields) > 0 {
		for i, f := range m.fields {
			cursor := "  "
			if i == m.cursor {
				cursor = settingsCursor.Render("▶ ")
			}
			key := fmt.Sprintf("%-*s", labelWidth, f.Spec.Label)
			body.WriteString(fmt.Sprintf("%s%s %s\n", cursor, settingsLabelStyle.Render(key), settingsValue.Render(f.Value)))
		}
		body.WriteString("\n")
		body.WriteString(settingsHelpStyle.Render(m.fields[m.cursor].Spec.Help) + "\n")
	} else {
		body.WriteString(settingsHintStyle.Render("No editable fields available.") + "\n")
	}

	if m.editing && len(m.fields) > 0 {
		m.input.Width = innerWidth - 6
		body.WriteString("\n")
		body.WriteString(settingsLabelStyle.Render("Editing "+m.fields[m.cursor].Spec.Label+":") + "\n")
		body.WriteString(m.input.View() + "\n")
		body.WriteString(settingsHintStyle.Render("Enter apply  Esc cancel"))
	} else {
		body.WriteString("\n")
		body.WriteString(settingsHintStyle.Render("↑↓/jk navigate  Enter edit  s save  r reload  q quit"))
	}

	if m.errMsg != "" {
		body.WriteString("\n\n" + settingsErrStyle.Render("Error: "+m.errMsg))
	} else if m.statusMsg != "" {
		body.WriteString("\n\n" + settingsOKStyle.Render("OK: "+m.statusMsg))
	}

	return "\n" + box.Render(body.String()) + "\n"
}

func canonicalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude-swarm.yaml"), nil
}

func loadSettingsFields(configPath string) ([]settingField, error) {
	doc, err := loadConfigDocument(configPath)
	if err != nil {
		return nil, err
	}

	defaults := map[string]string{
		"num":                 "6",
		"session":             "claude-swarm",
		"cli_type":            "codex,codex,claude,gemini:gemini-3-flash-preview,gemini:gemini-3-flash-preview,spare,pm",
		"cli_flags":           "",
		"pm_bootstrap_mode":   "prompt",
		"dispatch_plan_mode":  "true",
		"resume_buffer_secs":  "120",
		"monitor_interval":    "30",
		"worktree_prefix":     ".wt",
		"hub_mode":            "review",
		"review_refresh_secs": "30",
		"assignment_mode":     "parallel",
		"tickets_dir":         ".swarm/tickets",
	}

	specs := settingsFieldSpecs()
	fields := make([]settingField, 0, len(specs))
	for _, spec := range specs {
		val := defaults[spec.Key]
		if v, ok := stringValue(doc, spec.Key); ok {
			val = v
		}
		val = spec.Normalize(val)
		fields = append(fields, settingField{Spec: spec, Value: val})
	}
	return fields, nil
}

func settingsFieldSpecs() []settingSpec {
	return []settingSpec{
		{
			Key:       "num",
			Label:     "Workers (num)",
			Help:      "How many non-PM workers to launch. Must be a positive integer.",
			Normalize: trimValue,
			Validate: func(s string) error {
				return validateIntRange(s, 1, false)
			},
		},
		{
			Key:       "session",
			Label:     "Session Name",
			Help:      "Tmux session name used by swarm commands and keybindings.",
			Normalize: trimValue,
			Validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("cannot be empty")
				}
				return nil
			},
		},
		{
			Key:       "cli_type",
			Label:     "CLI Mix",
			Help:      "Comma-separated worker CLIs (claude, gemini, codex, pm, spare). Optional :model per worker.",
			Normalize: trimValue,
			Validate:  validateCLITypeList,
		},
		{
			Key:       "cli_flags",
			Label:     "CLI Flags",
			Help:      "Extra flags appended to worker CLI invocations. Leave empty for none.",
			Normalize: trimValue,
			Validate: func(string) error {
				return nil
			},
		},
		{
			Key:       "pm_bootstrap_mode",
			Label:     "PM Bootstrap",
			Help:      "PM startup context mode: prompt, full, or none.",
			Normalize: lowerTrimValue,
			Validate: func(s string) error {
				return validateEnum(s, "prompt", "full", "none")
			},
		},
		{
			Key:       "dispatch_plan_mode",
			Label:     "Dispatch Plan Mode",
			Help:      "Dispatch starts in Plan mode by default (true/false).",
			Normalize: lowerTrimValue,
			Validate: func(s string) error {
				_, err := strconv.ParseBool(s)
				if err != nil {
					return fmt.Errorf("must be true or false")
				}
				return nil
			},
		},
		{
			Key:       "monitor_interval",
			Label:     "Monitor Interval",
			Help:      "Seconds between usage-limit checks. Must be a positive integer.",
			Normalize: trimValue,
			Validate: func(s string) error {
				return validateIntRange(s, 1, false)
			},
		},
		{
			Key:       "resume_buffer_secs",
			Label:     "Resume Buffer",
			Help:      "Extra seconds to wait after a limit expires. Must be zero or positive.",
			Normalize: trimValue,
			Validate: func(s string) error {
				return validateIntRange(s, 0, true)
			},
		},
		{
			Key:       "hub_mode",
			Label:     "Hub Mode",
			Help:      "Hub right pane mode: review or git.",
			Normalize: lowerTrimValue,
			Validate: func(s string) error {
				return validateEnum(s, "review", "git")
			},
		},
		{
			Key:       "review_refresh_secs",
			Label:     "Review Refresh",
			Help:      "Seconds between PR dashboard refreshes. Must be a positive integer.",
			Normalize: trimValue,
			Validate: func(s string) error {
				return validateIntRange(s, 1, false)
			},
		},
		{
			Key:       "assignment_mode",
			Label:     "Assignment Mode",
			Help:      "Ticket assignment mode on startup: parallel, sequential, or manual.",
			Normalize: lowerTrimValue,
			Validate: func(s string) error {
				return validateEnum(s, "parallel", "sequential", "manual")
			},
		},
		{
			Key:       "worktree_prefix",
			Label:     "Worktree Prefix",
			Help:      "Prefix for worktree folders (for example .wt).",
			Normalize: trimValue,
			Validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("cannot be empty")
				}
				return nil
			},
		},
		{
			Key:       "tickets_dir",
			Label:     "Tickets Dir",
			Help:      "Ticket store path (relative to repo or absolute path).",
			Normalize: trimValue,
			Validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("cannot be empty")
				}
				return nil
			},
		},
	}
}

func trimValue(s string) string {
	return strings.TrimSpace(s)
}

func lowerTrimValue(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validateIntRange(raw string, min int, allowEqualMin bool) error {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("must be an integer")
	}
	if allowEqualMin {
		if v < min {
			return fmt.Errorf("must be >= %d", min)
		}
		return nil
	}
	if v < min {
		return fmt.Errorf("must be >= %d", min)
	}
	return nil
}

func validateEnum(raw string, allowed ...string) error {
	val := strings.TrimSpace(strings.ToLower(raw))
	for _, a := range allowed {
		if val == a {
			return nil
		}
	}
	return fmt.Errorf("must be one of: %s", strings.Join(allowed, ", "))
}

func validateCLITypeList(raw string) error {
	types := parseCLITypes(raw)
	if len(types) == 0 {
		return fmt.Errorf("must include at least one CLI")
	}
	for _, cliType := range types {
		if !isSupportedCLIType(cliType) {
			name, _ := parseWorker(cliType)
			return fmt.Errorf("unknown CLI type %q (allowed: claude, gemini, codex, pm, spare)", name)
		}
	}
	return nil
}

func persistSettings(configPath string, fields []settingField) error {
	doc, err := loadConfigDocument(configPath)
	if err != nil {
		return err
	}
	values, err := valuesFromFields(fields)
	if err != nil {
		return err
	}
	for k, v := range values {
		doc[k] = v
	}
	if err := writeConfigDocument(configPath, doc); err != nil {
		return err
	}
	return nil
}

func valuesFromFields(fields []settingField) (map[string]any, error) {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		val := f.Spec.Normalize(f.Value)
		if err := f.Spec.Validate(val); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Spec.Label, err)
		}
		switch f.Spec.Key {
		case "num", "monitor_interval", "resume_buffer_secs", "review_refresh_secs":
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("%s: must be an integer", f.Spec.Label)
			}
			out[f.Spec.Key] = n
		case "dispatch_plan_mode":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return nil, fmt.Errorf("%s: must be true or false", f.Spec.Label)
			}
			out[f.Spec.Key] = b
		default:
			out[f.Spec.Key] = val
		}
	}
	return out, nil
}

func loadConfigDocument(configPath string) (map[string]any, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return map[string]any{}, nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML in %s: %w", configPath, err)
	}
	if doc == nil {
		return map[string]any{}, nil
	}
	return doc, nil
}

func writeConfigDocument(configPath string, doc map[string]any) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding config YAML: %w", err)
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}

	tmp, err := os.CreateTemp(dir, ".claude-swarm-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp config file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("setting config file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replacing config file: %w", err)
	}
	return nil
}

func stringValue(doc map[string]any, key string) (string, bool) {
	raw, ok := doc[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return v, true
	case int:
		return strconv.Itoa(v), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		if v == float32(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func settingsWindowCommand() string {
	return "claude-swarm settings"
}
