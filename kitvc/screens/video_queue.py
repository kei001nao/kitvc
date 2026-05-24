from textual.widgets import Label, Static
from textual.app import ComposeResult
from textual.containers import Vertical
from ..widgets.media_lists import VideoList
import asyncio

class VideoQueueScreen(Static):
    DEFAULT_CSS = """
    VideoQueueScreen { 
        height: 1fr; 
        padding: 1 2;
        layout: vertical;
        background: transparent;
    }
    #video-queue-heading { text-style: bold; margin-bottom: 1; }
    #video-queue-help { color: $text-muted; margin-bottom: 1; }
    #video-queue-list { height: 1fr; border: solid $primary; background: $surface; }
    #video-queue-list:focus-within { border: tall $accent; }
    """

    def compose(self) -> ComposeResult:
        yield Label("Video Playback Queue", id="video-queue-heading")
        yield Label("p: Play/Pause  |  d: Remove  |  D: Clear Queue  |  Shift+Up/Down: Move  |  m/Space: Mark", id="video-queue-help")
        yield VideoList(id="video-queue-list")

    def on_mount(self) -> None:
        self.reload_queue()
        self.set_interval(0.2, self._update_playback_status)

    def reload_queue(self) -> None:
        videos = self.app.video_player._queue
        try:
            vl = self.query_one("#video-queue-list", VideoList)
            vl.load(videos)
            vl.set_current_index(self.app.video_player._current_idx, self.app.video_player._paused)
        except Exception:
            pass

    def _update_playback_status(self) -> None:
        try:
            vl = self.query_one("#video-queue-list", VideoList)
            idx = self.app.video_player._current_idx
            is_paused = self.app.video_player._paused
            vl.set_current_index(idx, is_paused)
        except Exception:
            pass

    async def _do_play_from_queue(self, idx: int) -> None:
        await self.app.video_player.play_from_queue(idx)
        self._update_playback_status()

    def on_video_list_video_selected(self, event: VideoList.VideoSelected) -> None:
        idx = event.index
        asyncio.create_task(self._do_play_from_queue(idx))

    def on_key(self, event) -> None:
        if event.key == "q":
            event.stop()
            return
            
        vl = self.query_one("#video-queue-list", VideoList)
        from textual.widgets import DataTable
        try:
            table = vl.query_one(DataTable)
        except Exception:
            return
        
        if event.key == "p":
            if self.app.video_player.get_current_track():
                return
            if table.row_count > 0 and table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    asyncio.create_task(self._do_play_from_queue(idx))
                    event.stop()
                    return
                except Exception:
                    pass

        elif (event.key == "d" or event.key == "delete") and table.row_count > 0:
            marked = vl._marked_indices
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
                            self.app.video_player.remove_from_queue(idx)
                        self.reload_queue()
                        self.app.notify(f"Removed {len(marked)} items from queue")
                self.app.push_screen(ConfirmModal(f"Remove {len(marked)} items?"), callback=check_confirm)
            event.stop()

        elif event.key == "D":
            if table.row_count > 0:
                from ..widgets.modals import ConfirmModal
                def check_confirm(confirmed: bool) -> None:
                    if confirmed:
                        self.app.video_player.clear_queue()
                        self.reload_queue()
                        self.app.notify("Video queue cleared")
                self.app.push_screen(ConfirmModal("All Clear?"), callback=check_confirm)
            event.stop()
        
        elif event.key == "shift+up" or event.key == "shift+down":
            if table.cursor_row is not None:
                try:
                    row_key = table.coordinate_to_cell_key(table.cursor_coordinate).row_key
                    idx = int(str(row_key.value))
                    to_idx = idx - 1 if event.key == "shift+up" else idx + 1
                    if 0 <= to_idx < len(self.app.video_player._queue):
                        self.app.video_player.move_in_queue(idx, to_idx)
                        self.reload_queue()
                        table.move_cursor(row=to_idx)
                except Exception:
                    pass
            event.stop()


