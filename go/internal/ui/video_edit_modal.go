package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type videoEditModal struct {
	viewport        viewport.Model
	fields          []videoEditField
	focusIndex      int
	width           int
	height          int
	submitted       bool
	cancelled       bool
	searchTMDB      bool
	thumbnailPath   string
	thumbnailWidth  int
	thumbnailHeight int
	filename        string
}

type videoEditFieldKind int

const (
	videoFieldInput videoEditFieldKind = iota
	videoFieldSelect
)

type videoEditField struct {
	Label   string
	Kind    videoEditFieldKind
	Input   textinput.Model
	Select  int
	Options []string
}

var videoTypeOptions = []string{"Movie", "TV Show", "Music Video"}

func newVideoEditModal(filename string, labels []string, fieldKinds []videoEditFieldKind, initialValues []string, options [][]string) *videoEditModal {
	fields := make([]videoEditField, len(labels))
	for i := range labels {
		f := videoEditField{
			Label: labels[i],
			Kind:  fieldKinds[i],
		}
		switch f.Kind {
		case videoFieldInput:
			ti := textinput.New()
			ti.Placeholder = labels[i]
			if i < len(initialValues) {
				ti.SetValue(initialValues[i])
			}
			ti.CharLimit = 500
			ti.SetWidth(40)
			fields[i] = f
			fields[i].Input = ti
		case videoFieldSelect:
			f.Options = options[i]
			f.Select = 0
			if i < len(initialValues) {
				for j, opt := range f.Options {
					if opt == initialValues[i] {
						f.Select = j
						break
					}
				}
			}
			fields[i] = f
		}
	}

	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))

	m := &videoEditModal{
		viewport:        vp,
		fields:          fields,
		focusIndex:      0,
		thumbnailWidth:  12,
		thumbnailHeight: 6,
		filename:        filename,
	}
	return m
}

func (m *videoEditModal) SetThumbnail(path string) {
	m.thumbnailPath = path
}

func (m *videoEditModal) Update(msg tea.Msg) (*videoEditModal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.cancelled = true
			return m, nil
		case "enter", "ctrl+j":
			m.submitted = true
			return m, nil
		case "tab", "down":
			m.nextField()
			return m, nil
		case "shift+tab", "up":
			m.prevField()
			return m, nil
		case "pgup":
			m.viewport.ScrollUp(m.height - 4)
			return m, nil
		case "pgdown":
			m.viewport.ScrollDown(m.height - 4)
			return m, nil
		case "ctrl+t":
			m.searchTMDB = true
			return m, nil
		}
	}

	// Update focused field
	if m.focusIndex < len(m.fields) {
		f := &m.fields[m.focusIndex]
		switch f.Kind {
		case videoFieldInput:
			var cmd tea.Cmd
			f.Input, cmd = f.Input.Update(msg)
			return m, cmd
		case videoFieldSelect:
			if keyMsg, ok := msg.(tea.KeyMsg); ok {
				switch keyMsg.String() {
				case "left", "h":
					if f.Select > 0 {
						f.Select--
					}
				case "right", "l":
					if f.Select < len(f.Options)-1 {
						f.Select++
					}
				}
			}
		}
	}

	// Scroll viewport
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *videoEditModal) scrollToFocused() {
	y := 2
	for i := 0; i < m.focusIndex; i++ {
		switch m.fields[i].Kind {
		case videoFieldInput:
			y += 3
		case videoFieldSelect:
			y += 3
		}
	}
	vpH := m.height - 6
	if vpH < 1 {
		vpH = 1
	}
	if y < m.viewport.YOffset() {
		m.viewport.SetYOffset(y)
	} else if y+3 > m.viewport.YOffset()+vpH {
		m.viewport.SetYOffset(y + 3 - vpH)
	}
}

func (m *videoEditModal) nextField() {
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Blur()
	}
	m.focusIndex++
	if m.focusIndex >= len(m.fields) {
		m.focusIndex = 0
	}
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Focus()
	}
	m.scrollToFocused()
}

func (m *videoEditModal) prevField() {
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Blur()
	}
	m.focusIndex--
	if m.focusIndex < 0 {
		m.focusIndex = len(m.fields) - 1
	}
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Focus()
	}
	m.scrollToFocused()
}

