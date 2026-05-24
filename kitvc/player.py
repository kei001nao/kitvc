import asyncio
import json
import os
from pathlib import Path
from typing import Callable, Optional

INFO_SOCKET = "/tmp/kitvc-info.sock"
MUSIC_SOCKET = "/tmp/kitvc-music.sock"
VIDEO_SOCKET = "/tmp/kitvc-video.sock"

MPV_BASE_ARGS = [
    "mpv",
    "--idle=yes",
    "--really-quiet",
]

class MpvInstance:
    def __init__(self, socket_path: str, extra_args: list[str] = None):
        self.socket_path = socket_path
        self.extra_args = extra_args or []
        self._process: Optional[asyncio.subprocess.Process] = None
        self._reader: Optional[asyncio.StreamReader] = None
        self._writer: Optional[asyncio.StreamWriter] = None
        self._req_id = 0
        self._pending: dict[int, asyncio.Future] = {}
        self._read_task: Optional[asyncio.Task] = None
        self.on_event: list[Callable[[dict], None]] = []

    async def start(self) -> None:
        if Path(self.socket_path).exists():
            try:
                os.unlink(self.socket_path)
            except OSError:
                pass

        args = list(MPV_BASE_ARGS) + [f"--input-ipc-server={self.socket_path}"] + self.extra_args
        self._process = await asyncio.create_subprocess_exec(
            *args,
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.DEVNULL,
        )

        for _ in range(50):
            if Path(self.socket_path).exists():
                break
            await asyncio.sleep(0.1)
        else:
            raise RuntimeError(f"mpv IPC socket {self.socket_path} did not appear")

        self._reader, self._writer = await asyncio.open_unix_connection(self.socket_path)
        self._read_task = asyncio.create_task(self._read_loop())

    async def _read_loop(self) -> None:
        while True:
            try:
                line = await self._reader.readline()
                if not line:
                    break
                data = json.loads(line.decode())
                req_id = data.get("request_id")
                if req_id is not None:
                    fut = self._pending.pop(req_id, None)
                    if fut and not fut.done():
                        fut.set_result(data.get("data"))
                
                if "event" in data:
                    for cb in self.on_event:
                        if asyncio.iscoroutinefunction(cb):
                            asyncio.create_task(cb(data))
                        else:
                            cb(data)
            except asyncio.CancelledError:
                break
            except Exception:
                pass

    async def cmd(self, command: list, *, wait: bool = False):
        if not self._writer:
            return None
        self._req_id += 1
        req_id = self._req_id
        loop = asyncio.get_running_loop()
        fut: asyncio.Future = loop.create_future()
        self._pending[req_id] = fut
        msg = json.dumps({"command": command, "request_id": req_id}) + "\n"
        try:
            self._writer.write(msg.encode())
            await self._writer.drain()
        except (ConnectionError, BrokenPipeError, OSError):
            return None

        if wait:
            try:
                return await asyncio.wait_for(asyncio.shield(fut), timeout=3.0)
            except asyncio.TimeoutError:
                self._pending.pop(req_id, None)
                return None
            except asyncio.CancelledError:
                return None
        return None

    async def shutdown(self) -> None:
        if self._read_task:
            self._read_task.cancel()
        if self._writer:
            self._writer.close()
            try:
                await self._writer.wait_closed()
            except Exception:
                pass
        if self._process:
            try:
                self._process.terminate()
                await self._process.wait()
            except Exception:
                pass
        if Path(self.socket_path).exists():
            try:
                os.unlink(self.socket_path)
            except OSError:
                pass

class MpvInfo:
    def __init__(self):
        self.mpv = MpvInstance(INFO_SOCKET, ["--vid=no", "--vo=null"])

    async def start(self) -> None:
        await self.mpv.start()

    async def get_metadata(self, path: str) -> dict:
        await self.mpv.cmd(["loadfile", path, "replace"])
        await asyncio.sleep(0.2)
        meta = await self.mpv.cmd(["get_property", "metadata"], wait=True) or {}
        duration = await self.mpv.cmd(["get_property", "duration"], wait=True) or 0
        width = await self.mpv.cmd(["get_property", "width"], wait=True) or 0
        height = await self.mpv.cmd(["get_property", "height"], wait=True) or 0
        
        res = dict(meta)
        res["duration"] = int(duration)
        res["width"] = width
        res["height"] = height
        return res

    async def shutdown(self) -> None:
        await self.mpv.shutdown()

