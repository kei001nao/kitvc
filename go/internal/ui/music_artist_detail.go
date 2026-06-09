package ui

import (
	"strconv"
	"kitvc/internal/db"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type musicArtistDetail struct {
	artist       string
	albums       []db.Album
	tracks       []db.TrackData
	albumsTable  table.Model
	tracksTable  table.Model
	focusedUpper bool
	width        int
	height       int
	styles       table.Styles
	marked       map[int]bool
}

func newMusicArtistDetail(width, height int, artist string, albums []db.Album) musicArtistDetail {
	// 1. Albums table (Upper)
	at := table.New(
		table.WithColumns([]table.Column{
			{Title: "Date", Width: 12},
			{Title: "Album", Width: width - 15},
		}),
		table.WithFocused(true),
		table.WithHeight(5), // Fixed height for albums table
		table.WithWidth(width),
	)

	var albumRows []table.Row
	for _, album := range albums {
		albumRows = append(albumRows, table.Row{album.ReleaseDate, album.Title})
	}
	at.SetRows(albumRows)

	// 2. Tracks table (Lower) - Initialized empty
	tt := table.New(
		table.WithColumns([]table.Column{
			{Title: "Initializing...", Width: 10},
		}),
		table.WithFocused(false),
		table.WithHeight(height - 12), // Adjusted for albums, labels, borders
		table.WithWidth(width),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(false).
		Foreground(lipgloss.Color("5")).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	at.SetStyles(s)
	tt.SetStyles(s)

	mad := musicArtistDetail{
		artist:       artist,
		albums:       albums,
		albumsTable:  at,
		tracksTable:  tt,
		focusedUpper: true,
		width:        width,
		height:       height,
		styles:       s,
		marked:       make(map[int]bool),
	}

	mad.SetSize(width, height)
	
	// Load tracks for the first album if any
	if len(albums) > 0 {
		mad.loadTracksForAlbum(albums[0].Title)
	}

	return mad
}

func (mad *musicArtistDetail) loadTracksForAlbum(albumTitle string) {
	tracks, _ := db.GetMusicTracks(mad.artist, albumTitle)
	mad.tracks = tracks
	mad.ClearMarks()

	var rows []table.Row
	for i, t := range tracks {
		rows = append(rows, table.Row{
			"",                        // M
			strconv.Itoa(i + 1),       // #
			strconv.Itoa(t.TrackNum),  // Alb#
			t.Title,
			t.Artist,
			t.Album,
			formatDuration(t.Duration),
		})
	}
	mad.tracksTable.SetRows(rows)
	if len(rows) > 0 {
		mad.tracksTable.SetCursor(0)
	}
}

func (mad musicArtistDetail) Update(msg tea.Msg) (musicArtistDetail, tea.Cmd) {
	var cmd tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if mad.focusedUpper {
				// Move focus to tracks list
				mad.focusedUpper = false
				mad.syncTableFocus()
				return mad, nil
			}
			// If lower is focused, Enter is handled by main ui.go to play the track
		case "esc":
			if !mad.focusedUpper {
				// Return focus to albums list
				mad.focusedUpper = true
				mad.syncTableFocus()
				return mad, nil
			}
		}
	}

	if mad.focusedUpper {
		oldCursor := mad.albumsTable.Cursor()
		mad.albumsTable, cmd = mad.albumsTable.Update(msg)
		newCursor := mad.albumsTable.Cursor()

		// If album selection changed, load new tracks
		if oldCursor != newCursor && newCursor >= 0 && newCursor < len(mad.albums) {
			mad.loadTracksForAlbum(mad.albums[newCursor].Title)
		}
	} else {
		mad.tracksTable, cmd = mad.tracksTable.Update(msg)
	}

	return mad, cmd
}

func (mad musicArtistDetail) View() string {
	albumsLabel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render("Albums")
	tracksLabel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render("Tracks")

	// Render tables
	albumsView := mad.albumsTable.View()
	tracksView := mad.tracksTable.View()

	return lipgloss.JoinVertical(lipgloss.Left,
		albumsLabel,
		albumsView,
		"",
		tracksLabel,
		tracksView,
	)
}

