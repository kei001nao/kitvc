import os
import subprocess
import json
import mutagen
import logging
import hashlib
from pathlib import Path
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
    """Extract embedded cover art and save to a stable location based on data hash."""
    abs_path = os.path.abspath(path)
    try:
        audio = mutagen.File(abs_path)
        data = None
        
        if audio is not None:
            if isinstance(audio, FLAC) and audio.pictures:
                data = audio.pictures[0].data
                logger.info(f"Extracted embedded FLAC picture from {abs_path}")
            elif isinstance(audio, MP3) and audio.tags:
                for tag in audio.tags.values():
                    if tag.FrameID == "APIC":
                        data = tag.data
                        logger.info(f"Extracted embedded MP3 APIC from {abs_path}")
                        break
            elif "covr" in audio: # MP4/M4A
                data = audio["covr"][0]
                logger.info(f"Extracted embedded MP4 covr from {abs_path}")

        if not data:
            # Fallback: search for image files in the same directory
            logger.info(f"No embedded art in {abs_path}, searching directory...")
            data = _read_folder_art_data(abs_path)

        if data:
            covers_dir = CONFIG_DIR / "covers"
            covers_dir.mkdir(parents=True, exist_ok=True)
            
            data_hash = hashlib.md5(data).hexdigest()
            target = covers_dir / f"{data_hash}.jpg"
            
            if not target.exists():
                logger.info(f"Saving new cover art ({len(data)} bytes) to: {target}")
                with open(target, "wb") as f:
                    f.write(data)
            return str(target)
        else:
            logger.warning(f"No cover art found for {abs_path}")
            
    except Exception as e:
        logger.error(f"Cover extraction failed for {abs_path}: {e}")
    return None

def _read_folder_art_data(abs_path: str) -> bytes | None:
    """Read an image file found in the same directory (or parent) as the track."""
    try:
        track_path = Path(abs_path).resolve()
        
        # Priority image extensions
        extensions = [".jpg", ".jpeg", ".png"]
        # Priority names (without extension)
        priority_names = ["cover", "folder", "front", "album", "art"]
        
        # Search current directory and parent directory (to handle Album/Track structure)
        search_dirs = [track_path.parent]
        # Also check one level up if parent name is just numbers (disc 1, etc) or similar
        if track_path.parent.parent and track_path.parent.parent != track_path.parent:
             search_dirs.append(track_path.parent.parent)

        for parent in search_dirs:
            if not parent.exists(): continue
            logger.info(f"Searching for images in: {parent}")
            
            # Use os.listdir for maximum reliability
            try:
                files = os.listdir(parent)
            except Exception as e:
                logger.error(f"Failed to list directory {parent}: {e}")
                continue
            
            # 1. Look for priority names (case-insensitive)
            for name in priority_names:
                for f in files:
                    f_path = parent / f
                    if f_path.is_file():
                        stem = f_path.stem.lower()
                        ext = f_path.suffix.lower()
                        if stem == name and ext in extensions:
                            logger.info(f"Priority folder art: {f_path} ({f_path.stat().st_size} bytes)")
                            return f_path.read_bytes()
            
            # 2. Look for ANY image file
            for f in files:
                f_path = parent / f
                if f_path.is_file() and f_path.suffix.lower() in extensions:
                    logger.info(f"Found image file: {f_path} ({f_path.stat().st_size} bytes)")
                    return f_path.read_bytes()
                
    except Exception as e:
        logger.error(f"Folder art search error in {abs_path}: {e}")
    return None

