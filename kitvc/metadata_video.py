from tmdbv3api import TMDb, Movie, TV, Season, Episode
import logging
import os

logger = logging.getLogger(__name__)

# Setup TMDB
tmdb = TMDb()

def _setup_tmdb():
    from .config import load_config
    config = load_config()
    api_key = config.get("video", {}).get("tmdb_api_key") or os.environ.get("TMDB_API_KEY")
    if api_key:
        tmdb.api_key = api_key
    else:
        tmdb.api_key = "YOUR_API_KEY"
    tmdb.language = "ja"

_setup_tmdb()

import requests
from pathlib import Path

def download_video_poster(url: str, target_dir: Path, name: str) -> str | None:
    """Download video poster and return local path."""
    if not url: return None
    try:
        target_dir.mkdir(parents=True, exist_ok=True)
        # Use a hash or ID for filename to avoid collisions
        import hashlib
        url_hash = hashlib.md5(url.encode()).hexdigest()
        target_path = target_dir / f"{name}_{url_hash}.jpg"
        
        if target_path.exists():
            return str(target_path)
            
        logger.info(f"Downloading video poster: {url}")
        response = requests.get(url, timeout=15)
        if response.status_code == 200:
            with open(target_path, "wb") as f:
                f.write(response.content)
            return str(target_path)
    except Exception as e:
        logger.error(f"Failed to download video poster: {e}")
    return None

def search_videos(title: str, is_tv=False, language: str = "ja"):
    """Search for video candidates on TMDB (Series or Movie)."""
    logger.info(f"--- TMDB Search List: '{title}' (is_tv={is_tv}, lang={language}) ---")
    if not tmdb.api_key or tmdb.api_key == "YOUR_API_KEY":
        return []
    
    tmdb.language = language
    results = []
    try:
        if is_tv:
            tv = TV()
            search_results = tv.search(title)
            for s in search_results:
                results.append({
                    "id": s.id,
                    "title": s.name,
                    "year": s.first_air_date[:4] if getattr(s, 'first_air_date', None) else "?",
                    "overview": s.overview
                })
        else:
            movie = Movie()
            search_results = movie.search(title)
            for m in search_results:
                results.append({
                    "id": m.id,
                    "title": m.title,
                    "year": m.release_date[:4] if getattr(m, 'release_date', None) else "?",
                    "overview": m.overview
                })
        logger.info(f"Found {len(results)} candidates for '{title}'")
    except Exception as e:
        logger.error(f"TMDB search failed: {e}")
    return results

def search_movie_exact(title: str, language: str = "ja"):
    """Search for a movie and return result ONLY if title matches exactly."""
    results = search_videos(title, is_tv=False, language=language)
    for r in results:
        if r["title"].strip().lower() == title.strip().lower():
            return r
    return None

def fetch_tv_seasons(tmdb_id: int, language: str = "ja"):
    """Fetch all seasons for a TV series."""
    tmdb.language = language
    try:
        tv = TV()
        show = tv.details(tmdb_id)
        seasons = []
        if hasattr(show, 'seasons'):
            for s in show.seasons:
                seasons.append({
                    "season_number": s.season_number,
                    "name": s.name,
                    "air_date": s.air_date[:4] if getattr(s, 'air_date', None) else "?",
                    "overview": s.overview
                })
        return seasons
    except Exception as e:
        logger.error(f"Failed to fetch seasons: {e}")
        return []

def fetch_tv_episodes(tmdb_id: int, season_number: int, language: str = "ja"):
    """Fetch all episodes for a TV season."""
    tmdb.language = language
    try:
        season_api = Season()
        season_details = season_api.details(tmdb_id, season_number)
        episodes = []
        if hasattr(season_details, 'episodes'):
            for e in season_details.episodes:
                episodes.append({
                    "episode_number": e.episode_number,
                    "name": e.name,
                    "air_date": e.air_date if getattr(e, 'air_date', None) else "?",
                    "overview": e.overview,
                    "still_path": f"https://image.tmdb.org/t/p/w500{e.still_path}" if getattr(e, 'still_path', None) else None
                })
        return episodes
    except Exception as e:
        logger.error(f"Failed to fetch episodes: {e}")
        return []