func (mad *musicArtistDetail) SetSize(w, h int) {
	if w <= 0 {
		w = 1
	}
	mad.width = w
	mad.height = h

	// Upper Table: height 5
	mad.albumsTable.SetWidth(w)
	mad.albumsTable.SetHeight(5)
	mad.albumsTable.SetColumns([]table.Column{
		{Title: "Date", Width: 12},
		{Title: "Album", Width: w - 15},
	})

	// Lower Table: height h - 14 (space for header/footer, labels, space, albums table)
	lowerHeight := h - 14
	if lowerHeight < 3 {
		lowerHeight = 3
	}
	mad.tracksTable.SetWidth(w)
	mad.tracksTable.SetHeight(lowerHeight)

	avail := w - 27
	if avail < 15 {
		avail = 15
	}

	durationWidth := 10
	otherAvail := avail - durationWidth
	if otherAvail < 10 {
		otherAvail = 10
	}

	cols := []table.Column{
		{Title: "M", Width: 3},
		{Title: "#", Width: 4},
		{Title: "Alb#", Width: 6},
		{Title: "Title", Width: otherAvail * 4 / 10},
		{Title: "Artist", Width: otherAvail * 3 / 10},
		{Title: "Album", Width: otherAvail - (otherAvail * 4 / 10) - (otherAvail * 3 / 10)},
		{Title: "Duration", Width: durationWidth},
	}
	for i := 3; i < 6; i++ {
		if cols[i].Width < 4 {
			cols[i].Width = 4
		}
	}
	mad.tracksTable.SetColumns(cols)
}

func (mad *musicArtistDetail) SetFocus(f bool) {
	if f {
		mad.syncTableFocus()
	} else {
		mad.albumsTable.Blur()
		mad.tracksTable.Blur()
		mad.albumsTable.SetStyles(mad.getStyles(false))
		mad.tracksTable.SetStyles(mad.getStyles(false))
	}
}

func (mad *musicArtistDetail) syncTableFocus() {
	if mad.focusedUpper {
		mad.albumsTable.Focus()
		mad.tracksTable.Blur()
		mad.albumsTable.SetStyles(mad.getStyles(true))
		mad.tracksTable.SetStyles(mad.getStyles(false))
	} else {
		mad.albumsTable.Blur()
		mad.tracksTable.Focus()
		mad.albumsTable.SetStyles(mad.getStyles(false))
		mad.tracksTable.SetStyles(mad.getStyles(true))
	}
}

func (mad musicArtistDetail) getStyles(focused bool) table.Styles {
	s := mad.styles
	if focused {
		s.Selected = s.Selected.Background(lipgloss.Color("57"))
	} else {
		s.Selected = s.Selected.Background(lipgloss.Color("240"))
	}
	return s
}

func (mad musicArtistDetail) SelectedAlbum() (string, bool) {
	row := mad.albumsTable.SelectedRow()
	if len(row) < 2 {
		return "", false
	}
	return row[1], true
}

func (mad musicArtistDetail) SelectedTrack() (db.TrackData, bool) {
	if mad.focusedUpper {
		return db.TrackData{}, false
	}
	row := mad.tracksTable.SelectedRow()
	if len(row) == 0 {
		return db.TrackData{}, false
	}
	cursor := mad.tracksTable.Cursor()
	if cursor >= 0 && cursor < len(mad.tracks) {
		return mad.tracks[cursor], true
	}
	return db.TrackData{}, false
}

func (mad *musicArtistDetail) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := mad.tracksTable.Rows()
	changed := false
	
	for i, t := range mad.tracks {
		var mark string
		switch {
		case t.Path == currentPath && isPaused:
			mark = "■"
		case t.Path == currentPath:
			mark = "▶"
		case mad.marked[i]:
			mark = "●"
		}
		
		if i < len(rows) && rows[i][0] != mark {
			rows[i][0] = mark
			changed = true
		}
	}
	
	if changed {
		mad.tracksTable.SetRows(rows)
	}
}

func (mad *musicArtistDetail) MarkedTracks() []string {
	var paths []string
	for i, marked := range mad.marked {
		if marked && i >= 0 && i < len(mad.tracks) {
			paths = append(paths, mad.tracks[i].Path)
		}
	}
	return paths
}

func (mad *musicArtistDetail) HasMarks() bool {
	for _, m := range mad.marked {
		if m {
			return true
		}
	}
	return false
}

func (mad *musicArtistDetail) ClearMarks() {
	mad.marked = make(map[int]bool)
}
