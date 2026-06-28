package db

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func InitDB(configDir string) error {
	dbPath := filepath.Join(configDir, "library.db")
	
	var err error
	db, err = sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS music_albums (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			artist TEXT,
			title TEXT,
			directory TEXT DEFAULT '',
			release_date TEXT,
			cover_path TEXT,
			mbid TEXT,
			comment TEXT,
			UNIQUE(artist, title, directory)
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
			sample_rate INTEGER DEFAULT 0,
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

	// Migration: add columns for existing databases
	migrations := []string{
		"ALTER TABLE music_albums ADD COLUMN directory TEXT DEFAULT ''",
		"ALTER TABLE music_tracks ADD COLUMN sample_rate INTEGER DEFAULT 0",
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			// Ignore "duplicate column" errors
			if !strings.Contains(err.Error(), "duplicate column") {
				log.Printf("Migration warning (%s): %v", m, err)
			}
		}
	}

	// Migration: recreate music_albums with proper UNIQUE constraint if needed
	migrateAlbumUnique(db)

	return nil
}

func migrateAlbumUnique(db *sql.DB) {
	row := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='music_albums'")
	var sqlStr string
	if err := row.Scan(&sqlStr); err != nil {
		return
	}
	// If old constraint (no directory in UNIQUE), recreate table
	if strings.Contains(sqlStr, "UNIQUE(artist, title)") && !strings.Contains(sqlStr, "UNIQUE(artist, title, directory)") {
		log.Println("Migrating music_albums unique constraint to include directory...")
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Migration failed (begin): %v", err)
			return
		}
		defer tx.Rollback()

		tx.Exec(`CREATE TABLE music_albums_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			artist TEXT,
			title TEXT,
			directory TEXT DEFAULT '',
			release_date TEXT,
			cover_path TEXT,
			mbid TEXT,
			comment TEXT,
			UNIQUE(artist, title, directory)
		)`)
		tx.Exec(`INSERT INTO music_albums_new (id, artist, title, directory, release_date, cover_path, mbid, comment)
			SELECT id, artist, title, '', release_date, cover_path, mbid, comment FROM music_albums`)
		// Restore sequence
		var maxID int64
		tx.QueryRow("SELECT COALESCE(MAX(id), 0) FROM music_albums_new").Scan(&maxID)
		if maxID > 0 {
			tx.Exec(fmt.Sprintf("UPDATE sqlite_sequence SET seq = %d WHERE name = 'music_albums'", maxID))
		}
		tx.Exec("DROP TABLE music_albums")
		tx.Exec("ALTER TABLE music_albums_new RENAME TO music_albums")
		if err := tx.Commit(); err != nil {
			log.Printf("Migration failed (commit): %v", err)
		} else {
			log.Println("music_albums migration completed successfully")
		}
	}
}

func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}
