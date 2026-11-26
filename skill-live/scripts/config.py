"""技能配置（与客户端 AppConfig 字段对齐）。

客户端把配置保存在 ``config.json``（service / storage / recording / ai /
finish / general）。技能沿用同一结构，并增加 ``state_db`` 用于本技能自己的
SQLite 素材库与 ASR 缓存；字段名保持与客户端一致，方便直接把客户端的
service / storage / finish 段复制过来。

配置优先级：
    命令行参数 > 环境变量 > 配置文件 > 默认值

默认路径：``~/.livemix/config.json``（可用 ``LIVEMIX_CONFIG_PATH`` 覆盖）。
"""

from __future__ import annotations

import copy
import os
import sys
from dataclasses import dataclass, field
from typing import Any, Dict, Optional

from util import read_json, write_json_atomic


def _home() -> str:
    return os.path.expanduser("~")


def _default_movies() -> str:
    candidates = [os.path.join(_home(), "Movies"), os.path.join(_home(), "Videos")]
    for candidate in candidates:
        if os.path.isdir(candidate):
            return candidate
    return candidates[0]


def _default_cache() -> str:
    if sys.platform == "darwin":
        return os.path.join(_home(), "Library", "Caches", "LiveMixSkill")
    if sys.platform.startswith("win"):
        base = os.environ.get("LOCALAPPDATA") or os.path.join(_home(), "AppData", "Local")
        return os.path.join(base, "LiveMixSkill", "cache")
    return os.path.join(_home(), ".cache", "livemix-skill")


def default_config_path() -> str:
    override = os.environ.get("LIVEMIX_CONFIG_PATH", "").strip()
    if override:
        return os.path.abspath(os.path.expanduser(override))
    return os.path.join(_home(), ".livemix", "config.json")


def default_db_path() -> str:
    override = os.environ.get("LIVEMIX_DB_PATH", "").strip()
    if override:
        return os.path.abspath(os.path.expanduser(override))
    return os.path.join(_home(), ".livemix", "livemix.db")


@dataclass
class ServiceConfig:
    api_url: str = "https://api.jcaigc.cn/v1"
    api_key: str = ""


@dataclass
class StorageConfig:
    recording_dir: str = ""
    export_dir: str = ""
    draft_dir: str = ""
    cache_dir: str = ""
    cache_limit_gb: int = 100
    auto_clean_cache: bool = True
    state_db: str = ""

    def __post_init__(self) -> None:
        movies = _default_movies()
        if not self.recording_dir:
            self.recording_dir = os.path.join(movies, "LiveMix", "Recordings")
        if not self.export_dir:
            self.export_dir = os.path.join(movies, "LiveMix", "Exports")
        if not self.draft_dir:
            self.draft_dir = os.path.join(movies, "LiveMix", "CapDraft")
        if not self.cache_dir:
            self.cache_dir = _default_cache()
        if not self.state_db:
            self.state_db = default_db_path()


@dataclass
class RecordingConfig:
    format: str = "hls"
    ffmpeg_path: str = ""
    segment_seconds: int = 4
    reconnect: bool = True
    reconnect_delay_max_seconds: int = 5
    stop_timeout_ms: int = 8000


@dataclass
class AiConfig:
    default_prompt: str = "默认提示词"
    export_srt: bool = True


@dataclass
class FinishConfig:
    transitions: bool = True
    effects: bool = True
    filters: bool = True
    stickers: bool = True
    bgm: bool = True
    sfx: bool = True
    bilingual_subtitles: bool = False
    dedup: bool = True
    speed_auto: bool = True
    fixed_speed: float = 1.0


@dataclass
class GeneralConfig:
    theme: str = "dark"
    launch_at_login: bool = False
    minimize_to_background: bool = False
    sound_on_complete: bool = True
    system_notifications: bool = True


