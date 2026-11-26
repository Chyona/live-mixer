"""素材获取：线上链接录制 / 抖音直播间 / 本地视频导入。

与 LiveMix/src/media/RecordingService.cpp 对齐：
- http(s) 先嗅探 8KB：mp4/mov 直链原文件下载，HLS/直播/未知交给 ffmpeg；
- HLS 录制使用 -c copy + event 播放列表，崩溃/中断后仍可恢复；
- 抖音直播间每次录制都重新解析带签名的 FLV/HLS 地址并确认可播放；
- 本地导入只登记原文件，不复制；单文件 ≤ 10GB，必须有视频流；
- mp4 直链下载完成后若音轨不是 AAC，仅转码音频、视频直拷。
"""

from __future__ import annotations

import json
import os
import subprocess
import time
import urllib.parse
from typing import Any, Callable, Dict, Optional

import douyin
import ffmpeg
import http_client as http
import locator
import probe
from config import AppConfig
from store import Store
from util import (
    current_ms,
    directory_size,
    hls_playlist_duration_ms,
    sanitize_file_component,
)

MAX_LOCAL_VIDEO_BYTES = 10 * 1024 * 1024 * 1024
PROBE_SNIFF_BYTES = 8192
PROBE_TIMEOUT_SECONDS = 8
DOWNLOAD_IDLE_TIMEOUT_SECONDS = 60
STDERR_TAIL_LIMIT = 16 * 1024

VIDEO_EXTENSIONS = {
    "mp4", "mov", "mkv", "flv", "ts", "m2ts", "avi", "webm", "m4v",
    "mpg", "mpeg", "wmv", "3gp", "m3u8",
}

ProgressCallback = Optional[Callable[[str], None]]


class RecordingError(RuntimeError):
    pass


def _notify(progress: ProgressCallback, message: str) -> None:
    if progress:
        progress(message)


def is_douyin_room_number(text: str) -> bool:
    return 4 <= len(text) <= 20 and text.isdigit()


def normalize_stream_url(text: str) -> Optional[str]:
    """对应 MainWindow::normalizeStreamUrl。"""
    trimmed = (text or "").strip()
    if is_douyin_room_number(trimmed):
        return f"https://live.douyin.com/{trimmed}"
    candidate = trimmed
    if "://" not in candidate and not os.path.exists(candidate):
        # QUrl::fromUserInput 会把裸域名补成 http://。
        if candidate.lower().startswith("www.") or "." in candidate.split("/")[0]:
            candidate = "http://" + candidate
    try:
        parsed = urllib.parse.urlsplit(candidate)
    except ValueError:
        return None
    if parsed.scheme.lower() not in ("http", "https", "rtmp", "rtmps", "rtsp"):
        return None
    if parsed.scheme.lower() in ("http", "https") and not parsed.netloc:
        return None
    return candidate


def is_supported_video_file(path: str) -> bool:
    if not os.path.isfile(path) or not os.access(path, os.R_OK):
        return False
    suffix = os.path.splitext(path)[1].lstrip(".").lower()
    return suffix in VIDEO_EXTENSIONS


def derive_title(uri: str, title: str) -> str:
    explicit = (title or "").strip()
    if explicit:
        return explicit
    parsed = urllib.parse.urlsplit(uri)
    base = os.path.splitext(os.path.basename(parsed.path))[0]
    if not base:
        base = "直播录制"
    return f"{time.strftime('%m-%d %H:%M')} {base}"


def prepare_output_directory(root_path: str, title: str) -> str:
    os.makedirs(root_path, exist_ok=True)
    stamp = time.strftime("%Y%m%d-%H%M%S")
    folder_base = stamp + "-" + sanitize_file_component(title, 48)
    path = os.path.join(root_path, folder_base)
    suffix = 2
    while os.path.exists(path) and suffix < 1000:
        path = os.path.join(root_path, f"{folder_base}-{suffix}")
        suffix += 1
    os.makedirs(path, exist_ok=True)
    return path


def url_looks_like_mp4_file(uri: str) -> bool:
    suffix = os.path.splitext(urllib.parse.urlsplit(uri).path)[1].lstrip(".").lower()
    return suffix in ("mp4", "m4v", "mov")


def looks_like_hls_playlist(head: bytes, content_type: str) -> bool:
    lower_type = (content_type or "").lower()
    if "mpegurl" in lower_type or "m3u8" in lower_type:
        return True
    body = head
    if body.startswith(b"\xef\xbb\xbf"):
        body = body[3:]
    return body.strip().startswith(b"#EXTM3U")


