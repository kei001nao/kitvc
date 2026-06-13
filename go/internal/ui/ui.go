package ui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kitvc/internal/config"
	"kitvc/internal/db"
	"kitvc/internal/library"
	"kitvc/internal/player"
	"kitvc/internal/tmdb"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type viewState int

const (
	viewMusicLibrary viewState = iota
	viewVideoLibrary
	viewMusicQueue
	viewMusicArtists
	viewMusicArtistDetail
	viewMusicRecent
	viewMusicFilter
	viewVideoFilter
	viewVideoContinue
	viewVideoRecent
	viewVideoHealth
)

var viewStateNames = map[viewState]string{
	viewMusicLibrary:      "music_library",
	viewVideoLibrary:      "video_library",
	viewMusicQueue:        "music_queue",
	viewMusicArtists:      "music_artists",
	viewMusicArtistDetail: "music_artist_detail",
	viewMusicRecent:       "music_recent",
	viewMusicFilter:       "music_filter",
	viewVideoFilter:       "video_filter",
	viewVideoContinue:     "video_continue",
	viewVideoRecent:       "video_recent",
	viewVideoHealth:       "video_health",
}

var viewStateFromName = map[string]viewState{
	"music_library":      viewMusicLibrary,
	"video_library":      viewVideoLibrary,
	"music_queue":        viewMusicQueue,
	"music_artists":      viewMusicArtists,
	"music_artist_detail": viewMusicArtistDetail,
	"music_recent":       viewMusicRecent,
	"music_filter":       viewMusicFilter,
	"video_filter":       viewVideoFilter,
	"video_continue":     viewVideoContinue,
	"video_recent":       viewVideoRecent,
	"video_health":       viewVideoHealth,
}

type pendingAction int

const (
	actionNone pendingAction = iota
	actionCreatePlaylist
	actionAddToPlaylist
	actionDeletePlaylist
	actionRemoveTrack
	actionEditTrack
	actionEditAlbum
	actionCreateMusicFilter
	actionEditMusicFilter
	actionDeleteMusicFilter
	actionCreateVideoFilter
	actionEditVideoFilter
	actionDeleteVideoFilter
	actionEditVideo
	actionBatchEditVideo
)

