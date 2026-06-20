package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kitvc/internal/config"
	"kitvc/internal/db"
	"kitvc/internal/library"
	"kitvc/internal/musicbrainz"
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
	actionCreateVideoPlaylist
	actionAddToVideoPlaylist
	actionDeleteVideoPlaylist
	actionRemoveVideoFile
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
	actionExportPlaylist
	actionExportVideoPlaylist
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
	pendingVideoFiles []string
	editFieldNames    []string
	editPaths         []string
	editAlbumID       int64
	editVideoFieldNames []string
	editVideoPaths      []string
	videoEdit           *videoEditModal
	videoFetch          *videoFetchModal
	musicFetch          *musicFetchModal
	musicFetchMarkedPaths []string
	musicFetchMetadata    *musicbrainz.ReleaseInfo
	musicBatchMode      bool
	musicBatchCancelled bool
	musicBatchPaths     []string
	musicBatchIndex     int
	musicBatchTotal     int
	playingAlbumID    int64
	currentFilterID   int64
	currentFilterName string
	currentVideoFilterID   int64
	currentVideoFilterName string
	filterEdit        *filterEditModal
	filterCondEdit    *filterConditionModal
	sortFieldSelect   *sortFieldSelectModal
	scanning          bool
	scanCancelled     bool
	scanTracks        []library.Track
	scanVideos        []library.Video
	scanIndex         int
	scanTotal         int

	// Incremental search
	searchMode       bool
	searchQuery      string
	searchOrigTracks []db.TrackData
	searchOrigVideos []db.VideoData

	// Help overlay
	helpOverlay string

	savedView viewState
	scanPhase         string
	messageTime       time.Time
	tmdbBatchMode     bool
	tmdbBatchVideos   []db.VideoData
	tmdbBatchIndex    int
	tmdbBatchTotal    int
	tmdbBatchCancelled bool

	playerStateInProgress bool

	tty ttyWriter

	displayer imageDisplayer
}

func (m model) Player() *player.MpvPlayer {
	return m.player
}

func InitialModel(cfg *config.Config) model {
	socketPath := player.MpvSocketPath(os.Getpid())
	p := player.NewMpvPlayer(socketPath, cfg.Player.MpvArgs)
	vol := float64(cfg.Player.Volume)

	return model{
		config:      cfg,
		player:      p,
		volume:      vol,
		progress:    progress.New(progress.WithDefaultBlend()),
		focusedSide: true,
		activeView:  viewMusicLibrary,
		sidebar:     newSidebar(cfg.UI.SidebarWidth, 20),
		tty:         stdTTY,
		displayer:   newImageDisplayer(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), loadUIStateCmd())
}

