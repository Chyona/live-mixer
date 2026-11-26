"""通用工具：与 Qt 客户端保持一致的时间、文件名、哈希与随机语义。"""

from __future__ import annotations

import hashlib
import json
import math
import os
import random
import tempfile
import time
import unicodedata
from dataclasses import dataclass
from typing import Any, Iterable, Optional

# ---------------------------------------------------------------------------
# 时间
# ---------------------------------------------------------------------------


def current_ms() -> int:
    """当前 Unix 毫秒时间戳（对应 QDateTime::currentMSecsSinceEpoch）。"""
    return int(time.time() * 1000)


def qt_round(value: float) -> int:
    """Qt qRound / qRound64 的语义：四舍五入，0.5 远离零。"""
    if value >= 0:
        return int(math.floor(value + 0.5))
    return int(math.ceil(value - 0.5))


def format_duration(milliseconds: int) -> str:
    """对应 MediaUtils::formatDuration：HH:MM:SS 或 MM:SS。"""
    total_seconds = max(0, int(milliseconds)) // 1000
    hours = total_seconds // 3600
    minutes = (total_seconds % 3600) // 60
    seconds = total_seconds % 60
    if hours > 0:
        return f"{hours}:{minutes:02d}:{seconds:02d}"
    return f"{minutes:02d}:{seconds:02d}"


def format_range(start_ms: int, end_ms: int) -> str:
    return f"{format_duration(start_ms)} - {format_duration(end_ms)}"


# ---------------------------------------------------------------------------
# 字符串 / 文件路径
# ---------------------------------------------------------------------------


def utf16_len(text: str) -> int:
    """按 Qt QString（UTF-16 code unit）计算长度；emoji 计 2。"""
    return len(text.encode("utf-16-le")) // 2


def sanitize_file_component(value: str, max_length: int = 100) -> str:
    """对应 MediaUtils::sanitizeFileComponent。

    - 控制字符与 / \\ : * ? " < > | % 替换为下划线；
    - 连续空白压缩并去掉首尾空白（QString::simplified）；
    - 去掉开头的点；
    - 空字符串回退为 recording；
    - 截断到 max_length。
    """
    result = []
    for ch in value:
        if unicodedata.category(ch) == "Cc" or ch in '/\\:*?"<>|%':
            result.append("_")
        else:
            result.append(ch)
    simplified = " ".join("".join(result).split())
    simplified = simplified.lstrip(".")
    if not simplified:
        simplified = "recording"
    return simplified[: max(1, max_length)]


def sanitize_material_base(value: str, max_length: int = 36) -> str:
    result = sanitize_file_component(value, max_length)
    if not result:
        return "material"
    return result


def is_http_url(value: str) -> bool:
    lowered = value.lower()
    return lowered.startswith("http://") or lowered.startswith("https://")


def is_within_directory(root: str, path: str) -> bool:
    root = os.path.normpath(root)
    path = os.path.normpath(path)
    if not root or not path:
        return False
    if path == root:
        return True
    return path.startswith(root.rstrip(os.sep) + os.sep)


def directory_size(path: str) -> int:
    if os.path.isfile(path):
        return os.path.getsize(path)
    total = 0
    for base, _dirs, files in os.walk(path):
        for name in files:
            try:
                total += os.path.getsize(os.path.join(base, name))
            except OSError:
                continue
    return total


def hls_playlist_duration_ms(playlist_path: str) -> int:
    """对应 MediaUtils::hlsPlaylistDurationMs：累加 #EXTINF。"""
    total = 0.0
    try:
        with open(playlist_path, "r", encoding="utf-8", errors="replace") as handle:
            for raw in handle:
                line = raw.strip()
                if not line.startswith("#EXTINF:"):
                    continue
                value = line[8:].split(",", 1)[0].strip()
                try:
                    seconds = float(value)
                except ValueError:
                    continue
                if seconds > 0:
                    total += seconds
    except OSError:
        return 0
    return qt_round(total * 1000.0)


def hash6(value: str) -> str:
    """素材文件名的 6 位短哈希。

    客户端使用 Qt qHash（进程内哈希种子固定，但算法为 Qt 私有实现）。
    这里使用 SHA-256 前 6 位十六进制，作用相同：同一 URL 稳定、不同 URL
    尽量不重名。剪映只关心草稿内相对路径，不依赖该哈希的具体取值。
    """
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:6]


# ---------------------------------------------------------------------------
# JSON / 原子写文件
# ---------------------------------------------------------------------------


def read_json(path: str, default: Any = None) -> Any:
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return default


def write_json_atomic(path: str, payload: Any, *, indent: int = 4,
                      sort_keys: bool = True) -> None:
    """先写同目录临时文件，再原子改名；便于剪映监听到完整目录。"""
    directory = os.path.dirname(os.path.abspath(path)) or "."
    os.makedirs(directory, exist_ok=True)
    fd, temp_path = tempfile.mkstemp(prefix=".tmp-", dir=directory)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            # QJsonDocument::toJson(Indented) 以换行结尾，保持字节级风格一致。
            json.dump(payload, handle, ensure_ascii=False, indent=indent,
                      sort_keys=sort_keys)
            handle.write("\n")
        os.replace(temp_path, path)
    except Exception:
        try:
            os.unlink(temp_path)
        except OSError:
            pass
        raise


def json_compact(payload: Any) -> str:
    """对应 QJsonDocument::Compact（不转义中文、无空格）。"""
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


def bool_text(value: bool) -> str:
    return "已开启" if value else "已关闭"


def clamp(value: float, low: float, high: float) -> float:
    return max(low, min(high, value))


# ---------------------------------------------------------------------------
# 随机源
# ---------------------------------------------------------------------------


class RandomSource:
    """包装 QRandomGenerator 的常用调用。

    默认使用系统随机源（与客户端一致）；指定 seed 时使用可复现的
    random.Random，仅用于调试 / 回归测试。
    """

    def __init__(self, seed: Optional[int] = None) -> None:
        self._seeded = seed is not None
        self._rng = random.Random(seed) if self._seeded else random.SystemRandom()

    @property
    def seeded(self) -> bool:
        return self._seeded

    def bounded(self, *args: int) -> int:
        """对应 QRandomGenerator::bounded：单参数 [0, n)，双参数 [low, high)。"""
        return self._rng.randrange(*args)

    def generate_double(self) -> float:
        """对应 QRandomGenerator::generateDouble，范围 [0, 1)。"""
        return self._rng.random()

    def choice(self, items: Iterable[Any]) -> Any:
        items = list(items)
        return items[self.bounded(len(items))]


@dataclass
class Progress:
    """简单的进度打印器；所有输出走 stderr，避免污染 --json 的 stdout。"""

    enabled: bool = True
    prefix: str = ""

    def __call__(self, message: str) -> None:
        if self.enabled:
            print(f"{self.prefix}{message}", flush=True)

    def step(self, step: str, message: str, percent: Optional[int] = None) -> None:
        if not self.enabled:
            return
        suffix = f" [{percent}%]" if percent is not None else ""
        print(f"{self.prefix}[{step}] {message}{suffix}", flush=True)
