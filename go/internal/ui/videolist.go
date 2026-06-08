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
		se := ""
		if v.Season > 0 || v.Episode > 0 {
			se = fmt.Sprintf("S%02dE%02d", v.Season, v.Episode)
		}

		rows = append(rows, table.Row{
			v.Series,
			se,
			v.Title,
			fmt.Sprintf("%d", v.Year),
			formatDuration(v.Duration),
			v.Filename,
		})
	}
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(false).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	tl := videoList{table: t, styles: s}
	tl.SetSize(width, height)
	tl.table.SetRows(rows)
	return tl
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
	
	// Define columns directly to ensure they are always set correctly
	avail := w - 30 // Subtract fixed widths and spacers
	if avail < 20 {
		avail = 20
	}

	cols := []table.Column{
		{Title: "Series", Width: avail * 2 / 10},
		{Title: "S/E", Width: 8},
		{Title: "Title", Width: avail * 5 / 10},
		{Title: "Year", Width: 6},
		{Title: "Duration", Width: 8},
		{Title: "Filename", Width: avail - (avail * 2 / 10) - (avail * 5 / 10)},
	}
	
	// Ensure minimums
	for i := range cols {
		if cols[i].Width < 4 { cols[i].Width = 4 }
	}

	vl.table.SetColumns(cols)
}

func (vl *videoList) SetFocus(f bool) {
	s := vl.styles
	if f {
		vl.table.Focus()
		s.Selected = s.Selected.Background(lipgloss.Color("57"))
	} else {
		vl.table.Blur()
		s.Selected = s.Selected.Background(lipgloss.Color("240"))
	}
	vl.table.SetStyles(s)
	vl.styles = s
}