func hideCursorCmd() tea.Cmd {
	return func() tea.Msg {
		stdTTY.WriteString("\x1b[?25l")
		stdTTY.Sync()
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	log.Printf("[DEBUG] Update: type=%T", msg)

	// Handle UI state loaded message
	if msg, ok := msg.(uiStateLoadedMsg); ok {
		m.sidebar.CollapseAll()
		for _, id := range msg.state.ExpandedNodes {
			m.sidebar.ExpandByID(id)
		}
		var cmds []tea.Cmd
		if msg.state.SelectedNode != "" {
			m.sidebar.SelectByID(msg.state.SelectedNode)
			if n := m.sidebar.SelectedNode(); n != nil {
				if cmd := m.handleSidebarChange(n); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.focusedSide = msg.state.FocusedSide
		if v, ok := viewStateFromName[msg.state.ActiveView]; ok {
			m.activeView = v
		}
		m.syncFocus()
		if cmd := m.videoFocusCoverCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Auto-clear timed messages after 3 seconds
	if m.message != "" && !m.messageTime.IsZero() && time.Since(m.messageTime) > 3*time.Second {
		m.message = ""
		m.messageTime = time.Time{}
	}

	switch msg := msg.(type) {
	case tmdbMetadataMsg:
		log.Printf("[DEBUGLOG] tmdbMetadataMsg received")
		m.applyTMDBMetadata(msg.metadata)
		var posterCmd tea.Cmd
		if m.videoEdit != nil {
			if path := m.videoEdit.PosterPath(); path != "" {
				row, col, _, _ := m.videoEditPosterPos()
				if m.videoEdit.NeedsDisplay(path, row, col) {
					posterCmd = m.videoEditPosterDisplayCmd()
					m.videoEdit.SetDisplayed(path, row, col)
				}
			}
		}
		return m, tea.Batch(posterCmd, m.syncVideoEditPosterCmd())
	case posterReadyMsg:
		log.Printf("[DEBUGLOG] posterReadyMsg received: isEdit=%v kittyLen=%d", msg.isEdit, len(msg.kitty))
		if msg.isEdit && m.videoEdit != nil {
			m.videoEdit.cachedKitty = msg.kitty
			return m, m.syncVideoEditPosterCmd()
		} else if !msg.isEdit && m.videoFetch != nil {
			m.videoFetch.cachedKitty = msg.kitty
			return m, m.syncPosterCmd()
		}
		return m, nil
	case scanProgressMsg:
		m.scanIndex = msg.current
		m.scanTotal = msg.total
		prefix := "Scanning music"
		if m.scanPhase == "video" {
			prefix = "Scanning video"
		}
		filePart := ""
		if msg.currentFile != "" {
			filePart = " - " + filepath.Base(msg.currentFile)
		}
		m.message = fmt.Sprintf("%s... %d/%d%s", prefix, msg.current, msg.total, filePart)
		if m.scanCancelled || m.scanIndex >= m.scanTotal {
			m.scanning = false
			m.scanCancelled = false
			m.message = ""
			if m.scanPhase == "music" {
				if err := library.ProcessAllAlbumCovers(); err != nil {
					m.setMessage(err.Error())
				}
				db.DeleteEmptyAlbums()
			}
			m.sidebar.Refresh()
			m.refreshActiveView()
			if n := m.sidebar.SelectedNode(); n != nil {
				if cmd := m.updateCoverForNode(n); cmd != nil {
					return m, cmd
				}
			}
			return m, nil
		}
		return m, m.processNextScanItem()
	case scanCountMsg:
		prefix := "Scanning music"
		if m.scanPhase == "video" {
			prefix = "Scanning video"
		}
		m.message = fmt.Sprintf("%s... 0/%d - reading metadata", prefix, msg.total)
		return m, nil
	case scanReadyMsg:
		if len(msg.tracks) > 0 {
			m.scanning = true
			m.scanCancelled = false
			m.scanPhase = "music"
			m.scanTracks = msg.tracks
			m.scanIndex = 0
			m.scanTotal = len(msg.tracks)
			return m, m.processNextScanItem()
		} else if len(msg.videos) > 0 {
			m.scanning = true
			m.scanCancelled = false
			m.scanPhase = "video"
			m.scanVideos = msg.videos
			m.scanIndex = 0
			m.scanTotal = len(msg.videos)
			return m, m.processNextScanItem()
		}
		m.setMessage("No files found")
		return m, nil
	case scanFinishedMsg:
		log.Printf("[DEBUGLOG] scanFinishedMsg received: count=%d", msg.count)
		m.setMessage(fmt.Sprintf("Scan finished: %d items found", msg.count))
		m.sidebar.Refresh()
		m.refreshActiveView()
		// Update cover for current selection
		if n := m.sidebar.SelectedNode(); n != nil {
			if cmd := m.updateCoverForNode(n); cmd != nil {
				return m, cmd
			}
		}
		return m, nil

	case tmdbBatchProgressMsg:
		m.tmdbBatchIndex = msg.current
		m.tmdbBatchTotal = msg.total
		m.setMessage(fmt.Sprintf("TMDB: %d/%d fetching...", msg.current, msg.total))
		if m.tmdbBatchCancelled || m.tmdbBatchIndex >= m.tmdbBatchTotal {
			cancelled := m.tmdbBatchCancelled
			m.tmdbBatchMode = false
			m.tmdbBatchCancelled = false
			if cancelled {
				m.setMessage("TMDB fetch cancelled")
			} else {
				m.setMessage(fmt.Sprintf("TMDB fetch complete: %d items", msg.total))
			}
			m.refreshActiveView()
			if n := m.sidebar.SelectedNode(); n != nil {
				if cmd := m.updateCoverForNode(n); cmd != nil {
					return m, cmd
				}
			}
			return m, nil
		}
		return m, m.processNextTMDbItem()

	case musicBatchProgressMsg:
		m.musicBatchIndex = msg.current
		m.musicBatchTotal = msg.total
		m.setMessage(fmt.Sprintf("MusicBrainz: %d/%d fetching...", msg.current, msg.total))
		if m.musicBatchCancelled || m.musicBatchIndex >= m.musicBatchTotal {
			cancelled := m.musicBatchCancelled
			m.musicBatchMode = false
			m.musicBatchCancelled = false
			if cancelled {
				m.setMessage("MusicBrainz fetch cancelled")
			} else {
				m.setMessage(fmt.Sprintf("MusicBrainz fetch complete: %d items", msg.total))
			}
			m.trackList.ClearMarks()
			m.artistDetail.ClearMarks()
			m.refreshActiveView()
			return m, nil
		}
		return m, m.processNextMusicItem()
	case errorMsg:
		if m.videoFetch == nil {
			m.setMessage(string(msg))
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
						m.setMessage(fmt.Sprintf("Created view '%s'", result.name))
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
					m.setMessage(fmt.Sprintf("Updated view '%s'", result.name))
					m.currentFilterName = result.name
					m.sidebar.Refresh()
					m.sidebar.ExpandByID("music_views")
					m.sidebar.SelectByID(fmt.Sprintf("music_filter:%d", m.currentFilterID))
					m.refreshFilterTracks(m.currentFilterID)
				} else if m.pendingAction == actionCreateVideoFilter {
					newID, err := db.CreateVideoFilter(result.name, result.condJSON, result.sortJSON)
					if err == nil {
						m.setMessage(fmt.Sprintf("Created view '%s'", result.name))
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
					m.setMessage(fmt.Sprintf("Updated view '%s'", result.name))
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
			m.musicFetchMarkedPaths = nil
			m.musicFetchMetadata = nil
		}
		return m, cmd
	}

	if m.videoFetch != nil {
		var cmd tea.Cmd
		m.videoFetch, cmd = m.videoFetch.Update(msg)
		if m.videoFetch.Cancelled {
			clearCmd := m.posterClearCmd()
			m.videoFetch = nil
			return m, tea.Batch(clearCmd, hideCursorCmd())
		}
		if m.videoFetch.Submitted {
			clearCmd := m.posterClearCmd()
			m, fetchCmd := m.handleVideoFetchSubmit()
			m.videoFetch = nil
			return m, tea.Batch(fetchCmd, clearCmd, hideCursorCmd())
		}
		// Render poster display if poster is ready and state has changed
		var posterCmd tea.Cmd
		if path := m.videoFetch.PosterPath(); path != "" {
			row, col, _, _ := m.posterPos()
			if m.videoFetch.NeedsDisplay(path, row, col) {
				posterCmd = m.posterDisplayCmd()
				m.videoFetch.SetDisplayed(path, row, col)
			}
		} else {
			m.videoFetch.ClearDisplayed()
		}
		return m, tea.Batch(cmd, posterCmd, hideCursorCmd())
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
			return m, tea.Batch(cmd, clearCmd, hideCursorCmd())
		}
		if m.videoEdit.submitted {
			clearCmd := m.videoEditPosterClearCmd()
			values := m.videoEdit.Values()
			m = m.handleVideoEditSubmit(values)
			m.videoEdit = nil
			return m, tea.Batch(cmd, clearCmd, hideCursorCmd())
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
				m.setMessage("TMDB API Key not set in config.toml or TMDB_API_KEY env")
				return m, tea.Batch(cmd, clearCmd, hideCursorCmd())
			}

			m.videoFetch = newVideoFetchModal(apiKey, query, isTV, initialSeason, initialEpisode)
			m.videoFetch.SetSize(m.width, m.height-10)
			return m, tea.Batch(cmd, clearCmd, hideCursorCmd())
		}

		// Render poster display if poster is ready and state has changed
		var posterCmd tea.Cmd
		if path := m.videoEdit.PosterPath(); path != "" {
			sl := m.videoEdit.OverlayStartLine()
			if sl >= 0 {
				row, col, _, _ := m.videoEditPosterPos()
				if m.videoEdit.NeedsDisplay(path, row, col) {
					posterCmd = m.videoEditPosterDisplayCmd()
					m.videoEdit.SetDisplayed(path, row, col)
				}
			}
		} else {
			m.videoEdit.ClearDisplayed()
		}
		return m, tea.Batch(cmd, posterCmd, hideCursorCmd())
	}

	if m.musicFetch != nil {
		var cmd tea.Cmd
		m.musicFetch, cmd = m.musicFetch.Update(msg)
		if m.musicFetch.Cancelled {
			log.Printf("[DEBUGLOG] musicFetch Cancelled")
			m.musicFetch = nil
			m.musicFetchMarkedPaths = nil
			return m, nil
		}
		if m.musicFetch.Submitted {
			log.Printf("[DEBUGLOG] musicFetch Submitted! releaseInfo=%v markedCount=%d", m.musicFetch.releaseInfo != nil, len(m.musicFetchMarkedPaths))
			m.musicFetchMetadata = m.musicFetch.releaseInfo
			m.musicFetch = nil

			if len(m.musicFetchMarkedPaths) > 1 {
				// Multiple items: direct update (fill empty fields)
				m.applyMusicMetadata(m.musicFetchMetadata, m.musicFetchMarkedPaths, false)
				m.musicFetchMarkedPaths = nil
				m.musicFetchMetadata = nil
				return m, nil
			} else {
				// Single item (or 1 item marked): confirmation dialog
				log.Printf("[DEBUGLOG] Creating confirm modal for single item metadata update")
				m.modal = newConfirmModal("Overwrite metadata for this item?")
				m.modal.help = "y: Yes  n: No / Esc: Cancel"
				m.pendingAction = actionEditTrack
				return m, nil
			}
		}
		return m, cmd
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case errorMsg:
		m.setMessage(string(msg))
		return m, nil
	case tea.KeyMsg:
		// Help overlay dismiss
		if m.helpOverlay != "" {
			m.helpOverlay = ""
			m.setMessage("")
			return m, nil
		}
		// Search mode handling
		if m.searchMode {
			key := msg.Key()
			switch key.String() {
			case "esc":
				m.exitSearch()
				return m, nil
			case "enter":
				// keep search filter active, fall through to normal handling (playback / focus switch)
			case "backspace":
				if len(m.searchQuery) > 0 {
					_, size := utf8.DecodeLastRuneInString(m.searchQuery)
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
					m.applySearchFilter()
				}
				return m, nil
			case "tab":
				// Tab is disabled in search mode to prevent sidebar focus + display corruption
				return m, nil
			case "space":
				// Block space sidebar navigation during search mode
				if m.focusedSide {
					return m, nil
				}
				// fall through to normal handling (play/pause toggle)
			default:
				if key.Text != "" {
					m.searchQuery += key.Text
					m.applySearchFilter()
					return m, nil
				}
				// Block all sidebar navigation during search mode
				if m.focusedSide {
					return m, nil
				}
				// Fall through: up/down navigate filtered list, space toggles playback, etc.
			}
		}
		log.Printf("[DEBUG] keypress: %q", msg.String())
		switch msg.String() {
		case "ctrl+c", "q":
			if m.player != nil {
				m.player.Stop()
			}
			m.saveUIState()
			return m, tea.Quit
		case "esc":
			if m.scanning {
				m.scanCancelled = true
				m.message = "Cancelling scan..."
				return m, nil
			}
			if m.tmdbBatchMode {
				m.tmdbBatchCancelled = true
				m.message = "Cancelling TMDB fetch..."
				return m, nil
			}
			if m.musicBatchMode {
				m.musicBatchCancelled = true
				m.message = "Cancelling MusicBrainz fetch..."
				return m, nil
			}
		case "tab":
			m.focusedSide = !m.focusedSide
			m.syncFocus()
			return m, m.videoFocusCoverCmd()
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
				m.config.Player.Volume = int(m.volume)
				config.SaveConfig(m.config)
			}
			return m, nil
		case "0":
			if m.player != nil {
				m.player.AdjustVolume(5)
				m.volume += 5
				if m.volume > 100 {
					m.volume = 100
				}
				m.config.Player.Volume = int(m.volume)
				config.SaveConfig(m.config)
			}
			return m, nil
		case "/":
			if !m.searchMode {
				m.enterSearch()
			}
			return m, nil
		case "E":
			// extract playlist ID from sidebar selection first, then fall back to currentPlaylistID
			playlistID := int64(0)
			isMusic := false
			sel := m.sidebar.SelectedNode()
			if sel != nil && sel.id != "" {
				if strings.HasPrefix(sel.id, "music_playlist:") {
					idStr := strings.TrimPrefix(sel.id, "music_playlist:")
					if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
						playlistID = id
						isMusic = true
					}
				} else if strings.HasPrefix(sel.id, "video_playlist:") {
					idStr := strings.TrimPrefix(sel.id, "video_playlist:")
					if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
						playlistID = id
						isMusic = false
					}
				}
			}
			if playlistID == 0 && m.currentPlaylistID > 0 {
				playlistID = m.currentPlaylistID
				isMusic = m.activeView == viewMusicLibrary
			}
			if playlistID > 0 {
				if isMusic {
					playlists, _ := db.GetMusicPlaylists()
					defName := "playlist.m3u"
					for _, p := range playlists {
						if p.ID == playlistID {
							defName = p.Name + ".m3u"
							break
						}
					}
					m.currentPlaylistID = playlistID
					m.pendingAction = actionExportPlaylist
					m.modal = newTextInputModal("Export M3U", defName, "Enter: Export  Esc: Cancel")
					m.modal.SetSize(m.width, m.height)
				} else {
					playlists, _ := db.GetVideoPlaylists()
					defName := "playlist.m3u"
					for _, p := range playlists {
						if p.ID == playlistID {
							defName = p.Name + ".m3u"
							break
						}
					}
					m.currentPlaylistID = playlistID
					m.pendingAction = actionExportVideoPlaylist
					m.modal = newTextInputModal("Export Video M3U", defName, "Enter: Export  Esc: Cancel")
					m.modal.SetSize(m.width, m.height)
				}
			}
			return m, nil
		case "f1":
			m.helpOverlay = m.helpOverlayView(m.width)
			return m, nil
		case "s":
			if m.scanning {
				return m, nil
			}
			m.message = "Scanning..."
			if m.activeView == viewVideoLibrary || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
				m.scanPhase = "video"
				return m, m.scanVideoCmd()
			}
			m.scanPhase = "music"
			return m, m.scanMusicCmd()
		case "ctrl+r":
			if m.scanning {
				return m, nil
			}
			m.sidebar.Refresh()
			m.refreshActiveView()
			if n := m.sidebar.SelectedNode(); n != nil {
				if cmd := m.updateCoverForNode(n); cmd != nil {
					return m, cmd
				}
			}
			m.setMessage("Refreshed")
			return m, nil
		case "enter":
			if m.focusedSide {
				m.focusedSide = false
				m.syncFocus()
				return m, m.videoFocusCoverCmd()
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
							m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-6, selectedArtist, albums[selectedArtist])
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
				} else if m.activeView == viewVideoLibrary {
					markedPaths := m.videoList.MarkedPaths()
					if len(markedPaths) > 0 {
						m.pendingAction = actionRemoveVideoFile
						m.pendingVideoFiles = markedPaths
						m.modal = newConfirmModal(fmt.Sprintf("Remove %d marked videos from the playlist?", len(markedPaths)))
						if m.modal != nil {
							m.modal.SetSize(m.width, m.height)
						}
					} else {
						selected := m.videoList.table.HighlightedRow()
						if selected.Data != nil {
							cursor := m.videoList.table.GetHighlightedRowIndex()
							if cursor >= 0 && cursor < len(m.videoList.videos) {
								v := m.videoList.videos[cursor]
								m.pendingAction = actionRemoveVideoFile
								m.pendingVideoFiles = []string{v.Path}
								m.modal = newConfirmModal("Remove '" + v.Filename + "' from the playlist?")
								if m.modal != nil {
									m.modal.SetSize(m.width, m.height)
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
				} else if sel != nil && strings.HasPrefix(sel.id, "video_playlist:") {
					m.pendingAction = actionDeleteVideoPlaylist
					m.modal = newConfirmModal("Delete video playlist '" + sel.label + "'?")
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
			if !m.focusedSide && m.currentPlaylistID > 0 {
				if m.activeView == viewMusicLibrary {
					tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
					if len(tracks) > 0 {
						cursor := m.trackList.table.GetHighlightedRowIndex()
						if cursor > 0 {
							db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor-1)
							m.refreshPlaylistTracks(m.currentPlaylistID)
							m.trackList.table = m.trackList.table.WithHighlightedRow(cursor - 1)
						}
					}
				} else if m.activeView == viewVideoLibrary {
					videos, _ := db.GetVideoPlaylistFiles(m.currentPlaylistID)
					if len(videos) > 0 {
						cursor := m.videoList.table.GetHighlightedRowIndex()
						if cursor > 0 {
							db.MoveVideoPlaylistFile(m.currentPlaylistID, cursor, cursor-1)
							m.refreshVideoPlaylistFiles(m.currentPlaylistID)
							m.videoList.table = m.videoList.table.WithHighlightedRow(cursor - 1)
						}
					}
				}
				return m, nil
			}
		case "shift+down":
			if !m.focusedSide && m.currentPlaylistID > 0 {
				if m.activeView == viewMusicLibrary {
					tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
					if len(tracks) > 0 {
						cursor := m.trackList.table.GetHighlightedRowIndex()
						if cursor < len(tracks)-1 {
							db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor+1)
							m.refreshPlaylistTracks(m.currentPlaylistID)
							m.trackList.table = m.trackList.table.WithHighlightedRow(cursor + 1)
						}
					}
				} else if m.activeView == viewVideoLibrary {
					videos, _ := db.GetVideoPlaylistFiles(m.currentPlaylistID)
					if len(videos) > 0 {
						cursor := m.videoList.table.GetHighlightedRowIndex()
						if cursor < len(videos)-1 {
							db.MoveVideoPlaylistFile(m.currentPlaylistID, cursor, cursor+1)
							m.refreshVideoPlaylistFiles(m.currentPlaylistID)
							m.videoList.table = m.videoList.table.WithHighlightedRow(cursor + 1)
						}
					}
				}
				return m, nil
			}
		case "D":
			if !m.focusedSide && m.currentPlaylistID > 0 {
				sel := m.sidebar.SelectedNode()
				if sel != nil {
					if strings.HasPrefix(sel.id, "music_playlist:") {
						m.pendingAction = actionDeletePlaylist
						m.modal = newConfirmModal("Delete playlist '" + sel.label + "'?")
						m.modal.SetSize(m.width, m.height)
					} else if strings.HasPrefix(sel.id, "video_playlist:") {
						m.pendingAction = actionDeleteVideoPlaylist
						m.modal = newConfirmModal("Delete video playlist '" + sel.label + "'?")
						m.modal.SetSize(m.width, m.height)
					}
				}
				return m, nil
			}
		case "a":
			if !m.focusedSide {
				var hasSelection bool
				var pathsToAdd []string
				var isVideo bool

				if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					markedPaths := m.trackList.MarkedPaths()
					if len(markedPaths) > 0 {
						pathsToAdd = markedPaths
						hasSelection = true
					} else {
						selected := m.trackList.table.HighlightedRow()
						if selected.Data != nil {
							cursor := m.trackList.table.GetHighlightedRowIndex()
							if cursor >= 0 && cursor < len(m.trackList.tracks) {
								pathsToAdd = []string{m.trackList.tracks[cursor].Path}
								hasSelection = true
							}
						}
					}
				} else if m.activeView == viewMusicArtistDetail {
					if m.artistDetail.focusedUpper {
						albumTitle, ok := m.artistDetail.SelectedAlbum()
						if ok {
							tracks, _ := db.GetMusicTracks(m.artistDetail.artist, albumTitle)
							pathsToAdd = make([]string, len(tracks))
							for i, t := range tracks {
								pathsToAdd[i] = t.Path
							}
							hasSelection = true
						}
					} else {
						markedPaths := m.artistDetail.MarkedTracks()
						if len(markedPaths) > 0 {
							pathsToAdd = markedPaths
							hasSelection = true
						} else {
							track, ok := m.artistDetail.SelectedTrack()
							if ok {
								pathsToAdd = []string{track.Path}
								hasSelection = true
							}
						}
					}
				} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoRecent || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoHealth {
					markedPaths := m.videoList.MarkedPaths()
					if len(markedPaths) > 0 {
						pathsToAdd = markedPaths
						hasSelection = true
						isVideo = true
					} else {
						selected := m.videoList.table.HighlightedRow()
						if selected.Data != nil {
							cursor := m.videoList.table.GetHighlightedRowIndex()
							if cursor >= 0 && cursor < len(m.videoList.videos) {
								pathsToAdd = []string{m.videoList.videos[cursor].Path}
								hasSelection = true
								isVideo = true
							}
						}
					}
				}

				if hasSelection {
					if isVideo {
						m.pendingAction = actionAddToVideoPlaylist
						m.pendingVideoFiles = pathsToAdd
						playlists, _ := db.GetVideoPlaylists()
						items := make([]string, 0, len(playlists)+1)
						items = append(items, "[New Playlist...]")
						for _, p := range playlists {
							items = append(items, p.Name)
						}
						m.modal = newListSelectModal("Add to Video Playlist", items, "↑↓: Select  Enter: Confirm  Esc: Cancel")
					} else {
						m.pendingAction = actionAddToPlaylist
						m.pendingTracks = pathsToAdd
						playlists, _ := db.GetMusicPlaylists()
						items := make([]string, 0, len(playlists)+1)
						items = append(items, "[New Playlist...]")
						for _, p := range playlists {
							items = append(items, p.Name)
						}
						m.modal = newListSelectModal("Add to Playlist", items, "↑↓: Select  Enter: Confirm  Esc: Cancel")
					}
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
					m.editFieldNames = []string{"artist", "album", "release_date", "genre"}

					allTracks, err := db.GetMusicTracksByAlbumID(album.ID)
					if err != nil {
						allTracks = nil
					}
					m.editPaths = make([]string, len(allTracks))
					for i, t := range allTracks {
						m.editPaths[i] = t.Path
					}

					initialGenre := ""
					if len(allTracks) > 0 {
						initialGenre = allTracks[0].Genre
					}

					m.modal = newFormModal("Edit Album",
						[]string{"Artist", "Album", "Date", "Genre"},
						[]string{album.Artist, album.Title, album.ReleaseDate, initialGenre},
						"Tab: Next  Enter: Save  Esc: Cancel")
					if m.modal != nil {
						m.modal.SetSize(m.width, m.height)
					}
				} else {
					var selectedTrack db.TrackData
					var ok bool
					if m.activeView == viewMusicLibrary {
						cursor := m.trackList.table.GetHighlightedRowIndex()
						if cursor >= 0 && cursor < len(m.trackList.tracks) {
							selectedTrack = m.trackList.tracks[cursor]
							ok = true
						}
					} else if m.activeView == viewMusicArtistDetail && !m.artistDetail.focusedUpper {
						selectedTrack, ok = m.artistDetail.SelectedTrack()
					} else if m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
						cursor := m.trackList.table.GetHighlightedRowIndex()
						if cursor >= 0 && cursor < len(m.trackList.tracks) {
							selectedTrack = m.trackList.tracks[cursor]
							ok = true
						}
					}

					if !ok {
						return m, nil
					}

					m.editPaths = []string{selectedTrack.Path}
					m.editFieldNames = []string{"title", "genre"}

					m.pendingAction = actionEditTrack
					m.modal = newFormModal("Edit Track",
						[]string{"Title", "Genre"},
						[]string{selectedTrack.Title, selectedTrack.Genre},
						"Tab: Next  Enter: Save  Esc: Cancel")
					if m.modal != nil {
						m.modal.SetSize(m.width, m.height)
					}
				}
			}
			return m, nil
		case "ctrl+a":
			if !m.focusedSide {
				if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					m.trackList.MarkAll()
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
				} else if m.activeView == viewMusicArtistDetail && !m.artistDetail.focusedUpper {
					m.artistDetail.MarkAll()
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
				} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
					m.videoList.MarkAll()
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
				}
			}
			return m, nil
		case "ctrl+b":
			if m.tmdbBatchMode || m.musicBatchMode {
				return m, nil
			}
			videoViews := map[viewState]bool{
				viewVideoLibrary: true, viewVideoContinue: true,
				viewVideoRecent: true, viewVideoHealth: true, viewVideoFilter: true,
			}
			musicViews := map[viewState]bool{
				viewMusicLibrary: true, viewMusicRecent: true,
				viewMusicFilter: true, viewMusicArtists: true, viewMusicArtistDetail: true,
			}

			if !m.focusedSide && videoViews[m.activeView] {
				marked := m.videoList.MarkedVideos()
				if len(marked) > 0 {
					m.tmdbBatchMode = true
					m.tmdbBatchCancelled = false
					m.tmdbBatchVideos = marked
					m.tmdbBatchIndex = 0
					m.tmdbBatchTotal = len(marked)
					m.setMessage("Fetching TMDB metadata...")
					return m, m.processNextTMDbItem()
				}
			} else if !m.focusedSide && musicViews[m.activeView] {
				marked := m.getMarkedMusicPaths()
				if len(marked) > 0 {
					m.musicBatchMode = true
					m.musicBatchCancelled = false
					m.musicBatchPaths = marked
					m.musicBatchIndex = 0
					m.musicBatchTotal = len(marked)
					m.setMessage("Fetching MusicBrainz metadata...")
					return m, m.processNextMusicItem()
				}
			}
			return m, nil
		case "ctrl+t":
			if !m.focusedSide && m.musicFetch == nil {
				musicViews := map[viewState]bool{
					viewMusicLibrary: true, viewMusicRecent: true,
					viewMusicFilter: true, viewMusicArtists: true, viewMusicArtistDetail: true,
				}
				if musicViews[m.activeView] {
					marked := m.getMarkedMusicPaths()
					if len(marked) > 1 {
						m.musicBatchMode = true
						m.musicBatchCancelled = false
						m.musicBatchPaths = marked
						m.musicBatchIndex = 0
						m.musicBatchTotal = len(marked)
						m.setMessage("Fetching MusicBrainz metadata...")
						return m, m.processNextMusicItem()
					}
					query, artist, album := m.getCurrentMusicQuery()
					m.musicFetch = newMusicFetchModal(query, artist, album)
					m.musicFetch.SetSize(m.width, m.height)
					m.musicFetchMarkedPaths = marked
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
		log.Printf("[DEBUGLOG] WindowSizeMsg: w=%d h=%d", msg.Width, msg.Height)
		m.width = msg.Width
		m.height = msg.Height
		m.progress.SetWidth(m.width - 20)

		sbWidth := m.getSidebarWidth()
	middleHeight := m.height - 6
	if middleHeight < 1 {
		middleHeight = 1
	}
	m.sidebar.SetSize(sbWidth, middleHeight)

	mainWidth := m.width - sbWidth - 2
	if mainWidth <= 0 {
		mainWidth = 1
	}

	m.trackList.SetSize(mainWidth-1, middleHeight)
	m.videoList.SetSize(mainWidth-1, middleHeight)
	m.musicArtists.SetSize(mainWidth-1, middleHeight)
	m.artistDetail.SetSize(mainWidth-1, middleHeight)
		if m.videoFetch != nil {
			m.videoFetch.SetSize(m.width-10, m.height-6)
			m.videoFetch.ClearDisplayed()
		}
		if m.modal != nil {
			m.modal.SetSize(m.width, m.height)
		}
		var cmds []tea.Cmd
		if m.sidebar.HasCover() {
			// Refresh cover with new window dimensions
			path := m.sidebar.CoverPath()
			sbWidth := m.getSidebarWidth()
			cols := sbWidth - 2
			maxRows := (m.height - 5) - 5
			m.sidebar.SetCoverPath(path, cols, maxRows)
		} else if !m.focusedSide {
			isVideoView := m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter ||
				m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth
			if isVideoView {
				cmds = append(cmds, m.videoFocusCoverCmd())
			}
		}
		cmds = append(cmds, m.coverPlaceCmd(), m.syncPosterCmd(), m.syncVideoEditPosterCmd())
		return m, tea.Batch(cmds...)
	case tickMsg:
		log.Printf("[DEBUG] tickMsg: start (IsRunning=%v)", m.player != nil && m.player.IsRunning())
		if m.player != nil {
			if !m.player.IsRunning() {
				log.Printf("[DEBUG] tickMsg: player not running, clearing state")
				if m.playbackPos > 0 && m.player.GetCurrentTrackPath() != "" {
					db.UpdateVideoLastPos(m.player.GetCurrentTrackPath(), m.playbackPos)
				}
				m.trackList.UpdatePlaybackStatus("", false)
				m.artistDetail.UpdatePlaybackStatus("", false)
				m.videoList.UpdatePlaybackStatus("", false)
				m.playbackPos = 0
				m.duration = 0
				m.currentTrack = ""
			} else if !m.playerStateInProgress {
				m.playerStateInProgress = true
				cmds = append(cmds, fetchPlayerStateCmd(m.player))
			}
			if cmd := m.setCoverFromPlaying(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		log.Printf("[DEBUG] tickMsg: complete")
		cmds = append(cmds, tick())
	case playerStateMsg:
		m.playerStateInProgress = false
		if m.player == nil {
			break
		}
		if msg.completedPath != "" {
			db.ClearVideoLastPos(msg.completedPath)
		}
		m.playbackPos = msg.timePos
		m.duration = msg.duration
		m.volume = msg.volume
		m.trackList.UpdatePlaybackStatus(msg.currentPath, msg.isPaused)
		m.artistDetail.UpdatePlaybackStatus(msg.currentPath, msg.isPaused)
		m.videoList.UpdatePlaybackStatus(msg.currentPath, msg.isPaused)
		if msg.currentPath != "" && msg.timePos > 0 {
			db.UpdateVideoLastPos(msg.currentPath, msg.timePos)
		}
		if cmd := m.setCoverFromPlaying(); cmd != nil {
			cmds = append(cmds, cmd)
		}
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
			oldAlbumID, _ := m.artistDetail.SelectedAlbumID()
			m.artistDetail, cmd = m.artistDetail.Update(msg)
			cmds = append(cmds, cmd)

			newAlbumID, _ := m.artistDetail.SelectedAlbumID()
			if oldAlbumID != newAlbumID {
				if coverCmd := m.updateCoverForAlbumID(newAlbumID); coverCmd != nil {
					cmds = append(cmds, coverCmd)
				}
			}
		} else if m.activeView == viewVideoLibrary || m.activeView == viewVideoFilter || m.activeView == viewVideoContinue || m.activeView == viewVideoRecent || m.activeView == viewVideoHealth {
			oldIdx := m.videoList.table.GetHighlightedRowIndex()
			var cmd tea.Cmd
			m.videoList, cmd = m.videoList.Update(msg)
			cmds = append(cmds, cmd)
			newIdx := m.videoList.table.GetHighlightedRowIndex()
			if oldIdx != newIdx && newIdx >= 0 && newIdx < len(m.videoList.videos) {
				cmds = append(cmds, m.updateCoverForVideo(m.videoList.videos[newIdx].Path))
			}
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

func (m *model) videoFocusCoverCmd() tea.Cmd {
	if m.focusedSide {
		return nil
	}
	switch m.activeView {
	case viewVideoLibrary, viewVideoFilter, viewVideoContinue, viewVideoRecent, viewVideoHealth:
		idx := m.videoList.table.GetHighlightedRowIndex()
		if idx >= 0 && idx < len(m.videoList.videos) {
			return m.updateCoverForVideo(m.videoList.videos[idx].Path)
		}
	}
	return nil
}

func (m *model) updateCoverForAlbumID(albumID int64) tea.Cmd {
	coverPath := library.GetCachedCoverPath(albumID)
	return m.updateCoverForPath(coverPath)
}

func (m *model) updateCoverForVideo(videoPath string) tea.Cmd {
	posterPath := db.GetVideoPosterPath(videoPath)
	return m.updateCoverForPath(posterPath)
}

func (m *model) updateCoverForPath(path string) tea.Cmd {
	cols := m.getSidebarWidth() - 2
	if cols < 10 {
		cols = 10
	}
	maxRows := (m.height - 5) - 5
	if maxRows < 6 {
		maxRows = 6
	}
	if m.sidebar.SetCoverPath(path, cols, maxRows) {
		return m.coverDisplayCmd()
	}
	return nil
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
	return m.updateCoverForPath(coverPath)
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
	return m.updateCoverForPath(coverPath)
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
			m.musicArtists = newMusicArtists(m.width-sbWidth-3, m.height-6, artists)
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
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-6, artist, albums[artist])
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
	case strings.HasPrefix(n.id, "video_playlist:"):
		idStr := strings.TrimPrefix(n.id, "video_playlist:")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		m.currentPlaylistID = id
		m.activeView = viewVideoLibrary
		m.videoList.ClearMarks()
		m.refreshVideoPlaylistFiles(id)
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

func (m *model) getMarkedMusicPaths() []string {
	if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
		return m.trackList.MarkedPaths()
	} else if m.activeView == viewMusicArtistDetail {
		return m.artistDetail.MarkedPaths()
	}
	return nil
}

func (m model) getCurrentMusicQuery() (string, string, string) {
	var query, artist, album string

	if m.activeView == viewMusicArtistDetail {
		artist = m.artistDetail.artist
		if t, ok := m.artistDetail.SelectedTrack(); ok {
			query = fmt.Sprintf("artist:\"%s\" AND release:\"%s\" AND recording:\"%s\"", t.Artist, t.Album, t.Title)
			artist = t.Artist
			album = t.Album
		} else if a, ok := m.artistDetail.SelectedAlbum(); ok {
			query = fmt.Sprintf("artist:\"%s\" AND release:\"%s\"", artist, a)
			album = a
		} else {
			query = fmt.Sprintf("artist:\"%s\"", artist)
		}
	} else if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
		idx := m.trackList.Cursor()
		if idx >= 0 && idx < len(m.trackList.tracks) {
			t := m.trackList.tracks[idx]
			query = fmt.Sprintf("artist:\"%s\" AND release:\"%s\" AND recording:\"%s\"", t.Artist, t.Album, t.Title)
			artist = t.Artist
			album = t.Album
		}
	} else if m.activeView == viewMusicArtists {
		if a := m.musicArtists.SelectedArtist(); a != "" {
			query = fmt.Sprintf("artist:\"%s\"", a)
			artist = a
		}
	}

	return query, artist, album
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

func (m *model) refreshArtistDetail() {
	if m.activeView != viewMusicArtistDetail || m.artistDetail.artist == "" {
		return
	}
	artist := m.artistDetail.artist
	_, albums, err := db.GetMusicArtistsAndAlbums()
	if err == nil {
		sbWidth := m.getSidebarWidth()
		m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-6, artist, albums[artist])
	}
}

func (m *model) refreshActiveView() {
	if m.activeView == viewMusicLibrary {
		m.refreshTrackList("", "")
	} else if m.activeView == viewMusicRecent {
		m.refreshRecentTracks()
	} else if m.activeView == viewMusicFilter {
		m.refreshFilterTracks(m.currentFilterID)
	} else if m.activeView == viewMusicArtistDetail {
		m.refreshArtistDetail()
	} else if m.activeView == viewVideoLibrary {
		m.refreshVideoList()
	} else if m.activeView == viewVideoContinue {
		m.refreshVideoContinue()
	} else if m.activeView == viewVideoRecent {
		m.refreshVideoRecent()
	} else if m.activeView == viewVideoHealth {
		m.refreshVideoHealth()
	} else if m.activeView == viewVideoFilter {
		m.refreshVideoFilterTracks(m.currentVideoFilterID)
	}
}

func (m *model) refreshTrackList(artist, album string) {
	sbWidth := m.getSidebarWidth()
	m.trackList = newTrackList(m.width-sbWidth-3, m.height-6, artist, album)
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

func (m model) getCurrentMusicPath() string {
	if m.activeView == viewMusicArtistDetail {
		if t, ok := m.artistDetail.SelectedTrack(); ok {
			return t.Path
		}
	} else if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
		idx := m.trackList.Cursor()
		if idx >= 0 && idx < len(m.trackList.tracks) {
			return m.trackList.tracks[idx].Path
		}
	}
	return ""
}

func (m model) handleModalSubmit(result modalUpdateResult) model {
	switch m.pendingAction {
	case actionCreatePlaylist:
		name := result.text
		db.CreateMusicPlaylist(name)
		m.setMessage(fmt.Sprintf("Created playlist '%s'", name))

		var newID int64
		playlists, _ := db.GetMusicPlaylists()
		for _, p := range playlists {
			if p.Name == name {
				newID = p.ID
				if len(m.pendingTracks) > 0 {
					for _, tp := range m.pendingTracks {
						db.AddTrackToMusicPlaylist(newID, tp)
					}
					m.setMessage(fmt.Sprintf("Created playlist '%s' and added %d track(s)", name, len(m.pendingTracks)))
				}
				break
			}
		}

		m.sidebar.Refresh()
		if newID > 0 {
			m.sidebar.ExpandByID("music_playlists")
			m.sidebar.SelectByID(fmt.Sprintf("music_playlist:%d", newID))
			m.currentPlaylistID = newID
			m.activeView = viewMusicLibrary
			m.refreshPlaylistTracks(newID)
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
				m.setMessage(fmt.Sprintf("Added to '%s'", p.Name))
				break
			}
		}
		m.trackList.ClearMarks()
		m.artistDetail.ClearMarks()
		m.modal = nil

	case actionDeletePlaylist:
		db.DeleteMusicPlaylist(m.currentPlaylistID)
		m.setMessage("Deleted playlist")
		m.currentPlaylistID = 0
		m.sidebar.Refresh()
		m.sidebar.ExpandByID("music_playlists")
		m.sidebar.SelectByID("music_playlists")
		m.focusedSide = true
		m.trackList.ClearMarks()
		m.artistDetail.ClearMarks()
		m.modal = nil

	case actionRemoveTrack:
		for _, tp := range m.pendingTracks {
			db.RemoveTrackFromMusicPlaylist(m.currentPlaylistID, tp)
		}
		m.setMessage("Removed from playlist")
		m.refreshPlaylistTracks(m.currentPlaylistID)
		m.trackList.ClearMarks()
		m.modal = nil

	case actionCreateVideoPlaylist:
		name := result.text
		db.CreateVideoPlaylist(name)
		m.setMessage(fmt.Sprintf("Created video playlist '%s'", name))

		var newID int64
		playlists, _ := db.GetVideoPlaylists()
		for _, p := range playlists {
			if p.Name == name {
				newID = p.ID
				if len(m.pendingVideoFiles) > 0 {
					for _, path := range m.pendingVideoFiles {
						db.AddFileToVideoPlaylist(newID, path)
					}
					m.setMessage(fmt.Sprintf("Created video playlist '%s' and added %d item(s)", name, len(m.pendingVideoFiles)))
				}
				break
			}
		}

		m.sidebar.Refresh()
		if newID > 0 {
			m.sidebar.ExpandByID("video_playlists")
			m.sidebar.SelectByID(fmt.Sprintf("video_playlist:%d", newID))
			m.currentPlaylistID = newID
			m.activeView = viewVideoLibrary
			m.refreshVideoPlaylistFiles(newID)
		}
		m.modal = nil

	case actionAddToVideoPlaylist:
		if result.text == "[New Playlist...]" {
			m.pendingAction = actionCreateVideoPlaylist
			m.modal = newTextInputModal(
				"New Video Playlist Name:",
				"Enter playlist name...",
				"Enter: Create  Esc: Cancel",
			)
			m.modal.SetSize(m.width, m.height)
			return m
		}
		playlists, _ := db.GetVideoPlaylists()
		for _, p := range playlists {
			if p.Name == result.text {
				for _, path := range m.pendingVideoFiles {
					db.AddFileToVideoPlaylist(p.ID, path)
				}
				m.setMessage(fmt.Sprintf("Added to '%s'", p.Name))
				break
			}
		}
		m.videoList.ClearMarks()
		m.modal = nil

	case actionDeleteVideoPlaylist:
		db.DeleteVideoPlaylist(m.currentPlaylistID)
		m.setMessage("Deleted video playlist")
		m.currentPlaylistID = 0
		m.sidebar.Refresh()
		m.sidebar.ExpandByID("video_playlists")
		m.sidebar.SelectByID("video_playlists")
		m.focusedSide = true
		m.videoList.ClearMarks()
		m.modal = nil

	case actionExportPlaylist:
		m.exportCurrentPlaylist(result.text)
		m.pendingTracks = nil
		m.modal = nil

	case actionExportVideoPlaylist:
		m.exportCurrentVideoPlaylist(result.text)
		m.pendingVideoFiles = nil
		m.modal = nil

	case actionRemoveVideoFile:
		for _, path := range m.pendingVideoFiles {
			db.RemoveFileFromVideoPlaylist(m.currentPlaylistID, path)
		}
		m.setMessage("Removed from video playlist")
		m.refreshVideoPlaylistFiles(m.currentPlaylistID)
		m.videoList.ClearMarks()
		m.modal = nil

	case actionEditTrack:
		if m.musicFetchMetadata != nil {
			path := m.getCurrentMusicPath()
			if len(m.musicFetchMarkedPaths) == 1 {
				path = m.musicFetchMarkedPaths[0]
			}
			if path != "" {
				m.applyMusicMetadata(m.musicFetchMetadata, []string{path}, true)
			}
			m.musicFetchMarkedPaths = nil
			m.musicFetchMetadata = nil
			m.modal = nil
			m.refreshActiveView()
			return m
		}
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
		m.setMessage(fmt.Sprintf("Updated %d track(s)", len(m.editPaths)))
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
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-3, m.height-6, m.artistDetail.artist, m.artistDetail.albums)
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
		newGenre := strings.TrimSpace(result.values[3])

		if newArtist == "" {
			m.setMessage("Artist name cannot be empty")
			m.modal = nil
			m.editFieldNames = nil
			m.editPaths = nil
			return m
		}

		// Fetch original values to check if tree rebuild is needed
		var origArtist, origAlbum string
		origArtist, origAlbum, _ = db.GetAlbumArtistTitle(m.editAlbumID)

		if err := db.UpdateAlbumMetadata(m.editAlbumID, newArtist, newAlbum, newDate); err != nil {
			m.setMessage(fmt.Sprintf("Failed to update album: %v", err))
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
			if newGenre != "" {
				tags["genre"] = newGenre
				db.UpdateTrackField(path, "genre", newGenre)
			}
			if err := library.WriteAudioTags(path, tags); err != nil {
				log.Printf("Failed to write tags to %s: %v", path, err)
			}
			db.UpdateTrackMTime(path, float64(time.Now().Unix()))
		}

		m.setMessage(fmt.Sprintf("Updated album: %s - %s", newArtist, newAlbum))

		needsTreeRebuild := newArtist != origArtist || newAlbum != origAlbum

		if needsTreeRebuild {
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
		m.setMessage(fmt.Sprintf("Deleted view '%s'", m.currentFilterName))
		m.currentFilterID = 0
		m.currentFilterName = ""
		m.activeView = viewMusicArtists
		m.sidebar.Refresh()
		m.modal = nil

	case actionCreateVideoFilter:
		m.modal = nil

	case actionDeleteVideoFilter:
		db.DeleteVideoFilter(m.currentVideoFilterID)
		m.setMessage(fmt.Sprintf("Deleted view '%s'", m.currentVideoFilterName))
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
			m.setMessage(fmt.Sprintf("Updated %d video(s)", len(m.editVideoPaths)))
		} else {
			m.setMessage("No changes")
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

	m.setMessage("Fetching TMDB metadata...")

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
		m.videoEdit.fields[6].Input.SetValue("0")
	}

	if meta.Episode > 0 {
		m.videoEdit.fields[7].Input.SetValue(strconv.Itoa(meta.Episode))
	} else {
		m.videoEdit.fields[7].Input.SetValue("0")
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
					m.videoEdit.SetFieldValue(12, fullPosterURL) // poster_path
					m.videoEdit.SetFieldValue(13, localPath)     // local_poster_path
					log.Printf("Poster saved to: %s, videoEdit.PosterPath()=%s", localPath, m.videoEdit.PosterPath())
				}
			}
		}
	}

	m.setMessage(fmt.Sprintf("Applied: %s", meta.Title))
}

