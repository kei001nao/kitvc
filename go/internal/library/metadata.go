package library

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	musicBrainzAPI  = "https://musicbrainz.org/ws/2"
	coverArtArchive = "https://coverartarchive.org"
	userAgent       = "kitvc/1.0 (kitvc-player)"
)

type mbRelease struct {
	ID string `json:"id"`
}

type mbResponse struct {
	Releases []mbRelease `json:"releases"`
}

func searchMusicBrainzRelease(artist, album string) (string, error) {
	query := fmt.Sprintf(`artist:"%s" AND release:"%s"`, escapeMBQuery(artist), escapeMBQuery(album))
	u := fmt.Sprintf("%s/release/?query=%s&fmt=json&limit=5", musicBrainzAPI, url.QueryEscape(query))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("musicbrainz returned status %d", resp.StatusCode)
	}

	var result mbResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Releases) == 0 {
		return "", fmt.Errorf("no release found for %s - %s", artist, album)
	}

	return result.Releases[0].ID, nil
}

func fetchCoverArt(mbid string) ([]byte, error) {
	u := fmt.Sprintf("%s/release/%s/front", coverArtArchive, mbid)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cover art archive returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func FetchOnlineCover(artist, album string) ([]byte, error) {
	// MusicBrainz rate limit: 1 req/s
	time.Sleep(1 * time.Second)

	mbid, err := searchMusicBrainzRelease(artist, album)
	if err != nil {
		return nil, fmt.Errorf("musicbrainz search failed: %w", err)
	}

	data, err := fetchCoverArt(mbid)
	if err != nil {
		return nil, fmt.Errorf("cover art fetch failed: %w", err)
	}

	return data, nil
}

func escapeMBQuery(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `\`, `\\`)
	return s
}
