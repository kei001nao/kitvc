package library

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// ... (AudioExtensions and VideoExtensions unchanged)

type Video struct {
	Path     string
	Filename string
	Size     int64
	Duration int
	Year     int
	MTime    float64
}

func ScanVideo(directories []string) ([]Video, error) {
	var videos []Video

	for _, dir := range directories {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(path))
			if !VideoExtensions[ext] {
				return nil
			}

			vInfo, err := readVideoMetadata(path)
			if err != nil {
				fmt.Printf("Error reading video metadata for %s: %v\n", path, err)
				return nil
			}

			videos = append(videos, Video{
				Path:     path,
				Filename: info.Name(),
				Size:     info.Size(),
				Duration: vInfo.Duration,
				Year:     vInfo.Year,
				MTime:    float64(info.ModTime().Unix()),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return videos, nil
}

type ffprobeOutput struct {
	Format struct {
		Duration string `json:"duration"`
		Tags     struct {
			Date         string `json:"date"`
			CreationTime string `json:"creation_time"`
		} `json:"tags"`
	} `json:"format"`
}

func readVideoMetadata(path string) (Video, error) {
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return Video{}, err
	}

	var data ffprobeOutput
	if err := json.Unmarshal(out, &data); err != nil {
		return Video{}, err
	}

	duration, _ := strconv.ParseFloat(data.Format.Duration, 64)
	year := 0
	dateStr := data.Format.Tags.Date
	if dateStr == "" {
		dateStr = data.Format.Tags.CreationTime
	}
	if len(dateStr) >= 4 {
		year, _ = strconv.Atoi(dateStr[:4])
	}

	return Video{
		Duration: int(duration),
		Year:     year,
	}, nil
}

var AudioExtensions = map[string]bool{
	".flac": true, ".mp3": true, ".opus": true, ".ogg": true,
	".m4a":  true, ".aac": true, ".wav": true, ".aiff": true,
}

var VideoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
}

type Track struct {
	Path        string
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNum    int
	DiscNum     int
	Genre       string
	Duration    int
	MTime       float64
}

func ScanMusic(directories []string) ([]Track, error) {
	var tracks []Track

	for _, dir := range directories {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(path))
			if !AudioExtensions[ext] {
				return nil
			}

			track, err := readAudioTags(path)
			if err != nil {
				// Log error and continue
				fmt.Printf("Error reading tags for %s: %v\n", path, err)
				return nil
			}
			track.MTime = float64(info.ModTime().Unix())
			tracks = append(tracks, track)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return tracks, nil
}

func readAudioTags(path string) (Track, error) {
	f, err := os.Open(path)
	if err != nil {
		return Track{}, err
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return Track{
			Path:  path,
			Title: filepath.Base(path),
		}, nil // Return basic info even if tags fail
	}

	trackNum, _ := m.Track()
	discNum, _ := m.Disc()

	return Track{
		Path:        path,
		Title:       m.Title(),
		Artist:      m.Artist(),
		Album:       m.Album(),
		AlbumArtist: m.AlbumArtist(),
		TrackNum:    trackNum,
		DiscNum:     discNum,
		Genre:       m.Genre(),
		// Duration: m.Duration(), // tag library might not provide duration for all formats
	}, nil
}