func (m model) handleVideoEditSubmit(values []string) model {
	for i, field := range m.editVideoFieldNames {
		if i >= len(values) {
			break
		}
		val := values[i]
		// Skip empty strings for integer fields to preserve existing data
		if val == "" && (field == "season" || field == "episode" || field == "year") {
			continue
		}
		// Sanitize: ensure season/episode are numeric
		if (field == "season" || field == "episode") {
			if _, err := strconv.Atoi(val); err != nil {
				val = "0"
			}
		}
		for _, path := range m.editVideoPaths {
			if err := db.UpdateVideoField(path, field, val); err != nil {
				log.Printf("Failed to update video %s field %s: %v", path, field, err)
			}
		}
	}

	if len(m.editVideoPaths) > 0 {
		m.setMessage(fmt.Sprintf("Updated %d video(s)", len(m.editVideoPaths)))
	} else {
		m.setMessage("No changes")
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

// Incremental search
func (m *model) enterSearch() {
	m.searchMode = true
	m.searchQuery = ""
	m.savedView = -1
	switch m.activeView {
	case viewMusicArtists:
		tracks, err := db.GetMusicTracks("", "")
		if err == nil && len(tracks) > 0 {
			m.searchOrigTracks = make([]db.TrackData, len(tracks))
			copy(m.searchOrigTracks, tracks)
			m.savedView = m.activeView
			m.activeView = viewMusicLibrary
			sbWidth := m.getSidebarWidth()
			mainWidth := m.width - sbWidth - 2
			if mainWidth < 1 {
				mainWidth = 1
			}
			middleHeight := m.height - 6
			if middleHeight < 1 {
				middleHeight = 1
			}
			m.trackList = newTrackListFromTracks(mainWidth, middleHeight, m.searchOrigTracks)
			m.syncFocus()
		} else {
			m.searchMode = false
		}
	case viewVideoLibrary:
		if len(m.videoList.videos) == 0 {
			m.searchMode = false
			return
		}
		m.searchOrigVideos = make([]db.VideoData, len(m.videoList.videos))
		copy(m.searchOrigVideos, m.videoList.videos)
	default:
		m.searchMode = false
	}
}

func (m *model) exitSearch() {
	m.searchMode = false
	m.searchQuery = ""
	if m.savedView >= 0 {
		m.activeView = m.savedView
		m.savedView = -1
		m.searchOrigTracks = nil
		m.searchOrigVideos = nil
		m.syncFocus()
		return
	}
	sbWidth := m.getSidebarWidth()
	mainWidth := m.width - sbWidth - 2
	if mainWidth < 1 {
		mainWidth = 1
	}
	middleHeight := m.height - 6
	if middleHeight < 1 {
		middleHeight = 1
	}
	if len(m.searchOrigTracks) > 0 {
		m.trackList = newTrackListFromTracks(mainWidth, middleHeight, m.searchOrigTracks)
		m.searchOrigTracks = nil
	}
	if len(m.searchOrigVideos) > 0 {
		m.videoList = newVideoListFromVideos(mainWidth, middleHeight, m.searchOrigVideos)
		m.searchOrigVideos = nil
	}
}

func (m *model) applySearchFilter() {
	q := strings.ToLower(m.searchQuery)
	if q == "" {
		sbWidth := m.getSidebarWidth()
		mainWidth := m.width - sbWidth - 2
		if mainWidth < 1 {
			mainWidth = 1
		}
		middleHeight := m.height - 6
		if middleHeight < 1 {
			middleHeight = 1
		}
		if len(m.searchOrigTracks) > 0 {
			m.trackList = newTrackListFromTracks(mainWidth, middleHeight, m.searchOrigTracks)
		}
		if len(m.searchOrigVideos) > 0 {
			m.videoList = newVideoListFromVideos(mainWidth, middleHeight, m.searchOrigVideos)
		}
		return
	}
	sbWidth := m.getSidebarWidth()
	mainWidth := m.width - sbWidth - 2
	if mainWidth < 1 {
		mainWidth = 1
	}
	middleHeight := m.height - 6
	if middleHeight < 1 {
		middleHeight = 1
	}
	if len(m.searchOrigTracks) > 0 {
		var filtered []db.TrackData
		for _, t := range m.searchOrigTracks {
			if strings.Contains(strings.ToLower(t.Title), q) ||
				strings.Contains(strings.ToLower(t.Artist), q) ||
				strings.Contains(strings.ToLower(t.Album), q) {
				filtered = append(filtered, t)
			}
		}
		m.trackList = newTrackListFromTracks(mainWidth, middleHeight, filtered)
	}
	if len(m.searchOrigVideos) > 0 {
		var filtered []db.VideoData
		for _, v := range m.searchOrigVideos {
			if strings.Contains(strings.ToLower(v.Title), q) ||
				strings.Contains(strings.ToLower(v.Series), q) ||
				strings.Contains(strings.ToLower(v.Filename), q) ||
				strings.Contains(strings.ToLower(v.Category), q) {
				filtered = append(filtered, v)
			}
		}
		m.videoList = newVideoListFromVideos(mainWidth, middleHeight, filtered)
	}
}

// M3U Export
func (m *model) exportCurrentPlaylist(filename string) {
	if m.currentPlaylistID <= 0 {
		return
	}
	tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
	dir := m.config.Playlist.MusicPlaylistDir
	if dir == "" {
		configDir, err := config.GetConfigDir()
		if err == nil {
			dir = filepath.Join(configDir, "playlists")
		}
	}
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		m.setMessage(fmt.Sprintf("Export failed: %v", err))
		return
	}
	defer f.Close()
	fmt.Fprintln(f, "#EXTM3U")
	for _, t := range tracks {
		fmt.Fprintf(f, "#EXTINF:%d,%s - %s\n", t.Duration, t.Artist, t.Title)
		fmt.Fprintln(f, t.Path)
	}
	m.setMessage(fmt.Sprintf("Exported %d tracks to %s", len(tracks), path))
}

func (m *model) exportCurrentVideoPlaylist(filename string) {
	if m.currentPlaylistID <= 0 {
		return
	}
	files, _ := db.GetVideoPlaylistFiles(m.currentPlaylistID)
	dir := m.config.Playlist.VideoPlaylistDir
	if dir == "" {
		configDir, err := config.GetConfigDir()
		if err == nil {
			dir = filepath.Join(configDir, "playlists")
		}
	}
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		m.setMessage(fmt.Sprintf("Export failed: %v", err))
		return
	}
	defer f.Close()
	fmt.Fprintln(f, "#EXTM3U")
	for _, v := range files {
		fmt.Fprintf(f, "#EXTINF:%d,%s\n", v.Duration, v.Title)
		fmt.Fprintln(f, v.Path)
	}
	m.setMessage(fmt.Sprintf("Exported %d videos to %s", len(files), path))
}

