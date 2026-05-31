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
from .screens.music import MusicLibraryScreen, MusicArtistScreen, MusicPlaylistScreen, QueueScreen
from .screens.video import VideoLibraryScreen, VideoCategoryScreen, VideoPlaylistScreen
from textual_image.widget import Image

LOGO = """█▄▀ █ ▀█▀ █ █ █▀
█ █ █  █  ╚▄▀ █▄""".strip("\n")

class Sidebar(Widget):
    DEFAULT_CSS = """
    Sidebar {
        width: 44;
        height: 100%;
        padding: 1 0;
        background: $surface;
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
        music.add_leaf("PlayLists", data="music_playlists")
        
        video = tree.root.add("Video", data="video_root", expand=True)
        video.add("Library", data="video_library")
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
            self.app.call_from_thread(self._populate_tree, artists, categories)
        except Exception as e:
            logger.error(f"Sidebar.refresh_tree failed: {e}")

    def _populate_tree(self, artists: list[str], categories: list[str]) -> None:
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
        
        video_lib = find_node(tree.root, "video_library")
        if video_lib:
            video_lib.remove_children()
            for cat in categories:
                video_lib.add_leaf(cat or "(unknown)", data={"type": "video_category", "name": cat})

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
        elif data == "video_library":
            self.app.switch_screen("video")
        elif data == "music_queue":
            self.app.switch_screen("music_queue")
        elif data == "music_playlists":
            self.app.switch_screen("music_playlists")
        elif data == "video_playlists":
            self.app.switch_screen("video_playlists")
        elif isinstance(data, dict):
            if data.get("type") == "artist":
                self.app.switch_screen_with_data("artist", data["name"])
            elif data.get("type") == "video_category":
                self.app.switch_screen_with_data("video_category", data["name"])

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
    ]

    def _generate_css(self, primary: str, accent: str, bg: str, surface: str, sidebar_width: int = 44) -> str:
        return f"""
        $primary: {primary};
        $accent: {accent};
        $background: {bg};
        $surface: {surface};

        App {{ background: $background; }}
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
        # Check if we are on a video screen
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

        from .widgets.modals import VideoScanChoiceModal
        def handle_choice(method: str | None) -> None:
            if method:
                # Ask for search details
                def handle_fetch_details(details: dict | None) -> None:
                    if details:
                        lang = details["language"]
                        is_tv = details["is_tv"]
                        self.update_video_language(lang)
                        self.update_video_media_type(is_tv)
                        self._do_targeted_video_scan(videos, details)
                
                # Use first video's data as default for the fetch dialog
                v = videos[0]
                default_query = v.get("series") or v.get("title")
                if method == "filename":
                    from .utils import parse_video_filename
                    meta = parse_video_filename(v["filename"])
                    default_query = meta.get("series") or meta.get("title")
                
                is_tv = bool(v.get("series") or v.get("season"))
                season = v.get("season")
                episode = v.get("episode")
                if method == "filename":
                    season = meta.get("season")
                    episode = meta.get("episode")
                
                self.show_video_fetch_dialog(default_query or "", is_tv, handle_fetch_details, season=season, episode=episode)
        
        self.push_screen(VideoScanChoiceModal(len(videos)), callback=handle_choice)

    @work(thread=True)
    def _do_targeted_video_scan(self, videos: list[dict], details: dict) -> None:
        query = details["query"]
        is_tv = details["is_tv"]
        lang = details["language"]
        
        self.call_from_thread(self.notify, f"Refreshing {len(videos)} video(s) [{lang}]...")
        
        for i, video in enumerate(videos, 1):
            if len(videos) > 1:
                self.call_from_thread(self.notify, f"Scanning ({i}/{len(videos)}): {video['filename']}")
            
            # Use custom query and type from the fetch modal for the FIRST video
            # For subsequent videos in a batch, it's tricky. 
            # If it's a batch, we probably should still use auto-logic but with the chosen language.
            # But let's assume if user specified a query, they want to apply it to all selected (e.g. same series).
            v_to_scan = dict(video)
            if i == 1:
                v_to_scan["series"] = query
                # We can't easily override 'is_tv' inside enrich_single_video without changes, 
                # but enrich_single_video already checks series/season.
            
            # Temporary change to VideoLibrary to support explicit override
            self.video_lib.enrich_single_video_by_id(v_to_scan, details["tmdb_id"], is_tv=is_tv, language=lang, season=details.get("season"), episode=details.get("episode"))
        
        self.call_from_thread(self.notify, "Targeted scan complete")
        self.call_from_thread(self._refresh_current_screen)

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

    def _refresh_current_screen(self) -> None:
        """Refresh the currently active screen if it supports reloading."""
        try:
            # If we are on MusicArtistScreen, it has _load_albums
            # Note: self.screen might be a screen or a specific widget depending on push_view
            # Let's check the focused screen and its children
            current = self.screen
            if hasattr(current, "_load_albums"):
                current._load_albums()
            elif hasattr(current, "reload_playlists"):
                current.reload_playlists()
                
            # Also check if it's within our ContentArea
            try:
                content = self.query_one("#content")
                for child in content.children:
                    if hasattr(child, "_load_albums"):
                        child._load_albums()
            except Exception:
                pass
        except Exception as e:
            logger.error(f"UI Refresh failed: {e}")

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
            elif name == "video": new_screen = VideoLibraryScreen()
            elif name in ("queue", "music_queue"): new_screen = QueueScreen()
            elif name == "artist": new_screen = MusicArtistScreen(data)
            elif name == "music_playlists": new_screen = MusicPlaylistScreen()
            elif name == "video_playlists":
                from .screens.video import VideoPlaylistScreen
                new_screen = VideoPlaylistScreen()
            elif name == "video_category":
                from .screens.video import VideoCategoryScreen
                new_screen = VideoCategoryScreen(data)
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

    def show_video_fetch_dialog(self, query: str, is_tv: bool | None, callback: callable, season: int = None, episode: int = None) -> None:
        from .widgets.modals import VideoFetchModal
        current_lang = self.config.get("video", {}).get("language", "ja")
        
        # Use provided is_tv, or default True
        if is_tv is None:
            is_tv = True
            
        self.push_screen(VideoFetchModal(query, is_tv, current_lang, season=season, episode=episode), callback=callback)

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

    async def on_unmount(self) -> None:
        await self.mpv_info.shutdown()
        await self.music_player.shutdown()
        await self.video_player.shutdown()

if __name__ == "__main__":
    app = KitvcApp()
    app.run()
