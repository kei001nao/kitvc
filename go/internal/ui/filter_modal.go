package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterField struct {
	Label string
	Value string
}

type filterOp struct {
	Label string
	Value string
}

var musicFilterFields = []filterField{
	{"Title", "title"}, {"Artist", "artist"}, {"Album", "album"},
	{"Genre", "genre"}, {"Track#", "track_num"}, {"Disc#", "disc_num"},
	{"Duration", "duration"}, {"BPM", "bpm"}, {"CreatedAt", "created_at"},
}

var musicFilterOps = []filterOp{
	{"==", "=="}, {"!=", "!="}, {"Contains", "contains"},
	{"Not Contains", "not_contains"}, {">", ">"}, {"<", "<"},
	{">=", ">="}, {"<=", "<="}, {"Is Null", "is_null"}, {"Is Not Null", "is_not_null"},
}

type filterCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type filterEditModal struct {
	name           string
	conditions     []filterCondition
	sortSequence   [][2]string
	matchType      string
	focusedSection int // 0=name, 1=conditions, 2=sort
	condCursor     int
	sortCursor     int
	width          int
	height         int
}

type filterEditResult struct {
	closed    bool
	submitted bool
	name      string
	condJSON  string
	sortJSON  string
}

func newFilterEditModal(name string, conditionsJSON, sortJSON string) *filterEditModal {
	var conditions []filterCondition
	if conditionsJSON != "" && conditionsJSON != "[]" {
		json.Unmarshal([]byte(conditionsJSON), &conditions)
	}
	if conditions == nil {
		conditions = []filterCondition{}
	}

	var sortSeq [][2]string
	if sortJSON != "" && sortJSON != "[]" {
		json.Unmarshal([]byte(sortJSON), &sortSeq)
	}
	if sortSeq == nil {
		sortSeq = [][2]string{}
	}

	matchType := "and"
	if strings.Contains(conditionsJSON, `"op":"or"`) || strings.Contains(conditionsJSON, `"op": "or"`) {
		matchType = "or"
	}

	return &filterEditModal{
		name:       name,
		conditions: conditions,
		sortSequence: sortSeq,
		matchType:  matchType,
	}
}

func (m *filterEditModal) Update(msg tea.Msg) (*filterEditModal, filterEditResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, filterEditResult{}, nil
	}

	switch keyMsg.String() {
	case "esc":
		return m, filterEditResult{closed: true}, nil

	case "ctrl+enter", "ctrl+j":
		if m.name == "" {
			return m, filterEditResult{}, nil
		}
		condData := map[string]interface{}{
			"op":    m.matchType,
			"rules": m.conditions,
		}
		condJSON, _ := json.Marshal(condData)
		sortJSON, _ := json.Marshal(m.sortSequence)
		return m, filterEditResult{
			closed:    true,
			submitted: true,
			name:      m.name,
			condJSON:  string(condJSON),
			sortJSON:  string(sortJSON),
		}, nil

	case "tab", "down":
		m.focusedSection = (m.focusedSection + 1) % 3
	case "shift+tab", "up":
		m.focusedSection = (m.focusedSection - 1 + 3) % 3

	case "left":
		if m.focusedSection == 0 {
			m.matchType = "and"
		}
	case "right":
		if m.focusedSection == 0 {
			m.matchType = "or"
		}

	case "j":
		if m.focusedSection == 1 && len(m.conditions) > 0 {
			m.condCursor = (m.condCursor + 1) % len(m.conditions)
		} else if m.focusedSection == 2 && len(m.sortSequence) > 0 {
			m.sortCursor = (m.sortCursor + 1) % len(m.sortSequence)
		}
	case "k":
		if m.focusedSection == 1 && len(m.conditions) > 0 {
			m.condCursor--
			if m.condCursor < 0 {
				m.condCursor = len(m.conditions) - 1
			}
		} else if m.focusedSection == 2 && len(m.sortSequence) > 0 {
			m.sortCursor--
			if m.sortCursor < 0 {
				m.sortCursor = len(m.sortSequence) - 1
			}
		}
	}

	return m, filterEditResult{}, nil
}

func (m *filterEditModal) HandleInput(msg tea.KeyMsg) (*filterEditModal, filterEditResult, tea.Cmd) {
	return m.Update(msg)
}

