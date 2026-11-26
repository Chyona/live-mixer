"""抖音直播间解析（对应 RecordingService 的 DouyinResolve 流程）。

直播间页面内嵌的流地址带签名且会过期，因此每次录制都重新拉页面：
1. 用浏览器 UA 打开直播间页面（支持 v.douyin.com 短链跳转）；
2. 优先解析 flv_pull_url（长连接更适合长时间录制），其次
   hls_pull_url_map / hls_pull_url / 页面任意 m3u8；
3. 确认解析出的流可播放后再交给 ffmpeg 录制；
4. 视频库中保存的仍是原始直播间链接。
"""

from __future__ import annotations

import json
import re
import urllib.parse
from typing import Any, Dict

import http_client as http

USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)
PAGE_TIMEOUT_SECONDS = 25
STREAM_PROBE_TIMEOUT_SECONDS = 8
PAGE_LIMIT_BYTES = 16 * 1024 * 1024

_ROOM_ID_PATTERN = re.compile(r"^/(\d+)")
_HLS_STRING_PATTERN = re.compile(r'"hls_pull_url"\s*:\s*"([^"]+)"')
_M3U8_PATTERN = re.compile(r'https?://[^"]*\.m3u8[^"]*')


class DouyinError(RuntimeError):
    pass


def is_live_room_url(url: str) -> bool:
    try:
        host = urllib.parse.urlsplit(url).hostname or ""
    except ValueError:
        return False
    return host.lower() == "live.douyin.com"


def is_share_url(url: str) -> bool:
    if is_live_room_url(url):
        return True
    try:
        host = urllib.parse.urlsplit(url).hostname or ""
    except ValueError:
        return False
    return host.lower() == "v.douyin.com"


def room_id_from_url(url: str) -> str:
    try:
        path = urllib.parse.urlsplit(url).path
    except ValueError:
        return ""
    match = _ROOM_ID_PATTERN.match(path)
    return match.group(1) if match else ""


def _decode_url(url: str) -> str:
    return (
        url.replace("\\u0026", "&")
        .replace("\\u002F", "/")
        .replace("\\u003D", "=")
        .replace("\\/", "/")
    )


def _normalized_html(body: bytes) -> str:
    html = body.decode("utf-8", errors="replace")
    return html.replace('\\"', '"')


def _json_object_at(text: str, start: int) -> str:
    depth = 0
    in_string = False
    index = start
    while index < len(text):
        ch = text[index]
        if in_string:
            if ch == "\\":
                index += 1
            elif ch == '"':
                in_string = False
            index += 1
            continue
        if ch == '"':
            in_string = True
        elif ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return text[start:index + 1]
        index += 1
    return ""


def _quality_url(urls: Dict[str, Any]) -> str:
    for quality in ("HD1", "FULL_HD1", "SD1", "SD2"):
        url = urls.get(quality)
        if isinstance(url, str) and url.lower().startswith("http"):
            return url
    for url in urls.values():
        if isinstance(url, str) and url.lower().startswith("http"):
            return url
    return ""


def _pull_url_map(html: str, key: str) -> str:
    needle = f'"{key}":'
    start = 0
    while True:
        key_pos = html.find(needle, start)
        if key_pos < 0:
            return ""
        start = key_pos + len(needle)
        brace_pos = start
        while brace_pos < len(html) and html[brace_pos].isspace():
            brace_pos += 1
        if brace_pos >= len(html) or html[brace_pos] != "{":
            continue
        object_text = _json_object_at(html, brace_pos)
        if not object_text:
            continue
        try:
            urls = json.loads(object_text)
        except ValueError:
            continue
        if isinstance(urls, dict):
            url = _quality_url(urls)
            if url:
                return url


def stream_from_html(body: bytes) -> Dict[str, str]:
    html = _normalized_html(body)
    if not html:
        return {"uri": "", "kind": ""}

    # 优先 FLV：长连接更适合长时间录制。
    uri = _pull_url_map(html, "flv_pull_url")
    if uri:
        return {"uri": uri, "kind": "flv"}

    uri = _pull_url_map(html, "hls_pull_url_map")
    if uri:
        return {"uri": uri, "kind": "hls"}

    match = _HLS_STRING_PATTERN.search(html)
    if match:
        return {"uri": _decode_url(match.group(1)), "kind": "hls"}

    match = _M3U8_PATTERN.search(html)
    if match:
        return {"uri": _decode_url(match.group(0)), "kind": "hls"}

    return {"uri": "", "kind": ""}


def _page_headers() -> Dict[str, str]:
    return {
        "User-Agent": USER_AGENT,
        "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        "Accept-Language": "zh-CN,zh;q=0.9",
        "Referer": "https://live.douyin.com/",
    }


def _stream_headers() -> Dict[str, str]:
    return {
        "User-Agent": USER_AGENT,
        "Accept": "*/*",
        "Range": "bytes=0-4095",
        "Referer": "https://live.douyin.com/",
    }


def resolve(page_uri: str, *, page_timeout: float = PAGE_TIMEOUT_SECONDS,
            probe_timeout: float = STREAM_PROBE_TIMEOUT_SECONDS) -> Dict[str, Any]:
    """解析直播间并确认流可播放；失败抛 DouyinError。"""
    room_id = room_id_from_url(page_uri)

    response = http.request(
        "GET", page_uri, headers=_page_headers(), timeout=page_timeout,
        max_bytes=PAGE_LIMIT_BYTES,
    )
    if not response.network_ok:
        if not response.body:
            reason = response.error or "网络错误"
            raise DouyinError(f"抖音直播间解析失败：{reason}")
    if response.status >= 400:
        raise DouyinError(
            f"抖音直播间解析失败：页面返回 HTTP {response.status}"
            "（房间不存在或链接已失效）。"
        )

    final_uri = response.url or page_uri
    if not is_live_room_url(final_uri):
        raise DouyinError("抖音直播间解析失败：该链接不是有效的抖音直播间地址。")
    if not room_id:
        room_id = room_id_from_url(final_uri)
    if not room_id:
        raise DouyinError("抖音直播间解析失败：无法从链接中识别直播间号。")

    stream = stream_from_html(response.body[:PAGE_LIMIT_BYTES])
    if not stream["uri"]:
        raise DouyinError(
            "抖音直播间解析失败：未找到直播流，可能未开播、已下播，"
            "或该直播需要登录才能观看。"
        )

    probe = http.probe_headers(
        stream["uri"], headers=_stream_headers(), timeout=probe_timeout
    )
    if not probe.network_ok and probe.status == 0:
        raise DouyinError(
            f"抖音直播录制失败：{probe.error or '无法连接直播间流。'}"
        )
    if probe.status == 403:
        raise DouyinError(
            "抖音直播录制失败：直播间流拒绝访问（HTTP 403，"
            "可能已下播或链接已失效）。"
        )
    if probe.status >= 400:
        raise DouyinError(
            f"抖音直播录制失败：直播间流不可用（HTTP {probe.status}，可能已下播）。"
        )
    if probe.status < 200 or probe.status >= 300:
        raise DouyinError("抖音直播录制失败：无法连接直播间流。")

    return {
        "room_uri": final_uri,
        "room_id": room_id,
        "stream_uri": stream["uri"],
        "stream_kind": stream["kind"],
        "title": f"抖音直播 {room_id}",
    }
