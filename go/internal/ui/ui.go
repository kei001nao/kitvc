package ui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
	viewMusicRecent
	viewMusicFilter
)

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
	playingAlbumID    int64
	currentFilterID   int64
	currentFilterName string
	filterEdit        *filterEditModal
	filterCondEdit    *filterConditionModal
	sortFieldSelect   *sortFieldSelectModal
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
		sidebar: newSidebar(cfg.UI.SidebarWidth, 20),
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
						dir := "ASC"
						if len(m.filterEdit.sortSequence) > 0 {
							dir = "DESC"
						}
						m.filterEdit.sortSequence = append(m.filterEdit.sortSequence, [2]string{result.field, dir})
					}
					m.sortFieldSelect = nil
				}
				return m, cmd
			}

			// Handle filter edit actions
			switch keyMsg.String() {
			case "a":
				if m.filterEdit.focusedSection == 1 {
					// Add condition
					m.filterCondEdit = newFilterConditionModal(0, 0, "")
					m.filterCondEdit.SetSize(m.width, m.height)
					return m, nil
				} else if m.filterEdit.focusedSection == 2 {
					// Add sort field
					usedFields := make(map[string]bool)
					for _, s := range m.filterEdit.sortSequence {
						usedFields[s[0]] = true
					}
					m.sortFieldSelect = newSortFieldSelectModal(usedFields)
					m.sortFieldSelect.SetSize(m.width, m.height)
					return m, nil
				}
			case "enter":
				if m.filterEdit.focusedSection == 1 && len(m.filterEdit.conditions) > 0 {
					// Edit condition
					c := m.filterEdit.conditions[m.filterEdit.condCursor]
					fieldIdx := 0
					for i, f := range musicFilterFields {
						if f.Value == c.Field {
							fieldIdx = i
							break
						}
					}
					opIdx := 0
					for i, o := range musicFilterOps {
						if o.Value == c.Op {
							opIdx = i
							break
						}
					}
					m.filterCondEdit = newFilterConditionModal(fieldIdx, opIdx, c.Value)
					m.filterCondEdit.SetSize(m.width, m.height)
					return m, nil
				}
			case "d":
				if m.filterEdit.focusedSection == 1 && len(m.filterEdit.conditions) > 0 {
					// Delete condition
					m.filterEdit.conditions = append(m.filterEdit.conditions[:m.filterEdit.condCursor], m.filterEdit.conditions[m.filterEdit.condCursor+1:]...)
					if m.filterEdit.condCursor >= len(m.filterEdit.conditions) {
						m.filterEdit.condCursor = len(m.filterEdit.conditions) - 1
					}
					if m.filterEdit.condCursor < 0 {
						m.filterEdit.condCursor = 0
					}
					return m, nil
				} else if m.filterEdit.focusedSection == 2 && len(m.filterEdit.sortSequence) > 0 {
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
				if m.filterEdit.focusedSection == 2 && len(m.filterEdit.sortSequence) > 0 {
					// Move sort up
					idx := m.filterEdit.sortCursor
					if idx > 0 {
						m.filterEdit.sortSequence[idx], m.filterEdit.sortSequence[idx-1] = m.filterEdit.sortSequence[idx-1], m.filterEdit.sortSequence[idx]
						m.filterEdit.sortCursor--
					}
					return m, nil
				}
			case "-":
				if m.filterEdit.focusedSection == 2 && len(m.filterEdit.sortSequence) > 0 {
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
					db.CreateMusicFilter(result.name, result.condJSON, result.sortJSON)
					m.message = fmt.Sprintf("Created view '%s'", result.name)
					m.sidebar.Refresh()
				} else if m.pendingAction == actionEditMusicFilter {
					db.UpdateMusicFilter(m.currentFilterID, result.name, result.condJSON, result.sortJSON)
					m.message = fmt.Sprintf("Updated view '%s'", result.name)
					m.currentFilterName = result.name
					m.sidebar.Refresh()
					// Refresh the current filter view
					m.refreshFilterTracks(m.currentFilterID)
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
		}
		return m, cmd
	}

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
							if t.Title == selected[3] && t.Artist == selected[4] {
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
					selected := m.trackList.table.SelectedRow()
					if len(selected) > 0 {
						tracks := m.trackList.tracks
						
						var paths []string
						startIndex := -1
						for i, t := range tracks {
							paths = append(paths, t.Path)
							if t.Title == selected[3] && t.Artist == selected[4] {
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
					markedPaths := m.trackList.MarkedPaths()
					if len(markedPaths) > 0 {
						m.pendingAction = actionRemoveTrack
						m.pendingTracks = markedPaths
						m.modal = newConfirmModal(fmt.Sprintf("Remove %d marked tracks from the playlist?", len(markedPaths)))
						if m.modal != nil {
							m.modal.SetSize(m.width, m.height)
						}
					} else {
						selected := m.trackList.table.SelectedRow()
						if len(selected) > 0 {
							tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
							for _, t := range tracks {
								if t.Title == selected[3] && t.Artist == selected[4] {
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
			}
			return m, nil
			}
		case "shift+up":
			if !m.focusedSide && m.currentPlaylistID > 0 && m.activeView == viewMusicLibrary {
				tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
				if len(tracks) > 0 {
					cursor := m.trackList.table.Cursor()
					if cursor > 0 {
						db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor-1)
						m.refreshPlaylistTracks(m.currentPlaylistID)
						m.trackList.table.SetCursor(cursor - 1)
					}
				}
				return m, nil
			}
		case "shift+down":
			if !m.focusedSide && m.currentPlaylistID > 0 && m.activeView == viewMusicLibrary {
				tracks, _ := db.GetMusicPlaylistTracks(m.currentPlaylistID)
				if len(tracks) > 0 {
					cursor := m.trackList.table.Cursor()
					if cursor < len(tracks)-1 {
						db.MoveMusicPlaylistTrack(m.currentPlaylistID, cursor, cursor+1)
						m.refreshPlaylistTracks(m.currentPlaylistID)
						m.trackList.table.SetCursor(cursor + 1)
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
						selected := m.trackList.table.SelectedRow()
						if len(selected) > 0 {
							artist, album := m.getCurrentFilter()
							tracks, _ := db.GetMusicTracks(artist, album)
							for _, t := range tracks {
								if t.Title == selected[3] && t.Artist == selected[4] {
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
						selected := m.trackList.table.SelectedRow()
						if len(selected) > 0 {
							for _, t := range m.trackList.tracks {
								if t.Title == selected[3] && t.Artist == selected[4] {
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
					m.filterEdit = newFilterEditModal("", "[]", "[]")
					m.filterEdit.SetSize(m.width, m.height)
				}
			}
			return m, nil
		case "m":
			if !m.focusedSide {
				if m.activeView == viewMusicLibrary || m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
					rows := m.trackList.table.Rows()
					cursor := m.trackList.table.Cursor()
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
						m.trackList.table.SetCursor(newCursor)
					}
				} else if m.activeView == viewMusicArtistDetail && !m.artistDetail.focusedUpper {
					rows := m.artistDetail.tracksTable.Rows()
					cursor := m.artistDetail.tracksTable.Cursor()
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
						m.artistDetail.tracksTable.SetCursor(newCursor)
					}
				}
			}
			return m, nil
		case "e":
			if !m.focusedSide && m.activeView == viewMusicFilter && m.currentFilterID > 0 {
				filter, err := db.GetMusicFilterByID(m.currentFilterID)
				if err == nil {
					m.pendingAction = actionEditMusicFilter
					m.filterEdit = newFilterEditModal(filter.Name, filter.ConditionsJSON, filter.SortJSON)
					m.filterEdit.SetSize(m.width, m.height)
				}
				return m, nil
			} else if !m.focusedSide {
				if m.activeView == viewMusicArtistDetail && m.artistDetail.focusedUpper {
					cursor := m.artistDetail.albumsTable.Cursor()
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
							cursor := m.trackList.table.Cursor()
							if cursor >= 0 && cursor < len(m.trackList.tracks) {
								selectedTracks = []db.TrackData{m.trackList.tracks[cursor]}
							}
						}
					} else if m.activeView == viewMusicRecent || m.activeView == viewMusicFilter {
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
							cursor := m.trackList.table.Cursor()
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
				return m, nil
			}
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
		}
		// Update cover for current selection
		if n := m.sidebar.SelectedNode(); n != nil {
			if cmd := m.updateCoverForNode(n); cmd != nil {
				cmds = append(cmds, cmd)
			}
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

			valPause, _ := m.player.GetProperty("pause")
			isPaused := false
			if p, ok := valPause.(bool); ok {
				isPaused = p
			}
			currentPath := m.player.GetCurrentTrackPath()
			m.trackList.UpdatePlaybackStatus(currentPath, isPaused)
			m.artistDetail.UpdatePlaybackStatus(currentPath, isPaused)
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
		} else if m.activeView == viewVideoLibrary {
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
	} else if m.activeView == viewVideoLibrary {
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

	switch {
	case n.id == "music_library":
		m.activeView = viewMusicArtists
		artists, _, err := db.GetMusicArtistsAndAlbums()
		if err == nil {
			sbWidth := m.getSidebarWidth()
			m.musicArtists = newMusicArtists(m.width-sbWidth-1, m.height-8, artists)
		}
	case n.id == "music_recent":
		m.activeView = viewMusicRecent
		m.refreshRecentTracks()
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
		m.refreshVideoPlaylistFiles(id)
	}
	if cmd := m.updateCoverForNode(n); cmd != nil {
		return cmd
	}
	m.syncFocus()
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
	m.videoList = newVideoListFromVideos(m.width-sbWidth-1, m.height-6, videos)
	m.syncFocus()
}

func (m *model) refreshPlaylistTracks(playlistID int64) {
	sbWidth := m.getSidebarWidth()
	tracks, _ := db.GetMusicPlaylistTracks(playlistID)
	oldMarked := m.trackList.marked
	m.trackList = newTrackListFromTracks(m.width-sbWidth-1, m.height-6, tracks)
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
	m.trackList = newTrackList(m.width-sbWidth-1, m.height-6, artist, albumTitle)
	m.syncFocus()
}

func (m *model) refreshRecentTracks() {
	sbWidth := m.getSidebarWidth()
	tracks, _ := db.GetRecentMusicTracks(50)
	m.trackList = newTrackListFromTracks(m.width-sbWidth-1, m.height-6, tracks)
	m.syncFocus()
}

func (m *model) refreshFilterTracks(filterID int64) {
	sbWidth := m.getSidebarWidth()
	filter, err := db.GetMusicFilterByID(filterID)
	if err != nil {
		m.trackList = newTrackListFromTracks(m.width-sbWidth-1, m.height-6, nil)
		m.syncFocus()
		return
	}
	tracks, _ := db.GetFilteredMusicTracks(filter.ConditionsJSON, filter.SortJSON)
	m.trackList = newTrackListFromTracks(m.width-sbWidth-1, m.height-6, tracks)
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
			if val == "" {
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
			m.artistDetail = newMusicArtistDetail(m.width-sbWidth-1, m.height-8, m.artistDetail.artist, m.artistDetail.albums)
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
			}
			if newDate != "" {
				tags["date"] = newDate
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
			m.handleSidebarChange(n)
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
	}

	return m
}

func (m *model) refreshVideoList() {
	sbWidth := m.getSidebarWidth()
	m.videoList = newVideoList(m.width-sbWidth-1, m.height-6)
	m.syncFocus()
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
	case viewMusicLibrary, viewMusicRecent, viewMusicFilter:
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

	mainView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr)),
		footer,
	)

	if m.modal.Active() {
		m.modal.SetSize(m.width, m.height)
		overlay := m.modal.View()
		contentArea := lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr))
		contentLines := strings.Split(contentArea, "\n")
		dialogLines := strings.Split(overlay, "\n")

		// trim trailing empty lines from dialog
		for len(dialogLines) > 0 && dialogLines[len(dialogLines)-1] == "" {
			dialogLines = dialogLines[:len(dialogLines)-1]
		}

		dialogH := len(dialogLines)
		if dialogH > 0 {
			startRow := (len(contentLines) - dialogH) / 2
			if startRow < 0 {
				startRow = 0
			}

			// find dialog visual width for centering
			dialogW := 0
			for _, line := range dialogLines {
				if w := lipgloss.Width(line); w > dialogW {
					dialogW = w
				}
			}
			leftPad := (m.width - dialogW) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			padStr := strings.Repeat(" ", leftPad)

			for i := 0; i < dialogH && startRow+i < len(contentLines); i++ {
				contentLines[startRow+i] = padStr + dialogLines[i]
			}
		}

		mainView = lipgloss.JoinVertical(lipgloss.Left,
			header,
			strings.Join(contentLines, "\n"),
			footer,
		)
	}

	// Render filter edit modal
	if m.filterEdit != nil {
		m.filterEdit.SetSize(m.width, m.height)
		overlay := m.filterEdit.View()
		contentArea := lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr))
		contentLines := strings.Split(contentArea, "\n")
		dialogLines := strings.Split(overlay, "\n")

		for len(dialogLines) > 0 && dialogLines[len(dialogLines)-1] == "" {
			dialogLines = dialogLines[:len(dialogLines)-1]
		}

		dialogH := len(dialogLines)
		if dialogH > 0 {
			startRow := (len(contentLines) - dialogH) / 2
			if startRow < 0 {
				startRow = 0
			}

			dialogW := 0
			for _, line := range dialogLines {
				if w := lipgloss.Width(line); w > dialogW {
					dialogW = w
				}
			}
			leftPad := (m.width - dialogW) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			padStr := strings.Repeat(" ", leftPad)

			for i := 0; i < dialogH && startRow+i < len(contentLines); i++ {
				contentLines[startRow+i] = padStr + dialogLines[i]
			}
		}

		mainView = lipgloss.JoinVertical(lipgloss.Left,
			header,
			strings.Join(contentLines, "\n"),
			footer,
		)
	}

	// Render nested modals
	if m.filterCondEdit != nil {
		m.filterCondEdit.SetSize(m.width, m.height)
		overlay := m.filterCondEdit.View()
		contentArea := lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr))
		contentLines := strings.Split(contentArea, "\n")
		dialogLines := strings.Split(overlay, "\n")

		for len(dialogLines) > 0 && dialogLines[len(dialogLines)-1] == "" {
			dialogLines = dialogLines[:len(dialogLines)-1]
		}

		dialogH := len(dialogLines)
		if dialogH > 0 {
			startRow := (len(contentLines) - dialogH) / 2
			if startRow < 0 {
				startRow = 0
			}

			dialogW := 0
			for _, line := range dialogLines {
				if w := lipgloss.Width(line); w > dialogW {
					dialogW = w
				}
			}
			leftPad := (m.width - dialogW) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			padStr := strings.Repeat(" ", leftPad)

			for i := 0; i < dialogH && startRow+i < len(contentLines); i++ {
				contentLines[startRow+i] = padStr + dialogLines[i]
			}
		}

		mainView = lipgloss.JoinVertical(lipgloss.Left,
			header,
			strings.Join(contentLines, "\n"),
			footer,
		)
	}

	if m.sortFieldSelect != nil {
		m.sortFieldSelect.SetSize(m.width, m.height)
		overlay := m.sortFieldSelect.View()
		contentArea := lipgloss.JoinHorizontal(lipgloss.Top, sbView, mainStyle.Render(mainContentStr))
		contentLines := strings.Split(contentArea, "\n")
		dialogLines := strings.Split(overlay, "\n")

		for len(dialogLines) > 0 && dialogLines[len(dialogLines)-1] == "" {
			dialogLines = dialogLines[:len(dialogLines)-1]
		}

		dialogH := len(dialogLines)
		if dialogH > 0 {
			startRow := (len(contentLines) - dialogH) / 2
			if startRow < 0 {
				startRow = 0
			}

			dialogW := 0
			for _, line := range dialogLines {
				if w := lipgloss.Width(line); w > dialogW {
					dialogW = w
				}
			}
			leftPad := (m.width - dialogW) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			padStr := strings.Repeat(" ", leftPad)

			for i := 0; i < dialogH && startRow+i < len(contentLines); i++ {
				contentLines[startRow+i] = padStr + dialogLines[i]
			}
		}

		mainView = lipgloss.JoinVertical(lipgloss.Left,
			header,
			strings.Join(contentLines, "\n"),
			footer,
		)
	}

	return mainView + "\x1b[?25l"
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
		Width(m.width).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render(helpStr)
}
