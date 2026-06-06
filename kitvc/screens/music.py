from textual import work
from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Label, DataTable, Static
from textual.containers import Vertical
from ..database import get_connection
from ..widgets.media_lists import TrackList
import asyncio
from pathlib import Path

class MusicLibraryScreen(Widget):
    DEFAULT_CSS = """
    MusicLibraryScreen { height: 1fr; padding: 1 2; layout: vertical; }
    MusicLibraryScreen Label { text-style: bold; margin-bottom: 0; }
    MusicLibraryScreen #music-heading { margin-bottom: 1; }
    MusicLibraryScreen .playlist-help { color: $text-muted; margin-bottom: 1; }
    MusicLibraryScreen DataTable { height: 1fr; background: $surface; border: solid $primary; }
    MusicLibraryScreen DataTable:focus { border: tall $accent; }
    """

    def compose(self) -> ComposeResult:
        yield Label("Music Library", id="music-heading")
        yield Label("Enter: Select", classes="playlist-help")
        yield DataTable(id="music-artists", cursor_type="row")

    def on_mount(self) -> None:
        table = self.query_one("#music-artists", DataTable)
        table.add_column("Artist", key="artist")
        self._load()

    @work(thread=True)
    def _load(self) -> None:
        with get_connection() as conn:
            artists = [row["artist"] for row in conn.execute("SELECT DISTINCT artist FROM music_tracks ORDER BY artist COLLATE NOCASE").fetchall()]
            count = conn.execute("SELECT COUNT(*) FROM music_tracks").fetchone()[0]
        
        self.app.call_from_thread(self._populate, artists, count)

    def _populate(self, artists: list[str], count: int) -> None:
        table = self.query_one("#music-artists", DataTable)
        table.clear()
        for name in artists:
            table.add_row(name, key=name)
        if artists:
            table.move_cursor(row=0)

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        artist_name = str(event.row_key.value)
        if artist_name:
            screen = MusicArtistScreen(artist_name)
            asyncio.create_task(self.app.push_view(screen))
            self.call_later(lambda: screen.query_one("#music-albums").focus())

