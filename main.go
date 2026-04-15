package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// Config

type Status struct {
	Name   string `yaml:"name"`
	Prefix string `yaml:"prefix"`
}

type Config struct {
	Statuses []Status `yaml:"statuses"`
}

func loadConfig() Config {
	defaultConfig := Config{
		Statuses: []Status{
			{Name: "none", Prefix: ""},
			{Name: "wip", Prefix: "🔨 "},
			{Name: "review", Prefix: "👀 "},
			{Name: "test", Prefix: "🧪 "},
			{Name: "done", Prefix: "✅ "},
			{Name: "blocked", Prefix: "🚫 "},
		},
	}

	paths := []string{}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "ztabs", "config.yml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "ztabs", "config.yml"))
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if len(cfg.Statuses) > 0 {
			return cfg
		}
	}
	return defaultConfig
}

// Zellij interaction

const commentPrefix = "💬 "

type Tab struct {
	ID       int
	Position int
	Name     string
	BaseName string
	Status   int
	Comment  string
}

// buildTabName composes a zellij tab name from status/comment/baseName.
// Layout: <status-prefix><comment-indicator?><baseName>.
func buildTabName(cfg Config, statusIdx int, hasComment bool, baseName string) string {
	var b strings.Builder
	if statusIdx >= 0 && statusIdx < len(cfg.Statuses) {
		b.WriteString(cfg.Statuses[statusIdx].Prefix)
	}
	if hasComment {
		b.WriteString(commentPrefix)
	}
	b.WriteString(baseName)
	return b.String()
}

func listTabs(cfg Config) ([]Tab, error) {
	out, err := exec.Command("zellij", "action", "list-tabs").Output()
	if err != nil {
		return nil, fmt.Errorf("zellij action list-tabs: %w", err)
	}

	comments, _ := loadComments()

	var tabs []Tab
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "TAB_ID") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		id, _ := strconv.Atoi(fields[0])
		pos, _ := strconv.Atoi(fields[1])
		name := strings.Join(fields[2:], " ")

		baseName, statusIdx := parseTabName(name, cfg)
		tabs = append(tabs, Tab{
			ID:       id,
			Position: pos,
			Name:     name,
			BaseName: baseName,
			Status:   statusIdx,
			Comment:  comments[baseName],
		})
	}
	return tabs, nil
}

// parseTabName strips the optional status prefix and the optional 💬 comment
// indicator from a raw zellij tab name. Returns the bare baseName and the
// status index (0 = "none"). The comment itself lives in the sidecar file,
// not in the name — only the 💬 indicator is cosmetic.
func parseTabName(name string, cfg Config) (string, int) {
	statusIdx := 0
	for i, s := range cfg.Statuses {
		if s.Prefix != "" && strings.HasPrefix(name, s.Prefix) {
			name = strings.TrimPrefix(name, s.Prefix)
			statusIdx = i
			break
		}
	}
	name = strings.TrimPrefix(name, commentPrefix)
	return name, statusIdx
}

func renameTab(id int, newName string) error {
	return exec.Command("zellij", "action", "rename-tab-by-id", strconv.Itoa(id), newName).Run()
}

func moveTab(id int, direction string) error {
	// Go to the tab, move it, then let the caller refresh
	if err := exec.Command("zellij", "action", "go-to-tab-by-id", strconv.Itoa(id)).Run(); err != nil {
		return err
	}
	return exec.Command("zellij", "action", "move-tab", direction).Run()
}

// Bubbletea model

type mode int

const (
	modeNormal mode = iota
	modeRename
	modeComment
)

type model struct {
	cfg       Config
	tabs      []Tab
	cursor    int
	err       error
	mode      mode
	textInput textinput.Model
}

func currentTabID() int {
	out, err := exec.Command("zellij", "action", "current-tab-info").Output()
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "id: ") {
			id, _ := strconv.Atoi(strings.TrimPrefix(line, "id: "))
			return id
		}
	}
	return -1
}

