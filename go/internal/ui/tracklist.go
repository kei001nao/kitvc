package ui

import (
	"strconv"
	"kitvc/internal/db"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type trackList struct {
	table  table.Model
	styles table.Styles
	tracks []db.TrackData
	marked map[int]bool
}

func newTrackList(width, height int, artist, albumTitle string) trackList {
	tracks, _ := db.GetMusicTracks(artist, albumTitle)
	return newTrackListFromTracks(width, height, tracks)
}

func newTrackListFromTracks(width, height int, tracks []db.TrackData) trackList {
	t := table.New(
		table.WithColumns([]table.Column{{Title: "Initializing...", Width: 10}}), // Dummy column to prevent panic
		table.WithFocused(true),
		table.WithHeight(height-4),
		table.WithWidth(width),
	)

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
	t.SetStyles(s)

	tl := trackList{table: t, styles: s, tracks: tracks, marked: make(map[int]bool)}
	tl.SetSize(width, height)
	tl.table.SetRows(rows)
	return tl
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
	tl.table.SetWidth(w)
	tl.table.SetHeight(h - 4)
	
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
		if cols[i].Width < 4 { cols[i].Width = 4 }
	}

	tl.table.SetColumns(cols)
}

func (tl *trackList) SetFocus(f bool) {
	if f {
		tl.table.Focus()
		tl.styles.Selected = tl.styles.Selected.Background(lipgloss.Color("57"))
	} else {
		tl.table.Blur()
		tl.styles.Selected = tl.styles.Selected.Background(lipgloss.Color("240"))
	}
	tl.table.SetStyles(tl.styles)
}

func (tl *trackList) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := tl.table.Rows()
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
		
		if i < len(rows) && rows[i][0] != mark {
			rows[i][0] = mark
			changed = true
		}
	}
	
	if changed {
		tl.table.SetRows(rows)
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