def write_music_tags(path: str, tags: dict) -> bool:
    """
    Write tags back to the music file.
    tags can contain: title, artist, date
    """
    try:
        # Use Easy mutagen interface where possible for simplicity
        audio = mutagen.File(path, easy=True)
        if audio is None:
            logger.error(f"Unsupported file format for writing: {path}")
            return False

        if "title" in tags:
            audio["title"] = tags["title"]
        if "artist" in tags:
            audio["artist"] = tags["artist"]
        if "date" in tags:
            # Easy mutagen handles 'date' for ID3 (TDRC), FLAC (DATE), etc.
            audio["date"] = str(tags["date"])

        audio.save()
        logger.info(f"Successfully wrote tags to {path}")
        
        # Update mtime in DB to prevent immediate re-scan from overwriting
        # (Though current scanner checks mtime, so we should update it)
        with get_connection() as conn:
            conn.execute("UPDATE music_tracks SET mtime = ? WHERE path = ?", (os.path.getmtime(path), path))
            
        return True
    except Exception as e:
        logger.error(f"Failed to write tags to {path}: {e}")
        return False

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

            # Keep track of albums scanned in this session to avoid redundant cover extraction
            scanned_albums = set()

            for music_dir in self.directories:
                if not os.path.isdir(music_dir): continue
                for root, _, files in os.walk(music_dir):
                    for fname in sorted(files):
                        if os.path.splitext(fname)[1].lower() not in AUDIO_EXTENSIONS: continue
                        path = os.path.normpath(os.path.join(root, fname))
                        found_paths.add(path)
                        
                        mtime = os.path.getmtime(path)
                        if cache.get(path) != mtime:
                            # Do NOT notify every single file to avoid flood
                            logger.info(f"Scanning: {path}")
                            tags = _read_music_tags(path)
                            if tags:
                                tags["path"] = path
                                tags["mtime"] = mtime
                                
                                # Optimization: only extract cover once per album during a scan
                                album_key = (tags["artist"], tags["album"])
                                if album_key in scanned_albums:
                                    tags["cover_path"] = None # Will be filled by DB's existing album info
                                else:
                                    scanned_albums.add(album_key)
                                
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
                    albums = [dict(row) for row in conn.execute("SELECT id, artist, title, mbid, cover_path FROM music_albums").fetchall()]
                else:
                    # Target albums with missing MBID or cover
                    albums = [dict(row) for row in conn.execute(
                        "SELECT id, artist, title, mbid, cover_path FROM music_albums WHERE mbid IS NULL OR cover_path IS NULL"
                    ).fetchall()]

            total = len(albums)
            for i, album in enumerate(albums, 1):
                album_id = album["id"]
                artist = album["artist"]
                title = album["title"]
                mbid = album["mbid"]
                
                if progress_cb:
                    progress_cb(f"Fetching metadata ({i}/{total}): {artist} - {title}")
                
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

    def enrich_metadata(self, progress_cb: Optional[Callable[[str], None]] = None):
        """Phase 2: Fetch missing video info from TMDB."""
        try:
            with get_connection() as conn:
                videos = [dict(row) for row in conn.execute(
                    "SELECT id, path, filename, title, series, season, episode, tmdb_id FROM video_files WHERE synopsis IS NULL"
                ).fetchall()]

            total = len(videos)
            for i, video in enumerate(videos, 1):
                path = video["path"]
                filename = video["filename"]
                
                # Determine search title
                search_title = video.get("series") or video.get("title")
                if not search_title:
                    from .utils import parse_video_filename
                    meta = parse_video_filename(filename)
                    search_title = meta.get("series") or meta.get("title")
                
                if not search_title:
                    continue

                is_tv = bool(video.get("series") or video.get("season"))
                
                if progress_cb:
                    progress_cb(f"Fetching TMDB ({i}/{total}): {search_title}")
                
                logger.info(f"Searching TMDB for: {search_title} (is_tv={is_tv})")
                meta = fetch_video_metadata(search_title, is_tv=is_tv)
                
                if meta:
                    updates = {}
                    for field in ["synopsis", "cast", "director", "year", "poster_path"]:
                        if meta.get(field):
                            updates[field] = meta[field]
                    
                    if updates:
                        with get_connection() as conn:
                            set_clause = ", ".join([f"{k} = ?" for k in updates.keys()])
                            params = list(updates.values())
                            params.append(path)
                            conn.execute(f"UPDATE video_files SET {set_clause} WHERE path = ?", params)
                            logger.info(f"Updated metadata for: {path}")

        except Exception as e:
            logger.exception("VideoLibrary.enrich_metadata failed")
