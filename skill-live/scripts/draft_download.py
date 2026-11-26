"""剪映草稿下载与本地化安装。

逐流程移植 LiveMix/src/api/CapCutMateClient.cpp 的 downloadDraft：
1. get_draft 取草稿文件列表；
2. 先读取全部 JSON 配置到内存，非 JSON 文件直接下载；
3. 解析配置 JSON，收集 materials.videos/audios/images 中的远程素材；
4. 下载素材到 staging/assets/<videos|audios|images>/<name>_<hash6>.<suffix>；
5. 把 JSON 中的远程 URL 改写为剪映占位符相对路径，并设置
   config.material_save_mode = 1；
6. draft_meta_info.json 的路径改写为最终草稿目录；
7. 写入「这是剪映草稿」提示文件；
8. 全部就绪后原子改名到正式目录，剪映目录监听可直接收录。
"""

from __future__ import annotations

import json
import os
import shutil
import urllib.parse
from typing import Any, Callable, Dict, List, Optional

import capcut
import http_client as http
from util import (
    hash6,
    is_http_url,
    sanitize_file_component,
    write_json_atomic,
)

DRAFT_PATH_PLACEHOLDER = (
    "##_draftpath_placeholder_0E685133-18CE-45ED-8CB8-2904A212EC80_##"
)

README_FILE_NAME = "【剪映草稿】请用剪映打开查看成片.txt"

_MATERIAL_LISTS = (
    {"key": "videos", "sub_dir": "videos", "suffix": "mp4", "kind": "video"},
    {"key": "audios", "sub_dir": "audios", "suffix": "mp3", "kind": "audio"},
    {"key": "images", "sub_dir": "images", "suffix": "jpg", "kind": "image"},
)

ProgressCallback = Optional[Callable[[str], None]]


class DraftDownloadError(RuntimeError):
    pass


def draft_readme_data() -> bytes:
    text = (
        "这是一个剪映草稿，不是可直接播放的视频文件。\n"
        "\n"
        "请打开剪映，在「本地草稿」中找到并打开这个草稿，即可查看和导出成片。\n"
    )
    # 带 UTF-8 BOM，Windows 记事本等编辑器才能正确识别中文。
    return b"\xef\xbb\xbf" + text.encode("utf-8")


def write_draft_readme_file(draft_dir: str) -> bool:
    if not os.path.isdir(draft_dir):
        return False
    try:
        temp_path = os.path.join(draft_dir, ".readme.tmp")
        with open(temp_path, "wb") as handle:
            handle.write(draft_readme_data())
        os.replace(temp_path, os.path.join(draft_dir, README_FILE_NAME))
        return True
    except OSError:
        return False


def relative_draft_file_path(file_url: str, draft_id: str) -> str:
    parsed = urllib.parse.urlsplit(file_url)
    path = urllib.parse.unquote(parsed.path)
    index = path.find(draft_id)
    if index >= 0:
        relative = path[index + len(draft_id):]
    else:
        relative = "/" + os.path.basename(path)
    relative = relative.lstrip("/")
    if not relative or ".." in relative:
        return ""
    return relative


def safe_material_file_name(url: str, fallback_base: str,
                            fallback_suffix: str) -> str:
    parsed = urllib.parse.urlsplit(url)
    file_name = os.path.basename(urllib.parse.unquote(parsed.path))
    base, suffix = os.path.splitext(file_name)
    if not base:
        base = fallback_base
    base = sanitize_file_component(base, 36)
    if not base:
        base = "material"
    digest = hash6(url)
    suffix = suffix.lstrip(".").lower() or fallback_suffix
    return f"{base}_{digest}.{suffix.lower()}"


def kind_for_path(path: str) -> str:
    suffix = os.path.splitext(path)[1].lstrip(".").lower()
    if suffix in ("mp4", "mov", "mkv", "webm", "m4v"):
        return "video"
    if suffix in ("mp3", "wav", "m4a", "aac", "flac"):
        return "audio"
    if suffix in ("png", "jpg", "jpeg", "webp"):
        return "image"
    return "config"


def _rewrite_material_paths(root: Dict[str, Any],
                            local_paths: Dict[str, str]) -> None:
    materials = root.get("materials")
    if not isinstance(materials, dict) or not materials:
        return
    for material_list in _MATERIAL_LISTS:
        key = material_list["key"]
        items = materials.get(key)
        if not isinstance(items, list):
            continue
        for index, value in enumerate(items):
            if not isinstance(value, dict):
                continue
            source_path = str(value.get("path", ""))
            if not is_http_url(source_path):
                continue
            relative = local_paths.get(source_path, "")
            if not relative:
                continue
            value["path"] = DRAFT_PATH_PLACEHOLDER + "/" + relative
            items[index] = value
        materials[key] = items
    root["materials"] = materials

    config = root.get("config")
    if not isinstance(config, dict):
        config = {}
    config["material_save_mode"] = 1
    root["config"] = config


def _localize_draft_meta(meta: Dict[str, Any], draft_dir: str,
                         folder_name: str) -> None:
    meta["draft_name"] = folder_name
    meta["draft_fold_path"] = draft_dir.replace("\\", "/")
    meta["draft_root_path"] = os.path.dirname(draft_dir).replace("\\", "/")

    groups = meta.get("draft_materials")
    if not isinstance(groups, list):
        return
    for group in groups:
        if not isinstance(group, dict):
            continue
        values = group.get("value")
        if not isinstance(values, list):
            continue
        for index, entry in enumerate(values):
            if not isinstance(entry, dict):
                continue
            file_path = str(entry.get("file_Path", ""))
            if not file_path or is_http_url(file_path):
                continue
            if file_path.startswith(DRAFT_PATH_PLACEHOLDER):
                file_path = file_path[len(DRAFT_PATH_PLACEHOLDER):]
            elif file_path.startswith("/") or (
                len(file_path) > 1 and file_path[1] == ":"
            ):
                # 无法可靠映射的绝对路径保持原样，避免写出错误的相对路径。
                continue
            file_path = file_path.lstrip("/")
            if file_path:
                entry["file_Path"] = "./" + file_path
                values[index] = entry
        group["value"] = values


