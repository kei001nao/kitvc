package ui

import (
	"fmt"
	"strings"
	"kitvc/internal/musicbrainz"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/evertras/bubble-table/table"
)

type musicFetchFocus int

const (
	focusMusicQuery musicFetchFocus = iota
	focusRelease
)

type musicFetchModal struct {
	client *musicbrainz.Client

	queryInput textinput.Model

	releaseTable table.Model

	releases   []musicbrainz.Release
	recordings []musicbrainz.Recording
	releaseInfo *musicbrainz.ReleaseInfo

	focus musicFetchFocus

	width  int
	height int

	help     string
	loading  bool
	errorMsg string

	Cancelled bool
	Submitted bool

	// Results
	SelectedArtist  string
	SelectedRelease string
	SelectedMBID    string
	SelectedTracks  []musicbrainz.ReleaseTrack
}

func releaseCols(w int) []table.Column {
	if w < 50 { w = 50 }
	// Track: 20, Release: 20, Artist: 15, Year: 10
	yearW := 8
	artistW := 15
	trackW := 20
	albumW := w - yearW - artistW - trackW - 6
	if albumW < 10 { albumW = 10 }
	
	return []table.Column{
		table.NewColumn("track", "Track", trackW).WithStyle(lipgloss.NewStyle().Align(lipgloss.Left)),
		table.NewColumn("title", "Release", albumW).WithStyle(lipgloss.NewStyle().Align(lipgloss.Left)),
		table.NewColumn("artist", "Artist", artistW).WithStyle(lipgloss.NewStyle().Align(lipgloss.Left)),
		table.NewColumn("date", "Year", yearW).WithStyle(lipgloss.NewStyle().Align(lipgloss.Center)),
	}
}

func newMusicFetchModal(query, artist, album string) *musicFetchModal {
	ti := textinput.New()
	ti.SetValue(query)
	ti.Focus()

	m := &musicFetchModal{
		client:      musicbrainz.NewClient(),
		queryInput:  ti,
		focus:       focusMusicQuery,
		help:        "Enter: Search  Tab: Switch Focus  Esc: Cancel",
		releaseTable: table.New(releaseCols(80)).
			Focused(false).
			HighlightStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))).
			Border(table.Border{
				Top: " ", Bottom: " ", Left: " ", Right: " ",
				TopLeft: " ", TopRight: " ", BottomLeft: " ", BottomRight: " ",
			}),
	}
	return m
}

func (m *musicFetchModal) SetSize(w, h int) {
	if w < 40 { w = 40 }
	if h < 10 { h = 10 }
	m.width = w
	m.height = h
	
	availW := m.width - 4
	tableH := m.height - 10
	if tableH < 2 { tableH = 2 }

	m.releaseTable = m.releaseTable.
		WithColumns(releaseCols(availW)).
		WithTargetHeight(tableH).
		WithMaxTotalWidth(availW)
}

func (m *musicFetchModal) loadReleaseTable() {
	if m.width < 10 || m.height < 10 {
		return
	}

	var rows []table.Row
	if len(m.recordings) > 0 {
		for _, r := range m.recordings {
			rows = append(rows, table.NewRow(table.RowData{
				"track":  r.Title,
				"title":  r.ReleaseTitle,
				"artist": r.ArtistName,
				"date":   r.Date,
			}))
		}
	} else {
		for _, r := range m.releases {
			rows = append(rows, table.NewRow(table.RowData{
				"track":  "-",
				"title":  r.Title,
				"artist": r.ArtistName,
				"date":   r.Date,
			}))
		}
	}
	
	m.releaseTable = m.releaseTable.
		WithRows(rows).
		Focused(true).
		WithHighlightedRow(0)
}

