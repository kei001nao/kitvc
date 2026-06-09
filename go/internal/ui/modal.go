package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalTextInput
	modalListSelect
	modalConfirm
	modalForm
)

type formField struct {
	Label string
	Input textinput.Model
}

type modal struct {
	kind       modalKind
	title      string
	textInput  textinput.Model
	items      []string
	cursor     int
	confirmMsg string
	help       string
	width      int
	height     int
	formFields []formField
	formFocus  int
}

type modalUpdateResult struct {
	closed    bool
	submitted bool
	text      string
	index     int
	values    []string
}

func newTextInputModal(title, placeholder, help string) *modal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 30

	return &modal{
		kind:      modalTextInput,
		title:     title,
		textInput: ti,
		help:      help,
	}
}

func newListSelectModal(title string, items []string, help string) *modal {
	return &modal{
		kind:  modalListSelect,
		title: title,
		items: items,
		help:  help,
	}
}

func newConfirmModal(confirmMsg string) *modal {
	return &modal{
		kind:       modalConfirm,
		confirmMsg: confirmMsg,
		help:       "Enter: Confirm  Esc: Cancel",
	}
}

func newFormModal(title string, labels []string, initialValues []string, help string) *modal {
	fields := make([]formField, len(labels))
	for i := range labels {
		ti := textinput.New()
		ti.Placeholder = labels[i]
		ti.SetValue(initialValues[i])
		ti.CharLimit = 200
		ti.Width = 30
		if i == 0 {
			ti.Focus()
		}
		fields[i] = formField{Label: labels[i], Input: ti}
	}
	return &modal{
		kind:       modalForm,
		title:      title,
		formFields: fields,
		formFocus:  0,
		help:       help,
	}
}

func (m *modal) Update(msg tea.Msg) (*modal, modalUpdateResult, tea.Cmd) {
	switch m.kind {
	case modalTextInput:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter", "ctrl+j":
				val := strings.TrimSpace(m.textInput.Value())
				if val != "" {
					return m, modalUpdateResult{closed: true, submitted: true, text: val}, nil
				}
				return m, modalUpdateResult{}, nil
			case "esc":
				return m, modalUpdateResult{closed: true}, nil
			}
		}
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, modalUpdateResult{}, cmd

	case modalListSelect:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			case "enter", "ctrl+j":
				if m.cursor >= 0 && m.cursor < len(m.items) {
					return m, modalUpdateResult{closed: true, submitted: true, text: m.items[m.cursor], index: m.cursor}, nil
				}
			case "esc":
				return m, modalUpdateResult{closed: true}, nil
			}
		}
		return m, modalUpdateResult{}, nil

	case modalConfirm:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter", "ctrl+j", "y":
				return m, modalUpdateResult{closed: true, submitted: true}, nil
			case "esc", "n":
				return m, modalUpdateResult{closed: true}, nil
			}
		}
		return m, modalUpdateResult{}, nil

	case modalForm:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "tab", "down", "j":
				m.formFields[m.formFocus].Input.Blur()
				m.formFocus = (m.formFocus + 1) % len(m.formFields)
				m.formFields[m.formFocus].Input.Focus()
				return m, modalUpdateResult{}, nil
			case "shift+tab", "up", "k":
				m.formFields[m.formFocus].Input.Blur()
				m.formFocus = (m.formFocus - 1 + len(m.formFields)) % len(m.formFields)
				m.formFields[m.formFocus].Input.Focus()
				return m, modalUpdateResult{}, nil
			case "enter", "ctrl+j":
				values := make([]string, len(m.formFields))
				for i, f := range m.formFields {
					values[i] = f.Input.Value()
				}
				return m, modalUpdateResult{closed: true, submitted: true, values: values}, nil
			case "esc":
				return m, modalUpdateResult{closed: true}, nil
			}
		}
		var cmd tea.Cmd
		m.formFields[m.formFocus].Input, cmd = m.formFields[m.formFocus].Input.Update(msg)
		return m, modalUpdateResult{}, cmd
	}
	return m, modalUpdateResult{}, nil
}

func (m *modal) View() string {
	dialogW := m.width
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 60 {
		dialogW = 60
	}
	innerW := dialogW - 4

	var content string

	switch m.kind {
	case modalTextInput:
		m.textInput.Width = innerW
		content = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Render(m.title),
			"",
			m.textInput.View(),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.help),
		)
	case modalListSelect:
		var itemViews []string
		for i, item := range m.items {
			if i == m.cursor {
				itemViews = append(itemViews, lipgloss.NewStyle().
					Foreground(lipgloss.Color("229")).
					Background(lipgloss.Color("57")).
					Render("▸ "+item))
			} else {
				itemViews = append(itemViews, "  "+item)
			}
		}

		maxH := 10
		vp := viewport.New(innerW, maxH)
		vp.SetContent(strings.Join(itemViews, "\n"))
		vp.GotoTop()

		content = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Render(m.title),
			"",
			vp.View(),
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.help),
		)
	case modalConfirm:
		content = lipgloss.JoinVertical(lipgloss.Left,
			m.confirmMsg,
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.help),
		)
	case modalForm:
		var lines []string
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(m.title))
		lines = append(lines, "")
		for _, f := range m.formFields {
			f.Input.Width = innerW - 2
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(f.Label+":"))
			lines = append(lines, f.Input.View())
			lines = append(lines, "")
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("249")).Render(m.help))
		content = strings.Join(lines, "\n")
	}

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

func (m *modal) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *modal) Active() bool {
	return m != nil && m.kind != modalNone
}
