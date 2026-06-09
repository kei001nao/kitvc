package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"kitvc/internal/config"
	"kitvc/internal/db"
)

func ExtractEmbeddedCover(trackPath string) ([]byte, string, error) {
	f, err := os.Open(trackPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, "", err
	}

	pic := m.Picture()
	if pic == nil {
		return nil, "", fmt.Errorf("no embedded cover")
	}

	ext := pic.Ext
	if ext == "" {
		ext = ".jpg"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	return pic.Data, ext, nil
}

func FindFolderCover(dir string) ([]byte, string, error) {
	candidates := []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png", "Cover.jpg", "Cover.png", "Front.jpg", "Front.png"}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err == nil {
			ext := filepath.Ext(name)
			return data, ext, nil
		}
	}
	return nil, "", fmt.Errorf("no folder cover found in %s", dir)
}

func CacheCoverData(albumID int64, data []byte, ext string) (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}

	coversDir := filepath.Join(configDir, "covers")
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%d%s", albumID, ext)
	path := filepath.Join(coversDir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	return path, nil
}

func GetCachedCoverPath(albumID int64) string {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}

	coversDir := filepath.Join(configDir, "covers")
	candidates := []string{".jpg", ".jpeg", ".png", ".gif"}
	for _, ext := range candidates {
		p := filepath.Join(coversDir, fmt.Sprintf("%d%s", albumID, ext))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func ProcessAlbumCover(album db.Album) error {
	cached := GetCachedCoverPath(album.ID)
	if cached != "" {
		return db.UpdateAlbumCover(album.ID, cached)
	}

	tracks, err := db.GetMusicTracksByAlbumID(album.ID)
	if err != nil || len(tracks) == 0 {
		return fmt.Errorf("no tracks for album %d", album.ID)
	}

	firstTrackPath := tracks[0].Path

	data, ext, err := ExtractEmbeddedCover(firstTrackPath)
	if err != nil {
		dir := filepath.Dir(firstTrackPath)
		data, ext, err = FindFolderCover(dir)
	}
	if err != nil {
		// Try online fetch
		dataOnline, errOnline := FetchOnlineCover(album.Artist, album.Title)
		if errOnline != nil {
			return fmt.Errorf("no cover found for album %d: %v", album.ID, errOnline)
		}
		data = dataOnline
		ext = ".jpg"
	}

	cachePath, err := CacheCoverData(album.ID, data, ext)
	if err != nil {
		return fmt.Errorf("failed to cache cover: %w", err)
	}

	return db.UpdateAlbumCover(album.ID, cachePath)
}

func ProcessAllAlbumCovers() error {
	albums, err := db.GetAllAlbums()
	if err != nil {
		return err
	}

	for _, a := range albums {
		if err := ProcessAlbumCover(a); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	return nil
}