class MusicArtistScreen(Widget):
    DEFAULT_CSS = """
    MusicArtistScreen { height: 1fr; padding: 1 2; layout: vertical; }
    MusicArtistScreen Label { text-style: bold; margin-bottom: 0; }
    MusicArtistScreen #music-artist-heading { margin-bottom: 1; }
    MusicArtistScreen .playlist-help { color: $text-muted; margin-bottom: 1; }
    MusicArtistScreen DataTable#music-albums { height: 11; background: $surface; border: solid $primary; margin-bottom: 1; }
    MusicArtistScreen DataTable#music-albums:focus { border: tall $accent; }
    MusicArtistScreen TrackList { height: 1fr; border: solid $primary; background: $surface; }
    MusicArtistScreen TrackList:focus-within { border: tall $accent; }
    """

    def __init__(self, artist_name: str, **kwargs):
        super().__init__(**kwargs)
        self._artist_name = artist_name
        self._current_album_tracks = []
        self._albums_list = []
        self._current_album_idx = -1

    def compose(self) -> ComposeResult:
        yield Label(self._artist_name, id="music-artist-heading")
        yield Label("Albums")
        yield Label("q: Add Album to Queue  |  a: Add Album to Playlist  |  Enter: Select", classes="playlist-help")
        yield DataTable(id="music-albums", cursor_type="row", zebra_stripes=True)
        yield Label("Tracks")
        yield Label("m/Space: Mark  |  a: Add to Playlist  |  q: Add to Queue  |  Enter: Play", classes="playlist-help")
        yield TrackList(id="music-tracks")

    def on_mount(self) -> None:
        table = self.query_one("#music-albums", DataTable)
        table.styles.height = 11  # Ensure height is set
        table.add_column("Date", key="release_date", width=12)
        table.add_column("Album", key="album")
        self._load_albums()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            tl = self.query_one("#music-tracks", TrackList)
            curr = self.app.music_player.get_current_track()
            if not curr:
                tl.set_current_index(-1)
                return
            is_paused = self.app.music_player._paused
            tl.set_current_index_by_path(curr["path"], is_paused)
        except Exception:
            pass

    @work(thread=True)
    def _load_albums(self) -> None:
        with get_connection() as conn:
            # Query the music_albums table directly
            albums = [dict(row) for row in conn.execute(
                "SELECT id, title as album, release_date, cover_path FROM music_albums WHERE artist = ? ORDER BY release_date DESC, title", 
                (self._artist_name,)
            ).fetchall()]
        self.app.call_from_thread(self._populate_albums, albums)

    def _populate_albums(self, albums: list[dict]) -> None:
        self._albums_list = albums
        table = self.query_one("#music-albums", DataTable)
        table.clear()
        self._current_album_idx = -1
        
        if not albums:
            # Clear tracks if no albums
            self.query_one(TrackList).load([])
            self.app.update_sidebar_cover(None)
            return

        for i, album in enumerate(albums):
            date_str = album['release_date'] or ""
            table.add_row(date_str, album['album'], key=str(i))
        if albums:
            table.move_cursor(row=0)
            self._current_album_idx = 0
            self._load_tracks(albums[0]['album'])
            if albums[0].get("cover_path"):
                self.app.update_sidebar_cover(albums[0]["cover_path"])

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        try:
            # ONLY handle events from the album selector
            if event.control.id != "music-albums":
                return

            if event.row_key is None:
                return

            idx = int(str(event.row_key.value))
            if getattr(self, "_current_album_idx", -1) == idx:
                return
            
            self._current_album_idx = idx
            album = self._albums_list[idx]
            self._load_tracks(album['album'])
            
            # Update sidebar cover on highlight
            if album.get("cover_path"):
                self.app.update_sidebar_cover(album["cover_path"])
            else:
                self.app.update_sidebar_cover(None)
                
        except (ValueError, IndexError):
            pass

    def on_key(self, event) -> None:
        table = self.query_one("#music-albums", DataTable)
        if event.key == "q":
            if table.has_focus and table.cursor_row is not None:
                try:
                    idx = int(str(table.coordinate_to_cell_key(table.cursor_coordinate).row_key.value))
                    album_name = self._albums_list[idx]['album']
                    self._add_album_to_queue(album_name)
                except Exception:
                    pass
            event.stop()
        elif event.key == "a":
            if table.has_focus and table.cursor_row is not None:
                if self._current_album_tracks:
                    self.app.show_playlist_add_dialog([t["path"] for t in self._current_album_tracks])
            event.stop()
        elif event.key == "ctrl+e":
            if table.has_focus and table.cursor_row is not None:
                try:
                    idx = int(str(table.coordinate_to_cell_key(table.cursor_coordinate).row_key.value))
                    album = self._albums_list[idx]
                    album_data = dict(album)
                    album_data["artist"] = self._artist_name
                    from .music import MusicAlbumEditModal
                    self.app.push_screen(MusicAlbumEditModal(album_data), callback=self._on_album_edit_finished)
                except Exception:
                    pass
            event.stop()

    @work(thread=True)
    def _add_album_to_queue(self, album_name: str) -> None:
        with get_connection() as conn:
            tracks = [dict(row) for row in conn.execute(
                "SELECT * FROM music_tracks WHERE artist = ? AND album = ? ORDER BY disc_num, track_num",
                (self._artist_name, album_name)
            ).fetchall()]
        for t in tracks: t["is_video"] = False
        self.app.music_player.add_to_queue(tracks)
        self.app.notify(f"Added album '{album_name}' to queue")

    @work(thread=True)
    def _load_tracks(self, album_name: str) -> None:
        with get_connection() as conn:
            tracks = [dict(row) for row in conn.execute(
                """
                SELECT t.*, a.cover_path as album_cover, a.release_date, a.mbid, a.comment as album_comment
                FROM music_tracks t
                LEFT JOIN music_albums a ON t.album_id = a.id
                WHERE t.artist = ? AND t.album = ? 
                ORDER BY t.disc_num, t.track_num
                """,
                (self._artist_name, album_name)
            ).fetchall()]
        
        for t in tracks:
            if t.get("album_cover"):
                t["cover_path"] = t["album_cover"]
                
        self._current_album_tracks = tracks
        self.app.call_from_thread(self.query_one(TrackList).load, tracks)

    def _on_album_edit_finished(self, result: bool) -> None:
        if result:
            # Re-check if this artist still has albums (they might have been renamed)
            with get_connection() as conn:
                exists = conn.execute("SELECT 1 FROM music_albums WHERE artist = ?", (self._artist_name,)).fetchone()
            
            if not exists:
                # If renamed or no albums, this screen is no longer valid, go back
                self.app.action_go_back()
                self.app.notify("Artist metadata changed, returning to library")
            else:
                self._load_albums()

            try:
                from ..app import Sidebar
                self.app.query_one(Sidebar).refresh_tree()
            except Exception:
                pass

    def on_track_list_track_edit_requested(self, event: TrackList.TrackEditRequested) -> None:
        from .music import MusicTrackEditModal
        self.app.push_screen(MusicTrackEditModal(event.track), callback=self._on_track_edit_finished)

    def _on_track_edit_finished(self, result: bool) -> None:
        if result:
            # Force reload the tracks for the currently selected album
            if self._current_album_idx != -1:
                album_name = self._albums_list[self._current_album_idx]['album']
                self._load_tracks(album_name)

    def on_track_list_track_selected(self, event: TrackList.TrackSelected) -> None:
        self.app.play_track(event.track, event.tracks, event.index)