func (m *videoEditModal) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.SetWidth(w - 6)
	m.viewport.SetHeight(h - 6)
}

func (m *videoEditModal) renderThumbnail() string {
	w := m.thumbnailWidth
	h := m.thumbnailHeight

	boxStyle := lipgloss.NewStyle().
		Width(w).
		Height(h).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Align(lipgloss.Center, lipgloss.Center)

	if m.thumbnailPath == "" {
		return boxStyle.Render(lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("[No Thumb]"))
	}

	name := m.thumbnailPath
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if len(name) > w-4 {
		name = name[:w-4]
	}

	return boxStyle.Render(lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(name))
}

func (m *videoEditModal) View() string {
	dialogW := m.width - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}
	thumbAreaW := m.thumbnailWidth + 2
 fieldsW := dialogW - thumbAreaW - 4
	if fieldsW < 20 {
		fieldsW = 20
	}

	var lines []string

	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Edit Video"))

	if m.filename != "" {
		name := m.filename
		maxLen := dialogW - 4
		if maxLen < 20 {
			maxLen = 20
		}
		if len(name) > maxLen {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(name[:maxLen]))
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(name[maxLen:]))
		} else {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(name))
		}
	}

	lines = append(lines, "")

	thumbBlock := m.renderThumbnail()
	thumbLines := strings.Split(thumbBlock, "\n")

	fieldLines := make([]string, 0)
	for i, f := range m.fields {
		isFocused := i == m.focusIndex

		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		if isFocused {
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
		}
		fieldLines = append(fieldLines, labelStyle.Render(f.Label+":"))

		switch f.Kind {
		case videoFieldInput:
			f.Input.SetWidth(fieldsW - 2)
			fieldLines = append(fieldLines, f.Input.View())
		case videoFieldSelect:
			var opts []string
			for j, opt := range f.Options {
				if j == f.Select {
					opts = append(opts, lipgloss.NewStyle().
						Foreground(lipgloss.Color("229")).
						Background(lipgloss.Color("57")).
						Render("▸ "+opt))
				} else {
					opts = append(opts, "  "+opt)
				}
			}
			fieldLines = append(fieldLines, strings.Join(opts, "  "))
		}
		fieldLines = append(fieldLines, "")
	}

	vpH := m.height - 6
	if vpH < 1 {
		vpH = 1
	}

	maxLines := len(fieldLines)
	if len(thumbLines) > maxLines {
		maxLines = len(thumbLines)
	}

	for i := 0; i < maxLines; i++ {
		left := ""
		if i < len(thumbLines) {
			left = thumbLines[i]
		}
		leftPad := thumbAreaW - lipgloss.Width(left)
		if leftPad < 0 {
			leftPad = 0
		}

		right := ""
		if i < len(fieldLines) {
			right = fieldLines[i]
		}

		lines = append(lines, left+strings.Repeat(" ", leftPad)+right)
	}

	scrollInfo := ""
	if maxLines > vpH {
		topLine := m.viewport.YOffset() + 1
		bottomLine := m.viewport.YOffset() + vpH
		if bottomLine > maxLines {
			bottomLine = maxLines
		}
		scrollInfo = fmt.Sprintf("  [%d-%d/%d]", topLine, bottomLine, maxLines)
	}

	tmdbBtn := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("Ctrl+t: TMDB Search")
	footerKeys := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("Tab/↓: Next  Shift+Tab/↑: Prev  Enter: Save  Esc: Cancel" + scrollInfo)
	lines = append(lines, "")
	lines = append(lines, tmdbBtn+"  "+footerKeys)

	content := strings.Join(lines, "\n")
	m.viewport.SetContent(content)

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(m.viewport.View())
}

func (m *videoEditModal) Values() []string {
	values := make([]string, len(m.fields))
	for i, f := range m.fields {
		switch f.Kind {
		case videoFieldInput:
			values[i] = strings.TrimSpace(f.Input.Value())
		case videoFieldSelect:
			values[i] = f.Options[f.Select]
		}
	}
	return values
}

func (m *videoEditModal) Active() bool {
	return m != nil && !m.submitted && !m.cancelled
}