@dataclass
class AppConfig:
    version: int = 4
    service: ServiceConfig = field(default_factory=ServiceConfig)
    storage: StorageConfig = field(default_factory=StorageConfig)
    recording: RecordingConfig = field(default_factory=RecordingConfig)
    ai: AiConfig = field(default_factory=AiConfig)
    finish: FinishConfig = field(default_factory=FinishConfig)
    general: GeneralConfig = field(default_factory=GeneralConfig)
    path: str = ""

    # ------------------------------------------------------------------
    def to_dict(self, *, mask_secrets: bool = True) -> Dict[str, Any]:
        payload = {
            "version": self.version,
            "service": {
                "apiUrl": self.service.api_url,
                "apiKey": _mask(self.service.api_key) if mask_secrets else self.service.api_key,
            },
            "storage": {
                "recordingDir": self.storage.recording_dir,
                "exportDir": self.storage.export_dir,
                "draftDir": self.storage.draft_dir,
                "cacheDir": self.storage.cache_dir,
                "cacheLimitGb": self.storage.cache_limit_gb,
                "autoCleanCache": self.storage.auto_clean_cache,
                "stateDb": self.storage.state_db,
            },
            "recording": {
                "format": self.recording.format,
                "ffmpegPath": self.recording.ffmpeg_path,
                "segmentSeconds": self.recording.segment_seconds,
                "reconnect": self.recording.reconnect,
                "reconnectDelayMaxSeconds": self.recording.reconnect_delay_max_seconds,
                "stopTimeoutMs": self.recording.stop_timeout_ms,
            },
            "ai": {
                "defaultPrompt": self.ai.default_prompt,
                "exportSrt": self.ai.export_srt,
            },
            "finish": {
                "transitions": self.finish.transitions,
                "effects": self.finish.effects,
                "filters": self.finish.filters,
                "stickers": self.finish.stickers,
                "bgm": self.finish.bgm,
                "sfx": self.finish.sfx,
                "bilingualSubtitles": self.finish.bilingual_subtitles,
                "dedup": self.finish.dedup,
                "speedAuto": self.finish.speed_auto,
                "fixedSpeed": self.finish.fixed_speed,
            },
            "general": {
                "theme": self.general.theme,
                "launchAtLogin": self.general.launch_at_login,
                "minimizeToBackground": self.general.minimize_to_background,
                "soundOnComplete": self.general.sound_on_complete,
                "systemNotifications": self.general.system_notifications,
            },
        }
        return payload

    def save(self) -> None:
        os.makedirs(os.path.dirname(os.path.abspath(self.path)), exist_ok=True)
        write_json_atomic(self.path, self.to_dict(mask_secrets=False))
        try:
            os.chmod(self.path, 0o600)
        except OSError:
            pass


def _mask(value: str) -> str:
    if not value:
        return ""
    if len(value) <= 8:
        return "*" * len(value)
    return value[:4] + "*" * (len(value) - 8) + value[-4:]


def _str_value(data: Dict[str, Any], key: str, default: str) -> str:
    value = data.get(key, default)
    if not isinstance(value, str):
        return default
    value = value.strip()
    return value or default


def _raw_str_value(data: Dict[str, Any], key: str, default: str) -> str:
    value = data.get(key, default)
    return value.strip() if isinstance(value, str) else default


def _path_value(data: Dict[str, Any], key: str, default: str) -> str:
    value = data.get(key, default)
    if not isinstance(value, str) or not value.strip():
        return default
    return os.path.normpath(os.path.expanduser(value.strip()))


def _int_value(data: Dict[str, Any], key: str, default: int) -> int:
    value = data.get(key, default)
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


def _bool_value(data: Dict[str, Any], key: str, default: bool) -> bool:
    value = data.get(key, default)
    return value if isinstance(value, bool) else default


