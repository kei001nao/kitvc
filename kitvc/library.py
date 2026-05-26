import os
import subprocess
import json
import mutagen
import logging
import hashlib
from mutagen.easyid3 import EasyID3
from mutagen.flac import FLAC
from mutagen.mp3 import MP3
from typing import Callable, Optional
from .database import get_connection, update_music_track, update_video_file, remove_missing_files, remove_unused_albums
from .config import CONFIG_DIR
from .metadata_music import search_release, fetch_music_metadata, download_cover_art

logger = logging.getLogger(__name__)

AUDIO_EXTENSIONS = {".flac", ".mp3", ".opus", ".ogg", ".m4a", ".aac", ".wav", ".aiff"}
VIDEO_EXTENSIONS = {".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v"}

def _extract_cover_art(path: str) -> str | None:
    """Extract embedded cover art and save to a stable location."""
    try:
        covers_dir = CONFIG_DIR / "covers"
        covers_dir.mkdir(parents=True, exist_ok=True)
        
        # Use stable hash (MD5) instead of unstable hash()
        file_hash = hashlib.md5(path.encode()).hexdigest()
        target = covers_dir / f"{file_hash}.jpg"
        
        if target.exists():
            return str(target)

        audio = mutagen.File(path)
        if audio is None:
            return None
            
        data = None
        if isinstance(audio, FLAC) and audio.pictures:
            data = audio.pictures[0].data
        elif isinstance(audio, MP3):
            # Check ID3 tags for APIC frame
            for tag in audio.tags.values():
                if tag.FrameID == "APIC":
                    data = tag.data
                    break
        elif "covr" in audio: # MP4/M4A
            data = audio["covr"][0]

        if data:
            with open(target, "wb") as f:
                f.write(data)
            return str(target)
    except Exception:
        pass
    return None

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
        
        # Get duration using full mutagen file (EasyID3 doesn't have info)
        full_f = mutagen.File(path)
        duration = int(full_f.info.length) if hasattr(full_f, "info") else 0

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

        mbid = first("musicbrainz_trackid")
        comment = first("comment")

        cover_path = _extract_cover_art(path)

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
            "cover_path": cover_path,
            "mbid": mbid,
            "comment": comment
        }
    except Exception:
        return None

def _get_video_info(path: str) -> dict:
    """Use ffprobe to get video duration and other metadata."""
    try:
        cmd = [
            "ffprobe", "-v", "quiet", "-print_format", "json",
            "-show_format", "-show_streams", path
        ]
        res = subprocess.run(cmd, capture_output=True, text=True)
        data = json.loads(res.stdout)
        
        fmt = data.get("format", {})
        duration = int(float(fmt.get("duration", 0)))
        
        # Try to find year in tags
        tags = fmt.get("tags", {})
        year = None
        if date := tags.get("date") or tags.get("creation_time"):
            try:
                year = int(date[:4])
            except ValueError:
                pass

        return {
            "duration": duration,
            "year": year,
            "title": tags.get("title")
        }
    except Exception:
        return {"duration": 0, "year": None, "title": None}

