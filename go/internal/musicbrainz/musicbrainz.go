package musicbrainz

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	baseURL    = "https://musicbrainz.org/ws/2"
	coverArtURL = "https://coverartarchive.org"
	userAgent  = "kitvc/1.0 (kitvc-player)"
	rateLimit  = 1 * time.Second
)

var (
	lastCall   time.Time
	lastCallMu sync.Mutex
	sharedClient = &Client{
		http: &http.Client{Timeout: 15 * time.Second},
	}
)

type Client struct {
	http     *http.Client
}

func SharedClient() *Client {
	return sharedClient
}

func NewClient() *Client {
	return SharedClient()
}

func (c *Client) rateLimitWait() {
	lastCallMu.Lock()
	defer lastCallMu.Unlock()
	elapsed := time.Since(lastCall)
	if elapsed < rateLimit {
		time.Sleep(rateLimit - elapsed)
	}
	lastCall = time.Now()
}

func (c *Client) get(urlStr string, dest interface{}) error {
	c.rateLimitWait()
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("MusicBrainz returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type artistList struct {
	Artists []Artist `json:"artists"`
}

type Release struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	TrackCount int    `json:"track-count"`
	ArtistCredit []struct {
		Name string `json:"name"`
	} `json:"artist-credit"`
	ArtistName string // Filled after decoding
}

type Recording struct {
	ID           string
	Title        string
	ArtistName   string
	ReleaseTitle string
	ReleaseID    string
	Date         string
}

type releaseList struct {
	Releases []Release `json:"releases"`
}

type recordingList struct {
	Recordings []struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		ArtistCredit []struct {
			Name string `json:"name"`
		} `json:"artist-credit"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"releases"`
	} `json:"recordings"`
}

func (c *Client) SearchReleases(query string) ([]Release, error) {
	u := fmt.Sprintf("%s/release/?query=%s&fmt=json&limit=20",
		baseURL, url.QueryEscape(query))
	var result releaseList
	if err := c.get(u, &result); err != nil {
		return nil, err
	}
	// Assemble ArtistName from ArtistCredit
	for i := range result.Releases {
		if len(result.Releases[i].ArtistCredit) > 0 {
			var names []string
			for _, ac := range result.Releases[i].ArtistCredit {
				names = append(names, ac.Name)
			}
			result.Releases[i].ArtistName = strings.Join(names, ", ")
		}
	}
	return result.Releases, nil
}

func (c *Client) SearchRecordings(query string) ([]Recording, error) {
	u := fmt.Sprintf("%s/recording/?query=%s&fmt=json&limit=20",
		baseURL, url.QueryEscape(query))
	var result recordingList
	if err := c.get(u, &result); err != nil {
		return nil, err
	}

	var recordings []Recording
	for _, r := range result.Recordings {
		rec := Recording{
			ID:    r.ID,
			Title: r.Title,
		}
		// Artist
		var names []string
		for _, ac := range r.ArtistCredit {
			names = append(names, ac.Name)
		}
		rec.ArtistName = strings.Join(names, ", ")

		// Pick the first release as primary context
		if len(r.Releases) > 0 {
			rec.ReleaseID = r.Releases[0].ID
			rec.ReleaseTitle = r.Releases[0].Title
			rec.Date = r.Releases[0].Date
		}
		recordings = append(recordings, rec)
	}
	return recordings, nil
}

func (c *Client) SearchArtist(query string) ([]Artist, error) {
	u := fmt.Sprintf("%s/artist/?query=%s&fmt=json&limit=10",
		baseURL, url.QueryEscape(query))
	var result artistList
	if err := c.get(u, &result); err != nil {
		return nil, err
	}
	return result.Artists, nil
}

func (c *Client) SearchReleasesByArtist(artist string, releaseTitle string) ([]Release, error) {
	var query string
	if releaseTitle != "" {
		query = fmt.Sprintf(`artist:"%s" AND release:"%s"`, EscapeQuery(artist), EscapeQuery(releaseTitle))
	} else {
		query = fmt.Sprintf(`artist:"%s"`, EscapeQuery(artist))
	}
	return c.SearchReleases(query)
}

type releaseDetail struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`
	Media   []struct {
		Tracks []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Position int    `json:"position"`
			Number   string `json:"number"`
		} `json:"tracks"`
	} `json:"media"`
	ArtistCredit []struct {
		Name string `json:"name"`
	} `json:"artist-credit"`
}

type ReleaseTrack struct {
	ID       string
	Title    string
	Position int
}

type ReleaseInfo struct {
	ID     string
	Title  string
	Date   string
	Artist string
	Tracks []ReleaseTrack
}

func (c *Client) GetRelease(mbid string) (*ReleaseInfo, error) {
	u := fmt.Sprintf("%s/release/%s?inc=recordings+artists&fmt=json", baseURL, mbid)
	var detail releaseDetail
	if err := c.get(u, &detail); err != nil {
		return nil, err
	}
	info := &ReleaseInfo{
		ID:    detail.ID,
		Title: detail.Title,
		Date:  detail.Date,
	}
	if len(detail.ArtistCredit) > 0 {
		var names []string
		for _, ac := range detail.ArtistCredit {
			names = append(names, ac.Name)
		}
		info.Artist = strings.Join(names, ", ")
	}
	for _, m := range detail.Media {
		for _, t := range m.Tracks {
			info.Tracks = append(info.Tracks, ReleaseTrack{
				ID:       t.ID,
				Title:    t.Title,
				Position: t.Position,
			})
		}
	}
	return info, nil
}

func (c *Client) FetchCoverArt(mbid string) ([]byte, string, error) {
	u := fmt.Sprintf("%s/release/%s/front", coverArtURL, mbid)
	c.rateLimitWait()
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("CoverArtArchive returned status %d", resp.StatusCode)
	}
	// Determine extension from content-type
	ext := ".jpg"
	ct := resp.Header.Get("Content-Type")
	switch {
	case ct == "image/png":
		ext = ".png"
	case ct == "image/gif":
		ext = ".gif"
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, ext, nil
}

func EscapeQuery(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `\`, `\\`)
	return s
}
