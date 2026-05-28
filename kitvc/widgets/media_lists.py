from textual.app import ComposeResult
from textual.message import Message
from textual.widget import Widget
from textual.widgets import DataTable

class TrackList(Widget):
    DEFAULT_CSS = """
    TrackList { height: 1fr; background: $surface; } 
    TrackList DataTable { height: 1fr; background: transparent; }
    TrackList DataTable .marked-row { background: $accent 20%; }
    """

    class TrackSelected(Message):
        def __init__(self, track, index: int, tracks: list) -> None:
            super().__init__()
            self.track = track
            self.index = index
            self.tracks = tracks

    class TrackEditRequested(Message):
        def __init__(self, track) -> None:
            super().__init__()
            self.track = track

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._tracks = []
        self._marked_indices = set()
        self._current_index = -1
        self._last_is_paused = False

    def compose(self) -> ComposeResult:
        yield DataTable(cursor_type="row", zebra_stripes=True)

    def on_mount(self) -> None:
        table = self.query_one(DataTable)
        table.add_column("M", key="mark")
        table.add_column("#", key="num")
        table.add_column("Alb#", key="track_num")
        table.add_column("Title", key="title")
        table.add_column("Artist", key="artist")
        table.add_column("Album", key="album")
        table.add_column("Time", key="time")
        if self._tracks:
            self.load(self._tracks)

    def set_current_index(self, index: int, is_paused: bool = False) -> None:
        # Only update if index or paused state changed
        if self._current_index == index and getattr(self, "_last_is_paused", None) == is_paused:
            return

        table = self.query_one(DataTable)
        
        # 1. Update the previous current index to remove the play/pause mark
        if self._current_index != -1 and self._current_index < len(self._tracks):
            try:
                row_key = str(self._current_index)
                mark = "●" if self._current_index in self._marked_indices else " "
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass
        
        # 2. Update the new current index to show the play/pause mark
        self._current_index = index
        self._last_is_paused = is_paused
        
        if self._current_index != -1 and self._current_index < len(self._tracks):
            try:
                row_key = str(self._current_index)
                mark = "■" if is_paused else "▶"
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass

    def load(self, tracks: list) -> None:
        self._tracks = tracks
        self._marked_indices.clear()
        self._current_index = -1
        self._last_is_paused = False
        
        tables = self.query(DataTable)
        if not tables:
            return
            
        table = tables.first()
        table.clear()
        for i, t in enumerate(tracks):
            dur = t.get("duration", 0)
            m, s = divmod(dur, 60)
            mark = " "
            if i == self._current_index:
                mark = "▶"
            elif i in self._marked_indices:
                mark = "●"
            
            table.add_row(
                mark,
                str(i + 1),
                str(t.get("track_num") or "-"),
                t.get("title", "?"),
                t.get("artist", "–"),
                t.get("album", "–"),
                f"{m}:{s:02d}",
                key=str(i)
            )

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        try:
            idx = int(str(event.row_key.value))
            track = self._tracks[idx]
            self.post_message(self.TrackSelected(track, idx, self._tracks))
        except (ValueError, IndexError):
            pass

    def get_marked_tracks(self) -> list[dict]:
        if not self._marked_indices:
            table = self.query_one(DataTable)
            if table.cursor_row is not None and table.cursor_row >= 0:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    return [self._tracks[idx]]
                except Exception:
                    return []
            return []
        return [self._tracks[i] for i in sorted(list(self._marked_indices))]

    def on_key(self, event) -> None:
        table = self.query_one(DataTable)
        if event.key == "m" or event.key == "space":
            if table.cursor_row is not None:
                row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                idx = int(str(row_key.value))
                if idx in self._marked_indices:
                    self._marked_indices.remove(idx)
                    table.update_cell(row_key, "mark", " ")
                else:
                    self._marked_indices.add(idx)
                    table.update_cell(row_key, "mark", "●")
                
                if table.row_count > 1:
                    next_row = (table.cursor_row + 1) % table.row_count
                    table.move_cursor(row=next_row)
            event.stop()
        elif event.key == "a":
            tracks = self.get_marked_tracks()
            if tracks:
                self.app.show_playlist_add_dialog([t["path"] for t in tracks])
            event.stop()
        elif event.key == "ctrl+e":
            if table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    self.post_message(self.TrackEditRequested(self._tracks[idx]))
                except Exception:
                    pass
            event.stop()
        elif event.key == "q":
            from ..screens.music import QueueScreen
            parent = self.parent
            while parent:
                if isinstance(parent, QueueScreen):
                    return
                parent = parent.parent
            
            tracks = self.get_marked_tracks()
            if tracks:
                self.app.player.add_to_queue(tracks)
                self.app.notify(f"Added {len(tracks)} tracks to queue")
            event.stop()

