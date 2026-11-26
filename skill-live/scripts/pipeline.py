"""一键成片流水线与剪映草稿生成。

逐参数移植 LiveMix/src/services/LiveClipService.cpp：
- 成片数量规则：每条至少 5 分钟素材、最多 30 分钟素材；
- 转写按目标条数均分为等长时间窗，切点优先落在句号上；
- LLM 返回句段索引，相邻区间间隔 ≤500ms 合并；
- ffmpeg 按原速裁剪并重新编码，倍速通过 add_videos 的 scene_timelines 应用；
- 字幕按标点断句、词级时间线对齐、每条 ≤15 字；
- 自动包装（转场/特效/滤镜/贴纸/BGM/音效/双语字幕/去重）与客户端同一份
  随机池、同一组参数。
"""

from __future__ import annotations

import json
import os
import shutil
import urllib.parse
from dataclasses import dataclass
from typing import Any, Dict, List, Optional, Sequence, Tuple

import assets
import asr
import capcut
import caption
import ffmpeg
import llm
import probe
from config import AppConfig
from prompts import PromptEntry
from store import Store
from util import (
    RandomSource,
    current_ms,
    format_duration,
    hls_playlist_duration_ms,
    qt_round,
    sanitize_file_component,
    utf16_len,
)

MIN_CLIP_MATERIAL_MS = 5 * 60 * 1000
MAX_CLIP_MATERIAL_MS = 30 * 60 * 1000
CHUNK_SPLIT_SEARCH_MS = 5 * 60 * 1000
RANGE_MERGE_GAP_MS = 500
CAPTION_MAX_CHARS = 15

LANDSCAPE = (1920, 1080)
PORTRAIT = (1080, 1920)

TRANSITION_DURATION_US = 800_000
SFX_VOLUME = 0.1
BGM_VOLUME = 0.1
DEDUP_IMAGE_COUNT = 5
DEDUP_IMAGE_DURATION_US = 20_000_000
DEDUP_IMAGE_ALPHA = 0.03
DEDUP_IMAGE_SCALE = 1.0
DEDUP_TRANSFORM_X_MIN = -1080
DEDUP_TRANSFORM_X_MAX = 1080
DEDUP_TRANSFORM_Y_MIN = -1920
DEDUP_TRANSFORM_Y_MAX = 1920
DEDUP_TAG = "去重"
SFX_MIN_DURATION_US = 3_000_000
SFX_MAX_DURATION_US = 10_000_000

