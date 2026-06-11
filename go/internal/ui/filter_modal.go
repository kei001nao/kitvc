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

var videoFilterFields = []filterField{
	{"Type", "type"}, {"Category", "category"}, {"SubCategory", "subcategory"},
	{"Series", "series"}, {"Season", "season"}, {"Episode", "episode"},
	{"Title", "title"}, {"Year", "year"}, {"Genres", "genres"},
	{"Duration", "duration"}, {"CreatedAt", "created_at"},
}

var videoFilterOps = []filterOp{
	{"==", "=="}, {"!=", "!="}, {"Contains", "contains"},
	{"Not Contains", "not_contains"}, {">", ">"}, {"<", "<"},
	{"Is Null", "is_null"}, {"Is Not Null", "is_not_null"},
}

type filterCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type filterEditModal struct {
	nameInput      textinput.Model
	conditions     []filterCondition
	sortSequence   [][2]string
	matchType      string
	focusedSection int // 0=name, 1=matchType, 2=conditions, 3=sort
	condCursor     int
	sortCursor     int
	width          int
	height         int
	filterFields   []filterField
	filterOps      []filterOp
}

type filterEditResult struct {
	closed    bool
	submitted bool
	name      string
	condJSON  string
	sortJSON  string
}

func newFilterEditModal(name string, conditionsJSON, sortJSON string, filterFields []filterField, filterOps []filterOp) *filterEditModal {
	var conditions []filterCondition

	// Parse the conditions JSON which has format: {"op":"and","rules":[...]}
	if conditionsJSON != "" && conditionsJSON != "[]" {
		var condData struct {
			Op    string             `json:"op"`
			Rules []filterCondition `json:"rules"`
		}
		if err := json.Unmarshal([]byte(conditionsJSON), &condData); err == nil {
			conditions = condData.Rules
		}
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

	ti := textinput.New()
	ti.SetValue(name)
	ti.Placeholder = "Enter view name..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	return &filterEditModal{
		nameInput:    ti,
		conditions:   conditions,
		sortSequence: sortSeq,
		matchType:    matchType,
		filterFields: filterFields,
		filterOps:    filterOps,
	}
}

