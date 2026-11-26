"""剪映小助手（CapCut Mate）接口客户端。

逐字段移植 LiveMix/src/api/CapCutMateClient.cpp：
- 基地址 https://capcut-mate.jcaigc.cn，路径前缀 /openapi/capcut-mate/v1/；
- 所有写接口都携带 create_draft 返回的 draft_url，原样透传；
- 列表字段 video_infos / image_infos / captions / audio_infos /
  effect_infos / filter_infos 是 JSON 字符串；scene_timelines 是数组；
- 剪映小助手的业务错误同样以 HTTP 200 返回，真实结果在 code 字段。
"""

from __future__ import annotations

import os
import urllib.parse
from typing import Any, Dict, List, Sequence

import http_client as http
from util import json_compact

CAPCUT_BASE_URL = "https://capcut-mate.jcaigc.cn"
CAPCUT_API_PREFIX = "/openapi/capcut-mate/v1"


class CapCutError(RuntimeError):
    pass


def endpoint(name: str) -> str:
    return f"{CAPCUT_BASE_URL}{CAPCUT_API_PREFIX}/{name}"


def _check_envelope(response: http.HttpResponse) -> Dict[str, Any]:
    if not response.network_ok:
        raise CapCutError(response.error or "网络请求失败。")
    if response.status != 200:
        raise CapCutError(f"服务返回 HTTP {response.status}。")
    payload = response.json
    if not isinstance(payload, dict) or not payload:
        raise CapCutError("服务返回了无法解析的响应。")
    code = payload.get("code", -1)
    try:
        code = int(code)
    except (TypeError, ValueError):
        code = -1
    if code != 0:
        message = str(payload.get("message", "")).strip()
        raise CapCutError(message or f"服务返回错误码 {code}。")
    return payload


def draft_id_from_url(draft_url: str) -> str:
    try:
        query = urllib.parse.urlparse(draft_url).query
        values = urllib.parse.parse_qs(query, keep_blank_values=True)
        return (values.get("draft_id") or [""])[0]
    except ValueError:
        return ""


def _file_mime(path: str) -> str:
    suffix = os.path.splitext(path)[1].lstrip(".").lower()
    if suffix == "mp3":
        return "audio/mpeg"
    if suffix == "wav":
        return "audio/wav"
    return "video/mp4"


def _upload_name(path: str) -> str:
    suffix = os.path.splitext(path)[1].lstrip(".").lower()
    return f"upload.{suffix}" if suffix else "upload.bin"


def upload_file(file_path: str, api_key: str, *,
                timeout: float = 15 * 60) -> str:
    if not os.path.isfile(file_path):
        raise CapCutError(f"待上传文件不存在：{file_path}")
    response = http.post_multipart(
        endpoint("upload_file"),
        [("apiKey", api_key)],
        file_path=file_path,
        file_field="file",
        file_name=_upload_name(file_path),
        file_mime=_file_mime(file_path),
        timeout=timeout,
    )
    payload = _check_envelope(response)
    remote_url = str(payload.get("url", "")).strip()
    if not remote_url:
        raise CapCutError("上传成功但未返回文件 URL。")
    return remote_url


def create_draft(width: int, height: int) -> Dict[str, str]:
    response = http.post_json(
        endpoint("create_draft"),
        {"width": max(1, int(width)), "height": max(1, int(height))},
        timeout=60,
    )
    payload = _check_envelope(response)
    draft_url = str(payload.get("draft_url", ""))
    draft_id = draft_id_from_url(draft_url)
    if not draft_url or not draft_id:
        raise CapCutError("创建草稿成功但未返回有效 draft_url。")
    return {"draft_url": draft_url, "draft_id": draft_id}