def classify_online_link(head: bytes, content_type: str) -> str:
    """返回 direct（原文件下载）或 stream（ffmpeg 录制）。"""
    if looks_like_hls_playlist(head, content_type):
        return "stream"
    lower_type = (content_type or "").lower()
    if any(lower_type.startswith(prefix) for prefix in (
        "video/mp4", "video/quicktime", "application/mp4", "video/x-m4v",
    )):
        return "direct"
    # 部分对象存储返回 application/octet-stream，按 MP4 box 头兜底识别。
    if len(head) >= 12 and head[4:8] == b"ftyp":
        return "direct"
    return "stream"


def download_file_name(uri: str) -> str:
    parsed = urllib.parse.urlsplit(uri)
    file_name = os.path.basename(urllib.parse.unquote(parsed.path))
    base, suffix = os.path.splitext(file_name)
    if not base:
        base = "source"
    suffix = suffix.lstrip(".").lower()
    if suffix not in ("mp4", "m4v", "mov"):
        suffix = "mp4"
    return sanitize_file_component(base, 80) + "." + suffix


def _last_error_line(text: str) -> str:
    lines = text.split("\n")
    for line in reversed(lines):
        line = line.strip()
        if not line:
            continue
        return line if len(line) <= 400 else line[:400] + "…"
    return ""


def _ffmpeg_args(config: AppConfig, input_uri: str, playlist_path: str,
                  segment_pattern: str, force_hls: bool) -> list:
    args = [
        "-hide_banner", "-y", "-loglevel", "warning",
        "-progress", "pipe:1", "-nostats",
    ]
    if config.recording.reconnect and input_uri.lower().startswith(("http://", "https://")):
        args += [
            "-reconnect", "1",
            "-reconnect_streamed", "1",
            "-reconnect_delay_max", str(config.recording.reconnect_delay_max_seconds),
        ]
    if force_hls:
        args += ["-f", "hls"]
    args += [
        "-i", input_uri,
        "-c", "copy",
        "-f", "hls",
        "-hls_time", str(config.recording.segment_seconds),
        "-hls_playlist_type", "event",
        "-hls_segment_filename", segment_pattern,
        playlist_path,
    ]
    return args


def _tail_log(log_path: str) -> str:
    try:
        with open(log_path, "rb") as handle:
            handle.seek(0, os.SEEK_END)
            size = handle.tell()
            handle.seek(max(0, size - STDERR_TAIL_LIMIT))
            return handle.read().decode("utf-8", errors="replace")
    except OSError:
        return ""