class MusicPlaylistScreen(Widget):
    DEFAULT_CSS = """
    MusicPlaylistScreen { height: 1fr; padding: 1 2; layout: vertical; }
    MusicPlaylistScreen Label { text-style: bold; margin-bottom: 0; }
    #playlist-selector { height: 8; border: solid $primary; margin-bottom: 1; background: $surface; }
    #playlist-selector:focus { border: tall $accent; }
    #playlist-tracks-container { height: 1fr; border: solid $primary; background: $surface; }
    #playlist-tracks-container:focus-within { border: tall $accent; }
    .playlist-help { color: $text-muted; margin-bottom: 1; }
    """

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._current_playlist_id = None
        self._current_playlist_name = None
        self._playlists_list = []
        self._current_playlist_idx = -1

    def compose(self) -> ComposeResult:
        yield Label("Playlists")
        yield Label("n: New  |  d: Delete Playlist  |  q: Add All to Queue  |  Enter: Select", classes="playlist-help")
        yield DataTable(id="playlist-selector", cursor_type="row")
        yield Label("Playlist Content")
        yield Label("d: Remove Track  |  Shift+Up/Down: Move  |  q: Add All to Queue", classes="playlist-help")
        with Vertical(id="playlist-tracks-container"):
            yield TrackList(id="playlist-tracks")

    def on_mount(self) -> None:
        table = self.query_one("#playlist-selector", DataTable)
        table.add_column("Playlist Name", key="name")
        self.reload_playlists()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            tl = self.query_one("#playlist-tracks", TrackList)
            curr = self.app.music_player.get_current_track()
            if not curr:
                tl.set_current_index(-1)
                return
            
            # For playlists, we should probably search by path because the queue might be different
            # But TrackList only has set_current_index. 
            # Let's add set_current_index_by_path to TrackList as well.
            is_paused = self.app.music_player._paused
            tl.set_current_index_by_path(curr["path"], is_paused)
        except Exception:
            pass

    def reload_playlists(self) -> None:
        from ..database import get_playlists
        self._playlists_list = get_playlists(is_video=False)
        table = self.query_one("#playlist-selector", DataTable)
        table.clear()
        self._current_playlist_idx = -1
        for i, p in enumerate(self._playlists_list):
            table.add_row(p['name'], key=str(i))
        
        if self._playlists_list:
            table.move_cursor(row=0)
            self._current_playlist_idx = 0
            self._load_playlist_tracks(self._playlists_list[0]["id"], self._playlists_list[0]["name"])
        else:
            self._current_playlist_id = None
            self._current_playlist_name = None
            self.query_one("#playlist-tracks", TrackList).load([])

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        try:
            idx = int(str(event.row_key.value))
            self._current_playlist_idx = idx
            p = self._playlists_list[idx]
            self._load_playlist_tracks(p["id"], p["name"])
            self.query_one("#playlist-tracks", TrackList).focus()
        except (ValueError, IndexError):
            pass

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        try:
            # ONLY handle events from the playlist selector
            if event.control.id != "playlist-selector":
                return

            if event.row_key is None:
                return

            idx = int(str(event.row_key.value))
            if getattr(self, "_current_playlist_idx", -1) == idx:
                return
            
            self._current_playlist_idx = idx
            p = self._playlists_list[idx]
            self._load_playlist_tracks(p["id"], p["name"])
        except (ValueError, IndexError):
            pass

    def _load_playlist_tracks(self, playlist_id: int, name: str) -> None:
        self._current_playlist_id = playlist_id
        self._current_playlist_name = name
        from ..database import get_playlist_tracks
        tracks = get_playlist_tracks(playlist_id)
        self.query_one("#playlist-tracks", TrackList).load(tracks)

    def on_key(self, event) -> None:
        table_selector = self.query_one("#playlist-selector", DataTable)
        tl = self.query_one("#playlist-tracks", TrackList)
        from textual.widgets import DataTable as TextualDataTable
        
        # Check if focus is within TrackList's DataTable
        try:
            table_tracks = tl.query_one(TextualDataTable)
            tl_focused = table_tracks.has_focus
        except Exception:
            tl_focused = False

        if table_selector.has_focus:
            if event.key == "n":
                self.app.show_playlist_create_dialog(callback=self._after_playlist_created)
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
                            delete_playlist(p_id, is_video=False)
                            self.app.notify(f"Playlist '{p_name}' deleted")
                            self.reload_playlists()
                    self.app.push_screen(ConfirmModal(f"Delete playlist '{p_name}'?"), callback=check_confirm)
                except Exception:
                    pass
                event.stop()
            elif event.key == "q" and table_selector.cursor_row is not None:
                if tl._tracks:
                    self.app.music_player.add_to_queue(tl._tracks)
                    self.app.notify(f"Added all tracks from playlist to queue")
                event.stop()
        
        elif tl_focused:
            if event.key == "q":
                self.app.music_player.add_to_queue(tl._tracks)
                self.app.notify(f"Added {len(tl._tracks)} tracks to queue")
                event.stop()
                return

            if event.key in ("d", "delete", "shift+up", "shift+down"):
                if table_tracks.cursor_row is not None:
                    try:
                        coord = table_tracks.cursor_coordinate
                        if table_tracks.is_valid_coordinate(coord):
                            row_key = table_tracks.coordinate_to_cell_key(coord).row_key
                            idx = int(str(row_key.value))
                            
                            if event.key == "d" or event.key == "delete":
                                track = tl._tracks[idx]
                                from ..widgets.modals import ConfirmModal
                                def check_confirm(confirmed: bool) -> None:
                                    if confirmed:
                                        from ..database import remove_from_playlist
                                        remove_from_playlist(self._current_playlist_id, track["path"], is_video=False)
                                        self.app.notify("Removed from playlist")
                                        self._load_playlist_tracks(self._current_playlist_id, self._current_playlist_name)
                                        self.app.export_playlist_to_m3u(self._current_playlist_id, is_video=False)
                                
                                self.app.push_screen(ConfirmModal("Remove from playlist?"), callback=check_confirm)
                                event.stop()
                            
                            elif event.key == "shift+up" or event.key == "shift+down":
                                to_idx = idx - 1 if event.key == "shift+up" else idx + 1
                                if 0 <= to_idx < len(tl._tracks):
                                    from ..database import move_in_playlist
                                    move_in_playlist(self._current_playlist_id, idx, to_idx, is_video=False)
                                    self._load_playlist_tracks(self._current_playlist_id, self._current_playlist_name)
                                    table_tracks.move_cursor(row=to_idx)
                                    self.app.export_playlist_to_m3u(self._current_playlist_id, is_video=False)
                                event.stop()
                    except Exception:
                        pass

    def _after_playlist_created(self, result: bool) -> None:
        if result:
            self.reload_playlists()
            self.app.query_one("Sidebar").refresh_tree()

    def on_track_list_track_selected(self, event: TrackList.TrackSelected) -> None:
        self.app.play_track(event.track, event.tracks, event.index)

    def on_track_list_track_edit_requested(self, event: TrackList.TrackEditRequested) -> None:
        from .music import MusicTrackEditModal
        self.app.push_screen(MusicTrackEditModal(event.track), callback=self._on_track_edit_finished)

    def _on_track_edit_finished(self, result: bool) -> None:
        if result:
            self._load_playlist_tracks(self._current_playlist_id, self._current_playlist_name)

