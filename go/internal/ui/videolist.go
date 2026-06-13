package ui

import (
	"fmt"
	"kitvc/internal/db"
	"log"

	"github.com/evertras/bubble-table/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
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
	cols, w := vl.buildColumns(width)

	rows := make([]table.Row, len(videos))
	for i, v := range videos {
		rows[i] = vl.videoRow(v, "", w)
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

func (vl videoList) buildColumns(width int) ([]table.Column, map[string]int) {
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
		{videoColFilename, "Filename", 40, 0, 1, true},
	}
	dividers := len(specs) - 1
	colWidth := width - dividers - 2
	if colWidth < 0 {
		colWidth = 0
	}
	w := calcColWidths(specs, colWidth)

	log.Printf("[DEBUGLOG] buildColumns width=%d, dividers=%d, colWidth=%d", width, dividers, colWidth)
	for _, spec := range specs {
		log.Printf("[DEBUGLOG] Spec: key=%s, min=%d, max=%d, flex=%d", spec.key, spec.min, spec.max, spec.flex)
	}
	for k, val := range w {
		log.Printf("[DEBUGLOG] Width for %s = %d", k, val)
	}
	cols := []table.Column{
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
	return cols, w
}

func limitStr(s string, max int) string {
	return runewidth.Truncate(s, max, "~")
}

func (vl videoList) videoRow(v db.VideoData, mark string, w map[string]int) table.Row {
	return table.NewRow(table.RowData{
		videoColMark:     mark,
		videoColType:     limitStr(v.Type, w[videoColType]),
		videoColCategory: limitStr(v.Category, w[videoColCategory]),
		videoColSubCat:   limitStr(v.Subcategory, w[videoColSubCat]),
		videoColSeries:   limitStr(v.Series, w[videoColSeries]),
		videoColSeason:   fmt.Sprintf("%d", v.Season),
		videoColEpisode:  fmt.Sprintf("%d", v.Episode),
		videoColTitle:    limitStr(v.Title, w[videoColTitle]),
		videoColDate:     limitStr(v.AirDate, w[videoColDate]),
		videoColDuration: formatDuration(v.Duration),
		videoColFilename: limitStr(v.Filename, w[videoColFilename]),
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
	cols, colWidths := vl.buildColumns(w)
	
	currentRows := vl.table.GetVisibleRows()
	rows := make([]table.Row, len(vl.videos))
	for i, v := range vl.videos {
		mark := ""
		if i < len(currentRows) {
			if m, ok := currentRows[i].Data[videoColMark].(string); ok {
				mark = m
			}
		}
		rows[i] = vl.videoRow(v, mark, colWidths)
	}

	vl.table = vl.table.
		WithColumns(cols).
		WithRows(rows).
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

func (vl *videoList) MarkedVideos() []db.VideoData {
	var videos []db.VideoData
	for i, marked := range vl.marked {
		if marked && i >= 0 && i < len(vl.videos) {
			videos = append(videos, vl.videos[i])
		}
	}
	return videos
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
