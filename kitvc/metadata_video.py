from tmdbv3api import TMDb, Movie, TV, Episode
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

def search_videos(title: str, is_tv=False, language: str = "ja"):
    """Search for video candidates on TMDB."""
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

def fetch_video_details_by_id(tmdb_id: int, is_tv=False, language: str = "ja", season: int = None, episode: int = None):
    """Fetch details for a specific TMDB ID, optionally for a specific season or episode."""
    logger.info(f"--- TMDB Fetch Details: ID={tmdb_id} (is_tv={is_tv}, lang={language}, S={season}, E={episode}) ---")
    tmdb.language = language
    try:
        if is_tv:
            tv = TV()
            show = tv.details(tmdb_id)
            
            # Default to series info
            res = {
                "synopsis": show.overview,
                "poster_path": f"https://image.tmdb.org/t/p/w500{show.poster_path}" if show.poster_path else None,
                "cast": ", ".join([c.name for c in show.credits.cast[:5]]) if hasattr(show, 'credits') else None,
                "year": int(show.first_air_date[:4]) if getattr(show, 'first_air_date', None) else None,
                "title": show.name
            }

            # If Season and Episode are provided, try to get episode details
            if season is not None and episode is not None:
                try:
                    ep_api = Episode()
                    ep_details = ep_api.details(tmdb_id, season, episode)
                    if ep_details:
                        res["title"] = f"{show.name} - {ep_details.name}" if ep_details.name else show.name
                        if ep_details.overview:
                            res["synopsis"] = ep_details.overview
                        logger.info(f"Episode found: {res['title']}")
                except Exception as e:
                    logger.warning(f"Could not fetch episode {season}E{episode}: {e}")
            
            # If only Season is provided, maybe update synopsis if season has one
            elif season is not None:
                try:
                    # TMDB also has Season details if needed, but for now we'll stick to this
                    pass
                except Exception:
                    pass
            
            return res
        else:
            movie = Movie()
            m = movie.details(tmdb_id)
            res = {
                "synopsis": m.overview,
                "poster_path": f"https://image.tmdb.org/t/p/w500{m.poster_path}" if m.poster_path else None,
                "cast": ", ".join([c.name for c in m.credits.cast[:5]]) if hasattr(m, 'credits') else None,
                "director": ", ".join([c.name for c in m.credits.crew if c.job == 'Director']) if hasattr(m, 'credits') else None,
                "year": int(m.release_date[:4]) if getattr(m, 'release_date', None) else None,
                "title": m.title
            }
            return res
    except Exception as e:
        logger.error(f"TMDB fetch details failed: {e}")
    return None

def fetch_video_metadata(title: str, is_tv=False, language: str = "ja"):
    """Fetch video info from TMDB."""
    logger.info(f"--- TMDB Fetch Start: '{title}' (is_tv={is_tv}, lang={language}) ---")
    if not tmdb.api_key or tmdb.api_key == "YOUR_API_KEY":
        logger.error("TMDB API Key is missing or invalid.")
        return None
    
    # Store original language to restore later if needed, 
    # but tmdb instance is shared, so let's just set it.
    tmdb.language = language
        
    try:
        if is_tv:
            tv = TV()
            logger.info(f"Searching TV shows for: '{title}'...")
            search_results = tv.search(title)
            if search_results:
                logger.info(f"Match found! Total results: {len(search_results)}. Using best match: ID={search_results[0].id}, Name='{search_results[0].name}'")
                show = tv.details(search_results[0].id)
                res = {
                    "synopsis": show.overview,
                    "poster_path": f"https://image.tmdb.org/t/p/w500{show.poster_path}" if show.poster_path else None,
                    "cast": ", ".join([c.name for c in show.credits.cast[:5]]) if hasattr(show, 'credits') else None,
                    "year": int(show.first_air_date[:4]) if getattr(show, 'first_air_date', None) else None
                }
                logger.info(f"Fetched TV metadata: {res}")
                return res
            else:
                logger.warning(f"No TV show matches found for: '{title}'")
        else:
            movie = Movie()
            logger.info(f"Searching movies for: '{title}'...")
            search_results = movie.search(title)
            if search_results:
                logger.info(f"Match found! Total results: {len(search_results)}. Using best match: ID={search_results[0].id}, Title='{search_results[0].title}'")
                m = movie.details(search_results[0].id)
                res = {
                    "synopsis": m.overview,
                    "poster_path": f"https://image.tmdb.org/t/p/w500{m.poster_path}" if m.poster_path else None,
                    "cast": ", ".join([c.name for c in m.credits.cast[:5]]) if hasattr(m, 'credits') else None,
                    "director": ", ".join([c.name for c in m.credits.crew if c.job == 'Director']) if hasattr(m, 'credits') else None,
                    "year": int(m.release_date[:4]) if getattr(m, 'release_date', None) else None
                }
                logger.info(f"Fetched Movie metadata: {res}")
                return res
            else:
                logger.warning(f"No movie matches found for: '{title}'")
    except Exception as e:
        logger.error(f"TMDB fetch failed with exception: {e}")
    return None
