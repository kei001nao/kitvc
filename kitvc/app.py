import asyncio
import logging
import os
from pathlib import Path

# Explicitly setup logging with FileHandler to ensure it works
log_path = Path("kitvc.log").absolute()
notifications_path = Path("notifications.txt").absolute()

root_logger = logging.getLogger()
root_logger.setLevel(logging.INFO)
# Clear existing handlers
for h in root_logger.handlers[:]:
    root_logger.removeHandler(h)

fh = logging.FileHandler(log_path)
fh.setFormatter(logging.Formatter('%(asctime)s - %(levelname)s - %(message)s'))
root_logger.addHandler(fh)

# Suppress noisy logs from MusicBrainz library
logging.getLogger("musicbrainzngs").setLevel(logging.WARNING)

logger = logging.getLogger(__name__)

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.widget import Widget
from textual.widgets import Label, Tree, Input, Button, DataTable
from textual.screen import Screen
from textual import work

from .config import load_config, load_theme, THEME_PATH
from .database import init_db, save_playback_position, get_playback_position, get_connection, get_playlists, create_playlist
from .library import MusicLibrary, VideoLibrary
from .player import MpvInfo, MusicPlayer, VideoPlayer
from .widgets.header import Header
from .widgets.modals import QuitModal, ConfirmModal, FileSelectModal
from .widgets.playback import PlaybackControl
from .screens.music import MusicLibraryScreen, MusicArtistScreen, MusicPlaylistScreen, QueueScreen, MusicRecentScreen, MusicFilterScreen
from .screens.video import VideoLibraryScreen, VideoCategoryScreen, VideoPlaylistScreen, VideoHealthScreen, VideoContinueScreen, VideoRecentScreen
from textual_image.widget import Image

LOGO = """█▄▀ █ ▀█▀ █ █ █▀
█ █ █  █  ╚▄▀ █▄""".strip("\n")