def _write_json_file(path: str, payload: Dict[str, Any]) -> None:
    write_json_atomic(path, payload, indent=4, sort_keys=True)


def download_draft(
    draft_id: str,
    draft_title: str,
    target_root: str,
    *,
    progress: ProgressCallback = None,
) -> str:
    """下载并本地化草稿，返回正式草稿目录。"""
    if not draft_id.strip():
        raise DraftDownloadError("草稿记录缺少 draft_id。")

    def notify(message: str) -> None:
        if progress:
            progress(message)

    file_urls = capcut.fetch_draft_files(draft_id)

    title = sanitize_file_component((draft_title or "").strip(), 40) or "直播切片"
    folder_name = f"{title}_{draft_id}"
    final_dir = os.path.join(target_root, folder_name)
    staging_root = os.path.join(target_root, ".livemix-staging")
    staging_dir = os.path.join(staging_root, folder_name)
    os.makedirs(staging_dir, exist_ok=True)

    listed = []
    seen_paths = set()
    for file_url in file_urls:
        relative = relative_draft_file_path(file_url, draft_id)
        if not relative:
            raise DraftDownloadError(f"无法识别草稿文件路径：{file_url}")
        if relative in seen_paths:
            continue
        seen_paths.add(relative)
        is_json = relative.lower().endswith(".json")
        listed.append({
            "url": file_url,
            "relative": relative,
            "kind": "config" if is_json else kind_for_path(relative),
            "is_json": is_json,
        })

    notify(f"草稿配置：{len(listed)} 个文件")
    json_files: List[Dict[str, Any]] = []
    for file in listed:
        if file["is_json"]:
            response = http.get(file["url"], timeout=60)
            if not response.is_http_ok or not response.body:
                raise DraftDownloadError(
                    f"下载草稿文件失败：{file['relative']}"
                )
            json_files.append({
                "relative": file["relative"],
                "data": response.body,
            })
            continue

        local_file = os.path.join(staging_dir, file["relative"])
        if os.path.isfile(local_file) and os.path.getsize(local_file) > 0:
            continue
        ok, error = http.download_to_file(
            file["url"], local_file, timeout=600
        )
        if not ok:
            raise DraftDownloadError(
                f"下载草稿文件失败：{file['relative']}（{error}）"
            )

    parsed: List[Dict[str, Any]] = []
    for json_file in json_files:
        try:
            payload = json.loads(json_file["data"].decode("utf-8"))
        except (ValueError, UnicodeDecodeError):
            payload = None
        if not isinstance(payload, dict) or not payload:
            raise DraftDownloadError(
                f"草稿 JSON 无法解析：{json_file['relative']}"
            )
        parsed.append({"relative": json_file["relative"], "object": payload})

    assets: List[Dict[str, str]] = []
    seen_assets = set()
    local_paths: Dict[str, str] = {}

    def add_asset(url: str, relative: str, kind: str) -> None:
        if not relative:
            return
        local_paths[url] = relative
        if relative in seen_assets:
            return
        seen_assets.add(relative)
        assets.append({"url": url, "relative": relative, "kind": kind})

    for json_file in parsed:
        materials = json_file["object"].get("materials")
        if not isinstance(materials, dict):
            continue
        for material_list in _MATERIAL_LISTS:
            items = materials.get(material_list["key"])
            if not isinstance(items, list):
                continue
            for value in items:
                if not isinstance(value, dict):
                    continue
                source_path = str(value.get("path", ""))
                if not is_http_url(source_path) or source_path in local_paths:
                    continue
                name = safe_material_file_name(
                    source_path,
                    str(value.get("material_name", "")),
                    material_list["suffix"],
                )
                add_asset(
                    source_path,
                    f"assets/{material_list['sub_dir']}/{name}",
                    material_list["kind"],
                )

    notify(f"下载素材：{len(assets)} 个文件")
    for index, asset in enumerate(assets, start=1):
        local_file = os.path.join(staging_dir, asset["relative"])
        if os.path.isfile(local_file) and os.path.getsize(local_file) > 0:
            continue
        notify(f"下载素材 {index}/{len(assets)}：{asset['relative']}")
        ok, error = http.download_to_file(
            asset["url"], local_file, timeout=600
        )
        if not ok:
            raise DraftDownloadError(
                f"下载素材失败：{asset['relative']}（{error}）"
            )

    notify("写入草稿目录")
    for json_file in parsed:
        payload = json_file["object"]
        if "materials" in payload:
            _rewrite_material_paths(payload, local_paths)
        elif os.path.basename(json_file["relative"]).lower() == "draft_meta_info.json":
            _localize_draft_meta(payload, final_dir, folder_name)
        target_path = os.path.join(staging_dir, json_file["relative"])
        os.makedirs(os.path.dirname(target_path), exist_ok=True)
        _write_json_file(target_path, payload)

    if not write_draft_readme_file(staging_dir):
        notify("提示文件写入失败（不影响草稿）")

    if os.path.exists(final_dir):
        shutil.rmtree(final_dir)
    os.rename(staging_dir, final_dir)
    try:
        os.rmdir(staging_root)
    except OSError:
        pass

    notify(f"草稿已安装：{final_dir}")
    return final_dir
