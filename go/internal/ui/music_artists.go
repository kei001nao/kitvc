package ui

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type musicArtists struct {
	table  table.Model
	styles table.Styles
}

func newMusicArtists(width, height int, artists []string) musicArtists {
	t := table.New(
		table.WithColumns([]table.Column{{Title: "Artist", Width: width - 2}}),
		table.WithFocused(true),
		table.WithHeight(height - 4),
		table.WithWidth(width),
	)

	var rows []table.Row
	for _, artist := range artists {
		rows = append(rows, table.Row{artist})
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

	ma := musicArtists{table: t, styles: s}
	ma.SetSize(width, height)
	ma.table.SetRows(rows)
	return ma
}

func (ma musicArtists) Update(msg tea.Msg) (musicArtists, tea.Cmd) {
	var cmd tea.Cmd
	ma.table, cmd = ma.table.Update(msg)
	return ma, cmd
}

func (ma musicArtists) View() string {
	return ma.table.View()
}

func (ma *musicArtists) SetSize(w, h int) {
	if w <= 0 {
		w = 1
	}
	ma.table.SetWidth(w)
	ma.table.SetHeight(h - 4)

	cols := []table.Column{
		{Title: "Artist", Width: w - 2},
	}
	ma.table.SetColumns(cols)
}

func (ma *musicArtists) SetFocus(f bool) {
	if f {
		ma.table.Focus()
		ma.styles.Selected = ma.styles.Selected.Background(lipgloss.Color("57"))
	} else {
		ma.table.Blur()
		ma.styles.Selected = ma.styles.Selected.Background(lipgloss.Color("240"))
	}
	ma.table.SetStyles(ma.styles)
}

func (ma musicArtists) SelectedArtist() string {
	row := ma.table.SelectedRow()
	if len(row) > 0 {
		return row[0]
	}
	return ""
}
