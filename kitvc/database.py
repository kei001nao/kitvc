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
            CREATE TABLE IF NOT EXISTS music_albums (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                artist TEXT,
                title TEXT,
                release_date TEXT,
                cover_path TEXT,
                mbid TEXT,
                comment TEXT,
                UNIQUE(artist, title)
            )
        """)
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
                genre TEXT,
                bpm REAL,
                duration INTEGER,
                last_pos REAL DEFAULT 0,
                album_id INTEGER,
                FOREIGN KEY(album_id) REFERENCES music_albums(id)
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
                duration INTEGER DEFAULT 0,
                last_pos REAL DEFAULT 0,
                thumbnail_path TEXT,
                synopsis TEXT,
                cast TEXT,
                director TEXT,
                year INTEGER,
                tmdb_id TEXT,
                poster_path TEXT
            )
        """)
        # Separate Playlist tables for Music and Video
        conn.execute("""
            CREATE TABLE IF NOT EXISTS music_playlists (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT UNIQUE
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS music_playlist_tracks (
                playlist_id INTEGER,
                track_path TEXT,
                sort_order INTEGER,
                FOREIGN KEY(playlist_id) REFERENCES music_playlists(id) ON DELETE CASCADE
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS video_playlists (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT UNIQUE
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS video_playlist_files (
                playlist_id INTEGER,
                file_path TEXT,
                sort_order INTEGER,
                FOREIGN KEY(playlist_id) REFERENCES video_playlists(id) ON DELETE CASCADE
            )
        """)
        
        # Migration: Add missing columns if they don't exist
        for table, cols in {
            "music_tracks": [
                ("cover_path", "TEXT"), ("mbid", "TEXT"), ("comment", "TEXT"),
                ("release_date", "TEXT")
            ],
            "video_files": [
                ("duration", "INTEGER DEFAULT 0"),
                ("synopsis", "TEXT"), ("cast", "TEXT"), ("director", "TEXT"),
                ("year", "INTEGER"), ("tmdb_id", "TEXT"), ("poster_path", "TEXT")
            ]
        }.items():
            existing_cols = [row["name"] for row in conn.execute(f"PRAGMA table_info({table})").fetchall()]
            for col_name, col_type in cols:
                if col_name not in existing_cols:
                    conn.execute(f"ALTER TABLE {table} ADD COLUMN {col_name} {col_type}")

        # Special migration for music_tracks: copy year to release_date if year exists and release_date is empty
        existing_cols = [row["name"] for row in conn.execute("PRAGMA table_info(music_tracks)").fetchall()]
        if "year" in existing_cols and "release_date" in existing_cols:
            conn.execute("UPDATE music_tracks SET release_date = CAST(year AS TEXT) WHERE (release_date IS NULL OR release_date = '') AND year IS NOT NULL")
        
        if "album_id" not in existing_cols:
            conn.execute("ALTER TABLE music_tracks ADD COLUMN album_id INTEGER REFERENCES music_albums(id)")

        # Populate music_albums from music_tracks
        conn.execute("""
            INSERT OR IGNORE INTO music_albums (artist, title, release_date, cover_path, mbid, comment)
            SELECT artist, album, MAX(release_date), MAX(cover_path), MAX(mbid), MAX(comment)
            FROM music_tracks
            WHERE album IS NOT NULL
            GROUP BY artist, album
        """)
        
        # Link music_tracks to music_albums
        conn.execute("""
            UPDATE music_tracks
            SET album_id = (
                SELECT id FROM music_albums 
                WHERE music_albums.artist = music_tracks.artist 
                AND music_albums.title = music_tracks.album
            )
            WHERE album_id IS NULL
        """)

        # Migration: Move data from old tables if they exist
        try:
            # Check if old table exists
            old_p = conn.execute("SELECT name FROM sqlite_master WHERE type='table' AND name='playlists'").fetchone()
            if old_p:
                # Copy music playlists (assuming they were mixed before, but let's just copy everything to both for safety if user wants, 
                # or better, just copy to music as it was primarily used for music)
                conn.execute("INSERT OR IGNORE INTO music_playlists (id, name) SELECT id, name FROM playlists")
                conn.execute("INSERT OR IGNORE INTO music_playlist_tracks (playlist_id, track_path, sort_order) SELECT playlist_id, track_path, sort_order FROM playlist_tracks")
                # We can't easily distinguish video playlists from the old structure without checking paths, 
                # but let's assume video was also there.
                conn.execute("INSERT OR IGNORE INTO video_playlists (id, name) SELECT id, name FROM playlists")
                # Only insert video files into video_playlist_files
                conn.execute("""
                    INSERT OR IGNORE INTO video_playlist_files (playlist_id, file_path, sort_order) 
                    SELECT pt.playlist_id, pt.track_path, pt.sort_order 
                    FROM playlist_tracks pt
                    JOIN video_files vf ON pt.track_path = vf.path
                """)
                # Clean up music_playlist_tracks (remove videos)
                conn.execute("""
                    DELETE FROM music_playlist_tracks WHERE track_path IN (SELECT path FROM video_files)
                """)
                
                # Drop old tables
                conn.execute("DROP TABLE playlist_tracks")
                conn.execute("DROP TABLE playlists")
        except sqlite3.Error:
            pass

        conn.commit()