// Keyboard help
func (m *model) helpOverlayView(width int) string {
	helpText := `  Key         Action
  ─────────────────────────────────────
  q / Ctrl+C  Quit
  Tab         Toggle sidebar/content focus
  ↑/↓         Navigate list/tree
  PgUp/PgDn   Scroll page up/down
  →/←         Expand/collapse tree node
  ←/→         Seek -5/+5 sec (content focused)
  H/L         Seek -10/+10 sec
  Enter       Play selected
  Space       Pause/Resume
  /           Search current list
  s           Scan library
  Ctrl+r      Refresh sidebar & list
  n           Create filter (sidebar, Views node)
  d           Delete item (playlist/track/filter)
  a           Add tracks to playlist
  e           Edit metadata (multi-select: batch edit)
  E           Export playlist to M3U
  Ctrl+t      MusicBrainz search
  Ctrl+s      TMDB search / M3U import
  F1          Show this help`
	return lipgloss.NewStyle().
		Width(width - 4).
		Height(22).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("5")).
		Render(helpText)
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

func (m *model) coverClearCmd() tea.Cmd {
	return func() tea.Msg {
		m.tty.WriteString("\x1b_Ga=d,i=1\x1b\\")
		m.tty.Sync()
		return nil
	}
}

func (m *model) writeImageToTTY(data string, startRow, startCol int) {
	if strings.HasPrefix(data, "\x1b_G") || strings.HasPrefix(data, "\x1bP") {
		// Kitty protocol (\x1b_G) or Sixel protocol (\x1bP)
		fmt.Fprintf(m.tty, "\x1b[%d;%dH", startRow, startCol)
		m.tty.WriteString(data)
	} else {
		// ANSI block graphics (Halfblocks): split by newline and write line-by-line
		lines := strings.Split(data, "\n")
		for i, line := range lines {
			if line == "" && i == len(lines)-1 {
				break
			}
			fmt.Fprintf(m.tty, "\x1b[%d;%dH", startRow+i, startCol)
			m.tty.WriteString(line)
		}
	}
}

