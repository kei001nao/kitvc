package ui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
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

	posterPath       string
	posterCols       int
	posterRows       int
	overlayStartLine int
	overlayStartCol  int
}

type videoEditFieldKind int

const (
	videoFieldInput videoEditFieldKind = iota
	videoFieldSelect
	videoFieldTextArea
)

type videoEditField struct {
	Label    string
	Kind     videoEditFieldKind
	Input    textinput.Model
	TextArea textarea.Model
	Select   int
	Options  []string
	Height   int
}

var videoTypeOptions = []string{"Movie", "TV Show", "Music Video"}

func newVideoEditModal(filename string, labels []string, fieldKinds []videoEditFieldKind, initialValues []string, options [][]string) *videoEditModal {
	fields := make([]videoEditField, len(labels))
	for i := range labels {
		f := videoEditField{
			Label:  labels[i],
			Kind:   fieldKinds[i],
			Height: 1, // Default height
		}
		// Increase height for overview/synopsis fields
		if strings.Contains(strings.ToLower(labels[i]), "overview") || strings.Contains(strings.ToLower(labels[i]), "synopsis") {
			f.Height = 5
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
		case videoFieldTextArea:
			ta := textarea.New()
			ta.Placeholder = labels[i]
			if i < len(initialValues) {
				ta.SetValue(initialValues[i])
				ta.MoveToBegin()
			}
			ta.CharLimit = 2000
			ta.SetHeight(f.Height)
			ta.SetWidth(40)
			ta.ShowLineNumbers = false
			fields[i] = f
			fields[i].TextArea = ta
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
		posterCols:      12,
		posterRows:      8,
	}
	if len(initialValues) > 12 {
		m.posterPath = initialValues[12]
	}
	return m
}

func (m *videoEditModal) SetThumbnail(path string) {
	m.thumbnailPath = path
}

func (m *videoEditModal) SetPosterPath(path string) { m.posterPath = path }

func (m *videoEditModal) SetFieldValue(index int, value string) {
	if index >= 0 && index < len(m.fields) && m.fields[index].Kind == videoFieldInput {
		m.fields[index].Input.SetValue(value)
	}
}
func (m *videoEditModal) PosterPath() string        { return m.posterPath }
func (m *videoEditModal) PosterCols() int           { return m.posterCols }
func (m *videoEditModal) PosterRows() int           { return m.posterRows }
func (m *videoEditModal) SetOverlayPos(sl, sc int)  { m.overlayStartLine = sl; m.overlayStartCol = sc }
func (m *videoEditModal) OverlayStartLine() int     { return m.overlayStartLine }
func (m *videoEditModal) OverlayStartCol() int      { return m.overlayStartCol }
func (m *videoEditModal) Width() int                { return m.width }

func (m *videoEditModal) HeaderHeight() int {
	dialogW := m.width - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}
	h := 1 // "Edit Video"
	if m.filename != "" {
		name := m.filename
		maxLen := dialogW - 4
		if maxLen < 20 {
			maxLen = 20
		}
		h += (len(name) + maxLen - 1) / maxLen
	}
	h += 1 // Spacer
	return h
}

func (m *videoEditModal) Update(msg tea.Msg) (*videoEditModal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		k := strings.ToLower(keyMsg.String())
		// Global keys for this modal
		switch k {
		case "esc":
			m.cancelled = true
			return m, nil
		case "ctrl+t", "ctrl+s", "ctrl+f":
			m.searchTMDB = true
			return m, nil
		case "enter", "ctrl+j":
			m.submitted = true
			return m, nil
		case "tab", "down":
			m.nextField()
			return m, nil
		case "up": // up/down only for fields, shift+tab also for backward
			m.prevField()
			return m, nil
		case "shift+tab":
			m.prevField()
			return m, nil
		case "pgup":
			m.viewport.ScrollUp(m.height - 4)
			return m, nil
		case "pgdown":
			m.viewport.ScrollDown(m.height - 4)
			return m, nil
		}
	}

	// Update focused field if it didn't match global keys
	if m.focusIndex < len(m.fields) {
		f := &m.fields[m.focusIndex]
		switch f.Kind {
		case videoFieldInput:
			var cmd tea.Cmd
			f.Input, cmd = f.Input.Update(msg)
			return m, cmd
		case videoFieldTextArea:
			var cmd tea.Cmd
			f.TextArea, cmd = f.TextArea.Update(msg)
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
	y := 0
	for i := 0; i < m.focusIndex; i++ {
		// Label (1) + Input/Select (Height) + Margin (1)
		y += 1 + m.fields[i].Height + 1
	}
	vpH := m.viewport.Height()
	if vpH < 1 {
		vpH = 1
	}

	fieldH := 1 + m.fields[m.focusIndex].Height

	if y < m.viewport.YOffset() {
		m.viewport.SetYOffset(y)
	} else if y+fieldH > m.viewport.YOffset()+vpH {
		m.viewport.SetYOffset(y + fieldH - vpH + 1)
	}
}

func (m *videoEditModal) nextField() {
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Blur()
	} else if m.fields[m.focusIndex].Kind == videoFieldTextArea {
		m.fields[m.focusIndex].TextArea.Blur()
	}
	m.focusIndex++
	if m.focusIndex >= len(m.fields) {
		m.focusIndex = 0
	}
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Focus()
	} else if m.fields[m.focusIndex].Kind == videoFieldTextArea {
		m.fields[m.focusIndex].TextArea.Focus()
	}
	m.scrollToFocused()
}

