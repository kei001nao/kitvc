package db

import (
	"database/sql"
	"fmt"
)

type TrackData struct {
	Path        string
	MTime       float64
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNum    int
	DiscNum     int
	Genre       string
	Duration    int
}

type Album struct {
	ID     int64
	Artist string
	Title  string
}

func UpdateMusicTrack(t TrackData) error {
	// 1. Ensure Album exists
	var albumID int64
	err := db.QueryRow("SELECT id FROM music_albums WHERE artist = ? AND title = ?", t.Artist, t.Album).Scan(&albumID)
	if err == sql.ErrNoRows {
		res, err := db.Exec("INSERT INTO music_albums (artist, title) VALUES (?, ?)", t.Artist, t.Album)
		if err != nil {
			return fmt.Errorf("failed to insert album: %w", err)
		}
		albumID, _ = res.LastInsertId()
	} else if err != nil {
		return fmt.Errorf("failed to query album: %w", err)
	}

	// 2. Insert or replace track
	_, err = db.Exec(`
		INSERT OR REPLACE INTO music_tracks (
			path, mtime, title, artist, album, album_artist, 
			track_num, disc_num, genre, duration, album_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.Path, t.MTime, t.Title, t.Artist, t.Album, t.AlbumArtist,
		t.TrackNum, t.DiscNum, t.Genre, t.Duration, albumID)

	if err != nil {
		return fmt.Errorf("failed to insert track: %w", err)
	}

	return nil
}

func CreateMusicPlaylist(name string) error {
	_, err := db.Exec("INSERT INTO music_playlists (name) VALUES (?)", name)
	return err
}

func AddTrackToMusicPlaylist(playlistID int64, trackPath string) error {
	var maxOrder int
	err := db.QueryRow("SELECT COALESCE(MAX(sort_order), 0) FROM music_playlist_tracks WHERE playlist_id = ?", playlistID).Scan(&maxOrder)
	if err != nil {
		return err
	}

	_, err = db.Exec("INSERT INTO music_playlist_tracks (playlist_id, track_path, sort_order) VALUES (?, ?, ?)",
		playlistID, trackPath, maxOrder+1)
	return err
}

func RemoveTrackFromMusicPlaylist(playlistID int64, trackPath string) error {
	_, err := db.Exec("DELETE FROM music_playlist_tracks WHERE playlist_id = ? AND track_path = ?", playlistID, trackPath)
	return err
}

func GetMusicTracks(artist, albumTitle string) ([]TrackData, error) {
	query := "SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration FROM music_tracks"
	var args []interface{}

	if artist != "" && albumTitle != "" {
		query += " WHERE artist = ? AND album = ?"
		args = append(args, artist, albumTitle)
	} else if artist != "" {
		query += " WHERE artist = ?"
		args = append(args, artist)
	}
	query += " ORDER BY album, disc_num, track_num"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []TrackData
	for rows.Next() {
		var t TrackData
		err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func GetMusicArtistsAndAlbums() ([]string, map[string][]Album, error) {
	rows, err := db.Query("SELECT id, artist, title FROM music_albums ORDER BY artist, title")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	artists := []string{}
	artistMap := make(map[string]bool)
	albums := make(map[string][]Album)

	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Artist, &a.Title); err != nil {
			return nil, nil, err
		}
		if !artistMap[a.Artist] {
			artists = append(artists, a.Artist)
			artistMap[a.Artist] = true
		}
		albums[a.Artist] = append(albums[a.Artist], a)
	}
	return artists, albums, nil
}

type Playlist struct {
	ID   int64
	Name string
}

func GetMusicPlaylists() ([]Playlist, error) {
	rows, err := db.Query("SELECT id, name FROM music_playlists ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playlists []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		playlists = append(playlists, p)
	}
	return playlists, nil
}

func GetMusicPlaylistTracks(playlistID int64) ([]TrackData, error) {
	rows, err := db.Query(`
		SELECT 
			t.path, t.mtime, t.title, t.artist, t.album, t.album_artist, 
			t.track_num, t.disc_num, t.genre, t.duration
		FROM music_tracks t
		JOIN music_playlist_tracks pt ON t.path = pt.track_path
		WHERE pt.playlist_id = ?
		ORDER BY pt.sort_order
	`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []TrackData
	for rows.Next() {
		var t TrackData
		err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}