func (m *model) coverDisplayCmd() tea.Cmd {
	path := m.sidebar.CoverPath()
	if path == "" || !m.sidebar.HasCover() {
		return m.coverClearCmd()
	}
	cols := m.sidebar.CoverCols()
	rows := m.sidebar.CoverRows()
	return func() tea.Msg {
		data, err := m.displayer.Render(path, cols, rows)
		if err != nil {
			return nil
		}

		kittyOut := string(data)
		if strings.HasPrefix(kittyOut, "\x1b_G") {
			kittyOut = "\x1b_Gi=1," + kittyOut[3:]
		}

		m.tty.WriteString("\x1b[?25l")
		m.tty.WriteString("\x1b_Ga=d,i=1\x1b\\")
		m.writeImageToTTY(kittyOut, m.sidebar.CoverRow()+1, 1)
		m.tty.WriteString("\x1b[?25l")
		m.tty.Sync()
		return nil
	}
}

func (m *model) coverPlaceCmd() tea.Cmd {
	return m.coverDisplayCmd()
}

func (m *model) syncPosterCmd() tea.Cmd {
	if m.videoFetch == nil || m.videoFetch.CachedKitty() == "" {
		return nil
	}
	row, col, _, _ := m.posterPos()
	kitty := m.videoFetch.CachedKitty()
	return func() tea.Msg {
		log.Printf("[DEBUGLOG] syncPosterCmd: start writing to TTY (ID=3) at row=%d col=%d len=%d", row+1, col+1, len(kitty))
		m.tty.WriteString("\x1b[?25l")
		m.tty.WriteString("\x1b[s")
		m.tty.WriteString("\x1b_Ga=d,i=3\x1b\\")
		m.writeImageToTTY(kitty, row+1, col+1)
		m.tty.WriteString("\x1b[u")
		m.tty.WriteString("\x1b[?25l")
		m.tty.Sync()
		log.Printf("[DEBUGLOG] syncPosterCmd: finished writing")
		return nil
	}
}

