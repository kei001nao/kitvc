package db

import (
	"fmt"
)

type VideoData struct {
	Path     string
	Filename string
	Size     int64
	Duration int
	Year     int
	MTime    float64
	Type     string
	Category string
	Series   string
	Season   int
	Episode  int
	Title    string
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
			COALESCE(type, ''), COALESCE(category, ''), COALESCE(series, ''), 
			COALESCE(season, 0), COALESCE(episode, 0), COALESCE(title, '')
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
			&v.Type, &v.Category, &v.Series, &v.Season, &v.Episode, &v.Title,
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
			COALESCE(v.type, ''), COALESCE(v.category, ''), COALESCE(v.series, ''), 
			COALESCE(v.season, 0), COALESCE(v.episode, 0), COALESCE(v.title, '')
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
			&v.Type, &v.Category, &v.Series, &v.Season, &v.Episode, &v.Title,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, v)
	}
	return videos, nil
}