class Sidebar(Widget):
    BINDINGS = [
        Binding("n", "create_view", "New View"),
        Binding("e", "edit_view", "Edit Selected View"),
        Binding("d", "delete_view", "Delete Selected View"),
    ]
    DEFAULT_CSS = """
    Sidebar {
        width: 44;
        height: 100%;
        padding: 1 0;
        background: $surface;
    }
    Sidebar:focus-within {
        border-right: solid $accent;
    }
    Sidebar #app-title {
        height: auto;
        padding: 0 1;
        text-style: bold;
        color: $primary;
        margin-top: 1;
    }
    Sidebar Tree {
        background: transparent;
        padding: 0 1;
        height: 1fr;
    }
    #sidebar-cover-container {
        width: 100%;
        height: 18;
        margin: 1 0;
        background: $surface;
        border: solid $primary;
        display: none;
        align: center middle;
    }
    #sidebar-cover-container.-has-image {
        display: block;
    }
    #sidebar-cover-container Image {
        width: 100%;
        height: 100%;
    }
    """

    def compose(self) -> ComposeResult:
        tree = Tree("Root", id="nav-tree")
        tree.show_root = False
        
        music = tree.root.add("Music", data="music_root", expand=True)
        music.add_leaf("Queue", data="music_queue")
        music.add("Library", data="music_library")
        music.add("Views", data="music_filters")
        music.add_leaf("Recently Added", data="music_recent")
        music.add_leaf("PlayLists", data="music_playlists")
        
        video = tree.root.add("Video", data="video_root", expand=True)
        video.add("Library", data="video_library")
        video.add_leaf("Continue Watching", data="video_continue")
        video.add_leaf("Recently Added", data="video_recent")
        video.add_leaf("Health Check", data="video_health")
        video.add("Views", data="video_filters")
        video.add_leaf("PlayLists", data="video_playlists")
        
        yield tree
        with Vertical(id="sidebar-cover-container"):
            # Placeholder, will be populated dynamically
            pass
        yield Label(LOGO, id="app-title")

    def on_mount(self) -> None:
        self.refresh_tree()

    @work(thread=True)
    def refresh_tree(self) -> None:
        try:
            with get_connection() as conn:
                artists = [row["artist"] for row in conn.execute("SELECT DISTINCT artist FROM music_tracks ORDER BY artist COLLATE NOCASE").fetchall()]
                categories = [row["category"] for row in conn.execute("SELECT DISTINCT category FROM video_files ORDER BY category").fetchall()]
                v_filters = [dict(row) for row in conn.execute("SELECT id, name FROM video_filters ORDER BY name").fetchall()]
                m_filters = [dict(row) for row in conn.execute("SELECT id, name FROM music_filters ORDER BY name").fetchall()]
            self.app.call_from_thread(self._populate_tree, artists, categories, v_filters, m_filters)
        except Exception as e:
            logger.error(f"Sidebar.refresh_tree failed: {e}")

    def _populate_tree(self, artists: list[str], categories: list[str], v_filters: list[dict], m_filters: list[dict]) -> None:
        tree = self.query_one("#nav-tree", Tree)
        
        def find_node(root, data_id):
            if root.data == data_id: return root
            for child in root.children:
                res = find_node(child, data_id)
                if res: return res
            return None

        music_lib = find_node(tree.root, "music_library")
        if music_lib:
            music_lib.remove_children()
            for name in artists:
                music_lib.add_leaf(name, data={"type": "artist", "name": name})
        
        music_filters = find_node(tree.root, "music_filters")
        if music_filters:
            music_filters.remove_children()
            for f in m_filters:
                music_filters.add_leaf(f["name"], data={"type": "music_filter", "id": f["id"], "name": f["name"]})

        video_lib = find_node(tree.root, "video_library")
        if video_lib:
            video_lib.remove_children()
            for cat in categories:
                video_lib.add_leaf(cat or "(unknown)", data={"type": "video_category", "name": cat})

        video_filters = find_node(tree.root, "video_filters")
        if video_filters:
            video_filters.remove_children()
            for f in v_filters:
                video_filters.add_leaf(f["name"], data={"type": "video_filter", "id": f["id"], "name": f["name"]})

    def select_node_by_data(self, data_id: any) -> None:
        tree = self.query_one("#nav-tree", Tree)
        def find_and_select(root):
            if root.data == data_id:
                tree.select_node(root)
                return True
            for child in root.children:
                if find_and_select(child): return True
            return False
        find_and_select(tree.root)

    def action_edit_view(self) -> None:
        tree = self.query_one("#nav-tree", Tree)
        node = tree.cursor_node
        if not node:
            return
        
        data = node.data
        if isinstance(data, dict) and data.get("type") == "video_filter":
            from .database import get_connection
            with get_connection() as conn:
                f = conn.execute("SELECT * FROM video_filters WHERE id = ?", (data["id"],)).fetchone()
            if f:
                from .widgets.modals import VideoFilterEditModal
                self.app.push_screen(VideoFilterEditModal(dict(f)), callback=self._after_view_edited)
        elif isinstance(data, dict) and data.get("type") == "music_filter":
            from .database import get_connection
            with get_connection() as conn:
                f = conn.execute("SELECT * FROM music_filters WHERE id = ?", (data["id"],)).fetchone()
            if f:
                from .widgets.modals import MusicFilterEditModal
                self.app.push_screen(MusicFilterEditModal(dict(f)), callback=self._after_view_edited)

    def action_create_view(self) -> None:
        tree = self.query_one("#nav-tree", Tree)
        node = tree.cursor_node
        if not node:
            return
        
        is_video_context = False
        is_music_context = False
        curr = node
        while curr:
            if curr.data in ("video_root", "video_library", "video_filters", "video_playlists"):
                is_video_context = True
                break
            if curr.data in ("music_root", "music_library", "music_filters", "music_playlists"):
                is_music_context = True
                break
            if isinstance(curr.data, dict):
                if curr.data.get("type") in ("video_category", "video_filter"):
                    is_video_context = True
                    break
                if curr.data.get("type") in ("artist", "music_filter"):
                    is_music_context = True
                    break
            curr = curr.parent

        if is_video_context:
            from .widgets.modals import VideoFilterEditModal
            self.app.push_screen(VideoFilterEditModal(), callback=self._after_view_edited)
        elif is_music_context:
            from .widgets.modals import MusicFilterEditModal
            self.app.push_screen(MusicFilterEditModal(), callback=self._after_view_edited)

    def action_delete_view(self) -> None:
        tree = self.query_one("#nav-tree", Tree)
        node = tree.cursor_node
        if not node:
            return

        data = node.data
        if isinstance(data, dict) and data.get("type") == "video_filter":
            filter_id = data["id"]
            filter_name = data["name"]
            from .widgets.modals import ConfirmModal
            def check_confirm(confirmed: bool) -> None:
                if confirmed:
                    from .database import delete_video_filter
                    delete_video_filter(filter_id)
                    self.app.notify(f"View '{filter_name}' deleted")
                    self.refresh_tree()
                    if self.app._current_screen_name == "video_filter" and \
                       self.app._current_screen_data.get("id") == filter_id:
                        self.app.switch_screen("video")
            self.app.push_screen(ConfirmModal(f"Delete view '{filter_name}'?"), callback=check_confirm)
        elif isinstance(data, dict) and data.get("type") == "music_filter":
            filter_id = data["id"]
            filter_name = data["name"]
            from .widgets.modals import ConfirmModal
            def check_confirm(confirmed: bool) -> None:
                if confirmed:
                    from .database import delete_music_filter
                    delete_music_filter(filter_id)
                    self.app.notify(f"View '{filter_name}' deleted")
                    self.refresh_tree()
                    if self.app._current_screen_name == "music_filter" and \
                       self.app._current_screen_data.get("id") == filter_id:
                        self.app.switch_screen("music")
            self.app.push_screen(ConfirmModal(f"Delete view '{filter_name}'?"), callback=check_confirm)

    def _after_view_edited(self, result: bool) -> None:
        if result:
            self.refresh_tree()
            if self.app._current_screen_name == "video_filter":
                data = self.app._current_screen_data
                self.app._current_screen_data = None
                self.app.switch_screen_with_data("video_filter", data, focus_right=True)
            elif self.app._current_screen_name == "music_filter":
                data = self.app._current_screen_data
                self.app._current_screen_data = None
                self.app.switch_screen_with_data("music_filter", data, focus_right=True)
    async def on_tree_node_selected(self, event: Tree.NodeSelected) -> None:
        await self._handle_node_change(event.node)

    async def on_tree_node_highlighted(self, event: Tree.NodeHighlighted) -> None:
        if event.node:
            await self._handle_node_change(event.node)

    async def _handle_node_change(self, node) -> None:
        data = node.data
        if data == "music_root":
            self.app.switch_screen("music_queue")
        elif data == "video_root":
            self.app.switch_screen("video")
        elif data == "music_library":
            self.app.switch_screen("music")
        elif data == "music_filters":
            # No default action for the 'Views' parent node itself
            pass
        elif data == "music_recent":
            self.app.switch_screen("music_recent")
        elif data == "video_library":
            self.app.switch_screen("video")
        elif data == "video_continue":
            self.app.switch_screen("video_continue")
        elif data == "video_recent":
            self.app.switch_screen("video_recent")
        elif data == "video_health":
            self.app.switch_screen("video_health")
        elif data == "music_queue":
            self.app.switch_screen("music_queue")
        elif data == "music_playlists":
            self.app.switch_screen("music_playlists")
        elif data == "video_playlists":
            self.app.switch_screen("video_playlists")
        elif isinstance(data, dict):
            if data.get("type") == "artist":
                self.app.switch_screen_with_data("artist", data["name"])
            elif data.get("type") == "music_filter":
                self.app.switch_screen_with_data("music_filter", data)
            elif data.get("type") == "video_category":
                self.app.switch_screen_with_data("video_category", data["name"])
            elif data.get("type") == "video_filter":
                self.app.switch_screen_with_data("video_filter", data)

