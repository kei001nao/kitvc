from textual import work
from textual.app import ComposeResult
from textual.screen import Screen
from textual.widget import Widget
from textual.widgets import Label, Input, Button, DataTable, Select, TextArea
from textual.containers import Vertical, Horizontal, VerticalScroll
from ..database import get_connection, update_video_manual_fields
from ..widgets.media_lists import VideoList
from ..utils import ensure_local_image

class VideoLibraryScreen(Widget):
    DEFAULT_CSS = """
    VideoLibraryScreen { height: 1fr; padding: 1 2; }
    VideoLibraryScreen #video-heading { text-style: bold; margin-bottom: 1; }
    VideoLibraryScreen #video-status { color: $text-muted; margin-bottom: 1; }
    VideoLibraryScreen VideoList { border: solid $primary; }
    """

    def compose(self) -> ComposeResult:
        yield Label("Video Library", id="video-heading")
        yield Label("Loading…", id="video-status")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-list", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception:
            pass

    @work(thread=True)
    def _load(self) -> None:
        with get_connection() as conn:
            videos = [dict(row) for row in conn.execute("SELECT * FROM video_files ORDER BY category, series, season, episode, title").fetchall()]
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        self.query_one("#video-status", Label).update(f"[dim]{len(videos)} videos[/dim]")
        self.query_one("#video-list", VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

    def on_key(self, event) -> None:
        if event.key == "p":
            vl = self.query_one("#video-list", VideoList)
            from textual.widgets import DataTable
            try:
                table = vl.query_one(DataTable)
                if table.cursor_row is not None:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    video = vl._videos[idx]
                    
                    # If this video is already the current track, let global toggle_pause handle it
                    curr = self.app.video_player.get_current_track()
                    if curr and curr["path"] == video["path"] and self.app.video_player.mpv:
                        return
                    
                    self.app.play_video(video, vl._videos, idx, resume=True)
                    event.stop()
                    return
            except Exception:
                pass

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()
            try:
                from ..app import Sidebar
                self.app.query_one(Sidebar).refresh_tree()
            except Exception:
                pass

class VideoCategoryScreen(Widget):
    DEFAULT_CSS = """
    VideoCategoryScreen { height: 1fr; padding: 1 2; }
    VideoCategoryScreen #category-heading { text-style: bold; margin-bottom: 1; }
    VideoCategoryScreen VideoList { border: solid $primary; }
    """
    def __init__(self, category_name: str, **kwargs):
        super().__init__(**kwargs)
        self.category_name = category_name

    def compose(self) -> ComposeResult:
        display_name = self.category_name if self.category_name else "(unknown)"
        yield Label(f"Category: {display_name}", id="category-heading")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-list", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception:
            pass

    @work(thread=True)
    def _load(self) -> None:
        with get_connection() as conn:
            if self.category_name is None:
                videos = [dict(row) for row in conn.execute(
                    "SELECT * FROM video_files WHERE category IS NULL ORDER BY series, season, episode, title"
                ).fetchall()]
            else:
                videos = [dict(row) for row in conn.execute(
                    "SELECT * FROM video_files WHERE category = ? ORDER BY series, season, episode, title", 
                    (self.category_name,)
                ).fetchall()]
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        if not videos:
            # If the category is now empty, go back to library
            try:
                from ..app import Sidebar
                sidebar = self.app.query_one(Sidebar)
                sidebar.refresh_tree()
                self.app.switch_screen("video")
                sidebar.select_node_by_data("video_library")
                self.app.notify(f"Category is now empty")
            except Exception:
                pass


        self.query_one(VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

    def on_key(self, event) -> None:
        if event.key == "p":
            vl = self.query_one("#video-list", VideoList)
            from textual.widgets import DataTable
            try:
                table = vl.query_one(DataTable)
                if table.cursor_row is not None:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    video = vl._videos[idx]

                    # If this video is already the current track, let global toggle_pause handle it
                    curr = self.app.video_player.get_current_track()
                    if curr and curr["path"] == video["path"] and self.app.video_player.mpv:
                        return

                    self.app.play_video(video, vl._videos, idx, resume=True)
                    event.stop()
                    return
            except Exception:
                pass

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()
            try:
                from ..app import Sidebar
                sidebar = self.app.query_one(Sidebar)
                sidebar.refresh_tree()
            except Exception:
                pass

class VideoFilterScreen(Widget):
    DEFAULT_CSS = """
    VideoFilterScreen { height: 1fr; padding: 1 2; }
    VideoFilterScreen #filter-heading { text-style: bold; margin-bottom: 1; }
    VideoFilterScreen VideoList { border: solid $primary; }
    """
    def __init__(self, filter_id: int, filter_name: str, **kwargs):
        super().__init__(**kwargs)
        self.filter_id = filter_id
        self.filter_name = filter_name

    def compose(self) -> ComposeResult:
        yield Label(f"View: {self.filter_name}", id="filter-heading")
        yield Label("n: New View | e: Edit View | d: Delete View | q: Add All to Queue", classes="video-playlist-help")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-list", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception:
            pass

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection, get_filtered_videos
        with get_connection() as conn:
            f = conn.execute("SELECT * FROM video_filters WHERE id = ?", (self.filter_id,)).fetchone()
        
        if f:
            videos = get_filtered_videos(f["conditions_json"], f["sort_json"])
        else:
            videos = []
        
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        self.query_one(VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

    def on_key(self, event) -> None:
        if event.key == "n":
            from ..widgets.modals import VideoFilterEditModal
            self.app.push_screen(VideoFilterEditModal(), callback=self._on_view_edited)
            event.stop()
        elif event.key == "e":
            from ..database import get_connection
            with get_connection() as conn:
                f = conn.execute("SELECT * FROM video_filters WHERE id = ?", (self.filter_id,)).fetchone()
            if f:
                from ..widgets.modals import VideoFilterEditModal
                self.app.push_screen(VideoFilterEditModal(dict(f)), callback=self._on_view_edited)
            event.stop()
        elif event.key == "d":
            from ..widgets.modals import ConfirmModal
            def check_confirm(confirmed: bool) -> None:
                if confirmed:
                    from ..database import delete_video_filter
                    delete_video_filter(self.filter_id)
                    self.app.notify(f"View '{self.filter_name}' deleted")
                    try:
                        from ..app import Sidebar
                        self.app.query_one(Sidebar).refresh_tree()
                    except Exception: pass
                    self.app.switch_screen("video")
            self.app.push_screen(ConfirmModal(f"Delete view '{self.filter_name}'?"), callback=check_confirm)
            event.stop()
        elif event.key == "q":
            vl = self.query_one("#video-list", VideoList)
            if vl._videos:
                self.app.video_player.add_to_queue(vl._videos)
                self.app.notify(f"Added {len(vl._videos)} videos to queue")
            event.stop()
        elif event.key == "p":
            vl = self.query_one("#video-list", VideoList)
            from textual.widgets import DataTable
            try:
                table = vl.query_one(DataTable)
                if table.cursor_row is not None:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    video = vl._videos[idx]

                    # If this video is already the current track, let global toggle_pause handle it
                    curr = self.app.video_player.get_current_track()
                    if curr and curr["path"] == video["path"] and self.app.video_player.mpv:
                        return

                    self.app.play_video(video, vl._videos, idx, resume=True)
                    event.stop()
                    return
            except Exception:
                pass

    def _on_view_edited(self, result: bool) -> None:
        if result:
            self._load()
            try:
                from ..app import Sidebar
                self.app.query_one(Sidebar).refresh_tree()
            except Exception: pass

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()

class VideoPlaylistScreen(Widget):
    DEFAULT_CSS = """
    VideoPlaylistScreen { height: 1fr; padding: 1 2; layout: vertical; }
    VideoPlaylistScreen Label { text-style: bold; margin-bottom: 0; }
    #video-playlist-selector { height: 8; border: solid $primary; margin-bottom: 1; background: $surface; }
    #video-playlist-tracks-container { height: 1fr; border: solid $primary; background: $surface; }
    .video-playlist-help { color: $text-muted; margin-bottom: 1; }
    """

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._current_playlist_id = None
        self._current_playlist_name = None
        self._playlists_list = []
        self._current_playlist_idx = -1

    def compose(self) -> ComposeResult:
        yield Label("Video Playlists")
        yield Label("n: New  |  d: Delete Playlist  |  q: Add All to Queue  |  Enter: Select", classes="video-playlist-help")
        yield DataTable(id="video-playlist-selector", cursor_type="row")
        yield Label("Playlist Content")
        yield Label("d: Remove  |  Shift+Up/Down: Move  |  q: Add All to Queue", classes="video-playlist-help")
        with Vertical(id="video-playlist-tracks-container"):
            yield VideoList(id="video-playlist-tracks")

    def on_mount(self) -> None:
        table = self.query_one("#video-playlist-selector", DataTable)
        table.add_column("Playlist Name", key="name")
        self.reload_playlists()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-playlist-tracks", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception:
            pass

    def reload_playlists(self) -> None:
        from ..database import get_playlists
        self._playlists_list = get_playlists(is_video=True)
        table = self.query_one("#video-playlist-selector", DataTable)
        table.clear()
        self._current_playlist_idx = -1
        for i, p in enumerate(self._playlists_list):
            table.add_row(p['name'], key=str(i))
        
        if self._playlists_list:
            table.move_cursor(row=0)
            self._current_playlist_idx = 0
            self._load_playlist_videos(self._playlists_list[0]["id"], self._playlists_list[0]["name"])
        else:
            self._current_playlist_id = None
            self._current_playlist_name = None
            self.query_one("#video-playlist-tracks", VideoList).load([])

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        try:
            idx = int(str(event.row_key.value))
            self._current_playlist_idx = idx
            p = self._playlists_list[idx]
            self._load_playlist_videos(p["id"], p["name"])
            self.query_one("#video-playlist-tracks", VideoList).focus()
        except (ValueError, IndexError):
            pass

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        try:
            # ONLY handle events from the video playlist selector
            if event.control.id != "video-playlist-selector":
                return

            if event.row_key is None:
                return

            idx = int(str(event.row_key.value))
            if getattr(self, "_current_playlist_idx", -1) == idx:
                return
            
            self._current_playlist_idx = idx
            p = self._playlists_list[idx]
            self._load_playlist_videos(p["id"], p["name"])
        except (ValueError, IndexError):
            pass

    def _load_playlist_videos(self, playlist_id: int, name: str) -> None:
        self._current_playlist_id = playlist_id
        self._current_playlist_name = name
        from ..database import get_playlist_items
        items = get_playlist_items(playlist_id, is_video=True)
        self.query_one("#video-playlist-tracks", VideoList).load(items)

    def on_key(self, event) -> None:
        table_selector = self.query_one("#video-playlist-selector", DataTable)
        vl = self.query_one("#video-playlist-tracks", VideoList)
        from textual.widgets import DataTable as TextualDataTable
        
        try:
            table_videos = vl.query_one(TextualDataTable)
            vl_focused = table_videos.has_focus
        except Exception:
            vl_focused = False

        if table_selector.has_focus:
            if event.key == "n":
                self.app.show_playlist_create_dialog(callback=self._after_playlist_created, is_video=True)
                event.stop()
            elif event.key == "d" and table_selector.cursor_row is not None:
                try:
                    idx = int(str(table_selector.coordinate_to_cell_key(table_selector.cursor_coordinate).row_key.value))
                    p = self._playlists_list[idx]
                    p_id = p["id"]
                    p_name = p["name"]
                    
                    from ..widgets.modals import ConfirmModal
                    def check_confirm(confirmed: bool) -> None:
                        if confirmed:
                            from ..database import delete_playlist
                            delete_playlist(p_id, is_video=True)
                            self.app.notify(f"Playlist '{p_name}' deleted")
                            self.reload_playlists()
                    self.app.push_screen(ConfirmModal(f"Delete playlist '{p_name}'?"), callback=check_confirm)
                except Exception:
                    pass
                event.stop()
            elif event.key == "q" and table_selector.cursor_row is not None:
                if vl._videos:
                    self.app.video_player.add_to_queue(vl._videos)
                    self.app.notify(f"Added all videos from playlist to queue")
                event.stop()
        
        elif vl_focused or event.key == "p":
            if event.key == "p":
                try:
                    if table_videos.cursor_row is not None:
                        row_key = table_videos.coordinate_to_cell_key(table_videos.cursor_coordinate).row_key
                        idx = int(str(row_key.value))
                        video = vl._videos[idx]
                        
                        # If this video is already the current track, let global toggle_pause handle it
                        curr = self.app.video_player.get_current_track()
                        if curr and curr["path"] == video["path"] and self.app.video_player.mpv:
                            return
                        
                        self.app.play_video(video, vl._videos, idx, resume=True)
                        event.stop()
                except Exception:
                    pass
                return

            if event.key == "q":
                self.app.video_player.add_to_queue(vl._videos)
                self.app.notify(f"Added {len(vl._videos)} videos to queue")
                event.stop()
                return

            if event.key in ("d", "delete", "shift+up", "shift+down"):
                if table_videos.cursor_row is not None:
                    try:
                        coord = table_videos.cursor_coordinate
                        if table_videos.is_valid_coordinate(coord):
                            row_key = table_videos.coordinate_to_cell_key(coord).row_key
                            idx = int(str(row_key.value))
                            
                            if event.key == "d" or event.key == "delete":
                                video = vl._videos[idx]
                                from ..widgets.modals import ConfirmModal
                                def check_confirm(confirmed: bool) -> None:
                                    if confirmed:
                                        from ..database import remove_from_playlist
                                        remove_from_playlist(self._current_playlist_id, video["path"], is_video=True)
                                        self.app.notify("Removed from playlist")
                                        self._load_playlist_videos(self._current_playlist_id, self._current_playlist_name)
                                        self.app.export_playlist_to_m3u(self._current_playlist_id, is_video=True)
                                
                                self.app.push_screen(ConfirmModal("Remove from playlist?"), callback=check_confirm)
                                event.stop()
                            
                            elif event.key == "shift+up" or event.key == "shift+down":
                                to_idx = idx - 1 if event.key == "shift+up" else idx + 1
                                if 0 <= to_idx < len(vl._videos):
                                    from ..database import move_in_playlist
                                    move_in_playlist(self._current_playlist_id, idx, to_idx, is_video=True)
                                    self._load_playlist_videos(self._current_playlist_id, self._current_playlist_name)
                                    table_videos.move_cursor(row=to_idx)
                                    self.app.export_playlist_to_m3u(self._current_playlist_id, is_video=True)
                                event.stop()
                    except Exception:
                        pass


    def _after_playlist_created(self, result: bool) -> None:
        if result:
            self.reload_playlists()
            self.app.query_one("Sidebar").refresh_tree()

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

class VideoHealthScreen(Widget):
    DEFAULT_CSS = """
    VideoHealthScreen { height: 1fr; padding: 1 2; }
    VideoHealthScreen #health-heading { text-style: bold; margin-bottom: 1; }
    VideoHealthScreen VideoList { border: solid $primary; }
    """
    def compose(self) -> ComposeResult:
        yield Label("Metadata Health Check (Missing Synopsis/Year)", id="health-heading")
        yield Label("s: Batch Scan Marked Items", classes="video-playlist-help")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection
        with get_connection() as conn:
            videos = [dict(row) for row in conn.execute(
                "SELECT * FROM video_files WHERE synopsis IS NULL OR year IS NULL ORDER BY category, series, season, episode, title"
            ).fetchall()]
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        self.query_one(VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()

class VideoContinueScreen(Widget):
    DEFAULT_CSS = """
    VideoContinueScreen { height: 1fr; padding: 1 2; }
    VideoContinueScreen #continue-heading { text-style: bold; margin-bottom: 1; }
    VideoContinueScreen VideoList { border: solid $primary; }
    """
    def compose(self) -> ComposeResult:
        yield Label("Continue Watching", id="continue-heading")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-list", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception: pass

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection
        with get_connection() as conn:
            # last_pos > 0 AND last_played_at IS NOT NULL
            videos = [dict(row) for row in conn.execute(
                "SELECT * FROM video_files WHERE last_pos > 0 AND last_played_at IS NOT NULL ORDER BY last_played_at DESC LIMIT 50"
            ).fetchall()]
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        self.query_one(VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=True)

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()

class VideoRecentScreen(Widget):
    DEFAULT_CSS = """
    VideoRecentScreen { height: 1fr; padding: 1 2; }
    VideoRecentScreen #recent-heading { text-style: bold; margin-bottom: 1; }
    VideoRecentScreen VideoList { border: solid $primary; }
    """
    def compose(self) -> ComposeResult:
        yield Label("Recently Added Videos", id="recent-heading")
        yield VideoList(id="video-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-list", VideoList)
            track = self.app.video_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.video_player._paused
            vl.set_current_index_by_path(path, is_paused)
        except Exception: pass

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection
        with get_connection() as conn:
            videos = [dict(row) for row in conn.execute(
                "SELECT * FROM video_files ORDER BY created_at DESC LIMIT 50"
            ).fetchall()]
        self.app.call_from_thread(self._populate, videos)

    def _populate(self, videos: list[dict]) -> None:
        self.query_one(VideoList).load(videos)

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        self.app.play_video(event.video, event.videos, event.index, resume=False)

    def on_video_list_video_edit_requested(self, event: VideoList.VideoEditRequested) -> None:
        self.app.push_screen(VideoEditModal(event.video), callback=self._on_edit_finished)

    def on_video_list_batch_edit_requested(self, event: VideoList.BatchEditRequested) -> None:
        self.app.push_screen(VideoBatchEditModal(event.videos), callback=self._on_edit_finished)

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()

class VideoBatchEditModal(Screen):
    DEFAULT_CSS = """
    VideoBatchEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #batch-edit-container {
        width: 70;
        height: auto;
        max-height: 90%;
        border: panel $primary;
        background: $surface;
        padding: 1;
    }
    #batch-edit-container Label { margin-top: 1; text-style: bold; }
    #batch-edit-container Button { margin-top: 2; width: 100%; }
    #batch-edit-container Input { margin-bottom: 0; border: none; background: $accent 10%; padding: 0 1; height: 1; }
    #batch-edit-container Select { margin-bottom: 0; }
    .modal-help { text-align: center; color: $text-muted; margin-top: 1; height: 1; }
    #batch-scroll { height: auto; max-height: 30; }
    """

    def __init__(self, videos: list[dict], **kwargs):
        super().__init__(**kwargs)
        self.videos = videos

    def compose(self) -> ComposeResult:
        with Vertical(id="batch-edit-container"):
            yield Label(f"Batch Edit: {len(self.videos)} video(s)")
            with VerticalScroll(id="batch-scroll"):
                yield Label("Type:")
                yield Select([("Keep", "Keep"), ("Movie", "Movie"), ("TV Show", "TV Show")], value="Keep", id="batch-type")
                
                yield Label("Category:")
                yield Input(placeholder="Leave blank to keep current", id="batch-category")
                
                yield Label("SubCategory:")
                yield Input(placeholder="Leave blank to keep current", id="batch-subcategory")

                yield Label("Series:")
                yield Input(placeholder="Leave blank to keep current", id="batch-series")

                yield Label("Season:")
                yield Input(placeholder="Leave blank to keep current (Number)", id="batch-season")

            yield Button("Apply Changes", variant="primary", id="apply-btn")
            yield Label("Ctrl+Enter: Save   ESC: Cancel", classes="modal-help")

    def on_mount(self) -> None:
        self.query_one("#batch-type").focus()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "apply-btn":
            self._do_save()

    def _do_save(self) -> None:
        updates = {}
        v_type = self.query_one("#batch-type", Select).value
        if v_type != "Keep": updates["type"] = v_type
        
        cat = self.query_one("#batch-category", Input).value.strip()
        if cat: updates["category"] = cat
        
        sub = self.query_one("#batch-subcategory", Input).value.strip()
        if sub: updates["subcategory"] = sub
        
        series = self.query_one("#batch-series", Input).value.strip()
        if series: updates["series"] = series
        
        season_val = self.query_one("#batch-season", Input).value.strip()
        if season_val:
            try:
                updates["season"] = int(season_val)
            except ValueError:
                self.app.notify("Season must be a number", severity="error")
                return
        
        if not updates:
            self.app.notify("No changes specified", severity="warning")
            return

        self._apply(updates)

    @work(thread=True)
    def _apply(self, updates: dict) -> None:
        from ..database import update_video_manual_fields
        for v in self.videos:
            update_video_manual_fields(v["path"], updates)
        
        self.app.call_from_thread(self.app.notify, f"Updated {len(self.videos)} video(s)")
        self.app.call_from_thread(self.dismiss, True)

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(False)
            event.stop()
        elif event.key in ("ctrl+enter", "ctrl+j", "ctrl+m"):
            self._do_save()
            event.stop()

import unicodedata

def get_display_width(text: str) -> int:
    return sum(2 if unicodedata.east_asian_width(c) in "WFA" else 1 for c in text)

def truncate_to_width(text: str, max_width: int) -> str:
    current_width = 0
    res = []
    for c in text:
        w = 2 if unicodedata.east_asian_width(c) in "WFA" else 1
        if current_width + w > max_width:
            break
        res.append(c)
        current_width += w
    return "".join(res)

from textual_image.widget import Image

class VideoEditModal(Screen):
    DEFAULT_CSS = """
    VideoEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #edit-container {
        width: 80;
        height: 95%;
        border: panel $primary;
        background: $surface;
        padding: 0 1;
    }
    #edit-scroll {
        height: 1fr;
        overflow-y: auto;
        padding: 0 2 0 1;
    }
    #edit-scroll > * {
        margin-right: 2;
    }
    #edit-container Image {
        width: 70;
        height: 20;
        margin: 0 0 1 0;
        border: solid $primary;
    }
    #edit-container Label {
        margin-top: 1;
        margin-bottom: 0;
        text-style: bold;
        height: 1;
    }
    #edit-filename {
        margin: 0 0 0 0;
        height: 1;
        color: $text-muted;
    }
    #edit-container Input, #edit-container TextArea {
        margin-bottom: 0;
        border: none;
        background: $accent 10%;
        padding: 0 1;
    }
    #edit-container Input { height: 1; }
    #edit-container TextArea { height: 3; }

    #edit-title, #edit-series, #edit-synopsis, #edit-genres, #edit-series-overview, #edit-episode-overview { width: 98%; margin-right: 4; }
    #edit-date { width: 14; }
    #edit-season, #edit-episode { width: 18; }
    #edit-category, #edit-subcategory { width: 100%; }
    #edit-type { width: 100%; height: 4; border: none; background: $accent 10%; margin: 0; }

    #edit-refresh-btn {
        margin: 1 0;
        width: 100%;
        background: $primary;
        color: $text;
        border: none;
        height: 3;
    }

    #edit-container Horizontal {
        width: 100%;
        height: auto;
    }
    #edit-scroll Vertical, #edit-scroll Horizontal {
        height: auto;
    }
    #row-class { margin-bottom: 0; }
    #v-type { width: 30; }
    #v-cat-sub { width: 1fr; margin-left: 2; margin-right: 2; }
    
    #row-date-se { margin-top: 1; }
    #v-season { width: 14; }
    #v-episode { width: 14; margin-left: 2; }
    #v-date { width: 22; margin-left: 2; }

    .edit-help {
        width: 100%;
        text-align: center;
        background: $primary;
        color: $text;
        margin-top: 1;
        height: 1;
    }
    """

    def __init__(self, video: dict, **kwargs):
        super().__init__(**kwargs)
        self.video = video
        self._limits = {
            "edit-title": 100,
            "edit-series": 100,
            "edit-category": 30,
            "edit-season": 4,
            "edit-episode": 4,
            "edit-date": 10
        }

    def compose(self) -> ComposeResult:
        with Vertical(id="edit-container"):
            yield Label(f"Editing: {self.video['filename']}", id="edit-filename")
            with Vertical(id="edit-scroll"):
                # Image Priority: local_still > local_poster > local_series > still_path > poster_path > thumbnail_path
                display_img = self.video.get("local_still_path") or \
                             self.video.get("local_poster_path") or \
                             self.video.get("local_series_poster_path") or \
                             self.video.get("still_path") or \
                             self.video.get("poster_path") or \
                             self.video.get("thumbnail_path")
                if display_img:
                    local_img = ensure_local_image(display_img)
                    if local_img:
                        yield Image(local_img)
                
                yield Button("Search TMDB (Series/Season/Episode)", id="edit-refresh-btn")

                # Classification Section (Type, Category, SubCategory)
                with Horizontal(id="row-class"):
                    with Vertical(id="v-type"):
                        yield Label("Type")
                        yield Select([("Movie", "Movie"), ("TV Show", "TV Show")], 
                                    value=self.video.get("type") or "Movie", id="edit-type")
                    with Vertical(id="v-cat-sub"):
                        yield Label("Category")
                        cat_val = self.video.get("category") or getattr(self.app, "_last_video_category", "")
                        yield Input(value=cat_val or "", id="edit-category")
                        yield Label("SubCategory")
                        yield Input(value=self.video.get("subcategory") or "", id="edit-subcategory")

                yield Label("Genres")
                yield Input(value=self.video.get("genres") or "", id="edit-genres")

                yield Label("Title (Episode Name for TV)")
                yield Input(value=self.video.get("title") or "", id="edit-title")

                yield Label("Series")
                yield Input(value=self.video.get("series") or "", id="edit-series")

                # S, E, Date row (Moved BELOW Series)
                with Horizontal(id="row-date-se"):
                    with Vertical(id="v-season"):
                        yield Label("Season")
                        yield Input(value=str(self.video.get("season") or ""), id="edit-season")
                    with Vertical(id="v-episode"):
                        yield Label("Episode")
                        yield Input(value=str(self.video.get("episode") or ""), id="edit-episode")
                    with Vertical(id="v-date"):
                        yield Label("Date (yyyy-mm-dd)")
                        yield Input(value=self.video.get("air_date") or "", id="edit-date")

                yield Label("Series Overview")

                yield TextArea(self.video.get("series_overview") or "", id="edit-series-overview")

                yield Label("Synopsis")
                yield TextArea(self.video.get("synopsis") or "", id="edit-synopsis")

                yield Label("Episode Overview")
                yield TextArea(self.video.get("episode_overview") or "", id="edit-episode-overview")

            yield Label("Ctrl+Enter: Save   ESC: Cancel   Ctrl+s: Search TMDB", classes="edit-help")

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id in self._limits:
            max_w = self._limits[event.input.id]
            current_w = get_display_width(event.value)
            if current_w > max_w:
                event.input.value = truncate_to_width(event.value, max_w)

    def on_mount(self) -> None:
        self.query_one("#edit-type").focus()

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(False)
            event.stop()
        elif event.key == "ctrl+s":
            self._refresh_info()
            event.stop()
        elif event.key in ("ctrl+j", "ctrl+enter", "ctrl+m"): 
            self._save()
            event.stop()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "edit-refresh-btn":
            self._refresh_info()

    def _refresh_info(self) -> None:
        def handle_fetch_details(details: dict | None) -> None:
            if details:
                self.app.update_video_language(details["language"])
                self.app.update_video_media_type(details["is_tv"])
                self._do_refresh_info(details)
        
        # Rule: Use series if TV, title if Movie
        v_type = str(self.query_one("#edit-type", Select).value or "").lower()
        if "tv" in v_type:
            query = self.query_one("#edit-series", Input).value or self.query_one("#edit-title", Input).value
            is_tv_hint = True
        else:
            query = self.query_one("#edit-title", Input).value
            is_tv_hint = False
            
        try:
            s_val = self.query_one("#edit-season", Input).value
            e_val = self.query_one("#edit-episode", Input).value
            season = int(s_val) if s_val else None
            episode = int(e_val) if e_val else None
        except ValueError:
            season = episode = None
            
        self.app.show_video_fetch_dialog(query, is_tv_hint, handle_fetch_details, season=season, episode=episode, is_batch=False)

    @work(thread=True)
    def _do_refresh_info(self, details: dict) -> None:
        lang = details["language"]
        tmdb_id = details.get("tmdb_id")
        is_tv = details["is_tv"]
        season = details.get("season")
        episode = details.get("episode")
        
        if tmdb_id:
            from ..metadata_video import fetch_video_details_by_id
            self.app.call_from_thread(self.app.notify, f"Fetching details for ID {tmdb_id}...")
            full_meta = fetch_video_details_by_id(tmdb_id, is_tv=is_tv, language=lang, season=season, episode=episode)
            
            if full_meta:
                # Merge selected S/E and ID into meta
                full_meta["season"] = season
                full_meta["episode"] = episode
                full_meta["tmdb_id"] = str(tmdb_id)
                
                # Fetching details should also trigger poster download
                from ..library import CONFIG_DIR
                from ..metadata_video import download_video_poster
                if full_meta.get("poster_path"):
                    local = download_video_poster(full_meta["poster_path"], CONFIG_DIR / "posters", full_meta.get("series") or full_meta.get("title") or "video")
                    if local:
                        full_meta["local_poster_path"] = local
                if full_meta.get("series_poster_path"):
                    local = download_video_poster(full_meta["series_poster_path"], CONFIG_DIR / "posters", (full_meta.get("series") or "series") + "_series")
                    if local:
                        full_meta["local_series_poster_path"] = local
                if full_meta.get("still_path"):
                    local = download_video_poster(full_meta["still_path"], CONFIG_DIR / "posters", (full_meta.get("series") or "series") + "_still")
                    if local:
                        full_meta["local_still_path"] = local

                # IMPORTANT: Update local state directly for immediate UI reflection
                for k, v in full_meta.items():
                    self.video[k] = v
                
                # Update DB in background
                from ..database import update_video_manual_fields
                update_video_manual_fields(self.video["path"], full_meta)
                
                # Update UI
                self.app.call_from_thread(self._update_ui)
                self.app.call_from_thread(self.app.notify, "Metadata updated from TMDB")
            else:
                self.app.call_from_thread(self.app.notify, "Failed to fetch details from TMDB", severity="error")
        else:
            self.app.call_from_thread(self.app.notify, "Search cancelled", severity="warning")

    def _update_ui(self) -> None:
        """Update UI fields after fetch."""
        # 1. Update text inputs
        fields_map = {
            "title": ("#edit-title", Input),
            "series": ("#edit-series", Input),
            "air_date": ("#edit-date", Input),
            "genres": ("#edit-genres", Input),
            "category": ("#edit-category", Input),
            "subcategory": ("#edit-subcategory", Input),
            "season": ("#edit-season", Input),
            "episode": ("#edit-episode", Input),
            "synopsis": ("#edit-synopsis", TextArea),
            "series_overview": ("#edit-series-overview", TextArea),
            "episode_overview": ("#edit-episode-overview", TextArea)
        }
        for field, (selector, widget_type) in fields_map.items():
            try:
                val = self.video.get(field)
                widget = self.query_one(selector, widget_type)
                if widget_type == Input:
                    widget.value = str(val) if val is not None else ""
                else:
                    widget.load_text(str(val) if val is not None else "")
            except Exception: pass
        
        # 2. Update Select widget
        try:
            self.query_one("#edit-type", Select).value = self.video.get("type") or "Movie"
        except Exception: pass

        # 3. Update Image Preview
        display_img = self.video.get("local_still_path") or \
                     self.video.get("local_poster_path") or \
                     self.video.get("local_series_poster_path") or \
                     self.video.get("still_path") or \
                     self.video.get("poster_path") or \
                     self.video.get("thumbnail_path")
        if display_img:
            self._refresh_poster(display_img)

    def _refresh_poster(self, path: str) -> None:
        local_path = ensure_local_image(path)
        if not local_path:
            return
        try:
            img_widget = self.query_one(Image)
            parent = img_widget.parent
            img_widget.remove()
            parent.mount(Image(local_path), before=self.query_one("#edit-refresh-btn"))
        except Exception:
            try:
                scroll = self.query_one("#edit-scroll")
                scroll.mount(Image(local_path), before=self.query_one("#edit-refresh-btn"))
            except Exception: pass

    def _save(self) -> None:
        try:
            def clean(val: str) -> str | None:
                if val is None: return None
                v = val.strip()
                return v if v else None

            fields = {
                "title": clean(self.query_one("#edit-title", Input).value),
                "series": clean(self.query_one("#edit-series", Input).value),
                "season": int(self.query_one("#edit-season", Input).value or 0),
                "episode": int(self.query_one("#edit-episode", Input).value or 0),
                "category": clean(self.query_one("#edit-category", Input).value),
                "subcategory": clean(self.query_one("#edit-subcategory", Input).value),
                "type": str(self.query_one("#edit-type", Select).value or "Movie"),
                "air_date": clean(self.query_one("#edit-date", Input).value),
                "genres": clean(self.query_one("#edit-genres", Input).value),
                "synopsis": clean(self.query_one("#edit-synopsis", TextArea).text),
                "series_overview": clean(self.query_one("#edit-series-overview", TextArea).text),
                "episode_overview": clean(self.query_one("#edit-episode-overview", TextArea).text),
            }
            # Update year from air_date for compatibility if possible
            if fields["air_date"] and len(fields["air_date"]) >= 4:
                try:
                    fields["year"] = int(fields["air_date"][:4])
                except ValueError: pass

            if fields["category"]:
                self.app._last_video_category = fields["category"]
            
            update_video_manual_fields(self.video["path"], fields)
            self.app.notify("Video info updated")
            self.dismiss(True)
        except ValueError:
            self.app.notify("Invalid number for Season/Episode", severity="error")
        except Exception as e:
            self.app.notify(f"Save failed: {e}", severity="error")

