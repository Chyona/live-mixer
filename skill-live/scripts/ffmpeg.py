"""ffmpeg 封装：与 LiveMix/src/media/FfmpegUtils.cpp 逐参数对齐。"""

from __future__ import annotations

import os
import subprocess
import tempfile
from typing import List

import locator
import probe
from util import hls_playlist_duration_ms

# 与客户端一致：切片重新编码为剪映兼容的 H.264/AAC。
_CUT_VIDEO_ARGS = [
    "-c:v", "libx264",
    "-preset", "veryfast",
    "-crf", "20",
    "-pix_fmt", "yuv420p",
    "-c:a", "aac",
    "-b:a", "128k",
    "-movflags", "+faststart",
]


class FfmpegError(RuntimeError):
    pass


def run_ffmpeg(args: List[str], timeout_ms: int,
               ffmpeg_configured: str = "") -> None:
    ffmpeg = locator.ffmpeg_path(ffmpeg_configured)
    if not ffmpeg:
        raise FfmpegError(
            "未找到 ffmpeg，请安装后重试，或设置 LIVEMIX_FFMPEG_DIR。"
        )
    command = [ffmpeg, *args]
    try:
        completed = subprocess.run(
            command, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
            timeout=max(1.0, timeout_ms / 1000.0), check=False,
        )
    except subprocess.TimeoutExpired:
        raise FfmpegError("ffmpeg 执行超时。")
    except OSError as error:
        raise FfmpegError(f"无法启动 ffmpeg：{error}")
    if completed.returncode != 0:
        text = completed.stderr.decode("utf-8", errors="replace").strip()
        if not text:
            text = "ffmpeg 执行失败。"
        raise FfmpegError(text[-800:])


def extract_mp3(input_path: str, output_path: str,
                ffmpeg_configured: str = "") -> None:
    """提取 16kHz 单声道 64kbps MP3，并保证与源 MP4 等长。

    与客户端一致：
    - aresample=async=1:first_pts=0 在音轨晚于视频起点时补开头静音；
    - apad 补尾部静音，-t 精确截到源视频时长；
    - 输出时长与源时长偏差 > 500ms 视为失败（会让字幕整体错位）。
    """
    if not os.path.isfile(input_path):
        raise FfmpegError(f"源文件不存在：{input_path}")
    output_dir = os.path.dirname(os.path.abspath(output_path))
    os.makedirs(output_dir, exist_ok=True)

    target_duration_ms = probe.media_duration_ms(
        input_path, ffmpeg_configured=ffmpeg_configured
    )
    has_duration = bool(target_duration_ms and target_duration_ms > 0)

    args = [
        "-y", "-nostdin", "-hide_banner", "-loglevel", "error",
        "-i", input_path, "-map", "0:a:0",
    ]
    if has_duration:
        args += [
            "-af", "aresample=async=1:first_pts=0,apad",
            "-t", f"{target_duration_ms / 1000.0:.3f}",
        ]
    else:
        args += ["-af", "aresample=async=1:first_pts=0"]
    args += [
        "-ac", "1", "-ar", "16000",
        "-c:a", "libmp3lame", "-b:a", "64k",
        output_path,
    ]
    run_ffmpeg(args, 30 * 60 * 1000, ffmpeg_configured)

    if not os.path.isfile(output_path) or os.path.getsize(output_path) <= 0:
        raise FfmpegError("音频提取失败，未生成有效文件。")

    if has_duration:
        output_duration_ms = probe.media_duration_ms(
            output_path, ffmpeg_configured=ffmpeg_configured
        )
        if output_duration_ms:
            difference = abs(output_duration_ms - target_duration_ms)
            if difference > 500:
                raise FfmpegError(
                    f"音频时长与源视频不一致（源 {target_duration_ms}ms，"
                    f"音频 {output_duration_ms}ms）。"
                )
            if difference > 100:
                print(
                    f"[warn] 提取的 MP3 与源视频时长相差 {difference}ms",
                    flush=True,
                )


def cut_clip(input_path: str, start_ms: int, end_ms: int, output_path: str,
             ffmpeg_configured: str = "") -> None:
    """按区间裁剪并重新编码（与 FfmpegUtils::cutClip 完全一致）。"""
    if end_ms <= start_ms:
        raise FfmpegError("切片时间范围无效。")
    output_dir = os.path.dirname(os.path.abspath(output_path))
    os.makedirs(output_dir, exist_ok=True)

    start_seconds = start_ms / 1000.0
    duration_seconds = (end_ms - start_ms) / 1000.0
    args = [
        "-y", "-nostdin", "-hide_banner", "-loglevel", "error",
        "-ss", f"{start_seconds:.3f}",
        "-t", f"{duration_seconds:.3f}",
        "-i", input_path,
        "-map", "0:v:0", "-map", "0:a:0?",
        *_CUT_VIDEO_ARGS,
        output_path,
    ]
    run_ffmpeg(args, 10 * 60 * 1000, ffmpeg_configured)

    if not os.path.isfile(output_path) or os.path.getsize(output_path) <= 0:
        raise FfmpegError("视频裁剪失败，未生成有效文件。")


def normalize_audio_aac(input_path: str, output_path: str,
                        ffmpeg_configured: str = "") -> None:
    """mp4 直链下载后的音频归一化：视频直拷，音频转 AAC。"""
    args = [
        "-y", "-nostdin", "-hide_banner", "-loglevel", "error",
        "-i", input_path,
        "-map", "0:v:0", "-map", "0:a:0",
        "-c:v", "copy",
        "-c:a", "aac", "-b:a", "128k",
        "-movflags", "+faststart",
        output_path,
    ]
    run_ffmpeg(args, 30 * 60 * 1000, ffmpeg_configured)
    if not os.path.isfile(output_path) or os.path.getsize(output_path) <= 0:
        raise FfmpegError("音频格式转换失败。")


