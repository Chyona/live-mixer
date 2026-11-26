"""云端导出成片：gen_video → gen_video_status 轮询 → 下载 MP4。

与 LiveMix/src/services/VideoExporter.cpp 一致：
- 提交后首次等待 1.5s，其后每 3s 轮询，最多 600 次（约 30 分钟）；
- completed / failed / pending / processing 四种状态；
- 完成后以 1 小时超时下载到「成片导出目录/<标题>.mp4」。
"""

from __future__ import annotations

import os
import time
from typing import Callable, Optional

import capcut
import http_client as http
from util import sanitize_file_component

POLL_INTERVAL_SECONDS = 3
FIRST_POLL_DELAY_SECONDS = 1.5
MAX_POLLS = 600
DOWNLOAD_TIMEOUT_SECONDS = 60 * 60

ProgressCallback = Optional[Callable[[str], None]]


class VideoExportError(RuntimeError):
    def __init__(self, message: str, video_url: str = "") -> None:
        super().__init__(message)
        self.video_url = video_url


def _download_error(error: str, video_url: str) -> VideoExportError:
    # 保留已生成成片的 URL，调用方可用 --resume-url 续传，避免重复计费。
    return VideoExportError(error or "成片下载失败", video_url=video_url)


def export_video(
    draft_url: str,
    api_key: str,
    export_dir: str,
    title: str,
    *,
    resume_video_url: str = "",
    progress: ProgressCallback = None,
) -> str:
    """导出成片并返回本地 MP4 路径。"""
    def notify(message: str) -> None:
        if progress:
            progress(message)

    if not (draft_url or "").strip():
        raise VideoExportError("草稿记录缺少 draft_url，无法导出成片。")
    if not (export_dir or "").strip():
        raise VideoExportError("尚未配置成片导出目录。")
    os.makedirs(export_dir, exist_ok=True)

    name = sanitize_file_component((title or "").strip() or "成片", 80) or "成片"
    output_path = os.path.join(export_dir, name + ".mp4")

    video_url = resume_video_url.strip()
    if not video_url:
        notify("提交云端合成…")
        capcut.gen_video(draft_url, api_key)

        completed = False
        for poll in range(MAX_POLLS):
            time.sleep(FIRST_POLL_DELAY_SECONDS if poll == 0 else POLL_INTERVAL_SECONDS)
            status = capcut.gen_video_status(draft_url)
            state = status["status"]
            remote = status["progress"]

            if state == "completed":
                video_url = status["video_url"]
                if not video_url:
                    raise VideoExportError("云端合成完成，但未返回成片地址。")
                completed = True
                break
            if state == "failed":
                raise VideoExportError(
                    status["error_message"] or "云端合成失败"
                )
            if state and state not in ("pending", "processing"):
                raise VideoExportError(f"云端合成返回未知状态：{state}")

            notify(f"云端合成中：{remote}%")
            if status["video_url"]:
                video_url = status["video_url"]

        if not completed:
            raise VideoExportError("云端合成超时，请稍后重试。")

    notify("下载成片…")

    def on_progress(received: int, total: int) -> None:
        if total > 0:
            percent = int(received * 100 / total)
            notify(f"下载成片：{percent}%")
        else:
            notify(f"下载成片：{received // (1024 * 1024)} MB")

    ok, error = http.download_to_file(
        video_url, output_path,
        timeout=DOWNLOAD_TIMEOUT_SECONDS,
        progress=on_progress,
    )
    if not ok:
        raise _download_error(error, video_url)

    notify(f"成片已导出：{output_path}")
    return output_path