type model struct {
	config            *config.Config
	player            *player.MpvPlayer
	width             int
	height            int
	message           string
	sidebar           sidebar
	trackList         trackList
	videoList         videoList
	musicArtists      musicArtists
	artistDetail      musicArtistDetail
	activeView        viewState
	focusedSide       bool
	currentPlaylistID int64
	currentTrack      string
	progress          progress.Model
	playbackPos       float64
	duration          float64
	volume            float64
	modal             *modal
	pendingAction     pendingAction
	pendingTracks     []string
	editFieldNames    []string
	editPaths         []string
	editAlbumID       int64
	editVideoFieldNames []string
	editVideoPaths      []string
	videoEdit           *videoEditModal
	videoFetch          *videoFetchModal
	playingAlbumID    int64
	currentFilterID   int64
	currentFilterName string
	currentVideoFilterID   int64
	currentVideoFilterName string
	filterEdit        *filterEditModal
	filterCondEdit    *filterConditionModal
	sortFieldSelect   *sortFieldSelectModal

	posterDisplayedPath string
	lastPosterRow       int
	lastPosterCol       int

	videoEditLastPosterRow int
	videoEditLastPosterCol int
	videoEditLastPosterPath string
	videoEditPosterDisplayed bool
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
		config:                   cfg,
		player:                   p,
		progress:                 progress.New(progress.WithDefaultBlend()),
		focusedSide:              true,
		activeView:               viewMusicLibrary,
		sidebar:                  newSidebar(cfg.UI.SidebarWidth, 20),
		lastPosterRow:            -1,
		lastPosterCol:            -1,
		videoEditLastPosterRow:   -1,
		videoEditLastPosterCol:   -1,
		videoEditPosterDisplayed: false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), loadUIStateCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle UI state loaded message
	if msg, ok := msg.(uiStateLoadedMsg); ok {
		m.sidebar.CollapseAll()
		for _, id := range msg.state.ExpandedNodes {
			m.sidebar.ExpandByID(id)
		}
		if msg.state.SelectedNode != "" {
			m.sidebar.SelectByID(msg.state.SelectedNode)
			if n := m.sidebar.SelectedNode(); n != nil {
				m.handleSidebarChange(n)
			}
		}
		m.focusedSide = msg.state.FocusedSide
		if v, ok := viewStateFromName[msg.state.ActiveView]; ok {
			m.activeView = v
		}
		m.syncFocus()
		return m, nil
	}

	// Handle TMDB Metadata and error messages first so they aren't swallowed by modals
	switch msg := msg.(type) {
	case tmdbMetadataMsg:
		m.applyTMDBMetadata(msg.metadata)
		return m, nil
	case errorMsg:
		if m.videoFetch == nil {
			m.message = string(msg)
			return m, nil
		}
	}

	// Handle filter edit modal
	if m.filterEdit != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			// Handle nested modals first
			if m.filterCondEdit != nil {
				updated, result, cmd := m.filterCondEdit.Update(keyMsg)
				m.filterCondEdit = updated
				if result.closed {
					if result.submitted {
						// Add or update condition
						if m.filterEdit.condCursor >= 0 && m.filterEdit.condCursor < len(m.filterEdit.conditions) {
							m.filterEdit.conditions[m.filterEdit.condCursor] = result.condition
						} else {
							m.filterEdit.conditions = append(m.filterEdit.conditions, result.condition)
						}
					}
					m.filterCondEdit = nil
				}
				return m, cmd
			}
			if m.sortFieldSelect != nil {
				updated, result, cmd := m.sortFieldSelect.Update(keyMsg)
				m.sortFieldSelect = updated
				if result.closed {
					if result.submitted {
						m.filterEdit.sortSequence = append(m.filterEdit.sortSequence, [2]string{result.field, result.direction})
					}
					m.sortFieldSelect = nil
				}
				return m, cmd
			}

			// Handle filter edit actions
			switch keyMsg.String() {
			case "a":
				if m.filterEdit.focusedSection == 2 {
					// Add condition - set condCursor to -1 so it appends
					m.filterEdit.condCursor = -1
					m.filterCondEdit = newFilterConditionModal(0, 0, "", m.filterEdit.filterFields, m.filterEdit.filterOps)
					m.filterCondEdit.SetSize(m.width, m.height)
					return m, nil
				} else if m.filterEdit.focusedSection == 3 {
					// Add sort field
					usedFields := make(map[string]bool)
					for _, s := range m.filterEdit.sortSequence {
						usedFields[s[0]] = true
					}
					m.sortFieldSelect = newSortFieldSelectModal(usedFields, m.filterEdit.filterFields)
					m.sortFieldSelect.SetSize(m.width, m.height)
					return m, nil
				}
			case "enter":
				if m.filterEdit.focusedSection == 2 && len(m.filterEdit.conditions) > 0 && m.filterEdit.condCursor >= 0 {
					// Edit condition
					c := m.filterEdit.conditions[m.filterEdit.condCursor]
					fieldIdx := 0
					for i, f := range m.filterEdit.filterFields {
						if f.Value == c.Field {
							fieldIdx = i
							break
						}
					}
					opIdx := 0
					for i, o := range m.filterEdit.filterOps {
						if o.Value == c.Op {
							opIdx = i
							break
						}
					}
					m.filterCondEdit = newFilterConditionModal(fieldIdx, opIdx, c.Value, m.filterEdit.filterFields, m.filterEdit.filterOps)
					m.filterCondEdit.SetSize(m.width, m.height)
					return m, nil
				}
			case "d":
				if m.filterEdit.focusedSection == 2 && len(m.filterEdit.conditions) > 0 {
					// Delete condition
					m.filterEdit.conditions = append(m.filterEdit.conditions[:m.filterEdit.condCursor], m.filterEdit.conditions[m.filterEdit.condCursor+1:]...)
					if m.filterEdit.condCursor >= len(m.filterEdit.conditions) {
						m.filterEdit.condCursor = len(m.filterEdit.conditions) - 1
					}
					if m.filterEdit.condCursor < 0 {
						m.filterEdit.condCursor = 0
					}
					return m, nil
				} else if m.filterEdit.focusedSection == 3 && len(m.filterEdit.sortSequence) > 0 && m.filterEdit.sortCursor >= 0 {
					// Delete sort field
					m.filterEdit.sortSequence = append(m.filterEdit.sortSequence[:m.filterEdit.sortCursor], m.filterEdit.sortSequence[m.filterEdit.sortCursor+1:]...)
					if m.filterEdit.sortCursor >= len(m.filterEdit.sortSequence) {
						m.filterEdit.sortCursor = len(m.filterEdit.sortSequence) - 1
					}
					if m.filterEdit.sortCursor < 0 {
						m.filterEdit.sortCursor = 0
					}
					return m, nil
				}
			case "+", "=":
				if m.filterEdit.focusedSection == 3 && len(m.filterEdit.sortSequence) > 0 && m.filterEdit.sortCursor >= 0 {
					// Move sort up
					idx := m.filterEdit.sortCursor
					if idx > 0 {
						m.filterEdit.sortSequence[idx], m.filterEdit.sortSequence[idx-1] = m.filterEdit.sortSequence[idx-1], m.filterEdit.sortSequence[idx]
						m.filterEdit.sortCursor--
					}
					return m, nil
				}
			case "-":
				if m.filterEdit.focusedSection == 3 && len(m.filterEdit.sortSequence) > 0 && m.filterEdit.sortCursor >= 0 {
					// Move sort down
					idx := m.filterEdit.sortCursor
					if idx < len(m.filterEdit.sortSequence)-1 {
						m.filterEdit.sortSequence[idx], m.filterEdit.sortSequence[idx+1] = m.filterEdit.sortSequence[idx+1], m.filterEdit.sortSequence[idx]
						m.filterEdit.sortCursor++
					}
					return m, nil
				}
			}
		}

		updated, result, cmd := m.filterEdit.Update(msg)
		m.filterEdit = updated
		if result.closed {
			if result.submitted {
				if m.pendingAction == actionCreateMusicFilter {
					newID, err := db.CreateMusicFilter(result.name, result.condJSON, result.sortJSON)
					if err == nil {
						m.message = fmt.Sprintf("Created view '%s'", result.name)
						m.currentFilterID = newID
						m.currentFilterName = result.name
						m.sidebar.Refresh()
						m.sidebar.ExpandByID("music_views")
						m.sidebar.SelectByID(fmt.Sprintf("music_filter:%d", newID))
						m.activeView = viewMusicFilter
						m.refreshFilterTracks(newID)
					}
				} else if m.pendingAction == actionEditMusicFilter {
					db.UpdateMusicFilter(m.currentFilterID, result.name, result.condJSON, result.sortJSON)
					m.message = fmt.Sprintf("Updated view '%s'", result.name)
					m.currentFilterName = result.name
					m.sidebar.Refresh()
					m.sidebar.ExpandByID("music_views")
					m.sidebar.SelectByID(fmt.Sprintf("music_filter:%d", m.currentFilterID))
					m.refreshFilterTracks(m.currentFilterID)
				} else if m.pendingAction == actionCreateVideoFilter {
					newID, err := db.CreateVideoFilter(result.name, result.condJSON, result.sortJSON)
					if err == nil {
						m.message = fmt.Sprintf("Created view '%s'", result.name)
						m.currentVideoFilterID = newID
						m.currentVideoFilterName = result.name
						m.sidebar.Refresh()
						m.sidebar.ExpandByID("video_views")
						m.sidebar.SelectByID(fmt.Sprintf("video_filter:%d", newID))
						m.activeView = viewVideoFilter
						m.refreshVideoFilterTracks(newID)
					}
				} else if m.pendingAction == actionEditVideoFilter {
					db.UpdateVideoFilter(m.currentVideoFilterID, result.name, result.condJSON, result.sortJSON)
					m.message = fmt.Sprintf("Updated view '%s'", result.name)
					m.currentVideoFilterName = result.name
					m.sidebar.Refresh()
					m.sidebar.ExpandByID("video_views")
					m.sidebar.SelectByID(fmt.Sprintf("video_filter:%d", m.currentVideoFilterID))
					m.refreshVideoFilterTracks(m.currentVideoFilterID)
				}
			}
			m.filterEdit = nil
			m.filterCondEdit = nil
			m.sortFieldSelect = nil
			m.pendingAction = actionNone
		}
		return m, cmd
	}

	if m.modal != nil {
		updated, result, cmd := m.modal.Update(msg)
		m.modal = updated
		if _, ok := msg.(tickMsg); ok {
			cmd = tea.Batch(cmd, tick())
		}
		if result.closed {
			if result.submitted {
				m = m.handleModalSubmit(result)
				if m.modal != nil {
					return m, cmd
				}
			}
			m.modal = nil
			m.pendingAction = actionNone
			m.pendingTracks = nil
			m.editFieldNames = nil
			m.editPaths = nil
			m.editAlbumID = 0
			m.editVideoFieldNames = nil
			m.editVideoPaths = nil
		}
		return m, cmd
	}

	if m.videoFetch != nil {
		var cmd tea.Cmd
		m.videoFetch, cmd = m.videoFetch.Update(msg)
		if m.videoFetch.Cancelled {
			clearCmd := m.posterClearCmd()
			m.videoFetch = nil
			m.posterDisplayedPath = ""
			return m, clearCmd
		}
		if m.videoFetch.Submitted {
			clearCmd := m.posterClearCmd()
			m, fetchCmd := m.handleVideoFetchSubmit()
			m.videoFetch = nil
			m.posterDisplayedPath = ""
			return m, tea.Batch(fetchCmd, clearCmd)
		}
		// Trigger chafa poster display if poster is ready and not yet displayed
		var posterCmd tea.Cmd
		if path := m.videoFetch.PosterPath(); path != "" {
			row, col, _, _ := m.posterPos()
			if path != m.posterDisplayedPath || row != m.lastPosterRow || col != m.lastPosterCol {
				m.posterDisplayedPath = path
				posterCmd = m.posterDisplayCmd()
			}
		} else if m.posterDisplayedPath != "" {
			m.posterDisplayedPath = ""
		}
		return m, tea.Batch(cmd, posterCmd)
	}

	if m.videoEdit != nil {
		var cmd tea.Cmd
		m.videoEdit, cmd = m.videoEdit.Update(msg)
		if _, ok := msg.(tickMsg); ok {
			cmd = tea.Batch(cmd, tick())
		}
		if m.videoEdit.cancelled {
			clearCmd := m.videoEditPosterClearCmd()
			m.videoEdit = nil
			m.pendingAction = actionNone
			m.editVideoFieldNames = nil
			m.editVideoPaths = nil
			m.videoEditPosterDisplayed = false
			return m, tea.Batch(cmd, clearCmd)
		}
		if m.videoEdit.submitted {
			clearCmd := m.videoEditPosterClearCmd()
			values := m.videoEdit.Values()
			m = m.handleVideoEditSubmit(values)
			m.videoEdit = nil
			m.videoEditPosterDisplayed = false
			return m, tea.Batch(cmd, clearCmd)
		}

		if m.videoEdit.searchTMDB {
			m.videoEdit.searchTMDB = false
			clearCmd := m.videoEditPosterClearCmd()
			query := ""
			isTV := false
			initialSeason := 0
			initialEpisode := 0

			// Extract current values from videoEdit fields
			values := m.videoEdit.Values()
			// type is at 0, series at 5, title at 4, season at 6, episode at 7
			if values[0] == "TV Show" {
				isTV = true
				query = values[5] // Series
				if query == "" {
					query = values[4] // Title
				}
				initialSeason, _ = strconv.Atoi(values[6])
				initialEpisode, _ = strconv.Atoi(values[7])
			} else {
				query = values[4] // Title
			}

			apiKey := m.config.Video.TMDBAPIKey
			if apiKey == "" {
				apiKey = os.Getenv("TMDB_API_KEY")
			}

			if apiKey == "" {
				m.message = "TMDB API Key not set in config.toml or TMDB_API_KEY env"
				return m, tea.Batch(cmd, clearCmd)
			}

			m.videoFetch = newVideoFetchModal(apiKey, query, isTV, initialSeason, initialEpisode)
			m.videoFetch.SetSize(m.width, m.height-10)
			m.videoEditPosterDisplayed = false
			return m, tea.Batch(cmd, clearCmd)
		}

		// Trigger chafa poster display if poster is ready and overlay position is set
		var posterCmd tea.Cmd
		if path := m.videoEdit.PosterPath(); path != "" {
			sl := m.videoEdit.OverlayStartLine()
			if sl >= 0 {
				row, col, _, _ := m.videoEditPosterPos()
				needsDisplay := !m.videoEditPosterDisplayed
				needsDisplay = needsDisplay || row != m.videoEditLastPosterRow || col != m.videoEditLastPosterCol
				needsDisplay = needsDisplay || path != m.videoEditLastPosterPath
				if needsDisplay {
					posterCmd = m.videoEditPosterDisplayCmd()
					m.videoEditPosterDisplayed = true
					m.videoEditLastPosterPath = path
				}
			}
		} else {
			m.videoEditPosterDisplayed = false
			m.videoEditLastPosterPath = ""
		}
		return m, tea.Batch(cmd, posterCmd)
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tmdbMetadataMsg:
		m.applyTMDBMetadata(msg.metadata)
		var posterCmd tea.Cmd
		if m.videoEdit != nil {
			if path := m.videoEdit.PosterPath(); path != "" {
				sl := m.videoEdit.OverlayStartLine()
				if sl >= 0 {
					row, col, _, _ := m.videoEditPosterPos()
					needsDisplay := !m.videoEditPosterDisplayed
					needsDisplay = needsDisplay || row != m.videoEditLastPosterRow || col != m.videoEditLastPosterCol
					if needsDisplay {
						log.Printf("tmdbMetadataMsg: triggering posterDisplayCmd for path=%s at row=%d col=%d", path, row, col)
						posterCmd = m.videoEditPosterDisplayCmd()
						m.videoEditPosterDisplayed = true
					} else {
						log.Printf("tmdbMetadataMsg: poster already displayed at this position")
					}
				} else {
					log.Printf("tmdbMetadataMsg: overlay position not set yet, will display on next update")
				}
			}
		}
		return m, posterCmd
	case errorMsg:
		m.message = string(msg)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.player != nil {
				m.player.Stop()
			}
			m.saveUIState()
			return m, tea.Quit
		case "tab":
			m.focusedSide = !m.focusedSide
			m.syncFocus()
			return m, nil
		case "left":
			if !m.focusedSide && m.player != nil && m.player.IsRunning() {
				m.player.Seek(-5)
				return m, nil
			}
		case "right":
			if !m.focusedSide && m.player != nil && m.player.IsRunning() {
				m.player.Seek(5)
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
			if m.activeView == viewVideoLibrary || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
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
					selected := m.trackList.table.HighlightedRow()
					if selected.Data != nil {
						artist, album := m.getCurrentFilter()
						tracks, _ := db.GetMusicTracks(artist, album)

						var paths []string
						startIndex := -1
						for i, t := range tracks {
							paths = append(paths, t.Path)
							selTitle, _ := selected.Data[trackColTitle].(string)
							selArtist, _ := selected.Data[trackColArtist].(string)
							if t.Title == selTitle && t.Artist == selArtist {
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
				} else if m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					selected := m.trackList.table.HighlightedRow()
					if selected.Data != nil {
						tracks := m.trackList.tracks

						var paths []string
						startIndex := -1
						for i, t := range tracks {
							paths = append(paths, t.Path)
							selTitle, _ := selected.Data[trackColTitle].(string)
							selArtist, _ := selected.Data[trackColArtist].(string)
							if t.Title == selTitle && t.Artist == selArtist {
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
							m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-8, selectedArtist, albums[selectedArtist])
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
				} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
					startIndex := m.videoList.table.GetHighlightedRowIndex()
					videos := m.videoList.videos
					if startIndex >= 0 && startIndex < len(videos) {
						var paths []string
						for _, v := range videos {
							paths = append(paths, v.Path)
						}
						m.player.SetProperty("vid", "auto")
						if m.config.Video.Fullscreen {
							m.player.SetProperty("fullscreen", "yes")
						}
						m.player.PlayQueue(paths, startIndex)
						v := videos[startIndex]
						m.currentTrack = v.Filename
						m.duration = float64(v.Duration)
					}
					return m, nil
				}
			}
		case "space": // Play/Pause
			if m.player != nil {
				m.player.CyclePause()
			}
			return m, nil
		case "d":
			if !m.focusedSide && m.currentPlaylistID > 0 {
				if m.activeView == viewMusicLibrary {
					markedPaths := m.trackList.MarkedPaths()
					if len(markedPaths) > 0 {
						m.pendingAction = actionRemoveTrack
						m.pendingTracks = markedPaths
						m.modal = newConfirmModal(fmt.Sprintf("Remove %d marked tracks from the playlist?", len(markedPaths)))
						if m.modal != nil {
							m.modal.SetSize(m.width, m.height)
						}
					} else {
					selected := m.trackList.table.HighlightedRow()
					if selected.Data != nil {
						tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
						for _, t := range tracks {
							selTitle, _ := selected.Data[trackColTitle].(string)
							selArtist, _ := selected.Data[trackColArtist].(string)
							if t.Title == selTitle && t.Artist == selArtist {
									m.pendingAction = actionRemoveTrack
									m.pendingTracks = []string{t.Path}
									m.modal = newConfirmModal("Remove this track from the playlist?")
									if m.modal != nil {
										m.modal.SetSize(m.width, m.height)
									}
									break
								}
							}
						}
					}
				}
				return m, nil
			} else if !m.focusedSide && m.activeView == viewMusicFilter && m.currentFilterID > 0 {
				m.pendingAction = actionDeleteMusicFilter
				m.modal = newConfirmModal(fmt.Sprintf("Delete view '%s'?", m.currentFilterName))
				m.modal.SetSize(m.width, m.height)
				return m, nil
		} else if !m.focusedSide && m.activeView == viewVideoFilter && m.currentVideoFilterID > 0 {
				m.pendingAction = actionDeleteVideoFilter
				m.modal = newConfirmModal(fmt.Sprintf("Delete view '%s'?", m.currentVideoFilterName))
				m.modal.SetSize(m.width, m.height)
				return m, nil
		} else if m.focusedSide {
			sel := m.sidebar.SelectedNode()
			if sel != nil && strings.HasPrefix(sel.id, "music_playlist:") {
				m.pendingAction = actionDeletePlaylist
				m.modal = newConfirmModal("Delete playlist '" + sel.label + "'?")
				m.modal.SetSize(m.width, m.height)
			} else if sel != nil && strings.HasPrefix(sel.id, "music_filter:") {
				idStr := strings.TrimPrefix(sel.id, "music_filter:")
				id, _ := strconv.ParseInt(idStr, 10, 64)
				m.currentFilterID = id
				m.currentFilterName = sel.label
				m.pendingAction = actionDeleteMusicFilter
				m.modal = newConfirmModal(fmt.Sprintf("Delete view '%s'?", sel.label))
				m.modal.SetSize(m.width, m.height)
			} else if sel != nil && strings.HasPrefix(sel.id, "video_filter:") {
				idStr := strings.TrimPrefix(sel.id, "video_filter:")
				id, _ := strconv.ParseInt(idStr, 10, 64)
				m.currentVideoFilterID = id
				m.currentVideoFilterName = sel.label
				m.pendingAction = actionDeleteVideoFilter
				m.modal = newConfirmModal(fmt.Sprintf("Delete view '%s'?", sel.label))
				m.modal.SetSize(m.width, m.height)
			}
			return m, nil
			}
		case "shift+up":
			if !m.focusedSide && m.currentPlaylistID > 0 && m.activeView == viewMusicLibrary {
				tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
				if len(tracks) > 0 {
					cursor := m.trackList.table.GetHighlightedRowIndex()
					if cursor > 0 {
						db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor-1)
						m.refreshPlaylistTracks(m.currentPlaylistID)
						m.trackList.table = m.trackList.table.WithHighlightedRow(cursor - 1)
					}
				}
				return m, nil
			}
		case "shift+down":
			if !m.focusedSide && m.currentPlaylistID > 0 && m.activeView == viewMusicLibrary {
				tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
				if len(tracks) > 0 {
					cursor := m.trackList.table.GetHighlightedRowIndex()
					if cursor < len(tracks)-1 {
						db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor+1)
						m.refreshPlaylistTracks(m.currentPlaylistID)
						m.trackList.table = m.trackList.table.WithHighlightedRow(cursor + 1)
					}
				}
				return m, nil
			}
		case "D":
			if !m.focusedSide && m.currentPlaylistID > 0 {
				sel := m.sidebar.SelectedNode()
				if sel != nil {
					m.pendingAction = actionDeletePlaylist
					m.modal = newConfirmModal("Delete playlist '" + sel.label + "'?")
					m.modal.SetSize(m.width, m.height)
				}
				return m, nil
			}
		case "a":
			if !m.focusedSide {
				var hasSelection bool
				var tracksToAdd []string
				if m.activeView == viewMusicLibrary {
					// use marked tracks when present
					markedPaths := m.trackList.MarkedPaths()
					if len(markedPaths) > 0 {
						tracksToAdd = markedPaths
						hasSelection = true
					} else {
						selected := m.trackList.table.HighlightedRow()
						if selected.Data != nil {
							artist, album := m.getCurrentFilter()
							tracks, _ := db.GetMusicTracks(artist, album)
							for _, t := range tracks {
								selTitle, _ := selected.Data[trackColTitle].(string)
								selArtist, _ := selected.Data[trackColArtist].(string)
								if t.Title == selTitle && t.Artist == selArtist {
									tracksToAdd = []string{t.Path}
									hasSelection = true
									break
								}
							}
						}
					}
				} else if m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					markedPaths := m.trackList.MarkedPaths()
					if len(markedPaths) > 0 {
						tracksToAdd = markedPaths
						hasSelection = true
					} else {
						selected := m.trackList.table.HighlightedRow()
						if selected.Data != nil {
							for _, t := range m.trackList.tracks {
								selTitle, _ := selected.Data[trackColTitle].(string)
								selArtist, _ := selected.Data[trackColArtist].(string)
								if t.Title == selTitle && t.Artist == selArtist {
									tracksToAdd = []string{t.Path}
									hasSelection = true
									break
								}
							}
						}
					}
				} else if m.activeView == viewMusicArtistDetail {
					if m.artistDetail.focusedUpper {
						albumTitle, ok := m.artistDetail.SelectedAlbum()
						if ok {
							tracks, _ := db.GetMusicTracks(m.artistDetail.artist, albumTitle)
							tracksToAdd = make([]string, len(tracks))
							for i, t := range tracks {
								tracksToAdd[i] = t.Path
							}
							hasSelection = true
						}
					} else {
						// use marked tracks when present
						markedPaths := m.artistDetail.MarkedTracks()
						if len(markedPaths) > 0 {
							tracksToAdd = markedPaths
							hasSelection = true
						} else {
							track, ok := m.artistDetail.SelectedTrack()
							if ok {
								tracksToAdd = []string{track.Path}
								hasSelection = true
							}
						}
					}
				}
				if hasSelection {
					m.pendingAction = actionAddToPlaylist
					m.pendingTracks = tracksToAdd
					playlists, _ := db.GetMusicPlaylists()
					items := make([]string, 0, len(playlists)+1)
					items = append(items, "[New Playlist...]")
				for _, p := range playlists {
					items = append(items, p.Name)
				}
					m.modal = newListSelectModal("Add to Playlist", items, "↑↓: Select  Enter: Confirm  Esc: Cancel")
					m.modal.SetSize(m.width, m.height)
				}
				return m, nil
			}
		case "n":
			if m.focusedSide {
				sel := m.sidebar.SelectedNode()
				if sel != nil && sel.id == "music_views" {
					m.pendingAction = actionCreateMusicFilter
					m.filterEdit = newFilterEditModal("", "[]", "[]", musicFilterFields, musicFilterOps)
					m.filterEdit.SetSize(m.width, m.height)
				} else if sel != nil && sel.id == "video_views" {
					m.pendingAction = actionCreateVideoFilter
					m.filterEdit = newFilterEditModal("", "[]", "[]", videoFilterFields, videoFilterOps)
					m.filterEdit.SetSize(m.width, m.height)
				}
			}
			return m, nil
		case "e":
			if m.focusedSide {
				sel := m.sidebar.SelectedNode()
				if sel != nil && strings.HasPrefix(sel.id, "music_filter:") {
					idStr := strings.TrimPrefix(sel.id, "music_filter:")
					id, _ := strconv.ParseInt(idStr, 10, 64)
					filter, err := db.GetMusicFilterByID(id)
					if err == nil {
						m.currentFilterID = id
						m.pendingAction = actionEditMusicFilter
						m.filterEdit = newFilterEditModal(filter.Name, filter.ConditionsJSON, filter.SortJSON, musicFilterFields, musicFilterOps)
						m.filterEdit.SetSize(m.width, m.height)
					}
				} else if sel != nil && strings.HasPrefix(sel.id, "video_filter:") {
					idStr := strings.TrimPrefix(sel.id, "video_filter:")
					id, _ := strconv.ParseInt(idStr, 10, 64)
					filter, err := db.GetVideoFilterByID(id)
					if err == nil {
						m.currentVideoFilterID = id
						m.pendingAction = actionEditVideoFilter
						m.filterEdit = newFilterEditModal(filter.Name, filter.ConditionsJSON, filter.SortJSON, videoFilterFields, videoFilterOps)
						m.filterEdit.SetSize(m.width, m.height)
					}
				}
			} else if !m.focusedSide && m.activeView == viewMusicFilter && m.currentFilterID > 0 {
				filter, err := db.GetMusicFilterByID(m.currentFilterID)
				if err == nil {
					m.pendingAction = actionEditMusicFilter
					m.filterEdit = newFilterEditModal(filter.Name, filter.ConditionsJSON, filter.SortJSON, musicFilterFields, musicFilterOps)
					m.filterEdit.SetSize(m.width, m.height)
				}
			} else if !m.focusedSide && m.activeView == viewVideoFilter && m.currentVideoFilterID > 0 {
				filter, err := db.GetVideoFilterByID(m.currentVideoFilterID)
				if err == nil {
					m.pendingAction = actionEditVideoFilter
					m.filterEdit = newFilterEditModal(filter.Name, filter.ConditionsJSON, filter.SortJSON, videoFilterFields, videoFilterOps)
					m.filterEdit.SetSize(m.width, m.height)
				}
			} else if !m.focusedSide {
				videoViews := map[viewState]bool{
					viewVideoLibrary: true, viewVideoContinue: true,
					viewVideoRecent: true, viewVideoHealth: true, viewVideoFilter: true,
				}
				if videoViews[m.activeView] {
					markedPaths := m.videoList.MarkedPaths()
					if len(markedPaths) > 0 {
						markedVideos := m.videoList.MarkedVideos()
						m.editVideoPaths = markedPaths
						m.editVideoFieldNames = videoEditFieldNames
						m.pendingAction = actionBatchEditVideo
						initialValues := videoBatchEditInitialValues(markedVideos)
						m.videoEdit = newVideoEditModal("Batch Edit", videoEditLabels, videoEditFieldKinds, initialValues, videoEditOptions)
						m.videoEdit.SetSize(m.width, m.height)
					} else {
						cursor := m.videoList.table.GetHighlightedRowIndex()
						if cursor >= 0 && cursor < len(m.videoList.videos) {
							v := m.videoList.videos[cursor]
							m.editVideoPaths = []string{v.Path}
							m.editVideoFieldNames = videoEditFieldNames
							m.pendingAction = actionEditVideo
							initialValues := videoEditInitialValues(v)
							m.videoEdit = newVideoEditModal(v.Filename, videoEditLabels, videoEditFieldKinds, initialValues, videoEditOptions)
							m.videoEdit.SetThumbnail(v.ThumbnailPath)
							m.videoEdit.SetSize(m.width, m.height)
						}
					}
				} else if m.activeView == viewMusicArtistDetail && m.artistDetail.focusedUpper {
					cursor := m.artistDetail.albumsTable.GetHighlightedRowIndex()
					if cursor < 0 || cursor >= len(m.artistDetail.albums) {
						return m, nil
					}
					album := m.artistDetail.albums[cursor]

					m.pendingAction = actionEditAlbum
					m.editAlbumID = album.ID
					m.editFieldNames = []string{"artist", "album", "release_date"}

					allTracks, err := db.GetMusicTracksByAlbumID(album.ID)
					if err != nil {
						allTracks = nil
					}
					m.editPaths = make([]string, len(allTracks))
					for i, t := range allTracks {
						m.editPaths[i] = t.Path
					}

					m.modal = newFormModal("Edit Album",
						[]string{"Artist", "Album", "Date"},
						[]string{album.Artist, album.Title, album.ReleaseDate},
						"Tab: Next  Enter: Save  Esc: Cancel")
					if m.modal != nil {
						m.modal.SetSize(m.width, m.height)
					}
				} else {
					var selectedTracks []db.TrackData
					if m.activeView == viewMusicLibrary {
						marked := m.trackList.MarkedPaths()
						if len(marked) > 0 {
							for _, t := range m.trackList.tracks {
								for _, p := range marked {
									if t.Path == p {
										selectedTracks = append(selectedTracks, t)
										break
									}
								}
							}
						} else {
							cursor := m.trackList.table.GetHighlightedRowIndex()
							if cursor >= 0 && cursor < len(m.trackList.tracks) {
								selectedTracks = []db.TrackData{m.trackList.tracks[cursor]}
							}
						}
					} else if m.activeView == viewMusicArtistDetail && !m.artistDetail.focusedUpper {
						marked := m.artistDetail.MarkedTracks()
						if len(marked) > 0 {
							for _, t := range m.artistDetail.tracks {
								for _, p := range marked {
									if t.Path == p {
										selectedTracks = append(selectedTracks, t)
										break
									}
								}
							}
						} else {
							track, ok := m.artistDetail.SelectedTrack()
							if ok {
								selectedTracks = []db.TrackData{track}
							}
						}
					} else if m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
						cursor := m.trackList.table.GetHighlightedRowIndex()
						if cursor >= 0 && cursor < len(m.trackList.tracks) {
							selectedTracks = []db.TrackData{m.trackList.tracks[cursor]}
						}
					}

					if len(selectedTracks) == 0 {
						return m, nil
					}

					m.editPaths = make([]string, len(selectedTracks))
					for i, t := range selectedTracks {
						m.editPaths[i] = t.Path
					}

					m.editFieldNames = []string{"title", "genre"}
					labels := []string{"Title", "Genre"}
					initialValues := make([]string, 2)
					for i, field := range m.editFieldNames {
						var commonVal string
						allSame := true
						for j, t := range selectedTracks {
							var val string
							switch field {
							case "title":
								val = t.Title
							case "genre":
								val = t.Genre
							}
							if j == 0 {
								commonVal = val
							} else if val != commonVal {
								allSame = false
								break
							}
						}
						if allSame {
							initialValues[i] = commonVal
						}
					}

					m.pendingAction = actionEditTrack
					m.modal = newFormModal(
						fmt.Sprintf("Edit Track (%d)", len(selectedTracks)),
						labels, initialValues,
						"Tab: Next  Enter: Save  Esc: Cancel")
					if m.modal != nil {
						m.modal.SetSize(m.width, m.height)
					}
				}
			}
			return m, nil
		case "m":
			if !m.focusedSide {
				if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					rows := m.trackList.table.GetVisibleRows()
					cursor := m.trackList.table.GetHighlightedRowIndex()
					if cursor < len(rows) {
						m.trackList.marked[cursor] = !m.trackList.marked[cursor]
						currentPath := ""
						isPaused := false
						if m.player != nil {
							currentPath = m.player.GetCurrentTrackPath()
							valPause, _ := m.player.GetProperty("pause")
							if p, ok := valPause.(bool); ok {
								isPaused = p
							}
						}
						m.trackList.UpdatePlaybackStatus(currentPath, isPaused)
						newCursor := cursor + 1
						if newCursor >= len(rows) {
							newCursor = 0
						}
						m.trackList.table = m.trackList.table.WithHighlightedRow(newCursor)
					}
				} else if m.activeView == viewMusicArtistDetail && !m.artistDetail.focusedUpper {
					rows := m.artistDetail.tracksTable.GetVisibleRows()
					cursor := m.artistDetail.tracksTable.GetHighlightedRowIndex()
					if cursor < len(rows) {
						m.artistDetail.marked[cursor] = !m.artistDetail.marked[cursor]
						currentPath := ""
						isPaused := false
						if m.player != nil {
							currentPath = m.player.GetCurrentTrackPath()
							valPause, _ := m.player.GetProperty("pause")
							if p, ok := valPause.(bool); ok {
								isPaused = p
							}
						}
						m.artistDetail.UpdatePlaybackStatus(currentPath, isPaused)
						newCursor := cursor + 1
						if newCursor >= len(rows) {
							newCursor = 0
						}
						m.artistDetail.tracksTable = m.artistDetail.tracksTable.WithHighlightedRow(newCursor)
					}
		} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
					rows := m.videoList.table.GetVisibleRows()
					cursor := m.videoList.table.GetHighlightedRowIndex()
					if cursor < len(rows) {
						m.videoList.marked[cursor] = !m.videoList.marked[cursor]
						currentPath := ""
						isPaused := false
						if m.player != nil {
							currentPath = m.player.GetCurrentTrackPath()
							valPause, _ := m.player.GetProperty("pause")
							if p, ok := valPause.(bool); ok {
								isPaused = p
							}
						}
						m.videoList.UpdatePlaybackStatus(currentPath, isPaused)
						newCursor := cursor + 1
						if newCursor >= len(rows) {
							newCursor = 0
						}
						m.videoList.table = m.videoList.table.WithHighlightedRow(newCursor)
					}
				}
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.SetWidth(m.width - 20)

		sbWidth := m.getSidebarWidth()
		m.sidebar.SetSize(sbWidth, m.height-8)

		mainWidth := m.width - sbWidth - 2
		if mainWidth <= 0 {
			mainWidth = 1
		}

		m.trackList.SetSize(mainWidth-1, m.height-8)
		m.videoList.SetSize(mainWidth-1, m.height-8)
		m.musicArtists.SetSize(mainWidth-1, m.height-8)
		m.artistDetail.SetSize(mainWidth-1, m.height-8)
		if m.videoFetch != nil {
			m.videoFetch.SetSize(m.width-10, m.height-6)
			m.posterDisplayedPath = ""
		}
		if m.modal != nil {
			m.modal.SetSize(m.width, m.height)
		}
		cmds = append(cmds, m.coverPlaceCmd())
	case scanFinishedMsg:

		m.message = fmt.Sprintf("Scan finished: %d items found", msg.count)
		if m.activeView == viewMusicLibrary {
			m.refreshTrackList("", "")
		} else if m.activeView == viewMusicRecent {
			m.refreshRecentTracks()
		} else if m.activeView == viewMusicFilter {
			m.refreshFilterTracks(m.currentFilterID)
		} else if m.activeView == viewVideoLibrary {
			m.refreshVideoList()
		} else if m.activeView == viewVideoContinue {
			m.refreshVideoContinue()
		} else if m.activeView == viewVideoRecent {
			m.refreshVideoRecent()
		} else if m.activeView == viewVideoHealth {
			m.refreshVideoHealth()
		}
		// Update cover for current selection
		if n := m.sidebar.SelectedNode(); n != nil {
			if cmd := m.updateCoverForNode(n); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case tickMsg:
		if m.player != nil {
			if !m.player.IsRunning() {
				if m.playbackPos > 0 && m.player.GetCurrentTrackPath() != "" {
					db.UpdateVideoLastPos(m.player.GetCurrentTrackPath(), m.playbackPos)
				}
				m.trackList.UpdatePlaybackStatus("", false)
				m.artistDetail.UpdatePlaybackStatus("", false)
				m.videoList.UpdatePlaybackStatus("", false)
				m.playbackPos = 0
				m.duration = 0
				m.currentTrack = ""
			} else {
				completedPath := m.player.ConsumeCompletedPath()
				if completedPath != "" {
					db.ClearVideoLastPos(completedPath)
				}
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

				valPause, _ := m.player.GetProperty("pause")
				isPaused := false
				if p, ok := valPause.(bool); ok {
					isPaused = p
				}
				currentPath := m.player.GetCurrentTrackPath()
				m.trackList.UpdatePlaybackStatus(currentPath, isPaused)
				m.artistDetail.UpdatePlaybackStatus(currentPath, isPaused)
				m.videoList.UpdatePlaybackStatus(currentPath, isPaused)

				if currentPath != "" && m.playbackPos > 0 {
					db.UpdateVideoLastPos(currentPath, m.playbackPos)
				}
			}
			if cmd := m.setCoverFromPlaying(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, tick())
	}

	if m.focusedSide {
		var cmd tea.Cmd
		oldNode := m.sidebar.SelectedNode()
		oldRows := m.sidebar.NumVisible()
		m.sidebar, cmd = m.sidebar.Update(msg)
		cmds = append(cmds, cmd)

		newNode := m.sidebar.SelectedNode()
		if oldNode != newNode && newNode != nil {
			cmds = append(cmds, m.handleSidebarChange(newNode))
		} else if m.sidebar.HasCover() && oldRows != m.sidebar.NumVisible() {
			cmds = append(cmds, m.coverPlaceCmd())
		}
	} else {
		if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
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
	} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
			var cmd tea.Cmd
			m.videoList, cmd = m.videoList.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) syncFocus() {
	if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
		m.trackList.SetFocus(!m.focusedSide)
	} else if m.activeView == viewMusicArtists {
		m.musicArtists.SetFocus(!m.focusedSide)
	} else if m.activeView == viewMusicArtistDetail {
		m.artistDetail.SetFocus(!m.focusedSide)
	} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
		m.videoList.SetFocus(!m.focusedSide)
	}
}

