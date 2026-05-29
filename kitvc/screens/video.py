from textual import work
from textual.app import ComposeResult
from textual.screen import Screen
from textual.widget import Widget
from textual.widgets import Label, Input, Button, DataTable
from textual.containers import Vertical, Horizontal
from ..database import get_connection, update_video_manual_fields
from ..widgets.media_lists import VideoList

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

    def _on_edit_finished(self, result: bool) -> None:
        if result:
            self._load()
            try:
                from ..app import Sidebar
                sidebar = self.app.query_one(Sidebar)
                sidebar.refresh_tree()
            except Exception:
                pass

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
        width: 74;
        height: 90%;
        border: panel $primary;
        background: $surface;
        padding: 0 1;
    }
    #edit-scroll {
        height: 1fr;
        overflow-y: auto;
        padding: 0 4 0 1;
    }
    #edit-scroll > * {
        margin-right: 2;
    }
    #edit-container Image {
        width: 64;
        height: 18;
        margin: 0 0 1 0;
        border: solid $primary;
    }
    #edit-container Label {
        margin-top: 0;
        margin-bottom: 0;
        text-style: bold;
        height: 1;
    }
    #edit-filename {
        margin: 0 0 0 0;
        height: 1;
    }
    #edit-container Input {
        margin-bottom: 0;
        height: 1;
        border: none;
        background: $accent 10%;
        padding: 0 1;
    }
    #edit-title, #edit-series { width: 62; }
    #edit-category { width: 32; }
    #edit-type { width: 22; }
    #edit-season, #edit-episode { width: 6; }

    #edit-refresh-btn {
        margin: 1 0 0 0;
        width: 100%;
        background: $primary;
        color: $text;
        border: none;
        height: 1;
    }

    #edit-synopsis-text {
        height: auto;
        min-height: 2;
        max-height: 8;
        background: $accent 5%;
        padding: 0 1;
        margin-bottom: 1;
        color: $text-muted;
        text-style: italic;
    }

    #edit-container Horizontal {
        margin-top: 0;
        height: auto;
    }
    #edit-container Horizontal Vertical {
        width: auto;
        height: auto;
        margin-right: 2;
    }
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
            "edit-title": 60,
            "edit-series": 60,
            "edit-category": 30,
            "edit-type": 20,
            "edit-season": 4,
            "edit-episode": 4
        }

    def compose(self) -> ComposeResult:
        with Vertical(id="edit-container"):
            yield Label(f"Editing: {self.video['filename']}", id="edit-filename")
            with Vertical(id="edit-scroll"):
                poster = self.video.get("poster_path")
                thumb = self.video.get("thumbnail_path")
                display_img = poster if poster else thumb
                
                if display_img:
                    from pathlib import Path
                    # Image widget handles URLs too
                    yield Image(display_img)
                
                yield Label("Synopsis")
                yield Label(self.video.get("synopsis") or "(No synopsis available)", id="edit-synopsis-text")
                yield Button("Fetch Info by Metadata", id="edit-refresh-btn")
                
                yield Label("Title")
                yield Input(value=self.video.get("title") or "", id="edit-title")
                yield Label("Series")
                yield Input(value=self.video.get("series") or "", id="edit-series")
                with Horizontal():
                    with Vertical():
                        yield Label("Season")
                        yield Input(value=str(self.video.get("season") or ""), id="edit-season", restrict=r"[0-9]*")
                    with Vertical(id="col-ep"):
                        yield Label("Episode")
                        yield Input(value=str(self.video.get("episode") or ""), id="edit-episode", restrict=r"[0-9]*")

                        # Inherit category from last session edit if current is empty
                        cat_val = self.video.get("category") or getattr(self.app, "_last_video_category", "")
                        yield Label("Category")
                        yield Input(value=cat_val or "", id="edit-category")

                        yield Label("Type")
                        yield Input(value=self.video.get("type") or "", id="edit-type")
            yield Label("Enter: Save   ESC: Cancel   Ctrl+s: Fetch", classes="edit-help")

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id in self._limits:
            max_w = self._limits[event.input.id]
            current_w = get_display_width(event.value)
            if current_w > max_w:
                event.input.value = truncate_to_width(event.value, max_w)

    def on_mount(self) -> None:
        self.query_one("#edit-title").focus()

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(False)
            event.stop()
        elif event.key == "ctrl+s":
            self._refresh_info()
            event.stop()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self._save()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "edit-refresh-btn":
            self._refresh_info()

    def _refresh_info(self) -> None:
        def handle_fetch_details(details: dict | None) -> None:
            if details:
                self.app.update_video_language(details["language"])
                self.app.update_video_media_type(details["is_tv"])
                self._do_refresh_info(details)
        
        query = self.query_one("#edit-series", Input).value or self.query_one("#edit-title", Input).value
        # If no explicit series/season, use None so app.show_video_fetch_dialog uses config default
        is_tv_hint = None
        if self.query_one("#edit-series", Input).value or self.query_one("#edit-season", Input).value:
            is_tv_hint = True
        
        try:
            season = int(self.query_one("#edit-season", Input).value or 0)
            episode = int(self.query_one("#edit-episode", Input).value or 0)
        except ValueError:
            season = episode = None
            
        self.app.show_video_fetch_dialog(query, is_tv_hint, handle_fetch_details, season=season, episode=episode)

    @work(thread=True)
    def _do_refresh_info(self, details: dict) -> None:
        lang = details["language"]
        tmdb_id = details.get("tmdb_id")
        is_tv = details["is_tv"]
        
        if tmdb_id:
            self.app.call_from_thread(self.app.notify, f"Fetching details for ID {tmdb_id}...")
            self.app.video_lib.enrich_single_video_by_id(self.video, tmdb_id, is_tv=is_tv, language=lang, season=details.get("season"), episode=details.get("episode"))
        else:
            # Fallback to old behavior if no ID (should not happen with new modal)
            query = details["query"]
            self.app.call_from_thread(self.app.notify, f"Fetching info [{lang}] for '{query}'...")
            
            temp_video = dict(self.video)
            temp_video["title"] = query
            temp_video["series"] = query if is_tv else None
            temp_video["season"] = int(self.query_one("#edit-season", Input).value or 0)
            self.app.video_lib.enrich_single_video(temp_video, use_filename=False, language=lang)
        
        # Reload video data from DB
        from ..database import get_connection
        with get_connection() as conn:
            updated = conn.execute("SELECT * FROM video_files WHERE path = ?", (self.video["path"],)).fetchone()
        
        if updated:
            self.video = dict(updated)
            self.app.call_from_thread(self._update_ui)
            self.app.call_from_thread(self.app.notify, "Metadata updated")

    def _update_ui(self) -> None:
        """Update UI fields after background refresh."""
        self.query_one("#edit-synopsis-text", Label).update(self.video.get("synopsis") or "(No synopsis available)")
        
        # ONLY update inputs if they are currently empty, or if we want to force update 
        # from newly fetched metadata. But TMDB fetch doesn't usually return Series/S/E.
        # Let's update ONLY the synopsis and title (if title was fetched).
        if self.video.get("title"):
             self.query_one("#edit-title", Input).value = self.video.get("title")

        # Keep current inputs for series/season/episode to avoid clearing them
        # if they were manually entered but not yet saved to DB.
        
        # Update Image if changed
        poster = self.video.get("poster_path")
        thumb = self.video.get("thumbnail_path")
        display_img = poster if poster else thumb
        if display_img:
            # We are already in the main thread (called via call_from_thread)
            self._refresh_poster(display_img)

    def _refresh_poster(self, path: str) -> None:
        try:
            img_widget = self.query_one(Image)
            # Recreating the image widget is the most reliable way to update
            parent = img_widget.parent
            img_widget.remove()
            parent.mount(Image(path), before=self.query_one("#edit-synopsis-text"))
        except Exception:
            pass

    def _save(self) -> None:
        try:
            def clean(val: str) -> str | None:
                v = val.strip()
                return v if v else None

            fields = {
                "title": clean(self.query_one("#edit-title", Input).value),
                "series": clean(self.query_one("#edit-series", Input).value),
                "season": int(self.query_one("#edit-season", Input).value or 0),
                "episode": int(self.query_one("#edit-episode", Input).value or 0),
                "category": clean(self.query_one("#edit-category", Input).value),
                "type": clean(self.query_one("#edit-type", Input).value),
            }
            # Remember the category for subsequent edits
            if fields["category"]:
                self.app._last_video_category = fields["category"]
            
            update_video_manual_fields(self.video["path"], fields)
            self.app.notify("Video info updated")
            self.dismiss(True)
        except ValueError:
            self.app.notify("Invalid number for Season/Episode", severity="error")