def get_playlists(is_video=False):
    table = "video_playlists" if is_video else "music_playlists"
    with get_connection() as conn:
        return [dict(row) for row in conn.execute(f"SELECT * FROM {table} ORDER BY name").fetchall()]

def create_playlist(name, is_video=False):
    table = "video_playlists" if is_video else "music_playlists"
    with get_connection() as conn:
        conn.execute(f"INSERT OR IGNORE INTO {table} (name) VALUES (?)", (name,))

def delete_playlist(playlist_id, is_video=False):
    p_table = "video_playlists" if is_video else "music_playlists"
    i_table = "video_playlist_files" if is_video else "music_playlist_tracks"
    with get_connection() as conn:
        conn.execute(f"DELETE FROM {p_table} WHERE id = ?", (playlist_id,))
        conn.execute(f"DELETE FROM {i_table} WHERE playlist_id = ?", (playlist_id,))

def add_to_playlist(playlist_id, path, is_video=False):
    table = "video_playlist_files" if is_video else "music_playlist_tracks"
    col = "file_path" if is_video else "track_path"
    with get_connection() as conn:
        order = conn.execute(f"SELECT COUNT(*) FROM {table} WHERE playlist_id = ?", (playlist_id,)).fetchone()[0]
        conn.execute(f"INSERT INTO {table} (playlist_id, {col}, sort_order) VALUES (?, ?, ?)", 
                     (playlist_id, path, order))

def get_playlist_items(playlist_id, is_video=False):
    i_table = "video_playlist_files" if is_video else "music_playlist_tracks"
    d_table = "video_files" if is_video else "music_tracks"
    col = "file_path" if is_video else "track_path"
    
    with get_connection() as conn:
        if is_video:
            query = f"SELECT d.* FROM {d_table} d JOIN {i_table} i ON d.path = i.{col} WHERE i.playlist_id = ? ORDER BY i.sort_order"
        else:
            query = f"""
                SELECT d.*, a.release_date, a.cover_path, a.mbid, a.comment as album_comment 
                FROM {d_table} d
                LEFT JOIN music_albums a ON d.album_id = a.id
                JOIN {i_table} i ON d.path = i.{col}
                WHERE i.playlist_id = ? 
                ORDER BY i.sort_order
            """
        
        items = [dict(row) for row in conn.execute(query, (playlist_id,)).fetchall()]
        
        for item in items:
            item["is_video"] = is_video
        return items

