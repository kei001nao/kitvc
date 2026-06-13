package ui

import (
	"fmt"
	"kitvc/internal/tmdb"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/evertras/bubble-table/table"
)

type fetchFocus int

const (
	focusQuery fetchFocus = iota
	focusType
	focusSeries
	focusSeason
	focusEpisode
)

type videoFetchModal struct {
	client     *tmdb.Client
	queryInput textinput.Model
	mediaType  string // "Movie", "TV Show", "Music Video"
	
	seriesTable  table.Model
	seasonTable  table.Model
	episodeTable table.Model
	
	seriesItems  []tmdb.SearchItem
	seasonItems  []tmdb.SeasonInfo // Simplified or just season info
	episodeItems []tmdb.EpisodeDetail
	
	focus fetchFocus
	
	width  int
	height int
	
	loading bool
	errorMsg string
	
	// Results to return
	SelectedID      int
	SelectedIsTV    bool
	SelectedSeason  int
	SelectedEpisode int
	Cancelled       bool
	Submitted       bool
}

func newVideoFetchModal(apiKey, query string, isTV bool, initialSeason, initialEpisode int) *videoFetchModal {
	ti := textinput.New()
	ti.SetValue(query)
	ti.Focus()
	
	mediaType := "Movie"
	if isTV {
		mediaType = "TV Show"
	}

	m := &videoFetchModal{
		client:     tmdb.NewClient(apiKey),
		queryInput: ti,
		mediaType:  mediaType,
		focus:      focusQuery,
		SelectedSeason: initialSeason,
		SelectedEpisode: initialEpisode,
	}
	
	m.initTables()
	return m
}

func (m *videoFetchModal) initTables() {
	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("5")).
		Bold(true)

	// Left aligned styles
	leftStyle := lipgloss.NewStyle().Align(lipgloss.Left)

	m.seriesTable = table.New([]table.Column{
		table.NewColumn("title", "Series/Movie", 30).WithStyle(leftStyle),
		table.NewColumn("year", "Year", 6).WithStyle(leftStyle),
	}).WithTargetHeight(10).
		Focused(false).
		HighlightStyle(highlightStyle).
		HeaderStyle(headerStyle).
		Border(table.Border{
			Top:    "",
			Bottom: "",
			Left:   "",
			Right:  "",
		})

	m.seasonTable = table.New([]table.Column{
		table.NewColumn("name", "Season", 20).WithStyle(leftStyle),
	}).WithTargetHeight(10).
		Focused(false).
		HighlightStyle(highlightStyle).
		HeaderStyle(headerStyle).
		Border(table.Border{
			Top:    "",
			Bottom: "",
			Left:   "",
			Right:  "",
		})

	m.episodeTable = table.New([]table.Column{
		table.NewColumn("num", "E#", 4).WithStyle(leftStyle),
		table.NewColumn("name", "Title", 30).WithStyle(leftStyle),
		table.NewColumn("date", "Date", 12).WithStyle(leftStyle),
	}).WithTargetHeight(10).
		Focused(false).
		HighlightStyle(highlightStyle).
		HeaderStyle(headerStyle).
		Border(table.Border{
			Top:    "",
			Bottom: "",
			Left:   "",
			Right:  "",
		})
}

