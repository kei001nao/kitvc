package ui

import (
	"strconv"
	"kitvc/internal/db"

	"github.com/evertras/bubble-table/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	albumColDate  = "date"
	albumColTitle = "album"
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
	marked       map[int]bool
}

func newMusicArtistDetail(width, height int, artist string, albums []db.Album) musicArtistDetail {
	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(false).
		Foreground(lipgloss.Color("5")).
		Bold(true)

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))

	// 1. Albums table (Upper)
	albumRows := make([]table.Row, len(albums))
	for i, album := range albums {
		albumRows[i] = table.NewRow(table.RowData{
			albumColDate:  album.ReleaseDate,
			albumColTitle: album.Title,
		})
	}

	at := table.New(buildAlbumCols(width)).
		WithRows(albumRows).
		WithTargetHeight(5).
		WithMaxTotalWidth(width).
		HeaderStyle(headerStyle).
		HighlightStyle(highlightStyle).
		Focused(true).
		WithBorderForeground(lipgloss.Color("240")).
		Border(emptyBorder)

	// 2. Tracks table (Lower) - Initialized empty
	tt := table.New([]table.Column{
		table.NewColumn("dummy", "Initializing...", 10),
	}).
		WithTargetHeight(height - 12).
		WithMaxTotalWidth(width).
		HeaderStyle(headerStyle).
		HighlightStyle(highlightStyle).
		Focused(false).
		WithBorderForeground(lipgloss.Color("240")).
		Border(emptyBorder)

	mad := musicArtistDetail{
		artist:       artist,
		albums:       albums,
		albumsTable:  at,
		tracksTable:  tt,
		focusedUpper: true,
		width:        width,
		height:       height,
		marked:       make(map[int]bool),
	}

	mad.SetSize(width, height)

	// Load tracks for the first album if any
	if len(albums) > 0 {
		mad.loadTracksForAlbum(albums[0].Title)
	}

	return mad
}

func buildAlbumCols(width int) []table.Column {
	leftAlign := lipgloss.NewStyle().Align(lipgloss.Left)
	specs := []colSpec{
		{albumColTitle, "Album", 30, 50, 1, true},
		{albumColDate, "Date", 10, 10, 0, true},
	}
	dividers := len(specs) - 1
	colWidth := width - dividers - 2
	if colWidth < 0 {
		colWidth = 0
	}
	w := calcColWidths(specs, colWidth)
	return []table.Column{
		table.NewColumn(albumColTitle, "Album", w[albumColTitle]).WithStyle(leftAlign),
		table.NewColumn(albumColDate, "Date", w[albumColDate]).WithStyle(leftAlign),
	}
}

func (mad *musicArtistDetail) loadTracksForAlbum(albumTitle string) {
	tracks, _ := db.GetMusicTracks(mad.artist, albumTitle)
	mad.tracks = tracks
	mad.ClearMarks()

	rows := make([]table.Row, len(tracks))
	for i, t := range tracks {
		rows[i] = table.NewRow(table.RowData{
			trackColMark:     "",
			trackColNum:      strconv.Itoa(i + 1),
			trackColAlbNum:   strconv.Itoa(t.TrackNum),
			trackColTitle:    t.Title,
			trackColArtist:   t.Artist,
			trackColAlbum:    t.Album,
			trackColDuration: formatDuration(t.Duration),
		})
	}
	mad.tracksTable = mad.tracksTable.WithRows(rows)
	if len(rows) > 0 {
		mad.tracksTable = mad.tracksTable.WithHighlightedRow(0)
	}
}