func (m *model) updateCoverForNode(n *node) tea.Cmd {
	coverPath := ""
	switch {
	case strings.HasPrefix(n.id, "artist:"):
		artist := strings.TrimPrefix(n.id, "artist:")
		_, albums, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			if albumList := albums[artist]; len(albumList) > 0 {
				coverPath = library.GetCachedCoverPath(albumList[0].ID)
			}
		}
	case strings.HasPrefix(n.id, "album:"):
		parts := strings.Split(n.id, ":")
		if len(parts) == 2 {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				coverPath = library.GetCachedCoverPath(id)
			}
		}
	}

	cols := m.getSidebarWidth() - 2
	if cols < 10 {
		cols = 10
	}
	maxRows := (m.height - 8) - 5
	if maxRows < 6 {
		maxRows = 6
	}
	if m.sidebar.SetCoverPath(coverPath, cols, maxRows) {
		return m.coverDisplayCmd()
	}
	return nil
}

func (m *model) setCoverFromPlaying() tea.Cmd {
	if m.player == nil {
		return nil
	}
	path := m.player.GetCurrentTrackPath()
	if path == "" {
		return nil
	}
	albumID, err := db.GetAlbumIDByTrackPath(path)
	if err != nil {
		return nil
	}
	if albumID == m.playingAlbumID {
		return nil
	}
	m.playingAlbumID = albumID
	coverPath := library.GetCachedCoverPath(albumID)
	if coverPath == "" {
		return nil
	}
	cols := m.getSidebarWidth() - 2
	if cols < 10 {
		cols = 10
	}
	maxRows := (m.height - 8) - 5
	if maxRows < 6 {
		maxRows = 6
	}
	if m.sidebar.SetCoverPath(coverPath, cols, maxRows) {
		return m.coverDisplayCmd()
	}
	return nil
}