func (m *videoFetchModal) Update(msg tea.Msg) (*videoFetchModal, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.Cancelled = true
			return m, nil
		case "tab":
			m.nextFocus()
			return m, nil
		case "shift+tab":
			m.prevFocus()
			return m, nil
		case "enter", "ctrl+j":
			return m.handleEnter()
		}

		// Handle focus-specific keys
		switch m.focus {
		case focusQuery:
			var cmd tea.Cmd
			m.queryInput, cmd = m.queryInput.Update(msg)
			cmds = append(cmds, cmd)
		case focusType:
			switch msg.String() {
			case "left", "h":
				m.cycleType(-1)
			case "right", "l":
				m.cycleType(1)
			}
		case focusSeries:
			var cmd tea.Cmd
			m.seriesTable, cmd = m.seriesTable.Update(msg)
			cmds = append(cmds, cmd)
			m.onSeriesSelected()
		case focusSeason:
			var cmd tea.Cmd
			m.seasonTable, cmd = m.seasonTable.Update(msg)
			cmds = append(cmds, cmd)
			m.onSeasonSelected()
		case focusEpisode:
			var cmd tea.Cmd
			m.episodeTable, cmd = m.episodeTable.Update(msg)
			cmds = append(cmds, cmd)
		}

	case searchResultMsg:
		m.loading = false
		m.seriesItems = msg.items
		m.populateSeriesTable()
		return m, nil

	case seasonsResultMsg:
		m.loading = false
		m.seasonItems = msg.seasons
		m.populateSeasonTable()
		return m, nil

	case episodesResultMsg:
		m.loading = false
		m.episodeItems = msg.episodes
		m.populateEpisodeTable()
		return m, nil

	case errorMsg:
		m.loading = false
		m.errorMsg = string(msg)
		return m, nil
	}

	return m, tea.Batch(cmds...)
}

func (m *videoFetchModal) nextFocus() {
	m.blurAll()
	m.focus = (m.focus + 1) % 5
	m.focusAll()
}

func (m *videoFetchModal) prevFocus() {
	m.blurAll()
	m.focus--
	if m.focus < 0 {
		m.focus = 4
	}
	m.focusAll()
}

func (m *videoFetchModal) blurAll() {
	m.queryInput.Blur()
	m.seriesTable = m.seriesTable.Focused(false)
	m.seasonTable = m.seasonTable.Focused(false)
	m.episodeTable = m.episodeTable.Focused(false)
}

func (m *videoFetchModal) focusAll() {
	switch m.focus {
	case focusQuery:
		m.queryInput.Focus()
	case focusSeries:
		m.seriesTable = m.seriesTable.Focused(true)
	case focusSeason:
		m.seasonTable = m.seasonTable.Focused(true)
	case focusEpisode:
		m.episodeTable = m.episodeTable.Focused(true)
	}
}