def fetch_video_details_by_id(tmdb_id: int, is_tv=False, language: str = "ja", season: int = None, episode: int = None):
    """Fetch unified metadata details by TMDB ID, with optional S/E."""
    logger.info(f"--- TMDB Fetch Details: ID={tmdb_id} (is_tv={is_tv}, lang={language}, S={season}, E={episode}) ---")
    tmdb.language = language
    try:
        if is_tv:
            tv = TV()
            show = tv.details(tmdb_id, append_to_response="credits")
            
            first_air = getattr(show, 'first_air_date', None)
            
            # Series base info
            res = {
                "tmdb_id": tmdb_id,
                "is_tv": True,
                "language": language,
                "type": "TV Show",
                "series": getattr(show, 'name', 'Unknown'),
                "series_overview": getattr(show, 'overview', ''),
                "first_air_date": first_air,
                "series_poster_path": f"https://image.tmdb.org/t/p/w500{show.poster_path}" if getattr(show, 'poster_path', None) else None,
                "poster_path": f"https://image.tmdb.org/t/p/w500{show.poster_path}" if getattr(show, 'poster_path', None) else None,
                "genres": ", ".join([g['name'] for g in show.genres]) if hasattr(show, 'genres') and show.genres else None,
                "year": int(first_air[:4]) if first_air and len(first_air) >= 4 else None,
                "air_date": first_air,
                "title": getattr(show, 'name', 'Unknown'),
                "synopsis": getattr(show, 'overview', '')
            }
            
            # Cast safety
            credits = getattr(show, 'credits', None)
            if credits:
                cast = getattr(credits, 'cast', None)
                if isinstance(cast, list):
                    res["cast"] = ", ".join([c.name for c in cast[:10]])

            # If Season is provided
            if season is not None:
                try:
                    season_api = Season()
                    s_details = season_api.details(tmdb_id, season)
                    if s_details:
                        res["season_name"] = getattr(s_details, 'name', None)
                        res["season_overview"] = getattr(s_details, 'overview', None)
                        if getattr(s_details, 'poster_path', None):
                            res["poster_path"] = f"https://image.tmdb.org/t/p/w500{s_details.poster_path}"
                except Exception as e:
                    logger.warning(f"Could not fetch season {season} details: {e}")

            # If Episode is provided
            if season is not None and episode is not None:
                try:
                    ep_api = Episode()
                    ep_details = ep_api.details(tmdb_id, season, episode)
                    if ep_details:
                        res["title"] = getattr(ep_details, 'name', res["series"])
                        res["synopsis"] = getattr(ep_details, 'overview', res["series_overview"])
                        res["episode_overview"] = getattr(ep_details, 'overview', '')
                        res["air_date"] = getattr(ep_details, 'air_date', res["first_air_date"])
                        if getattr(ep_details, 'still_path', None):
                            res["still_path"] = f"https://image.tmdb.org/t/p/w500{ep_details.still_path}"
                        logger.info(f"Episode found: {res['title']}")
                except Exception as e:
                    logger.warning(f"Could not fetch episode {season}E{episode} details: {e}")
            
            return res
        else:
            movie = Movie()
            m = movie.details(tmdb_id, append_to_response="credits")
            rel_date = getattr(m, 'release_date', None)
            res = {
                "tmdb_id": tmdb_id,
                "is_tv": False,
                "language": language,
                "type": "Movie",
                "title": getattr(m, 'title', 'Unknown'),
                "synopsis": getattr(m, 'overview', ''),
                "air_date": rel_date,
                "poster_path": f"https://image.tmdb.org/t/p/w500{m.poster_path}" if getattr(m, 'poster_path', None) else None,
                "year": int(rel_date[:4]) if rel_date and len(rel_date) >= 4 else None,
                "genres": ", ".join([g['name'] for g in m.genres]) if hasattr(m, 'genres') and m.genres else None
            }
            # Credits safety
            credits = getattr(m, 'credits', None)
            if credits:
                cast = getattr(credits, 'cast', None)
                if isinstance(cast, list):
                    res["cast"] = ", ".join([c.name for c in cast[:10]])
                
                crew = getattr(credits, 'crew', None)
                if isinstance(crew, list):
                    res["director"] = ", ".join([c.name for c in crew if getattr(c, 'job', '') == 'Director'])
            return res
    except Exception as e:
        logger.error(f"TMDB fetch details failed: {e}", exc_info=True)
    return None

def fetch_video_metadata(title: str, is_tv=False, language: str = "ja"):
    """Fetch video info from TMDB (Legacy/Auto-fetch)."""
    # This is still used by background scanner, but might need update or deprecation
    # For now, keep it simple using fetch_video_details_by_id logic
    results = search_videos(title, is_tv=is_tv, language=language)
    if results:
        return fetch_video_details_by_id(results[0]["id"], is_tv=is_tv, language=language)
    return None