class PlaylistCreateModal(Screen):
    DEFAULT_CSS = """
    PlaylistCreateModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #create-container {
        width: 40;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #create-container Label { margin-bottom: 0; }
    #create-container Input { margin-bottom: 1; border: none; background: $accent 10%; color: $text; }
    #create-container .playlist-help { width: 100%; text-align: center; background: $primary; color: $text; }
    """
    def __init__(self, is_video=False, **kwargs):
        super().__init__(**kwargs)
        self.is_video = is_video

    def compose(self) -> ComposeResult:
        with Vertical(id="create-container"):
            yield Label("New Playlist Name:")
            yield Input(placeholder="Enter name...", id="playlist-name")
            yield Label("Enter: Confirmed   Tab: Cancel", classes="playlist-help")

    def on_mount(self) -> None:
        self.query_one(Input).focus()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        name = event.value.strip()
        if name:
            create_playlist(name, is_video=self.is_video)
            self.dismiss(True)

    def on_key(self, event) -> None:
        if event.key == "tab" or event.key == "escape":
            self.dismiss(False)
            event.stop()

class PlaylistQuickAddModal(Screen):
    DEFAULT_CSS = """
    PlaylistQuickAddModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #quick-add-container {
        width: 40;
        height: auto;
        max-height: 15;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #quick-add-container DataTable { height: auto; max-height: 10; margin: 1 0 0 0; background: transparent; border: none; }
    #quick-add-container .playlist-help { width: 100%; text-align: center; background: $primary; color: $text; margin: 0; }
    """
    def __init__(self, track_paths: list[str], is_video=False, **kwargs):
        super().__init__(**kwargs)
        self.track_paths = track_paths
        self.is_video = is_video
        self._items = []

    def compose(self) -> ComposeResult:
        with Vertical(id="quick-add-container"):
            yield Label(f"Add {len(self.track_paths)} items to:")
            yield DataTable(id="playlist-list", cursor_type="row")
            yield Label("Enter: Confirmed   Tab: Cancel", classes="playlist-help")

    def on_mount(self) -> None:
        table = self.query_one("#playlist-list", DataTable)
        table.add_column("Playlist", key="playlist")
        
        self._items = []
        self._items.append({"name": "[New Playlist...]", "is_new": True})
        for p in get_playlists(is_video=self.is_video):
            self._items.append({"name": p["name"], "id": p["id"], "is_new": False})

        for i, item in enumerate(self._items):
            table.add_row(item["name"], key=str(i))

        table.index = getattr(self.app, "_last_playlist_add_idx", 0)
        if self._items:
            table.move_cursor(row=min(table.index, len(self._items)-1))
        table.focus()

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        try:
            idx = int(str(event.row_key.value))
            self.app._last_playlist_add_idx = idx
            item = self._items[idx]
            
            if item.get("is_new"):
                self.app.show_playlist_create_dialog(callback=self._after_new_playlist, is_video=self.is_video)
            else:
                self._add_to(item["id"], item["name"])
        except (ValueError, IndexError):
            pass

    def _after_new_playlist(self, result: bool) -> None:
        if result:
            table = "video_playlists" if self.is_video else "music_playlists"
            with get_connection() as conn:
                p = conn.execute(f"SELECT * FROM {table} ORDER BY id DESC LIMIT 1").fetchone()
            if p:
                self._add_to(p["id"], p["name"])
        else:
            pass

    def _add_to(self, playlist_id: int, name: str) -> None:
        from .database import add_to_playlist
        for path in self.track_paths:
            add_to_playlist(playlist_id, path, is_video=self.is_video)
        self.app.notify(f"Added to '{name}'")
        self.app.export_playlist_to_m3u(playlist_id, is_video=self.is_video)
        self.dismiss(True)

    def on_key(self, event) -> None:
        if event.key == "tab":
            self.dismiss(False)
            event.stop()
        elif event.key == "escape":
            self.dismiss(False)
            event.stop()
class ContentArea(Widget):
    DEFAULT_CSS = "ContentArea { width: 1fr; height: 100%; }"
    def compose(self) -> ComposeResult:
        yield QueueScreen()

