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
	nodeRecentlyAdded
	nodeMusicFilter
	nodeVideoFilter
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
	cover       coverArt
}

func newSidebar(width, height int) sidebar {
	s := sidebar{
		width:  width,
		height: height,
	}

	// 1. Music Library Node
	musicNode := &node{label: "Music Library", id: "music_library", typ: nodeCategory, expanded: true}
	
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

	// 2. Recently Added Node
	recentNode := &node{label: "Recently Added", id: "music_recent", typ: nodeRecentlyAdded, level: 1}
	musicNode.children = append(musicNode.children, recentNode)

	// 3. Views Node (Music Filters)
	viewsNode := &node{label: "Views", id: "music_views", typ: nodeCategory, expanded: false, level: 1}
	filters, err := db.GetMusicFilters()
	if err == nil {
		for _, f := range filters {
			viewsNode.children = append(viewsNode.children, &node{
				label: f.Name,
				id:    fmt.Sprintf("music_filter:%d", f.ID),
				typ:   nodeMusicFilter,
				level: 2,
			})
		}
	}
	musicNode.children = append(musicNode.children, viewsNode)

	// 4. Music Playlists Node
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

	// 5. Video Playlists Node
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

	// 6. Video Library Node with Views
	videoLibraryNode := &node{label: "Video Library", id: "video_library", typ: nodeCategory, expanded: true, level: 0}
	videoViewsNode := &node{label: "Views", id: "video_views", typ: nodeCategory, expanded: false, level: 1}
	videoFilters, err := db.GetVideoFilters()
	if err == nil {
		for _, f := range videoFilters {
			videoViewsNode.children = append(videoViewsNode.children, &node{
				label: f.Name,
				id:    fmt.Sprintf("video_filter:%d", f.ID),
				typ:   nodeVideoFilter,
				level: 2,
			})
		}
	}
	videoLibraryNode.children = append(videoLibraryNode.children, videoViewsNode)

	s.nodes = []*node{
		musicNode,
		musicPlaylistNode,
		videoLibraryNode,
		videoPlaylistNode,
	}

	s.rebuildVisible()
	return s
}

func (s *sidebar) Refresh() {
	// Rebuild nodes tree (same logic as newSidebar)
	s.nodes = []*node{}

	// 1. Music Library Node
	musicNode := &node{label: "Music Library", id: "music_library", typ: nodeCategory, expanded: true}
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

	// 2. Recently Added Node
	recentNode := &node{label: "Recently Added", id: "music_recent", typ: nodeRecentlyAdded, level: 1}
	musicNode.children = append(musicNode.children, recentNode)

	// 3. Views Node (Music Filters)
	viewsNode := &node{label: "Views", id: "music_views", typ: nodeCategory, expanded: false, level: 1}
	filters, err := db.GetMusicFilters()
	if err == nil {
		for _, f := range filters {
			viewsNode.children = append(viewsNode.children, &node{
				label: f.Name,
				id:    fmt.Sprintf("music_filter:%d", f.ID),
				typ:   nodeMusicFilter,
				level: 2,
			})
		}
	}
	musicNode.children = append(musicNode.children, viewsNode)

	// 4. Music Playlists Node
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

	// 5. Video Playlists Node
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

	// 6. Video Library Node with Views
	videoLibraryNode := &node{label: "Video Library", id: "video_library", typ: nodeCategory, expanded: true, level: 0}
	videoViewsNode := &node{label: "Views", id: "video_views", typ: nodeCategory, expanded: false, level: 1}
	videoFilters, err := db.GetVideoFilters()
	if err == nil {
		for _, f := range videoFilters {
			videoViewsNode.children = append(videoViewsNode.children, &node{
				label: f.Name,
				id:    fmt.Sprintf("video_filter:%d", f.ID),
				typ:   nodeVideoFilter,
				level: 2,
			})
		}
	}
	videoLibraryNode.children = append(videoLibraryNode.children, videoViewsNode)

	s.nodes = []*node{
		musicNode,
		musicPlaylistNode,
		videoLibraryNode,
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

func (s *sidebar) SelectByID(id string) bool {
	for i, n := range s.visibleRows {
		if n.id == id {
			s.cursor = i
			return true
		}
	}
	return false
}

func (s *sidebar) ExpandByID(id string) bool {
	var expand func(nodes []*node) bool
	expand = func(nodes []*node) bool {
		for _, n := range nodes {
			if n.id == id {
				n.expanded = true
				s.rebuildVisible()
				return true
			}
			if len(n.children) > 0 {
				if expand(n.children) {
					return true
				}
			}
		}
		return false
	}
	return expand(s.nodes)
}

func (s sidebar) SelectedNode() *node {
	if s.cursor >= 0 && s.cursor < len(s.visibleRows) {
		return s.visibleRows[s.cursor]
	}
	return nil
}

func (s sidebar) NumVisible() int {
	return len(s.visibleRows)
}

func (s *sidebar) SetCoverPath(path string, cols, maxRows int) bool {
	return s.cover.load(path, cols, maxRows)
}

func (s *sidebar) CoverPath() string {
	if !s.cover.cached {
		return ""
	}
	return s.cover.path
}

func (s *sidebar) CoverRow() int {
	return 3 + s.height - s.cover.rows
}

func (s *sidebar) CoverCols() int {
	return s.cover.cols
}

func (s *sidebar) CoverRows() int {
	return s.cover.rows
}

func (s *sidebar) HasCover() bool {
	return s.cover.cached && s.cover.art != ""
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
	var tree strings.Builder
	for i, n := range s.visibleRows {
		indent := strings.Repeat("  ", n.level)
		
		prefix := ""
		if len(n.children) > 0 {
			if n.expanded {
				prefix = "▼ "
			} else {
				prefix = "▶ "
			}
		}

		style := lipgloss.NewStyle().PaddingLeft(1).UnsetBackground()
		if i == s.cursor {
			style = style.Foreground(lipgloss.Color("1"))
		}

		line := fmt.Sprintf("%s%s%s", indent, prefix, n.label)
		availWidth := s.width - 2
		if availWidth > 0 && len(line) > availWidth {
			line = line[:availWidth]
		}
		
		tree.WriteString(style.Render(line))
		if i < len(s.visibleRows)-1 {
			tree.WriteString("\n")
		}
	}
	treeStr := tree.String()
	treeLines := len(s.visibleRows)

	var coverStr string
	coverLines := 0
	if s.cover.cached && s.cover.art != "" {
		coverStr = s.cover.art
		coverLines = s.cover.rows
	}

	// Push cover to bottom of sidebar area
	padding := s.height - treeLines - coverLines
	if padding < 0 {
		padding = 0
	}

	var content strings.Builder
	content.WriteString(treeStr)
	for i := 0; i < padding; i++ {
		content.WriteString("\n")
	}
	if coverStr != "" {
		content.WriteString(coverStr)
	}

	sidebarStyle := lipgloss.NewStyle().
		Width(s.width).
		Height(s.height).
		UnsetBackground()
	
	return sidebarStyle.Render(content.String())
}