func (m *model) handleSidebarChange(n *node) tea.Cmd {
	m.currentPlaylistID = 0
	m.currentFilterID = 0
	m.currentFilterName = ""
	m.currentVideoFilterID = 0
	m.currentVideoFilterName = ""

	switch {
	case n.id == "music_library":
		m.activeView = viewMusicArtists
		artists, _, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			sbWidth := m.getSidebarWidth()
			m.musicArtists = newMusicArtists(m.width-sbWidth-3, m.height-8, artists)
		}
	case n.id == "music_recent":
		m.activeView = viewMusicRecent
		m.refreshRecentTracks()
	case n.id == "video_library":
		m.activeView = viewVideoLibrary
		m.refreshVideoList()
	case n.id == "video_continue":
		m.activeView = viewVideoContinue
		m.refreshVideoContinue()
	case n.id == "video_recent":
		m.activeView = viewVideoRecent
		m.refreshVideoRecent()
	case n.id == "video_health":
		m.activeView = viewVideoHealth
		m.refreshVideoHealth()
	case strings.HasPrefix(n.id, "artist:"):
		artist := strings.TrimPrefix(n.id, "artist:")
		m.activeView = viewMusicArtistDetail
		_, albums, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			sbWidth := m.getSidebarWidth()
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-8, artist, albums[artist])
		}
	case strings.HasPrefix(n.id, "album:"):
		artist, albumTitle := m.getCurrentFilter()
		m.activeView = viewMusicLibrary
		m.refreshTrackList(artist, albumTitle)
	case strings.HasPrefix(n.id, "music_filter:"):
		idStr := strings.TrimPrefix(n.id, "music_filter:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentFilterID = id
		m.currentFilterName = n.label
		m.activeView = viewMusicFilter
		m.refreshFilterTracks(id)
	case strings.HasPrefix(n.id, "music_playlist:"):
		idStr := strings.TrimPrefix(n.id, "music_playlist:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentPlaylistID = id
		m.activeView = viewMusicLibrary
		m.trackList.ClearMarks()
		m.refreshPlaylistTracks(id)
	case strings.HasPrefix(n.id, "video_filter:"):
		idStr := strings.TrimPrefix(n.id, "video_filter:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentVideoFilterID = id
		m.currentVideoFilterName = n.label
		m.activeView = viewVideoFilter
		m.refreshVideoFilterTracks(id)
	case strings.HasPrefix(n.id, "video_playlist:"):
		idStr := strings.TrimPrefix(n.id, "video_playlist:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentPlaylistID = id
		m.activeView = viewVideoLibrary
		m.refreshVideoPlaylistFiles(id)
	}
	m.syncFocus()
	if cmd := m.updateCoverForNode(n); cmd != nil {
		return cmd
	}
	return nil
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
	m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshPlaylistTracks(playlistID int64) {
	sbWidth := m.getSidebarWidth()
	tracks, _ := db.GetMusicPlaylistTracks(playlistID)
	oldMarked := m.trackList.marked
	m.trackList = newTrackListFromTracks(m.width-sbWidth-3, m.height-6, tracks)
	m.trackList.marked = oldMarked
	if m.trackList.marked == nil {
		m.trackList.marked = make(map[int]bool)
	}
	if m.player != nil {
		currentPath := m.player.GetCurrentTrackPath()
		valPause, _ := m.player.GetProperty("pause")
		isPaused := false
		if p, ok := valPause.(bool); ok {
			isPaused = p
		}
		m.trackList.UpdatePlaybackStatus(currentPath, isPaused)
	}
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
	m.trackList = newTrackList(m.width-sbWidth-3, m.height-6, artist, albumTitle)
	m.syncFocus()
}

func (m *model) refreshRecentTracks() {
	sbWidth := m.getSidebarWidth()
	tracks, _ := db.GetRecentMusicTracks(50)
	m.trackList = newTrackListFromTracks(m.width-sbWidth-3, m.height-6, tracks)
	m.syncFocus()
}

func (m *model) refreshFilterTracks(filterID int64) {
	sbWidth := m.getSidebarWidth()
	filter, err := db.GetMusicFilterByID(filterID)
	if err != nil {
		m.trackList = newTrackListFromTracks(m.width-sbWidth-3, m.height-6, nil)
		m.syncFocus()
		return
	}
	tracks, _ := db.GetFilteredMusicTracks(filter.ConditionsJSON, filter.SortJSON)
	m.trackList = newTrackListFromTracks(m.width-sbWidth-3, m.height-6, tracks)
	m.syncFocus()
}

func (m *model) refreshVideoFilterTracks(filterID int64) {
	sbWidth := m.getSidebarWidth()
	filter, err := db.GetVideoFilterByID(filterID)
	if err != nil {
		m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, nil)
		m.syncFocus()
		return
	}
	videos, _ := db.GetFilteredVideos(filter.ConditionsJSON, filter.SortJSON)
	m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, videos)
	m.syncFocus()
}

func (m model) handleModalSubmit(result modalUpdateResult) model {
	switch m.pendingAction {
	case actionCreatePlaylist:
		name := result.text
		db.CreateMusicPlaylist(name)
		m.message = fmt.Sprintf("Created playlist '%s'", name)
		m.sidebar.Refresh()
		if len(m.pendingTracks) > 0 {
			playlists, _ := db.GetMusicPlaylists()
			for _, p := range playlists {
				if p.Name == name {
					for _, tp := range m.pendingTracks {
						db.AddTrackToMusicPlaylist(p.ID, tp)
					}
					m.message = fmt.Sprintf("Created playlist '%s' and added %d track(s)", name, len(m.pendingTracks))
					break
				}
			}
		}
		m.modal = nil

	case actionAddToPlaylist:
		if result.text == "[New Playlist...]" {
			m.pendingAction = actionCreatePlaylist
			m.modal = newTextInputModal(
				"New Playlist Name:",
				"Enter playlist name...",
				"Enter: Create  Esc: Cancel",
			)
			m.modal.SetSize(m.width, m.height)
			return m
		}
		playlists, _ := db.GetMusicPlaylists()
		for _, p := range playlists {
			if p.Name == result.text {
				for _, tp := range m.pendingTracks {
					db.AddTrackToMusicPlaylist(p.ID, tp)
				}
				m.message = fmt.Sprintf("Added to '%s'", p.Name)
				break
			}
		}
		m.trackList.ClearMarks()
		m.artistDetail.ClearMarks()
		m.modal = nil

	case actionDeletePlaylist:
		db.DeleteMusicPlaylist(m.currentPlaylistID)
		m.message = "Deleted playlist"
		m.currentPlaylistID = 0
		m.sidebar.Refresh()
		m.trackList.ClearMarks()
		m.artistDetail.ClearMarks()
		m.modal = nil

	case actionRemoveTrack:
		for _, tp := range m.pendingTracks {
			db.RemoveTrackFromMusicPlaylist(m.currentPlaylistID, tp)
		}
		m.message = "Removed from playlist"
		m.refreshPlaylistTracks(m.currentPlaylistID)
		m.trackList.ClearMarks()
		m.modal = nil

	case actionEditTrack:
		for i, field := range m.editFieldNames {
			val := strings.TrimSpace(result.values[i])
			allowEmpty := field == "genre"
			if val == "" && !allowEmpty {
				continue
			}

			for _, path := range m.editPaths {
				if err := db.UpdateTrackField(path, field, val); err != nil {
					log.Printf("Failed to update track %s: %v", path, err)
					continue
				}
			}

			for _, path := range m.editPaths {
				tags := map[string]string{field: val}
				if err := library.WriteAudioTags(path, tags); err != nil {
					log.Printf("Failed to write tags to %s: %v", path, err)
				}
				db.UpdateTrackMTime(path, float64(time.Now().Unix()))
			}
		}
		m.message = fmt.Sprintf("Updated %d track(s)", len(m.editPaths))
		m.modal = nil
		m.editFieldNames = nil
		m.editPaths = nil

		if m.activeView == viewMusicLibrary {
			artist, album := m.getCurrentFilter()
			m.refreshTrackList(artist, album)
		} else if m.activeView == viewMusicArtistDetail {
			sbWidth := m.getSidebarWidth()
			albumCursor := m.artistDetail.albumsTable.GetHighlightedRowIndex()
			trackCursor := m.artistDetail.tracksTable.GetHighlightedRowIndex()
			wasFocusedUpper := m.artistDetail.focusedUpper
			wasFocused := m.focusedSide
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-8, m.artistDetail.artist, m.artistDetail.albums)
			if albumCursor >= 0 && albumCursor < len(m.artistDetail.albums) {
				m.artistDetail.albumsTable = m.artistDetail.albumsTable.WithHighlightedRow(albumCursor)
				m.artistDetail.loadTracksForAlbum(m.artistDetail.albums[albumCursor].Title)
				if trackCursor >= 0 && trackCursor < len(m.artistDetail.tracks) {
					m.artistDetail.tracksTable = m.artistDetail.tracksTable.WithHighlightedRow(trackCursor)
				}
			}
			m.artistDetail.focusedUpper = wasFocusedUpper
			m.artistDetail.SetFocus(!wasFocused)
		} else if m.activeView == viewMusicRecent {
			m.refreshRecentTracks()
		} else if m.activeView == viewMusicFilter {
			m.refreshFilterTracks(m.currentFilterID)
		}

	case actionEditAlbum:
		newArtist := strings.TrimSpace(result.values[0])
		newAlbum := strings.TrimSpace(result.values[1])
		newDate := strings.TrimSpace(result.values[2])

		if newArtist == "" {
			m.message = "Artist name cannot be empty"
			m.modal = nil
			m.editFieldNames = nil
			m.editPaths = nil
			return m
		}

		if err := db.UpdateAlbumMetadata(m.editAlbumID, newArtist, newAlbum, newDate); err != nil {
			m.message = fmt.Sprintf("Failed to update album: %v", err)
			log.Printf("UpdateAlbumMetadata failed: %v", err)
			m.modal = nil
			m.editFieldNames = nil
			m.editPaths = nil
			return m
		}

		for _, path := range m.editPaths {
			tags := map[string]string{
				"artist": newArtist,
				"album":  newAlbum,
				"date":   newDate,
			}
			if err := library.WriteAudioTags(path, tags); err != nil {
				log.Printf("Failed to write tags to %s: %v", path, err)
			}
			db.UpdateTrackMTime(path, float64(time.Now().Unix()))
		}

		m.message = fmt.Sprintf("Updated album: %s - %s", newArtist, newAlbum)

		// Save expanded state of category nodes and selected node ID
		expandedMap := make(map[string]bool)
		for _, n := range m.sidebar.nodes {
			expandedMap[n.id] = n.expanded
		}
		savedID := ""
		if n := m.sidebar.SelectedNode(); n != nil {
			savedID = n.id
		}

		m.sidebar.Refresh()

		// Restore expanded states
		for _, n := range m.sidebar.nodes {
			if exp, ok := expandedMap[n.id]; ok {
				n.expanded = exp
			}
		}
		m.sidebar.rebuildVisible()

		// Restore cursor: try exact ID match
		if savedID != "" {
			found := m.sidebar.SelectByID(savedID)
			if !found && strings.HasPrefix(savedID, "artist:") {
				m.sidebar.SelectByID("artist:" + newArtist)
			} else if !found {
				albumIDStr := fmt.Sprintf("album:%d", m.editAlbumID)
				m.sidebar.SelectByID(albumIDStr)
			}
		}

		// Sync main view with the restored sidebar selection
		if n := m.sidebar.SelectedNode(); n != nil {
			// Preserve artist detail cursor positions if we're in artist detail view
			if m.activeView == viewMusicArtistDetail {
				savedAlbumCursor := m.artistDetail.albumsTable.GetHighlightedRowIndex()
				savedTrackCursor := m.artistDetail.tracksTable.GetHighlightedRowIndex()
				savedFocusedUpper := m.artistDetail.focusedUpper
				m.handleSidebarChange(n)
				if m.activeView == viewMusicArtistDetail {
					if savedAlbumCursor >= 0 && savedAlbumCursor < len(m.artistDetail.albums) {
						m.artistDetail.albumsTable = m.artistDetail.albumsTable.WithHighlightedRow(savedAlbumCursor)
						m.artistDetail.loadTracksForAlbum(m.artistDetail.albums[savedAlbumCursor].Title)
						if savedTrackCursor >= 0 && savedTrackCursor < len(m.artistDetail.tracks) {
							m.artistDetail.tracksTable = m.artistDetail.tracksTable.WithHighlightedRow(savedTrackCursor)
						}
					}
					m.artistDetail.focusedUpper = savedFocusedUpper
					m.artistDetail.syncTableFocus()
				}
			} else {
				m.handleSidebarChange(n)
			}
		}

		m.modal = nil
		m.editFieldNames = nil
		m.editPaths = nil
		m.editAlbumID = 0

	case actionCreateMusicFilter:
		m.modal = nil

	case actionDeleteMusicFilter:
		db.DeleteMusicFilter(m.currentFilterID)
		m.message = fmt.Sprintf("Deleted view '%s'", m.currentFilterName)
		m.currentFilterID = 0
		m.currentFilterName = ""
		m.activeView = viewMusicArtists
		m.sidebar.Refresh()
		m.modal = nil

	case actionCreateVideoFilter:
		m.modal = nil

	case actionDeleteVideoFilter:
		db.DeleteVideoFilter(m.currentVideoFilterID)
		m.message = fmt.Sprintf("Deleted view '%s'", m.currentVideoFilterName)
		m.currentVideoFilterID = 0
		m.currentVideoFilterName = ""
		m.activeView = viewVideoLibrary
		m.sidebar.Refresh()
		m.modal = nil

	case actionEditVideo, actionBatchEditVideo:
		updated := 0
		for i, field := range m.editVideoFieldNames {
			val := result.values[i]
			if val == "" {
				continue
			}
			for _, path := range m.editVideoPaths {
				if err := db.UpdateVideoField(path, field, val); err == nil {
					updated++
				}
			}
		}
		if updated > 0 {
			m.message = fmt.Sprintf("Updated %d video(s)", len(m.editVideoPaths))
		} else {
			m.message = "No changes"
		}
		m.modal = nil
		m.editVideoFieldNames = nil
		m.editVideoPaths = nil
		m.refreshCurrentVideoView()
	}

	return m
}