func (m *model) syncVideoEditPosterCmd() tea.Cmd {
	if m.videoEdit == nil || m.videoEdit.CachedKitty() == "" {
		return nil
	}
	row, col, _, _ := m.videoEditPosterPos()
	kitty := m.videoEdit.CachedKitty()
	return func() tea.Msg {
		log.Printf("[DEBUGLOG] syncVideoEditPosterCmd: start writing to TTY (ID=2) at row=%d col=%d len=%d", row+1, col+1, len(kitty))
		m.tty.WriteString("\x1b[?25l")
		m.tty.WriteString("\x1b[s")
		m.tty.WriteString("\x1b_Ga=d,i=2\x1b\\")
		m.writeImageToTTY(kitty, row+1, col+1)
		m.tty.WriteString("\x1b[u")
		m.tty.WriteString("\x1b[?25l")
		m.tty.Sync()
		log.Printf("[DEBUGLOG] syncVideoEditPosterCmd: finished writing")
		return nil
	}
}

func (m *model) posterPos() (row, col, w, h int) {
	if m.videoFetch == nil {
		return 0, 0, 0, 0
	}
	sl := m.videoFetch.OverlayStartLine()
	sc := m.videoFetch.OverlayStartCol()
	// sl is the starting line of the modal border (row 0 of the modal)
	// Modal content starts at row 1 (inside the top border)
	// PosterRow() tells us where the poster block starts relative to content top
	row = sl + 1 + m.videoFetch.PosterRow()
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
		m.tty.WriteString("\x1b[?25l")
		m.tty.WriteString("\x1b[s")
		fmt.Fprintf(m.tty, "\x1b[%d;%dH", row+1, col+1)
		m.tty.WriteString("\x1b_Ga=d,i=3\x1b\\")
		m.tty.WriteString("\x1b[u")
		m.tty.WriteString("\x1b[?25l")
		m.tty.Sync()
		return nil
	}
}

