from textual import work
from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Label, DirectoryTree, DataTable, Input, RadioSet, RadioButton, Button
from textual.containers import Vertical, Horizontal
from pathlib import Path

class VideoFetchModal(Screen):
    DEFAULT_CSS = """
    VideoFetchModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #fetch-container {
        width: 70;
        height: 90%;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #fetch-container Label { margin-top: 1; height: 1; text-style: bold; }
    #fetch-container Input { margin-bottom: 0; border: none; background: $accent 10%; color: $text; padding: 0 1; height: 1; }
    #tv-params-container { height: auto; margin-top: 0; }
    #tv-params-container Vertical { width: 10; margin-right: 2; height: auto; }
    #fetch-container RadioSet { background: transparent; border: none; padding: 0; margin: 0; height: 3; layout: horizontal; }
    #fetch-container RadioButton { margin-right: 2; padding: 0; }
    #fetch-container DataTable#lang-list { height: 6; background: transparent; border: none; margin-top: 1; }
    #fetch-container DataTable#result-list { height: 1fr; background: transparent; border: solid $primary; margin-top: 1; }
    #fetch-container Button#search-btn { margin-top: 1; width: 100%; background: $accent; color: $text; }
    .help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 1; height: 1; }
    """
    def __init__(self, query: str, is_tv: bool = True, language: str = "ja", season: int = None, episode: int = None, **kwargs):
        super().__init__(**kwargs)
        self.search_query = query
        self.is_tv = is_tv
        self.language = language
        self.season = season
        self.episode = episode
        self._langs = [
            ("ja", "Japanese (日本語)"),
            ("en", "English (英語)"),
            ("ko", "Korean (韓国語)"),
            ("zh", "Chinese (中国語)"),
        ]
        self._results = []

    def compose(self) -> ComposeResult:
        with Vertical(id="fetch-container"):
            yield Label("Search Query (Series):")
            yield Input(value=self.search_query, id="search-query")
            
            with Horizontal(id="tv-params-container"):
                with Vertical():
                    yield Label("S:")
                    yield Input(value=str(self.season or ""), id="fetch-season", restrict=r"[0-9]*")
                with Vertical():
                    yield Label("E:")
                    yield Input(value=str(self.episode or ""), id="fetch-episode", restrict=r"[0-9]*")

            yield Label("Media Type:")
            with RadioSet(id="media-type"):
                yield RadioButton("TV Show", value=self.is_tv)
                yield RadioButton("Movie", value=not self.is_tv)
            
            yield Label("Language:")
            yield DataTable(id="lang-list", cursor_type="row")
            
            yield Button("Search TMDB", id="search-btn")
            
            yield Label("Results (Select and Enter to Apply):")
            yield DataTable(id="result-list", cursor_type="row")
            
            yield Label("Enter: Select Result   Tab: Switch Focus   ESC: Cancel", classes="help")

    def on_mount(self) -> None:
        table_lang = self.query_one("#lang-list", DataTable)
        table_lang.add_column("Code")
        table_lang.add_column("Language")
        
        selected_row = 0
        for i, (code, name) in enumerate(self._langs):
            table_lang.add_row(code, name)
            if code == self.language:
                selected_row = i
        table_lang.move_cursor(row=selected_row)
        
        table_res = self.query_one("#result-list", DataTable)
        table_res.add_column("Title")
        table_res.add_column("Year")
        table_res.add_column("Overview")
        
        self.query_one("#search-query").focus()

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self._perform_search()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "search-btn":
            self._perform_search()

    @work(thread=True)
    def _perform_search(self) -> None:
        query = self.query_one("#search-query", Input).value.strip()
        if not query:
            return
            
        radio_set = self.query_one("#media-type", RadioSet)
        is_tv = (radio_set.pressed_index == 0)
        
        table_lang = self.query_one("#lang-list", DataTable)
        lang = self._langs[table_lang.cursor_row][0]
        
        self.app.call_from_thread(self.app.notify, f"Searching for '{query}'...")
        
        from ..metadata_video import search_videos
        results = search_videos(query, is_tv=is_tv, language=lang)
        
        self.app.call_from_thread(self._populate_results, results)

    def _populate_results(self, results: list[dict]) -> None:
        self._results = results
        table = self.query_one("#result-list", DataTable)
        table.clear()
        for i, res in enumerate(results):
            table.add_row(res["title"], res["year"], res["overview"] or "", key=str(i))
        
        if results:
            table.move_cursor(row=0)
            table.focus()
            self.app.notify(f"Found {len(results)} matches")
        else:
            self.app.notify("No matches found", severity="warning")

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        if event.control.id == "result-list":
            idx = int(str(event.row_key.value))
            res = self._results[idx]
            
            radio_set = self.query_one("#media-type", RadioSet)
            is_tv = (radio_set.pressed_index == 0)
            
            table_lang = self.query_one("#lang-list", DataTable)
            lang = self._langs[table_lang.cursor_row][0]
            
            # Get S/E if provided
            try:
                s_val = self.query_one("#fetch-season", Input).value
                e_val = self.query_one("#fetch-episode", Input).value
                season = int(s_val) if s_val else None
                episode = int(e_val) if e_val else None
            except ValueError:
                season = episode = None

            self.dismiss({
                "tmdb_id": res["id"],
                "is_tv": is_tv,
                "language": lang,
                "query": self.query_one("#search-query", Input).value,
                "season": season,
                "episode": episode
            })