class MusicLibrary:
    def __init__(self, directories: list[str]):
        self.directories = [os.path.abspath(os.path.expanduser(d)) for d in directories]

    def scan(self, progress_cb: Optional[Callable[[str], None]] = None):
        """Phase 1: Fast local tag scan."""
        try:
            found_paths = set()
            with get_connection() as conn:
                cache = {row["path"]: row["mtime"] for row in conn.execute("SELECT path, mtime FROM music_tracks").fetchall()}

            for music_dir in self.directories:
                if not os.path.isdir(music_dir): continue
                for root, _, files in os.walk(music_dir):
                    for fname in sorted(files):
                        if os.path.splitext(fname)[1].lower() not in AUDIO_EXTENSIONS: continue
                        path = os.path.normpath(os.path.join(root, fname))
                        found_paths.add(path)
                        
                        mtime = os.path.getmtime(path)
                        if cache.get(path) != mtime:
                            if progress_cb: progress_cb(fname)
                            tags = _read_music_tags(path)
                            if tags:
                                tags["path"] = path
                                tags["mtime"] = mtime
                                update_music_track(tags)

            remove_missing_files(list(found_paths), "music_tracks")
            remove_unused_albums()
        except Exception as e:
            logger.exception("MusicLibrary.scan failed")
            raise e

    def enrich_metadata(self, progress_cb: Optional[Callable[[str], None]] = None, force: bool = False):
        """Phase 2: Fetch missing album info from internet."""
        try:
            with get_connection() as conn:
                if force:
                    albums = conn.execute("SELECT id, artist, title, mbid, cover_path FROM music_albums").fetchall()
                else:
                    # Target albums with missing MBID or cover
                    albums = conn.execute(
                        "SELECT id, artist, title, mbid, cover_path FROM music_albums WHERE mbid IS NULL OR cover_path IS NULL"
                    ).fetchall()

            for album in albums:
                album_id = album["id"]
                artist = album["artist"]
                title = album["title"]
                mbid = album["mbid"]
                
                if progress_cb:
                    progress_cb(f"Fetching metadata for: {artist} - {title}")
                
                updates = {}
                
                # 1. Search for MBID if missing
                if not mbid:
                    mbid = search_release(artist, title)
                    if mbid:
                        updates["mbid"] = mbid
                        logger.info(f"Found MBID for {artist} - {title}: {mbid}")
                
                # 2. Fetch detailed metadata if we have MBID
                if mbid:
                    ext_meta = fetch_music_metadata(mbid)
                    if ext_meta:
                        if ext_meta.get("date"): updates["release_date"] = ext_meta["date"]
                        if ext_meta.get("comment"): updates["comment"] = ext_meta["comment"]
                        
                        # 3. Download cover if missing or forced
                        if force or not album["cover_path"]:
                            local_cover = download_cover_art(mbid, CONFIG_DIR / "covers")
                            if local_cover:
                                updates["cover_path"] = local_cover
                                logger.info(f"Downloaded cover for {artist} - {title}")
                
                if updates:
                    with get_connection() as conn:
                        set_clause = ", ".join([f"{k} = ?" for k in updates.keys()])
                        params = list(updates.values())
                        params.append(album_id)
                        conn.execute(f"UPDATE music_albums SET {set_clause} WHERE id = ?", params)
                        
        except Exception as e:
            logger.exception("MusicLibrary.enrich_metadata failed")

from .utils import generate_thumbnail

class VideoLibrary:
    def __init__(self, directories: list[str]):
        self.directories = [os.path.abspath(os.path.expanduser(d)) for d in directories]

    def scan(self, progress_cb: Optional[Callable[[str], None]] = None):
        found_paths = set()
        with get_connection() as conn:
            cache = {row["path"]: row["mtime"] for row in conn.execute("SELECT path, mtime FROM video_files").fetchall()}

        for video_dir in self.directories:
            if not os.path.isdir(video_dir): continue
            for root, _, files in os.walk(video_dir):
                for fname in sorted(files):
                    if os.path.splitext(fname)[1].lower() not in VIDEO_EXTENSIONS: continue
                    path = os.path.normpath(os.path.join(root, fname))
                    found_paths.add(path)
                    
                    mtime = os.path.getmtime(path)
                    if cache.get(path) != mtime:
                        if progress_cb: progress_cb(fname)
                        
                        thumb = generate_thumbnail(path)
                        info = _get_video_info(path)

                        video_data = {
                            "path": path,
                            "mtime": mtime,
                            "filename": fname,
                            "size": os.path.getsize(path),
                            "duration": info["duration"],
                            "year": info["year"],
                            "thumbnail_path": thumb
                        }
                        if info.get("title"):
                            video_data["title"] = info["title"]
                        update_video_file(video_data)

        remove_missing_files(list(found_paths), "video_files")