class MusicAlbumEditModal(Widget): # Using Widget to be contained within Screen or used as one
    # But user asked for Screen-like modal, so let's use Screen
    pass

from textual.screen import Screen
from textual.widgets import Input

class MusicAlbumEditModal(Screen):
    DEFAULT_CSS = """
    MusicAlbumEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #music-album-edit-container {
        width: 50;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 0 1;
    }
    #music-album-edit-container Label { text-style: bold; margin-top: 1; height: 1; }
    #music-album-edit-container Input { margin-bottom: 0; border: none; background: $accent 10%; color: $text; padding: 0 1; height: 1; }
    .edit-help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 1; height: 1; }
    """

    def __init__(self, album: dict, **kwargs):
        super().__init__(**kwargs)
        self.album = album

    def compose(self) -> ComposeResult:
        with Vertical(id="music-album-edit-container"):
            yield Label(f"Edit Album: {self.album['album']}")
            yield Label("Artist Name")
            yield Input(value=self.album.get("artist") or "", id="edit-artist")
            yield Label("Release Date")
            yield Input(value=self.album.get("release_date") or "", id="edit-date")
            yield Label("Enter: Save   ESC: Cancel", classes="edit-help")

    def on_mount(self) -> None:
        self.query_one("#edit-artist").focus()

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(False)
            event.stop()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self._save_task()

    @work(thread=True)
    def _save_task(self) -> None:
        new_artist = self.query_one("#edit-artist", Input).value.strip()
        new_date = self.query_one("#edit-date", Input).value.strip()
        
        if not new_artist:
            self.app.call_from_thread(self.app.notify, "Artist name cannot be empty", severity="error")
            return

        try:
            from ..database import update_music_album_metadata, get_connection
            from ..library import write_music_tags
            
            # 1. Update DB (Atomic)
            update_music_album_metadata(self.album["id"], new_artist, new_date)
            
            # 2. Update Files
            failed_files = []
            with get_connection() as conn:
                tracks = conn.execute("SELECT path FROM music_tracks WHERE album_id = ?", (self.album["id"],)).fetchall()
                for row in tracks:
                    path = row["path"]
                    if not write_music_tags(path, {"artist": new_artist, "date": new_date}):
                        failed_files.append(Path(path).name)
            
            if failed_files:
                self.app.call_from_thread(self.app.notify, f"Updated DB, but failed to write tags to {len(failed_files)} files", severity="warning")
            else:
                self.app.call_from_thread(self.app.notify, "Album metadata and file tags updated")
            
            self.app.call_from_thread(self.dismiss, True)
        except Exception as e:
            self.app.call_from_thread(self.app.notify, f"Error: {e}", severity="error")