class MusicPlayer:
    def __init__(self, config: dict | None = None):
        self.config = config or {}
        self.volume: int = self.config.get("player", {}).get("volume", 80)
        self.repeat: bool = False
        # Enable gapless audio and prefetching
        self.mpv = MpvInstance(MUSIC_SOCKET, [
            "--vid=no", 
            "--vo=null", 
            "--gapless-audio=yes", 
            "--prefetch-playlist=yes"
        ])
        self.mpv.on_event.append(self._handle_event)
        
        self.on_track_start: list[Callable] = []
        self.on_track_end: list[Callable] = []
        self._queue: list[dict] = []
        self._current_idx: int = -1
        self._paused: bool = False

    async def start(self) -> None:
        await self.mpv.start()
        await self.set_volume(self.volume)

    async def _handle_event(self, data: dict) -> None:
        event = data.get("event")
        if event == "file-loaded":
            # Try to synchronize current_idx if mpv moved to next automatically
            # We can use metadata or path to match, but path is safer.
            curr_path = await self.get_property("path")
            if curr_path:
                for i, track in enumerate(self._queue):
                    if track["path"] == curr_path:
                        self._current_idx = i
                        break
            
            # Fire start callback
            for cb in self.on_track_start:
                if asyncio.iscoroutinefunction(cb):
                    asyncio.create_task(cb())
                else:
                    cb()
            
            # Prefetch NEXT track to mpv playlist for gapless
            if self._current_idx + 1 < len(self._queue):
                next_track = self._queue[self._current_idx + 1]
                await self.mpv.cmd(["loadfile", next_track["path"], "append"])
            elif self.repeat and self._queue:
                # Loop back to start if repeat is on
                await self.mpv.cmd(["loadfile", self._queue[0]["path"], "append"])

        elif event == "end-file":
            # Fire end callback
            for cb in self.on_track_end:
                if asyncio.iscoroutinefunction(cb):
                    asyncio.create_task(cb())
                else:
                    cb()
            # Note: We NO LONGER call self.next() here because mpv 
            # autonomously plays the next appended file.

    async def play(self, url: str) -> None:
        self._paused = False
        await self.mpv.cmd(["set_property", "pause", False])
        # Use 'replace' to clear mpv's internal playlist and start fresh
        await self.mpv.cmd(["loadfile", url, "replace"])

    def add_to_queue(self, tracks: list[dict]) -> None:
        was_empty = len(self._queue) == 0
        self._queue.extend(tracks)
        # If nothing was playing and we just added tracks, maybe we should prefetch?
        # But usually play_from_queue or play_queue will be called.

    async def play_from_queue(self, idx: int) -> None:
        if 0 <= idx < len(self._queue):
            self._current_idx = idx
            track = self._queue[idx]
            # Replace current playback with selected track
            await self.play(track["path"])
            # The 'file-loaded' event will trigger and prefetch the next track.

    async def play_queue(self, tracks: list[dict], start_idx: int = 0) -> None:
        self._queue = tracks
        await self.play_from_queue(start_idx)

    async def next(self) -> None:
        # User manually triggered next
        if 0 <= self._current_idx < len(self._queue) - 1:
            await self.play_from_queue(self._current_idx + 1)
        elif self.repeat and self._queue:
            await self.play_from_queue(0)

    async def prev(self) -> None:
        # User manually triggered prev
        if self._current_idx > 0:
            await self.play_from_queue(self._current_idx - 1)
        elif self.repeat and self._queue:
            await self.play_from_queue(len(self._queue) - 1)

    async def _sync_mpv_playlist(self) -> None:
        """Synchronize mpv internal playlist with current Python _queue."""
        if not self.mpv:
            return
            
        # Get mpv's internal playlist
        playlist = await self.mpv.cmd(["get_property", "playlist"], wait=True)
        if not playlist:
            return
            
        # Find the index of the currently playing item in mpv's list
        current_id = -1
        for i, entry in enumerate(playlist):
            if entry.get("current"):
                current_id = i
                break
        
        if current_id != -1:
            # Remove all items after the current one in mpv's internal playlist
            # We go backwards to keep indices stable while removing
            for i in range(len(playlist) - 1, current_id, -1):
                await self.mpv.cmd(["playlist-remove", i])
        
        # Append the correct next track from our Python queue
        if self._current_idx + 1 < len(self._queue):
            next_track = self._queue[self._current_idx + 1]
            await self.mpv.cmd(["loadfile", next_track["path"], "append"])
        elif self.repeat and self._queue:
            await self.mpv.cmd(["loadfile", self._queue[0]["path"], "append"])

    def remove_from_queue(self, index: int) -> None:
        if 0 <= index < len(self._queue):
            self._queue.pop(index)
            if index < self._current_idx:
                self._current_idx -= 1
            elif index == self._current_idx:
                self._current_idx = -1
            
            # Synchronize mpv
            asyncio.create_task(self._sync_mpv_playlist())

    def move_in_queue(self, from_idx: int, to_idx: int) -> None:
        if 0 <= from_idx < len(self._queue) and 0 <= to_idx < len(self._queue):
            track = self._queue.pop(from_idx)
            self._queue.insert(to_idx, track)
            if self._current_idx == from_idx:
                self._current_idx = to_idx
            elif from_idx < self._current_idx <= to_idx:
                self._current_idx -= 1
            elif to_idx <= self._current_idx < from_idx:
                self._current_idx += 1
            
            # Synchronize mpv
            asyncio.create_task(self._sync_mpv_playlist())

    def get_current_track(self) -> Optional[dict]:
        if 0 <= self._current_idx < len(self._queue):
            return self._queue[self._current_idx]
        return None

    async def toggle_pause(self) -> None:
        self._paused = not self._paused
        await self.mpv.cmd(["cycle", "pause"])

    async def stop(self) -> None:
        await self.mpv.cmd(["stop"])

    def clear_queue(self) -> None:
        self._queue = []
        self._current_idx = -1

    async def seek(self, seconds: float) -> None:
        await self.mpv.cmd(["seek", seconds, "absolute"])

    async def set_volume(self, level: int) -> None:
        self.volume = max(0, min(100, level))
        await self.mpv.cmd(["set_property", "volume", self.volume])

    async def get_property(self, prop: str):
        return await self.mpv.cmd(["get_property", prop], wait=True)

    async def get_position(self) -> Optional[float]:
        return await self.get_property("time-pos")

    async def get_duration(self) -> Optional[float]:
        return await self.get_property("duration")

    async def shutdown(self) -> None:
        await self.mpv.shutdown()

