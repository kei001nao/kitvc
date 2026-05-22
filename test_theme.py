from textual.app import App
from textual.widgets import Label
from textual.containers import Horizontal

class Sidebar(Label):
    DEFAULT_CSS = "Sidebar { width: 20; border-right: panel $primary; }"

class TestApp(App):
    def on_mount(self):
        self.stylesheet.add_source("$primary: red;")
    
    def compose(self):
        yield Horizontal(Sidebar("Left"), Label("Right"))

if __name__ == "__main__":
    app = TestApp()
    app.run()