# 每条成片随机选一套字幕样式（字体、字号、颜色、描边、入场动画）。
CAPTION_STYLE_PRESETS: List[Dict[str, Any]] = [
    {"font": "新青年体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#000000", "in_animation": ""},
    {"font": "飞扬行书", "font_size": 8, "text_color": "#ffde00",
     "border_color": "#000000", "in_animation": ""},
    {"font": "荔枝体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#000000", "in_animation": "打字机 II"},
    {"font": "金陵体", "font_size": 8, "text_color": "#000000",
     "border_color": "#ffffff", "in_animation": "向右缓入"},
    {"font": "后现代体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#fe8a80", "in_animation": ""},
    {"font": "墩墩体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#ff1837", "in_animation": "打字光标"},
    {"font": "文研体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#ff1837", "in_animation": "向右擦除"},
    {"font": "古印宋简", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#000000", "in_animation": "日出"},
    {"font": "山雁体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#000000", "in_animation": "波浪弹入"},
    {"font": "俊雅体", "font_size": 8, "text_color": "#ffffff",
     "border_color": "#000000", "in_animation": ""},
    {"font": "风雅宋", "font_size": 8, "text_color": "#ab4a37",
     "border_color": "#ffffff", "in_animation": ""},
    {"font": "挥墨体", "font_size": 8, "text_color": "#ab4a37",
     "border_color": "#ffffff", "in_animation": "渐显"},
]
DEFAULT_CAPTION_STYLE = {
    "font": "金陵体", "font_size": 15, "text_color": "#ffffff",
    "border_color": "", "in_animation": "", "transform_x": 0.0,
    "transform_y": -800.0,
}

# 特效候选池：名称 + 时长约束（闪光类缩短）。
EFFECT_PRESETS: List[Tuple[str, int, int]] = [
    ("发光 II", 3_000_000, 5_000_000),
    ("ktv灯光", 3_000_000, 5_000_000),
    ("三格漫画", 3_000_000, 5_000_000),
    ("人鱼滤镜", 3_000_000, 5_000_000),
    ("仙女变身 II", 3_000_000, 5_000_000),
    ("夸夸弹幕", 3_000_000, 5_000_000),
    ("小花花", 3_000_000, 5_000_000),
    ("少女心", 3_000_000, 5_000_000),
    ("少女星闪", 3_000_000, 5_000_000),
    ("庆祝彩带", 3_000_000, 5_000_000),
    ("彩虹射线", 3_000_000, 5_000_000),
    ("抖动", 3_000_000, 5_000_000),
    ("撒星星", 3_000_000, 5_000_000),
    ("撒星星 II", 3_000_000, 5_000_000),
    ("撕纸涂鸦边框", 3_000_000, 5_000_000),
    ("星光", 3_000_000, 5_000_000),
    ("星光 II", 3_000_000, 5_000_000),
    ("星光绽放", 3_000_000, 5_000_000),
    ("星星闪烁", 3_000_000, 5_000_000),
    ("星雨", 3_000_000, 5_000_000),
    ("暗角", 3_000_000, 5_000_000),
    ("变清晰", 3_000_000, 5_000_000),
    ("镜像", 3_000_000, 5_000_000),
    ("录制边框 II", 3_000_000, 5_000_000),
    ("录制边框 III", 3_000_000, 5_000_000),
    ("录制边框 IIII", 3_000_000, 5_000_000),
    ("简约边框", 3_000_000, 5_000_000),
    ("手绘拍摄器", 3_000_000, 5_000_000),
    ("白色线框", 3_000_000, 5_000_000),
    ("秋日暖黄", 3_000_000, 5_000_000),
    ("渐渐放大", 3_000_000, 5_000_000),
    ("咔嚓", 100_000, 1_000_000),
    ("温柔细闪", 3_000_000, 5_000_000),
    ("细闪 II", 3_000_000, 5_000_000),
    ("星夜", 3_000_000, 5_000_000),
]

STICKER_PRESETS: List[Tuple[str, str, int, int, float]] = [
    ("WOW英文发光手写字", "7446241135808400664", -597, 1437, 0.6),
    ("Wow 哇哦 爆炸框", "7451806976591351066", 450, -1290, 1.5),
    ("东北话嘎嘎香好好吃点赞", "7320536109421743396", -660, 1500, 1.0),
    ("促销-推荐", "7511614047159209278", -660, 1500, 1.0),
    ("互动引导推荐", "7457470334497770776", -660, 1500, 1.0),
    ("美妆情绪表达太美啦", "7419883362036321555", -660, 1500, 1.0),
    ("超Nice", "7576165100781079870", -660, 1500, 1.0),
    ("爆款推荐", "7427495509792738610", -660, 1500, 1.0),
    ("强烈推荐", "7419269230199065895", -660, 1500, 1.0),
    ("膨胀风互动引导橙色推荐", "7332009879277817100", -660, 1500, 1.0),
    ("星星跳跃", "7640102113392987416", -660, 1500, 1.0),
    ("促销-千万别错过", "7511630681584274712", -660, 1500, 1.0),
    ("促销-好物分享", "7511668505528241432", -660, 1500, 1.0),
    ("燃爆全场爆炸贴文字", "7429213485097143564", -660, 1500, 1.0),
    ("满分", "7511728644570598718", -660, 1500, 1.0),
    ("wow", "7021054635322395934", -660, 1500, 0.6),
]

FILTER_TITLES = [
    "画质高清", "清透感", "电影镜头", "电影画质", "电影柔光",
    "电影感增强", "电影光感", "甜妹感", "漫空", "港风夜景",
]

TRANSITION_TITLES = [
    "叠化", "叠加", "闪黑", "闪白", "相片切换", "翻页", "左移",
    "立体旋转", "右移", "卡片弹出", "上下翻页", "上移", "下移",
    "旋转圆盘", "立体翻转", "推近", "拉远", "向左", "向右", "抖动",
    "吸入", "放射", "震动", "向下", "向上", "顺时针旋转",
    "逆时针旋转", "向右下", "向左下", "向下拖拽", "向下抖动",
    "旋转震动", "扭曲弹动", "眨眼", "流光",
]

SFX_CATALOG: List[Tuple[str, str]] = [
    ("2123770113113985024", "读评论提示音"),
    ("2123770111855693824", "诡异的滴水声"),
    ("2123770110643539968", "讲卖点"),
    ("2123770109427191808", "综艺卡通biu"),
    ("2123770108227620864", "服装分类别场景金币声"),
    ("2123770106927386624", "抓特写场景弹簧声"),
    ("2123770105622958080", "悬疑咚声"),
    ("2123770104381444096", "开篇提示水滴声"),
    ("2123770103022489600", "字幕音效-嘭"),
    ("2123770101801947136", "呼转场音效"),
    ("2123770100271026176", "划重点拳击声"),
    ("2123770098878517248", "写字幕场景打字声"),
    ("2123770097435676672", "一滴水滴声"),
]

SFX_USAGE_HINTS = {
    "一滴水滴声": "口语闲聊轻氛围音：日常叙述、过渡、普通讲解",
    "字幕音效-嘭": "重点强调重音音效：卖点、数字、结论、强强调",
    "悬疑咚声": "场景描述氛围感音效：画面感、环境、情绪氛围描写",
    "诡异的滴水声": "引流引导互动音效：引导关注、点赞、下单、互动号召",
    "读评论提示音": "用于视频中朗读用户评论时的入场或切换提示",
    "服装分类别场景金币声": "用于服装类视频的分类切换、商品展示或奖励提示",
    "呼转场音效": "用于悬疑或惊悚类视频的转场，营造不稳定的过渡效果",
    "划重点拳击声": "用于强调关键信息、划重点，增强冲击力和注意力吸引",
    "讲卖点": "用于引出或强调产品卖点，区别于普通重音，带有引导性",
    "开篇提示水滴声": "用于视频开头吸引注意、引入主题，或章节起始提示",
    "写字幕场景打字声": "配合字幕逐字出现，模拟打字机效果，增强文字节奏感",
    "抓特写场景弹簧声": "用于镜头拉近、特写切换或细节突出，增加弹性动感",
    "综艺卡通biu": "综艺感强调、趣味转场或亮点提示",
}


class PipelineError(RuntimeError):
    pass


@dataclass
class LiveClipRequest:
    source_id: int = -1
    input_path: str = ""
    title: str = ""
    prompt: Optional[PromptEntry] = None
    target_clip_count: int = -1
    speed_auto: bool = True
    fixed_speed: float = 1.0
    transitions: bool = True
    effects: bool = True
    filters: bool = True
    stickers: bool = True
    bgm: bool = True
    sfx: bool = True
    bilingual_subtitles: bool = False
    dedup: bool = True
    seed: Optional[int] = None
    keep_work: bool = False
    verbose: bool = False


ProgressCallback = Optional[Any]


# ---------------------------------------------------------------------------
# 数量规则
# ---------------------------------------------------------------------------


def min_clip_count_for_duration(duration_ms: int) -> int:
    if duration_ms <= 0:
        return 1
    return max(1, (duration_ms + MAX_CLIP_MATERIAL_MS - 1) // MAX_CLIP_MATERIAL_MS)


def max_clip_count_for_duration(duration_ms: int) -> int:
    if duration_ms <= 0:
        return 100
    return max(1, duration_ms // MIN_CLIP_MATERIAL_MS)


def nearest_canvas_size(source_width: int, source_height: int) -> Tuple[int, int]:
    if source_width <= 0 or source_height <= 0:
        return LANDSCAPE
    landscape_ratio = LANDSCAPE[0] / LANDSCAPE[1]
    portrait_ratio = PORTRAIT[0] / PORTRAIT[1]
    source_ratio = source_width / source_height
    if abs(source_ratio - portrait_ratio) < abs(source_ratio - landscape_ratio):
        return PORTRAIT
    return LANDSCAPE


# ---------------------------------------------------------------------------
# 分段 / 合并 / 元数据
# ---------------------------------------------------------------------------


def _ends_with_sentence_end(text: str) -> bool:
    trimmed = (text or "").strip()
    trailing_marks = "”’」』）)》】\"'"
    while trimmed and trimmed[-1] in trailing_marks:
        trimmed = trimmed[:-1]
    if not trimmed:
        return False
    return trimmed[-1] in "。！？.!?"


def split_index_near(utterances: Sequence[Dict[str, Any]], target_ms: int,
                     search_from: int, search_radius_ms: int) -> int:
    period_split = 0
    period_distance = search_radius_ms + 1
    nearest_split = max(1, search_from)
    nearest_distance = -1

    start = max(1, search_from)
    for index in range(start, len(utterances)):
        boundary_distance = abs(int(utterances[index].get("start_ms", 0)) - target_ms)
        if nearest_distance < 0 or boundary_distance < nearest_distance:
            nearest_distance = boundary_distance
            nearest_split = index

        if not _ends_with_sentence_end(str(utterances[index - 1].get("text", ""))):
            continue
        distance = abs(int(utterances[index - 1].get("end_ms", 0)) - target_ms)
        if distance <= search_radius_ms and (
            period_split == 0 or distance < period_distance
        ):
            period_distance = distance
            period_split = index

    return period_split if period_split > 0 else nearest_split


def build_chunks(transcript: Dict[str, Any],
                 chunk_count: int) -> List[Dict[str, Any]]:
    utterances = transcript.get("utterances") or []
    chunks: List[Dict[str, Any]] = []
    if not utterances:
        return chunks

    asr_duration_ms = asr.transcript_duration_ms(transcript)
    count = max(1, min(chunk_count, len(utterances)))

    def append_chunk(segments: List[Dict[str, Any]]) -> None:
        if not segments:
            return
        chunks.append({
            "start_ms": int(segments[0].get("start_ms", 0)),
            "end_ms": int(segments[-1].get("end_ms", 0)),
            "segments": segments,
        })

    if count <= 1:
        append_chunk(list(utterances))
        return chunks

    window_ms = asr_duration_ms // max(1, count)
    search_radius_ms = min(CHUNK_SPLIT_SEARCH_MS, max(1000, window_ms // 10))

    previous_split = 0
    for index in range(1, count):
        target_ms = asr_duration_ms * index // count
        max_split = len(utterances) - (count - index)
        split = split_index_near(
            utterances, target_ms, previous_split + 1, search_radius_ms
        )
        split = max(previous_split + 1, min(split, max_split))
        append_chunk(list(utterances[previous_split:split]))
        previous_split = split
    append_chunk(list(utterances[previous_split:]))
    return chunks


def merge_ranges(ranges: Sequence[Dict[str, int]]) -> List[Dict[str, int]]:
    merged: List[Dict[str, int]] = []
    if not ranges:
        return merged
    current = dict(ranges[0])
    for index in range(1, len(ranges)):
        nxt = ranges[index]
        gap = int(nxt["start_ms"]) - int(current["end_ms"])
        if int(nxt["start_ms"]) >= int(current["start_ms"]) and gap <= RANGE_MERGE_GAP_MS:
            current["end_ms"] = max(int(current["end_ms"]), int(nxt["end_ms"]))
            continue
        merged.append(current)
        current = dict(nxt)
    merged.append(current)
    return merged


def _truncate_utf16(text: str, max_units: int) -> str:
    if utf16_len(text) <= max_units:
        return text
    result = ""
    units = 0
    for ch in text:
        ch_units = utf16_len(ch)
        if units + ch_units > max_units:
            break
        result += ch
        units += ch_units
    return result


def sanitize_title(raw: str) -> str:
    title = (raw or "").strip()
    title = _truncate_utf16(title, 12)
    return title if utf16_len(title) >= 2 else ""


def sanitize_description(raw: str) -> str:
    return _truncate_utf16((raw or "").strip(), 128)


def sanitize_topics(raw: Sequence[str]) -> List[str]:
    topics: List[str] = []
    seen = set()
    for item in raw:
        topic = (item or "").strip()
        while topic.startswith("#") or topic.startswith("＃"):
            topic = topic[1:].strip()
        if not topic:
            continue
        topic = _truncate_utf16(topic, 12)
        if utf16_len(topic) < 2 or topic in seen:
            continue
        seen.add(topic)
        topics.append(topic)
        if len(topics) >= 6:
            break
    return topics


# ---------------------------------------------------------------------------
# 包装池 / 时间线工具
# ---------------------------------------------------------------------------


def build_sfx_categories(sfx_assets: Sequence[Dict[str, Any]]) -> List[Dict[str, Any]]:
    by_resource = {asset.get("resource_id", ""): asset for asset in sfx_assets}
    categories: List[Dict[str, Any]] = []
    for resource_id, catalog_name in SFX_CATALOG:
        asset = by_resource.get(resource_id)
        if not asset:
            continue
        name = str(asset.get("name", "")).strip() or catalog_name
        categories.append({
            "name": name,
            "url": asset.get("url", ""),
            "duration_ms": int(asset.get("duration_ms", 0)),
            "usage_hint": SFX_USAGE_HINTS.get(
                name, "通用音效：仅在字幕语义明显匹配时使用"
            ),
        })
    return categories


def audio_ranges_overlap(left: Dict[str, Any], right: Dict[str, Any]) -> bool:
    return (int(left["start_us"]) < int(right["end_us"])
            and int(right["start_us"]) < int(left["end_us"]))


def dedupe_sfx_infos(infos: Sequence[Dict[str, Any]]) -> List[Dict[str, Any]]:
    ordered = sorted(infos, key=lambda item: int(item["start_us"]))
    kept: List[Dict[str, Any]] = []
    for info in ordered:
        overlap = False
        for existing in kept:
            if existing["url"] == info["url"] and audio_ranges_overlap(info, existing):
                overlap = True
                break
        if not overlap:
            kept.append(info)
    return kept


def group_sfx_batches(infos: Sequence[Dict[str, Any]]) -> List[List[Dict[str, Any]]]:
    ordered = sorted(infos, key=lambda item: int(item["start_us"]))
    batches: List[List[Dict[str, Any]]] = []
    for info in ordered:
        placed = False
        for batch in batches:
            if not any(audio_ranges_overlap(info, existing) for existing in batch):
                batch.append(info)
                placed = True
                break
        if not placed:
            batches.append([info])
    return batches


def build_bgm_audio_infos(video_duration_us: int, bgm_duration_us: int,
                          audio_url: str) -> List[Dict[str, Any]]:
    infos: List[Dict[str, Any]] = []
    url = (audio_url or "").strip()
    if video_duration_us <= 0 or bgm_duration_us <= 0 or not url:
        return infos
    position_us = 0
    while position_us < video_duration_us:
        segment_end_us = position_us + min(
            bgm_duration_us, video_duration_us - position_us
        )
        infos.append({
            "url": url,
            "start_us": position_us,
            "end_us": segment_end_us,
            "volume": BGM_VOLUME,
        })
        position_us = segment_end_us
    return infos


def video_playback_duration_us(video: Dict[str, Any]) -> int:
    scene_start = int(video.get("scene_start_us", -1))
    scene_end = int(video.get("scene_end_us", -1))
    if scene_start >= 0 and scene_end > scene_start:
        return scene_end - scene_start
    return int(video.get("end_us", 0)) - int(video.get("start_us", 0))


def _pick_effect_window(captions_data: Sequence[Dict[str, Any]], min_us: int,
                        max_us: int, rng: RandomSource
                        ) -> Optional[Tuple[int, int]]:
    windows: List[Tuple[int, int]] = []
    for start in range(len(captions_data)):
        for end in range(start, len(captions_data)):
            span_us = int(captions_data[end]["end_us"]) - int(
                captions_data[start]["start_us"]
            )
            if span_us < min_us:
                continue
            if span_us > max_us:
                break
            windows.append((start, end))
    if not windows:
        return None
    start_index, end_index = windows[rng.bounded(len(windows))]
    return (
        int(captions_data[start_index]["start_us"]),
        int(captions_data[end_index]["end_us"]),
    )


def random_dedup_scale(rng: RandomSource) -> float:
    return 1.0 + 0.1 * rng.generate_double()


def random_dedup_start_us(total_us: int, duration_us: int,
                          rng: RandomSource) -> int:
    window = total_us - duration_us
    if window <= 0:
        return 0
    return int(rng.generate_double() * float(window))


def random_dedup_transform(low: int, high: int, rng: RandomSource) -> int:
    return rng.bounded(low + 1, high)


def build_dedup_placements(images: Sequence[Dict[str, Any]], total_us: int,
                           rng: RandomSource) -> List[Dict[str, Any]]:
    placements: List[Dict[str, Any]] = []
    if total_us <= 0:
        return placements
    duration_us = min(DEDUP_IMAGE_DURATION_US, total_us)
    for image in images:
        url = str(image.get("url", "")).strip()
        if not url:
            continue
        start_us = random_dedup_start_us(total_us, duration_us, rng)
        placements.append({
            "url": url,
            "start_us": start_us,
            "end_us": start_us + duration_us,
            "transform_x": random_dedup_transform(
                DEDUP_TRANSFORM_X_MIN, DEDUP_TRANSFORM_X_MAX, rng
            ),
            "transform_y": random_dedup_transform(
                DEDUP_TRANSFORM_Y_MIN, DEDUP_TRANSFORM_Y_MAX, rng
            ),
        })
    return placements


# ---------------------------------------------------------------------------
# 源文件准备
# ---------------------------------------------------------------------------


def source_fingerprint(path: str) -> str:
    try:
        info = os.stat(path)
        size = info.st_size
        # 与 Qt toMSecsSinceEpoch 一致：取整毫秒（截断亚毫秒）。
        mtime_ms = info.st_mtime_ns // 1_000_000
    except OSError:
        size = 0
        mtime_ms = 0
    return f"asr-v2:{size}:{mtime_ms}"


def resolve_draft_input_path(source: Dict[str, Any], config: AppConfig,
                             progress: ProgressCallback = None) -> str:
    local_path = str(source.get("local_path", ""))
    if not local_path.lower().endswith(".m3u8"):
        return local_path

    playlist_dir = os.path.dirname(os.path.abspath(local_path))
    playlist_ms = hls_playlist_duration_ms(local_path)
    source_uri = str(source.get("source_uri", ""))
    source_path = urllib.parse.unquote(urllib.parse.urlsplit(source_uri).path)
    source_base = os.path.splitext(os.path.basename(source_path))[0]
    title = str(source.get("title", ""))

    candidates = [
        os.path.join(playlist_dir, sanitize_file_component(title, 80) + ".mp4"),
        os.path.join(playlist_dir, os.path.basename(playlist_dir) + ".mp4"),
    ]
    if source_base:
        candidates.append(
            os.path.join(playlist_dir, sanitize_file_component(source_base, 80) + ".mp4")
        )
    candidates.append(os.path.join(playlist_dir, "recording.mp4"))

    playlist_mtime = os.path.getmtime(local_path)
    for candidate in candidates:
        if not os.path.isfile(candidate) or os.path.getsize(candidate) <= 0:
            continue
        if os.path.getmtime(candidate) < playlist_mtime:
            continue
        duration_ms = probe.media_duration_ms(
            candidate, ffmpeg_configured=config.recording.ffmpeg_path
        )
        if not duration_ms or duration_ms <= 0:
            continue
        if playlist_ms > 0 and duration_ms + 6000 < playlist_ms:
            continue
        if progress:
            progress(f"复用已有完整 MP4：{candidate}")
        return candidate

    if progress:
        progress("正在把 HLS 录像无损合成为完整 MP4…")
    return ffmpeg.ensure_recording_mp4(
        local_path, ffmpeg_configured=config.recording.ffmpeg_path
    )


# ---------------------------------------------------------------------------
# ASR（带缓存）
# ---------------------------------------------------------------------------


def ensure_transcript(
    store: Store,
    config: AppConfig,
    request: LiveClipRequest,
    source: Dict[str, Any],
    media_path: str,
    work_dir: str,
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    fingerprint_path = str(source.get("local_path", ""))
    fingerprint = source_fingerprint(fingerprint_path)
    cached = store.load_asr(int(source["id"]), fingerprint)
    if cached and cached.get("utterances"):
        if progress:
            progress("已命中本地 ASR 缓存，跳过重复识别")
        return asr.from_cache_json(cached)

    audio_path = os.path.join(work_dir, "audio.mp3")
    if progress:
        progress("正在提取 MP3 音频…")
    ffmpeg.extract_mp3(
        media_path, audio_path,
        ffmpeg_configured=config.recording.ffmpeg_path,
    )

    if progress:
        progress("正在上传整片音频…")
    audio_url = capcut.upload_file(audio_path, config.service.api_key)

    if progress:
        progress("正在整片识别…")
    transcript = asr.transcribe(
        audio_url, config.service.api_url, config.service.api_key
    )
    if not transcript.get("utterances"):
        raise PipelineError("ASR 未识别到语音内容。")

    store.save_asr(
        int(source["id"]), fingerprint, asr.to_cache_json(transcript)
    )
    if not request.keep_work:
        try:
            os.unlink(audio_path)
        except OSError:
            pass

    if progress:
        progress(
            f"语音转写完成，共 {len(transcript['utterances'])} 句，"
            f"覆盖 {format_duration(asr.transcript_duration_ms(transcript))}"
        )
    return transcript


# ---------------------------------------------------------------------------
# 草稿生成
# ---------------------------------------------------------------------------


def _random_caption_style(rng: RandomSource) -> Dict[str, Any]:
    preset = dict(CAPTION_STYLE_PRESETS[rng.bounded(len(CAPTION_STYLE_PRESETS))])
    style = dict(DEFAULT_CAPTION_STYLE)
    style.update(preset)
    return style


def _english_caption_style(chinese_style: Dict[str, Any]) -> Dict[str, Any]:
    style = dict(chinese_style)
    style["font"] = "Inter-SemiBold"
    style["font_size"] = max(5, int(chinese_style.get("font_size", 15)) - 2)
    return style


def _load_sfx_categories(config: AppConfig,
                         progress: ProgressCallback = None) -> List[Dict[str, Any]]:
    try:
        sfx_assets = assets.list_sfx()
        categories = build_sfx_categories(sfx_assets)
        if progress:
            progress(f"音效目录就绪：{len(categories)} 个音效")
        return categories
    except Exception as error:  # noqa: BLE001 - 目录不可用只跳过音效
        if progress:
            progress(f"音效目录不可用：{error}")
        return []


def create_draft_for_project(
    store: Store,
    config: AppConfig,
    request: LiveClipRequest,
    project: Dict[str, Any],
    index: int,
    speed: float,
    sfx_categories: Sequence[Dict[str, Any]],
    work_dir: str,
    rng: RandomSource,
    progress: ProgressCallback = None,
) -> Dict[str, Any]:
    """生成一条剪映草稿；返回落库后的草稿记录。"""
    def notify(message: str) -> None:
        if progress:
            progress(message)

    bounded_speed = max(0.5, min(2.0, speed))
    ranges = project.get("ranges") or []
    if not ranges:
        raise PipelineError("成片没有可用的视频片段。")
    api_key = config.service.api_key

    videos: List[Dict[str, Any]] = []
    captions_data: List[Dict[str, Any]] = []
    caption_texts: List[str] = []
    clip_paths: List[str] = []
    source_durations: List[int] = []
    playback_durations: List[int] = []
    canvas_width, canvas_height = LANDSCAPE
    has_canvas_size = False

    timed_captions: List[Dict[str, Any]] = []
    for utterance in project.get("captions") or []:
        timed_captions.extend(
            caption.split_utterance(utterance, CAPTION_MAX_CHARS)
        )

    # 1. 裁剪并重新编码。
    range_count = len(ranges)
    for range_index, clip_range in enumerate(ranges):
        clip_path = os.path.join(
            work_dir, f"clip_{index + 1}_{range_index + 1}.mp4"
        )
        notify(
            f"裁剪第 {range_index + 1}/{range_count} 段 · "
            f"{format_duration(clip_range['start_ms'])} - "
            f"{format_duration(clip_range['end_ms'])}"
        )
        ffmpeg.cut_clip(
            str(project["input_path"]),
            int(clip_range["start_ms"]),
            int(clip_range["end_ms"]),
            clip_path,
            ffmpeg_configured=config.recording.ffmpeg_path,
        )
        clip_paths.append(clip_path)

        # 首个成功探测到尺寸的切片锁定画布，与客户端一致。
        if not has_canvas_size:
            size = probe.video_size(
                clip_path, ffmpeg_configured=config.recording.ffmpeg_path
            )
            if size:
                canvas_width, canvas_height = nearest_canvas_size(*size)
                has_canvas_size = True

        want_ms = max(1, int(clip_range["end_ms"]) - int(clip_range["start_ms"]))
        source_ms = probe.media_duration_ms(
            clip_path, ffmpeg_configured=config.recording.ffmpeg_path
        ) or 0
        if source_ms <= 0 or abs(source_ms - want_ms) > 2500:
            source_ms = want_ms
        source_durations.append(source_ms)
        playback_durations.append(
            max(1, qt_round(source_ms / bounded_speed))
        )

    # 2. 上传切片并铺时间线。
    offset_ms = 0
    for clip_index, clip_path in enumerate(clip_paths):
        notify(f"上传第 {clip_index + 1}/{len(clip_paths)} 个切片")
        clip_url = capcut.upload_file(clip_path, api_key)
        video: Dict[str, Any] = {
            "url": clip_url,
            "start_us": offset_ms * 1000,
            "end_us": (offset_ms + source_durations[clip_index]) * 1000,
            "scene_start_us": -1,
            "scene_end_us": -1,
            "transition": "",
            "transition_duration_us": 0,
        }
        if abs(bounded_speed - 1.0) > 0.001:
            video["scene_start_us"] = offset_ms * 1000
            video["scene_end_us"] = (
                offset_ms + playback_durations[clip_index]
            ) * 1000
        videos.append(video)
        offset_ms += playback_durations[clip_index]

    # 3. 字幕时间线：跨区间累计，并做重叠保护。
    caption_offset_ms = 0
    previous_caption_end_us = -1
    for range_index in range(range_count):
        clip_range = ranges[range_index]
        range_end_us = (
            caption_offset_ms + playback_durations[range_index]
        ) * 1000
        for timed in timed_captions:
            caption_start = max(int(timed["start_ms"]), int(clip_range["start_ms"]))
            caption_end = min(int(timed["end_ms"]), int(clip_range["end_ms"]))
            if caption_end - caption_start < 50:
                continue
            start_us = (
                caption_offset_ms
                + qt_round((caption_start - int(clip_range["start_ms"])) / bounded_speed)
            ) * 1000
            end_us = min(
                (
                    caption_offset_ms
                    + qt_round((caption_end - int(clip_range["start_ms"])) / bounded_speed)
                ) * 1000,
                range_end_us,
            )
            if previous_caption_end_us >= 0 and start_us < previous_caption_end_us:
                start_us = previous_caption_end_us
            if end_us - start_us < 50 * 1000:
                continue
            captions_data.append({
                "start_us": start_us,
                "end_us": end_us,
                "text": str(timed["text"]),
            })
            caption_texts.append(str(timed["text"]))
            previous_caption_end_us = end_us
        caption_offset_ms += playback_durations[range_index]

    # 4. 创建草稿。
    notify(f"新建 {canvas_width}×{canvas_height} 工程")
    draft = capcut.create_draft(canvas_width, canvas_height)

    packaging_details: List[str] = []
    transition_title = ""
    transition_pair_index = -1

    if request.transitions:
        candidates = []
        for pair_index in range(len(videos) - 1):
            if video_playback_duration_us(videos[pair_index]) <= TRANSITION_DURATION_US:
                continue
            if video_playback_duration_us(videos[pair_index + 1]) <= TRANSITION_DURATION_US:
                continue
            candidates.append(pair_index)
        if candidates:
            transition_pair_index = candidates[rng.bounded(len(candidates))]
            transition_title = TRANSITION_TITLES[
                rng.bounded(len(TRANSITION_TITLES))
            ]
            videos[transition_pair_index]["transition"] = transition_title
            videos[transition_pair_index]["transition_duration_us"] = (
                TRANSITION_DURATION_US
            )
            notify(
                f"转场「{transition_title}」挂在第 {transition_pair_index + 1} 段之后"
            )
        else:
            notify("没有长度足够的相邻切片，跳过转场")

    # 5. 写入主视频轨（去重时逐段随机放大）。
    if request.dedup:
        written = 0
        cursor = 0
        while cursor < len(videos):
            batch = []
            if cursor == transition_pair_index and cursor + 1 < len(videos):
                batch = [videos[cursor], videos[cursor + 1]]
                cursor += 2
            else:
                batch = [videos[cursor]]
                cursor += 1
            scale = random_dedup_scale(rng)
            notify(f"写入第 {written + 1}/{len(videos)} 个视频片段")
            capcut.add_videos(
                draft["draft_url"], batch, scale_x=scale, scale_y=scale
            )
            written += len(batch)
        packaging_details.append("切片随机放大")
    else:
        capcut.add_videos(draft["draft_url"], videos, 1.0, 1.0)

    if transition_title:
        packaging_details.append(f"转场「{transition_title}」")

    # 6. 特效 / 贴纸 / 滤镜 / 去重图片（失败只降级，不阻断草稿）。
    if request.effects:
        title, min_us, max_us = EFFECT_PRESETS[
            rng.bounded(len(EFFECT_PRESETS))
        ]
        window = _pick_effect_window(captions_data, min_us, max_us, rng)
        if window:
            try:
                capcut.add_effects(draft["draft_url"], [{
                    "title": title,
                    "start_us": window[0],
                    "end_us": window[1],
                }])
                packaging_details.append(f"特效「{title}」")
            except capcut.CapCutError as error:
                notify(f"特效未写入：{error}")
                packaging_details.append("特效未写入")
        else:
            notify("没有合适的字幕窗口，跳过特效")

    if request.stickers:
        name, sticker_id, transform_x, transform_y, scale = STICKER_PRESETS[
            rng.bounded(len(STICKER_PRESETS))
        ]
        window = _pick_effect_window(
            captions_data, SFX_MIN_DURATION_US, SFX_MAX_DURATION_US, rng
        )
        if window:
            try:
                capcut.add_sticker(
                    draft["draft_url"], sticker_id,
                    window[0], window[1], scale, transform_x, transform_y,
                )
                packaging_details.append(f"贴纸「{name}」")
            except capcut.CapCutError as error:
                notify(f"贴纸未写入：{error}")
                packaging_details.append("贴纸未写入")
        else:
            notify("没有 3–10 秒的字幕窗口，跳过贴纸")

    if request.filters and offset_ms > 0:
        title = FILTER_TITLES[rng.bounded(len(FILTER_TITLES))]
        intensity = rng.bounded(1, 31)
        try:
            capcut.add_filters(draft["draft_url"], [{
                "title": title,
                "start_us": 0,
                "end_us": offset_ms * 1000,
                "intensity": intensity,
            }])
            packaging_details.append(f"滤镜「{title}」")
        except capcut.CapCutError as error:
            notify(f"滤镜未写入：{error}")
            packaging_details.append("滤镜未写入")

    if request.dedup:
        try:
            images = assets.random_images(DEDUP_IMAGE_COUNT, DEDUP_TAG)
            if not images:
                packaging_details.append("去重图片未获取")
            else:
                placements = build_dedup_placements(
                    images, offset_ms * 1000, rng
                )
                applied = 0
                for placement in placements:
                    try:
                        capcut.add_images(
                            draft["draft_url"],
                            [{
                                "url": placement["url"],
                                "start_us": placement["start_us"],
                                "end_us": placement["end_us"],
                            }],
                            DEDUP_IMAGE_SCALE, DEDUP_IMAGE_SCALE,
                            placement["transform_x"], placement["transform_y"],
                            DEDUP_IMAGE_ALPHA,
                        )
                        applied += 1
                    except capcut.CapCutError as error:
                        notify(f"去重图片未写入：{error}")
                packaging_details.append(
                    f"去重图片 {applied} 张" if applied > 0 else "去重图片未写入"
                )
        except assets.AssetsError as error:
            notify(f"去重图片未获取：{error}")
            packaging_details.append("去重图片未获取")

    # 7. 字幕（字体/颜色/动画每条成片随机一套）。
    caption_style = _random_caption_style(rng)
    for item in captions_data:
        item["in_animation"] = caption_style.get("in_animation", "")
    capcut.add_captions(draft["draft_url"], captions_data, caption_style)

    # 8. 自动音效。
    sfx_count = 0
    sfx_note = ""
    if request.sfx and captions_data and sfx_categories:
        notify("正在挑选字幕音效")
        segments = [
            {
                "start_ms": int(item["start_us"]) // 1000,
                "end_ms": int(item["end_us"]) // 1000,
                "text": caption_texts[caption_index],
            }
            for caption_index, item in enumerate(captions_data)
        ]
        categories = [
            {"name": category["name"], "usage_hint": category["usage_hint"]}
            for category in sfx_categories
        ]
        try:
            selections = llm.select_sfx(
                config.service.api_url, api_key, segments, categories,
                on_status=notify,
            )
            timeline_end_us = offset_ms * 1000
            caption_end_ms = (
                int(captions_data[-1]["end_us"]) // 1000 if captions_data else 0
            )
            budget = max(0, min(8, qt_round(caption_end_ms / 60000.0 * 5.0)))
            eligible = [
                index
                for index in range(min(len(selections), len(captions_data)))
                if 0 <= selections[index] < len(sfx_categories)
            ]
            take = min(budget, len(eligible))
            sfx_infos: List[Dict[str, Any]] = []
            for index in eligible[:take]:
                category = sfx_categories[selections[index]]
                start_us = int(captions_data[index]["start_us"])
                duration_us = int(category["duration_ms"]) * 1000
                end_us = start_us + duration_us
                if start_us < 0 or end_us > timeline_end_us:
                    continue
                sfx_infos.append({
                    "url": category["url"],
                    "start_us": start_us,
                    "end_us": end_us,
                    "volume": SFX_VOLUME,
                    "duration_us": duration_us,
                })

            batches = group_sfx_batches(dedupe_sfx_infos(sfx_infos))
            written = 0
            failed = False
            for batch in batches:
                try:
                    capcut.add_audios(draft["draft_url"], batch)
                    written += len(batch)
                except capcut.CapCutError as error:
                    failed = True
                    notify(f"音效批次未写入：{error}")
            sfx_count = written
            if failed:
                sfx_note = "部分音效未写入" if written > 0 else "音效未写入"
        except llm.LlmError as error:
            notify(f"音效选点失败：{error}")
            sfx_note = "音效未生成"
    elif request.sfx and not captions_data:
        sfx_note = "没有可配音效的字幕"
    elif request.sfx and not sfx_categories:
        sfx_note = "音效素材不可用"

    # 9. BGM。
    bgm_note = ""
    if request.bgm and offset_ms > 0:
        notify("正在随机匹配背景音乐")
        track = None
        try:
            track = assets.random_bgm()
        except assets.AssetsError as error:
            notify(f"背景音乐未获取：{error}")
            bgm_note = "背景音乐未获取"
        if track is not None:
            bgm_ms = int(track.get("duration_ms", 0))
            if bgm_ms <= 0:
                bgm_ms = probe.media_duration_ms(
                    track["url"], ffmpeg_configured=config.recording.ffmpeg_path
                ) or 0
            if bgm_ms > 0:
                bgm_infos = build_bgm_audio_infos(
                    offset_ms * 1000, bgm_ms * 1000, track["url"]
                )
                try:
                    capcut.add_audios(draft["draft_url"], bgm_infos)
                    bgm_note = f"背景音乐「{track['name']}」"
                    if len(bgm_infos) > 1:
                        bgm_note += f" · 循环 {len(bgm_infos)} 次"
                except capcut.CapCutError as error:
                    notify(f"背景音乐未写入：{error}")
                    bgm_note = "背景音乐未写入"
            else:
                bgm_note = "背景音乐时长未知"

    # 10. 中英文双字幕。
    english_count = 0
    english_note = ""
    if request.bilingual_subtitles and captions_data:
        pending_indices = [
            index for index, text in enumerate(caption_texts)
            if caption.contains_cjk_character(text)
        ]
        pending_texts = [caption_texts[index] for index in pending_indices]
        if not pending_texts:
            english_note = "没有需要翻译的中文字幕"
        else:
            notify(f"正在生成 {len(pending_texts)} 条英文译文")
            try:
                translations = llm.translate_captions(
                    config.service.api_url, api_key, pending_texts,
                    on_status=notify,
                )
                english_base_style = _english_caption_style(caption_style)
                groups: Dict[int, List[Dict[str, Any]]] = {}
                for translation_index, translated in enumerate(translations):
                    translated = (translated or "").strip()
                    if (not translated
                            or caption.contains_cjk_character(translated)
                            or translated.lower() == pending_texts[translation_index].lower()):
                        continue
                    chinese = captions_data[pending_indices[translation_index]]
                    chinese_lines = chinese["text"].count("\n") + 1
                    transform_y = qt_round(caption.english_caption_transform_y(
                        caption_style,
                        english_base_style["font_size"],
                        chinese_lines,
                        canvas_height,
                    ))
                    groups.setdefault(transform_y, []).append({
                        "start_us": chinese["start_us"],
                        "end_us": chinese["end_us"],
                        "text": translated,
                    })

                if not groups:
                    english_note = "未得到有效英文译文"
                else:
                    failed_groups = 0
                    for transform_y in sorted(groups):
                        style = dict(english_base_style)
                        style["transform_y"] = float(transform_y)
                        try:
                            capcut.add_captions(
                                draft["draft_url"], groups[transform_y], style
                            )
                            english_count += len(groups[transform_y])
                        except capcut.CapCutError as error:
                            failed_groups += 1
                            notify(f"英文轨写入失败：{error}")
                    if failed_groups > 0:
                        english_note = (
                            "部分英文轨写入失败" if english_count > 0
                            else "英文轨写入失败"
                        )
            except llm.LlmError as error:
                notify(f"英文译文生成失败：{error}")
                english_note = "英文译文生成失败"

    # 11. 落库。
    title = project.get("title") or "直播切片"
    tags = project.get("topics") or ["直播切片"]
    record = {
        "source_id": int(request.source_id),
        "draft_id": draft["draft_id"],
        "draft_url": draft["draft_url"],
        "title": title,
        "description": project.get("description", ""),
        "tags": list(tags),
        "source_start_ms": int(ranges[0]["start_ms"]),
        "source_end_ms": int(ranges[-1]["end_ms"]),
        "duration_ms": offset_ms,
        "created_at_ms": current_ms(),
        "caption_style": caption_style,
        "sfx_count": sfx_count,
        "english_count": english_count,
        "packaging": packaging_details,
        "sfx_note": sfx_note,
        "bgm_note": bgm_note,
        "english_note": english_note,
        "canvas": [canvas_width, canvas_height],
    }
    record["row_id"] = store.add_draft(record)

    if not request.keep_work:
        for clip_path in clip_paths:
            try:
                os.unlink(clip_path)
            except OSError:
                pass
    return record


# ---------------------------------------------------------------------------
# 主流水线
# ---------------------------------------------------------------------------


def _prepare_source(store: Store, request: LiveClipRequest,
                    config: AppConfig) -> Dict[str, Any]:
    if request.source_id >= 0:
        source = store.source(request.source_id)
        if not source:
            raise PipelineError(f"源视频不存在（source_id={request.source_id}）。")
        return source

    if not request.input_path:
        raise PipelineError("未指定源视频：请传 --input 或 --source-id。")

    input_path = os.path.abspath(os.path.expanduser(request.input_path))
    if not os.path.isfile(input_path):
        raise PipelineError(f"源视频文件不存在：{input_path}")

    title = (request.title or "").strip() or os.path.basename(input_path)

    if input_path.lower().endswith(".m3u8"):
        # HLS 播放列表没有视频流，不能走 ffprobe 的本地导入校验；
        # 时长取 #EXTINF 之和，真正裁剪前会先合成完整 MP4。
        duration_ms = hls_playlist_duration_ms(input_path)
        if duration_ms <= 0:
            raise PipelineError(f"无法读取 HLS 播放列表时长：{input_path}")
        source_id = store.ensure_source_for_input(
            input_path, title, duration_ms=duration_ms,
        )
        request.source_id = source_id
        return store.source(source_id) or {}

    info = probe.probe(input_path, ffmpeg_configured=config.recording.ffmpeg_path)
    if not info.ok:
        raise PipelineError(f"源视频不可用：{info.error}")
    source_id = store.ensure_source_for_input(
        input_path, title,
        duration_ms=info.duration_ms,
        size_bytes=info.size_bytes,
        probe_json=json.dumps(info.json_data, ensure_ascii=False),
    )
    request.source_id = source_id
    return store.source(source_id) or {}


def run_pipeline(store: Store, config: AppConfig, request: LiveClipRequest,
                 progress: ProgressCallback = None) -> List[Dict[str, Any]]:
    """执行一键成片，返回生成的草稿记录列表。"""
    def notify(message: str) -> None:
        if progress:
            progress(message)

    if not config.service.api_key.strip():
        raise PipelineError("尚未配置 API 密钥，请设置 LIVEMIX_API_KEY 或配置 service.apiKey。")
    if not config.service.api_url.strip():
        raise PipelineError("API 地址为空，请检查配置。")
    if not request.prompt or not request.prompt.content.strip():
        raise PipelineError("未选择选片提示词。")

    source = _prepare_source(store, request, config)
    if source.get("status") != "ready":
        raise PipelineError("源视频不可用，请等待录制完成或重新导入。")
    local_path = str(source.get("local_path", ""))
    if not local_path or not os.path.isfile(local_path):
        raise PipelineError(f"源视频文件不存在：{local_path}")

    source_duration_ms = int(source.get("duration_ms", 0) or 0)
    if source_duration_ms <= 0:
        source_duration_ms = probe.media_duration_ms(
            local_path, ffmpeg_configured=config.recording.ffmpeg_path
        ) or 0

    requested_clips = int(request.target_clip_count)
    if requested_clips > 0 and source_duration_ms > 0:
        min_allowed = min_clip_count_for_duration(source_duration_ms)
        max_allowed = max_clip_count_for_duration(source_duration_ms)
        if requested_clips < min_allowed or requested_clips > max_allowed:
            raise PipelineError(
                f"素材时长 {format_duration(source_duration_ms)}，成片数量需在 "
                f"{min_allowed}–{max_allowed} 条之间：每 5 分钟素材最多生成 1 条，"
                "数量过多会导致切片重复；每条最多使用 30 分钟素材。"
            )

    work_dir = os.path.join(
        config.storage.cache_dir,
        "clip-jobs",
        f"source_{request.source_id}_{current_ms()}",
    )
    os.makedirs(work_dir, exist_ok=True)
    rng = RandomSource(request.seed)
    try:
        notify(f"源视频：{source.get('title', '')}（{format_duration(source_duration_ms)}）")
        try:
            draft_input_path = resolve_draft_input_path(source, config, notify)
        except ffmpeg.FfmpegError as error:
            raise PipelineError(f"准备完整 MP4 失败：{error}")

        transcript = ensure_transcript(
            store, config, request, source, draft_input_path, work_dir, notify
        )

        asr_duration_ms = asr.transcript_duration_ms(transcript)
        utterance_count = len(transcript.get("utterances") or [])
        if utterance_count <= 0:
            raise PipelineError("转写结果中没有可用于选片的语句。")

        auto_clips = min_clip_count_for_duration(
            source_duration_ms if source_duration_ms > 0 else asr_duration_ms
        )
        target_total = requested_clips if requested_clips > 0 else auto_clips

        llm_window_count = min_clip_count_for_duration(asr_duration_ms)
        if target_total < llm_window_count:
            notify(
                f"成片数量提升到 {llm_window_count} 条，保证单次 LLM 选片不超过 30 分钟"
            )
            target_total = llm_window_count
        target_total = max(1, min(target_total, utterance_count))

        chunks = build_chunks(transcript, target_total)
        if not chunks:
            raise PipelineError("转写结果中没有可用于选片的语句。")
        target_total = len(chunks)

        notify(
            f"转写 {format_duration(asr_duration_ms)}，"
            f"切分为 {len(chunks)} 个时间窗，计划生成 {target_total} 条成片"
        )

        projects: List[Dict[str, Any]] = []
        for chunk_index, chunk in enumerate(chunks, start=1):
            segments = chunk["segments"]
            notify(
                f"正在分析第 {chunk_index}/{len(chunks)} 条成片"
                f"（{format_duration(chunk['start_ms'])} - "
                f"{format_duration(chunk['end_ms'])}）"
            )
            try:
                result = llm.select_project(
                    config.service.api_url,
                    config.service.api_key,
                    request.prompt.content,
                    segments,
                    on_status=notify,
                )
            except llm.LlmError as error:
                if not error.parse_failure:
                    raise PipelineError(
                        f"第 {chunk_index}/{len(chunks)} 条成片选片失败：{error.message}"
                    )
                notify("首次输出无法解析，正在用严格模式重试…")
                try:
                    result = llm.select_project(
                        config.service.api_url,
                        config.service.api_key,
                        request.prompt.content,
                        segments,
                        strict_json=True,
                        on_status=notify,
                    )
                except llm.LlmError as retry_error:
                    raise PipelineError(
                        f"第 {chunk_index}/{len(chunks)} 条成片选片失败："
                        f"{retry_error.message}"
                    )

            if not result.indices:
                notify("该段没有符合提示词的片段，跳过")
                continue

            ranges: List[Dict[str, int]] = []
            captions: List[Dict[str, Any]] = []
            picked = set()
            for index in result.indices:
                if index < 0 or index >= len(segments) or index in picked:
                    continue
                segment = segments[index]
                if int(segment.get("end_ms", 0)) <= int(segment.get("start_ms", 0)):
                    continue
                picked.add(index)
                ranges.append({
                    "start_ms": int(segment["start_ms"]),
                    "end_ms": int(segment["end_ms"]),
                })
                captions.append(segment)
            if not ranges:
                notify("该段没有有效的裁剪区间，跳过")
                continue
            ranges = merge_ranges(ranges)

            project = {
                "ranges": ranges,
                "captions": captions,
                "title": sanitize_title(result.title),
                "description": sanitize_description(result.description),
                "topics": sanitize_topics(result.topics),
                "input_path": draft_input_path,
            }
            projects.append(project)
            display_title = project["title"] or "直播切片"
            notify(
                f"第 {chunk_index}/{len(chunks)} 条成片已选出：{display_title}"
                f"（{len(ranges)} 段，共 "
                f"{format_duration(sum(int(r['end_ms']) - int(r['start_ms']) for r in ranges))}）"
            )

        if not projects:
            raise PipelineError("AI 没有选出可用的切片，请更换提示词后重试。")

        sfx_categories = _load_sfx_categories(config, notify) if request.sfx else []

        drafts: List[Dict[str, Any]] = []
        for index, project in enumerate(projects):
            if request.speed_auto:
                speed = 1.05 + 0.01 * rng.bounded(46)
            else:
                speed = max(0.5, min(2.0, request.fixed_speed))
            notify(
                f"生成第 {index + 1}/{len(projects)} 条草稿："
                f"{project['title'] or '直播切片'} · {speed:.2f}×"
            )
            try:
                record = create_draft_for_project(
                    store, config, request, project, index, speed,
                    sfx_categories, work_dir, rng, notify,
                )
            except (capcut.CapCutError, ffmpeg.FfmpegError,
                    PipelineError) as error:
                raise PipelineError(
                    f"第 {index + 1} 条草稿生成失败：{error}"
                )
            drafts.append(record)
            notify(
                f"第 {index + 1}/{len(projects)} 条草稿已生成："
                f"{record['title']} · 成片 {format_duration(record['duration_ms'])}"
            )
        return drafts
    finally:
        if request.keep_work:
            notify(f"保留工作目录：{work_dir}")
        else:
            shutil.rmtree(work_dir, ignore_errors=True)
