package library

import (
	"fmt"

	"kitvc/internal/musicbrainz"
)

func FetchOnlineCover(artist, album string) ([]byte, error) {
	client := musicbrainz.SharedClient()

	releases, err := client.SearchReleasesByArtist(artist, album)
	if err != nil {
		return nil, fmt.Errorf("musicbrainz search failed: %w", err)
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no release found for %s - %s", artist, album)
	}

	mbid := releases[0].ID
	data, _, err := client.FetchCoverArt(mbid)
	if err != nil {
		return nil, fmt.Errorf("cover art fetch failed: %w", err)
	}

	return data, nil
}
