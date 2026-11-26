"""ffprobe 封装（对应 MediaProbe）。

本地导入与下载完成后都用同一套校验：
- 必须存在至少一条真实视频流（忽略 attached_pic 封面图）；
- 读取容器时长、大小与完整 JSON，供后续排版与画布选择使用。
"""

from __future__ import annotations

import json
import os
import subprocess
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional

import locator


@dataclass
class ProbeInfo:
    ok: bool = False
    error: str = ""
    duration_ms: int = 0
    size_bytes: int = 0
    json_data: Dict[str, Any] = field(default_factory=dict)
    streams: List[Dict[str, Any]] = field(default_factory=list)

    @property
    def has_video(self) -> bool:
        for stream in self.streams:
            if stream.get("codec_type") != "video":
                continue
            if int(stream.get("disposition", {}).get("attached_pic", 0) or 0) == 1:
                continue
            return True
        return False

    @property
    def has_audio(self) -> bool:
        return any(stream.get("codec_type") == "audio" for stream in self.streams)

    @property
    def audio_codec(self) -> str:
        for stream in self.streams:
            if stream.get("codec_type") == "audio":
                return str(stream.get("codec_name", "")).lower()
        return ""

    def video_size(self) -> Optional[tuple]:
        for stream in self.streams:
            if stream.get("codec_type") != "video":
                continue
            if int(stream.get("disposition", {}).get("attached_pic", 0) or 0) == 1:
                continue
            width = int(stream.get("width", 0) or 0)
            height = int(stream.get("height", 0) or 0)
            if width > 0 and height > 0:
                return width, height
        return None


def _trimmed_error(text: str) -> str:
    text = text.strip()
    if not text:
        return "ffprobe 执行失败。"
    return text if len(text) <= 600 else text[:600] + "…"


def probe(file_path: str, timeout_seconds: float = 30,
          ffmpeg_configured: str = "") -> ProbeInfo:
    result = ProbeInfo()
    if not os.path.isfile(file_path):
        result.error = f"文件不存在：{file_path}"
        return result

    ffprobe = locator.ffprobe_path(ffmpeg_configured)
    if not ffprobe:
        result.error = "未找到 ffprobe。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。"
        return result

    args = [
        ffprobe, "-v", "error", "-print_format", "json",
        "-show_format", "-show_streams", file_path,
    ]
    try:
        completed = subprocess.run(
            args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=timeout_seconds, check=False,
        )
    except subprocess.TimeoutExpired:
        result.error = "读取媒体信息超时。"
        return result
    except OSError as error:
        result.error = f"无法启动 ffprobe：{error}"
        return result

    if completed.returncode != 0:
        result.error = _trimmed_error(
            completed.stderr.decode("utf-8", errors="replace")
        )
        return result

    try:
        root = json.loads(completed.stdout.decode("utf-8", errors="replace"))
    except ValueError as error:
        result.error = f"无法解析 ffprobe 输出：{error}"
        return result

    if not isinstance(root, dict) or not root.get("format"):
        result.error = "无法识别媒体格式。"
        return result

    result.streams = [s for s in root.get("streams", []) if isinstance(s, dict)]
    if not result.has_video:
        result.error = "文件中没有视频流，仅支持视频文件。"
        return result

    duration_value = root.get("format", {}).get("duration")
    try:
        duration_seconds = float(duration_value)
    except (TypeError, ValueError):
        duration_seconds = 0.0
    if duration_seconds > 0:
        result.duration_ms = int(duration_seconds * 1000.0 + 0.5)

    result.size_bytes = os.path.getsize(file_path)
    result.json_data = root
    result.ok = True
    return result


def media_duration_ms(file_path: str, timeout_seconds: float = 30,
                      ffmpeg_configured: str = "") -> Optional[int]:
    """对应 FfmpegUtils::mediaDurationMs；失败返回 None。"""
    ffprobe = locator.ffprobe_path(ffmpeg_configured)
    if not ffprobe:
        return None
    args = [
        ffprobe, "-v", "error", "-show_entries", "format=duration",
        "-of", "default=nw=1:nk=1", file_path,
    ]
    try:
        completed = subprocess.run(
            args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            timeout=timeout_seconds, check=False,
        )
    except (subprocess.TimeoutExpired, OSError):
        return None
    if completed.returncode != 0:
        return None
    try:
        seconds = float(completed.stdout.decode("ascii", errors="ignore").strip())
    except ValueError:
        return None
    if seconds <= 0:
        return None
    return int(seconds * 1000.0 + 0.5)


def video_size(file_path: str, ffmpeg_configured: str = "") -> Optional[tuple]:
    info = probe(file_path, ffmpeg_configured=ffmpeg_configured)
    if not info.ok:
        return None
    return info.video_size()
