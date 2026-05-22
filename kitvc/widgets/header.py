from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Label, ProgressBar
from textual.containers import Horizontal, Vertical

HEADER_LOGO = """█▄▀ █ ▀█▀ █ █ █▀
█ █ █  █  ╚▄▀ █▄"""

class Header(Widget):
    DEFAULT_CSS = """
    Header {
        height: 4;
        background: $surface;
        border-bottom: heavy $primary;
        padding: 0 2;
    }
    #header-info {
        height: 2;
        align: left middle;
    }
    #header-playback {
        width: 1fr;
        height: 2;
        color: $text;
        text-align: right;
        content-align: right middle;
    }
    #header-progress {
        height: 1;
        margin-top: 0;
    }
    #header-progress Bar {
        width: 1fr;
        color: $accent;
    }
    #header-progress Bar > .bar--complete {
        color: $accent;
    }
    #header-progress Bar > .bar--pending {
        color: $accent 20%;
    }
    """

    def compose(self) -> ComposeResult:
        with Vertical():
            with Horizontal(id="header-info"):
                yield Label("Nothing playing", id="header-playback")
            yield ProgressBar(id="header-progress", show_eta=False, show_percentage=False)

    def update_info(self, title: str, artist: str, position: float, duration: float, volume: int, queue_pos: str = "") -> None:
        playback_label = self.query_one("#header-playback", Label)
        
        cur_m, cur_s = divmod(int(position), 60)
        dur_m, dur_s = divmod(int(duration), 60)
        time_str = f"{cur_m:02d}:{cur_s:02d} / {dur_m:02d}:{dur_s:02d}"
        
        info_parts = []
        if queue_pos:
            info_parts.append(f"[{queue_pos}]")
        
        track_info = f"{title}"
        if artist:
            track_info += f" - {artist}"
        info_parts.append(track_info)
        info_parts.append(time_str)
        info_parts.append(f"Vol: {volume}%")
        
        playback_label.update("  |  ".join(info_parts))

        pb = self.query_one("#header-progress", ProgressBar)
        if duration > 0:
            pb.total = duration
            pb.progress = position
        else:
            pb.total = 100
            pb.progress = 0

    def clear(self) -> None:
        self.query_one("#header-playback", Label).update("Nothing playing")
        pb = self.query_one("#header-progress", ProgressBar)
        pb.total = 100
        pb.progress = 0