func (m *videoEditModal) prevField() {
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Blur()
	} else if m.fields[m.focusIndex].Kind == videoFieldTextArea {
		m.fields[m.focusIndex].TextArea.Blur()
	}
	m.focusIndex--
	if m.focusIndex < 0 {
		m.focusIndex = len(m.fields) - 1
	}
	if m.fields[m.focusIndex].Kind == videoFieldInput {
		m.fields[m.focusIndex].Input.Focus()
	} else if m.fields[m.focusIndex].Kind == videoFieldTextArea {
		m.fields[m.focusIndex].TextArea.Focus()
	}
	m.scrollToFocused()
}

func (m *videoEditModal) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.SetWidth(w - 6)
	// Viewport height will be set in View() based on other elements
}

func (m *videoEditModal) renderPosterBlock() string {
	dialogW := m.width - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}

	var sb strings.Builder
	centerLine := m.posterRows / 2
	for i := 0; i < m.posterRows; i++ {
		line := strings.Repeat(" ", dialogW)
		if m.posterPath == "" && i == centerLine {
			noPoster := "[No Poster]"
			pad := (dialogW - lipgloss.Width(noPoster)) / 2
			if pad < 0 {
				pad = 0
			}
			line = strings.Repeat(" ", pad) + noPoster + strings.Repeat(" ", dialogW-pad-lipgloss.Width(noPoster))
		}
		sb.WriteString(line)
		if i < m.posterRows-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m *videoEditModal) View() string {
	dialogW := m.width - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}
	
	headerH := m.HeaderHeight()

	var headerLines []string
	headerLines = append(headerLines, lipgloss.NewStyle().Bold(true).Render("Edit Video"))
	if m.filename != "" {
		name := m.filename
		maxLen := dialogW - 4
		if maxLen < 20 {
			maxLen = 20
		}
		// Wrap name if too long
		for len(name) > 0 {
			chunk := name
			if len(chunk) > maxLen {
				chunk = name[:maxLen]
				name = name[maxLen:]
			} else {
				name = ""
			}
			headerLines = append(headerLines, lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(chunk))
		}
	}
	headerLines = append(headerLines, "")
	headerView := strings.Join(headerLines, "\n")

	var footerLines []string
	tmdbBtn := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Ctrl+s: TMDB Search")
	footerKeys := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Tab/↓: Next  Shift+Tab/↑: Prev  Enter: Save  Esc: Cancel")
	footerLines = append(footerLines, "")
	footerLines = append(footerLines, tmdbBtn+"  "+footerKeys)
	footerView := strings.Join(footerLines, "\n")

	footerH := lipgloss.Height(footerView)

	fieldsW := dialogW - 4
	if fieldsW < 20 {
		fieldsW = 20
	}

	// Calculate viewport height: Total height - borders(2) - padding(0) - header - poster - footer - spacers(2)
	vpH := m.height - 2 - headerH - m.posterRows - footerH - 2
	if vpH < 5 {
		vpH = 5
	}
	m.viewport.SetHeight(vpH)

	var fieldLines []string
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
		case videoFieldTextArea:
			f.TextArea.SetWidth(fieldsW - 2)
			f.TextArea.SetHeight(f.Height)
			fieldLines = append(fieldLines, f.TextArea.View())
		case videoFieldSelect:
			var opts []string
			for j, opt := range f.Options {
				if j == f.Select {
					opts = append(opts, lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Render("▸ "+opt))
				} else {
					opts = append(opts, "  "+opt)
				}
			}
			fieldLines = append(fieldLines, strings.Join(opts, "  "))
		}
		fieldLines = append(fieldLines, "")
	}

	m.viewport.SetContent(strings.Join(fieldLines, "\n"))

	posterBlock := m.renderPosterBlock()

	mainView := lipgloss.JoinVertical(lipgloss.Left,
		headerView,
		posterBlock,
		m.viewport.View(),
		footerView,
	)

	return lipgloss.NewStyle().
		Width(dialogW).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(mainView)
}

func (m *videoEditModal) Values() []string {
	values := make([]string, len(m.fields))
	for i, f := range m.fields {
		switch f.Kind {
		case videoFieldInput:
			values[i] = strings.TrimSpace(f.Input.Value())
		case videoFieldTextArea:
			values[i] = strings.TrimSpace(f.TextArea.Value())
		case videoFieldSelect:
			values[i] = f.Options[f.Select]
		}
	}
	return values
}

func (m *videoEditModal) Active() bool {
	return m != nil && !m.submitted && !m.cancelled
}