def recording_mp4_path(playlist_path: str) -> str:
    """HLS 录制对应的完整 MP4 路径： <录制目录>/index.mp4。"""
    if not playlist_path.lower().endswith(".m3u8"):
        return playlist_path
    return os.path.join(os.path.dirname(os.path.abspath(playlist_path)), "index.mp4")


def _is_usable_mp4(path: str, playlist_mtime: float, playlist_ms: int,
                   tolerance_ms: int, ffmpeg_configured: str) -> bool:
    if not os.path.isfile(path) or os.path.getsize(path) <= 0:
        return False
    if os.path.getmtime(path) < playlist_mtime:
        return False
    duration_ms = probe.media_duration_ms(path, ffmpeg_configured=ffmpeg_configured)
    if not duration_ms or duration_ms <= 0:
        return False
    return playlist_ms <= 0 or duration_ms + tolerance_ms >= playlist_ms


def ensure_recording_mp4(playlist_path: str, ffmpeg_configured: str = "") -> str:
    """把 HLS 录像无损合成为完整 MP4；已存在且完整时直接复用。

    与客户端一致：
    - 播放列表含 #EXT-X-ENDLIST 视为正常完成，容差 2s；否则 6s；
    - 中断录像先补一份带 ENDLIST 的临时播放列表，避免末尾分片被丢弃；
    - 清理超过 1 小时的 .part.mp4 半成品；
    - 产物先写临时文件，校验时长后原子改名为 index.mp4。
    """
    if not playlist_path.lower().endswith(".m3u8"):
        return playlist_path

    output_path = recording_mp4_path(playlist_path)
    playlist_dir = os.path.dirname(os.path.abspath(playlist_path))
    try:
        with open(playlist_path, "rb") as handle:
            playlist_data = handle.read()
    except OSError as error:
        raise FfmpegError(f"无法读取 HLS 播放列表：{playlist_path}（{error}）")

    playlist_complete = b"#EXT-X-ENDLIST" in playlist_data
    playlist_ms = hls_playlist_duration_ms(playlist_path)
    tolerance_ms = 2000 if playlist_complete else 6000
    playlist_mtime = os.path.getmtime(playlist_path)

    if _is_usable_mp4(output_path, playlist_mtime, playlist_ms, tolerance_ms,
                      ffmpeg_configured):
        return output_path

    # 清理上次崩溃留下的半成品，避免长期占用双份磁盘空间。
    import time
    stale_before = time.time() - 3600
    try:
        for name in os.listdir(playlist_dir):
            if name.startswith("index-") and name.endswith(".part.mp4"):
                candidate = os.path.join(playlist_dir, name)
                if os.path.isfile(candidate) and os.path.getmtime(candidate) < stale_before:
                    os.unlink(candidate)
    except OSError:
        pass

    playlist_for_remux = playlist_path
    temp_playlist_path = ""
    if not playlist_complete:
        fd, temp_playlist_path = tempfile.mkstemp(
            prefix=".livemix-remux-", suffix=".m3u8", dir=playlist_dir
        )
        with os.fdopen(fd, "wb") as handle:
            handle.write(playlist_data)
            if not playlist_data.endswith(b"\n"):
                handle.write(b"\n")
            handle.write(b"#EXT-X-ENDLIST\n")
        playlist_for_remux = temp_playlist_path

    fd, temp_output = tempfile.mkstemp(
        prefix="index-", suffix=".part.mp4", dir=playlist_dir
    )
    os.close(fd)
    try:
        args = [
            "-y", "-nostdin", "-hide_banner", "-loglevel", "error",
            "-i", playlist_for_remux,
            "-map", "0:v:0", "-map", "0:a:0?",
            "-c", "copy",
            "-f", "mp4", "-movflags", "+faststart",
            temp_output,
        ]
        run_ffmpeg(args, 30 * 60 * 1000, ffmpeg_configured)

        output_ms = probe.media_duration_ms(
            temp_output, ffmpeg_configured=ffmpeg_configured
        )
        if not output_ms or output_ms <= 0:
            raise FfmpegError("MP4 合成失败：输出文件不可用。")
        if playlist_ms > 0 and output_ms + tolerance_ms < playlist_ms:
            raise FfmpegError(
                f"MP4 合成不完整：输出 {output_ms}ms，播放列表 {playlist_ms}ms。"
            )

        if os.path.exists(output_path):
            os.remove(output_path)
        os.replace(temp_output, output_path)
        os.chmod(output_path, 0o644)
        return output_path
    except Exception:
        try:
            os.unlink(temp_output)
        except OSError:
            pass
        raise
    finally:
        if temp_playlist_path:
            try:
                os.unlink(temp_playlist_path)
            except OSError:
                pass


def extract_thumbnail(input_path: str, position_ms: int, output_path: str,
                      ffmpeg_configured: str = "") -> None:
    """视频库封面帧（640 宽），技能内主要用于预览与排障。"""
    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
    seconds = max(0, position_ms) / 1000.0
    args = [
        "-y", "-nostdin", "-hide_banner", "-loglevel", "error",
        "-ss", f"{seconds:.3f}", "-i", input_path,
        "-frames:v", "1", "-vf", "scale=640:-2", output_path,
    ]
    run_ffmpeg(args, 30000, ffmpeg_configured)
    if not os.path.isfile(output_path) or os.path.getsize(output_path) <= 0:
        raise FfmpegError("封面截帧失败。")