func initialModel() model {
	cfg := loadConfig()
	tabs, err := listTabs(cfg)

	ti := textinput.New()
	ti.CharLimit = 80
	ti.Width = 40

	cursor := 0
	activeID := currentTabID()
	for i, t := range tabs {
		if t.ID == activeID {
			cursor = i
			break
		}
	}

	return model{cfg: cfg, tabs: tabs, cursor: cursor, err: err, textInput: ti}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeRename:
		return m.updateRename(msg)
	case modeComment:
		return m.updateComment(msg)
	}
	return m.updateNormal(msg)
}

func (m model) updateNormal(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.tabs)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if len(m.tabs) > 0 {
				tab := m.tabs[m.cursor]
				_ = exec.Command("zellij", "action", "go-to-tab-by-id", strconv.Itoa(tab.ID)).Run()
				return m, tea.Quit
			}
		case "tab", " ", "l", "right":
			if len(m.tabs) > 0 {
				m.cycleStatus(1)
			}
		case "shift+tab", "h", "left":
			if len(m.tabs) > 0 {
				m.cycleStatus(-1)
			}
		case "r":
			if len(m.tabs) > 0 {
				m.mode = modeRename
				m.textInput.SetValue(m.tabs[m.cursor].BaseName)
				m.textInput.Focus()
				m.textInput.CursorEnd()
				return m, textinput.Blink
			}
		case "c":
			if len(m.tabs) > 0 {
				m.mode = modeComment
				m.textInput.SetValue(m.tabs[m.cursor].Comment)
				m.textInput.Focus()
				m.textInput.CursorEnd()
				return m, textinput.Blink
			}
		case "J":
			if len(m.tabs) > 0 && m.cursor < len(m.tabs)-1 {
				m.moveTabInList(1)
			}
		case "K":
			if len(m.tabs) > 0 && m.cursor > 0 {
				m.moveTabInList(-1)
			}
		}
	}
	return m, nil
}

