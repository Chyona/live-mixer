"""ffmpeg / ffprobe 定位（对应 FfmpegLocator）。"""

from __future__ import annotations

import os
import shutil
import sys
from typing import Optional


def _executable_name(base: str) -> str:
    return base + ".exe" if sys.platform.startswith("win") else base


def _is_executable(path: str) -> bool:
    return bool(path) and os.path.isfile(path) and os.access(path, os.X_OK)


def resolve(base_name: str, configured_path: str = "") -> str:
    """按客户端优先级查找可执行文件。

    1. 设置中的 recording.ffmpegPath（仅 ffmpeg，且必须是可执行文件）；
    2. LIVEMIX_FFMPEG_DIR 环境变量；
    3. PATH；
    4. macOS Homebrew 常见目录。
    """
    name = _executable_name(base_name)
    configured = (configured_path or "").strip()
    if configured and os.path.isfile(configured) and os.access(configured, os.X_OK):
        return os.path.abspath(configured)

    candidates = []
    override_dir = os.environ.get("LIVEMIX_FFMPEG_DIR", "").strip()
    if override_dir:
        candidates.append(os.path.join(override_dir, name))

    # 脚本目录与当前工作目录相邻的 ffmpeg 目录也纳入候选，便于自带二进制。
    script_dir = os.path.dirname(os.path.abspath(__file__))
    candidates.append(os.path.join(script_dir, "ffmpeg", name))
    candidates.append(os.path.join(os.getcwd(), name))

    found = shutil.which(base_name)
    if found:
        candidates.append(found)

    if sys.platform == "darwin":
        candidates.append(os.path.join("/opt/homebrew/bin", name))
        candidates.append(os.path.join("/usr/local/bin", name))

    for candidate in candidates:
        if _is_executable(candidate):
            return os.path.realpath(candidate)
    return ""


def ffmpeg_path(configured_path: str = "") -> str:
    return resolve("ffmpeg", configured_path)


def ffprobe_path(configured_path: str = "") -> str:
    return resolve("ffprobe", configured_path)


def ffmpeg_dir(configured_path: str = "") -> Optional[str]:
    path = ffmpeg_path(configured_path)
    return os.path.dirname(path) if path else None
