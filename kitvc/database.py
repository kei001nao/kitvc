import sqlite3
from pathlib import Path
from .config import CONFIG_DIR

DB_PATH = CONFIG_DIR / "library.db"

def get_connection():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn

def init_db():
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    with get_connection() as conn:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS music_tracks (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                path TEXT UNIQUE,
                mtime REAL,
                title TEXT,
                artist TEXT,
                album TEXT,
                album_artist TEXT,
                track_num INTEGER,
                disc_num INTEGER,
                year INTEGER,
                genre TEXT,
                bpm REAL,
                duration INTEGER,
                last_pos REAL DEFAULT 0
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS video_files (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                path TEXT UNIQUE,
                mtime REAL,
                filename TEXT,
                size INTEGER,
                type TEXT,
                category TEXT,
                series TEXT,
                season INTEGER,
                episode INTEGER,
                title TEXT,
                last_pos REAL DEFAULT 0,
                thumbnail_path TEXT
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS playlists (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT UNIQUE
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS playlist_tracks (
                playlist_id INTEGER,
                track_path TEXT,
                sort_order INTEGER,
                FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE
            )
        """)
        conn.commit()

def get_playlists():
    with get_connection() as conn:
        return [dict(row) for row in conn.execute("SELECT * FROM playlists ORDER BY name").fetchall()]

def create_playlist(name):
    with get_connection() as conn:
        conn.execute("INSERT OR IGNORE INTO playlists (name) VALUES (?)", (name,))

def delete_playlist(playlist_id):
    with get_connection() as conn:
        conn.execute("DELETE FROM playlists WHERE id = ?", (playlist_id,))
        conn.execute("DELETE FROM playlist_tracks WHERE playlist_id = ?", (playlist_id,))

def add_to_playlist(playlist_id, track_path):
    with get_connection() as conn:
        order = conn.execute("SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = ?", (playlist_id,)).fetchone()[0]
        conn.execute("INSERT INTO playlist_tracks (playlist_id, track_path, sort_order) VALUES (?, ?, ?)", 
                     (playlist_id, track_path, order))

def get_playlist_items(playlist_id):
    with get_connection() as conn:
        paths = [dict(row) for row in conn.execute("""
            SELECT track_path, sort_order FROM playlist_tracks 
            WHERE playlist_id = ? 
            ORDER BY sort_order
        """, (playlist_id,)).fetchall()]
        
        items = []
        for p in paths:
            # Try music first
            m = conn.execute("SELECT * FROM music_tracks WHERE path = ?", (p["track_path"],)).fetchone()
            if m:
                item = dict(m)
                item["is_video"] = False
                items.append(item)
            else:
                # Try video
                v = conn.execute("SELECT * FROM video_files WHERE path = ?", (p["track_path"],)).fetchone()
                if v:
                    item = dict(v)
                    item["is_video"] = True
                    items.append(item)
        return items

def get_playlist_tracks(playlist_id):
    # Compatibility wrapper for existing music logic
    return [i for i in get_playlist_items(playlist_id) if not i.get("is_video")]

def save_playback_position(path, pos, is_video=False):
    table = "video_files" if is_video else "music_tracks"
    with get_connection() as conn:
        conn.execute(f"UPDATE {table} SET last_pos = ? WHERE path = ?", (pos, path))

def get_playback_position(path, is_video=False):
    table = "video_files" if is_video else "music_tracks"
    with get_connection() as conn:
        row = conn.execute(f"SELECT last_pos FROM {table} WHERE path = ?", (path,)).fetchone()
        return row["last_pos"] if row else 0

def update_music_track(track_data):
    with get_connection() as conn:
        conn.execute("""
            INSERT OR REPLACE INTO music_tracks (
                path, mtime, title, artist, album, album_artist, 
                track_num, disc_num, year, genre, bpm, duration
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            track_data["path"], track_data["mtime"], track_data["title"],
            track_data["artist"], track_data["album"], track_data["album_artist"],
            track_data["track_num"], track_data["disc_num"], track_data["year"],
            track_data["genre"], track_data["bpm"], track_data["duration"]
        ))

from .utils import parse_video_filename

def update_video_file(video_data):
    # Only update path, mtime, filename, size if it's a new file or changed
    # Keep other manual fields if they exist
    with get_connection() as conn:
        existing = conn.execute("SELECT * FROM video_files WHERE path = ?", (video_data["path"],)).fetchone()
        if existing:
            conn.execute("""
                UPDATE video_files SET 
                    mtime = ?, filename = ?, size = ?, thumbnail_path = ?
                WHERE path = ?
            """, (video_data["mtime"], video_data["filename"], video_data["size"], 
                  video_data.get("thumbnail_path"), video_data["path"]))
        else:
            # New file: Auto-classify
            meta = parse_video_filename(video_data["filename"])
            conn.execute("""
                INSERT INTO video_files (
                    path, mtime, filename, size, 
                    series, season, episode, thumbnail_path
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """, (
                video_data["path"], video_data["mtime"], video_data["filename"], 
                video_data["size"], meta.get("series"), meta.get("season"), 
                meta.get("episode"), video_data.get("thumbnail_path")
            ))

def update_video_manual_fields(path, fields):
    # fields is a dict of category, series, season, episode, title, type
    allowed = {"type", "category", "series", "season", "episode", "title"}
    sets = []
    values = []
    for k, v in fields.items():
        if k in allowed:
            sets.append(f"{k} = ?")
            values.append(v)
    
    if not sets:
        return
    
    values.append(path)
    with get_connection() as conn:
        conn.execute(f"UPDATE video_files SET {', '.join(sets)} WHERE path = ?", tuple(values))

def remove_from_playlist(playlist_id, track_path):
    with get_connection() as conn:
        conn.execute("DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_path = ?", (playlist_id, track_path))
        # Re-sort remaining items
        tracks = conn.execute("SELECT track_path FROM playlist_tracks WHERE playlist_id = ? ORDER BY sort_order", (playlist_id,)).fetchall()
        for i, t in enumerate(tracks):
            conn.execute("UPDATE playlist_tracks SET sort_order = ? WHERE playlist_id = ? AND track_path = ?", (i, playlist_id, t["track_path"]))

def move_in_playlist(playlist_id, from_idx, to_idx):
    with get_connection() as conn:
        tracks = [row["track_path"] for row in conn.execute("SELECT track_path FROM playlist_tracks WHERE playlist_id = ? ORDER BY sort_order", (playlist_id,)).fetchall()]
        if 0 <= from_idx < len(tracks) and 0 <= to_idx < len(tracks):
            path = tracks.pop(from_idx)
            tracks.insert(to_idx, path)
            for i, p in enumerate(tracks):
                conn.execute("UPDATE playlist_tracks SET sort_order = ? WHERE playlist_id = ? AND track_path = ?", (i, playlist_id, p))

def rename_playlist(playlist_id, new_name):
    with get_connection() as conn:
        conn.execute("UPDATE playlists SET name = ? WHERE id = ?", (new_name, playlist_id))

def remove_missing_files(current_paths, table):
    # This is a bit slow for many files, but okay for starters
    with get_connection() as conn:
        conn.execute(f"DELETE FROM {table} WHERE path NOT IN ({','.join(['?']*len(current_paths))})", tuple(current_paths))