class VideoScanChoiceModal(Screen):
    DEFAULT_CSS = """
    VideoScanChoiceModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #choice-container {
        width: 50;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #choice-container Label { width: 100%; text-align: center; margin-bottom: 1; }
    #choice-container DataTable { height: 6; background: transparent; border: none; }
    #choice-container .help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 1; }
    """
    def __init__(self, target_count: int, **kwargs):
        super().__init__(**kwargs)
        self.target_count = target_count

    def compose(self) -> ComposeResult:
        with Vertical(id="choice-container"):
            yield Label(f"Scan {self.target_count} video(s) for info:")
            yield DataTable(id="choice-list", cursor_type="row")
            yield Label("Enter: Select   ESC: Cancel", classes="help")

    def on_mount(self) -> None:
        table = self.query_one(DataTable)
        table.add_column("Scan Method")
        table.add_row("1. By Filename (Auto-parse S/E)")
        table.add_row("2. By Metadata (Use DB Series/S/E)")
        table.focus()

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        idx = event.cursor_row
        if idx == 0:
            self.dismiss("filename")
        elif idx == 1:
            self.dismiss("metadata")

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()

class ConfirmModal(Screen):
    DEFAULT_CSS = """
    ConfirmModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #confirm-container {
        width: 40;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #confirm-container Label { width: 100%; text-align: center; margin-bottom: 1; }
    #confirm-container .playlist-help { width: 100%; text-align: center; background: $primary; color: $text; }
    """
    def __init__(self, message: str, prompt: str = "Y: Confirmed  N: Cancel", **kwargs):
        super().__init__(**kwargs)
        self.message = message
        self.prompt = prompt

    def compose(self) -> ComposeResult:
        with Vertical(id="confirm-container"):
            yield Label(self.message)
            yield Label(self.prompt, classes="playlist-help")

    def on_key(self, event) -> None:
        if event.key.lower() == "y":
            self.dismiss(True)
        elif event.key.lower() == "n" or event.key == "escape":
            self.dismiss(False)
        event.stop()

class QuitModal(Screen):
    DEFAULT_CSS = """
    QuitModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #quit-container {
        width: 40;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #quit-container Label { width: 100%; text-align: center; margin-bottom: 1; }
    #quit-container .playlist-help { width: 100%; text-align: center; background: $primary; color: $text; }
    """
    def compose(self) -> ComposeResult:
        with Vertical(id="quit-container"):
            yield Label("Quit kitvc?")
            yield Label("y: Confirmed   n: Cancel", classes="playlist-help")

    def on_key(self, event) -> None:
        if event.key == "y":
            self.app.exit()
        elif event.key == "n" or event.key == "escape":
            self.dismiss()
        event.stop()

class FileSelectModal(Screen):
    DEFAULT_CSS = """
    FileSelectModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #file-select-container {
        width: 80%;
        height: 80%;
        border: panel $primary;
        background: $background;
        padding: 1;
    }
    #file-select-container Label { width: 100%; text-align: center; margin-bottom: 1; }
    #file-select-container DirectoryTree {
        border: solid $primary;
        height: 1fr;
        background: transparent;
    }
    #file-select-container .file-help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 1; }
    """
    def __init__(self, initial_dir: str = ".", pattern: str = "*", **kwargs):
        super().__init__(**kwargs)
        self.initial_dir = initial_dir
        self.pattern = pattern

    def compose(self) -> ComposeResult:
        with Vertical(id="file-select-container"):
            yield Label(f"Select M3U File ({self.pattern})")
            yield DirectoryTree(self.initial_dir, id="file-tree")
            yield Label("Enter: Select   ESC: Cancel", classes="file-help")

    def on_mount(self) -> None:
        self.query_one(DirectoryTree).focus()

    def on_directory_tree_file_selected(self, event: DirectoryTree.FileSelected) -> None:
        path = event.path
        if path.match(self.pattern):
            self.dismiss(str(path))
        else:
            self.app.notify(f"Please select a file matching {self.pattern}", severity="warning")

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()
