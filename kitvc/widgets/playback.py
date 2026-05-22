from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Label, ProgressBar, Static
from textual.containers import Horizontal, Vertical

class PlaybackControl(Widget):
    DEFAULT_CSS = """
    PlaybackControl {
        height: 3;
        background: $surface;
        border-top: heavy $primary;
        padding: 0 2;
    }
    #playback-info {
        height: 1;
    }
    #playback-title {
        width: 1fr;
        color: $text;
        text-style: bold;
    }
    #playback-time {
        width: auto;
        color: $text;
    }
    #playback-progress {
        height: 1;
        margin-top: 0;
    }
    #playback-progress Bar {
        width: 1fr;
        color: $primary;
    }
    #playback-progress Bar > .bar--complete {
        color: $primary;
    }
    #playback-progress Bar > .bar--pending {
        color: $primary 20%;
    }
    Label {
        color: $text;
    }
    """

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._duration = 0
        self._position = 0

    def compose(self) -> ComposeResult:
        with Vertical():
            with Horizontal(id="playback-info"):
                yield Label("Nothing playing", id="playback-title")
                yield Label("--:-- / --:--", id="playback-time")
            yield ProgressBar(id="playback-progress", show_eta=False, show_percentage=False)

    def update_info(self, title: str, artist: str, position: float, duration: float) -> None:
        self._position = position
        self._duration = duration

        title_label = self.query_one("#playback-title", Label)
        if artist:
            title_label.update(f"{title} - {artist}")
        else:
            title_label.update(title)

        time_label = self.query_one("#playback-time", Label)
        cur_m, cur_s = divmod(int(position), 60)
        dur_m, dur_s = divmod(int(duration), 60)
        time_label.update(f"{cur_m:02d}:{cur_s:02d} / {dur_m:02d}:{dur_s:02d}")

        pb = self.query_one("#playback-progress", ProgressBar)
        if duration > 0:
            pb.total = duration
            pb.progress = position
        else:
            pb.total = 100
            pb.progress = 0

    def clear(self) -> None:
        self.query_one("#playback-title", Label).update("Nothing playing")
        self.query_one("#playback-time", Label).update("--:-- / --:--")
        pb = self.query_one("#playback-progress", ProgressBar)
        pb.total = 100
        pb.progress = 0