class MusicTrackEditModal(Screen):
    DEFAULT_CSS = """
    MusicTrackEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #music-track-edit-container {
        width: 50;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 0 1;
    }
    #music-track-edit-container Label { text-style: bold; margin-top: 1; height: 1; }
    #music-track-edit-container Input { margin-bottom: 0; border: none; background: $accent 10%; color: $text; padding: 0 1; height: 1; }
    .edit-help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 1; height: 1; }
    """

    def __init__(self, track: dict, **kwargs):
        super().__init__(**kwargs)
        self.track = track

    def compose(self) -> ComposeResult:
        with Vertical(id="music-track-edit-container"):
            yield Label(f"Edit Track: {Path(self.track['path']).name}")
            yield Label("Track Title")
            yield Input(value=self.track.get("title") or "", id="edit-title")
            yield Label("Artist")
            yield Input(value=self.track.get("artist") or "", id="edit-artist")
            yield Label("Album")
            yield Input(value=self.track.get("album") or "", id="edit-album")
            yield Label("Enter: Save   ESC: Cancel", classes="edit-help")

    def on_mount(self) -> None:
        self.query_one("#edit-title").focus()

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(False)
            event.stop()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self._save_task()

    @work(thread=True)
    def _save_task(self) -> None:
        new_title = self.query_one("#edit-title", Input).value.strip()
        new_artist = self.query_one("#edit-artist", Input).value.strip()
        new_album = self.query_one("#edit-album", Input).value.strip()
        
        if not new_title:
            self.app.call_from_thread(self.app.notify, "Track title cannot be empty", severity="error")
            return

        try:
            from ..database import update_music_track_metadata
            from ..library import write_music_tags
            
            # 1. Update DB
            update_music_track_metadata(self.track["path"], new_title, new_artist, new_album)
            
            # 2. Update File
            if write_music_tags(self.track["path"], {"title": new_title, "artist": new_artist}):
                self.app.call_from_thread(self.app.notify, "Track metadata and file tags updated")
            else:
                self.app.call_from_thread(self.app.notify, "Updated DB, but failed to write tags to file", severity="warning")
                
            self.app.call_from_thread(self.dismiss, True)
        except Exception as e:
            self.app.call_from_thread(self.app.notify, f"Error: {e}", severity="error")