func (m *videoFetchModal) cycleType(delta int) {
	opts := []string{"Movie", "TV Show", "Music Video"}
	idx := 0
	for i, opt := range opts {
		if opt == m.mediaType {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(opts)) % len(opts)
	m.mediaType = opts[idx]
}

type searchResultMsg struct{ items []tmdb.SearchItem }
type seasonsResultMsg struct{ seasons []tmdb.SeasonInfo }
type episodesResultMsg struct{ episodes []tmdb.EpisodeDetail }
type errorMsg string

func (m *videoFetchModal) handleEnter() (*videoFetchModal, tea.Cmd) {
	if m.focus == focusQuery || m.focus == focusType {
		m.loading = true
		m.errorMsg = ""
		query := m.queryInput.Value()
		isTV := m.mediaType == "TV Show"
		
		m.focus = focusSeries
		m.focusAll()

		return m, func() tea.Msg {
			var items []tmdb.SearchItem
			var err error
			if isTV {
				items, err = m.client.SearchTV(query)
			} else {
				items, err = m.client.SearchMovie(query)
			}
			if err != nil {
				return errorMsg(err.Error())
			}
			return searchResultMsg{items}
		}
	}

	if m.focus == focusSeries {
		if m.mediaType == "TV Show" {
			m.nextFocus()
			return m, m.fetchSeasons()
		} else {
			// Movie selected
			m.Submitted = true
			m.onFinalSelect()
			return m, nil
		}
	}

	if m.focus == focusSeason {
		m.nextFocus()
		return m, m.fetchEpisodes()
	}

	if m.focus == focusEpisode {
		m.Submitted = true
		m.onFinalSelect()
		return m, nil
	}
	
	return m, nil
}

func (m *videoFetchModal) fetchSeasons() tea.Cmd {
	idx := m.seriesTable.GetHighlightedRowIndex()
	if idx < 0 || idx >= len(m.seriesItems) {
		return nil
	}
	m.loading = true
	id := m.seriesItems[idx].ID
	return func() tea.Msg {
		details, err := m.client.FetchTVDetails(id)
		if err != nil {
			return errorMsg(err.Error())
		}
		return seasonsResultMsg{details.Seasons}
	}
}

func (m *videoFetchModal) fetchEpisodes() tea.Cmd {
	sIdx := m.seriesTable.GetHighlightedRowIndex()
	seIdx := m.seasonTable.GetHighlightedRowIndex()
	if sIdx < 0 || sIdx >= len(m.seriesItems) || seIdx < 0 || seIdx >= len(m.seasonItems) {
		return nil
	}
	m.loading = true
	seriesID := m.seriesItems[sIdx].ID
	seasonNum := m.seasonItems[seIdx].SeasonNumber
	return func() tea.Msg {
		details, err := m.client.FetchTVSeason(seriesID, seasonNum)
		if err != nil {
			return errorMsg(err.Error())
		}
		return episodesResultMsg{details.Episodes}
	}
}

func (m *videoFetchModal) onFinalSelect() {
	sIdx := m.seriesTable.GetHighlightedRowIndex()
	if sIdx >= 0 && sIdx < len(m.seriesItems) {
		m.SelectedID = m.seriesItems[sIdx].ID
		m.SelectedIsTV = (m.mediaType == "TV Show")
	}
	
	if m.SelectedIsTV {
		seIdx := m.seasonTable.GetHighlightedRowIndex()
		if seIdx >= 0 && seIdx < len(m.seasonItems) {
			m.SelectedSeason = m.seasonItems[seIdx].SeasonNumber
		}
		eIdx := m.episodeTable.GetHighlightedRowIndex()
		if eIdx >= 0 && eIdx < len(m.episodeItems) {
			m.SelectedEpisode = m.episodeItems[eIdx].EpisodeNumber
		}
	}
}

func (m *videoFetchModal) populateSeriesTable() {
	rows := make([]table.Row, len(m.seriesItems))
	for i, item := range m.seriesItems {
		title := item.Title
		if title == "" {
			title = item.Name
		}
		year := ""
		if item.ReleaseDate != "" && len(item.ReleaseDate) >= 4 {
			year = item.ReleaseDate[:4]
		} else if item.FirstAirDate != "" && len(item.FirstAirDate) >= 4 {
			year = item.FirstAirDate[:4]
		}
		rows[i] = table.NewRow(table.RowData{
			"title": title,
			"year":  year,
		})
	}
	m.seriesTable = m.seriesTable.WithRows(rows)
	if len(rows) > 0 {
		m.seriesTable = m.seriesTable.WithHighlightedRow(0)
	}
}

func (m *videoFetchModal) populateSeasonTable() {
	rows := make([]table.Row, len(m.seasonItems))
	for i, item := range m.seasonItems {
		rows[i] = table.NewRow(table.RowData{
			"name": fmt.Sprintf("%s (%d ep)", item.Name, item.EpisodeCount),
		})
	}
	m.seasonTable = m.seasonTable.WithRows(rows)
	if len(rows) > 0 {
		m.seasonTable = m.seasonTable.WithHighlightedRow(0)
	}
}

func (m *videoFetchModal) populateEpisodeTable() {
	rows := make([]table.Row, len(m.episodeItems))
	for i, item := range m.episodeItems {
		rows[i] = table.NewRow(table.RowData{
			"num":  item.EpisodeNumber,
			"name": item.Name,
			"date": item.AirDate,
		})
	}
	m.episodeTable = m.episodeTable.WithRows(rows)
	if len(rows) > 0 {
		m.episodeTable = m.episodeTable.WithHighlightedRow(0)
	}
}

func (m *videoFetchModal) onSeriesSelected() {
}

func (m *videoFetchModal) onSeasonSelected() {
}

func (m *videoFetchModal) View() string {
	var header strings.Builder
	// Header
	header.WriteString(lipgloss.NewStyle().Bold(true).Render("TMDB Search") + "\n\n")
	
	// Query Input
	queryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if m.focus == focusQuery {
		queryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	}
	header.WriteString(queryStyle.Render("Search Query:") + "\n")
	header.WriteString(m.queryInput.View() + "\n\n")
	
	// Type Selection
	typeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if m.focus == focusType {
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	}
	header.WriteString(typeStyle.Render("Media Type:  "))
	opts := []string{"Movie", "TV Show", "Music Video"}
	for _, opt := range opts {
		if opt == m.mediaType {
			header.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Render(" "+opt+" ") + "  ")
		} else {
			header.WriteString(" " + opt + "   ")
		}
	}
	header.WriteString("\n\n")
	headerView := header.String()

	var footer strings.Builder
	// Footer
	if m.loading {
		footer.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("Loading...") + "\n")
	} else if m.errorMsg != "" {
		footer.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Error: "+m.errorMsg) + "\n")
	} else {
		footer.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Tab: Focus  Enter: Search/Select  Esc: Cancel") + "\n")
	}
	footerView := footer.String()

	// Adjust table heights based on available space
	headerH := lipgloss.Height(headerView)
	footerH := lipgloss.Height(footerView)
	tableH := m.height - 10 - headerH - footerH
	if tableH < 5 {
		tableH = 5
	}
	m.seriesTable = m.seriesTable.WithTargetHeight(tableH)
	m.seasonTable = m.seasonTable.WithTargetHeight(tableH)
	m.episodeTable = m.episodeTable.WithTargetHeight(tableH)

	// Wrap tables in focused styles
	baseStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))

	focusedStyle := baseStyle.Copy().
		BorderForeground(lipgloss.Color("5"))

	seriesStyle := baseStyle
	if m.focus == focusSeries {
		seriesStyle = focusedStyle
	}
	seasonStyle := baseStyle
	if m.focus == focusSeason {
		seasonStyle = focusedStyle
	}
	episodeStyle := baseStyle
	if m.focus == focusEpisode {
		episodeStyle = focusedStyle
	}

	// Tables
	tables := lipgloss.JoinHorizontal(lipgloss.Top,
		seriesStyle.Render(m.seriesTable.View()),
		seasonStyle.Render(m.seasonTable.View()),
		episodeStyle.Render(m.episodeTable.View()),
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		headerView,
		tables,
		"\n",
		footerView,
	)

	return lipgloss.NewStyle().
		Width(m.width - 4).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(content)
}

