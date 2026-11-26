"""素材服务（assets OpenAPI）：随机 BGM、随机去重图片、音效目录。

与 LiveMix/src/api/AssetsClient.cpp 一致：
- 基地址 https://assets.gogoshine.com；
- GET 请求使用 HMAC-SHA256 签名头 X-Access-Key / X-Timestamp /
  X-Nonce / X-Signature；
- 签名字符串 = GET\\npath\\ncanonicalQuery\\ntimestamp\\nnonce\\nbodyHash；
- 凭证为编译期内置常量，不写入配置文件。
"""

from __future__ import annotations

import hashlib
import hmac
import random
import time
import urllib.parse
from typing import Any, Dict, List, Optional, Sequence

import http_client as http

ASSETS_BASE_URL = "https://assets.gogoshine.com"
ASSETS_API_PREFIX = "/openapi/assets/v1"
ASSETS_ACCESS_KEY = "ak_13398500743920e8a34e115fb46898fc"
ASSETS_SECRET_KEY = "sk_5237a982ce2b8893c06b10e36633682ae73c887149c0e997"

_EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


class AssetsError(RuntimeError):
    pass


def _service_url(endpoint_name: str, query: Optional[Sequence[tuple]] = None) -> str:
    url = ASSETS_BASE_URL.rstrip("/") + ASSETS_API_PREFIX + "/" + endpoint_name
    if query:
        url += "?" + urllib.parse.urlencode(query, quote_via=urllib.parse.quote)
    return url


def _canonical_query(query: str) -> str:
    pairs = urllib.parse.parse_qsl(query, keep_blank_values=True)
    encoded = []
    for key, value in pairs:
        encoded.append((
            urllib.parse.quote(key, safe=""),
            urllib.parse.quote(value, safe=""),
        ))
    encoded.sort()
    return "&".join(f"{key}={value}" for key, value in encoded)


def _make_nonce() -> str:
    return f"{random.SystemRandom().getrandbits(64):016x}"


def _signed_headers(url: str) -> Dict[str, str]:
    parsed = urllib.parse.urlparse(url)
    timestamp = str(int(time.time()))
    nonce = _make_nonce()
    canonical = "\n".join([
        "GET",
        parsed.path,
        _canonical_query(parsed.query),
        timestamp,
        nonce,
        _EMPTY_SHA256,
    ])
    signature = hmac.new(
        ASSETS_SECRET_KEY.encode("utf-8"),
        canonical.encode("utf-8"),
        hashlib.sha256,
    ).hexdigest()
    return {
        "X-Access-Key": ASSETS_ACCESS_KEY,
        "X-Timestamp": timestamp,
        "X-Nonce": nonce,
        "X-Signature": signature,
    }


def _check_response(response: http.HttpResponse) -> Dict[str, Any]:
    if not response.network_ok:
        raise AssetsError(response.error or "网络请求失败。")
    if response.status != 200:
        if response.status == 401:
            raise AssetsError("素材服务鉴权失败（HTTP 401），请检查凭证。")
        if response.status == 403:
            raise AssetsError(
                "素材服务拒绝访问（HTTP 403），请确认凭证已开通对应资源权限。"
            )
        raise AssetsError(f"素材服务返回 HTTP {response.status}。")
    payload = response.json
    if not isinstance(payload, dict) or not payload:
        raise AssetsError("素材服务返回了无法解析的响应。")
    try:
        code = int(payload.get("code", -1))
    except (TypeError, ValueError):
        code = -1
    if code != 0:
        message = str(payload.get("message", "")).strip()
        raise AssetsError(message or f"素材服务返回错误码 {code}。")
    data = payload.get("data")
    return data if isinstance(data, dict) else {}


def _signed_get(url: str) -> Dict[str, Any]:
    response = http.get_with_headers(url, _signed_headers(url), timeout=15)
    return _check_response(response)


def random_bgm() -> Dict[str, Any]:
    url = _service_url("bgm/random", [("limit", "1")])
    data = _signed_get(url)
    items = data.get("items")
    if not isinstance(items, list) or not items:
        raise AssetsError("素材服务没有可用的背景音乐。")
    item = items[0] if isinstance(items[0], dict) else {}
    file_url = str(item.get("file_url", "")).strip()
    if not file_url:
        raise AssetsError("素材服务返回的背景音乐缺少文件地址。")
    name = str(item.get("name", "")).strip() or "背景音乐"
    try:
        duration_ms = int(float(item.get("duration_ms", 0) or 0))
    except (TypeError, ValueError):
        duration_ms = 0
    return {"url": file_url, "name": name, "duration_ms": duration_ms}


def random_images(limit: int, tags: str = "") -> List[Dict[str, Any]]:
    query = [("limit", str(max(1, min(100, int(limit)))))]
    if tags.strip():
        query.append(("tags", tags.strip()))
    data = _signed_get(_service_url("images/random", query))
    items = data.get("items")
    images: List[Dict[str, Any]] = []
    if isinstance(items, list):
        for value in items:
            if not isinstance(value, dict):
                continue
            file_url = str(value.get("file_url", "")).strip()
            if not file_url:
                continue
            images.append({
                "url": file_url,
                "width": int(value.get("width", 0) or 0),
                "height": int(value.get("height", 0) or 0),
            })
    return images


def list_sfx() -> List[Dict[str, Any]]:
    data = _signed_get(_service_url("sfx", [("page", "1"), ("page_size", "100")]))
    items = data.get("items")
    assets: List[Dict[str, Any]] = []
    if isinstance(items, list):
        for value in items:
            if not isinstance(value, dict):
                continue
            resource_id = str(value.get("resource_id", "")).strip()
            name = str(value.get("name", "")).strip()
            file_url = str(value.get("file_url", "")).strip()
            try:
                duration_ms = int(float(value.get("duration_ms", 0) or 0))
            except (TypeError, ValueError):
                duration_ms = 0
            if not resource_id or not name or not file_url or duration_ms <= 0:
                continue
            assets.append({
                "resource_id": resource_id,
                "name": name,
                "url": file_url,
                "duration_ms": duration_ms,
            })
    return assets