class QueueScreen(Static):
    DEFAULT_CSS = """
    QueueScreen { 
        height: 1fr; 
        padding: 1 2;
        layout: vertical;
        background: transparent;
    }
    #queue-heading { text-style: bold; margin-bottom: 1; }
    #queue-help { color: $text-muted; margin-bottom: 1; }
    #queue-tracks { height: 1fr; border: solid $primary; background: $surface; }
    #queue-tracks:focus-within { border: tall $accent; }
    """

    def compose(self) -> ComposeResult:
        yield Label("Playback Queue", id="queue-heading")
        yield Label("p: Play/Pause  |  d: Remove  |  Shift+Up/Down: Move  |  a: Add to Playlist  |  m: Mark", id="queue-help")
        yield TrackList(id="queue-tracks")

    def on_mount(self) -> None:
        self.reload_queue()
        # Periodically update the playing mark with higher frequency
        self.set_interval(0.2, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            tl = self.query_one("#queue-tracks", TrackList)
            idx = self.app.music_player._current_idx
            is_paused = self.app.music_player._paused
            tl.set_current_index(idx, is_paused)
        except Exception:
            pass

    def reload_queue(self) -> None:
        tracks = self.app.music_player._queue
        try:
            tl = self.query_one("#queue-tracks", TrackList)
            tl.load(tracks)
            tl.set_current_index(self.app.music_player._current_idx, self.app.music_player._paused)
        except Exception:
            pass

    async def _do_play_from_queue(self, idx: int) -> None:
        await self.app.music_player.play_from_queue(idx)
        self._update_playback_status()

    def on_track_list_track_selected(self, event: TrackList.TrackSelected) -> None:
        idx = event.index
        asyncio.create_task(self._do_play_from_queue(idx))
        try:
            tl = self.query_one("#queue-tracks", TrackList)
            tl.set_current_index(idx)
        except Exception:
            pass

    def on_key(self, event) -> None:
        if event.key == "q":
            event.stop()
            return
            
        tl = self.query_one(TrackList)
        from textual.widgets import DataTable
        try:
            table = tl.query_one(DataTable)
        except Exception:
            return
        
        if event.key == "p":
            if self.app.music_player.get_current_track():
                # Already playing, let global toggle_pause handle it
                return
            if table.row_count > 0 and table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    asyncio.create_task(self.app.music_player.play_from_queue(idx))
                    event.stop()
                    return
                except Exception:
                    pass

        elif (event.key == "d" or event.key == "delete") and table.row_count > 0:
            marked = tl._marked_indices
            if not marked and table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    marked = {idx}
                except Exception:
                    pass
            
            if marked:
                from ..widgets.modals import ConfirmModal
                def check_confirm(confirmed: bool) -> None:
                    if confirmed:
                        for idx in sorted(list(marked), reverse=True):
                            self.app.music_player.remove_from_queue(idx)
                        self.reload_queue()
                        self.app.notify(f"Removed {len(marked)} items from queue")
                
                self.app.push_screen(ConfirmModal(f"Remove {len(marked)} items?"), callback=check_confirm)
            event.stop()

        elif event.key == "D": # Shift+d
            if table.row_count > 0:
                from ..widgets.modals import ConfirmModal
                def check_confirm(confirmed: bool) -> None:
                    if confirmed:
                        self.app.music_player.clear_queue()
                        self.reload_queue()
                        self.app.notify("Queue cleared")
                
                self.app.push_screen(ConfirmModal("All Clear?"), callback=check_confirm)
            event.stop()
        
        elif event.key == "shift+up" or event.key == "shift+down":
            if table.cursor_row is not None:
                idx = int(str(table.coordinate_to_cell_key(table.cursor_coordinate).row_key.value))
                to_idx = idx - 1 if event.key == "shift+up" else idx + 1
                if 0 <= to_idx < len(self.app.music_player._queue):
                    self.app.music_player.move_in_queue(idx, to_idx)
                    self.reload_queue()
                    # Ensure cursor follows the moved item
                    self.call_later(lambda: table.move_cursor(row=to_idx))
            event.stop()

class MusicRecentScreen(Widget):
    DEFAULT_CSS = """
    MusicRecentScreen { height: 1fr; padding: 1 2; }
    MusicRecentScreen #recent-heading { text-style: bold; margin-bottom: 1; }
    MusicRecentScreen TrackList { border: solid $primary; }
    """
    def compose(self) -> ComposeResult:
        yield Label("Recently Added Music", id="recent-heading")
        yield TrackList(id="track-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            tl = self.query_one("#track-list", TrackList)
            track = self.app.music_player.get_current_track()
            path = track["path"] if track else None
            is_paused = self.app.music_player._paused
            tl.set_current_index_by_path(path, is_paused)
        except Exception: pass

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection
        with get_connection() as conn:
            tracks = [dict(row) for row in conn.execute(
                "SELECT * FROM music_tracks ORDER BY created_at DESC LIMIT 50"
            ).fetchall()]
        self.app.call_from_thread(self._populate, tracks)

    def _populate(self, tracks: list[dict]) -> None:
        self.query_one(TrackList).load(tracks)

    def on_track_list_track_selected(self, event: TrackList.TrackSelected) -> None:
        self.app.music_player.play_track(event.track, event.tracks, event.index)

class MusicFilterScreen(Widget):
    DEFAULT_CSS = """
    MusicFilterScreen { height: 1fr; padding: 1 2; }
    MusicFilterScreen #filter-heading { text-style: bold; margin-bottom: 1; }
    MusicFilterScreen TrackList { border: solid $primary; }
    """
    def __init__(self, filter_id: int, filter_name: str, **kwargs):
        super().__init__(**kwargs)
        self.filter_id = filter_id
        self.filter_name = filter_name

    def compose(self) -> ComposeResult:
        yield Label(f"Music View: {self.filter_name}", id="filter-heading")
        yield Label("n: New View | e: Edit View | d: Delete View | q: Add All to Queue", classes="playlist-help")
        yield TrackList(id="track-list")

    def on_mount(self) -> None:
        self._load()
        self.set_interval(0.5, self._update_playback_status)

    def _update_playback_status(self) -> None:
        try:
            tl = self.query_one("#track-list", TrackList)
            curr = self.app.music_player.get_current_track()
            if not curr:
                tl.set_current_index(-1)
                return
            is_paused = self.app.music_player._paused
            tl.set_current_index_by_path(curr["path"], is_paused)
        except Exception: pass

    @work(thread=True)
    def _load(self) -> None:
        from ..database import get_connection, get_filtered_tracks
        with get_connection() as conn:
            f = conn.execute("SELECT * FROM music_filters WHERE id = ?", (self.filter_id,)).fetchone()
        
        if f:
            tracks = get_filtered_tracks(f["conditions_json"], f["sort_json"])
        else:
            tracks = []
        
        self.app.call_from_thread(self._populate, tracks)

    def _populate(self, tracks: list[dict]) -> None:
        self.query_one(TrackList).load(tracks)

    def on_track_list_track_selected(self, event: TrackList.TrackSelected) -> None:
        self.app.music_player.play_track(event.track, event.tracks, event.index)

    def on_key(self, event) -> None:
        if event.key == "n":
            from ..widgets.modals import MusicFilterEditModal
            self.app.push_screen(MusicFilterEditModal(), callback=self._after_view_edited)
            event.stop()
        elif event.key == "e":
            from ..database import get_connection
            with get_connection() as conn:
                f = conn.execute("SELECT * FROM music_filters WHERE id = ?", (self.filter_id,)).fetchone()
            if f:
                from ..widgets.modals import MusicFilterEditModal
                self.app.push_screen(MusicFilterEditModal(dict(f)), callback=self._after_view_edited)
            event.stop()
        elif event.key == "d":
            from ..widgets.modals import ConfirmModal
            def check_confirm(confirmed: bool) -> None:
                if confirmed:
                    from ..database import delete_music_filter
                    delete_music_filter(self.filter_id)
                    self.app.notify(f"View '{self.filter_name}' deleted")
                    try:
                        from ..app import Sidebar
                        self.app.query_one(Sidebar).refresh_tree()
                    except Exception: pass
                    self.app.switch_screen("music")
            self.app.push_screen(ConfirmModal(f"Delete view '{self.filter_name}'?"), callback=check_confirm)
            event.stop()
        elif event.key == "q":
            tl = self.query_one("#track-list", TrackList)
            if tl._tracks:
                self.app.music_player.add_to_queue(tl._tracks)
                self.app.notify(f"Added {len(tl._tracks)} tracks to queue")
            event.stop()

    def _after_view_edited(self, result: bool) -> None:
        if result:
            self._load()
            try:
                from ..app import Sidebar
                self.app.query_one(Sidebar).refresh_tree()
            except Exception: pass
