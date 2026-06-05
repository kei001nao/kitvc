from textual import work
from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Label, DirectoryTree, DataTable, Input, RadioSet, RadioButton, Button, TextArea, Select, OptionList
from textual.coordinate import Coordinate
from textual.containers import Vertical, Horizontal, VerticalScroll
from pathlib import Path
import json
import logging
import os

logger = logging.getLogger(__name__)

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
    #batch-notice { width: 100%; text-align: center; background: $accent; color: $text; text-style: bold; }
    """
    def __init__(self, query: str, is_tv: bool = True, language: str = "ja", season: int = None, episode: int = None, is_batch: bool = False, **kwargs):
        super().__init__(**kwargs)
        self.search_query = query
        self.is_tv = is_tv
        self.language = language
        self.target_season = season
        self.target_episode = episode
        self.is_batch = is_batch
        self._langs = [
            ("ja", "Japanese"), ("en", "English"), ("ko", "Korean"), ("zh", "Chinese"),
        ]
        self._series_results = []
        self._season_results = []
        self._episode_results = []

    def compose(self) -> ComposeResult:
        with Vertical(id="fetch-container"):
            if self.is_batch:
                yield Label(" BATCH MODE: Selecting a Series/Season will apply to ALL selected videos. ", id="batch-notice")
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
            
            help_text = "Tab: Focus   Enter: Select/Fetch   ESC: Cancel"
            if self.is_batch:
                help_text += "   S: Select current Series (Batch)"
            yield Label(help_text, classes="help")

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
        elif self.is_batch and event.key.lower() == "s":
            # Select current series for batch
            self._select_at_series_level()
            event.stop()

    def _select_at_series_level(self) -> None:
        radio_set = self.query_one("#media-type", RadioSet)
        is_tv = (radio_set.pressed_index == 0)
        table_lang = self.query_one("#lang-list", DataTable)
        lang = self._langs[table_lang.cursor_row][0]

        t_series = self.query_one("#series-list", DataTable)
        if t_series.cursor_row < 0: return
        try:
            series_row_key = t_series.coordinate_to_cell_key(t_series.cursor_coordinate).row_key
            series_idx = int(str(series_row_key.value))
            if 0 <= series_idx < len(self._series_results):
                series_id = self._series_results[series_idx]["id"]
            else:
                return
        except Exception:
            return

        self.dismiss({
            "tmdb_id": series_id,
            "is_tv": is_tv,
            "language": lang,
            "query": self.query_one("#search-query", Input).value,
            "season": None,
            "episode": None
        })

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
            if 0 <= idx < len(self._series_results):
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
            if 0 <= idx < len(self._season_results):
                res = self._season_results[idx]
                self._update_preview(res["overview"])
                
                t_series = self.query_one("#series-list", DataTable)
                try:
                    series_row_key = t_series.coordinate_to_cell_key(t_series.cursor_coordinate).row_key
                    series_idx = int(str(series_row_key.value))
                    if 0 <= series_idx < len(self._series_results):
                        series_id = self._series_results[series_idx]["id"]
                        table_lang = self.query_one("#lang-list", DataTable)
                        lang = self._langs[table_lang.cursor_row][0]
                        self._fetch_episodes_deferred(series_id, res["season_number"], lang)
                except Exception:
                    pass
            
        elif event.control.id == "episode-list":
            idx = int(str(event.row_key.value))
            if 0 <= idx < len(self._episode_results):
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
        if series_idx < 0 or series_idx >= len(self._series_results): return
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
            if 0 <= season_idx < len(self._season_results):
                season_num = self._season_results[season_idx]["season_number"]
                ep_idx = int(str(event.row_key.value))
                if 0 <= ep_idx < len(self._episode_results):
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
            if self.is_batch:
                # In batch mode, Enter on series can mean "Select this series"
                self._select_at_series_level()
            else:
                # Go to Season list
                self.query_one("#season-list").focus()
        elif event.control.id == "season-list":
            if self.is_batch:
                # Select at season level
                season_idx = int(str(event.row_key.value))
                if 0 <= season_idx < len(self._season_results):
                    season_num = self._season_results[season_idx]["season_number"]
                    self.dismiss({
                        "tmdb_id": series_id,
                        "is_tv": True,
                        "language": lang,
                        "query": self.query_one("#search-query", Input).value,
                        "season": season_num,
                        "episode": None
                    })
            else:
                # Go to Episode list
                self.query_one("#episode-list").focus()

class FilterConditionModal(Screen):
    """Small modal to add or edit a single filter condition."""
    DEFAULT_CSS = """
    FilterConditionModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #cond-edit-container {
        width: 60;
        height: auto;
        border: panel $primary;
        background: $surface;
        padding: 1;
    }
    #cond-edit-container Label { margin-top: 1; text-style: bold; }
    #cond-edit-container Button { margin-top: 2; width: 100%; }
    .modal-help { text-align: center; color: $text-muted; margin-top: 1; }
    """

    def __init__(self, fields, operators, condition=None, **kwargs):
        super().__init__(**kwargs)
        self.fields = fields
        self.operators = operators
        self.condition = condition or {"field": "type", "op": "==", "value": ""}

    def compose(self) -> ComposeResult:
        with Vertical(id="cond-edit-container"):
            yield Label("Field:")
            yield Select(self.fields, value=self.condition["field"], id="field-select")
            yield Label("Operator:")
            yield Select(self.operators, value=self.condition["op"], id="op-select")
            yield Label("Value:")
            yield Input(value=str(self.condition["value"] or ""), id="value-input")
            yield Button("OK", variant="primary", id="save-btn")
            yield Label("ESC: Cancel", classes="modal-help")

    def on_mount(self) -> None:
        self.query_one("#field-select").focus()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "save-btn":
            field = self.query_one("#field-select").value
            op = self.query_one("#op-select").value
            
            if field is None or field == Select.BLANK:
                self.app.notify("Please select a field", severity="error")
                return
            if op is None or op == Select.BLANK:
                self.app.notify("Please select an operator", severity="error")
                return

            res = {
                "field": field,
                "op": op,
                "value": self.query_one("#value-input").value
            }
            self.dismiss(res)

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()
        elif event.key == "enter":
            focused = self.focused
            if isinstance(focused, Input):
                self.on_button_pressed(Button.Pressed(self.query_one("#save-btn")))
                event.stop()
            elif isinstance(focused, Button) and focused.id == "save-btn":
                # Button will handle its own click
                pass

class SortFieldSelectModal(Screen):
    DEFAULT_CSS = """
    SortFieldSelectModal { align: center middle; background: rgba(0, 0, 0, 0.5); }
    #sort-pick-container { width: 45; height: auto; border: panel $primary; background: $surface; padding: 1; }
    #sort-pick-container OptionList { height: 12; border: solid $primary 10%; }
    .modal-help { text-align: center; color: $text-muted; margin-top: 1; }
    """
    def __init__(self, available_fields: list[tuple[str, str]], **kwargs):
        super().__init__(**kwargs)
        self.available = available_fields
        self.choices = []
        for label, val in available_fields:
            self.choices.append((f"{label} (ASC)", (val, "ASC")))
            self.choices.append((f"{label} (DESC)", (val, "DESC")))

    def compose(self) -> ComposeResult:
        with Vertical(id="sort-pick-container"):
            yield Label("Select Field and Direction:")
            yield OptionList(*[l for l, v in self.choices], id="field-list")
            yield Label("ESC: Cancel", classes="modal-help")
    
    def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        self.dismiss(self.choices[event.option_index][1])
    
    def on_key(self, event) -> None:
        if event.key == "escape": self.dismiss(None)

class VideoFilterEditModal(Screen):
    DEFAULT_CSS = """
    VideoFilterEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #filter-edit-container {
        width: 85;
        height: 90%;
        border: panel $primary;
        background: $surface;
        padding: 1;
    }
    #filter-edit-container Label { margin-top: 1; text-style: bold; }
    #filter-edit-container .section-header {
        background: $primary 20%;
        padding: 0 1;
        margin-top: 1;
        height: 1;
        text-style: bold;
    }
    
    #filter-edit-container DataTable {
        height: auto;
        max-height: 10;
        border: solid $primary 10%;
        background: transparent;
        margin-top: 1;
    }

    #match-type-row { height: 4; align: left middle; }
    #match-type-row Label { margin-top: 1; }
    #match-type-radio { height: 3; border: none; background: transparent; layout: vertical; }
    
    .table-help { color: $text-muted; margin-bottom: 0; }
    
    #filter-edit-scroll {
        height: 1fr;
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

    FIELDS = [
        ("Type", "type"), ("Category", "category"), ("SubCategory", "subcategory"),
        ("Series", "series"), ("Season", "season"), ("Episode", "episode"), 
        ("Title", "title"), ("Year", "year"), ("Genres", "genres"), 
        ("Duration", "duration"), ("CreatedAt", "created_at")
    ]
    
    OPERATORS = [
        ("==", "=="), ("!=", "!="), ("Contains", "contains"), 
        ("Not Contains", "not_contains"), (">", ">"), ("<", "<"), 
        ("Is Null", "is_null"), ("Is Not Null", "is_not_null")
    ]

    def __init__(self, filter_data: dict = None, **kwargs):
        super().__init__(**kwargs)
        self.filter_data = filter_data or {"id": None, "name": "", "conditions_json": "[]", "sort_json": "[]"}
        
        # Internal state
        try:
            conds = json.loads(self.filter_data["conditions_json"])
            if isinstance(conds, dict):
                self.match_type = conds.get("op", "and").lower()
                self.conditions = conds.get("rules", [])
            else:
                self.match_type = "and"
                self.conditions = conds
        except:
            self.match_type = "and"
            self.conditions = []
            
        try:
            self.sort_sequence = json.loads(self.filter_data["sort_json"])
            if not isinstance(self.sort_sequence, list): self.sort_sequence = []
        except:
            self.sort_sequence = []

    def compose(self) -> ComposeResult:
        with Vertical(id="filter-edit-container"):
            yield Label("View Name:")
            yield Input(value=self.filter_data["name"], placeholder="e.g. Action Movies", id="filter-name")
            
            with VerticalScroll(id="filter-edit-scroll"):
                yield Label("Filter Conditions", classes="section-header")
                with Horizontal(id="match-type-row"):
                    yield Label("Match: ")
                    yield RadioSet(
                        RadioButton("All (AND)", value=(self.match_type == "and")),
                        RadioButton("Any (OR)", value=(self.match_type == "or")),
                        id="match-type-radio"
                    )
                
                yield Label("a: Add Condition | Enter: Edit | d: Delete", classes="table-help")
                yield DataTable(id="cond-table", cursor_type="row")
                
                yield Label("Sort Order", classes="section-header")
                yield Label("a: Add Field | d: Delete | +/-: Move Up/Down", classes="table-help")
                yield DataTable(id="sort-table", cursor_type="row")

            yield Label("Ctrl+Enter: Save   ESC: Cancel", classes="edit-help")

    def on_mount(self) -> None:
        c_table = self.query_one("#cond-table", DataTable)
        c_table.add_columns("Field", "Operator", "Value")
        self._refresh_cond_table()

        s_table = self.query_one("#sort-table", DataTable)
        s_table.add_columns("Priority", "Field")
        self._refresh_sort_table()
        
        self.query_one("#filter-name").focus()

    def _refresh_cond_table(self) -> None:
        table = self.query_one("#cond-table", DataTable)
        curr_row = table.cursor_row
        table.clear()
        for i, c in enumerate(self.conditions):
            f_label = next((f[0] for f in self.FIELDS if f[1] == c["field"]), c["field"])
            val_str = str(c["value"]) if c["value"] is not None else ""
            table.add_row(f_label, c["op"], val_str, key=str(i))
        
        if len(self.conditions) > 0:
            target_row = min(curr_row if curr_row is not None else 0, len(self.conditions) - 1)
            table.move_cursor(row=target_row)

    def _refresh_sort_table(self) -> None:
        table = self.query_one("#sort-table", DataTable)
        curr_row = table.cursor_row
        table.clear()
        for i, item in enumerate(self.sort_sequence):
            # Compatibility check: item could be string or [field, dir]
            if isinstance(item, (list, tuple)):
                field_id, direction = item
            else:
                field_id, direction = item, "ASC"
                
            f_label = next((f[0] for f in self.FIELDS if f[1] == field_id), field_id)
            table.add_row(f"#{i+1}", f"{f_label} ({direction})", key=str(i))
        
        if len(self.sort_sequence) > 0:
            target_row = min(curr_row if curr_row is not None else 0, len(self.sort_sequence) - 1)
            table.move_cursor(row=target_row)

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()
        elif event.key in ("ctrl+enter", "ctrl+j"):
            self._save()
            event.stop()
        
        # Handle Table Actions
        focused = self.focused
        if isinstance(focused, DataTable):
            row_idx = focused.cursor_row
            if focused.id == "cond-table":
                if event.key == "a":
                    self._add_condition()
                    event.stop()
                elif event.key == "d" and row_idx is not None:
                    self._delete_condition(row_idx)
                    event.stop()
                elif event.key == "enter" and row_idx is not None:
                    self._edit_condition(row_idx)
                    event.stop()
            elif focused.id == "sort-table":
                if event.key == "a":
                    self._add_sort()
                    event.stop()
                elif event.key == "d" and row_idx is not None:
                    self._delete_sort(row_idx)
                    event.stop()
                elif event.key in ("+", "=") and row_idx is not None:
                    self._move_sort(row_idx, -1)
                    event.stop()
                elif event.key == "-" and row_idx is not None:
                    self._move_sort(row_idx, 1)
                    event.stop()

    def _add_condition(self) -> None:
        def callback(res):
            if res:
                self.conditions.append(res)
                self._refresh_cond_table()
        self.app.push_screen(FilterConditionModal(self.FIELDS, self.OPERATORS), callback=callback)

    def _edit_condition(self, idx: int) -> None:
        if not (0 <= idx < len(self.conditions)):
            return
        def callback(res):
            if res:
                self.conditions[idx] = res
                self._refresh_cond_table()
        self.app.push_screen(FilterConditionModal(self.FIELDS, self.OPERATORS, self.conditions[idx]), callback=callback)

    def _delete_condition(self, idx: int) -> None:
        if 0 <= idx < len(self.conditions):
            self.conditions.pop(idx)
            self._refresh_cond_table()

    def _add_sort(self) -> None:
        used_fields = []
        for item in self.sort_sequence:
            if isinstance(item, (list, tuple)): used_fields.append(item[0])
            else: used_fields.append(item)
            
        available = [(label, val) for label, val in self.FIELDS if val not in used_fields]
        if not available:
            self.app.notify("No more fields to add for sorting")
            return

        def callback(res):
            if res:
                self.sort_sequence.append(res)
                self._refresh_sort_table()
        self.app.push_screen(SortFieldSelectModal(available), callback=callback)

    def _delete_sort(self, idx: int) -> None:
        if 0 <= idx < len(self.sort_sequence):
            self.sort_sequence.pop(idx)
            self._refresh_sort_table()

    def _move_sort(self, idx: int, direction: int) -> None:
        new_idx = idx + direction
        if 0 <= idx < len(self.sort_sequence) and 0 <= new_idx < len(self.sort_sequence):
            self.sort_sequence[idx], self.sort_sequence[new_idx] = self.sort_sequence[new_idx], self.sort_sequence[idx]
            self._refresh_sort_table()
            self.query_one("#sort-table").move_cursor(row=new_idx)

    def _save(self) -> None:
        name = self.query_one("#filter-name", Input).value.strip()
        if not name:
            self.app.notify("Name is required", severity="error")
            return

        radio = self.query_one("#match-type-radio", RadioSet)
        match_type = "and" if radio.pressed_index == 0 else "or"
        
        cond_data = {"op": match_type, "rules": self.conditions}
        
        from ..database import create_video_filter, update_video_filter
        c_json = json.dumps(cond_data)
        s_json = json.dumps(self.sort_sequence)
        
        if self.filter_data["id"] is None:
            create_video_filter(name, c_json, s_json)
        else:
            update_video_filter(self.filter_data["id"], name, c_json, s_json)

        self.dismiss(True)

