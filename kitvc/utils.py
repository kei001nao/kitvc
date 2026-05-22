import re
import subprocess
import os
from pathlib import Path
from .config import CONFIG_DIR

CACHE_DIR = Path.home() / ".cache" / "kitvc"
THUMB_DIR = CACHE_DIR / "thumbnails"

def parse_video_filename(filename: str) -> dict:
    """
    S01E01, 1x01, S1E1 などのパターンを解析する
    """
    patterns = [
        r"(.*)[sS](\d+)[eE](\d+)",  # S01E01
        r"(.*)(\d+)x(\d+)",         # 1x01
        r"(.*)Season\s*(\d+)\s*Episode\s*(\d+)" # Season 1 Episode 1
    ]
    
    for pattern in patterns:
        match = re.search(pattern, filename, re.IGNORECASE)
        if match:
            series = match.group(1).strip(" ._-")
            try:
                season = int(match.group(2))
                episode = int(match.group(3))
                return {
                    "series": series,
                    "season": season,
                    "episode": episode
                }
            except ValueError:
                continue
    return {}

def generate_thumbnail(video_path: str) -> str | None:
    """
    ffmpegを使用して動画からサムネイルを生成する
    """
    THUMB_DIR.mkdir(parents=True, exist_ok=True)
    
    # Hash path for unique filename
    import hashlib
    path_hash = hashlib.md5(video_path.encode()).hexdigest()
    thumb_path = THUMB_DIR / f"{path_hash}.jpg"
    
    if thumb_path.exists():
        return str(thumb_path)
    
    try:
        # Extract frame at 10 seconds or 10%
        # -ss 10 (seek to 10s), -vframes 1 (output 1 frame)
        cmd = [
            "ffmpeg", "-y",
            "-ss", "00:00:10",
            "-i", video_path,
            "-vframes", "1",
            "-q:v", "2",
            "-s", "320x180",
            str(thumb_path)
        ]
        # Run silently
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
        return str(thumb_path)
    except Exception:
        # Fallback if 10s is too long (e.g. short clips)
        try:
            cmd[2] = "00:00:01"
            subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
            return str(thumb_path)
        except Exception:
            return None
