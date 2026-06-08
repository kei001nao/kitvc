package db

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func InitDB(configDir string) error {
	dbPath := filepath.Join(configDir, "library.db")
	
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS music_albums (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			artist TEXT,
			title TEXT,
			release_date TEXT,
			cover_path TEXT,
			mbid TEXT,
			comment TEXT,
			UNIQUE(artist, title)
		)`,
		`CREATE TABLE IF NOT EXISTS music_tracks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT UNIQUE,
			mtime REAL,
			title TEXT,
			artist TEXT,
			album TEXT,
			album_artist TEXT,
			track_num INTEGER,
			disc_num INTEGER,
			genre TEXT,
			bpm REAL,
			duration INTEGER,
			last_pos REAL DEFAULT 0,
			last_played_at REAL,
			created_at REAL DEFAULT (strftime('%s','now')),
			album_id INTEGER,
			FOREIGN KEY(album_id) REFERENCES music_albums(id)
		)`,
		`CREATE TABLE IF NOT EXISTS video_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT UNIQUE,
			mtime REAL,
			filename TEXT,
			size INTEGER,
			type TEXT,
			category TEXT,
			series TEXT,
			season INTEGER,
			episode INTEGER,
			title TEXT,
			duration INTEGER DEFAULT 0,
			last_pos REAL DEFAULT 0,
			last_played_at REAL,
			created_at REAL DEFAULT (strftime('%s','now')),
			thumbnail_path TEXT,
			synopsis TEXT,
			cast TEXT,
			director TEXT,
			year INTEGER,
			tmdb_id TEXT,
			poster_path TEXT,
			air_date TEXT,
			series_overview TEXT,
			first_air_date TEXT,
			series_poster_path TEXT,
			genres TEXT,
			season_name TEXT,
			season_overview TEXT,
			still_path TEXT,
			episode_overview TEXT,
			local_poster_path TEXT,
			local_series_poster_path TEXT,
			local_still_path TEXT,
			subcategory TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS music_playlists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS music_playlist_tracks (
			playlist_id INTEGER,
			track_path TEXT,
			sort_order INTEGER,
			FOREIGN KEY(playlist_id) REFERENCES music_playlists(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS video_playlists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS video_playlist_files (
			playlist_id INTEGER,
			file_path TEXT,
			sort_order INTEGER,
			FOREIGN KEY(playlist_id) REFERENCES video_playlists(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS video_filters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			conditions_json TEXT,
			sort_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS music_filters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			conditions_json TEXT,
			sort_json TEXT
		)`,
	}

	for _, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	return nil
}

func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}
