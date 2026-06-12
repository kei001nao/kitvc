package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

type VideoData struct {
	Path        string
	Filename    string
	Size        int64
	Duration    int
	Year        int
	MTime       float64
	Type        string
	Category    string
	Subcategory string
	Series      string
	Season      int
	Episode     int
	Title       string
	AirDate     string
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
			filename = excluded.filename,
			size = excluded.size,
			duration = excluded.duration,
			year = excluded.year,
			mtime = excluded.mtime,
			type = excluded.type,
			category = excluded.category,
			series = excluded.series,
			season = excluded.season,
			episode = excluded.episode,
			title = excluded.title
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
			COALESCE(series, ''), COALESCE(season, 0), COALESCE(episode, 0), 
			COALESCE(title, ''), COALESCE(air_date, '')
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
			COALESCE(v.title, ''), COALESCE(v.air_date, '')
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
			COALESCE(series, ''), COALESCE(season, 0), COALESCE(episode, 0), 
			COALESCE(title, ''), COALESCE(air_date, '')
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

var videoAllowedFields = map[string]bool{
	"type": true, "category": true, "subcategory": true,
	"series": true, "season": true, "episode": true,
	"title": true, "air_date": true, "genres": true,
	"synopsis": true, "year": true, "cast": true,
	"director": true, "series_overview": true, "episode_overview": true,
	"tmdb_id": true, "poster_path": true,
}

func UpdateVideoField(path, field, value string) error {
	if !videoAllowedFields[field] {
		return fmt.Errorf("field %q is not allowed for update", field)
	}
	query := fmt.Sprintf("UPDATE video_files SET %s = ? WHERE path = ?", field)
	_, err := db.Exec(query, value, path)
	return err
}

func GetContinueWatchingVideos() ([]VideoData, error) {
	rows, err := db.Query(`
		SELECT 
			path, filename, size, duration, year, mtime,
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(subcategory, ''),
			COALESCE(series, ''), COALESCE(season, 0), COALESCE(episode, 0), 
			COALESCE(title, ''), COALESCE(air_date, '')
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
			COALESCE(series, ''), COALESCE(season, 0), COALESCE(episode, 0), 
			COALESCE(title, ''), COALESCE(air_date, '')
		FROM video_files
		ORDER BY created_at DESC
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
			COALESCE(series, ''), COALESCE(season, 0), COALESCE(episode, 0), 
			COALESCE(title, ''), COALESCE(air_date, '')
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
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}
