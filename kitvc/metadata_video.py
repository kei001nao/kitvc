from tmdbv3api import TMDb, Movie, TV
import logging
import os

logger = logging.getLogger(__name__)

# Setup TMDB
tmdb = TMDb()
tmdb.api_key = os.environ.get("TMDB_API_KEY", "YOUR_API_KEY")
tmdb.language = "ja"

def fetch_video_metadata(title: str, is_tv=False):
    """Fetch video info from TMDB."""
    if not tmdb.api_key or tmdb.api_key == "YOUR_API_KEY":
        return None
        
    try:
        if is_tv:
            tv = TV()
            search_results = tv.search(title)
            if search_results:
                show = tv.details(search_results[0].id)
                return {
                    "synopsis": show.overview,
                    "poster_path": f"https://image.tmdb.org/t/p/w500{show.poster_path}" if show.poster_path else None,
                    "cast": ", ".join([c.name for c in show.credits.cast[:5]]) if hasattr(show, 'credits') else None,
                    "year": int(show.first_air_date[:4]) if getattr(show, 'first_air_date', None) else None
                }
        else:
            movie = Movie()
            search_results = movie.search(title)
            if search_results:
                m = movie.details(search_results[0].id)
                return {
                    "synopsis": m.overview,
                    "poster_path": f"https://image.tmdb.org/t/p/w500{m.poster_path}" if m.poster_path else None,
                    "cast": ", ".join([c.name for c in m.credits.cast[:5]]) if hasattr(m, 'credits') else None,
                    "director": ", ".join([c.name for c in m.credits.crew if c.job == 'Director']) if hasattr(m, 'credits') else None,
                    "year": int(m.release_date[:4]) if getattr(m, 'release_date', None) else None
                }
    except Exception as e:
        logger.error(f"TMDB fetch failed: {e}")
    return None
