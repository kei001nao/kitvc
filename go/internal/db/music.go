package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"golang.org/x/text/unicode/norm"
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
	SampleRate  int
}

type Album struct {
	ID          int64
	Artist      string
	Title       string
	Directory   string
	ReleaseDate string
}

func normalizeString(s string) string {
	return norm.NFKC.String(s)
}

func getBaseAlbumName(album string) string {
	for _, sep := range []string{" : ", ": ", " (", " [", " ~"} {
		if idx := strings.Index(album, sep); idx > 0 {
			return strings.TrimSpace(album[:idx])
		}
	}
	return album
}

func UpdateMusicTrack(t TrackData, force bool) error {
	// Normalize strings to avoid duplicates due to full-width/half-width differences
	t.Artist = normalizeString(t.Artist)
	t.Album = normalizeString(t.Album)
	t.Title = normalizeString(t.Title)

	dir := filepath.Dir(t.Path)

	var albumID int64
	var existingTitle string
	var existingDir string
	var err error

	// 1. If not forcing, check if track already exists and has an album_id
	if !force {
		var exAlbumID int64
		var exArtist, exAlbum, exTitle string
		err = db.QueryRow("SELECT album_id, artist, album, title FROM music_tracks WHERE path = ?", t.Path).Scan(&exAlbumID, &exArtist, &exAlbum, &exTitle)
		if err == nil && exAlbumID != 0 {
			albumID = exAlbumID
			if t.Artist == "" {
				t.Artist = exArtist
			}
			if t.Album == "" {
				t.Album = exAlbum
			}
			if t.Title == "" {
				t.Title = exTitle
			}
			goto updateTrack
		}
	}

	// 2. Ensure Album exists - match by artist + album + directory
	err = db.QueryRow("SELECT id, title, directory FROM music_albums WHERE artist = ? AND title = ? AND directory = ?", t.Artist, t.Album, dir).Scan(&albumID, &existingTitle, &existingDir)
	if err == sql.ErrNoRows {
		// Try flexible matching in Go (same artist + same directory)
		rows, errQuery := db.Query("SELECT id, title, directory FROM music_albums WHERE artist = ?", t.Artist)
		if errQuery == nil {
			targetNorm := t.Album
			targetBase := getBaseAlbumName(targetNorm)

			for rows.Next() {
				var id int64
				var title, albumDir string
				if errScan := rows.Scan(&id, &title, &albumDir); errScan != nil {
					continue
				}

				// Must match directory
				if albumDir != dir {
					continue
				}

				dbNorm := normalizeString(title)
				if dbNorm == targetNorm {
					albumID = id
					existingTitle = title
					existingDir = albumDir
					break
				}

				dbBase := getBaseAlbumName(dbNorm)
				if dbBase == targetBase || dbBase == targetNorm || dbNorm == targetBase {
					albumID = id
					existingTitle = title
					existingDir = albumDir
					break
				}
			}
			rows.Close()
		}

		if albumID != 0 {
			// Found via flexible matching
			t.Album = existingTitle
			err = nil
		} else {
			// Still not found, create new one with directory
			res, errExec := db.Exec("INSERT INTO music_albums (artist, title, directory) VALUES (?, ?, ?)", t.Artist, t.Album, dir)
			if errExec != nil {
				return fmt.Errorf("failed to insert album: %w", errExec)
			}
			albumID, _ = res.LastInsertId()
			err = nil
		}
	} else if err != nil {
		return fmt.Errorf("failed to query album: %w", err)
	} else {
		// Found exact match, use the title from DB to ensure same casing/normalization
		t.Album = existingTitle
	}

updateTrack:
	// Double check cover art inheritance if we are about to delete an old record or merge
	var currentAlbumID int64
	var currentCoverPath sql.NullString
	err = db.QueryRow("SELECT album_id, a.cover_path FROM music_tracks t JOIN music_albums a ON t.album_id = a.id WHERE t.path = ?", t.Path).Scan(&currentAlbumID, &currentCoverPath)
	if err == nil && currentAlbumID != albumID && currentCoverPath.Valid && currentCoverPath.String != "" {
		// Moving track to a different album entry, migrate cover if target has none
		db.Exec("UPDATE music_albums SET cover_path = ? WHERE id = ? AND (cover_path IS NULL OR cover_path = '')", currentCoverPath.String, albumID)
	}

	if force {
		// Full overwrite
		_, err = db.Exec(`
			INSERT INTO music_tracks (
				path, mtime, title, artist, album, album_artist, 
				track_num, disc_num, genre, duration, sample_rate, album_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				mtime = excluded.mtime,
				title = excluded.title,
				artist = excluded.artist,
				album = excluded.album,
				album_artist = excluded.album_artist,
				track_num = excluded.track_num,
				disc_num = excluded.disc_num,
				genre = excluded.genre,
				duration = excluded.duration,
				sample_rate = excluded.sample_rate,
				album_id = excluded.album_id
		`, t.Path, t.MTime, t.Title, t.Artist, t.Album, t.AlbumArtist,
			t.TrackNum, t.DiscNum, t.Genre, t.Duration, t.SampleRate, albumID)
	} else {
		// Insert track; on conflict only overwrite empty/NULL fields, 
		// and PROTECT album_id/names if they already exist.
		_, err = db.Exec(`
			INSERT INTO music_tracks (
				path, mtime, title, artist, album, album_artist, 
				track_num, disc_num, genre, duration, sample_rate, album_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET
				mtime = excluded.mtime,
				title = CASE WHEN title IS NULL OR title = '' THEN excluded.title ELSE title END,
				artist = CASE WHEN artist IS NULL OR artist = '' THEN excluded.artist ELSE artist END,
				album = CASE WHEN album IS NULL OR album = '' THEN excluded.album ELSE album END,
				album_artist = CASE WHEN album_artist IS NULL OR album_artist = '' THEN excluded.album_artist ELSE album_artist END,
				track_num = CASE WHEN track_num IS NULL THEN excluded.track_num ELSE track_num END,
				disc_num = CASE WHEN disc_num IS NULL THEN excluded.disc_num ELSE disc_num END,
				genre = CASE WHEN genre IS NULL OR genre = '' THEN excluded.genre ELSE genre END,
				duration = CASE WHEN duration IS NULL THEN excluded.duration ELSE duration END,
				sample_rate = CASE WHEN sample_rate IS NULL THEN excluded.sample_rate ELSE sample_rate END,
				album_id = CASE WHEN album_id IS NULL OR album_id = 0 THEN excluded.album_id ELSE album_id END
		`, t.Path, t.MTime, t.Title, t.Artist, t.Album, t.AlbumArtist,
			t.TrackNum, t.DiscNum, t.Genre, t.Duration, t.SampleRate, albumID)
	}

	if err != nil {
		return fmt.Errorf("failed to insert track: %w", err)
	}

	return nil
}

