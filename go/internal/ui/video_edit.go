package ui

import (
	"fmt"
	"kitvc/internal/db"
)

var videoEditLabels = []string{
	"Type", "Category", "SubCategory", "Genres", "Title", "Series",
	"Season", "Episode", "Date",
	"Series Overview", "Synopsis", "Episode Overview",
}

var videoEditFieldNames = []string{
	"type", "category", "subcategory", "genres", "title", "series",
	"season", "episode", "air_date",
	"series_overview", "synopsis", "episode_overview",
}

var videoEditFieldKinds = []videoEditFieldKind{
	videoFieldSelect,   // Type
	videoFieldInput,    // Category
	videoFieldInput,    // SubCategory
	videoFieldInput,    // Genres
	videoFieldInput,    // Title
	videoFieldInput,    // Series
	videoFieldInput,    // Season
	videoFieldInput,    // Episode
	videoFieldInput,    // Date
	videoFieldTextArea, // Series Overview
	videoFieldTextArea, // Synopsis
	videoFieldTextArea, // Episode Overview
}

var videoEditOptions = [][]string{
	videoTypeOptions,  // Type
	nil,               // Category
	nil,               // SubCategory
	nil,               // Genres
	nil,               // Title
	nil,               // Series
	nil,               // Season
	nil,               // Episode
	nil,               // Date
	nil,               // Series Overview
	nil,               // Synopsis
	nil,               // Episode Overview
}

func videoEditInitialValues(v db.VideoData) []string {
	return []string{
		v.Type,
		v.Category,
		v.Subcategory,
		v.Genres,
		v.Title,
		v.Series,
		fmt.Sprintf("%d", v.Season),
		fmt.Sprintf("%d", v.Episode),
		v.AirDate,
		v.SeriesOverview,
		v.Synopsis,
		v.EpisodeOverview,
	}
}

func videoBatchEditInitialValues(videos []db.VideoData) []string {
	if len(videos) == 0 {
		return make([]string, len(videoEditLabels))
	}

	res := make([]string, len(videoEditLabels))
	first := videoEditInitialValues(videos[0])

	for i := range videoEditLabels {
		common := true
		val := first[i]
		for j := 1; j < len(videos); j++ {
			current := videoEditInitialValues(videos[j])
			if current[i] != val {
				common = false
				break
			}
		}
		if common {
			res[i] = val
		}
	}
	return res
}
