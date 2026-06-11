package ui

import (
	"fmt"
	"kitvc/internal/db"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type videoList struct {
	table  table.Model
	styles table.Styles
	videos []db.VideoData
	marked map[int]bool
}

func newVideoList(width, height int) videoList {
	videos, _ := db.GetVideos()
	return newVideoListFromVideos(width, height, videos)
}

func newVideoListFromVideos(width, height int, videos []db.VideoData) videoList {
	t := table.New(
		table.WithColumns([]table.Column{{Title: "Initializing...", Width: 10}}), // Dummy column to prevent panic
		table.WithFocused(true),
		table.WithHeight(height-4),
		table.WithWidth(width),
	)

	var rows []table.Row
	for _, v := range videos {
		rows = append(rows, videoRow(v, ""))
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

	tl := videoList{table: t, styles: s, videos: videos, marked: make(map[int]bool)}
	tl.SetSize(width, height)
	tl.table.SetRows(rows)
	return tl
}

func videoRow(v db.VideoData, mark string) table.Row {
	return table.Row{
		mark,           // M
		v.Category,     // Category
		v.Subcategory,  // SubCat
		v.Series,       // Series
		fmt.Sprintf("%d", v.Season),  // S
		fmt.Sprintf("%d", v.Episode), // E
		v.Title,        // Title
		v.AirDate,      // Date
		formatDuration(v.Duration),  // Duration
		v.Filename,     // Filename
	}
}

func (vl videoList) Update(msg tea.Msg) (videoList, tea.Cmd) {
	var cmd tea.Cmd
	vl.table, cmd = vl.table.Update(msg)
	return vl, cmd
}

func (vl videoList) View() string {
	return vl.table.View()
}

func (vl *videoList) SetSize(w, h int) {
	if w <= 0 {
		w = 1
	}
	vl.table.SetWidth(w)
	vl.table.SetHeight(h - 4)

	// Fixed columns: M(3) + Category(8) + SubCat(8) + S(3) + E(3) + Date(10) + Duration(8) = 43
	// Variable columns: Series, Title, Filename share the remaining space
	avail := w - 43
	if avail < 15 {
		avail = 15
	}

	cols := []table.Column{
		{Title: "M", Width: 3},
		{Title: "Category", Width: 8},
		{Title: "SubCat", Width: 8},
		{Title: "Series", Width: avail * 2 / 10},
		{Title: "S", Width: 3},
		{Title: "E", Width: 3},
		{Title: "Title", Width: avail * 3 / 10},
		{Title: "Date", Width: 10},
		{Title: "Duration", Width: 8},
		{Title: "Filename", Width: avail - (avail*2/10) - (avail*3/10)},
	}

	for i := range cols {
		if cols[i].Width < 4 {
			cols[i].Width = 4
		}
	}

	vl.table.SetColumns(cols)
}

func (vl *videoList) SetFocus(f bool) {
	if f {
		vl.table.Focus()
		vl.styles.Selected = vl.styles.Selected.Background(lipgloss.Color("57"))
	} else {
		vl.table.Blur()
		vl.styles.Selected = vl.styles.Selected.Background(lipgloss.Color("240"))
	}
	vl.table.SetStyles(vl.styles)
}

func (vl *videoList) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := vl.table.Rows()
	changed := false

	for i, v := range vl.videos {
		var mark string
		switch {
		case v.Path == currentPath && isPaused:
			mark = "■"
		case v.Path == currentPath:
			mark = "▶"
		case vl.marked[i]:
			mark = "●"
		}

		if i < len(rows) && rows[i][0] != mark {
			rows[i][0] = mark
			changed = true
		}
	}

	if changed {
		vl.table.SetRows(rows)
	}
}

func (vl *videoList) ClearMarks() {
	vl.marked = make(map[int]bool)
}

func (vl *videoList) MarkedPaths() []string {
	var paths []string
	for i, marked := range vl.marked {
		if marked && i >= 0 && i < len(vl.videos) {
			paths = append(paths, vl.videos[i].Path)
		}
	}
	return paths
}