func (m *filterEditModal) Update(msg tea.Msg) (*filterEditModal, filterEditResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, filterEditResult{}, nil
	}

	// Handle text input when focused on name section
	if m.focusedSection == 0 && m.nameInput.Focused() {
		switch keyMsg.String() {
		case "tab":
			m.nameInput.Blur()
			m.focusedSection = 1
			return m, filterEditResult{}, nil
		case "shift+tab":
			m.nameInput.Blur()
			m.focusedSection = 3
			return m, filterEditResult{}, nil
		case "esc":
			return m, filterEditResult{closed: true}, nil
		case "ctrl+j", "enter":
			if m.nameInput.Value() == "" {
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
				name:      m.nameInput.Value(),
				condJSON:  string(condJSON),
				sortJSON:  string(sortJSON),
			}, nil
		}
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, filterEditResult{}, cmd
	}

	switch keyMsg.String() {
	case "esc":
		return m, filterEditResult{closed: true}, nil

	case "ctrl+j", "enter":
		if m.nameInput.Value() == "" {
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
			name:      m.nameInput.Value(),
			condJSON:  string(condJSON),
			sortJSON:  string(sortJSON),
		}, nil

	case "tab":
		// Move to next section
		m.focusedSection = (m.focusedSection + 1) % 4
		if m.focusedSection == 0 {
			m.nameInput.Focus()
		}
	case "shift+tab":
		// Move to previous section
		m.focusedSection = (m.focusedSection - 1 + 4) % 4
		if m.focusedSection == 0 {
			m.nameInput.Focus()
		}

	case "down":
		// Move down within current section
		if m.focusedSection == 1 {
			// Match Type: toggle AND/OR
			if m.matchType == "and" {
				m.matchType = "or"
			}
		} else if m.focusedSection == 2 && len(m.conditions) > 0 {
			// Conditions: move cursor down
			m.condCursor = (m.condCursor + 1) % len(m.conditions)
		} else if m.focusedSection == 3 && len(m.sortSequence) > 0 {
			// Sort: move cursor down
			m.sortCursor = (m.sortCursor + 1) % len(m.sortSequence)
		}
	case "up":
		// Move up within current section
		if m.focusedSection == 1 {
			// Match Type: toggle AND/OR
			if m.matchType == "or" {
				m.matchType = "and"
			}
		} else if m.focusedSection == 2 && len(m.conditions) > 0 {
			// Conditions: move cursor up
			m.condCursor--
			if m.condCursor < 0 {
				m.condCursor = len(m.conditions) - 1
			}
		} else if m.focusedSection == 3 && len(m.sortSequence) > 0 {
			// Sort: move cursor up
			m.sortCursor--
			if m.sortCursor < 0 {
				m.sortCursor = len(m.sortSequence) - 1
			}
		}

	case "left":
		if m.focusedSection == 1 {
			m.matchType = "and"
		}
	case "right":
		if m.focusedSection == 1 {
			m.matchType = "or"
		}
	}

	return m, filterEditResult{}, nil
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
	nameLabel := "View Name:"
	nameStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 0 {
		nameLabel = "► View Name:"
		nameStyle = nameStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, nameStyle.Render(nameLabel))
	lines = append(lines, "  "+m.nameInput.View())

	// Match type section
	lines = append(lines, "")
	matchLabel := "Match Type:"
	matchStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 1 {
		matchLabel = "► Match Type:"
		matchStyle = matchStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, matchStyle.Render(matchLabel))

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
	condHeader := "Filter Conditions (a: Add  Enter: Edit  d: Delete)"
	condStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 2 {
		condHeader = "► Filter Conditions (↑↓: Navigate  a: Add  Enter: Edit  d: Delete)"
		condStyle = condStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, condStyle.Render(condHeader))

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
			if m.focusedSection == 2 && i == m.condCursor {
				prefix = "▸ "
			}
			lines = append(lines, fmt.Sprintf("%s%s %s %s", prefix, fLabel, c.Op, c.Value))
		}
	}

	// Sort section
	lines = append(lines, "")
	sortHeader := "Sort Order (a: Add  d: Delete  +/-: Move)"
	sortStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 3 {
		sortHeader = "► Sort Order (↑↓: Navigate  a: Add  d: Delete  +/-: Move)"
		sortStyle = sortStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, sortStyle.Render(sortHeader))

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
			if m.focusedSection == 3 && i == m.sortCursor {
				prefix = "▸ "
			}
			lines = append(lines, fmt.Sprintf("%s#%d %s (%s)", prefix, i+1, fLabel, s[1]))
		}
	}

	// Help
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Ctrl+J/Enter: Save   ESC: Cancel   Tab: Next section   ↑↓: Navigate within section"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

type filterConditionModal struct {
	fields          []filterField
	ops             []filterOp
	fieldIdx        int
	opIdx           int
	valueInput      textinput.Model
	focusedSection  int // 0=field, 1=operator, 2=value
	width           int
	height          int
}

type filterConditionResult struct {
	closed    bool
	submitted bool
	condition filterCondition
}