func (m *musicFetchModal) Update(msg tea.Msg) (*musicFetchModal, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case musicReleaseSearchResultMsg:
		m.loading = false
		m.releases = msg.releases
		m.recordings = nil // Clear recordings if it was release search
		if len(msg.releases) == 0 {
			m.errorMsg = "No releases found"
		} else {
			m.errorMsg = ""
			m.focus = focusRelease
			m.loadReleaseTable()
			m.queryInput.Blur()
		}
		return m, nil

	case musicRecordingSearchResultMsg:
		m.loading = false
		m.recordings = msg.recordings
		m.releases = nil
		if len(msg.recordings) == 0 {
			m.errorMsg = "No tracks found"
		} else {
			m.errorMsg = ""
			m.focus = focusRelease
			m.loadReleaseTable()
			m.queryInput.Blur()
		}
		return m, nil

	case musicReleaseDetailMsg:
		m.loading = false
		m.releaseInfo = msg.release
		m.SelectedMBID = msg.release.ID
		m.SelectedRelease = msg.release.Title
		m.SelectedArtist = msg.release.Artist
		m.SelectedTracks = msg.release.Tracks
		m.Submitted = true
		return m, nil

	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}
		
		k := msg.String()
		switch k {
		case "esc":
			m.Cancelled = true
			return m, nil

		case "enter":
			if m.focus == focusMusicQuery {
				query := m.queryInput.Value()
				if query != "" {
					m.loading = true
					m.errorMsg = ""
					return m, func() tea.Msg {
						// If query contains "recording:", use SearchRecordings
						if strings.Contains(query, "recording:") {
							recs, err := m.client.SearchRecordings(query)
							if err != nil {
								return musicRecordingSearchResultMsg{err: err}
							}
							return musicRecordingSearchResultMsg{recordings: recs}
						}
						releases, err := m.client.SearchReleases(query)
						if err != nil {
							return musicReleaseSearchResultMsg{err: err}
						}
						return musicReleaseSearchResultMsg{releases: releases}
					}
				}
			} else if m.focus == focusRelease {
				idx := m.releaseTable.GetHighlightedRowIndex()
				var mbid string
				if len(m.recordings) > 0 {
					if idx >= 0 && idx < len(m.recordings) {
						mbid = m.recordings[idx].ReleaseID
					}
				} else if idx >= 0 && idx < len(m.releases) {
					mbid = m.releases[idx].ID
				}

				if mbid != "" {
					m.loading = true
					m.errorMsg = ""
					return m, func() tea.Msg {
						info, err := m.client.GetRelease(mbid)
						if err != nil {
							return musicReleaseDetailMsg{err: err}
						}
						return musicReleaseDetailMsg{release: info}
					}
				}
			}

		case "tab", "shift+tab":
			if m.focus == focusMusicQuery {
				if len(m.releases) > 0 {
					m.focus = focusRelease
					m.releaseTable = m.releaseTable.Focused(true)
					m.queryInput.Blur()
				}
			} else {
				m.focus = focusMusicQuery
				m.releaseTable = m.releaseTable.Focused(false)
				m.queryInput.Focus()
			}
			return m, nil
		}
	}

	if m.focus == focusMusicQuery {
		m.queryInput, cmd = m.queryInput.Update(msg)
	} else if m.focus == focusRelease {
		m.releaseTable, cmd = m.releaseTable.Update(msg)
	}

	return m, cmd
}

func (m *musicFetchModal) View() string {
	if m.width < 10 || m.height < 10 {
		return "Initializing..."
	}

	style := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("5")).
		Padding(1, 1)

	var content string

	header := "MusicBrainz Search"
	if m.loading {
		header = "Searching..."
	} else if m.errorMsg != "" {
		header = m.errorMsg
	}

	switch m.focus {
	case focusMusicQuery:
		content = fmt.Sprintf("%s\n\nSearch (Artist, Album, Title):\n%s", header, m.queryInput.View())
	case focusRelease:
		content = fmt.Sprintf("%s\n\nSelect Release (Up/Down: move, Enter: fetch tracks, apply):\n%s", header, m.releaseTable.View())
	}

	return style.Render(content)
}

type musicArtistSearchResultMsg struct {
	artists []musicbrainz.Artist
	err     error
}

type musicRecordingSearchResultMsg struct {
	recordings []musicbrainz.Recording
	err        error
}

type musicReleaseSearchResultMsg struct {
	releases []musicbrainz.Release
	err      error
}

type musicReleaseDetailMsg struct {
	release *musicbrainz.ReleaseInfo
	err     error
}