class VideoList(Widget):
    DEFAULT_CSS = """
    VideoList { height: 1fr; background: $surface; } 
    VideoList DataTable { height: 1fr; background: transparent; }
    VideoList DataTable .marked-row { background: $accent 20%; }
    """

    class VideoSelected(Message):
        def __init__(self, video, index: int, videos: list) -> None:
            super().__init__()
            self.video = video
            self.index = index
            self.videos = videos

    class VideoEditRequested(Message):
        def __init__(self, video) -> None:
            super().__init__()
            self.video = video

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._videos = []
        self._marked_indices = set()
        self._current_index = -1
        self._last_is_paused = False

    def compose(self) -> ComposeResult:
        yield DataTable(cursor_type="row", zebra_stripes=True)

    def on_mount(self) -> None:
        table = self.query_one(DataTable)
        table.add_column("M", key="mark")
        table.add_column("Title", key="title")
        table.add_column("Series", key="series")
        table.add_column("S", key="season")
        table.add_column("E", key="episode")
        table.add_column("Category", key="category")
        table.add_column("Type", key="type")
        table.add_column("Filename", key="filename")
        table.add_column("Size", key="size")
        if self._videos:
            self.load(self._videos)

    def set_current_index_by_path(self, path: str | None, is_paused: bool = False) -> None:
        target_idx = -1
        if path:
            norm_path = os.path.normpath(path)
            for i, v in enumerate(self._videos):
                if os.path.normpath(v["path"]) == norm_path:
                    target_idx = i
                    break
        
        if self._current_index == target_idx and getattr(self, "_last_is_paused", None) == is_paused:
            return

        table = self.query_one(DataTable)
        if self._current_index != -1 and self._current_index < len(self._videos):
            try:
                row_key = str(self._current_index)
                mark = "●" if self._current_index in self._marked_indices else " "
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass
        
        self._current_index = target_idx
        self._last_is_paused = is_paused
        
        if self._current_index != -1 and self._current_index < len(self._videos):
            try:
                row_key = str(self._current_index)
                mark = "■" if is_paused else "▶"
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass

    def set_current_index(self, index: int, is_paused: bool = False) -> None:
        if self._current_index == index and getattr(self, "_last_is_paused", None) == is_paused:
            return

        table = self.query_one(DataTable)
        if self._current_index != -1 and self._current_index < len(self._videos):
            try:
                row_key = str(self._current_index)
                mark = "●" if self._current_index in self._marked_indices else " "
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass
        
        self._current_index = index
        self._last_is_paused = is_paused
        
        if self._current_index != -1 and self._current_index < len(self._videos):
            try:
                row_key = str(self._current_index)
                mark = "■" if is_paused else "▶"
                table.update_cell(row_key, "mark", mark)
            except Exception:
                pass

    def load(self, videos: list) -> None:
        self._videos = videos
        self._marked_indices.clear()
        self._current_index = -1
        self._last_is_paused = False
        
        tables = self.query(DataTable)
        if not tables:
            return
            
        table = tables.first()
        table.clear()
        for i, v in enumerate(videos):
            size_mb = v.get("size", 0) / (1024 * 1024)
            mark = " "
            if i == self._current_index:
                mark = "▶"
            elif i in self._marked_indices:
                mark = "●"
                
            table.add_row(
                mark,
                v.get("title") or "",
                v.get("series") or "",
                str(v.get("season") or ""),
                str(v.get("episode") or ""),
                v.get("category") or "",
                v.get("type") or "",
                v.get("filename", ""),
                f"{size_mb:.1f} MB",
                key=str(i)
            )

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        idx = int(str(event.row_key.value))
        video = self._videos[idx]
        self.post_message(self.VideoSelected(video, idx, self._videos))

    def get_marked_videos(self) -> list[dict]:
        if not self._marked_indices:
            table = self.query_one(DataTable)
            if table.cursor_row is not None and table.cursor_row >= 0:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    return [self._videos[idx]]
                except Exception:
                    return []
            return []
        return [self._videos[i] for i in sorted(list(self._marked_indices))]

    def on_key(self, event) -> None:
        table = self.query_one(DataTable)
        if event.key == "m" or event.key == "space":
            if table.cursor_row is not None:
                row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                idx = int(str(row_key.value))
                if idx in self._marked_indices:
                    self._marked_indices.remove(idx)
                    table.update_cell(row_key, "mark", " ")
                else:
                    self._marked_indices.add(idx)
                    table.update_cell(row_key, "mark", "●")
                
                if table.row_count > 1:
                    next_row = (table.cursor_row + 1) % table.row_count
                    table.move_cursor(row=next_row)
            event.stop()
        elif event.key == "e":
            if table.cursor_row is not None:
                row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                idx = int(str(row_key.value))
                self.post_message(self.VideoEditRequested(self._videos[idx]))
            event.stop()
        elif event.key == "a":
            videos = self.get_marked_videos()
            if videos:
                self.app.show_playlist_add_dialog([v["path"] for v in videos], is_video=True)
            event.stop()
        elif event.key == "q":
            videos = self.get_marked_videos()
            if videos:
                self.app.video_player.add_to_queue(videos)
                self.app.notify(f"Added {len(videos)} videos to queue")
            event.stop()
