from textual.app import ComposeResult
from textual.screen import Screen
from textual.widgets import Label, DirectoryTree
from textual.containers import Vertical
from pathlib import Path

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