type tmdbMetadataMsg struct {
	metadata *tmdb.VideoMetadata
}

func (m model) handleVideoFetchSubmit() (model, tea.Cmd) {
	id := m.videoFetch.SelectedID
	isTV := m.videoFetch.SelectedIsTV
	season := m.videoFetch.SelectedSeason
	episode := m.videoFetch.SelectedEpisode
	client := m.videoFetch.client

	m.message = "Fetching TMDB metadata..."

	return m, func() tea.Msg {
		meta, err := client.FetchVideoMetadataByID(id, isTV, season, episode)
		if err != nil {
			return errorMsg(err.Error()) // Reusing errorMsg or defining a new one
		}
		return tmdbMetadataMsg{meta}
	}
}

func (m *model) applyTMDBMetadata(meta *tmdb.VideoMetadata) {
	if m.videoEdit == nil {
		return
	}

	// Indices: 0:Type, 4:Title, 5:Series, 6:Season, 7:Episode, 8:Date, 9:SeriesOverview, 10:Synopsis, 11:EpisodeOverview, 3:Genres
	
	// Automatic Type selection
	if meta.Series != "" {
		m.videoEdit.fields[0].Select = 1 // TV Show
		m.videoEdit.fields[5].Input.SetValue(meta.Series)
	} else {
		m.videoEdit.fields[0].Select = 0 // Movie
		m.videoEdit.fields[5].Input.SetValue("")
	}
	
	m.videoEdit.fields[4].Input.SetValue(meta.Title)
	
	if meta.Season > 0 {
		m.videoEdit.fields[6].Input.SetValue(strconv.Itoa(meta.Season))
	} else {
		m.videoEdit.fields[6].Input.SetValue("")
	}

	if meta.Episode > 0 {
		m.videoEdit.fields[7].Input.SetValue(strconv.Itoa(meta.Episode))
	} else {
		m.videoEdit.fields[7].Input.SetValue("")
	}
	
	m.videoEdit.fields[8].Input.SetValue(meta.AirDate)
	m.videoEdit.fields[9].TextArea.SetValue(meta.SeriesOverview)
	m.videoEdit.fields[9].TextArea.MoveToBegin()
	m.videoEdit.fields[10].TextArea.SetValue(meta.Synopsis)
	m.videoEdit.fields[10].TextArea.MoveToBegin()
	m.videoEdit.fields[11].TextArea.SetValue(meta.EpisodeOverview)
	m.videoEdit.fields[11].TextArea.MoveToBegin()
	
	if len(meta.Genres) > 0 {
		m.videoEdit.fields[3].Input.SetValue(strings.Join(meta.Genres, ", "))
	}

	// Save poster to permanent storage (~/.config/kitvc/posters/)
	// Priority: Episode still > Season poster > Series poster
	var posterURL string
	var posterName string
	
	if meta.StillPath != "" && meta.Episode > 0 {
		// Episode still image (highest priority for TV episodes)
		posterURL = meta.StillPath
		posterName = fmt.Sprintf("tmdb_%d_s%d_e%d", meta.ID, meta.Season, meta.Episode)
		log.Printf("Using episode still: %s", posterURL)
	} else if meta.SeasonPosterPath != "" && meta.Season > 0 {
		// Season poster
		posterURL = meta.SeasonPosterPath
		posterName = fmt.Sprintf("tmdb_%d_s%d", meta.ID, meta.Season)
		log.Printf("Using season poster: %s", posterURL)
	} else if meta.PosterPath != "" {
		// Series poster (fallback)
		posterURL = meta.PosterPath
		posterName = fmt.Sprintf("tmdb_%d", meta.ID)
		log.Printf("Using series poster: %s", posterURL)
	}
	
	if posterURL != "" {
		configDir, err := config.GetConfigDir()
		if err == nil {
			postersDir := filepath.Join(configDir, "posters")
			if posterName == "" {
				posterName = meta.Series
				if posterName == "" {
					posterName = meta.Title
				}
			}
			if posterName != "" {
				localPath, err := tmdb.DownloadPoster(posterURL, postersDir, posterName)
				log.Printf("DownloadPoster: localPath=%q err=%v", localPath, err)
				if err == nil {
					m.videoEdit.SetThumbnail(localPath)
					m.videoEdit.SetPosterPath(localPath)
					// Update form fields: poster_path (TMDB URL) and local_poster_path (local file)
					fullPosterURL := "https://image.tmdb.org/t/p/w500" + posterURL
					m.videoEdit.SetFieldValue(12, fullPosterURL)  // poster_path
					m.videoEdit.SetFieldValue(13, localPath)       // local_poster_path
					log.Printf("Poster saved to: %s, videoEdit.PosterPath()=%s", localPath, m.videoEdit.PosterPath())
				}
			}
		}
	}

	m.message = fmt.Sprintf("Applied: %s", meta.Title)
}

