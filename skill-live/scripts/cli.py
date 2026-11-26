"""命令行入口：录制 / 导入 / 一键成片 / 导出成片 / 下载草稿 / 自检。"""

from __future__ import annotations

import argparse
import json
import os
import shlex
import shutil
import sys
import traceback
from typing import Any, Dict, List, Optional

import asr
import capcut
import config as config_module
import draft_download
import http_client as http
import locator
import pipeline
import prompts as prompts_module
import recording
import selftest
import video_export

from config import AppConfig
from prompts import PromptEntry
from store import Store
from util import current_ms, format_duration
from version import __version__


def _emit(payload: Dict[str, Any]) -> None:
    print(json.dumps(payload, ensure_ascii=False, indent=2))


def _progress(args: argparse.Namespace):
    def report(message: str) -> None:
        if not getattr(args, "quiet", False):
            print(message, file=sys.stderr, flush=True)
    return report


def _load_config(args: argparse.Namespace) -> AppConfig:
    return config_module.load(
        getattr(args, "config", None),
        api_url_override=getattr(args, "api_url", "") or "",
        api_key_override=getattr(args, "api_key", "") or "",
        db_override=getattr(args, "db", "") or "",
    )


def _toggle(args: argparse.Namespace, name: str, fallback: bool) -> bool:
    value = getattr(args, name, None)
    return fallback if value is None else bool(value)


def _resolve_prompt(args: argparse.Namespace, cfg: AppConfig) -> PromptEntry:
    if getattr(args, "prompt_file", ""):
        spec = args.prompt_file
    elif getattr(args, "prompt", ""):
        spec = args.prompt
    else:
        spec = cfg.ai.default_prompt
    try:
        return prompts_module.resolve_prompt(spec)
    except ValueError as error:
        raise ValueError(str(error))


# ---------------------------------------------------------------------------
# 命令实现
# ---------------------------------------------------------------------------


def command_init(args: argparse.Namespace) -> int:
    path = (
        os.path.abspath(os.path.expanduser(args.config))
        if getattr(args, "config", None)
        else config_module.default_config_path()
    )
    if os.path.isfile(path) and not args.force:
        cfg = config_module.load(path)
        _emit({
            "ok": True,
            "created": False,
            "config_path": cfg.path,
            "message": "配置文件已存在；如需覆盖请加 --force。",
            "config": cfg.to_dict(mask_secrets=True),
        })
        return 0
    cfg = config_module.load(path)
    cfg.save()
    _emit({
        "ok": True,
        "created": True,
        "config_path": cfg.path,
        "message": "已写入默认配置；请填写 service.apiKey。",
        "config": cfg.to_dict(mask_secrets=True),
    })
    return 0


