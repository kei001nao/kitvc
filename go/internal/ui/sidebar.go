package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"kitvc/internal/db"
)

type nodeType int

const (
	nodeCategory nodeType = iota
	nodeArtist
	nodeAlbum
	nodeMusicPlaylist
	nodeVideoPlaylist
)

type node struct {
	label    string
	id       string
	typ      nodeType
	children []*node
	expanded bool
	level    int
}

type sidebar struct {
	nodes       []*node
	visibleRows []*node
	cursor      int
	width       int
	height      int
}

func newSidebar(width, height int) sidebar {
	s := sidebar{
		width:  width,
		height: height,
	}

	// 1. Music Library Node
	musicNode := &node{label: "Music Library", id: "music_library", typ: nodeCategory, expanded: false}
	
	artists, albums, err := db.GetMusicArtistsAndAlbums()
	if err == nil {
		for _, artist := range artists {
			artistNode := &node{label: artist, id: "artist:" + artist, typ: nodeArtist, level: 1}
			for _, album := range albums[artist] {
				albumNode := &node{
					label: album.Title,
					id:    fmt.Sprintf("album:%d", album.ID),
					typ:   nodeAlbum,
					level: 2,
				}
				artistNode.children = append(artistNode.children, albumNode)
			}
			musicNode.children = append(musicNode.children, artistNode)
		}
	}

	// 2. Music Playlists Node
	musicPlaylistNode := &node{label: "Music Playlists", id: "music_playlists", typ: nodeCategory, expanded: false}
	playlists, err := db.GetMusicPlaylists()
	if err == nil {
		for _, p := range playlists {
			musicPlaylistNode.children = append(musicPlaylistNode.children, &node{
				label: p.Name,
				id:    fmt.Sprintf("music_playlist:%d", p.ID),
				typ:   nodeMusicPlaylist,
				level: 1,
			})
		}
	}

	// 3. Video Playlists Node
	videoPlaylistNode := &node{label: "Video Playlists", id: "video_playlists", typ: nodeCategory, expanded: false}
	vPlaylists, err := db.GetVideoPlaylists()
	if err == nil {
		for _, p := range vPlaylists {
			videoPlaylistNode.children = append(videoPlaylistNode.children, &node{
				label: p.Name,
				id:    fmt.Sprintf("video_playlist:%d", p.ID),
				typ:   nodeVideoPlaylist,
				level: 1,
			})
		}
	}

	s.nodes = []*node{
		musicNode,
		musicPlaylistNode,
		{label: "Video Library", id: "video_library", typ: nodeCategory},
		videoPlaylistNode,
	}

	s.rebuildVisible()
	return s
}

func (s *sidebar) Refresh() {
	// Rebuild nodes tree (same logic as newSidebar)
	s.nodes = []*node{}

	// 1. Music Library Node
	musicNode := &node{label: "Music Library", id: "music_library", typ: nodeCategory, expanded: false}
	artists, albums, err := db.GetMusicArtistsAndAlbums()
	if err == nil {
		for _, artist := range artists {
			artistNode := &node{label: artist, id: "artist:" + artist, typ: nodeArtist, level: 1}
			for _, album := range albums[artist] {
				albumNode := &node{
					label: album.Title,
					id:    fmt.Sprintf("album:%d", album.ID),
					typ:   nodeAlbum,
					level: 2,
				}
				artistNode.children = append(artistNode.children, albumNode)
			}
			musicNode.children = append(musicNode.children, artistNode)
		}
	}

	// 2. Music Playlists Node
	musicPlaylistNode := &node{label: "Music Playlists", id: "music_playlists", typ: nodeCategory, expanded: false}
	playlists, err := db.GetMusicPlaylists()
	if err == nil {
		for _, p := range playlists {
			musicPlaylistNode.children = append(musicPlaylistNode.children, &node{
				label: p.Name,
				id:    fmt.Sprintf("music_playlist:%d", p.ID),
				typ:   nodeMusicPlaylist,
				level: 1,
			})
		}
	}

	// 3. Video Playlists Node
	videoPlaylistNode := &node{label: "Video Playlists", id: "video_playlists", typ: nodeCategory, expanded: false}
	vPlaylists, err := db.GetVideoPlaylists()
	if err == nil {
		for _, p := range vPlaylists {
			videoPlaylistNode.children = append(videoPlaylistNode.children, &node{
				label: p.Name,
				id:    fmt.Sprintf("video_playlist:%d", p.ID),
				typ:   nodeVideoPlaylist,
				level: 1,
			})
		}
	}

	s.nodes = []*node{
		musicNode,
		musicPlaylistNode,
		{label: "Video Library", id: "video_library", typ: nodeCategory},
		videoPlaylistNode,
	}

	s.rebuildVisible()
}

func (s *sidebar) rebuildVisible() {
	s.visibleRows = []*node{}
	for _, n := range s.nodes {
		s.addVisible(n)
	}
}

func (s *sidebar) addVisible(n *node) {
	s.visibleRows = append(s.visibleRows, n)
	if n.expanded {
		for _, child := range n.children {
			s.addVisible(child)
		}
	}
}

func (s sidebar) SelectedNode() *node {
	if s.cursor >= 0 && s.cursor < len(s.visibleRows) {
		return s.visibleRows[s.cursor]
	}
	return nil
}

func (s sidebar) Update(msg tea.Msg) (sidebar, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down":
			if s.cursor < len(s.visibleRows)-1 {
				s.cursor++
			}
		case "right":
			if n := s.SelectedNode(); n != nil && len(n.children) > 0 {
				if !n.expanded {
					n.expanded = true
					s.rebuildVisible()
				}
			}
		case "left":
			if n := s.SelectedNode(); n != nil {
				if n.expanded {
					n.expanded = false
					s.rebuildVisible()
				} else if n.level > 0 {
					// TODO: jump to parent?
				}
			}
		}
	}
	return s, nil
}

func (s *sidebar) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s sidebar) View(focused bool) string {
	var b strings.Builder

	for i, n := range s.visibleRows {
		// Indentation
		indent := strings.Repeat("  ", n.level)
		
		// Prefix (expanded/collapsed)
		prefix := "  "
		if len(n.children) > 0 {
			if n.expanded {
				prefix = "▼ "
			} else {
				prefix = "▶ "
			}
		}

		style := lipgloss.NewStyle().PaddingLeft(1).UnsetBackground()
		if i == s.cursor {
			// Always red foreground for selected node, no background
			style = style.Foreground(lipgloss.Color("1"))
		}

		line := fmt.Sprintf("%s%s%s", indent, prefix, n.label)
		// Truncate line if it's longer than sidebar width - padding - border
		availWidth := s.width - 2
		if availWidth > 0 && len(line) > availWidth {
			line = line[:availWidth]
		}
		
		b.WriteString(style.Render(line) + "\n")
	}

	sidebarStyle := lipgloss.NewStyle().
		Width(s.width).
		Height(s.height).
		UnsetBackground()
	
	return sidebarStyle.Render(b.String())
}
