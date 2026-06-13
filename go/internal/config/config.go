package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Music    MusicConfig    `toml:"music"`
	Video    VideoConfig    `toml:"video"`
	Playlist PlaylistConfig `toml:"playlist"`
	Player   PlayerConfig   `toml:"player"`
	Theme    ThemeConfig    `toml:"theme"`
	UI       UIConfig       `toml:"ui"`
}

type MusicConfig struct {
	Directories []string `toml:"directories"`
}

type VideoConfig struct {
	Directories []string `toml:"directories"`
	Fullscreen  bool     `toml:"fullscreen"`
	TMDBAPIKey  string   `toml:"tmdb_api_key"`
	LastLanguage string  `toml:"last_language"`
}

type PlaylistConfig struct {
	MusicPlaylistDir string `toml:"music_playlist_dir"`
	VideoPlaylistDir string `toml:"video_playlist_dir"`
}

type PlayerConfig struct {
	MpvArgs []string `toml:"mpv_args"`
	Volume  int      `toml:"volume"`
}

type ThemeConfig struct {
	WatchInterval int `toml:"watch_interval"`
}

type UIConfig struct {
	SidebarWidth int `toml:"sidebar_width"`
}

type Theme struct {
	Colors map[string]string `toml:"colors"`
}

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "kitvc"), nil
}

func GetCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "kitvc"), nil
}

func LoadConfig() (*Config, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	var cfg Config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Music: MusicConfig{
			Directories: []string{filepath.Join(home, "Music")},
		},
		Video: VideoConfig{
			Directories: []string{filepath.Join(home, "Videos")},
			Fullscreen:  false,
		},
		Player: PlayerConfig{
			MpvArgs: []string{},
			Volume:  80,
		},
		Theme: ThemeConfig{
			WatchInterval: 2,
		},
		UI: UIConfig{
			SidebarWidth: 44,
		},
	}
}