def add_videos(draft_url: str, videos: Sequence[Dict[str, Any]],
               scale_x: float = 1.0, scale_y: float = 1.0, *,
               timeout: float = 120) -> None:
    if not videos:
        raise CapCutError("没有可添加到草稿的视频片段。")

    video_infos: List[Dict[str, Any]] = []
    scene_timelines: List[Dict[str, int]] = []
    has_scene_timelines = True

    for video in videos:
        info = {
            "video_url": video.get("url", ""),
            "start": int(video.get("start_us", 0)),
            "end": int(video.get("end_us", 0)),
        }
        transition = str(video.get("transition", "") or "").strip()
        transition_duration = int(video.get("transition_duration_us", 0) or 0)
        if transition and transition_duration > 0:
            info["transition"] = transition
            info["transition_duration"] = transition_duration
        video_infos.append(info)

        scene_start = int(video.get("scene_start_us", -1))
        scene_end = int(video.get("scene_end_us", -1))
        if scene_start >= 0 and scene_end > scene_start:
            scene_timelines.append({"start": scene_start, "end": scene_end})
        else:
            has_scene_timelines = False

    payload: Dict[str, Any] = {
        "draft_url": draft_url,
        "video_infos": json_compact(video_infos),
        "scale_x": scale_x,
        "scale_y": scale_y,
    }
    if has_scene_timelines:
        payload["scene_timelines"] = scene_timelines

    response = http.post_json(endpoint("add_videos"), payload, timeout=timeout)
    _check_envelope(response)


def add_images(draft_url: str, images: Sequence[Dict[str, Any]],
               scale_x: float, scale_y: float, transform_x: int,
               transform_y: int, alpha: float, *, timeout: float = 120) -> None:
    image_infos = []
    for image in images:
        url = str(image.get("url", "")).strip()
        start_us = int(image.get("start_us", 0))
        end_us = int(image.get("end_us", 0))
        if not url or end_us <= start_us:
            continue
        image_infos.append({
            "image_url": url,
            "start": start_us,
            "end": end_us,
        })
    if not image_infos:
        raise CapCutError("没有可添加到草稿的有效图片。")

    payload = {
        "draft_url": draft_url,
        "image_infos": json_compact(image_infos),
        "scale_x": scale_x,
        "scale_y": scale_y,
        "transform_x": int(transform_x),
        "transform_y": int(transform_y),
        "alpha": alpha,
    }
    response = http.post_json(endpoint("add_images"), payload, timeout=timeout)
    _check_envelope(response)


def add_sticker(draft_url: str, sticker_id: str, start_us: int, end_us: int,
                scale: float, transform_x: int, transform_y: int, *,
                timeout: float = 60) -> None:
    if not sticker_id.strip() or end_us <= start_us:
        raise CapCutError("贴纸参数无效。")
    payload = {
        "draft_url": draft_url,
        "sticker_id": sticker_id.strip(),
        "start": int(start_us),
        "end": int(end_us),
        "scale": scale,
        "transform_x": int(transform_x),
        "transform_y": int(transform_y),
    }
    response = http.post_json(endpoint("add_sticker"), payload, timeout=timeout)
    _check_envelope(response)


def add_captions(draft_url: str, captions: Sequence[Dict[str, Any]],
                 style: Dict[str, Any], *, timeout: float = 120) -> None:
    caption_infos = []
    for caption in captions:
        text = str(caption.get("text", "")).strip()
        start_us = int(caption.get("start_us", 0))
        end_us = int(caption.get("end_us", 0))
        if end_us <= start_us or not text:
            continue
        info: Dict[str, Any] = {
            "start": start_us,
            "end": end_us,
            "text": text,
        }
        in_animation = str(
            caption.get("in_animation", "")
            or style.get("in_animation", "")
            or ""
        ).strip()
        if in_animation:
            info["in_animation"] = in_animation
        caption_infos.append(info)
    if not caption_infos:
        return

    payload: Dict[str, Any] = {
        "draft_url": draft_url,
        "captions": json_compact(caption_infos),
        "text_color": style.get("text_color", "#ffffff"),
        "font_size": int(style.get("font_size", 15)),
        "transform_x": style.get("transform_x", 0.0),
        "transform_y": style.get("transform_y", -800.0),
    }
    if style.get("border_color"):
        payload["border_color"] = style["border_color"]
    if style.get("font"):
        payload["font"] = style["font"]

    response = http.post_json(endpoint("add_captions"), payload, timeout=timeout)
    _check_envelope(response)