def record_stream(
    store: Store,
    config: AppConfig,
    *,
    source_uri: str,
    input_uri: str,
    title: str,
    source_kind: str = "stream",
    force_hls: bool = False,
    max_duration_seconds: float = 0.0,
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    ffmpeg_bin = locator.ffmpeg_path(config.recording.ffmpeg_path)
    if not ffmpeg_bin:
        raise RecordingError(
            "未找到 ffmpeg。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。"
        )
    if not locator.ffprobe_path(config.recording.ffmpeg_path):
        raise RecordingError(
            "未找到 ffprobe。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。"
        )

    resolved_title = derive_title(source_uri, title)
    output_dir = prepare_output_directory(
        config.storage.recording_dir, resolved_title
    )
    playlist_path = os.path.join(output_dir, "index.m3u8")
    segment_pattern = os.path.join(output_dir, "segment_%05d.ts")
    started_at_ms = current_ms()
    source_id, session_id = store.create_recording_source(
        title=resolved_title,
        source_uri=source_uri,
        output_path=playlist_path,
        started_at_ms=started_at_ms,
        container="hls",
        # 客户端 schema 的 source_kind 只有 online / local；stream/douyin
        # 仅作为本次结果的展示字段。
        source_kind="online",
        local_path=playlist_path,
    )

    log_path = os.path.join(output_dir, "ffmpeg.log")
    log_file = open(log_path, "ab")
    args = _ffmpeg_args(config, input_uri, playlist_path, segment_pattern, force_hls)
    log_file.write(f"ffmpeg: {ffmpeg_bin}\nargs: {' '.join(args)}\n\n".encode("utf-8"))
    log_file.flush()

    _notify(progress, f"开始录制：{resolved_title}")
    _notify(progress, f"输出目录：{output_dir}")

    try:
        process = subprocess.Popen(
            [ffmpeg_bin, *args],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=log_file,
            bufsize=0,
        )
    except OSError as error:
        message = f"录制失败：无法启动 ffmpeg：{error}"
        log_file.close()
        store.finish_recording(
            source_id, session_id, success=False, local_path=playlist_path,
            duration_ms=0, size_bytes=0, error_message=message,
        )
        _notify(progress, message)
        return {
            "ok": False,
            "source_id": source_id,
            "title": resolved_title,
            "source_uri": source_uri,
            "local_path": playlist_path,
            "output_dir": output_dir,
            "duration_ms": 0,
            "size_bytes": 0,
            "message": message,
            "warning": message,
            "kind": source_kind,
        }

    first_out_ms = -1
    last_out_ms = -1
    last_duration_ms = 0
    stop_requested = False
    interrupted = False

    def request_stop() -> None:
        nonlocal stop_requested
        if stop_requested:
            return
        stop_requested = True
        try:
            if process.stdin and not process.stdin.closed:
                process.stdin.write(b"q\n")
                process.stdin.flush()
        except (BrokenPipeError, OSError):
            pass

    try:
        while True:
            line = process.stdout.readline() if process.stdout else b""
            if not line:
                break
            text = line.decode("utf-8", errors="replace").strip()
            if "=" not in text:
                continue
            key, value = text.split("=", 1)
            if key in ("out_time_us", "out_time_ms"):
                try:
                    microseconds = int(value)
                except ValueError:
                    continue
                milliseconds = microseconds // 1000
                if first_out_ms < 0:
                    first_out_ms = milliseconds
                last_out_ms = milliseconds
                last_duration_ms = max(0, last_out_ms - first_out_ms)
            elif key == "progress":
                _notify(
                    progress,
                    f"录制中：{last_duration_ms // 1000} 秒",
                )
                if (max_duration_seconds > 0
                        and last_duration_ms >= max_duration_seconds * 1000):
                    request_stop()
    except KeyboardInterrupt:
        interrupted = True
        request_stop()
    finally:
        if interrupted and not stop_requested:
            request_stop()
        try:
            timeout = max(1.0, min(60.0, config.recording.stop_timeout_ms / 1000.0))
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            process.kill()
            try:
                process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        except KeyboardInterrupt:
            # 用户在等待 ffmpeg 收尾时再次中断：强制结束，避免漏写数据库。
            process.kill()
            try:
                process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        try:
            if process.stdout:
                process.stdout.close()
        except OSError:
            pass
        try:
            if process.stdin and not process.stdin.closed:
                process.stdin.close()
        except OSError:
            pass
        log_file.flush()
        log_file.close()

    playlist_valid = os.path.isfile(playlist_path) and os.path.getsize(playlist_path) > 0
    exit_code = process.returncode if process.returncode is not None else -1
    success = (exit_code == 0 and playlist_valid) or (stop_requested and playlist_valid)

    if success:
        message = "录制已停止，文件已保存" if stop_requested else "录制完成"
        warning = ""
    else:
        reason = _last_error_line(_tail_log(log_path))
        if not reason:
            reason = f"ffmpeg 退出码 {exit_code}"
        message = f"录制失败：{reason}"
        warning = message

    finished_at_ms = current_ms()
    duration_ms = hls_playlist_duration_ms(playlist_path)
    if duration_ms <= 0:
        duration_ms = last_duration_ms
    if duration_ms <= 0:
        duration_ms = max(0, finished_at_ms - started_at_ms)
    size_bytes = directory_size(output_dir)

    store.finish_recording(
        source_id, session_id, success=success, local_path=playlist_path,
        duration_ms=duration_ms, size_bytes=size_bytes,
        error_message="" if success else message,
    )

    _notify(progress, message)
    return {
        "ok": success,
        "source_id": source_id,
        "title": resolved_title,
        "source_uri": source_uri,
        "local_path": playlist_path,
        "output_dir": output_dir,
        "duration_ms": duration_ms,
        "size_bytes": size_bytes,
        "message": message,
        "warning": warning,
        "kind": source_kind,
    }


def _fail_download(store: Store, source_id: int, session_id: int,
                   output_dir: str, output_path: str, message: str,
                   progress: ProgressCallback, title: str,
                   source_uri: str) -> Dict[str, Any]:
    import shutil
    shutil.rmtree(output_dir, ignore_errors=True)
    store.finish_recording(
        source_id, session_id, success=False, local_path=output_path,
        duration_ms=0, size_bytes=0, error_message=message,
    )
    _notify(progress, message)
    return {
        "ok": False,
        "source_id": source_id,
        "title": title,
        "source_uri": source_uri,
        "local_path": output_path,
        "duration_ms": 0,
        "size_bytes": 0,
        "message": message,
        "warning": message,
        "kind": "download",
    }


def record_download(
    store: Store,
    config: AppConfig,
    *,
    source_uri: str,
    title: str,
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    resolved_title = derive_title(source_uri, title)
    output_dir = prepare_output_directory(
        config.storage.recording_dir, resolved_title
    )
    final_path = os.path.join(output_dir, download_file_name(source_uri))
    temp_path = final_path + ".part"
    started_at_ms = current_ms()

    source_id, session_id = store.create_recording_source(
        title=resolved_title,
        source_uri=source_uri,
        output_path=final_path,
        started_at_ms=started_at_ms,
        container="mp4",
        source_kind="online",
        local_path=final_path,
    )

    _notify(progress, f"下载直链：{source_uri}")
    _notify(progress, f"输出文件：{final_path}")

    last_report = [0.0]

    def on_progress(received: int, total: int) -> None:
        now = time.monotonic()
        if now - last_report[0] < 0.5 and (total <= 0 or received < total):
            return
        last_report[0] = now
        if total > 0:
            percent = int(received * 100 / total)
            _notify(progress, f"下载中：{percent}%")
        else:
            _notify(progress, f"下载中：{received // (1024 * 1024)} MB")

    try:
        ok, error = http.download_to_file(
            source_uri, temp_path,
            timeout=DOWNLOAD_IDLE_TIMEOUT_SECONDS,
            progress=on_progress,
        )
    except KeyboardInterrupt:
        ok, error = False, "下载已取消"
    if ok:
        _notify(progress, "下载完成，正在校验文件…")
    if not ok:
        return _fail_download(
            store, source_id, session_id, output_dir, final_path,
            error or "下载失败", progress, resolved_title, source_uri,
        )

    if not os.path.isfile(temp_path) or os.path.getsize(temp_path) <= 0:
        return _fail_download(
            store, source_id, session_id, output_dir, final_path,
            "下载失败：未收到有效文件内容。", progress,
            resolved_title, source_uri,
        )

    info = probe.probe(temp_path, ffmpeg_configured=config.recording.ffmpeg_path)
    if not info.ok:
        return _fail_download(
            store, source_id, session_id, output_dir, final_path,
            f"下载完成但无法识别视频：{info.error}", progress,
            resolved_title, source_uri,
        )

    warning = ""
    if not info.has_audio:
        warning = "源文件没有音频轨，无法进行语音转写。"
    elif info.audio_codec != "aac":
        normalized_path = final_path + ".normalizing.mp4"
        try:
            _notify(progress, "音频格式转换中…")
            ffmpeg.normalize_audio_aac(
                temp_path, normalized_path,
                ffmpeg_configured=config.recording.ffmpeg_path,
            )
            os.replace(normalized_path, final_path)
            os.unlink(temp_path)
            _notify(progress, "音频已转换为 AAC")
        except Exception:
            try:
                if os.path.exists(normalized_path):
                    os.unlink(normalized_path)
            except OSError:
                pass
            warning = "音频格式转换失败，部分播放器可能无声。"

    if not os.path.exists(final_path):
        os.replace(temp_path, final_path)

    duration_ms = info.duration_ms
    if duration_ms <= 0:
        duration_ms = probe.media_duration_ms(
            final_path, ffmpeg_configured=config.recording.ffmpeg_path
        ) or 0
    size_bytes = os.path.getsize(final_path)
    message = "下载完成"
    if warning:
        message += f"（{warning.rstrip('。')}）"

    store.finish_recording(
        source_id, session_id, success=True, local_path=final_path,
        duration_ms=duration_ms, size_bytes=size_bytes,
        error_message=warning,
    )
    _notify(progress, message)
    return {
        "ok": True,
        "source_id": source_id,
        "title": resolved_title,
        "source_uri": source_uri,
        "local_path": final_path,
        "duration_ms": duration_ms,
        "size_bytes": size_bytes,
        "message": message,
        "warning": warning,
        "kind": "download",
    }


def record_online(
    store: Store,
    config: AppConfig,
    uri: str,
    *,
    title: str = "",
    max_duration_seconds: float = 0.0,
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    if not locator.ffmpeg_path(config.recording.ffmpeg_path):
        raise RecordingError(
            "未找到 ffmpeg。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。"
        )
    if not locator.ffprobe_path(config.recording.ffmpeg_path):
        raise RecordingError(
            "未找到 ffprobe。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。"
        )

    normalized = normalize_stream_url(uri)
    if not normalized:
        raise RecordingError(
            "仅支持 http / https / rtmp / rtsp 视频地址，"
            "或抖音直播间链接 / 房间号。"
        )

    parsed = urllib.parse.urlsplit(normalized)
    scheme = parsed.scheme.lower()

    if scheme in ("http", "https") and douyin.is_share_url(normalized):
        if douyin.is_live_room_url(normalized) and not douyin.room_id_from_url(normalized):
            raise RecordingError("无法识别抖音直播间号，请检查链接。")
        _notify(progress, "正在解析抖音直播间…")
        resolved = douyin.resolve(normalized)
        _notify(progress, f"已解析 {resolved['stream_kind'].upper()} 流，开始录制")
        resolved_title = (title or "").strip() or resolved["title"]
        return record_stream(
            store, config,
            source_uri=resolved["room_uri"],
            input_uri=resolved["stream_uri"],
            title=resolved_title,
            source_kind="douyin",
            force_hls=False,
            max_duration_seconds=max_duration_seconds,
            progress=progress,
        )

    if scheme in ("http", "https"):
        _notify(progress, "正在探测链接…")
        sniff = http.sniff(
            normalized,
            headers={"Accept": "*/*", "Range": "bytes=0-8191"},
            max_bytes=PROBE_SNIFF_BYTES,
            timeout=PROBE_TIMEOUT_SECONDS,
        )
        fallback = (
            sniff.status >= 400
            or sniff.timed_out
            or (not sniff.network_ok and len(sniff.head) < 12)
        )
        if fallback:
            kind = "direct" if url_looks_like_mp4_file(normalized) else "stream"
        else:
            kind = classify_online_link(sniff.head, sniff.content_type)
        if kind == "direct":
            return record_download(
                store, config, source_uri=normalized, title=title,
                progress=progress,
            )
        force_hls = (not fallback) and looks_like_hls_playlist(
            sniff.head, sniff.content_type
        )
        return record_stream(
            store, config,
            source_uri=normalized,
            input_uri=normalized,
            title=title,
            source_kind="stream",
            force_hls=force_hls,
            max_duration_seconds=max_duration_seconds,
            progress=progress,
        )

    if scheme in ("rtmp", "rtmps", "rtsp"):
        return record_stream(
            store, config,
            source_uri=normalized,
            input_uri=normalized,
            title=title,
            source_kind="stream",
            max_duration_seconds=max_duration_seconds,
            progress=progress,
        )

    raise RecordingError("仅支持 http / https / rtmp / rtsp 视频地址。")


def import_local(
    store: Store,
    path: str,
    *,
    title: str = "",
    note: str = "",
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    absolute = os.path.abspath(os.path.expanduser(path))
    if not is_supported_video_file(absolute):
        raise RecordingError(
            "仅支持视频文件：mp4 / mov / mkv / flv / ts / m3u8"
            "（以及 avi / webm / m4v / mpg / mpeg / wmv / 3gp / m2ts）。"
        )
    if os.path.getsize(absolute) > MAX_LOCAL_VIDEO_BYTES:
        raise RecordingError("单个本地视频不能超过 10 GB。")

    _notify(progress, f"校验本地视频：{absolute}")
    info = probe.probe(absolute)
    if not info.ok:
        raise RecordingError(f"导入失败：{info.error}")

    source_title = (title or "").strip() or os.path.basename(absolute)
    source_id = store.ensure_source_for_input(
        absolute,
        source_title,
        note=note,
        duration_ms=info.duration_ms,
        size_bytes=info.size_bytes,
        probe_json=json.dumps(info.json_data, ensure_ascii=False),
    )
    _notify(progress, f"已导入：{source_title}")
    return {
        "ok": True,
        "source_id": source_id,
        "title": source_title,
        "source_uri": absolute,
        "local_path": absolute,
        "duration_ms": info.duration_ms,
        "size_bytes": info.size_bytes,
        "message": f"已导入 {source_title}",
        "warning": "",
        "kind": "local",
    }
