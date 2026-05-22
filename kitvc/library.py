import os
import mutagen
from typing import Callable, Optional
from .database import get_connection, update_music_track, update_video_file, remove_missing_files

AUDIO_EXTENSIONS = {".flac", ".mp3", ".opus", ".ogg", ".m4a", ".aac", ".wav", ".aiff"}
VIDEO_EXTENSIONS = {".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v"}

def _read_music_tags(path: str) -> dict | None:
    try:
        f = mutagen.File(path, easy=True)
        if f is None:
            return None

        def first(key: str) -> str:
            v = f.get(key, [])
            return str(v[0]) if v else ""

        title = first("title") or os.path.splitext(os.path.basename(path))[0]
        artist = first("artist") or "Unknown Artist"
        album_artist = first("albumartist") or artist
        album = first("album") or "Unknown Album"
        duration = int(f.info.length) if hasattr(f, "info") else 0

        track_num = 1
        if raw := first("tracknumber"):
            try:
                track_num = int(raw.split("/")[0])
            except ValueError:
                pass

        disc_num = 1
        if raw := first("discnumber"):
            try:
                disc_num = int(raw.split("/")[0])
            except ValueError:
                pass

        year = None
        if raw := first("date"):
            try:
                year = int(raw[:4])
            except ValueError:
                pass

        bpm = None
        if raw := first("bpm"):
            try:
                bpm = float(raw)
            except ValueError:
                pass

        return {
            "title": title,
            "artist": artist,
            "album_artist": album_artist,
            "album": album,
            "track_num": track_num,
            "disc_num": disc_num,
            "year": year,
            "genre": first("genre"),
            "bpm": bpm,
            "duration": duration,
        }
    except Exception:
        return None

class MusicLibrary:
    def __init__(self, directories: list[str]):
        self.directories = [os.path.expanduser(d) for d in directories]

    def scan(self, progress_cb: Optional[Callable[[str], None]] = None):
        found_paths = set()
        
        # Get cached mtimes from DB
        with get_connection() as conn:
            cache = {row["path"]: row["mtime"] for row in conn.execute("SELECT path, mtime FROM music_tracks").fetchall()}

        for music_dir in self.directories:
            if not os.path.isdir(music_dir):
                continue
            for root, _, files in os.walk(music_dir):
                for fname in sorted(files):
                    if os.path.splitext(fname)[1].lower() not in AUDIO_EXTENSIONS:
                        continue
                    path = os.path.join(root, fname)
                    found_paths.add(path)
                    
                    mtime = os.path.getmtime(path)
                    if cache.get(path) != mtime:
                        if progress_cb:
                            progress_cb(fname)
                        tags = _read_music_tags(path)
                        if tags:
                            tags["path"] = path
                            tags["mtime"] = mtime
                            update_music_track(tags)

        # Remove tracks that are no longer present
        if found_paths:
            remove_missing_files(list(found_paths), "music_tracks")

from .utils import generate_thumbnail

class VideoLibrary:
    def __init__(self, directories: list[str]):
        self.directories = [os.path.expanduser(d) for d in directories]

    def scan(self, progress_cb: Optional[Callable[[str], None]] = None):
        found_paths = set()
        
        with get_connection() as conn:
            cache = {row["path"]: row["mtime"] for row in conn.execute("SELECT path, mtime FROM video_files").fetchall()}

        for video_dir in self.directories:
            if not os.path.isdir(video_dir):
                continue
            for root, _, files in os.walk(video_dir):
                for fname in sorted(files):
                    if os.path.splitext(fname)[1].lower() not in VIDEO_EXTENSIONS:
                        continue
                    path = os.path.join(root, fname)
                    found_paths.add(path)
                    
                    mtime = os.path.getmtime(path)
                    if cache.get(path) != mtime:
                        if progress_cb:
                            progress_cb(fname)
                        
                        thumb = generate_thumbnail(path)
                        video_data = {
                            "path": path,
                            "mtime": mtime,
                            "filename": fname,
                            "size": os.path.getsize(path),
                            "thumbnail_path": thumb
                        }
                        update_video_file(video_data)

        if found_paths:
            remove_missing_files(list(found_paths), "video_files")