def add_effects(draft_url: str, effects: Sequence[Dict[str, Any]],
                *, timeout: float = 120) -> None:
    if not effects:
        return
    effect_infos = []
    for effect in effects:
        title = str(effect.get("title", "")).strip()
        start_us = int(effect.get("start_us", 0))
        end_us = int(effect.get("end_us", 0))
        if end_us <= start_us or not title:
            continue
        effect_infos.append({
            "effect_title": title, "start": start_us, "end": end_us,
        })
    if not effect_infos:
        return
    payload = {
        "draft_url": draft_url,
        "effect_infos": json_compact(effect_infos),
    }
    response = http.post_json(endpoint("add_effects"), payload, timeout=timeout)
    _check_envelope(response)


def add_filters(draft_url: str, filters: Sequence[Dict[str, Any]],
                *, timeout: float = 120) -> None:
    if not filters:
        return
    filter_infos = []
    for item in filters:
        title = str(item.get("title", "")).strip()
        start_us = int(item.get("start_us", 0))
        end_us = int(item.get("end_us", 0))
        if end_us <= start_us or not title:
            continue
        filter_infos.append({
            "filter_title": title,
            "start": start_us,
            "end": end_us,
            "intensity": float(item.get("intensity", 100.0)),
        })
    if not filter_infos:
        return
    payload = {
        "draft_url": draft_url,
        "filter_infos": json_compact(filter_infos),
    }
    response = http.post_json(endpoint("add_filters"), payload, timeout=timeout)
    _check_envelope(response)


def add_audios(draft_url: str, audios: Sequence[Dict[str, Any]],
               *, timeout: float = 120) -> None:
    if not audios:
        return
    audio_infos = []
    for audio in audios:
        url = str(audio.get("url", "")).strip()
        start_us = int(audio.get("start_us", 0))
        end_us = int(audio.get("end_us", 0))
        if end_us <= start_us or not url:
            continue
        info: Dict[str, Any] = {
            "audio_url": url,
            "start": start_us,
            "end": end_us,
            "volume": float(audio.get("volume", 0.1)),
        }
        duration_us = int(audio.get("duration_us", -1))
        if duration_us > 0:
            info["duration"] = duration_us
        audio_infos.append(info)
    if not audio_infos:
        return
    payload = {
        "draft_url": draft_url,
        "audio_infos": json_compact(audio_infos),
    }
    response = http.post_json(endpoint("add_audios"), payload, timeout=timeout)
    _check_envelope(response)


def gen_video(draft_url: str, api_key: str, *, timeout: float = 60) -> None:
    if not (draft_url or "").strip():
        raise CapCutError("草稿地址为空，无法导出成片。")
    if not (api_key or "").strip():
        raise CapCutError("尚未配置 API 密钥，无法导出成片。")
    payload = {"apiKey": api_key.strip(), "draft_url": draft_url}
    response = http.post_json(endpoint("gen_video"), payload, timeout=timeout)
    _check_envelope(response)


def gen_video_status(draft_url: str, *, timeout: float = 60) -> Dict[str, Any]:
    if not (draft_url or "").strip():
        raise CapCutError("草稿地址为空，无法查询成片状态。")
    response = http.post_json(
        endpoint("gen_video_status"), {"draft_url": draft_url}, timeout=timeout
    )
    payload = _check_envelope(response)
    status = str(payload.get("status", "")).strip().lower()
    try:
        progress = int(payload.get("progress", 0))
    except (TypeError, ValueError):
        progress = 0
    error_message = str(payload.get("error_message", "")).strip()
    if not error_message:
        error_message = str(payload.get("message", "")).strip()
    return {
        "status": status,
        "progress": max(0, min(100, progress)),
        "video_url": str(payload.get("video_url", "")).strip(),
        "error_message": error_message,
    }


def fetch_draft_files(draft_id: str, *, timeout: float = 60) -> List[str]:
    url = endpoint("get_draft") + "?" + urllib.parse.urlencode({"draft_id": draft_id})
    response = http.get(url, timeout=timeout)
    payload = _check_envelope(response)
    files = payload.get("files")
    file_urls = [
        value for value in files
        if isinstance(value, str) and value
    ] if isinstance(files, list) else []
    if not file_urls:
        raise CapCutError("草稿文件列表为空。")
    return file_urls
