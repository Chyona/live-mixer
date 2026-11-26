"""同步 HTTP 客户端（urllib 标准库实现）。

对应 LiveMix/src/api/HttpClient.cpp：
- GET / JSON POST / multipart 上传 / SSE 流式读取 / 流式下载；
- 下载失败不留半截文件（同 QSaveFile）；
- 错误信息与客户端一致地去掉带签名的 URL。

脚本是顺序执行模型，不需要 Qt 的嵌套事件循环；urllib 的 timeout 即
「单次 socket 操作超时」，用于 SSE 时等价于空闲超时。
"""

from __future__ import annotations

import json
import os
import socket
import ssl
import tempfile
import time
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, Iterable, Optional, Tuple

USER_AGENT = "LiveMixStudio/1.0"

_SSL_CONTEXT = ssl.create_default_context()
_OPENER = urllib.request.build_opener(
    urllib.request.HTTPSHandler(context=_SSL_CONTEXT),
    urllib.request.HTTPRedirectHandler(),
)


@dataclass
class HttpResponse:
    network_ok: bool = False
    status: int = 0
    body: bytes = b""
    headers: Dict[str, str] = field(default_factory=dict)
    error: str = ""
    url: str = ""
    json: Any = None

    @property
    def is_http_ok(self) -> bool:
        return self.network_ok and 200 <= self.status < 300


