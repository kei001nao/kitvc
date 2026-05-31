from textual import work
from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Label, DirectoryTree, DataTable, Input, RadioSet, RadioButton, Button
from textual.coordinate import Coordinate
from textual.containers import Vertical, Horizontal
from pathlib import Path

class VideoFetchModal(Screen):
    DEFAULT_CSS = """
    VideoFetchModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #fetch-container {
        width: 95%;
        height: 95%;
        border: panel $primary;
        background: $surface;
        padding: 1 1 0 1;
    }
    #fetch-header { height: 14; margin-bottom: 0; border-bottom: solid $primary; }
    #fetch-header Label { margin-top: 0; height: 1; text-style: bold; }
    #fetch-header Input { margin-bottom: 0; border: none; background: $accent 10%; color: $text; padding: 0 1; height: 1; }
    
    #type-box { width: 30; }
    #lang-box { width: 1fr; }
    
    #fetch-header RadioSet { background: transparent; border: none; padding: 0; margin: 0; height: 3; layout: horizontal; }
    #fetch-header RadioButton { margin-right: 2; padding: 0; }
    #fetch-header DataTable#lang-list { height: 5; background: transparent; border: none; margin-top: 0; }
    #fetch-header Button#search-btn { margin-top: 1; width: 100%; background: $accent; color: $text; height: 3; }
    
    #list-container { height: 1fr; margin-top: 0; }
    #list-container DataTable { height: 100%; background: transparent; border: solid $primary; }
    #series-list { width: 30%; }
    #season-list { width: 20%; }
    #episode-list { width: 50%; }

    #preview-area { height: 6; border-top: solid $primary; padding: 1; }
    .help { width: 100%; text-align: center; background: $primary; color: $text; margin-top: 0; height: 1; }
    """
    def __init__(self, query: str, is_tv: bool = True, language: str = "ja", season: int = None, episode: int = None, **kwargs):
        super().__init__(**kwargs)
        self.search_query = query
        self.is_tv = is_tv
        self.language = language
        self.target_season = season
        self.target_episode = episode
        self._langs = [
            ("ja", "Japanese"), ("en", "English"), ("ko", "Korean"), ("zh", "Chinese"),
        ]
        self._series_results = []
        self._season_results = []
        self._episode_results = []

    def compose(self) -> ComposeResult:
        with Vertical(id="fetch-container"):
            with Vertical(id="fetch-header"):
                yield Label("Search Query:")
                yield Input(value=self.search_query, id="search-query")
                
                with Horizontal():
                    with Vertical(id="type-box"):
                        yield Label("Media Type:")
                        with RadioSet(id="media-type"):
                            yield RadioButton("TV Show", value=self.is_tv)
                            yield RadioButton("Movie", value=not self.is_tv)
                    with Vertical(id="lang-box"):
                        yield Label("Language:")
                        yield DataTable(id="lang-list", cursor_type="row")
                
                yield Button("Search TMDB", id="search-btn")
            
            with Horizontal(id="list-container"):
                yield DataTable(id="series-list", cursor_type="row")
                yield DataTable(id="season-list", cursor_type="row")
                yield DataTable(id="episode-list", cursor_type="row")
            
            with Vertical(id="preview-area"):
                yield Label("", id="preview-text")
            
            yield Label("Tab: Focus   Enter: Select/Fetch   ESC: Cancel", classes="help")

    def on_mount(self) -> None:
        # Lang table
        table_lang = self.query_one("#lang-list", DataTable)
        table_lang.add_column("Code", width=4)
        table_lang.add_column("Language")
        table_lang.fixed_columns = 0
        table_lang.header_height = 0
        
        selected_row = 0
        for i, (code, name) in enumerate(self._langs):
            table_lang.add_row(code, name)
            if code == self.language:
                selected_row = i
        table_lang.move_cursor(row=selected_row)
        
        # Series table
        t_series = self.query_one("#series-list", DataTable)
        t_series.add_column("Series/Movie")
        t_series.add_column("Year", width=5)
        
        # Season table
        t_season = self.query_one("#season-list", DataTable)
        t_season.add_column("Season")
        
        # Episode table
        t_episode = self.query_one("#episode-list", DataTable)
        t_episode.add_column("E#", width=3)
        t_episode.add_column("Episode Title")
        t_episode.add_column("Date", width=10)
        
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
        if not query: return
            
        radio_set = self.query_one("#media-type", RadioSet)
        is_tv = (radio_set.pressed_index == 0)
        
        table_lang = self.query_one("#lang-list", DataTable)
        lang = self._langs[table_lang.cursor_row][0]
        
        self.app.call_from_thread(self.app.notify, f"Searching for '{query}'...")
        
        from ..metadata_video import search_videos
        results = search_videos(query, is_tv=is_tv, language=lang)
        self.app.call_from_thread(self._populate_series, results, is_tv)

    def _populate_series(self, results: list[dict], is_tv: bool) -> None:
        self._series_results = results
        table = self.query_one("#series-list", DataTable)
        table.clear()
        for i, res in enumerate(results):
            table.add_row(res["title"], res["year"], key=str(i))
        
        # Clear children
        self.query_one("#season-list", DataTable).clear()
        self.query_one("#episode-list", DataTable).clear()
        self._season_results = []
        self._episode_results = []
        
        if results:
            table.move_cursor(row=0)
            table.focus()
            self._update_preview(results[0]["overview"])
            if is_tv:
                table_lang = self.query_one("#lang-list", DataTable)
                lang = self._langs[table_lang.cursor_row][0]
                self._fetch_seasons_deferred(results[0]["id"], lang)
        else:
            self.app.notify("No matches found", severity="warning")

    def _update_preview(self, text: str) -> None:
        preview = self.query_one("#preview-text", Label)
        preview.update(text or "(No overview)")

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        if event.control.id == "series-list":
            idx = int(str(event.row_key.value))
            res = self._series_results[idx]
            self._update_preview(res["overview"])
            
            radio_set = self.query_one("#media-type", RadioSet)
            if radio_set.pressed_index == 0: # TV
                # Pass data to worker
                table_lang = self.query_one("#lang-list", DataTable)
                lang = self._langs[table_lang.cursor_row][0]
                self._fetch_seasons_deferred(res["id"], lang)
        
        elif event.control.id == "season-list":
            idx = int(str(event.row_key.value))
            res = self._season_results[idx]
            self._update_preview(res["overview"])
            
            t_series = self.query_one("#series-list", DataTable)
            series_row_key = t_series.coordinate_to_cell_key(Coordinate(t_series.cursor_row, 0)).row_key
            series_idx = int(str(series_row_key.value))
            series_id = self._series_results[series_idx]["id"]
            table_lang = self.query_one("#lang-list", DataTable)
            lang = self._langs[table_lang.cursor_row][0]
            self._fetch_episodes_deferred(series_id, res["season_number"], lang)
            
        elif event.control.id == "episode-list":
            idx = int(str(event.row_key.value))
            res = self._episode_results[idx]
            self._update_preview(res["overview"])

    @work(thread=True, exclusive=True)
    def _fetch_seasons_deferred(self, series_id: int, lang: str) -> None:
        import time
        time.sleep(0.3)
        from ..metadata_video import fetch_tv_seasons
        seasons = fetch_tv_seasons(series_id, language=lang)
        self.app.call_from_thread(self._populate_seasons, seasons)

    def _populate_seasons(self, seasons: list[dict]) -> None:
        self._season_results = seasons
        table = self.query_one("#season-list", DataTable)
        table.clear()
        
        target_row = 0
        for i, s in enumerate(seasons):
            table.add_row(f"{s['name']} ({s['air_date']})", key=str(i))
            if self.target_season is not None and s['season_number'] == self.target_season:
                target_row = i
        
        if seasons:
            table.move_cursor(row=target_row)
            # Fetch episodes for first season
            t_series = self.query_one("#series-list", DataTable)
            series_row_key = t_series.coordinate_to_cell_key(Coordinate(t_series.cursor_row, 0)).row_key
            series_idx = int(str(series_row_key.value))
            series_id = self._series_results[series_idx]["id"]
            table_lang = self.query_one("#lang-list", DataTable)
            lang = self._langs[table_lang.cursor_row][0]
            self._fetch_episodes_deferred(series_id, seasons[target_row]["season_number"], lang)
        else:
            self.query_one("#episode-list", DataTable).clear()

    @work(thread=True, exclusive=True)
    def _fetch_episodes_deferred(self, series_id: int, season_num: int, lang: str) -> None:
        import time
        time.sleep(0.3)
        from ..metadata_video import fetch_tv_episodes
        episodes = fetch_tv_episodes(series_id, season_num, language=lang)
        self.app.call_from_thread(self._populate_episodes, episodes)

    def _populate_episodes(self, episodes: list[dict]) -> None:
        self._episode_results = episodes
        table = self.query_one("#episode-list", DataTable)
        table.clear()
        
        target_row = 0
        for i, e in enumerate(episodes):
            table.add_row(str(e['episode_number']), e['name'], e['air_date'], key=str(i))
            if self.target_episode is not None and e['episode_number'] == self.target_episode:
                target_row = i
        
        if episodes:
            table.move_cursor(row=target_row)

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        radio_set = self.query_one("#media-type", RadioSet)
        is_tv = (radio_set.pressed_index == 0)
        table_lang = self.query_one("#lang-list", DataTable)
        lang = self._langs[table_lang.cursor_row][0]

        t_series = self.query_one("#series-list", DataTable)
        if t_series.cursor_row < 0: return
        series_row_key = t_series.coordinate_to_cell_key(Coordinate(t_series.cursor_row, 0)).row_key
        series_idx = int(str(series_row_key.value))
        series_id = self._series_results[series_idx]["id"]

        if not is_tv:
            # Movie selected
            self.dismiss({
                "tmdb_id": series_id,
                "is_tv": False,
                "language": lang,
                "query": self.query_one("#search-query", Input).value,
                "season": None,
                "episode": None
            })
            return

        # TV Show: Check where Enter was pressed
        if event.control.id == "episode-list":
            # Fully selected E
            season_idx = self.query_one("#season-list", DataTable).cursor_row
            season_num = self._season_results[season_idx]["season_number"]
            ep_idx = int(str(event.row_key.value))
            ep_num = self._episode_results[ep_idx]["episode_number"]
            
            self.dismiss({
                "tmdb_id": series_id,
                "is_tv": True,
                "language": lang,
                "query": self.query_one("#search-query", Input).value,
                "season": season_num,
                "episode": ep_num
            })
        elif event.control.id == "series-list":
            # Go to Season list
            self.query_one("#season-list").focus()
        elif event.control.id == "season-list":
            # Go to Episode list
            self.query_one("#episode-list").focus()

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