func (m model) handleVideoEditSubmit(values []string) model {
	for i, field := range m.editVideoFieldNames {
		if i >= len(values) {
			break
		}
		val := values[i]
		if val == "" {
			continue
		}

		for _, path := range m.editVideoPaths {
			if err := db.UpdateVideoField(path, field, val); err != nil {
				log.Printf("Failed to update video %s: %v", path, err)
			}
		}
	}

	if m.videoEdit != nil {
		posterPath := m.videoEdit.PosterPath()
		if posterPath != "" {
			for _, path := range m.editVideoPaths {
				if err := db.UpdateVideoField(path, "poster_path", posterPath); err != nil {
					log.Printf("Failed to update poster path for video %s: %v", path, err)
				}
			}
		}
	}

	if len(m.editVideoPaths) > 0 {
		m.message = fmt.Sprintf("Updated %d video(s)", len(m.editVideoPaths))
	} else {
		m.message = "No changes"
	}
	m.pendingAction = actionNone
	m.editVideoFieldNames = nil
	m.editVideoPaths = nil
	m.refreshCurrentVideoView()
	return m
}

func (m *model) saveUIState() {
	state := config.UIState{
		ExpandedNodes: m.sidebar.GetExpandedNodeIDs(),
		FocusedSide:   m.focusedSide,
		ActiveView:    viewStateNames[m.activeView],
	}
	if n := m.sidebar.SelectedNode(); n != nil {
		state.SelectedNode = n.id
	}
	config.SaveUIState(state)
}