type posterReadyMsg struct {
	kitty  string
	isEdit bool
}

func (m *model) posterDisplayCmd() tea.Cmd {
	if m.videoFetch == nil || m.videoFetch.PosterPath() == "" {
		return nil
	}
	path := m.videoFetch.PosterPath()
	cols := m.videoFetch.PosterCols()
	rows := m.videoFetch.PosterRows()

	return func() tea.Msg {
		if path == "" {
			return nil
		}

		data, err := m.displayer.Render(path, cols, rows)
		if err != nil {
			return nil
		}

		kittyOut := string(data)
		log.Printf("[DEBUGLOG] posterDisplayCmd: render output len=%d, path=%s", len(kittyOut), path)

		kittyOut = strings.ReplaceAll(kittyOut, "\x1b_G", "\x1b_Gi=3,")
		if len(kittyOut) > 50 {
			log.Printf("[DEBUGLOG] posterDisplayCmd: first 50 chars: %q", kittyOut[:50])
		}

		return posterReadyMsg{kitty: kittyOut, isEdit: false}
	}
}

func (m *model) videoEditPosterClearCmd() tea.Cmd {
	return func() tea.Msg {
		m.tty.WriteString("\x1b_Ga=d,i=2\x1b\\")
		m.tty.WriteString("\x1b[?25l")
		m.tty.Sync()
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

func (m *model) videoEditPosterDisplayCmd() tea.Cmd {
	if m.videoEdit == nil || m.videoEdit.PosterPath() == "" {
		return nil
	}
	path := m.videoEdit.PosterPath()
	cols := m.videoEdit.PosterCols()
	rows := m.videoEdit.PosterRows()

	return func() tea.Msg {
		if path == "" {
			return nil
		}

		data, err := m.displayer.Render(path, cols, rows)
		if err != nil {
			return nil
		}

		kittyOut := string(data)
		log.Printf("[DEBUGLOG] videoEditPosterDisplayCmd: render output len=%d, path=%s", len(kittyOut), path)

		kittyOut = strings.ReplaceAll(kittyOut, "\x1b_G", "\x1b_Gi=2,")
		if len(kittyOut) > 50 {
			log.Printf("[DEBUGLOG] videoEditPosterDisplayCmd: first 50 chars: %q", kittyOut[:50])
		}

		return posterReadyMsg{kitty: kittyOut, isEdit: true}
	}
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Every(250*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type playerStateMsg struct {
	timePos       float64
	duration      float64
	volume        float64
	isPaused      bool
	currentPath   string
	completedPath string
}

func fetchPlayerStateCmd(p *player.MpvPlayer) tea.Cmd {
	return func() tea.Msg {
		completedPath := p.ConsumeCompletedPath()

		msg := playerStateMsg{
			completedPath: completedPath,
		}

		if val, err := p.TryGetProperty("time-pos"); err == nil {
			msg.timePos, _ = val.(float64)
		}
		if val, err := p.TryGetProperty("duration"); err == nil {
			msg.duration, _ = val.(float64)
		}
		if val, err := p.TryGetProperty("volume"); err == nil {
			msg.volume, _ = val.(float64)
		}
		if valPause, err := p.TryGetProperty("pause"); err == nil {
			msg.isPaused, _ = valPause.(bool)
		}
		msg.currentPath = p.GetCurrentTrackPath()

		return msg
	}
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
	count     int
	cancelled bool
}

type scanProgressMsg struct {
	current     int
	total       int
	currentFile string
}

type scanCountMsg struct {
	total int
}

type scanReadyMsg struct {
	tracks []library.Track
	videos []library.Video
}

type tmdbBatchProgressMsg struct {
	current int
	total   int
}

type musicBatchProgressMsg struct {
	current int
	total   int
}

func (m model) scanMusicCmd() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return scanCountMsg{total: library.CountAudioFiles(m.config.Music.Directories)}
		},
		func() tea.Msg {
			tracks, err := library.ScanMusic(m.config.Music.Directories)
			if err != nil {
				return scanReadyMsg{}
			}
			return scanReadyMsg{tracks: tracks}
		},
	)
}

func (m model) scanVideoCmd() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return scanCountMsg{total: library.CountVideoFiles(m.config.Video.Directories)}
		},
		func() tea.Msg {
			videos, err := library.ScanVideo(m.config.Video.Directories)
			if err != nil {
				return scanReadyMsg{}
			}
			return scanReadyMsg{videos: videos}
		},
	)
}

func (m model) processNextScanItem() tea.Cmd {
	return func() tea.Msg {
		currentFile := ""
		if m.scanPhase == "music" {
			t := m.scanTracks[m.scanIndex]
			currentFile = t.Path
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
			}, false)
		} else {
			v := m.scanVideos[m.scanIndex]
			currentFile = v.Path
			db.UpdateVideoFile(db.VideoData{
				Path:     v.Path,
				Filename: v.Filename,
				Size:     v.Size,
				Duration: v.Duration,
				Year:     v.Year,
				MTime:    v.MTime,
			})
		}
		return scanProgressMsg{current: m.scanIndex + 1, total: m.scanTotal, currentFile: currentFile}
	}
}

func (m model) processNextTMDbItem() tea.Cmd {
	return func() tea.Msg {
		v := m.tmdbBatchVideos[m.tmdbBatchIndex]
		apiKey := m.config.Video.TMDBAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("TMDB_API_KEY")
		}
		if apiKey == "" {
			return tmdbBatchProgressMsg{current: m.tmdbBatchIndex + 1, total: m.tmdbBatchTotal}
		}
		client := tmdb.NewClient(apiKey)

		var meta *tmdb.VideoMetadata
		if v.Type == "TV Show" && v.Series != "" {
			items, err := client.SearchTV(v.Series)
			if err == nil && len(items) > 0 {
				meta, _ = client.FetchVideoMetadataByID(items[0].ID, true, v.Season, v.Episode)
			}
		} else {
			query := v.Title
			if query == "" {
				query = v.Filename
			}
			if query != "" {
				items, err := client.SearchMovie(query)
				if err == nil {
					// Prefer exact title match
					for _, item := range items {
						if item.Title == query {
							meta, _ = client.FetchVideoMetadataByID(item.ID, false, 0, 0)
							break
						}
					}
					if meta == nil && len(items) > 0 {
						meta, _ = client.FetchVideoMetadataByID(items[0].ID, false, 0, 0)
					}
				}
			}
		}

		if meta != nil {
			m.applyBatchTMDBMetadata(v.Path, meta)
		}
		return tmdbBatchProgressMsg{current: m.tmdbBatchIndex + 1, total: m.tmdbBatchTotal}
	}
}

func (m model) processNextMusicItem() tea.Cmd {
	return func() tea.Msg {
		if m.musicBatchCancelled || m.musicBatchIndex >= len(m.musicBatchPaths) {
			return musicBatchProgressMsg{current: m.musicBatchIndex, total: m.musicBatchTotal}
		}

		path := m.musicBatchPaths[m.musicBatchIndex]
		track, err := db.GetMusicTrackByPath(path)
		if err != nil {
			return musicBatchProgressMsg{current: m.musicBatchIndex + 1, total: m.musicBatchTotal}
		}

		client := musicbrainz.NewClient()
		// Search by Artist + Album + Title for high precision
		query := fmt.Sprintf("artist:\"%s\" AND release:\"%s\" AND recording:\"%s\"",
			musicbrainz.EscapeQuery(track.Artist), musicbrainz.EscapeQuery(track.Album), musicbrainz.EscapeQuery(track.Title))

		recs, err := client.SearchRecordings(query)
		if err == nil && len(recs) > 0 {
			// Take first result and fetch full release info
			info, err := client.GetRelease(recs[0].ReleaseID)
			if err == nil {
				m.applyMusicMetadata(info, []string{path}, false) // force=false (empty only)
			}
		}

		return musicBatchProgressMsg{current: m.musicBatchIndex + 1, total: m.musicBatchTotal}
	}
}