class VideoPlayer:
    def __init__(self, config: dict | None = None):
        self.config = config or {}
        self.volume: int = self.config.get("player", {}).get("volume", 80)
        self.repeat: bool = False
        self._queue: list[dict] = []
        self._current_idx: int = -1
        self._paused: bool = False
        self.mpv: Optional[MpvInstance] = None
        
        self.on_track_start: list[Callable] = []
        self.on_track_end: list[Callable] = []

    async def _ensure_mpv(self) -> None:
        if not self.mpv:
            self.mpv = MpvInstance(VIDEO_SOCKET, self.config.get("player", {}).get("mpv_args", []))
            self.mpv.on_event.append(self._handle_event)
            await self.mpv.start()
            await self.set_volume(self.volume)

    async def _handle_event(self, data: dict) -> None:
        event = data.get("event")
        if event == "file-loaded":
            for cb in self.on_track_start:
                if asyncio.iscoroutinefunction(cb):
                    asyncio.create_task(cb())
                else:
                    cb()
        elif event == "end-file":
            reason = data.get("reason")
            if reason == "eof":
                for cb in self.on_track_end:
                    if asyncio.iscoroutinefunction(cb):
                        asyncio.create_task(cb())
                    else:
                        cb()
                asyncio.create_task(self.next())

    async def play(self, url: str) -> None:
        await self._ensure_mpv()
        self._paused = False
        await self.mpv.cmd(["set_property", "pause", False])
        await self.mpv.cmd(["loadfile", url, "replace"])

    def add_to_queue(self, videos: list[dict]) -> None:
        self._queue.extend(videos)

    async def play_from_queue(self, idx: int) -> None:
        if 0 <= idx < len(self._queue):
            self._current_idx = idx
            video = self._queue[idx]
            await self.play(video["path"])

    async def play_queue(self, videos: list[dict], start_idx: int = 0) -> None:
        self._queue = videos
        await self.play_from_queue(start_idx)

    async def next(self) -> None:
        if 0 <= self._current_idx < len(self._queue) - 1:
            await self.play_from_queue(self._current_idx + 1)
        elif self.repeat and self._queue:
            await self.play_from_queue(0)

    async def prev(self) -> None:
        if self._current_idx > 0:
            await self.play_from_queue(self._current_idx - 1)

    def remove_from_queue(self, index: int) -> None:
        if 0 <= index < len(self._queue):
            self._queue.pop(index)
            if index < self._current_idx:
                self._current_idx -= 1
            elif index == self._current_idx:
                self._current_idx = -1

    def move_in_queue(self, from_idx: int, to_idx: int) -> None:
        if 0 <= from_idx < len(self._queue) and 0 <= to_idx < len(self._queue):
            video = self._queue.pop(from_idx)
            self._queue.insert(to_idx, video)
            if self._current_idx == from_idx:
                self._current_idx = to_idx
            elif from_idx < self._current_idx <= to_idx:
                self._current_idx -= 1
            elif to_idx <= self._current_idx < from_idx:
                self._current_idx += 1

    def get_current_track(self) -> Optional[dict]:
        if 0 <= self._current_idx < len(self._queue):
            return self._queue[self._current_idx]
        return None

    async def toggle_pause(self) -> None:
        if self.mpv:
            self._paused = not self._paused
            await self.mpv.cmd(["cycle", "pause"])

    async def stop(self) -> None:
        if self.mpv:
            await self.mpv.cmd(["stop"])
            await self.mpv.shutdown()
            self.mpv = None
            self._paused = False

    def clear_queue(self) -> None:
        self._queue = []
        self._current_idx = -1

    async def seek(self, seconds: float) -> None:
        if self.mpv:
            await self.mpv.cmd(["seek", seconds, "absolute"])

    async def set_volume(self, level: int) -> None:
        self.volume = max(0, min(100, level))
        if self.mpv:
            await self.mpv.cmd(["set_property", "volume", self.volume])

    async def get_property(self, prop: str):
        if self.mpv:
            return await self.mpv.cmd(["get_property", prop], wait=True)
        return None

    async def get_position(self) -> Optional[float]:
        return await self.get_property("time-pos")

    async def get_duration(self) -> Optional[float]:
        return await self.get_property("duration")

    async def shutdown(self) -> None:
        if self.mpv:
            await self.mpv.shutdown()
            self.mpv = None
