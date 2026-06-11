package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	APIKey   string
	Language string
	HTTP     *http.Client
}

type SearchItem struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	PosterPath  string  `json:"poster_path"`
	ReleaseDate string  `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	MediaType   string  `json:"media_type"`
	VoteAverage float64 `json:"vote_average"`
}

type SearchResponse struct {
	Results []SearchItem `json:"results"`
}

type TVDetails struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Overview         string `json:"overview"`
	PosterPath       string `json:"poster_path"`
	FirstAirDate     string `json:"first_air_date"`
	NumberOfSeasons  int    `json:"number_of_seasons"`
	NumberOfEpisodes int    `json:"number_of_episodes"`
}

type SeasonDetails struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Overview    string          `json:"overview"`
	SeasonNumber int            `json:"season_number"`
	Episodes    []EpisodeDetail `json:"episodes"`
}

type EpisodeDetail struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	EpisodeNumber int    `json:"episode_number"`
	SeasonNumber  int    `json:"season_number"`
	AirDate       string `json:"air_date"`
	StillPath     string `json:"still_path"`
}

type MovieDetails struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`
	Year         int    `json:"year"`
	Genres       []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Credits *struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
}

type TVDetailsFull struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Overview         string `json:"overview"`
	PosterPath       string `json:"poster_path"`
	FirstAirDate     string `json:"first_air_date"`
	NumberOfSeasons  int    `json:"number_of_seasons"`
	Genres           []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Credits *struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
	} `json:"credits"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:   apiKey,
		Language: "ja",
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) SearchMulti(query string) ([]SearchItem, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/search/multi?api_key=%s&language=%s&query=%s",
		c.APIKey, c.Language, url.QueryEscape(query))
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *Client) SearchTV(query string) ([]SearchItem, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&language=%s&query=%s",
		c.APIKey, c.Language, url.QueryEscape(query))
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *Client) SearchMovie(query string) ([]SearchItem, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&language=%s&query=%s",
		c.APIKey, c.Language, url.QueryEscape(query))
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (c *Client) FetchTVDetails(tmdbID int) (*TVDetailsFull, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s&language=%s&append_to_response=credits",
		tmdbID, c.APIKey, c.Language)
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var details TVDetailsFull
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}
	return &details, nil
}

func (c *Client) FetchTVSeason(tmdbID, seasonNumber int) (*SeasonDetails, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/season/%d?api_key=%s&language=%s",
		tmdbID, seasonNumber, c.APIKey, c.Language)
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var details SeasonDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}
	return &details, nil
}

func (c *Client) FetchMovieDetails(tmdbID int) (*MovieDetails, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?api_key=%s&language=%s&append_to_response=credits",
		tmdbID, c.APIKey, c.Language)
	
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var details MovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}
	return &details, nil
}

func DownloadPoster(imageURL, targetDir, name string) (string, error) {
	if imageURL == "" {
		return "", fmt.Errorf("empty image URL")
	}
	
	fullURL := "https://image.tmdb.org/t/p/w500" + imageURL
	
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}
	
	targetPath := filepath.Join(targetDir, name+".jpg")
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	}
	
	resp, err := http.Get(fullURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return targetPath, err
}
