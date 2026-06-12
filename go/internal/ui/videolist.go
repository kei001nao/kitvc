package ui

import (
	"fmt"
	"kitvc/internal/db"

	"github.com/evertras/bubble-table/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	videoColMark     = "mark"
	videoColType     = "type"
	videoColCategory = "category"
	videoColSubCat   = "subcategory"
	videoColSeries   = "series"
	videoColSeason   = "season"
	videoColEpisode  = "episode"
	videoColTitle    = "title"
	videoColDate     = "date"
	videoColDuration = "duration"
	videoColFilename = "filename"
)

type videoList struct {
	table  table.Model
	videos []db.VideoData
	marked map[int]bool
}

func newVideoList(width, height int) videoList {
	videos, _ := db.GetVideos()
	return newVideoListFromVideos(width, height, videos)
}

func newVideoListFromVideos(width, height int, videos []db.VideoData) videoList {
	tl := videoList{videos: videos, marked: make(map[int]bool)}
	tl.table = tl.buildTable(width, height, videos)
	return tl
}

func (vl videoList) buildTable(width, height int, videos []db.VideoData) table.Model {
	cols := vl.buildColumns(width)

	rows := make([]table.Row, len(videos))
	for i, v := range videos {
		rows[i] = vl.videoRow(v, "")
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
		WithHorizontalFreezeColumnCount(4).
		HeaderStyle(headerStyle).
		HighlightStyle(highlightStyle).
		Focused(true).
		WithBorderForeground(lipgloss.Color("240")).
		Border(emptyBorder)

	return t
}

func (vl videoList) buildColumns(width int) []table.Column {
	leftAlign := lipgloss.NewStyle().Align(lipgloss.Left)
	specs := []colSpec{
		{videoColMark, "M", 1, 1, 0, false},
		{videoColType, "Type", 7, 7, 0, true},
		{videoColCategory, "Category", 8, 8, 0, true},
		{videoColSubCat, "SubCat", 8, 8, 0, true},
		{videoColSeries, "Series", 10, 28, 2, true},
		{videoColSeason, "S", 2, 2, 0, false},
		{videoColEpisode, "E", 2, 2, 0, false},
		{videoColTitle, "Title", 12, 28, 2, true},
		{videoColDate, "Date", 10, 10, 0, true},
		{videoColDuration, "Duration", 8, 8, 0, false},
		{videoColFilename, "Filename", 40, 58, 1, true},
	}
	dividers := len(specs) - 1
	colWidth := width - dividers
	if colWidth < 0 {
		colWidth = 0
	}
	w := calcColWidths(specs, colWidth)
	return []table.Column{
		table.NewColumn(videoColMark, "M", w[videoColMark]),
		table.NewColumn(videoColType, "Type", w[videoColType]).WithStyle(leftAlign),
		table.NewColumn(videoColCategory, "Category", w[videoColCategory]).WithStyle(leftAlign),
		table.NewColumn(videoColSubCat, "SubCat", w[videoColSubCat]).WithStyle(leftAlign),
		table.NewColumn(videoColSeries, "Series", w[videoColSeries]).WithStyle(leftAlign),
		table.NewColumn(videoColSeason, "S", w[videoColSeason]),
		table.NewColumn(videoColEpisode, "E", w[videoColEpisode]),
		table.NewColumn(videoColTitle, "Title", w[videoColTitle]).WithStyle(leftAlign),
		table.NewColumn(videoColDate, "Date", w[videoColDate]).WithStyle(leftAlign),
		table.NewColumn(videoColDuration, "Duration", w[videoColDuration]),
		table.NewColumn(videoColFilename, "Filename", w[videoColFilename]).WithStyle(leftAlign),
	}
}

func limitStr(s string, max int) string {
	if len([]rune(s)) > max {
		return string([]rune(s)[:max-1]) + "~"
	}
	return s
}

func (vl videoList) videoRow(v db.VideoData, mark string) table.Row {
	return table.NewRow(table.RowData{
		videoColMark:     mark,
		videoColType:     limitStr(v.Type, 7),
		videoColCategory: limitStr(v.Category, 8),
		videoColSubCat:   limitStr(v.Subcategory, 8),
		videoColSeries:   limitStr(v.Series, 28),
		videoColSeason:   fmt.Sprintf("%d", v.Season),
		videoColEpisode:  fmt.Sprintf("%d", v.Episode),
		videoColTitle:    limitStr(v.Title, 28),
		videoColDate:     limitStr(v.AirDate, 10),
		videoColDuration: formatDuration(v.Duration),
		videoColFilename: v.Filename,
	})
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
	cols := vl.buildColumns(w)
	vl.table = vl.table.
		WithColumns(cols).
		WithMaxTotalWidth(w).
		WithTargetHeight(h - 4)
}

func (vl *videoList) SetFocus(f bool) {
	vl.table = vl.table.Focused(f)
}

func (vl *videoList) UpdatePlaybackStatus(currentPath string, isPaused bool) {
	rows := vl.table.GetVisibleRows()
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

		if i < len(rows) {
			currentMark, _ := rows[i].Data[videoColMark].(string)
			if currentMark != mark {
				rows[i].Data[videoColMark] = mark
				changed = true
			}
		}
	}

	if changed {
		vl.table = vl.table.WithRows(rows)
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
