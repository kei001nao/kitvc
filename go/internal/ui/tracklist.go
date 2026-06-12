package ui

import (
	"strconv"
	"kitvc/internal/db"

	"github.com/evertras/bubble-table/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var emptyBorder = table.Border{
	Top:    "", Left: "", Right: "", Bottom: "",
	TopRight: "", TopLeft: "", BottomRight: "", BottomLeft: "",
	TopJunction: "", LeftJunction: "", RightJunction: "", BottomJunction: "",
	InnerJunction: "", InnerDivider: " ",
}

type colSpec struct {
	key   string
	title string
	min   int
	max   int
	flex  int
	left  bool
}

func calcColWidths(specs []colSpec, totalWidth int) map[string]int {
	minTotal := 0
	flexTotal := 0
	for _, s := range specs {
		minTotal += s.min
		flexTotal += s.flex
	}

	extra := totalWidth - minTotal
	if extra < 0 {
		extra = 0
	}

	widths := make(map[string]int, len(specs))
	remaining := extra
	for _, s := range specs {
		if flexTotal > 0 && s.flex > 0 {
			w := s.min + extra*s.flex/flexTotal
			if s.max > 0 && w > s.max {
				w = s.max
			}
			widths[s.key] = w
			remaining -= (w - s.min)
		} else {
			widths[s.key] = s.min
		}
	}
	for remaining > 0 {
		added := false
		for _, s := range specs {
			if remaining <= 0 {
				break
			}
			if flexTotal > 0 && s.flex > 0 {
				w := widths[s.key] + 1
				if s.max <= 0 || w <= s.max {
					widths[s.key] = w
					remaining--
					added = true
				}
			}
		}
		if !added {
			break
		}
	}
	return widths
}

const (
	trackColMark     = "mark"
	trackColNum      = "num"
	trackColAlbNum   = "albnum"
	trackColTitle    = "title"
	trackColArtist   = "artist"
	trackColAlbum    = "album"
	trackColDuration = "duration"
)

type trackList struct {
	table  table.Model
	tracks []db.TrackData
	marked map[int]bool
}

func newTrackList(width, height int, artist, albumTitle string) trackList {
	tracks, _ := db.GetMusicTracks(artist, albumTitle)
	return newTrackListFromTracks(width, height, tracks)
}

func newTrackListFromTracks(width, height int, tracks []db.TrackData) trackList {
	tl := trackList{tracks: tracks, marked: make(map[int]bool)}
	tl.table = tl.buildTable(width, height, tracks)
	return tl
}

func (tl trackList) buildTable(width, height int, tracks []db.TrackData) table.Model {
	cols := tl.buildColumns(width)

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

	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(false).
		Foreground(lipgloss.Color("5")).
		Bold(true)

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))

	t := table.New(cols).
		WithRows(rows).
		WithTargetHeight(height - 4).
		WithMaxTotalWidth(width).
		WithHorizontalFreezeColumnCount(3).
		HeaderStyle(headerStyle).
		HighlightStyle(highlightStyle).
		Focused(true).
		WithBorderForeground(lipgloss.Color("240")).
		Border(emptyBorder)

	return t
}

func (tl trackList) buildColumns(width int) []table.Column {
	leftAlign := lipgloss.NewStyle().Align(lipgloss.Left)
	specs := []colSpec{
		{trackColMark, "M", 1, 1, 0, false},
		{trackColNum, "#", 3, 3, 0, false},
		{trackColAlbNum, "Alb#", 4, 4, 0, false},
		{trackColTitle, "Title", 8, 20, 1, true},
		{trackColArtist, "Artist", 6, 16, 1, true},
		{trackColAlbum, "Album", 8, 20, 1, true},
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

func (tl trackList) Update(msg tea.Msg) (trackList, tea.Cmd) {
	var cmd tea.Cmd
	tl.table, cmd = tl.table.Update(msg)
	return tl, cmd
}

func (tl trackList) View() string {
	return tl.table.View()
}

func (tl *trackList) SetSize(w, h int) {
	if w <= 0 {
		w = 1
	}
	cols := tl.buildColumns(w)
	tl.table = tl.table.
		WithColumns(cols).
		WithMaxTotalWidth(w).
		WithTargetHeight(h - 4)
}

func (tl *trackList) SetFocus(f bool) {
	tl.table = tl.table.Focused(f)
}

func (tl *trackList) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := tl.table.GetVisibleRows()
	changed := false

	for i, t := range tl.tracks {
		var mark string
		switch {
		case t.Path == currentPath && isPaused:
			mark = "■"
		case t.Path == currentPath:
			mark = "▶"
		case tl.marked[i]:
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
		tl.table = tl.table.WithRows(rows)
	}
}

func (tl *trackList) ClearMarks() {
	tl.marked = make(map[int]bool)
}

func (tl *trackList) MarkedPaths() []string {
	var paths []string
	for i, marked := range tl.marked {
		if marked && i >= 0 && i < len(tl.tracks) {
			paths = append(paths, tl.tracks[i].Path)
		}
	}
	return paths
}

func (tl trackList) SelectedRow() table.Row {
	return tl.table.HighlightedRow()
}

func (tl trackList) Cursor() int {
	return tl.table.GetHighlightedRowIndex()
}

func (tl *trackList) SetCursor(n int) {
	tl.table = tl.table.WithHighlightedRow(n)
}
