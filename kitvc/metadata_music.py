import musicbrainzngs
import logging
import os
import requests
from pathlib import Path
from .database import get_connection

logger = logging.getLogger(__name__)

# Setup MusicBrainz
musicbrainzngs.set_useragent("kitvc", "0.1", "https://github.com/kei/kitvc")

def fetch_music_metadata(mbid: str):
    """
    Fetch additional info from MusicBrainz.
    mbid: MusicBrainz Release ID
    """
    if not mbid:
        return None
    try:
        # Fetch release info with extra data
        result = musicbrainzngs.get_release_by_id(
            mbid, 
            includes=["artists", "release-groups", "labels", "recordings"]
        )
        release = result.get("release", {})
        
        # Extract fields
        data = {
            "title": release.get("title"),
            "date": release.get("date"),
            "country": release.get("country"),
            "barcode": release.get("barcode"),
            "label": release.get("label-info-list", [{}])[0].get("label", {}).get("name") if release.get("label-info-list") else None,
            "comment": release.get("annotation"),
        }
        
        # Cover Art Archive Link (Standard location)
        # http://coverartarchive.org/release/<mbid>/front
        data["cover_url"] = f"http://coverartarchive.org/release/{mbid}/front"
        
        return data
    except Exception as e:
        logger.error(f"MusicBrainz fetch failed: {e}")
        return None

def download_cover_art(mbid: str, target_dir: Path) -> str | None:
    """Download cover art from Cover Art Archive and return local path."""
    try:
        target_dir.mkdir(parents=True, exist_ok=True)
        target_path = target_dir / f"{mbid}.jpg"
        
        if target_path.exists():
            return str(target_path)
            
        url = f"http://coverartarchive.org/release/{mbid}/front"
        logger.info(f"Downloading cover art: {url}")
        
        response = requests.get(url, timeout=15)
        if response.status_code == 200:
            with open(target_path, "wb") as f:
                f.write(response.content)
            return str(target_path)
        else:
            logger.warning(f"Cover art not found or error: {response.status_code}")
    except Exception as e:
        logger.error(f"Failed to download cover art: {e}")
    return None

def search_release(artist: str, album: str):
    """Search for a release to get its MBID if not present in tags."""
    try:
        query = f"artist:\"{artist}\" AND release:\"{album}\""
        result = musicbrainzngs.search_releases(query=query, limit=1)
        if result.get("release-list"):
            return result["release-list"][0]["id"]
    except Exception as e:
        logger.error(f"MusicBrainz search failed: {e}")
    return None