class KitvcApp(App):
    def notify(self, message: str, *, title: str = "", severity: str = "information", timeout: float = 3) -> None:
        """Override notify to also write to a text file for accessibility/debugging."""
        super().notify(message, title=title, severity=severity, timeout=timeout)
        try:
            with open(notifications_path, "a", encoding="utf-8") as f:
                f.write(f"[{severity.upper()}] {title + ': ' if title else ''}{message}\n")
        except Exception:
            pass

    BINDINGS = [
        Binding("ctrl+q", "quit", "Quit"),
        Binding("p", "toggle_pause", "Play/Pause"),
        Binding("escape", "quit", "Quit"),
        Binding("backspace", "go_back", "Back"),
        Binding("/", "search", "Search"),
        Binding("1", "switch_to_queue", "Music Queue", priority=True),
        Binding("2", "switch_to_playlists", "Music PL", priority=True),
        Binding("3", "switch_to_video_playlists", "Video PL", priority=True),
        Binding("ctrl+s", "scan", "Scan Lib"),
        Binding("ctrl+shift+s", "video_targeted_scan", "Video Scan", show=False),
        Binding("ctrl+o", "import_playlist", "Import M3U"),
        Binding("l", "seek(5)", "Seek +5s", show=False),
        Binding("h", "seek(-5)", "Seek -5s", show=False),
        Binding("L", "seek(30)", "Seek +30s", show=False),
        Binding("H", "seek(-30)", "Seek -30s", show=False),
        Binding("9", "volume_down", "Vol -10%", show=False),
        Binding("0", "volume_up", "Vol +10%", show=False),
        Binding("ctrl+right_square_bracket", "resize_sidebar(2)", "Resize +", show=False),
        Binding("ctrl+left_square_bracket", "resize_sidebar(-2)", "Resize -", show=False),
        Binding("ctrl+right", "resize_sidebar(2)", "Resize +", show=False),
        Binding("ctrl+left", "resize_sidebar(-2)", "Resize -", show=False),
    ]

    def _generate_css(self, primary: str, accent: str, bg: str, surface: str, sidebar_width: int = 44) -> str:
        return f"""
        $primary: {primary};
        $accent: {accent};
        $background: {bg};
        $surface: {surface};

        App {{ background: $background; padding: 1 2; }}
        #main-layout {{ layout: horizontal; height: 1fr; }}
        Sidebar {{ width: {sidebar_width}; border-right: solid $primary; background: $background; }}
        Sidebar #app-title {{ height: auto; color: $primary; text-style: bold; margin: 0 0 1 1; }}
        Tree {{ background: transparent; scrollbar-color: $primary; scrollbar-size: 1 1; }}
        DataTable {{ scrollbar-color: $primary; scrollbar-size: 1 1; }}
        
        /* DataTable Header Stabilization */
        DataTable > .datatable--header {{
            background: $primary 20%;
            color: $text;
            text-style: bold;
        }}
        DataTable:focus > .datatable--header {{
            background: $primary 40%;
        }}

        Tree:focus > .tree--cursor {{ background: $accent; text-style: bold; }}
        .tree--guides-selected {{ color: $accent; }}
        
        /* Cursor and Selection Styling */
        DataTable > .datatable--cursor {{ background: $accent 20%; }}
        DataTable:focus > .datatable--cursor {{ background: $accent; text-style: bold; }}
        
        #footer {{ height: 1; background: $background; }}
        """

    def __init__(self):
        logger.info("Initializing KitvcApp")
        try:
            init_db()  # Initialize DB before anything else
            self.config = load_config()
            theme = load_theme()
            colors = theme.get("colors", {})
            primary = colors.get("primary", "deepskyblue")
            accent = colors.get("accent", "magenta")
            bg = colors.get("background", "")
            surface = colors.get("surface", "")
            if not bg: bg = "$background"
            if not surface: surface = "$surface"
            
            sidebar_width = self.config.get("ui", {}).get("sidebar_width", 44)
            self.CSS = self._generate_css(primary, accent, bg, surface, sidebar_width)
            
            super().__init__()
            self.mpv_info = MpvInfo()
            self.music_player = MusicPlayer(self.config)
            self.video_player = VideoPlayer(self.config)
            self.music_lib = MusicLibrary(self.config["music"]["directories"])
            self.video_lib = VideoLibrary(self.config["video"]["directories"])
            self.screen_history = []
            self._last_playlist_add_idx = 0
            self._last_video_category = ""
            self._current_media = None
            self._current_screen_name = None
            self._current_screen_data = None
            self._theme_mtime = 0.0
            self._last_sidebar_path = None
            if THEME_PATH.exists():
                self._theme_mtime = THEME_PATH.stat().st_mtime
        except Exception as e:
            logger.exception("Error during KitvcApp.__init__")
            raise

    def action_resize_sidebar(self, delta: int) -> None:
        try:
            sidebar = self.query_one("Sidebar")
        except Exception:
            return

        if "ui" not in self.config:
            self.config["ui"] = {}
        
        current_width = self.config["ui"].get("sidebar_width", 44)
        new_width = max(10, min(100, current_width + delta))
        
        if new_width != current_width:
            self.config["ui"]["sidebar_width"] = new_width
            sidebar.styles.width = new_width
            
            from .config import save_config
            save_config(self.config)
            # No need to refresh_css if we update styles.width directly
        else:
            self.notify(f"Sidebar width at limit: {new_width}", severity="warning")

    def _apply_theme(self) -> None:
        pass

    def compose(self) -> ComposeResult:
        yield Header(id="header")
        with Horizontal(id="main-layout"):
            yield Sidebar()
            yield ContentArea(id="content")
        yield Label("  ctrl+q: Quit | p: Play/Pause | ctrl+s: Scan | 1-3: Nav | h/l: Seek | ctrl+o: Import M3U", id="footer")

    async def on_mount(self) -> None:
        logger.info("Mounting KitvcApp")
        try:
            self._apply_theme()
            await self.mpv_info.start()
            await self.music_player.start()
            self.set_interval(0.5, self._poll_player)
            self.scan_libraries(asyncio.get_running_loop())
            watch_interval = self.config.get("theme", {}).get("watch_interval", 2)
            if watch_interval > 0:
                self.set_interval(watch_interval, self._watch_theme)
        except Exception as e:
            logger.exception("Error during KitvcApp.on_mount")
            self.notify(f"Startup error: {e}", severity="error")

    def update_sidebar_cover(self, path: str | None) -> None:
        """Update the cover art displayed in the sidebar by recreating the widget."""
        # Optimization: only update if the path has changed
        if hasattr(self, "_last_sidebar_path") and self._last_sidebar_path == path:
            return
        self._last_sidebar_path = path

        try:
            container = self.query_one("#sidebar-cover-container", Vertical)
            
            # Remove old images
            for child in list(container.children):
                child.remove()
            
            if path and Path(path).exists():
                new_img = Image(path)
                container.mount(new_img)
                container.set_class(True, "-has-image")
                logger.info(f"Sidebar cover re-mounted: {path}")
            else:
                container.set_class(False, "-has-image")
        except Exception as e:
            logger.debug(f"Failed to update sidebar cover: {e}")

    async def _poll_player(self) -> None:
        header = self.query_one("#header", Header)
        active_player = None
        if self.video_player.mpv and self.video_player.get_current_track():
            active_player = self.video_player
        elif self.music_player.get_current_track():
            active_player = self.music_player
            
        if not active_player:
            header.clear()
            return
            
        pos = await active_player.get_position()
        dur = await active_player.get_duration()
        paused = await active_player.get_property("pause")
        active_player._paused = (paused == True)
        
        track = active_player.get_current_track()
        if pos is not None and dur is not None and track:
            # 1. Ensure we have the latest metadata (including cover_path) from DB
            with get_connection() as conn:
                if not track.get("is_video"):
                    row = conn.execute("""
                        SELECT t.*, a.cover_path as album_cover, a.release_date, a.mbid
                        FROM music_tracks t
                        LEFT JOIN music_albums a ON t.album_id = a.id
                        WHERE t.path = ?
                    """, (track["path"],)).fetchone()
                    if row:
                        db_data = dict(row)
                        # Always prefer album cover if available
                        if db_data.get("album_cover"):
                            track["cover_path"] = db_data["album_cover"]
                        elif db_data.get("cover_path"):
                             track["cover_path"] = db_data["cover_path"]
                             
                        if db_data.get("mbid"): track["mbid"] = db_data["mbid"]
                else:
                    row = conn.execute("""
                        SELECT local_still_path, local_poster_path, local_series_poster_path, 
                               still_path, poster_path, series_poster_path, thumbnail_path
                        FROM video_files WHERE path = ?
                    """, (track["path"],)).fetchone()
                    if row:
                        db_data = dict(row)
                        # Priority: local_still > local_poster > local_series > still > poster > series > thumb
                        track["cover_path"] = db_data.get("local_still_path") or \
                                             db_data.get("local_poster_path") or \
                                             db_data.get("local_series_poster_path") or \
                                             db_data.get("still_path") or \
                                             db_data.get("poster_path") or \
                                             db_data.get("series_poster_path") or \
                                             db_data.get("thumbnail_path")

            # 2. Update sidebar cover
            current_cover = track.get("cover_path")
            self.update_sidebar_cover(current_cover)

            self._current_media = track
            title = track.get("title") or track.get("filename")
            artist = track.get("artist") or track.get("series") or ""
            volume = active_player.volume
            queue_pos = f"{active_player._current_idx + 1}/{len(active_player._queue)}"
            header.update_info(title, artist, pos, dur, volume, queue_pos)
            if getattr(self, "_last_save_time", 0) < asyncio.get_event_loop().time() - 5:
                self._last_save_time = asyncio.get_event_loop().time()
                save_playback_position(track["path"], pos, is_video=track.get("is_video", False))
        else:
            header.clear()

    async def action_seek(self, seconds: int) -> None:
        if self.video_player.mpv and self.video_player.get_current_track():
            pos = await self.video_player.get_position()
            if pos is not None: await self.video_player.seek(pos + seconds)
        elif self.music_player.get_current_track():
            pos = await self.music_player.get_position()
            if pos is not None: await self.music_player.seek(pos + seconds)

    async def action_volume_up(self) -> None:
        new_vol = min(100, (self.video_player.volume if self.video_player.mpv else self.music_player.volume) + 10)
        await self.music_player.set_volume(new_vol)
        await self.video_player.set_volume(new_vol)
        if "player" not in self.config: self.config["player"] = {}
        self.config["player"]["volume"] = new_vol
        from .config import save_config
        save_config(self.config)
        await self._poll_player()

    async def action_volume_down(self) -> None:
        new_vol = max(0, (self.video_player.volume if self.video_player.mpv else self.music_player.volume) - 10)
        await self.music_player.set_volume(new_vol)
        await self.video_player.set_volume(new_vol)
        if "player" not in self.config: self.config["player"] = {}
        self.config["player"]["volume"] = new_vol
        from .config import save_config
        save_config(self.config)
        await self._poll_player()

    async def action_scan(self) -> None:
        self.notify("Scanning libraries...")
        self.scan_libraries(asyncio.get_running_loop())

    def action_video_targeted_scan(self) -> None:
        """Trigger targeted video scan for marked/selected items."""
        from .widgets.media_lists import VideoList
        try:
            vl = self.query_one(VideoList)
        except Exception:
            self.notify("No video list focused", severity="warning")
            return

        videos = vl.get_marked_videos()
        if not videos:
            self.notify("No videos selected/marked", severity="warning")
            return

        # Classification helper
        def is_tv_video(v):
            v_type = str(v.get("type") or "").lower()
            if "movie" in v_type: return False
            if "tv" in v_type: return True
            # Fallback for untyped: check for series/season metadata
            if v.get("series") or v.get("season"): return True
            return False

        tv_vids = [v for v in videos if is_tv_video(v)]
        movie_vids = [v for v in videos if not is_tv_video(v)]

        # If ONLY movies are selected, auto-fetch immediately (skip ALL dialogs)
        if movie_vids and not tv_vids:
            self._do_batch_movie_auto_fetch(movie_vids)
            return

        from .widgets.modals import VideoScanChoiceModal
        def handle_choice(method: str | None) -> None:
            if not method: return
            
            if method == "search":
                # Auto-fetch movies in background
                if movie_vids:
                    self._do_batch_movie_auto_fetch(movie_vids)
                
                # Then handle TV shows via dialog
                if tv_vids:
                    def handle_fetch_details(details: dict | None) -> None:
                        if details:
                            self.update_video_language(details["language"])
                            self.update_video_media_type(details["is_tv"])
                            # CRITICAL: Only scan the TV videos here!
                            self._do_batch_scan_by_id(tv_vids, details)
                    
                    v = tv_vids[0]
                    self.show_video_fetch_dialog(v.get("series") or v.get("title") or "", True, handle_fetch_details, 
                                                 season=v.get("season"), episode=v.get("episode"), is_batch=(len(tv_vids) > 1))
            else:
                # For auto methods (filename/metadata), process all
                self._do_batch_scan_auto(videos, method)
        
        self.push_screen(VideoScanChoiceModal(len(videos)), callback=handle_choice)

    @work(thread=True)
    def _do_batch_movie_auto_fetch(self, videos: list[dict]) -> None:
        lang = self.config.get("video", {}).get("language", "ja")
        self.call_from_thread(self.notify, f"Auto-fetching metadata for {len(videos)} Movie(s)...")
        for i, video in enumerate(videos, 1):
            if len(videos) > 1:
                self.call_from_thread(self.notify, f"Movie ({i}/{len(videos)}): {video['title'] or video['filename']}")
            self.video_lib.enrich_movie_by_exact_title(video, language=lang)
        
        if not any(v.get("type") and "tv" in str(v.get("type")).lower() for v in videos):
            # If ONLY movies were selected, show completion
            self.call_from_thread(self.notify, "Movie batch fetch complete")
            self.call_from_thread(self._refresh_current_screen)

    @work(thread=True)
    def _do_batch_scan_auto(self, videos: list[dict], method: str) -> None:
        lang = self.config.get("video", {}).get("language", "ja")
        use_filename = (method == "filename")
        self.call_from_thread(self.notify, f"Auto-scanning {len(videos)} video(s)...")
        for i, video in enumerate(videos, 1):
            if len(videos) > 1:
                self.call_from_thread(self.notify, f"Scanning ({i}/{len(videos)}): {video['filename']}")
            self.video_lib.enrich_single_video(video, use_filename=use_filename, language=lang)
        
        self.call_from_thread(self.notify, "Batch scan complete")
        self.call_from_thread(self._refresh_current_screen)

    @work(thread=True)
    def _do_batch_scan_by_id(self, videos: list[dict], details: dict) -> None:
        tmdb_id = details["tmdb_id"]
        is_tv = details["is_tv"]
        lang = details["language"]
        
        self.call_from_thread(self.notify, f"Refreshing {len(videos)} video(s) by TMDB ID {tmdb_id}...")
        
        for i, video in enumerate(videos, 1):
            if len(videos) > 1:
                self.call_from_thread(self.notify, f"Scanning ({i}/{len(videos)}): {video['filename']}")
            
            season = video.get("season")
            episode = video.get("episode")
            
            if len(videos) == 1:
                season = details.get("season")
                episode = details.get("episode")
            elif is_tv and (season is None or episode is None):
                from .utils import parse_video_filename
                meta = parse_video_filename(video["filename"])
                season = meta.get("season")
                episode = meta.get("episode")

            self.video_lib.enrich_single_video_by_id(video, tmdb_id, is_tv=is_tv, language=lang, season=season, episode=episode)
        
        self.call_from_thread(self.notify, "Batch scan complete")
        self.call_from_thread(self._refresh_current_screen)

    def _refresh_current_screen(self) -> None:
        """Reload the data in the currently active screen."""
        try:
            # 1. Video Screens
            video_screens = [
                "VideoLibraryScreen", "VideoCategoryScreen", "VideoFilterScreen",
                "VideoHealthScreen", "VideoContinueScreen", "VideoRecentScreen",
                "VideoPlaylistScreen"
            ]
            for s_name in video_screens:
                for screen in self.query(s_name):
                    if hasattr(screen, "_load"): screen._load()
                    if hasattr(screen, "reload_playlists"): screen.reload_playlists()

            # 2. Music Screens
            music_screens = [
                "MusicRecentScreen", "MusicFilterScreen", "MusicPlaylistScreen",
                "MusicArtistScreen"
            ]
            for s_name in music_screens:
                for screen in self.query(s_name):
                    if hasattr(screen, "_load"): screen._load()
                    if hasattr(screen, "_load_albums"): screen._load_albums()
                    if hasattr(screen, "reload_playlists"): screen.reload_playlists()

            # 3. Sidebar
            try:
                self.query_one("Sidebar").refresh_tree()
            except Exception: pass

        except Exception as e:
            logger.error(f"UI Refresh failed: {e}")

    async def _watch_theme(self) -> None:
        if not THEME_PATH.exists(): return
        try:
            mtime = THEME_PATH.stat().st_mtime
            if mtime != self._theme_mtime:
                self._theme_mtime = mtime
                theme = load_theme()
                colors = theme.get("colors", {})
                primary = colors.get("primary", "deepskyblue")
                accent = colors.get("accent", "magenta")
                bg = colors.get("background", "")
                surface = colors.get("surface", "")
                if not bg: bg = "$background"
                if not surface: surface = "$surface"
                
                sidebar_width = self.config.get("ui", {}).get("sidebar_width", 44)
                new_css = self._generate_css(primary, accent, bg, surface, sidebar_width)
                css_key = None
                for key in self.stylesheet.source.keys():
                    if isinstance(key, tuple) and len(key) == 2 and key[1].endswith(".CSS"):
                        css_key = key
                        break
                if css_key:
                    original = self.stylesheet.source[css_key]
                    self.stylesheet.source[css_key] = (new_css, original[1], original[2], original[3])
                    self.stylesheet.reparse()
                    self.stylesheet.update(self)
                    for screen in self.screen_stack: self.stylesheet.update(screen)
                    try: self.screen._refresh_layout(self.size)
                    except Exception: pass
                    self.refresh()
                    self.notify("Theme updated live")
        except Exception: pass

    @work(thread=True)
    def scan_libraries(self, loop: asyncio.AbstractEventLoop) -> None:
        logger.info("Starting library scan")
        try:
            # Phase 1: Fast scan (No UI notification per file to avoid flood)
            self.call_from_thread(self.notify, "Scanning local files...")
            self.music_lib.scan()

            # Phase 2: Metadata enrichment
            logger.info("Enriching music metadata (Phase 2)...")

            # Counter for throttling notifications
            self._enrich_count = 0

            def on_enrich_progress(msg):
                self._enrich_count += 1
                # Only notify every 5 items to avoid UI flood
                if self._enrich_count % 5 == 1 or "Enriching" in msg:
                    self.call_from_thread(self.notify, msg, timeout=3)

            self.music_lib.enrich_metadata(progress_cb=on_enrich_progress)
            self.call_from_thread(self.notify, "Online metadata fetch complete!", severity="information")
            
            # Refresh UI to show newly found covers/metadata
            self.call_from_thread(self._refresh_current_screen)

            # Video Scan
            self.call_from_thread(self.notify, "Scanning video library...")
            self.video_lib.scan()
            
            # Phase 2 Video: Metadata enrichment
            self.video_lib.enrich_metadata(progress_cb=on_enrich_progress)

            logger.info("Library scan complete!")
            self.call_from_thread(self.notify, "Library scan complete!")
            self.call_from_thread(self._refresh_sidebar)
        except Exception as e:
            logger.exception("Error during library scan")
            self.call_from_thread(self.notify, f"Scan failed: {e}", severity="error")

    def _refresh_sidebar(self) -> None:
        try: self.query_one(Sidebar).refresh_tree()
        except Exception: pass

    async def push_view(self, widget: Widget) -> None:
        content = self.query_one("#content", ContentArea)
        for child in list(content.children):
            child.display = False
            if child not in self.screen_history: self.screen_history.append(child)
        await content.mount(widget)

    async def action_go_back(self) -> None:
        if self.screen_history:
            content = self.query_one("#content", ContentArea)
            current = content.children[-1] if content.children else None
            if current: await current.remove()
            old = self.screen_history.pop()
            if old:
                old.display = True
                old.focus()

    def switch_screen(self, name: str, focus_right: bool = False) -> None:
        asyncio.create_task(self._switch_content(name, focus_right=focus_right))

    def switch_screen_with_data(self, name: str, data: any, focus_right: bool = False) -> None:
        asyncio.create_task(self._switch_content(name, data, focus_right=focus_right))

    async def _switch_content(self, name: str, data: any = None, focus_right: bool = False) -> None:
        if getattr(self, "_switching", False): return
        if self._current_screen_name == name and self._current_screen_data == data:
            if focus_right: self._apply_focus_right(name)
            return
        self._switching = True
        try:
            content = self.query_one("#content", ContentArea)
            for child in list(content.children): await child.remove()
            self.screen_history = []
            self._current_screen_name = name
            self._current_screen_data = data
            if name == "music": new_screen = MusicLibraryScreen()
            elif name == "music_recent": new_screen = MusicRecentScreen()
            elif name == "video": new_screen = VideoLibraryScreen()
            elif name == "video_continue": new_screen = VideoContinueScreen()
            elif name == "video_recent": new_screen = VideoRecentScreen()
            elif name == "video_health": new_screen = VideoHealthScreen()
            elif name in ("queue", "music_queue"): new_screen = QueueScreen()
            elif name == "artist": new_screen = MusicArtistScreen(data)
            elif name == "music_filter":
                from .screens.music import MusicFilterScreen
                new_screen = MusicFilterScreen(data["id"], data["name"])
            elif name == "music_playlists": new_screen = MusicPlaylistScreen()
            elif name == "video_playlists":
                from .screens.video import VideoPlaylistScreen
                new_screen = VideoPlaylistScreen()
            elif name == "video_category":
                from .screens.video import VideoCategoryScreen
                new_screen = VideoCategoryScreen(data)
            elif name == "video_filter":
                from .screens.video import VideoFilterScreen
                new_screen = VideoFilterScreen(data["id"], data["name"])
            else: new_screen = None
            if new_screen:
                await content.mount(new_screen)
                if focus_right: self._apply_focus_right(name)
                else: self.query_one("#nav-tree").focus()
        except Exception as e: self.notify(f"Error switching screen: {e}", severity="error")
        finally: self._switching = False

    def _apply_focus_right(self, name: str) -> None:
        if name in ("music_queue", "queue"):
            def focus_queue():
                try:
                    from .widgets.media_lists import TrackList
                    tl = self.query_one("#queue-tracks", TrackList)
                    table = tl.query_one(DataTable)
                    table.focus()
                    if table.row_count > 0: table.move_cursor(row=0)
                except Exception: pass
            self.call_later(focus_queue)
        elif name == "music_playlists":
            def focus_playlists():
                try:
                    table = self.query_one("#playlist-selector", DataTable)
                    table.focus()
                    if table.row_count > 0: table.move_cursor(row=0)
                except Exception: pass
            self.call_later(focus_playlists)
        elif name == "video_playlists":
            def focus_video_playlists():
                try:
                    table = self.query_one("#video-playlist-selector", DataTable)
                    table.focus()
                    if table.row_count > 0: table.move_cursor(row=0)
                except Exception: pass
            self.call_later(focus_video_playlists)

    def play_track(self, track: dict, tracks: list[dict] = None, idx: int = 0) -> None:
        asyncio.create_task(self.video_player.stop())
        if tracks:
            for t in tracks: t["is_video"] = False
        else: track["is_video"] = False
        self._current_media = track
        resume_pos = get_playback_position(track["path"], is_video=False)
        asyncio.create_task(self._play_music_with_resume(tracks or [track], idx, resume_pos))

    async def _play_music_with_resume(self, items, idx, pos):
        await self.music_player.play_queue(items, idx)
        if pos > 0:
            await asyncio.sleep(0.5)
            await self.music_player.seek(pos)

    def play_video(self, video: dict, videos: list[dict] = None, idx: int = 0, resume: bool = True) -> None:
        if videos:
            for v in videos: v["is_video"] = True
        else: video["is_video"] = True
        self._current_media = video
        resume_pos = get_playback_position(video["path"], is_video=True) if resume else 0
        asyncio.create_task(self._play_video_with_resume(videos or [video], idx, resume_pos))

    async def _play_video_with_resume(self, items, idx, pos):
        await self.music_player.stop()
        await self.video_player.play_queue(items, idx)
        if pos > 0:
            await asyncio.sleep(0.5)
            await self.video_player.seek(pos)

    def show_playlist_create_dialog(self, callback=None, is_video=False) -> None:
        cb = callback or self._on_playlist_created
        self.push_screen(PlaylistCreateModal(is_video=is_video), callback=cb)

    def _on_playlist_created(self, result: bool) -> None:
        if result: self.query_one(Sidebar).refresh_tree()

    def show_playlist_add_dialog(self, track_paths: list[str], is_video: bool = False) -> None:
        if isinstance(track_paths, str): track_paths = [track_paths]
        self.push_screen(PlaylistQuickAddModal(track_paths, is_video=is_video))

    def show_video_fetch_dialog(self, query: str, is_tv: bool | None, callback: callable, season: int = None, episode: int = None, is_batch: bool = False) -> None:
        from .widgets.modals import VideoFetchModal
        current_lang = self.config.get("video", {}).get("language", "ja")
        
        # Use provided is_tv, or default True
        if is_tv is None:
            is_tv = True
            
        self.push_screen(VideoFetchModal(query, is_tv, current_lang, season=season, episode=episode, is_batch=is_batch), callback=callback)

    def update_video_language(self, lang: str) -> None:
        if "video" not in self.config:
            self.config["video"] = {}
        self.config["video"]["language"] = lang
        from .config import save_config
        save_config(self.config)
        self.notify(f"Default language set to: {lang}")

    def update_video_media_type(self, is_tv: bool) -> None:
        # No longer persisting to config as per user request
        pass

    @work(thread=True)
    def export_playlist_to_m3u(self, playlist_id: int, is_video: bool = False) -> None:
        from .database import get_playlist_items
        table = "video_playlists" if is_video else "music_playlists"
        with get_connection() as conn:
            p = conn.execute(f"SELECT name FROM {table} WHERE id = ?", (playlist_id,)).fetchone()
        if not p: return
        name = p["name"]
        items = get_playlist_items(playlist_id, is_video=is_video)
        config_key = "video_playlist_dir" if is_video else "music_playlist_dir"
        config_dir = self.config.get("playlist", {}).get(config_key)
        if config_dir: m3u_dir = Path(config_dir[0]) if isinstance(config_dir, list) else Path(str(config_dir))
        else:
            base_dir = Path(self.config["video"]["directories"][0]) if is_video else Path(self.config["music"]["directories"][0])
            m3u_dir = base_dir / "Playlists"
        m3u_dir.mkdir(parents=True, exist_ok=True)
        m3u_path = m3u_dir / f"{name}.m3u"
        with open(m3u_path, "w", encoding="utf-8") as f:
            f.write("#EXTM3U\n")
            for t in items:
                f.write(f"#EXTINF:{t.get('duration', 0)},{t.get('artist') or 'Unknown'} - {t.get('title') or t.get('filename')}\n")
                f.write(f"{t['path']}\n")

    async def action_toggle_pause(self) -> None:
        if self.video_player.mpv and self.video_player.get_current_track(): await self.video_player.toggle_pause()
        elif self.music_player.get_current_track(): await self.music_player.toggle_pause()
        elif self.music_player._queue: await self.music_player.play_from_queue(0)

    def action_quit(self) -> None: self.push_screen(QuitModal())

    def action_switch_to_music_queue(self) -> None:
        self.switch_screen("music_queue", focus_right=True)
        try: self.query_one(Sidebar).select_node_by_data("music_queue")
        except Exception: pass

    def action_switch_to_music_playlists(self) -> None:
        self.switch_screen("music_playlists", focus_right=True)
        try: self.query_one(Sidebar).select_node_by_data("music_playlists")
        except Exception: pass

    def action_switch_to_video_playlists(self) -> None:
        self.switch_screen("video_playlists", focus_right=True)
        try: self.query_one(Sidebar).select_node_by_data("video_playlists")
        except Exception: pass

    def action_switch_to_queue(self) -> None:
        self.action_switch_to_music_queue()

    def action_switch_to_playlists(self) -> None:
        self.action_switch_to_music_playlists()

    def action_import_playlist(self) -> None:
        is_video = (self.video_player.mpv and self.video_player.get_current_track())
        config_key = "video_playlist_dir" if is_video else "music_playlist_dir"
        initial_dir = self.config.get("playlist", {}).get(config_key)
        if isinstance(initial_dir, list) and initial_dir: initial_dir = initial_dir[0]
        elif not initial_dir: initial_dir = self.config["video"]["directories"][0] if is_video else self.config["music"]["directories"][0]
        self.push_screen(FileSelectModal(initial_dir=initial_dir, pattern="*.m3u"), callback=self._on_m3u_selected)

    def _on_m3u_selected(self, m3u_path: str | None) -> None:
        if m3u_path: asyncio.create_task(self._do_import_m3u(m3u_path))

    async def _do_import_m3u(self, m3u_path: str) -> None:
        path = Path(m3u_path)
        path = Path(m3u_path)
        name = path.stem
        is_video = (self.video_player.mpv and self.video_player.get_current_track())
        try:
            with open(path, "r", encoding="utf-8") as f: lines = f.readlines()
            tracks = []
            for line in lines:
                line = line.strip()
                if not line or line.startswith("#"): continue
                t_path = Path(line)
                if not t_path.is_absolute(): t_path = path.parent / t_path
                tracks.append(str(t_path.resolve()))
            if not tracks:
                self.notify("No tracks found in M3U", severity="error")
                return
            p_table = "video_playlists" if is_video else "music_playlists"
            i_table = "video_playlist_files" if is_video else "music_playlist_tracks"
            col = "file_path" if is_video else "track_path"
            with get_connection() as conn:
                conn.execute(f"INSERT OR IGNORE INTO {p_table} (name) VALUES (?)", (name,))
                p = conn.execute(f"SELECT id FROM {p_table} WHERE name = ?", (name,)).fetchone()
                p_id = p["id"]
                conn.execute(f"DELETE FROM {i_table} WHERE playlist_id = ?", (p_id,))
                for i, t_path in enumerate(tracks):
                    conn.execute(f"INSERT INTO {i_table} (playlist_id, {col}, sort_order) VALUES (?, ?, ?)", (p_id, t_path, i))
            self.notify(f"Imported '{name}' with {len(tracks)} items")
            self.query_one(Sidebar).refresh_tree()
        except Exception as e: self.notify(f"Import failed: {e}", severity="error")

    def action_search(self) -> None:
        from .widgets.modals import GlobalSearchModal
        def handle_search(item: dict | None) -> None:
            if item:
                if item["type"] == "Music":
                    if item["cat"]:
                        self.switch_screen_with_data("artist", item["cat"])
                    else:
                        self.switch_screen("music")
                else:
                    with get_connection() as conn:
                        v = conn.execute("SELECT category FROM video_files WHERE path = ?", (item["path"],)).fetchone()
                        if v and v["category"]:
                            self.switch_screen_with_data("video_category", v["category"])
                        else:
                            self.switch_screen("video")
                self.notify(f"Navigated to {item['type']} screen")
        self.push_screen(GlobalSearchModal(), callback=handle_search)

    async def on_unmount(self) -> None:
        await self.mpv_info.shutdown()
        await self.music_player.shutdown()
        await self.video_player.shutdown()

if __name__ == "__main__":
    app = KitvcApp()
    app.run()