def command_doctor(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    ffmpeg = locator.ffmpeg_path(cfg.recording.ffmpeg_path)
    ffprobe = locator.ffprobe_path(cfg.recording.ffmpeg_path)
    result: Dict[str, Any] = {
        "ok": bool(ffmpeg and ffprobe),
        "version": __version__,
        "config_path": cfg.path,
        "config_exists": os.path.isfile(cfg.path),
        "db_path": cfg.storage.state_db,
        "api_url": cfg.service.api_url,
        "api_key_configured": bool(cfg.service.api_key),
        "ffmpeg": ffmpeg,
        "ffprobe": ffprobe,
        "paths": {
            "recording": cfg.storage.recording_dir,
            "export": cfg.storage.export_dir,
            "draft": cfg.storage.draft_dir,
            "cache": cfg.storage.cache_dir,
        },
        "finish": {
            "transitions": cfg.finish.transitions,
            "effects": cfg.finish.effects,
            "filters": cfg.finish.filters,
            "stickers": cfg.finish.stickers,
            "bgm": cfg.finish.bgm,
            "sfx": cfg.finish.sfx,
            "bilingualSubtitles": cfg.finish.bilingual_subtitles,
            "dedup": cfg.finish.dedup,
            "speedAuto": cfg.finish.speed_auto,
            "fixedSpeed": cfg.finish.fixed_speed,
        },
        "prompts": prompts_module.builtin_names(),
    }

    if args.ping:
        if not cfg.service.api_key:
            result["ping"] = {"ok": False, "message": "未配置 API 密钥"}
            result["ok"] = False
        else:
            url = cfg.service.api_url.rstrip("/") + "/models"
            response = http.get(
                url, headers={"Authorization": f"Bearer {cfg.service.api_key}"},
                timeout=20,
            )
            if response.is_http_ok:
                message = "连接成功"
                ping_ok = True
            elif response.status in (401, 403):
                message = "密钥无效"
                ping_ok = False
            elif response.network_ok:
                message = f"服务可达但返回 HTTP {response.status}"
                ping_ok = False
            else:
                message = response.error or "网络错误"
                ping_ok = False
            result["ping"] = {"ok": ping_ok, "message": message,
                              "status": response.status}
            if not ping_ok:
                result["ok"] = False

    _emit(result)
    return 0 if result["ok"] else 1


def command_record(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    if args.output_dir:
        cfg.storage.recording_dir = os.path.abspath(os.path.expanduser(args.output_dir))
    if args.segment_seconds is not None:
        cfg.recording.segment_seconds = max(1, min(30, args.segment_seconds))
    if args.no_reconnect:
        cfg.recording.reconnect = False
    if args.stop_timeout is not None:
        cfg.recording.stop_timeout_ms = max(1000, min(60000, args.stop_timeout))

    progress = _progress(args)
    store = Store(cfg.storage.state_db)
    try:
        for item in store.recover_interrupted_recordings():
            progress(
                f"恢复上次中断的素材 #{item['source_id']}：{item['warning']}"
            )
        result = recording.record_online(
            store, cfg, args.source,
            title=args.title,
            max_duration_seconds=args.max_duration,
            progress=progress,
        )
    finally:
        store.close()

    _emit({"ok": bool(result.get("ok")), "material": result})
    return 0 if result.get("ok") else 1


def command_import(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    store = Store(cfg.storage.state_db)
    try:
        result = recording.import_local(
            store, args.path, title=args.title, note=args.note,
            progress=_progress(args),
        )
    finally:
        store.close()
    _emit({"ok": True, "material": result})
    return 0


def _source_request(args: argparse.Namespace) -> pipeline.LiveClipRequest:
    if not args.input and args.source_id is None:
        raise ValueError("请传 --input 或 --source-id。")
    if args.input and args.source_id is not None:
        raise ValueError("--input 与 --source-id 只能选一个。")
    return pipeline.LiveClipRequest(
        source_id=args.source_id if args.source_id is not None else -1,
        input_path=args.input or "",
        title=args.title or "",
    )


def command_clip(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    request = _source_request(args)
    request.prompt = _resolve_prompt(args, cfg)
    request.target_clip_count = args.clips if args.clips is not None else -1
    if args.speed is not None:
        request.speed_auto = False
        request.fixed_speed = args.speed
    else:
        request.speed_auto = cfg.finish.speed_auto
        request.fixed_speed = cfg.finish.fixed_speed
    request.transitions = _toggle(args, "transitions", cfg.finish.transitions)
    request.effects = _toggle(args, "effects", cfg.finish.effects)
    request.filters = _toggle(args, "filters", cfg.finish.filters)
    request.stickers = _toggle(args, "stickers", cfg.finish.stickers)
    request.bgm = _toggle(args, "bgm", cfg.finish.bgm)
    request.sfx = _toggle(args, "sfx", cfg.finish.sfx)
    request.bilingual_subtitles = _toggle(
        args, "bilingual_subtitles", cfg.finish.bilingual_subtitles
    )
    request.dedup = _toggle(args, "dedup", cfg.finish.dedup)
    request.seed = args.seed
    request.keep_work = args.keep_work
    request.verbose = args.verbose

    store = Store(cfg.storage.state_db)
    try:
        store.recover_interrupted_recordings()
        drafts = pipeline.run_pipeline(
            store, cfg, request, progress=_progress(args)
        )
    finally:
        store.close()

    output_drafts = []
    for draft in drafts:
        output_drafts.append({
            "row_id": draft.get("row_id"),
            "draft_id": draft.get("draft_id"),
            "draft_url": draft.get("draft_url"),
            "title": draft.get("title"),
            "description": draft.get("description"),
            "tags": draft.get("tags"),
            "duration_ms": draft.get("duration_ms"),
            "duration": format_duration(int(draft.get("duration_ms", 0))),
            "source_start_ms": draft.get("source_start_ms"),
            "source_end_ms": draft.get("source_end_ms"),
            "canvas": draft.get("canvas"),
            "packaging": draft.get("packaging"),
            "caption_style": {
                "font": (draft.get("caption_style") or {}).get("font"),
                "font_size": (draft.get("caption_style") or {}).get("font_size"),
            },
            "sfx_count": draft.get("sfx_count"),
            "english_count": draft.get("english_count"),
            "notes": {
                "sfx": draft.get("sfx_note", ""),
                "bgm": draft.get("bgm_note", ""),
                "english": draft.get("english_note", ""),
            },
            "export_hint": (
                "python3 scripts/livemix.py export --draft-url "
                f"{shlex.quote(str(draft.get('draft_url')))} "
                f"--title {shlex.quote(str(draft.get('title')))}"
            ),
            "download_hint": (
                "python3 scripts/livemix.py download-draft --draft-url "
                f"{shlex.quote(str(draft.get('draft_url')))} "
                f"--title {shlex.quote(str(draft.get('title')))}"
            ),
        })

    payload = {
        "ok": True,
        "source_id": request.source_id,
        "prompt": request.prompt.name if request.prompt else "",
        "count": len(output_drafts),
        "drafts": output_drafts,
    }
    if args.json_out:
        path = os.path.abspath(os.path.expanduser(args.json_out))
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
        payload["json_out"] = path
    _emit(payload)
    return 0


def command_transcribe(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    if not cfg.service.api_key:
        raise ValueError(
            "尚未配置 API 密钥，请设置 LIVEMIX_API_KEY 或配置 service.apiKey。"
        )
    request = pipeline.LiveClipRequest(
        source_id=args.source_id if args.source_id is not None else -1,
        input_path=args.input or "",
        title=args.title or "",
    )
    progress = _progress(args)
    store = Store(cfg.storage.state_db)
    work_dir = ""
    try:
        store.recover_interrupted_recordings()
        source = pipeline._prepare_source(store, request, cfg)
        if source.get("status") != "ready":
            raise ValueError("源视频不可用，请等待录制完成或重新导入。")
        media_path = pipeline.resolve_draft_input_path(source, cfg, progress)
        fingerprint = pipeline.source_fingerprint(str(source.get("local_path", "")))
        cached = store.load_asr(int(source["id"]), fingerprint)
        if cached and cached.get("utterances"):
            transcript = asr.from_cache_json(cached)
            is_cached = True
            progress("已命中本地 ASR 缓存，跳过重复识别")
        else:
            work_dir = os.path.join(
                cfg.storage.cache_dir, "transcribe",
                f"source_{source['id']}_{current_ms()}",
            )
            os.makedirs(work_dir, exist_ok=True)
            audio_path = os.path.join(work_dir, "audio.mp3")
            progress("正在提取 MP3 音频…")
            import ffmpeg as ffmpeg_module
            ffmpeg_module.extract_mp3(
                media_path, audio_path,
                ffmpeg_configured=cfg.recording.ffmpeg_path,
            )
            progress("正在上传整片音频…")
            audio_url = capcut.upload_file(audio_path, cfg.service.api_key)
            progress("正在整片识别…")
            transcript = asr.transcribe(
                audio_url, cfg.service.api_url, cfg.service.api_key
            )
            if not transcript.get("utterances"):
                raise pipeline.PipelineError("ASR 未识别到语音内容。")
            store.save_asr(
                int(source["id"]), fingerprint, asr.to_cache_json(transcript)
            )
            is_cached = False
    finally:
        store.close()
        if work_dir:
            shutil.rmtree(work_dir, ignore_errors=True)

    payload = {
        "ok": True,
        "source_id": source["id"],
        "title": source.get("title", ""),
        "cached": is_cached,
        "duration_ms": asr.transcript_duration_ms(transcript),
        "utterance_count": len(transcript.get("utterances") or []),
        "char_count": asr.transcript_char_count(transcript),
        "speaker_count": asr.speaker_count(transcript),
        "average_confidence": asr.average_confidence(transcript),
        "text": transcript.get("text", ""),
        "utterances": transcript.get("utterances") or [],
    }
    if args.output:
        path = os.path.abspath(os.path.expanduser(args.output))
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
        payload["output"] = path
    _emit(payload)
    return 0


def _resolve_draft_target(store: Store,
                          args: argparse.Namespace) -> Dict[str, Any]:
    row = None
    if args.row_id is not None:
        row = store.draft(args.row_id)
        if not row:
            raise ValueError(f"草稿记录不存在：{args.row_id}")
    draft_url = (args.draft_url or (row or {}).get("draft_url") or "").strip()
    draft_id = (
        getattr(args, "draft_id", "")
        or (row or {}).get("draft_id")
        or capcut.draft_id_from_url(draft_url)
    )
    title = (args.title or (row or {}).get("title") or "直播切片").strip()
    if not draft_url and not draft_id:
        raise ValueError("请传 --draft-url、--draft-id 或 --row-id。")
    return {
        "row": row,
        "draft_url": draft_url,
        "draft_id": draft_id.strip(),
        "title": title or "直播切片",
    }


def command_download_draft(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    store = Store(cfg.storage.state_db)
    try:
        target = _resolve_draft_target(store, args)
        root = os.path.abspath(os.path.expanduser(
            args.draft_root or cfg.storage.draft_dir
        ))
        os.makedirs(root, exist_ok=True)
        local_path = draft_download.download_draft(
            target["draft_id"], target["title"], root,
            progress=_progress(args),
        )
        row = target["row"]
        if row is None:
            # 未传 --row-id 时按 draft_url / draft_id 反查记录，保持
            # 客户端的 downloaded_path 回写行为。
            for item in store.list_drafts():
                if (target["draft_url"]
                        and item.get("draft_url") == target["draft_url"]) or (
                        target["draft_id"]
                        and item.get("draft_id") == target["draft_id"]):
                    row = item
                    break
        if row:
            store.mark_draft_downloaded(int(row["id"]), local_path)
    finally:
        store.close()

    _emit({
        "ok": True,
        "draft_id": target["draft_id"],
        "title": target["title"],
        "draft_root": root,
        "local_path": local_path,
        "message": "草稿已安装到剪映草稿目录；打开剪映即可看到。",
    })
    return 0


def command_export(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    if not cfg.service.api_key:
        raise ValueError(
            "尚未配置 API 密钥，请设置 LIVEMIX_API_KEY 或配置 service.apiKey。"
        )
    store = Store(cfg.storage.state_db)
    target = None
    path = ""
    resume_error = None
    try:
        target = _resolve_draft_target(store, args)
        if not target["draft_url"]:
            raise ValueError(
                "导出成片需要 --draft-url 或 --row-id；只有 draft_id 不足以提交合成。"
            )
        export_dir = os.path.abspath(os.path.expanduser(
            args.output_dir or cfg.storage.export_dir
        ))
        try:
            path = video_export.export_video(
                target["draft_url"], cfg.service.api_key, export_dir,
                target["title"], resume_video_url=args.resume_url,
                progress=_progress(args),
            )
        except video_export.VideoExportError as error:
            if not error.video_url:
                raise
            resume_error = error
    finally:
        store.close()

    if resume_error is not None:
        # 云端已生成成片、只有下载失败：保留 URL，用户可 --resume-url 续传。
        _emit({
            "ok": False,
            "draft_id": (target or {}).get("draft_id", ""),
            "title": (target or {}).get("title", ""),
            "error": str(resume_error),
            "video_url": resume_error.video_url,
            "resume_hint": (
                "python3 scripts/livemix.py export --draft-url "
                f"{shlex.quote((target or {}).get('draft_url', ''))} "
                f"--title {shlex.quote((target or {}).get('title', ''))} "
                f"--resume-url {shlex.quote(resume_error.video_url)}"
            ),
        })
        return 1

    _emit({
        "ok": True,
        "draft_id": target["draft_id"],
        "title": target["title"],
        "output_path": path,
        "message": "成片已导出。",
    })
    return 0


def command_sources(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    store = Store(cfg.storage.state_db)
    try:
        sources = store.list_sources(args.status or "")
    finally:
        store.close()
    for item in sources:
        # probe_json 可能很大，CLI 输出只保留排障需要的字段。
        item.pop("probe_json", None)
    _emit({"ok": True, "count": len(sources), "sources": sources})
    return 0


def command_drafts(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    store = Store(cfg.storage.state_db)
    try:
        drafts = store.list_drafts(args.source_id)
    finally:
        store.close()
    _emit({"ok": True, "count": len(drafts), "drafts": drafts})
    return 0


def command_prompts(args: argparse.Namespace) -> int:
    if args.action == "show":
        if not args.name:
            raise ValueError("请指定提示词名称：prompts show <名称>。")
        try:
            entry = prompts_module.resolve_prompt(args.name)
        except ValueError as error:
            raise ValueError(str(error))
        _emit({
            "ok": True,
            "name": entry.name,
            "note": entry.note,
            "edited_at": entry.edited_at,
            "content": entry.content,
        })
        return 0
    _emit({
        "ok": True,
        "prompts": [
            {
                "name": entry.name,
                "note": entry.note,
                "summary": entry.summary,
                "edited_at": entry.edited_at,
            }
            for entry in prompts_module.BUILTIN_PROMPTS
        ],
    })
    return 0


def command_recover(args: argparse.Namespace) -> int:
    cfg = _load_config(args)
    store = Store(cfg.storage.state_db)
    try:
        recovered = store.recover_interrupted_recordings()
    finally:
        store.close()
    _emit({"ok": True, "count": len(recovered), "recovered": recovered})
    return 0


def command_selftest(args: argparse.Namespace) -> int:
    result = selftest.run()
    _emit(result)
    return 0 if result["ok"] else 1


# ---------------------------------------------------------------------------
# 参数解析
# ---------------------------------------------------------------------------


def _common_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument(
        "--config", default=None,
        help="配置文件路径（默认 ~/.livemix/config.json）",
    )
    parser.add_argument("--api-url", default="", help="覆盖 LLM/ASR 网关地址")
    parser.add_argument("--api-key", default="", help="覆盖 API 密钥")
    parser.add_argument("--db", default="", help="覆盖 SQLite 路径")
    parser.add_argument("--quiet", action="store_true", help="不打印进度")
    parser.add_argument("--verbose", action="store_true", help="异常时打印堆栈")
    return parser


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="livemix.py",
        description="开源直播切片助手：直播录制 / 本地素材 → AI 选片 → 剪映草稿 / 成片",
    )
    parser.add_argument("--version", action="version",
                        version=f"livemix-live-clipper {__version__}")
    sub = parser.add_subparsers(dest="command", required=True)
    common = _common_parser()

    init = sub.add_parser("init", parents=[common],
                          help="写入默认配置文件")
    init.add_argument("--force", action="store_true", help="覆盖已有配置")
    init.set_defaults(func=command_init)

    doctor = sub.add_parser("doctor", parents=[common],
                            help="检查 ffmpeg/ffprobe、配置与服务连接")
    doctor.add_argument("--ping", action="store_true", help="请求 /models 验证密钥")
    doctor.set_defaults(func=command_doctor)

    record = sub.add_parser("record", parents=[common],
                            help="录制直播流 / 下载直链 / 解析抖音直播间")
    record.add_argument("source", help="视频地址、抖音直播间链接或房间号")
    record.add_argument("--title", default="", help="素材标题")
    record.add_argument("--output-dir", default="", help="录制目录（覆盖配置）")
    record.add_argument("--max-duration", type=float, default=0.0,
                        help="最长录制秒数；0 表示一直录到手动停止/Ctrl+C")
    record.add_argument("--segment-seconds", type=int, default=None,
                        help="HLS 分片秒数（1–30，默认 4）")
    record.add_argument("--no-reconnect", action="store_true",
                        help="关闭 http 流断线重连")
    record.add_argument("--stop-timeout", type=int, default=None,
                        help="停止录制时等待 ffmpeg 收尾的毫秒数")
    record.set_defaults(func=command_record)

    import_cmd = sub.add_parser("import", parents=[common],
                                help="导入本地视频（只登记，不复制）")
    import_cmd.add_argument("path", help="本地视频路径")
    import_cmd.add_argument("--title", default="", help="素材标题")
    import_cmd.add_argument("--note", default="", help="备注")
    import_cmd.set_defaults(func=command_import)

    clip = sub.add_parser("clip", parents=[common],
                          help="一键成片：ASR → AI 选片 → 剪映草稿")
    clip.add_argument("--input", default="", help="源视频路径（mp4 或 m3u8）")
    clip.add_argument("--source-id", type=int, default=None,
                      help="已登记素材的 id（与 --input 二选一）")
    clip.add_argument("--title", default="", help="素材标题（仅首次登记时使用）")
    clip.add_argument("--prompt", default="",
                      help="内置提示词名称 / 文件路径 / 提示词文本")
    clip.add_argument("--prompt-file", default="", help="提示词文件路径")
    clip.add_argument("--clips", type=int, default=None,
                      help="目标成片数量；省略为自动（每条最多 30 分钟素材）")
    clip.add_argument("--speed", type=float, default=None,
                      help="固定倍速 0.5–2.0；省略时按配置自动 1.05–1.50×")
    clip.add_argument("--seed", type=int, default=None,
                      help="随机种子（调试用；默认使用系统随机源）")
    clip.add_argument("--keep-work", action="store_true",
                      help="保留切片与音频临时文件")
    clip.add_argument("--json-out", default="", help="把结果写入指定 JSON 文件")
    for name, help_text in (
        ("transitions", "自动转场"),
        ("effects", "自动特效"),
        ("filters", "自动滤镜"),
        ("stickers", "自动贴纸"),
        ("bgm", "自动背景音乐"),
        ("sfx", "自动音效"),
        ("bilingual-subtitles", "中英文双字幕"),
        ("dedup", "平台去重"),
    ):
        clip.add_argument(
            f"--{name}", dest=name.replace("-", "_"),
            action=argparse.BooleanOptionalAction, default=None,
            help=help_text,
        )
    clip.set_defaults(func=command_clip)

    transcribe = sub.add_parser("transcribe", parents=[common],
                                help="整片 ASR 转写（带本地缓存）")
    transcribe.add_argument("--input", default="", help="源视频路径")
    transcribe.add_argument("--source-id", type=int, default=None)
    transcribe.add_argument("--title", default="")
    transcribe.add_argument("--output", default="", help="转写 JSON 输出路径")
    transcribe.set_defaults(func=command_transcribe)

    download = sub.add_parser("download-draft", parents=[common],
                              help="下载并安装剪映草稿到本地草稿箱")
    download.add_argument("--draft-url", default="")
    download.add_argument("--draft-id", default="")
    download.add_argument("--row-id", type=int, default=None)
    download.add_argument("--title", default="")
    download.add_argument("--draft-root", default="", help="剪映草稿目录（覆盖配置）")
    download.set_defaults(func=command_download_draft)

    export = sub.add_parser("export", parents=[common],
                            help="云端导出 MP4 成片")
    export.add_argument("--draft-url", default="")
    export.add_argument("--row-id", type=int, default=None)
    export.add_argument("--title", default="")
    export.add_argument("--output-dir", default="", help="成片导出目录（覆盖配置）")
    export.add_argument("--resume-url", default="",
                        help="续传：直接使用已完成合成的成片 URL")
    export.set_defaults(func=command_export)

    sources_cmd = sub.add_parser("sources", parents=[common],
                                 help="列出素材库（录制 / 下载 / 导入）")
    sources_cmd.add_argument("--status", default="",
                             choices=["", "recording", "ready", "failed"])
    sources_cmd.set_defaults(func=command_sources)

    drafts_cmd = sub.add_parser("drafts", parents=[common],
                                help="列出已生成的草稿记录")
    drafts_cmd.add_argument("--source-id", type=int, default=None)
    drafts_cmd.set_defaults(func=command_drafts)

    recover_cmd = sub.add_parser(
        "recover", parents=[common],
        help="恢复上次异常中断的录制（有播放列表则保留为可用）",
    )
    recover_cmd.set_defaults(func=command_recover)

    prompts_cmd = sub.add_parser("prompts", parents=[common],
                                 help="列出或查看内置提示词")
    prompts_cmd.add_argument("action", choices=["list", "show"], nargs="?",
                             default="list")
    prompts_cmd.add_argument("name", nargs="?", default="")
    prompts_cmd.set_defaults(func=command_prompts)

    selftest_cmd = sub.add_parser("selftest", parents=[common],
                                  help="运行纯逻辑自检")
    selftest_cmd.set_defaults(func=command_selftest)

    return parser


def main(argv: Optional[List[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except KeyboardInterrupt:
        print("已取消。", file=sys.stderr)
        _emit({"ok": False, "error": "已取消"})
        return 130
    except SystemExit:
        raise
    except Exception as error:  # noqa: BLE001 - CLI 统一输出错误
        if getattr(args, "verbose", False):
            traceback.print_exc()
        message = str(error) or error.__class__.__name__
        print(f"错误：{message}", file=sys.stderr)
        _emit({"ok": False, "error": message,
               "error_type": error.__class__.__name__})
        return 1
