import os
import tomllib
from pathlib import Path

CONFIG_DIR = Path.home() / ".config" / "kitvc"
CONFIG_PATH = CONFIG_DIR / "config.toml"
THEME_PATH = CONFIG_DIR / "theme.toml"

DEFAULT_CONFIG = {
    "music": {
        "directories": [str(Path.home() / "Music")],
    },
    "video": {
        "directories": [str(Path.home() / "Videos")],
        "fullscreen": False,
    },
    "playlist": {
        "music_playlist_dir": "",
        "video_playlist_dir": "",
    },
    "player": {
        "mpv_args": [],
        "volume": 80,
    },
    "theme": {
        "watch_interval": 2,
    },
    "ui": {
        "sidebar_width": 44,
    }
}

DEFAULT_THEME = {
    "colors": {
        "primary": "deepskyblue",
        "accent": "magenta",
        "background": "",
        "surface": "",
    }
}

def load_config():
    if not CONFIG_PATH.exists():
        save_config(DEFAULT_CONFIG)
        return DEFAULT_CONFIG
    
    with open(CONFIG_PATH, "rb") as f:
        return tomllib.load(f)

def load_theme():
    if not THEME_PATH.exists():
        save_theme(DEFAULT_THEME)
        return DEFAULT_THEME
    
    with open(THEME_PATH, "rb") as f:
        try:
            return tomllib.load(f)
        except Exception:
            return DEFAULT_THEME

def save_theme(theme):
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    with open(THEME_PATH, "w") as f:
        for section, values in theme.items():
            f.write(f"[{section}]\n")
            for key, value in values.items():
                f.write(f"{key} = \"{value}\"\n")
            f.write("\n")

def save_config(config):
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    with open(CONFIG_PATH, "w") as f:
        # Simple TOML writer for basic structure
        for section, values in config.items():
            f.write(f"[{section}]\n")
            for key, value in values.items():
                if isinstance(value, list):
                    list_str = ", ".join(f'"{v}"' for v in value)
                    f.write(f"{key} = [{list_str}]\n")
                elif isinstance(value, str):
                    f.write(f"{key} = \"{value}\"\n")
                elif isinstance(value, bool):
                    f.write(f"{key} = {'true' if value else 'false'}\n")
                elif isinstance(value, (int, float)):
                    f.write(f"{key} = {value}\n")
            f.write("\n")
