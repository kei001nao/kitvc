package ui

import (
	"fmt"
	"kitvc/internal/db"
)

var videoEditLabels = []string{
	"Type", "Category", "SubCategory", "Series",
	"Season", "Episode", "Title", "Date",
	"Genres", "Synopsis",
}

var videoEditFieldNames = []string{
	"type", "category", "subcategory", "series",
	"season", "episode", "title", "air_date",
	"genres", "synopsis",
}

func videoEditInitialValues(v db.VideoData) []string {
	return []string{
		v.Type,
		v.Category,
		v.Subcategory,
		v.Series,
		fmt.Sprintf("%d", v.Season),
		fmt.Sprintf("%d", v.Episode),
		v.Title,
		v.AirDate,
		"",
		"",
	}
}

func videoBatchEditInitialValues() []string {
	return make([]string, len(videoEditLabels))
}