def _parse_json(body: bytes) -> Any:
    try:
        return json.loads(body.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return None


def _request_headers(extra: Optional[Dict[str, str]] = None,
                     accept: str = "application/json") -> Dict[str, str]:
    headers = {
        "Accept": accept,
        "User-Agent": USER_AGENT,
    }
    if extra:
        for key, value in extra.items():
            headers[key] = value
    return headers


def _timeout_text(timeout_seconds: float) -> str:
    return f"请求超时（{int(timeout_seconds)} 秒）"


def request(
    method: str,
    url: str,
    *,
    data: Any = None,
    headers: Optional[Dict[str, str]] = None,
    timeout: float = 60,
    max_bytes: Optional[int] = None,
) -> HttpResponse:
    """执行一次请求并读取完整响应体。

    data 可以是 bytes，也可以是带 read() 的文件对象（大文件上传用）。
    """
    response = HttpResponse()
    if data is not None and hasattr(data, "read"):
        # urllib 不会替文件对象补 Content-Length；必须显式给出，否则
        # 部分网关会一直等待请求体结束。
        merged = dict(headers or {})
        if not any(key.lower() == "content-length" for key in merged):
            try:
                current = data.tell()
                data.seek(0, os.SEEK_END)
                size = data.tell()
                data.seek(current)
            except (OSError, AttributeError):
                size = None
            if size is not None:
                merged["Content-Length"] = str(size)
        headers = merged
    request_obj = urllib.request.Request(url, data=data, method=method)
    for key, value in (headers or {}).items():
        request_obj.add_header(key, value)
    try:
        with _OPENER.open(request_obj, timeout=timeout) as raw:
            response.network_ok = True
            response.status = getattr(raw, "status", 0) or 200
            response.url = raw.geturl()
            response.headers = {k.lower(): v for k, v in raw.headers.items()}
            response.body = raw.read() if max_bytes is None else raw.read(max_bytes)
    except urllib.error.HTTPError as error:
        # 同 Qt：收到 HTTP 响应即视为网络层成功，业务错误由状态码/响应体判断。
        response.network_ok = True
        response.status = error.code
        response.url = error.geturl() or url
        response.headers = {k.lower(): v for k, v in (error.headers or {}).items()}
        try:
            response.body = (
                error.read() if max_bytes is None else error.read(max_bytes)
            )
        except OSError:
            response.body = b""
    except (urllib.error.URLError, socket.timeout, TimeoutError, OSError) as error:
        response.network_ok = False
        reason = getattr(error, "reason", None)
        if isinstance(error, (socket.timeout, TimeoutError)) or isinstance(reason, socket.timeout):
            response.error = _timeout_text(timeout)
        else:
            response.error = str(reason or error)
        return response

    response.json = _parse_json(response.body)
    return response


def get(url: str, *, headers: Optional[Dict[str, str]] = None,
        timeout: float = 60) -> HttpResponse:
    return request("GET", url, headers=_request_headers(headers), timeout=timeout)


def get_with_headers(url: str, headers: Dict[str, str], *,
                     timeout: float = 30) -> HttpResponse:
    merged = _request_headers(headers)
    return request("GET", url, headers=merged, timeout=timeout)


def post_json(url: str, payload: Any, *,
              headers: Optional[Dict[str, str]] = None,
              authorization: str = "",
              timeout: float = 120) -> HttpResponse:
    merged = _request_headers(headers)
    merged["Content-Type"] = "application/json"
    if authorization:
        merged["Authorization"] = authorization
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    return request("POST", url, data=body, headers=merged, timeout=timeout)


def _write_multipart(spool: Any, fields: Iterable[Tuple[str, str]],
                     file_path: Optional[str], file_field: str,
                     file_name: Optional[str],
                     file_mime: Optional[str]) -> str:
    """把 multipart 写入可溢出到磁盘的临时文件，避免大文件占满内存。"""
    boundary = "----LiveMixSkill" + uuid.uuid4().hex

    def write(chunk: bytes) -> None:
        spool.write(chunk)

    for name, value in fields:
        write(f"--{boundary}\r\n".encode("utf-8"))
        write(
            f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode("utf-8")
        )
        write(str(value).encode("utf-8"))
        write(b"\r\n")

    if file_path:
        if not file_name:
            suffix = os.path.splitext(file_path)[1].lstrip(".").lower()
            file_name = f"upload.{suffix}" if suffix else "upload.bin"
        if not file_mime:
            file_mime = "application/octet-stream"
        write(f"--{boundary}\r\n".encode("utf-8"))
        write(
            (
                f'Content-Disposition: form-data; name="{file_field}"; '
                f'filename="{file_name}"\r\n'
            ).encode("utf-8")
        )
        write(f"Content-Type: {file_mime}\r\n\r\n".encode("utf-8"))
        with open(file_path, "rb") as handle:
            while True:
                chunk = handle.read(1024 * 1024)
                if not chunk:
                    break
                write(chunk)
        write(b"\r\n")

    write(f"--{boundary}--\r\n".encode("utf-8"))
    return f"multipart/form-data; boundary={boundary}"


def post_multipart(
    url: str,
    fields: Iterable[Tuple[str, str]],
    *,
    file_path: Optional[str] = None,
    file_field: str = "file",
    file_name: Optional[str] = None,
    file_mime: Optional[str] = None,
    headers: Optional[Dict[str, str]] = None,
    authorization: str = "",
    timeout: float = 600,
) -> HttpResponse:
    spool = tempfile.SpooledTemporaryFile(max_size=8 * 1024 * 1024, mode="w+b")
    try:
        content_type = _write_multipart(
            spool, fields, file_path, file_field, file_name, file_mime
        )
        size = spool.tell()
        spool.seek(0)
        merged = _request_headers(headers)
        merged["Content-Type"] = content_type
        merged["Content-Length"] = str(size)
        if authorization:
            merged["Authorization"] = authorization
        return request("POST", url, data=spool, headers=merged, timeout=timeout)
    finally:
        spool.close()


def post_json_stream(
    url: str,
    payload: Any,
    *,
    headers: Optional[Dict[str, str]] = None,
    authorization: str = "",
    idle_timeout: float = 300,
    on_line: Optional[Callable[[bytes], bool]] = None,
) -> HttpResponse:
    """POST JSON 并逐行回调 SSE 数据；回调返回 True 时提前结束。

    完整响应体仍会累积在 HttpResponse.body，供网关忽略 stream=true、
    直接返回完整 completion JSON 时兜底解析。
    """
    response = HttpResponse()
    merged = _request_headers(headers)
    merged["Content-Type"] = "application/json"
    merged["Accept"] = "text/event-stream"
    if authorization:
        merged["Authorization"] = authorization

    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    request_obj = urllib.request.Request(url, data=body, method="POST")
    for key, value in merged.items():
        request_obj.add_header(key, value)

    stopped = False
    try:
        with _OPENER.open(request_obj, timeout=idle_timeout) as raw:
            response.network_ok = True
            response.status = getattr(raw, "status", 0) or 200
            response.url = raw.geturl()
            response.headers = {k.lower(): v for k, v in raw.headers.items()}
            while True:
                line = raw.readline()
                if not line:
                    break
                response.body += line
                if on_line is not None:
                    try:
                        if on_line(line):
                            stopped = True
                            break
                    except Exception:
                        # 解析失败不终止流，保持与客户端一致：坏行直接忽略。
                        pass
    except urllib.error.HTTPError as error:
        response.network_ok = True
        response.status = error.code
        response.url = error.geturl() or url
        response.headers = {k.lower(): v for k, v in (error.headers or {}).items()}
        try:
            response.body = error.read()
        except OSError:
            response.body = b""
    except (urllib.error.URLError, socket.timeout, TimeoutError, OSError) as error:
        response.network_ok = False
        reason = getattr(error, "reason", None)
        if isinstance(error, (socket.timeout, TimeoutError)) or isinstance(reason, socket.timeout):
            response.error = f"请求超时（{int(idle_timeout)} 秒未收到数据）"
        else:
            response.error = str(reason or error)

    if stopped:
        response.network_ok = True
    response.json = _parse_json(response.body)
    return response


# ---------------------------------------------------------------------------
# 探测 / 嗅探
# ---------------------------------------------------------------------------


@dataclass
class SniffResult:
    status: int = 0
    final_url: str = ""
    content_type: str = ""
    head: bytes = b""
    network_ok: bool = False
    error: str = ""
    timed_out: bool = False


def sniff(url: str, *, headers: Optional[Dict[str, str]] = None,
          max_bytes: int = 8192, timeout: float = 8) -> SniffResult:
    """读取响应头 + 最多 max_bytes 字节；对应客户端 beginLinkProbe。

    直播流不会结束，因此按块读取并在达到上限/超时时主动关闭，保留已读内容。
    """
    result = SniffResult()
    merged = _request_headers(headers, accept="*/*")
    merged.setdefault("Range", "bytes=0-8191")
    request_obj = urllib.request.Request(url, method="GET")
    for key, value in merged.items():
        request_obj.add_header(key, value)
    started = time.monotonic()
    try:
        with _OPENER.open(request_obj, timeout=timeout) as raw:
            result.network_ok = True
            result.status = getattr(raw, "status", 0) or 200
            result.final_url = raw.geturl()
            result.content_type = raw.headers.get("Content-Type", "")
            collected = bytearray()
            while len(collected) < max_bytes:
                if time.monotonic() - started >= timeout:
                    result.timed_out = True
                    break
                try:
                    chunk = raw.read1(min(1024, max_bytes - len(collected)))
                except (socket.timeout, TimeoutError):
                    result.timed_out = True
                    break
                if not chunk:
                    break
                collected.extend(chunk)
            result.head = bytes(collected[:max_bytes])
    except urllib.error.HTTPError as error:
        result.network_ok = True
        result.status = error.code
        result.final_url = error.geturl() or url
        result.content_type = (error.headers or {}).get("Content-Type", "")
        try:
            result.head = error.read(max_bytes)
        except OSError:
            result.head = b""
    except (urllib.error.URLError, socket.timeout, TimeoutError, OSError) as error:
        result.network_ok = False
        reason = getattr(error, "reason", None)
        if isinstance(error, (socket.timeout, TimeoutError)) or isinstance(reason, socket.timeout):
            result.timed_out = True
            result.error = f"请求超时（{int(timeout)} 秒）"
        else:
            result.error = str(reason or error)
    return result


def probe_headers(url: str, *, headers: Optional[Dict[str, str]] = None,
                  timeout: float = 8) -> SniffResult:
    """只建立连接读取响应头（用于直播流可用性探测），不读取响应体。"""
    result = SniffResult()
    merged = _request_headers(headers, accept="*/*")
    request_obj = urllib.request.Request(url, method="GET")
    for key, value in merged.items():
        request_obj.add_header(key, value)
    try:
        with _OPENER.open(request_obj, timeout=timeout) as raw:
            result.network_ok = True
            result.status = getattr(raw, "status", 0) or 200
            result.final_url = raw.geturl()
            result.content_type = raw.headers.get("Content-Type", "")
    except urllib.error.HTTPError as error:
        result.network_ok = True
        result.status = error.code
        result.final_url = error.geturl() or url
        result.content_type = (error.headers or {}).get("Content-Type", "")
    except (urllib.error.URLError, socket.timeout, TimeoutError, OSError) as error:
        result.network_ok = False
        reason = getattr(error, "reason", None)
        if isinstance(error, (socket.timeout, TimeoutError)) or isinstance(reason, socket.timeout):
            result.error = f"请求超时（{int(timeout)} 秒）"
        else:
            result.error = str(reason or error)
    return result


# ---------------------------------------------------------------------------
# 下载
# ---------------------------------------------------------------------------


def download_to_file(
    url: str,
    target_path: str,
    *,
    headers: Optional[Dict[str, str]] = None,
    timeout: float = 600,
    progress: Optional[Callable[[int, int], None]] = None,
    should_abort: Optional[Callable[[], bool]] = None,
) -> Tuple[bool, str]:
    """流式下载；先写 .part，成功后原子改名，失败不留半截文件。

    返回 (ok, error)。错误文案与客户端下载器保持一致。
    """
    directory = os.path.dirname(os.path.abspath(target_path)) or "."
    if not os.path.isdir(directory):
        try:
            os.makedirs(directory, exist_ok=True)
        except OSError as error:
            return False, f"无法创建目录：{directory}（{error}）"

    temp_path = target_path + ".part"
    merged = _request_headers(headers, accept="*/*")
    request_obj = urllib.request.Request(url, method="GET")
    for key, value in merged.items():
        request_obj.add_header(key, value)

    received = 0
    total = -1
    try:
        with _OPENER.open(request_obj, timeout=timeout) as raw:
            status = getattr(raw, "status", 0) or 200
            if status < 200 or status >= 300:
                return False, f"下载失败：服务器返回 HTTP {status}"
            content_length = raw.headers.get("Content-Length")
            if content_length and content_length.isdigit():
                total = int(content_length)
            with open(temp_path, "wb") as handle:
                while True:
                    if should_abort and should_abort():
                        raise _Aborted()
                    try:
                        chunk = raw.read(64 * 1024)
                    except (socket.timeout, TimeoutError):
                        raise _TimedOut()
                    if not chunk:
                        break
                    handle.write(chunk)
                    received += len(chunk)
                    if progress:
                        progress(received, total)
            if total > 0 and received != total:
                raise _Incomplete(received, total)
        os.replace(temp_path, target_path)
        return True, ""
    except _Aborted:
        _remove_quietly(temp_path)
        return False, "下载失败：下载已取消"
    except _TimedOut:
        _remove_quietly(temp_path)
        return False, "下载失败：下载超时"
    except _Incomplete as error:
        _remove_quietly(temp_path)
        return False, f"下载失败：文件不完整（{error.received} / {error.total} 字节）。"
    except urllib.error.HTTPError as error:
        _remove_quietly(temp_path)
        if error.code == 403:
            return False, "下载失败：服务器拒绝访问（HTTP 403，链接可能已失效）"
        if error.code == 404:
            return False, "下载失败：文件不存在（HTTP 404）"
        return False, f"下载失败：服务器返回 HTTP {error.code}"
    except (urllib.error.URLError, socket.timeout, TimeoutError, OSError) as error:
        _remove_quietly(temp_path)
        reason = getattr(error, "reason", None)
        if isinstance(error, (socket.timeout, TimeoutError)) or isinstance(reason, socket.timeout):
            return False, "下载失败：下载超时"
        return False, f"下载失败：{reason or error}"


class _Aborted(Exception):
    pass


class _TimedOut(Exception):
    pass


class _Incomplete(Exception):
    def __init__(self, received: int, total: int) -> None:
        super().__init__(received, total)
        self.received = received
        self.total = total


def _remove_quietly(path: str) -> None:
    try:
        os.unlink(path)
    except OSError:
        pass


def download_bytes(url: str, *, headers: Optional[Dict[str, str]] = None,
                   timeout: float = 60) -> HttpResponse:
    return request("GET", url, headers=_request_headers(headers), timeout=timeout)