func (m model) updateRename(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			newBase := m.textInput.Value()
			if newBase != "" {
				tab := &m.tabs[m.cursor]
				oldBase := tab.BaseName
				tab.BaseName = newBase
				tab.Name = buildTabName(m.cfg, tab.Status, tab.Comment != "", newBase)
				_ = renameTab(tab.ID, tab.Name)
				if oldBase != newBase {
					_ = migrateTabState(oldBase, newBase)
				}
			}
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateComment(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			tab := &m.tabs[m.cursor]
			newText := m.textInput.Value()
			tab.Comment = newText
			_ = setComment(tab.BaseName, newText)
			tab.Name = buildTabName(m.cfg, tab.Status, tab.Comment != "", tab.BaseName)
			_ = renameTab(tab.ID, tab.Name)
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.textInput.Blur()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *model) cycleStatus(dir int) {
	tab := &m.tabs[m.cursor]
	n := len(m.cfg.Statuses)
	tab.Status = (tab.Status + dir + n) % n
	tab.Name = buildTabName(m.cfg, tab.Status, tab.Comment != "", tab.BaseName)
	_ = renameTab(tab.ID, tab.Name)
	_ = setStatus(tab.BaseName, m.cfg.Statuses[tab.Status].Name)
}

func (m *model) moveTabInList(dir int) {
	i := m.cursor
	j := i + dir

	tab := m.tabs[i]
	if dir > 0 {
		_ = moveTab(tab.ID, "Right")
	} else {
		_ = moveTab(tab.ID, "Left")
	}

	// Swap in our local list and update positions
	m.tabs[i], m.tabs[j] = m.tabs[j], m.tabs[i]
	m.tabs[i].Position, m.tabs[j].Position = m.tabs[j].Position, m.tabs[i].Position
	m.cursor = j
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}
	if len(m.tabs) == 0 {
		return "No tabs found.\n\nPress q to quit."
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("183")).
		Width(10)

	var b strings.Builder
	b.WriteString(titleStyle.Render("ztabs — Zellij Tab Status Manager"))
	b.WriteString("\n")

	for i, tab := range m.tabs {
		cursor := "  "
		nameRender := dimStyle.Render
		if i == m.cursor {
			cursor = "▸ "
			nameRender = selectedStyle.Render
		}

		statusName := m.cfg.Statuses[tab.Status].Name
		statusLabel := statusStyle.Render("[" + statusName + "]")

		commentPreview := ""
		if tab.Comment != "" {
			preview := tab.Comment
			if len([]rune(preview)) > 40 {
				preview = string([]rune(preview)[:40]) + "…"
			}
			commentPreview = "  " + dimStyle.Render("— "+preview)
		}

		if m.mode == modeRename && i == m.cursor {
			b.WriteString(fmt.Sprintf("%s%s %s%s\n", cursor, statusLabel, m.textInput.View(), commentPreview))
		} else if m.mode == modeComment && i == m.cursor {
			b.WriteString(fmt.Sprintf("%s%s %s  %s %s\n", cursor, statusLabel, nameRender(tab.Name), dimStyle.Render("💬"), m.textInput.View()))
		} else {
			b.WriteString(fmt.Sprintf("%s%s %s%s\n", cursor, statusLabel, nameRender(tab.Name), commentPreview))
		}
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	switch m.mode {
	case modeRename, modeComment:
		b.WriteString(helpStyle.Render("⏎ confirm • esc cancel"))
	default:
		b.WriteString(helpStyle.Render("↑/k ↓/j navigate • h/← l/→ status • ⏎ goto • r rename • c comment • J/K move • q quit"))
	}
	return b.String()
}

func headlessCycle(dir int) error {
	cfg := loadConfig()
	tabs, err := listTabs(cfg)
	if err != nil {
		return err
	}
	activeID := currentTabID()
	if activeID < 0 {
		return fmt.Errorf("cannot determine current tab")
	}
	for _, t := range tabs {
		if t.ID != activeID {
			continue
		}
		n := len(cfg.Statuses)
		newStatus := (t.Status + dir + n) % n
		newName := buildTabName(cfg, newStatus, t.Comment != "", t.BaseName)
		if err := renameTab(t.ID, newName); err != nil {
			return err
		}
		return setStatus(t.BaseName, cfg.Statuses[newStatus].Name)
	}
	return fmt.Errorf("current tab not found in list")
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "next":
			if err := headlessCycle(1); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "prev":
			if err := headlessCycle(-1); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "migrate-state", "migrate-comment":
			// "migrate-comment" is kept as an alias so older builds of the
			// background plugin keep working until it's rebuilt.
			if len(os.Args) < 4 {
				fmt.Fprintln(os.Stderr, "usage: ztabs migrate-state OLD NEW")
				os.Exit(1)
			}
			// Inputs are raw zellij tab names — strip status/💬 prefixes to
			// recover the baseName that the sidecar is keyed by.
			cfg := loadConfig()
			oldBase, _ := parseTabName(os.Args[2], cfg)
			newBase, _ := parseTabName(os.Args[3], cfg)
			if err := migrateTabState(oldBase, newBase); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "-h", "--help", "help":
			fmt.Println("ztabs — Zellij Tab Status Manager")
			fmt.Println()
			fmt.Println("Usage:")
			fmt.Println("  ztabs [flags]                 open the interactive TUI")
			fmt.Println("  ztabs next                    cycle current tab status forward")
			fmt.Println("  ztabs prev                    cycle current tab status backward")
			fmt.Println("  ztabs migrate-state OLD NEW   rename sidecar key after tab rename")
			fmt.Println()
			fmt.Println("TUI flags:")
			fmt.Println("  --exit-mode MODE  switch zellij to MODE after the TUI exits")
			fmt.Println("                    (e.g. normal, locked, pane, tab); empty = no switch")
			return
		}
	}

	exitMode := flag.String("exit-mode", "", "zellij input mode to switch to when the TUI exits")
	flag.Parse()

	p := tea.NewProgram(initialModel())
	_, runErr := p.Run()
	if *exitMode != "" {
		_ = exec.Command("zellij", "action", "switch-mode", *exitMode).Run()
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
		os.Exit(1)
	}
}