func UpdateAlbumMBID(albumID int64, mbid string, force bool) error {
	if force {
		_, err := db.Exec("UPDATE music_albums SET mbid = ? WHERE id = ?", mbid, albumID)
		return err
	}
	_, err := db.Exec("UPDATE music_albums SET mbid = ? WHERE id = ? AND (mbid IS NULL OR mbid = '')", mbid, albumID)
	return err
}

func UpdateAlbumDate(albumID int64, date string, force bool) error {
	if force {
		_, err := db.Exec("UPDATE music_albums SET release_date = ? WHERE id = ?", date, albumID)
		return err
	}
	_, err := db.Exec("UPDATE music_albums SET release_date = ? WHERE id = ? AND (release_date IS NULL OR release_date = '')", date, albumID)
	return err
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
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM music_playlist_tracks WHERE playlist_id = ? AND track_path = ?", playlistID, trackPath)
	if err != nil {
		return err
	}

	rows, err := tx.Query("SELECT rowid FROM music_playlist_tracks WHERE playlist_id = ? ORDER BY sort_order", playlistID)
	if err != nil {
		return err
	}
	var rowids []int64
	for rows.Next() {
		var rid int64
		rows.Scan(&rid)
		rowids = append(rowids, rid)
	}
	rows.Close()

	for i, rid := range rowids {
		_, err = tx.Exec("UPDATE music_playlist_tracks SET sort_order = ? WHERE rowid = ?", i+1, rid)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func DeleteMusicPlaylist(playlistID int64) error {
	_, err := db.Exec("DELETE FROM music_playlist_tracks WHERE playlist_id = ?", playlistID)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM music_playlists WHERE id = ?", playlistID)
	return err
}

func RenameMusicPlaylist(playlistID int64, name string) error {
	_, err := db.Exec("UPDATE music_playlists SET name = ? WHERE id = ?", name, playlistID)
	return err
}

func MoveMusicPlaylistTrack(playlistID int64, fromIdx, toIdx int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT rowid FROM music_playlist_tracks WHERE playlist_id = ? ORDER BY sort_order", playlistID)
	if err != nil {
		return err
	}
	var rowids []int64
	for rows.Next() {
		var rid int64
		rows.Scan(&rid)
		rowids = append(rowids, rid)
	}
	rows.Close()

	if fromIdx < 0 || fromIdx >= len(rowids) || toIdx < 0 || toIdx >= len(rowids) {
		return fmt.Errorf("invalid index: from=%d to=%d len=%d", fromIdx, toIdx, len(rowids))
	}

	rid := rowids[fromIdx]
	rowids = append(rowids[:fromIdx], rowids[fromIdx+1:]...)
	newRowids := make([]int64, 0, len(rowids)+1)
	newRowids = append(newRowids, rowids[:toIdx]...)
	newRowids = append(newRowids, rid)
	newRowids = append(newRowids, rowids[toIdx:]...)

	for i, r := range newRowids {
		_, err = tx.Exec("UPDATE music_playlist_tracks SET sort_order = ? WHERE rowid = ?", i+1, r)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func UpdateTrackField(trackPath string, field string, value interface{}) error {
	var column string
	switch field {
	case "title":
		column = "title"
	case "artist":
		column = "artist"
	case "album":
		column = "album"
	case "tracknum":
		column = "track_num"
	case "genre":
		column = "genre"
	default:
		return fmt.Errorf("unknown field: %s", field)
	}

	q := fmt.Sprintf("UPDATE music_tracks SET %s = ? WHERE path = ?", column)
	_, err := db.Exec(q, value, trackPath)
	return err
}

func UpdateTrackMTime(path string, mtime float64) error {
	_, err := db.Exec("UPDATE music_tracks SET mtime = ? WHERE path = ?", mtime, path)
	return err
}

func GetAlbumArtistTitle(albumID int64) (string, string, error) {
	var artist, title string
	err := db.QueryRow("SELECT artist, title FROM music_albums WHERE id = ?", albumID).Scan(&artist, &title)
	if err != nil {
		return "", "", err
	}
	return artist, title, nil
}

func UpdateAlbumMetadata(albumID int64, newArtist string, newAlbum string, newDate string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE music_albums SET artist = ?, title = ?, release_date = ? WHERE id = ?", newArtist, newAlbum, newDate, albumID)
	if err != nil {
		return err
	}

	_, err = tx.Exec("UPDATE music_tracks SET artist = ?, album = ? WHERE album_id = ?", newArtist, newAlbum, albumID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func GetMusicTrackByPath(path string) (TrackData, error) {
	var t TrackData
	err := db.QueryRow(
		"SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration, sample_rate FROM music_tracks WHERE path = ?",
		path,
	).Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration, &t.SampleRate)
	if err != nil {
		return TrackData{}, err
	}
	return t, nil
}

func GetAlbumIDByTrackPath(path string) (int64, error) {
	var id int64
	err := db.QueryRow(
		"SELECT album_id FROM music_tracks WHERE path = ?",
		path,
	).Scan(&id)
	return id, err
}

func GetMusicTracks(artist, albumTitle string) ([]TrackData, error) {
	query := "SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration, sample_rate FROM music_tracks"
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
		err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration, &t.SampleRate)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func GetMusicArtistsAndAlbums() ([]string, map[string][]Album, error) {
	rows, err := db.Query("SELECT id, artist, title, COALESCE(directory, ''), COALESCE(release_date, '') FROM music_albums ORDER BY artist, release_date DESC, title")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	artists := []string{}
	artistMap := make(map[string]bool)
	albums := make(map[string][]Album)

	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Artist, &a.Title, &a.Directory, &a.ReleaseDate); err != nil {
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

func DeleteEmptyAlbums() error {
	_, err := db.Exec("DELETE FROM music_albums WHERE id NOT IN (SELECT DISTINCT album_id FROM music_tracks)")
	return err
}

func UpdateAlbumCover(albumID int64, coverPath string) error {
	_, err := db.Exec("UPDATE music_albums SET cover_path = ? WHERE id = ? AND (cover_path IS NULL OR cover_path = '')", coverPath, albumID)
	return err
}

func GetAllAlbums() ([]Album, error) {
	rows, err := db.Query("SELECT id, artist, title, COALESCE(directory, ''), COALESCE(release_date, '') FROM music_albums ORDER BY artist, title")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		if err := rows.Scan(&a.ID, &a.Artist, &a.Title, &a.Directory, &a.ReleaseDate); err != nil {
			return nil, err
		}
		albums = append(albums, a)
	}
	return albums, nil
}

func GetMusicTracksByAlbumID(albumID int64) ([]TrackData, error) {
	rows, err := db.Query(
		"SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration, sample_rate FROM music_tracks WHERE album_id = ?",
		albumID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []TrackData
	for rows.Next() {
		var t TrackData
		if err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration, &t.SampleRate); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

type MusicFilter struct {
	ID             int64
	Name           string
	ConditionsJSON string
	SortJSON       string
}

func GetRecentMusicTracks(limit int) ([]TrackData, error) {
	rows, err := db.Query(
		"SELECT path, mtime, title, artist, album, album_artist, track_num, disc_num, genre, duration, sample_rate FROM music_tracks ORDER BY created_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []TrackData
	for rows.Next() {
		var t TrackData
		if err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration, &t.SampleRate); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func GetMusicFilters() ([]MusicFilter, error) {
	rows, err := db.Query("SELECT id, name, COALESCE(conditions_json, ''), COALESCE(sort_json, '') FROM music_filters ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var filters []MusicFilter
	for rows.Next() {
		var f MusicFilter
		if err := rows.Scan(&f.ID, &f.Name, &f.ConditionsJSON, &f.SortJSON); err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

func GetMusicFilterByID(id int64) (*MusicFilter, error) {
	var f MusicFilter
	err := db.QueryRow(
		"SELECT id, name, COALESCE(conditions_json, ''), COALESCE(sort_json, '') FROM music_filters WHERE id = ?",
		id,
	).Scan(&f.ID, &f.Name, &f.ConditionsJSON, &f.SortJSON)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func CreateMusicFilter(name, conditionsJSON, sortJSON string) (int64, error) {
	result, err := db.Exec("INSERT INTO music_filters (name, conditions_json, sort_json) VALUES (?, ?, ?)", name, conditionsJSON, sortJSON)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateMusicFilter(id int64, name, conditionsJSON, sortJSON string) error {
	_, err := db.Exec("UPDATE music_filters SET name = ?, conditions_json = ?, sort_json = ? WHERE id = ?", name, conditionsJSON, sortJSON, id)
	return err
}

func DeleteMusicFilter(id int64) error {
	_, err := db.Exec("DELETE FROM music_filters WHERE id = ?", id)
	return err
}

func GetMusicPlaylistTracks(playlistID int64) ([]TrackData, error) {
	rows, err := db.Query(`
		SELECT 
			t.path, t.mtime, t.title, t.artist, t.album, t.album_artist, 
			t.track_num, t.disc_num, t.genre, t.duration, t.sample_rate
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
		err := rows.Scan(&t.Path, &t.MTime, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNum, &t.DiscNum, &t.Genre, &t.Duration, &t.SampleRate)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}
