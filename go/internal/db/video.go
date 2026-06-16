package db

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type VideoData struct {
	Path              string
	Filename          string
	Size              int64
	Duration          int
	Year              int
	MTime             float64
	Type              string
	Category          string
	Subcategory       string
	Series            string
	Season            int
	Episode           int
	Title             string
	AirDate           string
	Genres            string
	Synopsis          string
	SeriesOverview    string
	EpisodeOverview   string
	ThumbnailPath     string
	PosterPath        string
	LocalPosterPath   string
}

type VideoFilter struct {
	ID             int64
	Name           string
	ConditionsJSON string
	SortJSON       string
}

// FilterField defines a field that can be used in filters
type FilterField struct {
	Label string
	Value string
}

// VideoFilterFields defines the fields that can be used in video filters
var VideoFilterFields = []FilterField{
	{Value: "type", Label: "Type"},
	{Value: "category", Label: "Category"},
	{Value: "subcategory", Label: "SubCategory"},
	{Value: "series", Label: "Series"},
	{Value: "season", Label: "Season"},
	{Value: "episode", Label: "Episode"},
	{Value: "title", Label: "Title"},
	{Value: "year", Label: "Year"},
	{Value: "genres", Label: "Genres"},
	{Value: "duration", Label: "Duration"},
	{Value: "created_at", Label: "CreatedAt"},
}

func UpdateVideoFile(v VideoData) error {
	_, err := db.Exec(`
		INSERT INTO video_files (
			path, filename, size, duration, year, mtime, 
			type, category, series, season, episode, title,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (strftime('%s','now')))
		ON CONFLICT(path) DO UPDATE SET
			mtime = excluded.mtime,
			filename = CASE WHEN filename IS NULL OR filename = '' THEN excluded.filename ELSE filename END,
			size = CASE WHEN size IS NULL THEN excluded.size ELSE size END,
			duration = CASE WHEN duration IS NULL THEN excluded.duration ELSE duration END,
			year = CASE WHEN year IS NULL THEN excluded.year ELSE year END,
			type = CASE WHEN type IS NULL OR type = '' THEN excluded.type ELSE type END,
			category = CASE WHEN category IS NULL OR category = '' THEN excluded.category ELSE category END,
			series = CASE WHEN series IS NULL OR series = '' THEN excluded.series ELSE series END,
			season = CASE WHEN season IS NULL THEN excluded.season ELSE season END,
			episode = CASE WHEN episode IS NULL THEN excluded.episode ELSE episode END,
			title = CASE WHEN title IS NULL OR title = '' THEN excluded.title ELSE title END
	`, v.Path, v.Filename, v.Size, v.Duration, v.Year, v.MTime,
		v.Type, v.Category, v.Series, v.Season, v.Episode, v.Title)

	if err != nil {
		return fmt.Errorf("failed to update video file: %w", err)
	}
	return nil
}

func CreateVideoPlaylist(name string) error {
	_, err := db.Exec("INSERT INTO video_playlists (name) VALUES (?)", name)
	return err
}

func AddFileToVideoPlaylist(playlistID int64, filePath string) error {
	var maxOrder int
	err := db.QueryRow("SELECT COALESCE(MAX(sort_order), 0) FROM video_playlist_files WHERE playlist_id = ?", playlistID).Scan(&maxOrder)
	if err != nil {
		return err
	}

	_, err = db.Exec("INSERT INTO video_playlist_files (playlist_id, file_path, sort_order) VALUES (?, ?, ?)",
		playlistID, filePath, maxOrder+1)
	return err
}

func RemoveFileFromVideoPlaylist(playlistID int64, filePath string) error {
	_, err := db.Exec("DELETE FROM video_playlist_files WHERE playlist_id = ? AND file_path = ?", playlistID, filePath)
	return err
}

func GetVideos() ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(NULLIF(season, ''), 0), COALESCE(NULLIF(episode, ''), 0), 
			COALESCE(title, ''), COALESCE(air_date, ''),
			COALESCE(genres, ''), COALESCE(synopsis, ''),
			COALESCE(series_overview, ''), COALESCE(episode_overview, ''),
			COALESCE(thumbnail_path, ''), COALESCE(poster_path, ''), COALESCE(local_poster_path, '')
		FROM video_files
		ORDER BY series, season, episode, filename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoData
	for rows.Next() {
		var v VideoData
		err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}

type VideoPlaylist struct {
	ID   int64
	Name string
}