class MusicFilterEditModal(Screen):
    DEFAULT_CSS = """
    MusicFilterEditModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #filter-edit-container {
        width: 85;
        height: 90%;
        border: panel $primary;
        background: $surface;
        padding: 1;
    }
    #filter-edit-container Label { margin-top: 1; text-style: bold; }
    #filter-edit-container .section-header {
        background: $primary 20%;
        padding: 0 1;
        margin-top: 1;
        height: 1;
        text-style: bold;
    }
    
    #filter-edit-container DataTable {
        height: auto;
        max-height: 10;
        border: solid $primary 10%;
        background: transparent;
        margin-top: 1;
    }

    #match-type-row { height: 4; align: left middle; }
    #match-type-row Label { margin-top: 1; }
    #match-type-radio { height: 3; border: none; background: transparent; layout: vertical; }
    
    .table-help { color: $text-muted; margin-bottom: 0; }
    
    #filter-edit-scroll {
        height: 1fr;
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

    FIELDS = [
        ("Title", "title"), ("Artist", "artist"), ("Album", "album"),
        ("Genre", "genre"), ("Track#", "track_num"), ("Disc#", "disc_num"),
        ("Duration", "duration"), ("BPM", "bpm"), ("CreatedAt", "created_at")
    ]
    
    OPERATORS = [
        ("==", "=="), ("!=", "!="), ("Contains", "contains"), 
        ("Not Contains", "not_contains"), (">", ">"), ("<", "<"), 
        ("Is Null", "is_null"), ("Is Not Null", "is_not_null")
    ]

    def __init__(self, filter_data: dict = None, **kwargs):
        super().__init__(**kwargs)
        self.filter_data = filter_data or {"id": None, "name": "", "conditions_json": "[]", "sort_json": "[]"}
        
        # Internal state
        try:
            conds = json.loads(self.filter_data["conditions_json"])
            if isinstance(conds, dict):
                self.match_type = conds.get("op", "and").lower()
                self.conditions = conds.get("rules", [])
            else:
                self.match_type = "and"
                self.conditions = conds
        except:
            self.match_type = "and"
            self.conditions = []
            
        try:
            self.sort_sequence = json.loads(self.filter_data["sort_json"])
            if not isinstance(self.sort_sequence, list): self.sort_sequence = []
        except:
            self.sort_sequence = []

    def compose(self) -> ComposeResult:
        with Vertical(id="filter-edit-container"):
            yield Label("Music View Name:")
            yield Input(value=self.filter_data["name"], placeholder="e.g. Jazz Favorites", id="filter-name")
            
            with VerticalScroll(id="filter-edit-scroll"):
                yield Label("Filter Conditions", classes="section-header")
                with Horizontal(id="match-type-row"):
                    yield Label("Match: ")
                    yield RadioSet(
                        RadioButton("All (AND)", value=(self.match_type == "and")),
                        RadioButton("Any (OR)", value=(self.match_type == "or")),
                        id="match-type-radio"
                    )
                
                yield Label("a: Add Condition | Enter: Edit | d: Delete", classes="table-help")
                yield DataTable(id="cond-table", cursor_type="row")
                
                yield Label("Sort Order", classes="section-header")
                yield Label("a: Add Field | d: Delete | +/-: Move Up/Down", classes="table-help")
                yield DataTable(id="sort-table", cursor_type="row")

            yield Label("Ctrl+Enter: Save   ESC: Cancel", classes="edit-help")

    def on_mount(self) -> None:
        c_table = self.query_one("#cond-table", DataTable)
        c_table.add_columns("Field", "Operator", "Value")
        self._refresh_cond_table()

        s_table = self.query_one("#sort-table", DataTable)
        s_table.add_columns("Priority", "Field")
        self._refresh_sort_table()
        
        self.query_one("#filter-name").focus()

    def _refresh_cond_table(self) -> None:
        table = self.query_one("#cond-table", DataTable)
        curr_row = table.cursor_row
        table.clear()
        for i, c in enumerate(self.conditions):
            f_label = next((f[0] for f in self.FIELDS if f[1] == c["field"]), c["field"])
            val_str = str(c["value"]) if c["value"] is not None else ""
            table.add_row(f_label, c["op"], val_str, key=str(i))
        
        if len(self.conditions) > 0:
            target_row = min(curr_row if curr_row is not None else 0, len(self.conditions) - 1)
            table.move_cursor(row=target_row)

    def _refresh_sort_table(self) -> None:
        table = self.query_one("#sort-table", DataTable)
        curr_row = table.cursor_row
        table.clear()
        for i, item in enumerate(self.sort_sequence):
            if isinstance(item, (list, tuple)):
                field_id, direction = item
            else:
                field_id, direction = item, "ASC"
                
            f_label = next((f[0] for f in self.FIELDS if f[1] == field_id), field_id)
            table.add_row(f"#{i+1}", f"{f_label} ({direction})", key=str(i))
        
        if len(self.sort_sequence) > 0:
            target_row = min(curr_row if curr_row is not None else 0, len(self.sort_sequence) - 1)
            table.move_cursor(row=target_row)

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()
        elif event.key in ("ctrl+enter", "ctrl+j"):
            self._save()
            event.stop()
        
        focused = self.focused
        if isinstance(focused, DataTable):
            row_idx = focused.cursor_row
            if focused.id == "cond-table":
                if event.key == "a":
                    self._add_condition()
                    event.stop()
                elif event.key == "d" and row_idx is not None:
                    self._delete_condition(row_idx)
                    event.stop()
                elif event.key == "enter" and row_idx is not None:
                    self._edit_condition(row_idx)
                    event.stop()
            elif focused.id == "sort-table":
                if event.key == "a":
                    self._add_sort()
                    event.stop()
                elif event.key == "d" and row_idx is not None:
                    self._delete_sort(row_idx)
                    event.stop()
                elif event.key in ("+", "=") and row_idx is not None:
                    self._move_sort(row_idx, -1)
                    event.stop()
                elif event.key == "-" and row_idx is not None:
                    self._move_sort(row_idx, 1)
                    event.stop()

    def _add_condition(self) -> None:
        def callback(res):
            if res:
                self.conditions.append(res)
                self._refresh_cond_table()
        self.app.push_screen(FilterConditionModal(self.FIELDS, self.OPERATORS), callback=callback)

    def _edit_condition(self, idx: int) -> None:
        if not (0 <= idx < len(self.conditions)): return
        def callback(res):
            if res:
                self.conditions[idx] = res
                self._refresh_cond_table()
        self.app.push_screen(FilterConditionModal(self.FIELDS, self.OPERATORS, self.conditions[idx]), callback=callback)

    def _delete_condition(self, idx: int) -> None:
        if 0 <= idx < len(self.conditions):
            self.conditions.pop(idx)
            self._refresh_cond_table()

    def _add_sort(self) -> None:
        used_fields = []
        for item in self.sort_sequence:
            if isinstance(item, (list, tuple)): used_fields.append(item[0])
            else: used_fields.append(item)
            
        available = [(label, val) for label, val in self.FIELDS if val not in used_fields]
        if not available:
            self.app.notify("No more fields to add for sorting")
            return

        def callback(res):
            if res:
                self.sort_sequence.append(res)
                self._refresh_sort_table()
        self.app.push_screen(SortFieldSelectModal(available), callback=callback)

    def _delete_sort(self, idx: int) -> None:
        if 0 <= idx < len(self.sort_sequence):
            self.sort_sequence.pop(idx)
            self._refresh_sort_table()

    def _move_sort(self, idx: int, direction: int) -> None:
        new_idx = idx + direction
        if 0 <= idx < len(self.sort_sequence) and 0 <= new_idx < len(self.sort_sequence):
            self.sort_sequence[idx], self.sort_sequence[new_idx] = self.sort_sequence[new_idx], self.sort_sequence[idx]
            self._refresh_sort_table()
            self.query_one("#sort-table").move_cursor(row=new_idx)

    def _save(self) -> None:
        name = self.query_one("#filter-name", Input).value.strip()
        if not name:
            self.app.notify("Name is required", severity="error")
            return

        radio = self.query_one("#match-type-radio", RadioSet)
        match_type = "and" if radio.pressed_index == 0 else "or"
        
        cond_data = {"op": match_type, "rules": self.conditions}
        
        from ..database import create_music_filter, update_music_filter
        c_json = json.dumps(cond_data)
        s_json = json.dumps(self.sort_sequence)
        
        if self.filter_data["id"] is None:
            create_music_filter(name, c_json, s_json)
        else:
            update_music_filter(self.filter_data["id"], name, c_json, s_json)

        self.dismiss(True)

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
        table.add_row("3. Search & Apply (Pick TMDB entry for all)")
        table.focus()

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        idx = event.cursor_row
        if idx == 0:
            self.dismiss("filename")
        elif idx == 1:
            self.dismiss("metadata")
        elif idx == 2:
            self.dismiss("search")

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
    def __init__(self, **kwargs):
        super().__init__(**kwargs)

    def compose(self) -> ComposeResult:
        with Vertical(id="quit-container"):
            yield Label("Quit KITVC?")
            yield Label("Q/Ctrl+C: Quit  ESC: Cancel", classes="playlist-help")

    def on_key(self, event) -> None:
        if event.key.lower() == "q" or event.key == "ctrl+c":
            self.app.exit()
        elif event.key == "escape":
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

class GlobalSearchModal(Screen):
    DEFAULT_CSS = """
    GlobalSearchModal {
        align: center middle;
        background: rgba(0, 0, 0, 0.5);
    }
    #search-container {
        width: 80%;
        height: 80%;
        border: panel $primary;
        background: $surface;
        padding: 1;
    }
    #search-container Label { height: 1; text-style: bold; margin-bottom: 1; }
    #search-container Input { margin-bottom: 1; border: none; background: $accent 10%; padding: 0 1; height: 1; }
    #search-container DataTable { height: 1fr; background: transparent; border: solid $primary 10%; }
    .search-help { text-align: center; color: $text-muted; margin-top: 1; height: 1; }
    """

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._results = []

    def compose(self) -> ComposeResult:
        with Vertical(id="search-container"):
            yield Label("Global Search")
            yield Input(placeholder="Search Music or Video...", id="search-input")
            yield DataTable(id="search-results", cursor_type="row")
            yield Label("Enter: Go to Item   ESC: Close", classes="search-help")

    def on_mount(self) -> None:
        table = self.query_one("#search-results", DataTable)
        table.add_column("Type", width=6)
        table.add_column("Category/Artist")
        table.add_column("Title")
        table.add_column("Filename")
        self.query_one("#search-input").focus()

    def on_input_changed(self, event: Input.Changed) -> None:
        self._search(event.value)

    @work(thread=True, exclusive=True)
    def _search(self, query: str) -> None:
        if len(query) < 2:
            self.app.call_from_thread(self._populate, [])
            return

        from ..database import get_connection
        results = []
        q = f"%{query}%"
        with get_connection() as conn:
            # Music
            music = conn.execute(
                "SELECT 'Music' as type, artist as cat, title, path as filename, path FROM music_tracks "
                "WHERE artist LIKE ? OR album LIKE ? OR title LIKE ? OR path LIKE ? LIMIT 50",
                (q, q, q, q)
            ).fetchall()
            results.extend([dict(r) for r in music])
            
            # Video
            video = conn.execute(
                "SELECT 'Video' as type, series as cat, title, filename, path FROM video_files "
                "WHERE series LIKE ? OR category LIKE ? OR title LIKE ? OR filename LIKE ? LIMIT 50",
                (q, q, q, q)
            ).fetchall()
            results.extend([dict(r) for r in video])

        self.app.call_from_thread(self._populate, results)

    def _populate(self, results: list[dict]) -> None:
        table = self.query_one("#search-results", DataTable)
        table.clear()
        self._results = results
        for i, r in enumerate(results):
            disp_filename = r["filename"]
            if r["type"] == "Music":
                disp_filename = os.path.basename(r["path"])
            table.add_row(r["type"], r["cat"] or "", r["title"] or "", disp_filename, key=str(i))

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        try:
            idx = int(str(event.row_key.value))
            item = self._results[idx]
            self.dismiss(item)
        except (ValueError, IndexError):
            pass

    def on_key(self, event) -> None:
        if event.key == "escape":
            self.dismiss(None)
            event.stop()