func (m *model) loadUIState() {
	state, err := config.LoadUIState()
	if err != nil {
		return
	}
	for _, id := range state.ExpandedNodes {
		m.sidebar.ExpandByID(id)
	}
	if state.SelectedNode != "" {
		m.sidebar.SelectByID(state.SelectedNode)
		if n := m.sidebar.SelectedNode(); n != nil {
			m.handleSidebarChange(n)
		}
	}
	m.focusedSide = state.FocusedSide
	if v, ok := viewStateFromName[state.ActiveView]; ok {
		m.activeView = v
	}
	m.syncFocus()
}

func (m *model) refreshVideoList() {
	sbWidth := m.getSidebarWidth()
	m.videoList = newVideoList(m.width-sbWidth-3, m.height-6)
	m.syncFocus()
}

func (m *model) refreshVideoContinue() {
	sbWidth := m.getSidebarWidth()
	videos, _ := db.GetContinueWatchingVideos()
	m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshVideoRecent() {
	sbWidth := m.getSidebarWidth()
	videos, _ := db.GetRecentlyAddedVideos()
	m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshVideoHealth() {
	sbWidth := m.getSidebarWidth()
	videos, _ := db.GetUnhealthyVideos()
	m.videoList = newVideoListFromVideos(m.width-sbWidth-3, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshCurrentVideoView() {
	switch m.activeView {
	case viewVideoLibrary:
		m.refreshVideoList()
	case viewVideoContinue:
		m.refreshVideoContinue()
	case viewVideoRecent:
		m.refreshVideoRecent()
	case viewVideoHealth:
		m.refreshVideoHealth()
	case viewVideoFilter:
		if m.currentVideoFilterID > 0 {
			m.refreshVideoFilterTracks(m.currentVideoFilterID)
		}
	}
}

func (m *model) coverDisplayCmd() tea.Cmd {
	path := m.sidebar.CoverPath()
	if path == "" || !m.sidebar.HasCover() {
		return nil
	}
	cols := m.sidebar.CoverCols()
	rows := m.sidebar.CoverRows()
	return func() tea.Msg {
		// Use high-resolution Kitty protocol.
		cmd := exec.Command("chafa", "-f", "kitty",
			"--symbols", "none",
			"--probe", "off",
			"--size", fmt.Sprintf("%dx%d", cols, rows),
			path)
		cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
		out, err := cmd.Output()
		if err != nil {
			return nil
		}
		
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Hide cursor, move to position, draw, then hide cursor again to be sure.
		f.WriteString("\x1b[?25l") 
		fmt.Fprintf(f, "\x1b[%d;1H", m.sidebar.CoverRow()+1)
		f.Write(out)
		f.WriteString("\x1b[?25l")
		f.Sync()
		return nil
	}
}

func (m *model) coverPlaceCmd() tea.Cmd {
	return m.coverDisplayCmd()
}

func (m *model) posterPos() (row, col, w, h int) {
	if m.videoFetch == nil {
		return 0, 0, 0, 0
	}
	sl := m.videoFetch.OverlayStartLine()
	sc := m.videoFetch.OverlayStartCol()
	// sl is the starting line of the modal border (row 0 of the modal)
	// Modal content starts at row 1 (inside the top border)
	// Header height tells us where the poster block starts relative to content top
	row = sl + 1 + m.videoFetch.HeaderHeight()
	col = sc + 2 // left border(1) + left pad(1)
	w = m.videoFetch.PosterCols()
	h = m.videoFetch.PosterRows()
	return
}

func (m *model) posterClearCmd() tea.Cmd {
	row, col, w, h := m.posterPos()
	if w <= 0 || h <= 0 {
		return nil
	}
	return func() tea.Msg {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f.Close()
		// Move cursor to poster top-left and delete any Kitty image at this position
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		// a=d: delete, d=c: delete by column/row position
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		f.WriteString("\x1b[?25l")
		f.Sync()
		return nil
	}
}

func (m *model) posterDisplayCmd() tea.Cmd {
	if m.videoFetch == nil || m.videoFetch.PosterPath() == "" {
		return nil
	}
	path := m.videoFetch.PosterPath()
	cols := m.videoFetch.PosterCols()
	rows := m.videoFetch.PosterRows()
	oldRow, oldCol := m.lastPosterRow, m.lastPosterCol

	return func() tea.Msg {
		if m.videoFetch == nil {
			return nil
		}
		row, col, _, _ := m.posterPos()

		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		// Always clear current position before drawing
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		
		// If position moved, clear old position too
		if oldRow >= 0 && (oldRow != row || oldCol != col) {
			fmt.Fprintf(f, "\x1b[%d;%dH", oldRow+1, oldCol+1)
			f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		}
		f.Close()

		// Draw new poster with chafa
		cmd := exec.Command("chafa", "-f", "kitty",
			"--symbols", "none",
			"--probe", "off",
			"--size", fmt.Sprintf("%dx%d", cols, rows),
			path)
		cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
		out, err := cmd.Output()
		if err != nil {
			return nil
		}

		f2, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f2.Close()

		f2.WriteString("\x1b[?25l")
		fmt.Fprintf(f2, "\x1b[%d;%dH", row+1, col+1)
		f2.Write(out)
		f2.WriteString("\x1b[?25l")
		f2.Sync()

		m.lastPosterRow, m.lastPosterCol = row, col
		return nil
	}
}

func (m *model) videoEditPosterPos() (row, col, w, h int) {
	if m.videoEdit == nil {
		return 0, 0, 0, 0
	}
	sl := m.videoEdit.OverlayStartLine()
	sc := m.videoEdit.OverlayStartCol()
	w = m.videoEdit.PosterCols()
	h = m.videoEdit.PosterRows()
	dialogW := m.videoEdit.Width() - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}
	// sl is top border of modal
	// HeaderHeight() is distance from content start to poster start
	row = sl + 1 + m.videoEdit.HeaderHeight()
	col = sc + 2 + (dialogW-w)/2
	return
}

func (m *model) videoEditPosterClearCmd() tea.Cmd {
	row, col, w, h := m.videoEditPosterPos()
	if w <= 0 || h <= 0 {
		return nil
	}
	return func() tea.Msg {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f.Close()
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		f.WriteString("\x1b[?25l")
		f.Sync()
		return nil
	}
}

func (m *model) videoEditPosterDisplayCmd() tea.Cmd {
	if m.videoEdit == nil || m.videoEdit.PosterPath() == "" {
		return nil
	}
	path := m.videoEdit.PosterPath()
	cols := m.videoEdit.PosterCols()
	rows := m.videoEdit.PosterRows()
	oldRow, oldCol := m.videoEditLastPosterRow, m.videoEditLastPosterCol

	return func() tea.Msg {
		if m.videoEdit == nil {
			return nil
		}
		row, col, _, _ := m.videoEditPosterPos()

		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		// Clear current and old positions
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		if oldRow >= 0 && (oldRow != row || oldCol != col) {
			fmt.Fprintf(f, "\x1b[%d;%dH", oldRow+1, oldCol+1)
			f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		}
		f.Close()

		cmd := exec.Command("chafa", "-f", "kitty",
			"--symbols", "none",
			"--probe", "off",
			"--size", fmt.Sprintf("%dx%d", cols, rows),
			path)
		cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
		out, err := cmd.Output()
		if err != nil {
			return nil
		}

		f2, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f2.Close()
		f2.WriteString("\x1b[?25l")
		fmt.Fprintf(f2, "\x1b[%d;%dH", row+1, col+1)
		f2.Write(out)
		f2.WriteString("\x1b[?25l")
		f2.Sync()

		m.videoEditLastPosterRow, m.videoEditLastPosterCol = row, col
		return nil
	}
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Every(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type uiStateLoadedMsg struct {
	state config.UIState
}

func loadUIStateCmd() tea.Cmd {
	return func() tea.Msg {
		state, err := config.LoadUIState()
		if err != nil {
			return nil
		}
		return uiStateLoadedMsg{state: state}
	}
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

		// Process album covers after scan
		library.ProcessAllAlbumCovers()

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

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Loading...")
		v.AltScreen = true
		return v
	}

	header := m.renderHeader()
	sbView := m.sidebar.View(m.focusedSide)
	sbWidth := m.getSidebarWidth()

	mainWidth := m.width - sbWidth - 2
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
	case viewMusicLibrary, viewMusicRecent, viewMusicFilter:
		mainContentStr = m.trackList.View()
	case viewMusicArtists:
		mainContentStr = m.musicArtists.View()
	case viewMusicArtistDetail:
		mainContentStr = m.artistDetail.View()
	case viewVideoLibrary, viewVideoContinue, viewVideoRecent, viewVideoFilter, viewVideoHealth:
		mainContentStr = m.videoList.View()
	default:
		mainContentStr = "Unknown View"
	}

	footer := m.renderFooter()

	backgroundView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr)),
		footer,
	)

	// If any modal is active, overlay it
	var overlay string
	if m.videoFetch != nil {
		m.videoFetch.SetSize(m.width-10, m.height-6)
		overlay = m.videoFetch.View()
	} else if m.videoEdit != nil {
		m.videoEdit.SetSize(m.width-10, m.height-6)
		overlay = m.videoEdit.View()
	} else if m.filterCondEdit != nil {
		m.filterCondEdit.SetSize(m.width-10, m.height-6)
		overlay = m.filterCondEdit.View()
	} else if m.filterEdit != nil {
		m.filterEdit.SetSize(m.width-10, m.height-6)
		overlay = m.filterEdit.View()
	} else if m.sortFieldSelect != nil {
		m.sortFieldSelect.SetSize(m.width-10, m.height-6)
		overlay = m.sortFieldSelect.View()
	} else if m.modal != nil && m.modal.Active() {
		m.modal.SetSize(m.width-10, m.height-6)
		overlay = m.modal.View()
	}

	mainView := backgroundView
	if overlay != "" {
		// Manual overlay by patching lines to preserve header/footer
		bgLines := strings.Split(backgroundView, "\n")
		ovLines := strings.Split(overlay, "\n")
		
		// Remove empty trailing lines from overlay
		for len(ovLines) > 0 && strings.TrimSpace(ovLines[len(ovLines)-1]) == "" {
			ovLines = ovLines[:len(ovLines)-1]
		}

		ovH := len(ovLines)
		ovW := 0
		for _, l := range ovLines {
			if w := lipgloss.Width(l); w > ovW {
				ovW = w
			}
		}

		startLine := (m.height - ovH) / 2
		if startLine < 1 { startLine = 1 }
		
		startCol := (m.width - ovW) / 2
		if startCol < 0 { startCol = 0 }

		if m.videoFetch != nil {
			m.videoFetch.SetOverlayPos(startLine, startCol)
		} else if m.videoEdit != nil {
			m.videoEdit.SetOverlayPos(startLine, startCol)
		}

		for i := 0; i < ovH && startLine+i < len(bgLines); i++ {
			ovLine := ovLines[i]
			// Simple replacement of line content
			bgLines[startLine+i] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, ovLine)
		}
		mainView = strings.Join(bgLines, "\n")
	}

	v := tea.NewView(mainView)
	v.AltScreen = true
	return v
}

func (m model) renderHeader() string {
	if m.currentTrack == "" {
		return lipgloss.NewStyle().
			Width(m.width - 5).
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

	availWidth := m.width - 4
	fixedWidth := len(timeStr) + len(volStr) + 12
	maxNameWidth := availWidth - fixedWidth
	if maxNameWidth < 10 {
		maxNameWidth = 10
	}
	nameStr := m.currentTrack
	if len(nameStr) > maxNameWidth {
		nameStr = nameStr[:maxNameWidth-3] + "..."
	}

	infoLine := lipgloss.JoinHorizontal(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render(nameStr),
		"  |  ",
		volStr,
	)

	timeProgressLine := lipgloss.JoinHorizontal(lipgloss.Left,
		timeStr,
		" ",
		progressStr,
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		infoLine,
		timeProgressLine,
	)

	return lipgloss.NewStyle().
		Width(m.width - 5).
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
		{"e", "Edit"},
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
		Width(m.width - 5).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render(helpStr)
}