func (m *filterEditModal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *filterEditModal) View() string {
	dialogW := m.width - 10
	if dialogW < 50 {
		dialogW = 50
	}
	if dialogW > 80 {
		dialogW = 80
	}

	var lines []string

	// Name section
	nameStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 0 {
		nameStyle = nameStyle.Foreground(lipgloss.Color("5"))
	}
	lines = append(lines, nameStyle.Render("View Name:"))
	lines = append(lines, "  "+m.name)

	// Match type section
	lines = append(lines, "")
	matchLabel := "Match Type:"
	if m.focusedSection == 0 {
		matchLabel = "► Match Type:"
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(matchLabel))

	andMarker := "○ AND"
	orMarker := "○ OR"
	if m.matchType == "and" {
		andMarker = "● AND"
	} else {
		orMarker = "● OR"
	}
	lines = append(lines, "  "+andMarker+"   "+orMarker)

	// Conditions section
	lines = append(lines, "")
	condHeader := "Filter Conditions (↑↓: Navigate  a: Add  Enter: Edit  d: Delete)"
	if m.focusedSection == 1 {
		condHeader = "► Filter Conditions (↑↓: Navigate  a: Add  Enter: Edit  d: Delete)"
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(condHeader))

	if len(m.conditions) == 0 {
		lines = append(lines, "  (No conditions - matches all tracks)")
	} else {
		for i, c := range m.conditions {
			fLabel := c.Field
			for _, f := range musicFilterFields {
				if f.Value == c.Field {
					fLabel = f.Label
					break
				}
			}
			prefix := "  "
			if m.focusedSection == 1 && i == m.condCursor {
				prefix = "▸ "
			}
			lines = append(lines, fmt.Sprintf("%s%s %s %s", prefix, fLabel, c.Op, c.Value))
		}
	}

	// Sort section
	lines = append(lines, "")
	sortHeader := "Sort Order (↑↓: Navigate  a: Add  d: Delete  +/-: Move)"
	if m.focusedSection == 2 {
		sortHeader = "► Sort Order (↑↓: Navigate  a: Add  d: Delete  +/-: Move)"
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(sortHeader))

	if len(m.sortSequence) == 0 {
		lines = append(lines, "  (No sort - default order)")
	} else {
		for i, s := range m.sortSequence {
			fLabel := s[0]
			for _, f := range musicFilterFields {
				if f.Value == s[0] {
					fLabel = f.Label
					break
				}
			}
			prefix := "  "
			if m.focusedSection == 2 && i == m.sortCursor {
				prefix = "▸ "
			}
			lines = append(lines, fmt.Sprintf("%s#%d %s (%s)", prefix, i+1, fLabel, s[1]))
		}
	}

	// Help
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Ctrl+Enter: Save   ESC: Cancel   Tab/↑↓: Navigate sections"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

type filterConditionModal struct {
	fields    []filterField
	ops       []filterOp
	fieldIdx  int
	opIdx     int
	valueInput textinput.Model
	width     int
	height    int
}

type filterConditionResult struct {
	closed    bool
	submitted bool
	condition filterCondition
}

func newFilterConditionModal(fieldIdx, opIdx int, value string) *filterConditionModal {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 30

	return &filterConditionModal{
		fields:     musicFilterFields,
		ops:        musicFilterOps,
		fieldIdx:   fieldIdx,
		opIdx:      opIdx,
		valueInput: ti,
	}
}

func (m *filterConditionModal) Update(msg tea.Msg) (*filterConditionModal, filterConditionResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, filterConditionResult{}, nil
	}

	switch keyMsg.String() {
	case "esc":
		return m, filterConditionResult{closed: true}, nil

	case "enter":
		if m.valueInput.Focused() {
			cond := filterCondition{
				Field: m.fields[m.fieldIdx].Value,
				Op:    m.ops[m.opIdx].Value,
				Value: m.valueInput.Value(),
			}
			return m, filterConditionResult{closed: true, submitted: true, condition: cond}, nil
		}

	case "tab", "down":
		if m.fieldIdx < len(m.fields)-1 {
			m.fieldIdx++
		} else if m.opIdx < len(m.ops)-1 {
			m.opIdx++
		} else {
			m.valueInput.Focus()
		}
		return m, filterConditionResult{}, nil

	case "shift+tab", "up":
		if m.valueInput.Focused() {
			m.valueInput.Blur()
		} else if m.opIdx > 0 {
			m.opIdx--
		} else if m.fieldIdx > 0 {
			m.fieldIdx--
		}
		return m, filterConditionResult{}, nil

	case "left":
		if m.fieldIdx < len(m.fields)-1 {
			m.fieldIdx++
		}
	case "right":
		if m.opIdx > 0 {
			m.opIdx--
		}
	}

	var cmd tea.Cmd
	m.valueInput, cmd = m.valueInput.Update(msg)
	return m, filterConditionResult{}, cmd
}

func (m *filterConditionModal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *filterConditionModal) View() string {
	dialogW := 60

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Add Filter Condition"))
	lines = append(lines, "")

	// Field selection
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Field:"))
	for i, f := range m.fields {
		prefix := "  "
		if i == m.fieldIdx {
			prefix = "▸ "
		}
		lines = append(lines, prefix+f.Label)
	}

	lines = append(lines, "")

	// Operator selection
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Operator:"))
	for i, o := range m.ops {
		prefix := "  "
		if i == m.opIdx {
			prefix = "▸ "
		}
		lines = append(lines, prefix+o.Label)
	}

	lines = append(lines, "")

	// Value input
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Value:"))
	lines = append(lines, m.valueInput.View())

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Enter: OK   ESC: Cancel"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

type sortFieldSelectModal struct {
	fields   []filterField
	cursor   int
	width    int
	height   int
}

type sortFieldSelectResult struct {
	closed    bool
	submitted bool
	field     string
}

func newSortFieldSelectModal(usedFields map[string]bool) *sortFieldSelectModal {
	var available []filterField
	for _, f := range musicFilterFields {
		if !usedFields[f.Value] {
			available = append(available, f)
		}
	}
	return &sortFieldSelectModal{fields: available}
}

func (m *sortFieldSelectModal) Update(msg tea.Msg) (*sortFieldSelectModal, sortFieldSelectResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, sortFieldSelectResult{}, nil
	}

	switch keyMsg.String() {
	case "esc":
		return m, sortFieldSelectResult{closed: true}, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.fields)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.fields) {
			return m, sortFieldSelectResult{
				closed:    true,
				submitted: true,
				field:     m.fields[m.cursor].Value,
			}, nil
		}
	}

	return m, sortFieldSelectResult{}, nil
}

func (m *sortFieldSelectModal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *sortFieldSelectModal) View() string {
	dialogW := 40
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Select Sort Field"))
	lines = append(lines, "")

	for i, f := range m.fields {
		prefix := "  "
		if i == m.cursor {
			prefix = "▸ "
		}
		lines = append(lines, prefix+f.Label)
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Enter: Select   ESC: Cancel"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}
