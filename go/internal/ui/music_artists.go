package ui

import (
	"github.com/evertras/bubble-table/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	artistColName = "artist"
)

type musicArtists struct {
	table table.Model
}

func newMusicArtists(width, height int, artists []string) musicArtists {
	rows := make([]table.Row, len(artists))
	for i, artist := range artists {
		rows[i] = table.NewRow(table.RowData{
			artistColName: artist,
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

	cols := []table.Column{
		table.NewColumn(artistColName, "Artist", 40).WithStyle(lipgloss.NewStyle().Align(lipgloss.Left)),
	}

	t := table.New(cols).
		WithRows(rows).
		WithTargetHeight(height - 4).
		WithMaxTotalWidth(width).
		HeaderStyle(headerStyle).
		HighlightStyle(highlightStyle).
		Focused(true).
		WithBorderForeground(lipgloss.Color("240")).
		Border(emptyBorder)

	return musicArtists{table: t}
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
	ma.table = ma.table.
		WithMaxTotalWidth(w).
		WithTargetHeight(h - 4)
}

func (ma *musicArtists) SetFocus(f bool) {
	ma.table = ma.table.Focused(f)
}

func (ma musicArtists) SelectedArtist() string {
	row := ma.table.HighlightedRow()
	if val, ok := row.Data[artistColName].(string); ok {
		return val
	}
	return ""
}