func GetVideoPlaylists() ([]VideoPlaylist, error) {
	rows, err := db.Query("SELECT id, name FROM video_playlists ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playlists []VideoPlaylist
	for rows.Next() {
		var p VideoPlaylist
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		playlists = append(playlists, p)
	}
	return playlists, nil
}

func GetVideoPlaylistFiles(playlistID int64) ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			v.path, v.filename, v.size, v.duration, v.year, v.mtime,
			COALESCE(v.type, ''), COALESCE(v.category, ''), COALESCE(v.subcategory, ''),
			COALESCE(v.series, ''), COALESCE(v.season, 0), COALESCE(v.episode, 0), 
			COALESCE(v.title, ''), COALESCE(v.air_date, ''),
			COALESCE(v.genres, ''), COALESCE(v.synopsis, ''),
			COALESCE(v.series_overview, ''), COALESCE(v.episode_overview, ''),
			COALESCE(v.thumbnail_path, ''), COALESCE(v.poster_path, ''), COALESCE(v.local_poster_path, '')
			FROM video_files v
			JOIN video_playlist_files vpf ON v.path = vpf.file_path
			WHERE vpf.playlist_id = ?
			ORDER BY vpf.sort_order
			`, playlistID)
			if err != nil {
			return nil, err
			}
			defer rows.Close()

			var videos []VideoData
			for rows.Next() {
			var v VideoData
			err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
			)
			if err != nil {
			return nil, err
			}
			videos = append(videos, v)
			}
	return videos, nil
}

func GetVideoFilters() ([]VideoFilter, error) {
	rows, err := db.Query("SELECT id, name, COALESCE(conditions_json, ''), COALESCE(sort_json, '') FROM video_filters ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var filters []VideoFilter
	for rows.Next() {
		var f VideoFilter
		if err := rows.Scan(&f.ID, &f.Name, &f.ConditionsJSON, &f.SortJSON); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

func GetVideoFilterByID(id int64) (*VideoFilter, error) {
	var f VideoFilter
	err := db.QueryRow(
		"SELECT id, name, COALESCE(conditions_json, ''), COALESCE(sort_json, '') FROM video_filters WHERE id = ?",
		id,
	).Scan(&f.ID, &f.Name, &f.ConditionsJSON, &f.SortJSON)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func CreateVideoFilter(name, conditionsJSON, sortJSON string) (int64, error) {
	result, err := db.Exec("INSERT INTO video_filters (name, conditions_json, sort_json) VALUES (?, ?, ?)", name, conditionsJSON, sortJSON)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateVideoFilter(id int64, name, conditionsJSON, sortJSON string) error {
	_, err := db.Exec("UPDATE video_filters SET name = ?, conditions_json = ?, sort_json = ? WHERE id = ?", name, conditionsJSON, sortJSON, id)
	return err
}

func DeleteVideoFilter(id int64) error {
	_, err := db.Exec("DELETE FROM video_filters WHERE id = ?", id)
	return err
}

func GetFilteredVideos(conditionsJSON, sortJSON string) ([]VideoData, error) {
	var conditions interface{}
	if conditionsJSON != "" {
		json.Unmarshal([]byte(conditionsJSON), &conditions)
	}

	var sortFields []filterSortItem
	if sortJSON != "" {
		json.Unmarshal([]byte(sortJSON), &sortFields)
	}

	whereClause, params := buildWhereClause(conditions)

	query := `SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(NULLIF(season, ''), 0), COALESCE(NULLIF(episode, ''), 0), 
			COALESCE(title, ''), COALESCE(air_date, ''),
			COALESCE(genres, ''), COALESCE(synopsis, ''),
			COALESCE(series_overview, ''), COALESCE(episode_overview, ''),
			COALESCE(thumbnail_path, ''), COALESCE(poster_path, ''), COALESCE(local_poster_path, '')
		FROM video_files`
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	if len(sortFields) > 0 {
		var orderParts []string
		for _, item := range sortFields {
			if len(item) >= 1 {
				field := fmt.Sprintf("%v", item[0])
				if !isSafeFieldName(field) {
					continue
				}
				direction := "ASC"
				if len(item) >= 2 {
					dir := fmt.Sprintf("%v", item[1])
					if strings.EqualFold(dir, "DESC") {
						direction = "DESC"
					}
				}
				orderParts = append(orderParts, field+" COLLATE NOCASE "+direction)
			}
		}
		if len(orderParts) > 0 {
			query += " ORDER BY " + strings.Join(orderParts, ", ")
		}
	} else {
		query += " ORDER BY series, season, episode, filename"
	}

	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoData
	for rows.Next() {
		var v VideoData
		err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}

func UpdateVideoLastPos(path string, lastPos float64) error {
	_, err := db.Exec(`
		UPDATE video_files 
		SET last_pos = ?, last_played_at = strftime('%s','now')
		WHERE path = ?
	`, lastPos, path)
	return err
}

func ClearVideoLastPos(path string) error {
	_, err := db.Exec(`
		UPDATE video_files 
		SET last_pos = 0
		WHERE path = ?
	`, path)
	return err
}

func GetVideoPosterPath(path string) string {
	if path == "" {
		return ""
	}
	var localPosterPath, posterPath string
	err := db.QueryRow(`
		SELECT COALESCE(local_poster_path, ''), COALESCE(poster_path, '')
		FROM video_files WHERE path = ?
	`, path).Scan(&localPosterPath, &posterPath)
	if err != nil {
		return ""
	}
	if localPosterPath != "" {
		if _, err := os.Stat(localPosterPath); err == nil {
			return localPosterPath
		}
	}
	if posterPath != "" {
		if strings.HasPrefix(posterPath, "/") {
			if _, err := os.Stat(posterPath); err == nil {
				return posterPath
			}
		}
	}
	return ""
}

var videoAllowedFields = map[string]bool{
	"type": true, "category": true, "subcategory": true,
	"series": true, "season": true, "episode": true,
	"title": true, "air_date": true, "genres": true,
	"synopsis": true, "year": true, "cast": true,
	"director": true, "series_overview": true, "episode_overview": true,
	"tmdb_id": true, "poster_path": true, "local_poster_path": true,
}

func UpdateVideoField(path, field, value string) error {
	if !videoAllowedFields[field] {
		return fmt.Errorf("field %q is not allowed for update", field)
	}
	query := fmt.Sprintf("UPDATE video_files SET %s = ? WHERE path = ?", field)
	_, err := db.Exec(query, value, path)
	return err
}

// UpdateVideoFieldIfEmpty updates a field only when the existing value is NULL or empty/zero.
func UpdateVideoFieldIfEmpty(path, field, value string) error {
	if !videoAllowedFields[field] {
		return fmt.Errorf("field %q is not allowed for update", field)
	}
	switch field {
	case "season", "episode", "year":
		_, err := db.Exec(fmt.Sprintf(
			"UPDATE video_files SET %s = ? WHERE path = ? AND (%s IS NULL OR %s = 0)",
			field, field, field,
		), value, path)
		return err
	default:
		_, err := db.Exec(fmt.Sprintf(
			"UPDATE video_files SET %s = ? WHERE path = ? AND (%s IS NULL OR %s = '')",
			field, field, field,
		), value, path)
		return err
	}
}

func GetContinueWatchingVideos() ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(NULLIF(season, ''), 0), COALESCE(NULLIF(episode, ''), 0), 
			COALESCE(title, ''), COALESCE(air_date, ''),
			COALESCE(genres, ''), COALESCE(synopsis, ''),
			COALESCE(series_overview, ''), COALESCE(episode_overview, ''),
			COALESCE(thumbnail_path, ''), COALESCE(poster_path, ''), COALESCE(local_poster_path, '')
		FROM video_files
		WHERE last_pos > 0
		ORDER BY last_played_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoData
	for rows.Next() {
		var v VideoData
		err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}

func GetRecentlyAddedVideos() ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(NULLIF(season, ''), 0), COALESCE(NULLIF(episode, ''), 0), 
			COALESCE(title, ''), COALESCE(air_date, ''),
			COALESCE(genres, ''), COALESCE(synopsis, ''),
			COALESCE(series_overview, ''), COALESCE(episode_overview, ''),
			COALESCE(thumbnail_path, ''), COALESCE(poster_path, ''), COALESCE(local_poster_path, '')
		FROM video_files
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoData
	for rows.Next() {
		var v VideoData
		err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}

func GetUnhealthyVideos() ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(NULLIF(season, ''), 0), COALESCE(NULLIF(episode, ''), 0), 
			COALESCE(title, ''), COALESCE(air_date, ''),
			COALESCE(genres, ''), COALESCE(synopsis, ''),
			COALESCE(series_overview, ''), COALESCE(episode_overview, ''),
			COALESCE(thumbnail_path, ''), COALESCE(poster_path, ''), COALESCE(local_poster_path, '')
		FROM video_files
		WHERE synopsis IS NULL OR year IS NULL
		ORDER BY category, series, season, episode, title
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoData
	for rows.Next() {
		var v VideoData
		err := rows.Scan(
			&v.Path, &v.Filename, &v.Size, &v.Duration, &v.Year, &v.MTime,
			&v.Type, &v.Category, &v.Subcategory, &v.Series, &v.Season, &v.Episode,
			&v.Title, &v.AirDate,
			&v.Genres, &v.Synopsis, &v.SeriesOverview, &v.EpisodeOverview,
			&v.ThumbnailPath, &v.PosterPath, &v.LocalPosterPath,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}
