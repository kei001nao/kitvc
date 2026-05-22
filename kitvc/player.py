import asyncio
import json
import os
from pathlib import Path
from typing import Callable, Optional

SOCKET_PATH = "/tmp/kitvc-mpv.sock"

MPV_BASE_ARGS = [
    "mpv",
    "--idle=yes",
    f"--input-ipc-server={SOCKET_PATH}",
    "--really-quiet",
    "--prefetch-playlist=yes",
]

class Player:
    def __init__(self, config: dict | None = None):
        cfg = config or {}
        self._mpv_args = list(MPV_BASE_ARGS)
        # Add custom mpv args from config
        self._mpv_args.extend(cfg.get("player", {}).get("mpv_args", []))
        
        self._process: Optional[asyncio.subprocess.Process] = None
        self._reader: Optional[asyncio.StreamReader] = None
        self._writer: Optional[asyncio.StreamWriter] = None
        self._req_id = 0
        self._pending: dict[int, asyncio.Future] = {}
        self._read_task: Optional[asyncio.Task] = None
        self.volume: int = cfg.get("player", {}).get("volume", 80)
        self.repeat: bool = False

        self.on_track_start: list[Callable] = []
        self.on_track_end: list[Callable] = []
        self._queue: list[dict] = []
        self._current_idx: int = -1
        self._paused: bool = False

    async def start(self) -> None:
        if Path(SOCKET_PATH).exists():
            os.unlink(SOCKET_PATH)

        self._process = await asyncio.create_subprocess_exec(
            *self._mpv_args,
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.DEVNULL,
        )

        for _ in range(50):
            if Path(SOCKET_PATH).exists():
                break
            await asyncio.sleep(0.1)
        else:
            raise RuntimeError("mpv IPC socket did not appear – is mpv installed?")

        self._reader, self._writer = await asyncio.open_unix_connection(SOCKET_PATH)
        self._read_task = asyncio.create_task(self._read_loop())
        await self.set_volume(self.volume)

    async def _read_loop(self) -> None:
        while True:
            try:
                line = await self._reader.readline()
                if not line:
                    await self._handle_disconnect()
                    break
                data = json.loads(line.decode())
                req_id = data.get("request_id")
                if req_id is not None:
                    fut = self._pending.pop(req_id, None)
                    if fut and not fut.done():
                        fut.set_result(data.get("data"))
                event = data.get("event")
                if event == "file-loaded":
                    for cb in self.on_track_start:
                        asyncio.create_task(cb())
                elif event == "end-file":
                    reason = data.get("reason")
                    # Only auto-next if the file actually ended normally (EOF)
                    # 'stop' or 'replaced' reasons should not trigger next()
                    if reason == "eof":
                        for cb in self.on_track_end:
                            asyncio.create_task(cb())
                        asyncio.create_task(self.next())
            except asyncio.CancelledError:
                break
            except Exception:
                pass

    async def _handle_disconnect(self) -> None:
        if self._read_task:
            self._read_task.cancel()
            self._read_task = None
        if self._writer:
            try:
                self._writer.close()
                await self._writer.wait_closed()
            except Exception:
                pass
            self._writer = None
        self._reader = None
        if self._process:
            try:
                self._process.terminate()
                await self._process.wait()
            except Exception:
                pass
            self._process = None
        for fut in self._pending.values():
            if not fut.done():
                fut.cancel()
        self._pending.clear()
        if Path(SOCKET_PATH).exists():
            try:
                os.unlink(SOCKET_PATH)
            except OSError:
                pass

    async def shutdown(self) -> None:
        await self._handle_disconnect()

    async def _cmd(self, command: list, *, wait: bool = False):
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
            await self._handle_disconnect()
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

    async def play(self, url: str, is_video: bool = False) -> None:
        if not self._writer:
            await self.start()
            
        if is_video:
            await self._cmd(["set_property", "video", "auto"])
        else:
            await self._cmd(["set_property", "video", "no"])
        
        self._paused = False
        await self._cmd(["set_property", "pause", False])
        await self._cmd(["loadfile", url, "replace"])

    def add_to_queue(self, tracks: list[dict]) -> None:
        self._queue.extend(tracks)

    async def play_from_queue(self, idx: int) -> None:
        if 0 <= idx < len(self._queue):
            self._current_idx = idx
            track = self._queue[idx]
            await self.play(track["path"], is_video=track.get("is_video", False))

    async def play_queue(self, tracks: list[dict], start_idx: int = 0) -> None:
        self._queue = tracks
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
                self._current_idx = -1 # Stopped current

    def move_in_queue(self, from_idx: int, to_idx: int) -> None:
        if 0 <= from_idx < len(self._queue) and 0 <= to_idx < len(self._queue):
            track = self._queue.pop(from_idx)
            self._queue.insert(to_idx, track)
            # Update current_idx
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
        self._paused = not self._paused
        await self._cmd(["cycle", "pause"])

    async def stop(self) -> None:
        await self._cmd(["stop"])

    def clear_queue(self) -> None:
        self._queue = []
        self._current_idx = -1

    async def seek(self, seconds: float) -> None:
        await self._cmd(["seek", seconds, "absolute"])

    async def set_volume(self, level: int) -> None:
        self.volume = max(0, min(100, level))
        await self._cmd(["set_property", "volume", self.volume])

    async def get_property(self, prop: str):
        return await self._cmd(["get_property", prop], wait=True)

    async def get_position(self) -> Optional[float]:
        return await self.get_property("time-pos")

    async def get_duration(self) -> Optional[float]:
        return await self.get_property("duration")

    async def get_metadata(self) -> dict:
        return await self.get_property("metadata") or {}