func (mad musicArtistDetail) Update(msg tea.Msg) (musicArtistDetail, tea.Cmd) {
	var cmd tea.Cmd

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if mad.focusedUpper {
				mad.focusedUpper = false
				mad.syncTableFocus()
				return mad, nil
			}
		case "esc":
			if !mad.focusedUpper {
				mad.focusedUpper = true
				mad.syncTableFocus()
				return mad, nil
			}
		}
	}

	if mad.focusedUpper {
		oldCursor := mad.albumsTable.GetHighlightedRowIndex()
		mad.albumsTable, cmd = mad.albumsTable.Update(msg)
		newCursor := mad.albumsTable.GetHighlightedRowIndex()

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

	// Upper Table: 1/3 of available height
	upperHeight := h / 3
	if upperHeight < 3 {
		upperHeight = 3
	}
	mad.albumsTable = mad.albumsTable.
		WithColumns(buildAlbumCols(w)).
		WithMaxTotalWidth(w).
		WithTargetHeight(upperHeight)

	// Lower Table: remaining height minus labels and gaps
	lowerHeight := h - upperHeight - 6
	if lowerHeight < 3 {
		lowerHeight = 3
	}

	mad.tracksTable = mad.tracksTable.
		WithMaxTotalWidth(w).
		WithTargetHeight(lowerHeight).
		WithColumns(buildTrackCols(w))
}

func buildTrackCols(width int) []table.Column {
	leftAlign := lipgloss.NewStyle().Align(lipgloss.Left)
	specs := []colSpec{
		{trackColMark, "M", 1, 1, 0, false},
		{trackColNum, "#", 4, 4, 0, false},
		{trackColAlbNum, "Alb#", 6, 6, 0, false},
		{trackColTitle, "Title", 10, 20, 1, true},
		{trackColArtist, "Artist", 8, 16, 1, true},
		{trackColAlbum, "Album", 10, 20, 1, true},
		{trackColDuration, "Duration", 8, 8, 0, false},
	}
	dividers := len(specs) - 1
	colWidth := width - dividers
	if colWidth < 0 {
		colWidth = 0
	}
	w := calcColWidths(specs, colWidth)
	return []table.Column{
		table.NewColumn(trackColMark, "M", w[trackColMark]),
		table.NewColumn(trackColNum, "#", w[trackColNum]),
		table.NewColumn(trackColAlbNum, "Alb#", w[trackColAlbNum]),
		table.NewColumn(trackColTitle, "Title", w[trackColTitle]).WithStyle(leftAlign),
		table.NewColumn(trackColArtist, "Artist", w[trackColArtist]).WithStyle(leftAlign),
		table.NewColumn(trackColAlbum, "Album", w[trackColAlbum]).WithStyle(leftAlign),
		table.NewColumn(trackColDuration, "Duration", w[trackColDuration]),
	}
}

func (mad *musicArtistDetail) SetFocus(f bool) {
	if f {
		mad.syncTableFocus()
	} else {
		mad.albumsTable = mad.albumsTable.Focused(false)
		mad.tracksTable = mad.tracksTable.Focused(false)
	}
}

func (mad *musicArtistDetail) syncTableFocus() {
	if mad.focusedUpper {
		mad.albumsTable = mad.albumsTable.Focused(true)
		mad.tracksTable = mad.tracksTable.Focused(false)
	} else {
		mad.albumsTable = mad.albumsTable.Focused(false)
		mad.tracksTable = mad.tracksTable.Focused(true)
	}
}

func (mad musicArtistDetail) SelectedAlbum() (string, bool) {
	row := mad.albumsTable.HighlightedRow()
	if val, ok := row.Data[albumColTitle].(string); ok {
		return val, true
	}
	return "", false
}

func (mad musicArtistDetail) SelectedTrack() (db.TrackData, bool) {
	if mad.focusedUpper {
		return db.TrackData{}, false
	}
	cursor := mad.tracksTable.GetHighlightedRowIndex()
	if cursor >= 0 && cursor < len(mad.tracks) {
		return mad.tracks[cursor], true
	}
	return db.TrackData{}, false
}

func (mad *musicArtistDetail) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := mad.tracksTable.GetVisibleRows()
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

		if i < len(rows) {
			currentMark, _ := rows[i].Data[trackColMark].(string)
			if currentMark != mark {
				rows[i].Data[trackColMark] = mark
				changed = true
			}
		}
	}

	if changed {
		mad.tracksTable = mad.tracksTable.WithRows(rows)
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
