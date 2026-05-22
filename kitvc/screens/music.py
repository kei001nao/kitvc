from textual import work
from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Label, DataTable, Static
from textual.containers import Vertical
from ..database import get_connection
from ..widgets.media_lists import TrackList
import asyncio

class MusicLibraryScreen(Widget):
    DEFAULT_CSS = """
    MusicLibraryScreen { height: 1fr; padding: 1 2; layout: vertical; }
    MusicLibraryScreen Label { text-style: bold; margin-bottom: 0; }
    MusicLibraryScreen #music-heading { margin-bottom: 1; }
    MusicLibraryScreen .playlist-help { color: $text-muted; margin-bottom: 1; }
    MusicLibraryScreen DataTable { height: 1fr; background: $surface; border: solid $primary; }
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
    MusicArtistScreen DataTable#music-albums { height: 8; background: $surface; border: solid $primary; margin-bottom: 1; }
    MusicArtistScreen TrackList { height: 1fr; border: solid $primary; background: $surface; }
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
        yield DataTable(id="music-albums", cursor_type="row")
        yield Label("Tracks")
        yield Label("m/Space: Mark  |  a: Add to Playlist  |  q: Add to Queue  |  Enter: Play", classes="playlist-help")
        yield TrackList(id="music-tracks")

    def on_mount(self) -> None:
        table = self.query_one("#music-albums", DataTable)
        table.add_column("Year", key="year", width=6)
        table.add_column("Album", key="album")
        self._load_albums()

    @work(thread=True)
    def _load_albums(self) -> None:
        with get_connection() as conn:
            albums = [dict(row) for row in conn.execute(
                "SELECT DISTINCT album, year FROM music_tracks WHERE artist = ? ORDER BY year DESC, album", 
                (self._artist_name,)
            ).fetchall()]
        self.app.call_from_thread(self._populate_albums, albums)

    def _populate_albums(self, albums: list[dict]) -> None:
        self._albums_list = albums
        table = self.query_one("#music-albums", DataTable)
        table.clear()
        self._current_album_idx = -1
        for i, album in enumerate(albums):
            year = str(album['year']) if album['year'] else ""
            table.add_row(year, album['album'], key=str(i))
        if albums:
            self._current_album_idx = 0
            self._load_tracks(albums[0]['album'])
            table.move_cursor(row=0)

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        try:
            # ONLY handle events from the album selector
            if event.control.id != "music-albums":
                return

            idx = int(str(event.row_key.value))
            if getattr(self, "_current_album_idx", -1) == idx:
                return
            
            self._current_album_idx = idx
            album_name = self._albums_list[idx]['album']
            self._load_tracks(album_name)
        except (ValueError, IndexError):
            pass

    def on_key(self, event) -> None:
        if event.key == "q":
            table = self.query_one("#music-albums", DataTable)
            if table.has_focus and table.cursor_row is not None:
                try:
                    idx = int(str(table.coordinate_to_cell_key(table.cursor_coordinate).row_key.value))
                    album_name = self._albums_list[idx]['album']
                    self._add_album_to_queue(album_name)
                except Exception:
                    pass
        elif event.key == "a":
            table = self.query_one("#music-albums", DataTable)
            if table.has_focus and table.cursor_row is not None:
                if self._current_album_tracks:
                    self.app.show_playlist_add_dialog([t["path"] for t in self._current_album_tracks])

    @work(thread=True)
    def _add_album_to_queue(self, album_name: str) -> None:
        with get_connection() as conn:
            tracks = [dict(row) for row in conn.execute(
                "SELECT * FROM music_tracks WHERE artist = ? AND album = ? ORDER BY disc_num, track_num",
                (self._artist_name, album_name)
            ).fetchall()]
        for t in tracks: t["is_video"] = False
        self.app.player.add_to_queue(tracks)
        self.app.notify(f"Added album '{album_name}' to queue")

    @work(thread=True)
    def _load_tracks(self, album_name: str) -> None:
        with get_connection() as conn:
            tracks = [dict(row) for row in conn.execute(
                "SELECT * FROM music_tracks WHERE artist = ? AND album = ? ORDER BY disc_num, track_num",
                (self._artist_name, album_name)
            ).fetchall()]
        self._current_album_tracks = tracks
        self.app.call_from_thread(self.query_one(TrackList).load, tracks)

class MusicPlaylistScreen(Widget):
    DEFAULT_CSS = """
    MusicPlaylistScreen { height: 1fr; padding: 1 2; layout: vertical; }
    MusicPlaylistScreen Label { text-style: bold; margin-bottom: 0; }
    #playlist-selector { height: 8; border: solid $primary; margin-bottom: 1; background: $surface; }
    #playlist-tracks-container { height: 1fr; border: solid $primary; background: $surface; }
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
                    self.app.player.add_to_queue(tl._tracks)
                    self.app.notify(f"Added all tracks from playlist to queue")
                event.stop()
        
        elif tl_focused:
            if event.key == "q":
                self.app.player.add_to_queue(tl._tracks)
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
        # Disable Enter to play in playlist screen
        pass

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
            idx = self.app.player._current_idx
            is_paused = self.app.player._paused
            tl.set_current_index(idx, is_paused)
        except Exception:
            pass

    def reload_queue(self) -> None:
        tracks = self.app.player._queue
        try:
            tl = self.query_one("#queue-tracks", TrackList)
            tl.load(tracks)
            tl.set_current_index(self.app.player._current_idx, self.app.player._paused)
        except Exception:
            pass

    async def _do_play_from_queue(self, idx: int) -> None:
        await self.app.player.play_from_queue(idx)
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
            if self.app.player.get_current_track():
                # Already playing, let global toggle_pause handle it
                return
            if table.row_count > 0 and table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    asyncio.create_task(self.app.player.play_from_queue(idx))
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
                            self.app.player.remove_from_queue(idx)
                        self.reload_queue()
                        self.app.notify(f"Removed {len(marked)} items from queue")
                
                self.app.push_screen(ConfirmModal(f"Remove {len(marked)} items?"), callback=check_confirm)
            event.stop()

        elif event.key == "D": # Shift+d
            if table.row_count > 0:
                from ..widgets.modals import ConfirmModal
                def check_confirm(confirmed: bool) -> None:
                    if confirmed:
                        self.app.player.clear_queue()
                        self.reload_queue()
                        self.app.notify("Queue cleared")
                
                self.app.push_screen(ConfirmModal("All Clear?"), callback=check_confirm)
            event.stop()
        
        elif event.key == "shift+up" or event.key == "shift+down":
            if table.cursor_row is not None:
                idx = int(str(table.coordinate_to_cell_key(table.cursor_coordinate).row_key.value))
                to_idx = idx - 1 if event.key == "shift+up" else idx + 1
                if 0 <= to_idx < len(self.app.player._queue):
                    self.app.player.move_in_queue(idx, to_idx)
                    self.reload_queue()
                    table.move_cursor(row=to_idx)
            event.stop()