def _float_value(data: Dict[str, Any], key: str, default: float) -> float:
    value = data.get(key, default)
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def load(
    path: Optional[str] = None,
    *,
    api_url_override: str = "",
    api_key_override: str = "",
    db_override: str = "",
) -> AppConfig:
    config = AppConfig()
    config.path = os.path.abspath(os.path.expanduser(path)) if path else default_config_path()

    raw = read_json(config.path, default=None)
    if isinstance(raw, dict):
        service = raw.get("service") or {}
        storage = raw.get("storage") or {}
        recording = raw.get("recording") or {}
        ai = raw.get("ai") or {}
        finish = raw.get("finish") or {}
        general = raw.get("general") or {}

        config.service.api_url = _raw_str_value(
            service, "apiUrl", config.service.api_url
        ) or config.service.api_url
        config.service.api_key = _raw_str_value(service, "apiKey", "")

        config.storage.recording_dir = _path_value(
            storage, "recordingDir", config.storage.recording_dir
        )
        config.storage.export_dir = _path_value(
            storage, "exportDir", config.storage.export_dir
        )
        config.storage.draft_dir = _path_value(
            storage, "draftDir", config.storage.draft_dir
        )
        config.storage.cache_dir = _path_value(
            storage, "cacheDir", config.storage.cache_dir
        )
        config.storage.cache_limit_gb = _int_value(
            storage, "cacheLimitGb", config.storage.cache_limit_gb
        )
        config.storage.auto_clean_cache = _bool_value(
            storage, "autoCleanCache", config.storage.auto_clean_cache
        )
        config.storage.state_db = _path_value(
            storage, "stateDb", config.storage.state_db
        )

        config.recording.format = "hls"
        config.recording.ffmpeg_path = _str_value(recording, "ffmpegPath", "")
        config.recording.segment_seconds = _int_value(
            recording, "segmentSeconds", config.recording.segment_seconds
        )
        config.recording.reconnect = _bool_value(
            recording, "reconnect", config.recording.reconnect
        )
        config.recording.reconnect_delay_max_seconds = _int_value(
            recording, "reconnectDelayMaxSeconds",
            config.recording.reconnect_delay_max_seconds,
        )
        config.recording.stop_timeout_ms = _int_value(
            recording, "stopTimeoutMs", config.recording.stop_timeout_ms
        )

        config.ai.default_prompt = _str_value(
            ai, "defaultPrompt", config.ai.default_prompt
        )
        config.ai.export_srt = _bool_value(ai, "exportSrt", config.ai.export_srt)

        config.finish.transitions = _bool_value(
            finish, "transitions", config.finish.transitions
        )
        config.finish.effects = _bool_value(finish, "effects", config.finish.effects)
        config.finish.filters = _bool_value(finish, "filters", config.finish.filters)
        config.finish.stickers = _bool_value(
            finish, "stickers", config.finish.stickers
        )
        config.finish.bgm = _bool_value(finish, "bgm", config.finish.bgm)
        config.finish.sfx = _bool_value(finish, "sfx", config.finish.sfx)
        config.finish.bilingual_subtitles = _bool_value(
            finish, "bilingualSubtitles", config.finish.bilingual_subtitles
        )
        config.finish.dedup = _bool_value(finish, "dedup", config.finish.dedup)
        config.finish.speed_auto = _bool_value(
            finish, "speedAuto", config.finish.speed_auto
        )
        config.finish.fixed_speed = _float_value(
            finish, "fixedSpeed", config.finish.fixed_speed
        )

        config.general.theme = _str_value(general, "theme", config.general.theme)
        config.general.launch_at_login = _bool_value(
            general, "launchAtLogin", config.general.launch_at_login
        )
        config.general.minimize_to_background = _bool_value(
            general, "minimizeToBackground", config.general.minimize_to_background
        )
        config.general.sound_on_complete = _bool_value(
            general, "soundOnComplete", config.general.sound_on_complete
        )
        config.general.system_notifications = _bool_value(
            general, "systemNotifications", config.general.system_notifications
        )

    # 环境变量：与客户端一致的 API 配置，外加技能的目录覆盖。
    env_api_url = os.environ.get("LIVEMIX_API_URL", "").strip()
    env_api_key = os.environ.get("LIVEMIX_API_KEY", "").strip()
    if env_api_url:
        config.service.api_url = env_api_url
    if env_api_key:
        config.service.api_key = env_api_key
    if api_url_override:
        config.service.api_url = api_url_override.strip()
    if api_key_override:
        config.service.api_key = api_key_override.strip()

    for env_name, attr in (
        ("LIVEMIX_RECORDING_DIR", "recording_dir"),
        ("LIVEMIX_EXPORT_DIR", "export_dir"),
        ("LIVEMIX_DRAFT_DIR", "draft_dir"),
        ("LIVEMIX_CACHE_DIR", "cache_dir"),
    ):
        value = os.environ.get(env_name, "").strip()
        if value:
            setattr(config.storage, attr, os.path.normpath(os.path.expanduser(value)))
    if db_override:
        config.storage.state_db = os.path.abspath(os.path.expanduser(db_override))

    _normalize(config)
    return config


def _normalize(config: AppConfig) -> None:
    config.version = 4
    for attr in ("recording_dir", "export_dir", "draft_dir", "cache_dir", "state_db"):
        value = getattr(config.storage, attr)
        if not value:
            setattr(config.storage, attr, getattr(AppConfig().storage, attr))
        setattr(config.storage, attr, os.path.normpath(os.path.expanduser(value)))
    config.storage.cache_limit_gb = max(1, min(1000, int(config.storage.cache_limit_gb)))
    config.recording.format = "hls"
    config.recording.segment_seconds = max(1, min(30, int(config.recording.segment_seconds)))
    config.recording.reconnect_delay_max_seconds = max(
        1, min(30, int(config.recording.reconnect_delay_max_seconds))
    )
    config.recording.stop_timeout_ms = max(
        1000, min(60000, int(config.recording.stop_timeout_ms))
    )
    config.recording.ffmpeg_path = config.recording.ffmpeg_path.strip()
    config.service.api_url = config.service.api_url.strip()
    config.service.api_key = config.service.api_key.strip()
    config.ai.default_prompt = config.ai.default_prompt.strip() or "默认提示词"
    config.finish.fixed_speed = max(0.5, min(2.0, float(config.finish.fixed_speed)))
    if config.general.theme != "system":
        config.general.theme = "dark"


def ensure_initialized(path: Optional[str] = None, **kwargs: Any) -> AppConfig:
    config = load(path, **kwargs)
    if not os.path.isfile(config.path):
        config.save()
    return config


def clone(config: AppConfig) -> AppConfig:
    return copy.deepcopy(config)


def as_env_hints(config: AppConfig) -> Dict[str, str]:
    return {
        "LIVEMIX_CONFIG_PATH": config.path,
        "LIVEMIX_DB_PATH": config.storage.state_db,
        "LIVEMIX_FFMPEG_DIR": os.path.dirname(
            config.recording.ffmpeg_path
        ) if config.recording.ffmpeg_path else "",
    }