func newFilterConditionModal(fieldIdx, opIdx int, value string, filterFields []filterField, filterOps []filterOp) *filterConditionModal {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Blur() // Start blurred, focus only when on value section
	ti.CharLimit = 100
	ti.Width = 30

	return &filterConditionModal{
		fields:         filterFields,
		ops:            filterOps,
		fieldIdx:       fieldIdx,
		opIdx:          opIdx,
		valueInput:     ti,
		focusedSection: 0, // Focus on field first
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

	case "ctrl+j", "enter":
		cond := filterCondition{
			Field: m.fields[m.fieldIdx].Value,
			Op:    m.ops[m.opIdx].Value,
			Value: m.valueInput.Value(),
		}
		return m, filterConditionResult{closed: true, submitted: true, condition: cond}, nil

	case "tab":
		// Move to next section: Field -> Operator -> Value -> Field
		m.focusedSection = (m.focusedSection + 1) % 3
		if m.focusedSection == 2 {
			m.valueInput.Focus()
		} else {
			m.valueInput.Blur()
		}
		return m, filterConditionResult{}, nil

	case "shift+tab":
		// Move to previous section: Value -> Operator -> Field -> Value
		m.focusedSection = (m.focusedSection - 1 + 3) % 3
		if m.focusedSection == 2 {
			m.valueInput.Focus()
		} else {
			m.valueInput.Blur()
		}
		return m, filterConditionResult{}, nil

	case "up":
		// Move up in current selection
		if m.focusedSection == 0 && m.fieldIdx > 0 {
			m.fieldIdx--
		} else if m.focusedSection == 1 && m.opIdx > 0 {
			m.opIdx--
		}
		return m, filterConditionResult{}, nil

	case "down":
		// Move down in current selection
		if m.focusedSection == 0 && m.fieldIdx < len(m.fields)-1 {
			m.fieldIdx++
		} else if m.focusedSection == 1 && m.opIdx < len(m.ops)-1 {
			m.opIdx++
		}
		return m, filterConditionResult{}, nil
	}

	// Handle text input for value
	if m.focusedSection == 2 {
		var cmd tea.Cmd
		m.valueInput, cmd = m.valueInput.Update(msg)
		return m, filterConditionResult{}, cmd
	}

	return m, filterConditionResult{}, nil
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
	fieldLabel := "Field:"
	fieldStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 0 {
		fieldLabel = "► Field:"
		fieldStyle = fieldStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, fieldStyle.Render(fieldLabel))
	for i, f := range m.fields {
		prefix := "  "
		if i == m.fieldIdx {
			if m.focusedSection == 0 {
				prefix = "▸ "  // Currently focused
			} else {
				prefix = "✓ "  // Previously selected
			}
		}
		lines = append(lines, prefix+f.Label)
	}

	lines = append(lines, "")

	// Operator selection
	opLabel := "Operator:"
	opStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 1 {
		opLabel = "► Operator:"
		opStyle = opStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, opStyle.Render(opLabel))
	for i, o := range m.ops {
		prefix := "  "
		if i == m.opIdx {
			if m.focusedSection == 1 {
				prefix = "▸ "  // Currently focused
			} else {
				prefix = "✓ "  // Previously selected
			}
		}
		lines = append(lines, prefix+o.Label)
	}

	lines = append(lines, "")

	// Value input
	valueLabel := "Value:"
	valueStyle := lipgloss.NewStyle().Bold(true)
	if m.focusedSection == 2 {
		valueLabel = "► Value:"
		valueStyle = valueStyle.Foreground(lipgloss.Color("2")) // Green
	}
	lines = append(lines, valueStyle.Render(valueLabel))
	lines = append(lines, m.valueInput.View())

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Tab: Next field   ↑↓: Select   Ctrl+J/Enter: OK   ESC: Cancel"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

type sortFieldSelectItem struct {
	field      string
	fieldLabel string
	direction  string // "ASC" or "DESC"
}

type sortFieldSelectModal struct {
	items  []sortFieldSelectItem
	cursor int
	width  int
	height int
}

type sortFieldSelectResult struct {
	closed    bool
	submitted bool
	field     string
	direction string
}

func newSortFieldSelectModal(usedFields map[string]bool, filterFields []filterField) *sortFieldSelectModal {
	var items []sortFieldSelectItem
	for _, f := range filterFields {
		if !usedFields[f.Value] {
			items = append(items, sortFieldSelectItem{
				field:      f.Value,
				fieldLabel: f.Label,
				direction:  "ASC",
			})
			items = append(items, sortFieldSelectItem{
				field:      f.Value,
				fieldLabel: f.Label,
				direction:  "DESC",
			})
		}
	}
	return &sortFieldSelectModal{items: items}
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
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor >= 0 && m.cursor < len(m.items) {
			return m, sortFieldSelectResult{
				closed:    true,
				submitted: true,
				field:     m.items[m.cursor].field,
				direction: m.items[m.cursor].direction,
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

	for i, item := range m.items {
		prefix := "  "
		if i == m.cursor {
			prefix = "▸ "
		}
		lines = append(lines, fmt.Sprintf("%s%s(%s)", prefix, item.fieldLabel, item.direction))
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("↑↓: Select   Enter: OK   ESC: Cancel"))

	content := strings.Join(lines, "\n")

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}