func (m *videoFetchModal) SetSize(w, h int) {
	m.width = w
	m.height = h

	// Left aligned styles
	leftStyle := lipgloss.NewStyle().Align(lipgloss.Left)

	// Adjust table widths proportionally
	availW := w - 10
	seriesW := availW * 30 / 100
	seasonW := availW * 15 / 100
	episodeW := availW * 55 / 100

	// Update columns with new widths
	m.seriesTable = m.seriesTable.WithColumns([]table.Column{
		table.NewColumn("title", "Series/Movie", seriesW-10).WithStyle(leftStyle),
		table.NewColumn("year", "Year", 6).WithStyle(leftStyle),
	}).WithMaxTotalWidth(seriesW)

	m.seasonTable = m.seasonTable.WithColumns([]table.Column{
		table.NewColumn("name", "Season", seasonW-4).WithStyle(leftStyle),
	}).WithMaxTotalWidth(seasonW)

	m.episodeTable = m.episodeTable.WithColumns([]table.Column{
		table.NewColumn("num", "E#", 4).WithStyle(leftStyle),
		table.NewColumn("name", "Title", episodeW-20).WithStyle(leftStyle),
		table.NewColumn("date", "Date", 12).WithStyle(leftStyle),
	}).WithMaxTotalWidth(episodeW)
}