func (m *model) applyMusicMetadata(meta *musicbrainz.ReleaseInfo, paths []string, force bool) {
	if meta == nil || len(paths) == 0 {
		return
	}

	updatedCount := 0
	for _, path := range paths {
		track, err := db.GetMusicTrackByPath(path)
		if err != nil {
			continue
		}

		// Try to find matching track in metadata by track number
		var match *musicbrainz.ReleaseTrack
		for _, mt := range meta.Tracks {
			if mt.Position == track.TrackNum {
				match = &mt
				break
			}
		}

		// If no track number match, and it's a single track update, take the first/highlighted one?
		// Actually, if it's a single track, we might want to let the user select the track from the list.
		// For now, let's assume if it's single, we match by whatever we have or just album info.

		title := track.Title
		if match != nil {
			title = match.Title
		}

		// Prepare updated track data
		newTrack := db.TrackData{
			Path:        track.Path,
			MTime:       track.MTime,
			Title:       title,
			Artist:      meta.Artist,
			Album:       meta.Title,
			AlbumArtist: meta.Artist,
			TrackNum:    track.TrackNum,
			DiscNum:     track.DiscNum,
			Genre:       track.Genre,
			Duration:    track.Duration,
		}

		if err := db.UpdateMusicTrack(newTrack, force); err == nil {
			updatedCount++
			// Update album-level info too
			if albumID, err := db.GetAlbumIDByTrackPath(path); err == nil {
				db.UpdateAlbumMBID(albumID, meta.ID, force)
				db.UpdateAlbumDate(albumID, meta.Date, force)

				// Fetch cover in background if MBID is available
				if meta.ID != "" {
					go func(aid int64, mid string) {
						library.FetchAndCacheMBICover(aid, mid)
					}(albumID, meta.ID)
				}
			}
		}
	}

	m.setMessage(fmt.Sprintf("Applied metadata to %d track(s)", updatedCount))
	if updatedCount > 0 {
		db.DeleteEmptyAlbums()
	}
	
	// Refresh view
	if m.activeView == viewMusicLibrary {
		artist, album := m.getCurrentFilter()
		m.refreshTrackList(artist, album)
	} else if m.activeView == viewMusicArtistDetail {
		// Re-load current artist/album
		if n := m.sidebar.SelectedNode(); n != nil {
			m.handleSidebarChange(n)
		}
	}
}

func (m *model) applyBatchTMDBMetadata(path string, meta *tmdb.VideoMetadata) {
	if meta.Title != "" {
		db.UpdateVideoFieldIfEmpty(path, "title", meta.Title)
	}
	if meta.Series != "" {
		db.UpdateVideoFieldIfEmpty(path, "series", meta.Series)
	}
	if meta.Season > 0 {
		db.UpdateVideoFieldIfEmpty(path, "season", fmt.Sprintf("%d", meta.Season))
	}
	if meta.Episode > 0 {
		db.UpdateVideoFieldIfEmpty(path, "episode", fmt.Sprintf("%d", meta.Episode))
	}
	if meta.AirDate != "" {
		db.UpdateVideoFieldIfEmpty(path, "air_date", meta.AirDate)
	}
	if len(meta.Genres) > 0 {
		db.UpdateVideoFieldIfEmpty(path, "genres", strings.Join(meta.Genres, ", "))
	}
	if meta.Synopsis != "" {
		db.UpdateVideoFieldIfEmpty(path, "synopsis", meta.Synopsis)
	}
	if meta.SeriesOverview != "" {
		db.UpdateVideoFieldIfEmpty(path, "series_overview", meta.SeriesOverview)
	}
	if meta.EpisodeOverview != "" {
		db.UpdateVideoFieldIfEmpty(path, "episode_overview", meta.EpisodeOverview)
	}
	if meta.Series != "" {
		db.UpdateVideoFieldIfEmpty(path, "type", "TV Show")
	} else {
		db.UpdateVideoFieldIfEmpty(path, "type", "Movie")
	}

	// Download poster: episode still > season poster > series/movie poster
	var posterURL string
	var posterName string
	if meta.StillPath != "" && meta.Episode > 0 {
		posterURL = meta.StillPath
		posterName = fmt.Sprintf("tmdb_%d_s%d_e%d", meta.ID, meta.Season, meta.Episode)
	} else if meta.SeasonPosterPath != "" && meta.Season > 0 {
		posterURL = meta.SeasonPosterPath
		posterName = fmt.Sprintf("tmdb_%d_s%d", meta.ID, meta.Season)
	} else if meta.PosterPath != "" {
		posterURL = meta.PosterPath
		posterName = fmt.Sprintf("tmdb_%d", meta.ID)
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
				if err == nil {
					fullPosterURL := "https://image.tmdb.org/t/p/w500" + posterURL
					db.UpdateVideoFieldIfEmpty(path, "poster_path", fullPosterURL)
					db.UpdateVideoFieldIfEmpty(path, "local_poster_path", localPath)
				}
			}
		}
	}
}

func (m *model) setMessage(text string) {
	m.message = text
	m.messageTime = time.Now()
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

	var header string
	if m.searchMode {
		header = m.renderSearchBar()
	} else {
		header = m.renderHeader()
	}
	sbView := m.sidebar.View(m.focusedSide)
	sbWidth := m.getSidebarWidth()

	mainWidth := m.width - sbWidth - 2
	if mainWidth <= 0 {
		mainWidth = 1
	}

	middleHeight := m.height - 6
	if middleHeight < 1 {
		middleHeight = 1
	}
	mainStyle := lipgloss.NewStyle().
		Width(mainWidth).
		Height(middleHeight).
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

	// Join sidebar and main content, then force-truncate/pad to middleHeight.
	// This is critical because lipgloss.Height(n) sets a minimum, not a maximum,
	// so child views returning more than middleHeight lines would overflow the
	// total terminal height and push the footer off-screen.
	midSection := lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr))
	if midLines := strings.Split(midSection, "\n"); len(midLines) > middleHeight {
		midSection = strings.Join(midLines[:middleHeight], "\n")
	} else {
		midSection = lipgloss.NewStyle().Height(middleHeight).Render(midSection)
	}

	footer := m.renderFooter()

	backgroundView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		midSection,
		footer,
	)

	// If any modal is active, overlay it
	var overlay string
	if m.helpOverlay != "" {
		overlay = m.helpOverlay
	} else if m.musicFetch != nil {
		mw := 100
		mh := 25
		if mw > m.width-4 { mw = m.width - 4 }
		if mh > m.height-4 { mh = m.height - 4 }
		m.musicFetch.SetSize(mw, mh)
		overlay = m.musicFetch.View()
	} else if m.videoFetch != nil {
		m.videoFetch.SetSize(m.width-10, m.height-6)
		overlay = m.videoFetch.View()
	} else if m.videoEdit != nil {
		m.videoEdit.SetSize(m.width-10, m.height-6)
		overlay = m.videoEdit.View()
	} else if m.filterCondEdit != nil {
		m.filterCondEdit.SetSize(m.width-10, m.height-6)
		overlay = m.filterCondEdit.View()
	} else if m.sortFieldSelect != nil {
		m.sortFieldSelect.SetSize(m.width-10, m.height-6)
		overlay = m.sortFieldSelect.View()
	} else if m.filterEdit != nil {
		m.filterEdit.SetSize(m.width-10, m.height-6)
		overlay = m.filterEdit.View()
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

	if !m.isAnyInputFocused() {
		mainView += "\x1b[?25l"
	}

	v := tea.NewView(mainView)
	if !m.isAnyInputFocused() {
		v.Cursor = nil
	}
	v.AltScreen = true
	return v
}

func (m model) isAnyInputFocused() bool {
	if m.videoFetch != nil && m.videoFetch.IsQueryFocused() {
		return true
	}
	if m.videoEdit != nil {
		// videoEditModal always has a focused field unless it's a select?
		// But usually we want the cursor for textarea too.
		return true
	}
	if m.modal != nil && (m.modal.kind == modalTextInput || m.modal.kind == modalForm) {
		return true
	}
	if m.filterEdit != nil && m.filterEdit.focusedSection == 0 { // Filter name
		return true
	}
	if m.filterCondEdit != nil && m.filterCondEdit.focusedSection == 2 { // Value input
		return true
	}
	return false
}

func (m model) renderSearchBar() string {
	var count, origCount int
	if len(m.searchOrigTracks) > 0 {
		origCount = len(m.searchOrigTracks)
		count = len(m.trackList.tracks)
	} else if len(m.searchOrigVideos) > 0 {
		origCount = len(m.searchOrigVideos)
		count = len(m.videoList.videos)
	}
	searchStr := fmt.Sprintf(" SEARCH: /%s_", m.searchQuery)
	infoStr := fmt.Sprintf(" %d/%d ", count, origCount)
	avail := m.width - 5 - lipgloss.Width(searchStr) - lipgloss.Width(infoStr) - 6
	if avail > 0 {
		searchStr += strings.Repeat(" ", avail) + infoStr
	} else {
		searchStr += infoStr
	}
	return lipgloss.NewStyle().
		Width(m.width - 5).
		Height(3).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("5")).
		Padding(0, 2).
		Render(searchStr)
}

func (m model) renderHeader() string {
	percent := 0.0
	if m.duration > 0 {
		percent = m.playbackPos / m.duration
	}

	progressStr := m.progress.ViewAs(percent)
	volStr := fmt.Sprintf("Vol: %d%%", int(m.volume))

	var nameStr, timeStr string
	if m.currentTrack == "" {
		nameStr = "Nothing playing"
		timeStr = "0:00 / 0:00"
	} else {
		nameStr = m.currentTrack
		timeStr = fmt.Sprintf("%s / %s", formatDuration(int(m.playbackPos)), formatDuration(int(m.duration)))
	}

	availWidth := m.width - 4
	fixedWidth := len(timeStr) + len(volStr) + 12
	maxNameWidth := availWidth - fixedWidth
	if maxNameWidth < 10 {
		maxNameWidth = 10
	}
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
	if m.modal != nil && m.modal.Active() {
		log.Printf("[DEBUGLOG] renderFooter: Modal active, help=%q", m.modal.help)
		return lipgloss.NewStyle().
			Width(m.width - 5).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 2).
			Render(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.modal.help))
	}

	if m.musicFetch != nil {
		return lipgloss.NewStyle().
			Width(m.width - 5).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 2).
			Render(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.musicFetch.help))
	}

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

	var lines string
	if m.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		lines = msgStyle.Render(m.message)
	} else {
		lines = helpStr
	}

	return lipgloss.NewStyle().
		Width(m.width - 5).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render(lines)
}
