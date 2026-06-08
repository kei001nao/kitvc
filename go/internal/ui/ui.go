package ui

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"kitvc/internal/config"
	"kitvc/internal/db"
	"kitvc/internal/library"
	"kitvc/internal/player"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewState int

const (
	viewMusicLibrary viewState = iota
	viewVideoLibrary
	viewMusicQueue
	viewMusicArtists
	viewMusicArtistDetail
)

type model struct {
	config       *config.Config
	player       *player.MpvPlayer
	width        int
	height       int
	message      string
	sidebar      sidebar
	trackList    trackList
	videoList    videoList
	musicArtists musicArtists
	artistDetail musicArtistDetail
	activeView   viewState
	focusedSide  bool // true if sidebar has focus
	currentPlaylistID int64 // 0 if not viewing a playlist
	currentTrack string
	progress     progress.Model
	playbackPos  float64
	duration     float64
	volume       float64
}

func InitialModel(cfg *config.Config) model {
	// Socket path for mpv IPC
	socketPath := fmt.Sprintf("/tmp/kitvc-mpv-%d.sock", os.Getpid())
	p := player.NewMpvPlayer(socketPath, cfg.Player.MpvArgs)
	if err := p.Start(); err != nil {
		// We still create the model, but player will be disconnected
		log.Printf("Warning: failed to start mpv: %v", err)
	}

	return model{
		config:      cfg,
		player:      p,
		progress:    progress.New(progress.WithDefaultGradient()),
		focusedSide: true,
		activeView:  viewMusicLibrary,
		sidebar:     newSidebar(cfg.UI.SidebarWidth, 20),
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.player != nil {
				m.player.Stop()
			}
			return m, tea.Quit
		case "tab":
			m.focusedSide = !m.focusedSide
			m.syncFocus()
			return m, nil
		case "left":
			if !m.focusedSide {
				if m.player != nil {
					m.player.Seek(-5)
				}
				return m, nil
			}
		case "right":
			if !m.focusedSide {
				if m.player != nil {
					m.player.Seek(5)
				}
				return m, nil
			}
		case "H":
			if m.player != nil {
				m.player.Seek(-10)
			}
			return m, nil
		case "L":
			if m.player != nil {
				m.player.Seek(10)
			}
			return m, nil
		case "9":
			if m.player != nil {
				m.player.AdjustVolume(-5)
				m.volume -= 5
				if m.volume < 0 {
					m.volume = 0
				}
			}
			return m, nil
		case "0":
			if m.player != nil {
				m.player.AdjustVolume(5)
				m.volume += 5
				if m.volume > 100 {
					m.volume = 100
				}
			}
			return m, nil
		case "s":
			m.message = "Scanning..."
			if m.activeView == viewVideoLibrary {
				return m, m.scanVideoCmd()
			}
			return m, m.scanMusicCmd()
		case "enter":
			if m.focusedSide {
				m.focusedSide = false
				m.syncFocus()
				return m, nil
			} else {
				if m.activeView == viewMusicLibrary {
					selected := m.trackList.table.SelectedRow()
					if len(selected) > 0 {
						artist, album := m.getCurrentFilter()
						tracks, _ := db.GetMusicTracks(artist, album)
						
						var paths []string
						startIndex := -1
						for i, t := range tracks {
							paths = append(paths, t.Path)
							if t.Title == selected[0] && t.Artist == selected[1] {
								startIndex = i
							}
						}
						
						if startIndex >= 0 {
							m.player.PlayQueue(paths, startIndex)
							t := tracks[startIndex]
							m.currentTrack = fmt.Sprintf("%s - %s", t.Artist, t.Title)
							m.duration = float64(t.Duration)
						}
					}
					return m, nil
				} else if m.activeView == viewMusicArtists {
					selectedArtist := m.musicArtists.SelectedArtist()
					if selectedArtist != "" {
						m.activeView = viewMusicArtistDetail
						_, albums, err := db.GetMusicArtistsAndAlbums()
						if err == nil {
							sbWidth := m.getSidebarWidth()
							m.artistDetail = newMusicArtistDetail(m.width-sbWidth-1, m.height-8, selectedArtist, albums[selectedArtist])
							m.focusedSide = false
							m.syncFocus()
						}
					}
					return m, nil
				} else if m.activeView == viewMusicArtistDetail {
					if !m.artistDetail.focusedUpper {
						track, ok := m.artistDetail.SelectedTrack()
						if ok {
							tracks := m.artistDetail.tracks
							var paths []string
							startIndex := -1
							for i, t := range tracks {
								paths = append(paths, t.Path)
								if t.Path == track.Path {
									startIndex = i
								}
							}
							if startIndex >= 0 {
								m.player.PlayQueue(paths, startIndex)
								m.currentTrack = fmt.Sprintf("%s - %s", track.Artist, track.Title)
								m.duration = float64(track.Duration)
							}
						}
						return m, nil
					}
				} else if m.activeView == viewVideoLibrary {
					selected := m.videoList.table.SelectedRow()
					if len(selected) > 0 {
						videos, _ := db.GetVideos()
						
						var paths []string
						startIndex := -1
						for i, v := range videos {
							paths = append(paths, v.Path)
							if v.Filename == selected[len(selected)-1] {
								startIndex = i
							}
						}

						if startIndex >= 0 {
							m.player.SetProperty("vid", "auto")
							m.player.PlayQueue(paths, startIndex)
							v := videos[startIndex]
							m.currentTrack = v.Filename
							m.duration = float64(v.Duration)
						}
					}
					return m, nil
				}
			}
		case " ": // Play/Pause
			if m.player != nil {
				m.player.CyclePause()
			}
			return m, nil
		case "d":
			if !m.focusedSide && m.currentPlaylistID > 0 {
				if m.activeView == viewMusicLibrary {
					selected := m.trackList.table.SelectedRow()
					if len(selected) > 0 {
						// Need to find the path. We can refresh or keep paths in the list.
						// For now, let's re-query to find the path matching the selection
						artist, album := m.getCurrentFilter()
						var tracks []db.TrackData
						if artist == "" && album == "" {
							// In playlist, getCurrentFilter might not return what we need
							tracks, _ = db.GetMusicPlaylistTracks(m.currentPlaylistID)
						} else {
							tracks, _ = db.GetMusicTracks(artist, album)
						}
						
						for _, t := range tracks {
							if t.Title == selected[0] && t.Artist == selected[1] {
								db.RemoveTrackFromMusicPlaylist(m.currentPlaylistID, t.Path)
								m.refreshPlaylistTracks(m.currentPlaylistID)
								break
							}
						}
					}
				} else if m.activeView == viewVideoLibrary {
					selected := m.videoList.table.SelectedRow()
					if len(selected) > 0 {
						videos, _ := db.GetVideoPlaylistFiles(m.currentPlaylistID)
						for _, v := range videos {
							if v.Filename == selected[len(selected)-1] {
								db.RemoveFileFromVideoPlaylist(m.currentPlaylistID, v.Path)
								m.refreshVideoPlaylistFiles(m.currentPlaylistID)
								break
							}
						}
					}
				}
				return m, nil
			}
		case "a":
			if !m.focusedSide {
				if m.activeView == viewMusicLibrary {
					selected := m.trackList.table.SelectedRow()
					if len(selected) > 0 {
						playlists, _ := db.GetMusicPlaylists()
						var targetID int64
						if len(playlists) == 0 {
							db.CreateMusicPlaylist("My Playlist")
							playlists, _ = db.GetMusicPlaylists()
						}
						if len(playlists) > 0 {
							targetID = playlists[0].ID
							artist, album := m.getCurrentFilter()
							tracks, _ := db.GetMusicTracks(artist, album)
							for _, t := range tracks {
								if t.Title == selected[0] && t.Artist == selected[1] {
									db.AddTrackToMusicPlaylist(targetID, t.Path)
									m.message = fmt.Sprintf("Added to %s", playlists[0].Name)
									break
								}
							}
						}
					}
				} else if m.activeView == viewVideoLibrary {
					selected := m.videoList.table.SelectedRow()
					if len(selected) > 0 {
						playlists, _ := db.GetVideoPlaylists()
						var targetID int64
						if len(playlists) == 0 {
							db.CreateVideoPlaylist("My Video Playlist")
							playlists, _ = db.GetVideoPlaylists()
						}
						if len(playlists) > 0 {
							targetID = playlists[0].ID
							videos, _ := db.GetVideos()
							for _, v := range videos {
								if v.Filename == selected[len(selected)-1] {
									db.AddFileToVideoPlaylist(targetID, v.Path)
									m.message = fmt.Sprintf("Added to %s", playlists[0].Name)
									break
								}
							}
						}
					}
				}
				m.sidebar.Refresh()
				return m, nil
			}
		case "n":
			if m.activeView == viewMusicLibrary {
				playlists, _ := db.GetMusicPlaylists()
				db.CreateMusicPlaylist(fmt.Sprintf("New Playlist %d", len(playlists)+1))
			} else if m.activeView == viewVideoLibrary {
				playlists, _ := db.GetVideoPlaylists()
				db.CreateVideoPlaylist(fmt.Sprintf("New Video Playlist %d", len(playlists)+1))
			}
			m.sidebar.Refresh()
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = m.width - 20

		sbWidth := m.getSidebarWidth()
		m.sidebar.SetSize(sbWidth, m.height-8)

		mainWidth := m.width - sbWidth - 1
		if mainWidth <= 0 {
			mainWidth = 1
		}

		m.trackList.SetSize(mainWidth, m.height-8)
		m.videoList.SetSize(mainWidth, m.height-8)
		m.musicArtists.SetSize(mainWidth, m.height-8)
		m.artistDetail.SetSize(mainWidth, m.height-8)
	case scanFinishedMsg:

		m.message = fmt.Sprintf("Scan finished: %d items found", msg.count)
		if m.activeView == viewMusicLibrary {
			m.refreshTrackList("", "")
		} else if m.activeView == viewVideoLibrary {
			m.refreshVideoList()
		}
	case tickMsg:
		if m.player != nil {
			val, _ := m.player.GetProperty("time-pos")
			if pos, ok := val.(float64); ok {
				m.playbackPos = pos
			}
			val, _ = m.player.GetProperty("duration")
			if dur, ok := val.(float64); ok {
				m.duration = dur
			}
			val, _ = m.player.GetProperty("volume")
			if v, ok := val.(float64); ok {
				m.volume = v
			}
		}
		cmds = append(cmds, tick())
	}

	if m.focusedSide {
		var cmd tea.Cmd
		oldNode := m.sidebar.SelectedNode()
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)

		newNode := m.sidebar.SelectedNode()
		if oldNode != newNode && newNode != nil {
			m.handleSidebarChange(newNode)
		}
	} else {
		if m.activeView == viewMusicLibrary {
			var cmd tea.Cmd
			m.trackList, cmd = m.trackList.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.activeView == viewMusicArtists {
			var cmd tea.Cmd
			m.musicArtists, cmd = m.musicArtists.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.activeView == viewMusicArtistDetail {
			var cmd tea.Cmd
			m.artistDetail, cmd = m.artistDetail.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.activeView == viewVideoLibrary {
			var cmd tea.Cmd
			m.videoList, cmd = m.videoList.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) syncFocus() {
	if m.activeView == viewMusicLibrary {
		m.trackList.SetFocus(!m.focusedSide)
	} else if m.activeView == viewMusicArtists {
		m.musicArtists.SetFocus(!m.focusedSide)
	} else if m.activeView == viewMusicArtistDetail {
		m.artistDetail.SetFocus(!m.focusedSide)
	} else if m.activeView == viewVideoLibrary {
		m.videoList.SetFocus(!m.focusedSide)
	}
}

func (m *model) handleSidebarChange(n *node) {
	m.currentPlaylistID = 0 // Reset by default

	switch {
	case n.id == "music_library":
		m.activeView = viewMusicArtists
		artists, _, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			sbWidth := m.getSidebarWidth()
			m.musicArtists = newMusicArtists(m.width-sbWidth-1, m.height-8, artists)
		}
	case n.id == "video_library":
		m.activeView = viewVideoLibrary
		m.refreshVideoList()
	case strings.HasPrefix(n.id, "artist:"):
		artist := strings.TrimPrefix(n.id, "artist:")
		m.activeView = viewMusicArtistDetail
		_, albums, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			sbWidth := m.getSidebarWidth()
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-1, m.height-8, artist, albums[artist])
		}
	case strings.HasPrefix(n.id, "album:"):
		artist, album := m.getCurrentFilter()
		m.activeView = viewMusicLibrary
		m.refreshTrackList(artist, album)
	case strings.HasPrefix(n.id, "music_playlist:"):
		idStr := strings.TrimPrefix(n.id, "music_playlist:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentPlaylistID = id
		m.activeView = viewMusicLibrary
		m.refreshPlaylistTracks(id)
	case strings.HasPrefix(n.id, "video_playlist:"):
		idStr := strings.TrimPrefix(n.id, "video_playlist:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentPlaylistID = id
		m.activeView = viewVideoLibrary
		m.refreshVideoPlaylistFiles(id)
	}
	m.syncFocus()
}

func (m *model) getSidebarWidth() int {
	w := m.config.UI.SidebarWidth
	if w == 0 {
		w = 30
	}
	// Limit sidebar width to 30% of total width if narrow, but at least 20
	maxW := int(float64(m.width) * 0.3)
	if maxW < 20 {
		maxW = 20
	}
	if w > maxW && m.width < 120 {
		w = maxW
	}
	if m.width-w-1 < 30 {
		w = m.width - 31
		if w < 10 {
			w = 10
		}
	}
	// Final safety checks to prevent wrapping
	if w >= m.width-2 {
		w = m.width - 3
	}
	if w <= 0 {
		return 1
	}
	return w
}

func (m *model) refreshVideoPlaylistFiles(playlistID int64) {
	sbWidth := m.getSidebarWidth()
	videos, _ := db.GetVideoPlaylistFiles(playlistID)
	m.videoList = newVideoListFromVideos(m.width-sbWidth-1, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshPlaylistTracks(playlistID int64) {
	sbWidth := m.getSidebarWidth()
	tracks, _ := db.GetMusicPlaylistTracks(playlistID)
	m.trackList = newTrackListFromTracks(m.width-sbWidth-1, m.height-6, tracks)
	m.syncFocus()
}

func (m model) getCurrentFilter() (string, string) {
	selected := m.sidebar.SelectedNode()
	if selected == nil {
		return "", ""
	}

	artist := ""
	album := ""

	if strings.HasPrefix(selected.id, "artist:") {
		artist = strings.TrimPrefix(selected.id, "artist:")
	} else if strings.HasPrefix(selected.id, "album:") {
		album = selected.label
		// Find parent artist node
		for _, row := range m.sidebar.visibleRows {
			for _, child := range row.children {
				if child.id == selected.id {
					artist = strings.TrimPrefix(row.id, "artist:")
					break
				}
			}
			if artist != "" {
				break
			}
		}
	}

	return artist, album
}

func (m *model) refreshTrackList(artist, albumTitle string) {
	sbWidth := m.getSidebarWidth()
	m.trackList = newTrackList(m.width-sbWidth-1, m.height-6, artist, albumTitle)
	m.syncFocus()
}

func (m *model) refreshVideoList() {
	sbWidth := m.getSidebarWidth()
	m.videoList = newVideoList(m.width-sbWidth-1, m.height-6)
	m.syncFocus()
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Every(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type scanFinishedMsg struct {
	count int
}

func (m model) scanMusicCmd() tea.Cmd {
	return func() tea.Msg {
		tracks, err := library.ScanMusic(m.config.Music.Directories)
		if err != nil {
			return scanFinishedMsg{count: 0}
		}

		for _, t := range tracks {
			db.UpdateMusicTrack(db.TrackData{
				Path:        t.Path,
				MTime:       t.MTime,
				Title:       t.Title,
				Artist:      t.Artist,
				Album:       t.Album,
				AlbumArtist: t.AlbumArtist,
				TrackNum:    t.TrackNum,
				DiscNum:     t.DiscNum,
				Genre:       t.Genre,
				Duration:    t.Duration,
			})
		}

		return scanFinishedMsg{count: len(tracks)}
	}
}

func (m model) scanVideoCmd() tea.Cmd {
	return func() tea.Msg {
		videos, err := library.ScanVideo(m.config.Video.Directories)
		if err != nil {
			return scanFinishedMsg{count: 0}
		}

		for _, v := range videos {
			db.UpdateVideoFile(db.VideoData{
				Path:     v.Path,
				Filename: v.Filename,
				Size:     v.Size,
				Duration: v.Duration,
				Year:     v.Year,
				MTime:    v.MTime,
			})
		}

		return scanFinishedMsg{count: len(videos)}
	}
}

func formatDuration(seconds int) string {
	mins := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	header := m.renderHeader()
	sbView := m.sidebar.View(m.focusedSide)
	sbWidth := m.getSidebarWidth()

	mainWidth := m.width - sbWidth - 1
	if mainWidth <= 0 {
		mainWidth = 1
	}

	mainStyle := lipgloss.NewStyle().
		Width(mainWidth).
		Height(m.height - 8). // Adjusted for header and footer
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("240"))

	// Highlight focused area
	if m.focusedSide {
		// sidebar handles its own focus styling
	} else {
		mainStyle = mainStyle.BorderForeground(lipgloss.Color("5"))
	}

	var mainContentStr string
	switch m.activeView {
	case viewMusicLibrary:
		mainContentStr = m.trackList.View()
	case viewMusicArtists:
		mainContentStr = m.musicArtists.View()
	case viewMusicArtistDetail:
		mainContentStr = m.artistDetail.View()
	case viewVideoLibrary:
		mainContentStr = m.videoList.View()
	default:
		mainContentStr = "Unknown View"
	}

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr)),
		footer,
	)
}

func (m model) renderHeader() string {
	if m.currentTrack == "" {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(3).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 2).
			Render("Nothing playing")
	}

	percent := 0.0
	if m.duration > 0 {
		percent = m.playbackPos / m.duration
	}
	
	progressStr := m.progress.ViewAs(percent)
	timeStr := fmt.Sprintf("%s / %s", formatDuration(int(m.playbackPos)), formatDuration(int(m.duration)))
	
	volStr := fmt.Sprintf("Vol: %d%%", int(m.volume))

	infoLine := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render(m.currentTrack),
		"  |  ",
		timeStr,
		"  |  ",
		volStr,
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		infoLine,
		progressStr,
	)

	return lipgloss.NewStyle().
		Width(m.width).
		Height(3).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render(content)
}

func (m model) renderFooter() string {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)

	keys := []struct {
		key, desc string
	}{
		{"q", "Quit"},
		{"TAB", "Focus"},
		{"ENTER", "Play"},
		{"SPACE", "Pause"},
		{"s", "Scan"},
		{"←→", "Seek"},
		{"9/0", "Vol"},
		{"a", "Add"},
		{"d", "Del"},
		{"n", "New"},
	}

	var helpParts []string
	for _, k := range keys {
		helpParts = append(helpParts, fmt.Sprintf("%s %s", keyStyle.Render(k.key), helpStyle.Render(k.desc)))
	}

	helpStr := strings.Join(helpParts, "  ")

	return lipgloss.NewStyle().
		Width(m.width).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render(helpStr)
}