def get_playlist_tracks(playlist_id):
    # Compatibility wrapper for existing music logic
    return get_playlist_items(playlist_id, is_video=False)

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
        # 1. Ensure Album exists and has data
        existing_album = conn.execute(
            "SELECT id, release_date, cover_path, mbid, comment FROM music_albums WHERE artist = ? AND title = ?", 
            (track_data["artist"], track_data["album"])
        ).fetchone()

        if not existing_album:
            cursor = conn.execute("""
                INSERT INTO music_albums (artist, title, release_date, cover_path, mbid, comment)
                VALUES (?, ?, ?, ?, ?, ?)
            """, (
                track_data["artist"], track_data["album"],
                track_data.get("release_date"), track_data.get("cover_path"),
                track_data.get("mbid"), track_data.get("comment")
            ))
            album_id = cursor.lastrowid
        else:
            album_id = existing_album["id"]
            # Update album metadata if current track has more info (e.g. mbid found now)
            updates = {}
            for field in ["release_date", "cover_path", "mbid", "comment"]:
                if track_data.get(field) and not existing_album[field]:
                    updates[field] = track_data[field]
            
            if updates:
                set_clause = ", ".join([f"{k} = ?" for k in updates.keys()])
                conn.execute(f"UPDATE music_albums SET {set_clause} WHERE id = ?", (*updates.values(), album_id))

        # 2. Update/Insert Track (Note: redundant fields removed from SQL)
        conn.execute("""
            INSERT OR REPLACE INTO music_tracks (
                path, mtime, title, artist, album, album_artist, 
                track_num, disc_num, genre, bpm, duration, album_id
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            track_data["path"], track_data["mtime"], track_data["title"],
            track_data["artist"], track_data["album"], track_data["album_artist"],
            track_data["track_num"], track_data["disc_num"],
            track_data["genre"], track_data["bpm"], track_data["duration"],
            album_id
        ))

def get_album_by_id(album_id: int):
    with get_connection() as conn:
        return conn.execute("SELECT * FROM music_albums WHERE id = ?", (album_id,)).fetchone()

def remove_unused_albums():
    with get_connection() as conn:
        conn.execute("DELETE FROM music_albums WHERE id NOT IN (SELECT DISTINCT album_id FROM music_tracks)")


from .utils import parse_video_filename

def update_video_file(video_data):
    # Only update path, mtime, filename, size, duration if it's a new file or changed
    # Keep other manual fields if they exist
    with get_connection() as conn:
        existing = conn.execute("SELECT * FROM video_files WHERE path = ?", (video_data["path"],)).fetchone()
        if existing:
            conn.execute("""
                UPDATE video_files SET 
                    mtime = ?, filename = ?, size = ?, duration = ?, thumbnail_path = ?,
                    synopsis = ?, cast = ?, director = ?, year = ?, tmdb_id = ?, poster_path = ?
                WHERE path = ?
            """, (
                video_data["mtime"], video_data["filename"], video_data["size"], 
                video_data.get("duration", 0), video_data.get("thumbnail_path"),
                video_data.get("synopsis"), video_data.get("cast"), video_data.get("director"),
                video_data.get("year"), video_data.get("tmdb_id"), video_data.get("poster_path"),
                video_data["path"]
            ))
        else:
            # New file: Auto-classify
            meta = parse_video_filename(video_data["filename"])
            conn.execute("""
                INSERT INTO video_files (
                    path, mtime, filename, size, duration,
                    series, season, episode, thumbnail_path,
                    synopsis, cast, director, year, tmdb_id, poster_path
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, (
                video_data["path"], video_data["mtime"], video_data["filename"], 
                video_data["size"], video_data.get("duration", 0), meta.get("series"), 
                meta.get("season"), meta.get("episode"), video_data.get("thumbnail_path"),
                video_data.get("synopsis"), video_data.get("cast"), video_data.get("director"),
                video_data.get("year"), video_data.get("tmdb_id"), video_data.get("poster_path")
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

def remove_from_playlist(playlist_id, path, is_video=False):
    table = "video_playlist_files" if is_video else "music_playlist_tracks"
    col = "file_path" if is_video else "track_path"
    with get_connection() as conn:
        conn.execute(f"DELETE FROM {table} WHERE playlist_id = ? AND {col} = ?", (playlist_id, path))
        # Re-sort remaining items
        tracks = conn.execute(f"SELECT {col} FROM {table} WHERE playlist_id = ? ORDER BY sort_order", (playlist_id,)).fetchall()
        for i, t in enumerate(tracks):
            conn.execute(f"UPDATE {table} SET sort_order = ? WHERE playlist_id = ? AND {col} = ?", (i, playlist_id, t[col]))

def move_in_playlist(playlist_id, from_idx, to_idx, is_video=False):
    table = "video_playlist_files" if is_video else "music_playlist_tracks"
    col = "file_path" if is_video else "track_path"
    with get_connection() as conn:
        tracks = [row[col] for row in conn.execute(f"SELECT {col} FROM {table} WHERE playlist_id = ? ORDER BY sort_order", (playlist_id,)).fetchall()]
        if 0 <= from_idx < len(tracks) and 0 <= to_idx < len(tracks):
            path = tracks.pop(from_idx)
            tracks.insert(to_idx, path)
            for i, p in enumerate(tracks):
                conn.execute(f"UPDATE {table} SET sort_order = ? WHERE playlist_id = ? AND {col} = ?", (i, playlist_id, p))

def rename_playlist(playlist_id, new_name, is_video=False):
    table = "video_playlists" if is_video else "music_playlists"
    with get_connection() as conn:
        conn.execute(f"UPDATE {table} SET name = ? WHERE id = ?", (new_name, playlist_id))

def remove_missing_files(current_paths, table):
    if not current_paths:
        with get_connection() as conn:
            conn.execute(f"DELETE FROM {table}")
        return

    with get_connection() as conn:
        # Create a temp table to hold current paths
        conn.execute("CREATE TEMP TABLE current_scan_paths (path TEXT)")
        # Insert in chunks to avoid parameter limit
        chunk_size = 500
        for i in range(0, len(current_paths), chunk_size):
            chunk = current_paths[i:i + chunk_size]
            conn.executemany("INSERT INTO current_scan_paths VALUES (?)", [(p,) for p in chunk])
        
        # Delete records that are NOT in the temp table
        conn.execute(f"DELETE FROM {table} WHERE path NOT IN (SELECT path FROM current_scan_paths)")
        conn.execute("DROP TABLE current_scan_paths")
