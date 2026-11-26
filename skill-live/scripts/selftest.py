"""技能自检：覆盖与客户端对齐的关键纯逻辑。

不依赖网络、ffmpeg 或 API 密钥，可随时运行：
    python3 scripts/livemix.py selftest
"""

from __future__ import annotations

import traceback
from typing import Any, Callable, Dict, List

import asr
import caption
import llm
import pipeline
import recording
from prompts import BUILTIN_PROMPTS
from util import utf16_len


def _test_caption_balanced_split() -> str:
    text = "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三"
    pieces = caption.split_text_balanced(text, 15)
    assert [len(piece) for piece in pieces] == [11, 11, 11], pieces
    return "33 字均衡拆为 11/11/11"


def _test_caption_tokens() -> str:
    text = "最低只要100%正品原价29.3元hello"
    pieces = caption.split_text_balanced(text, 8)
    joined = "".join(pieces)
    assert "100%" in joined and "29.3" in joined and "hello" in joined, pieces
    for piece in pieces:
        assert not piece.endswith("100") and "29." not in piece[-3:], pieces
    return "英文单词/数字/百分号保持完整"


def _test_caption_punctuation() -> str:
    clauses = caption.split_clauses("价格是29.3元，100%正品。", 15)
    texts = [clause.display_text for clause in clauses]
    # 与客户端一致：Unicode 标点（含 %）会被清理，数字中的小数点保留。
    assert texts == ["价格是29.3元", "100正品"], texts
    return "数字小数点不拆句，标点被清理"


def _test_caption_word_timing() -> str:
    utterance = {
        "text": "你好世界",
        "start_ms": 0,
        "end_ms": 400,
        "words": [
            {"text": "你", "start_ms": 0, "end_ms": 100},
            {"text": "好", "start_ms": 100, "end_ms": 200},
            {"text": "世", "start_ms": 200, "end_ms": 300},
            {"text": "界", "start_ms": 300, "end_ms": 400},
        ],
    }
    timed = caption.split_utterance(utterance, 2)
    assert [(item["text"], item["start_ms"], item["end_ms"]) for item in timed] == [
        ("你好", 0, 200), ("世界", 200, 400),
    ], timed
    return "词级时间线对齐两条字幕"


def _test_english_position() -> str:
    style = {"font_size": 8, "transform_y": -800.0}
    value = caption.english_caption_transform_y(style, 6, 1, 1920)
    assert round(value) == -920, value
    return "单行中文+单行英文 transform_y=-920"


def _test_clip_counts() -> str:
    assert pipeline.min_clip_count_for_duration(60 * 60 * 1000) == 2
    assert pipeline.max_clip_count_for_duration(60 * 60 * 1000) == 12
    assert pipeline.min_clip_count_for_duration(0) == 1
    assert pipeline.max_clip_count_for_duration(0) == 100
    return "60 分钟素材允许 2–12 条"


def _test_chunks() -> str:
    utterances = [
        {"text": f"第{i}句。" if i % 10 == 0 else f"第{i}句",
         "start_ms": i * 1000, "end_ms": (i + 1) * 1000, "words": []}
        for i in range(120)
    ]
    transcript = {"text": "", "utterances": utterances}
    chunks = pipeline.build_chunks(transcript, 4)
    assert len(chunks) == 4, len(chunks)
    assert sum(len(chunk["segments"]) for chunk in chunks) == 120
    for left, right in zip(chunks, chunks[1:]):
        assert left["end_ms"] <= right["start_ms"], (left, right)
    return "4 个时间窗覆盖全部句段且不重叠"


def _test_merge_ranges() -> str:
    merged = pipeline.merge_ranges([
        {"start_ms": 0, "end_ms": 1000},
        {"start_ms": 1400, "end_ms": 2000},
        {"start_ms": 5000, "end_ms": 6000},
    ])
    assert merged == [
        {"start_ms": 0, "end_ms": 2000},
        {"start_ms": 5000, "end_ms": 6000},
    ], merged
    return "相邻区间 ≤500ms 合并"


def _test_sanitize() -> str:
    assert pipeline.sanitize_title(
        "  这是一个超过十二个汉字的标题内容  "
    ) == "这是一个超过十二个汉字的"
    assert pipeline.sanitize_topics(["#宏观", "宏观", "AI", "#A"]) == ["宏观", "AI"]
    assert pipeline.sanitize_description("x" * 200) == "x" * 128
    return "标题 12 字 / 话题去重截断 / 描述 128 字"


def _test_canvas() -> str:
    assert pipeline.nearest_canvas_size(1920, 1080) == (1920, 1080)
    assert pipeline.nearest_canvas_size(1080, 1920) == (1080, 1920)
    assert pipeline.nearest_canvas_size(0, 0) == (1920, 1080)
    return "画布按源画幅选择横屏/竖屏"


def _test_bgm_loop() -> str:
    infos = pipeline.build_bgm_audio_infos(25_000_000, 10_000_000, "https://x/a.mp3")
    assert len(infos) == 3, infos
    assert infos[0] == {"url": "https://x/a.mp3", "start_us": 0,
                        "end_us": 10_000_000, "volume": 0.1}
    assert infos[-1]["end_us"] == 25_000_000
    return "BGM 短于成片时按整首循环铺满"


def _test_sfx_batches() -> str:
    infos = [
        {"url": "a", "start_us": 0, "end_us": 10},
        {"url": "b", "start_us": 5, "end_us": 15},
        {"url": "a", "start_us": 5, "end_us": 15},
    ]
    kept = pipeline.dedupe_sfx_infos(infos)
    assert len(kept) == 2, kept
    batches = pipeline.group_sfx_batches(kept)
    assert len(batches) == 2, batches
    return "同 URL 重叠去重，重叠音效分批写入"


def _test_prompt_catalog() -> str:
    names = [entry.name for entry in BUILTIN_PROMPTS]
    assert names == ["财经类直播", "服装切片 · 黄金3秒定稿", "默认提示词", "服装切片 · 基础版"], names
    assert "黄金3秒" in BUILTIN_PROMPTS[1].content
    assert "成片时长限制" in BUILTIN_PROMPTS[0].content
    return "内置 4 套提示词与客户端一致"


def _test_asr_parse() -> str:
    payload = {
        "text": "你好世界",
        "duration": 1.0,
        "segments": [
            {"text": "你好", "start": 0.0, "end": 0.5, "confidence": 0.9,
             "words": [{"word": "你", "start": 0.0, "end": 0.2}]},
            {"text": "世界", "start": 0.5, "end": 1.0},
        ],
    }
    transcript = asr.transcript_from_verbose_json(payload)
    assert asr.transcript_duration_ms(transcript) == 1000
    assert len(transcript["utterances"]) == 2
    assert transcript["utterances"][0]["confidence"] == 90.0

    # 缓存格式必须与客户端 AsrTranscript::toJson 一致（start_time/end_time）。
    cache = asr.to_cache_json(transcript)
    word = cache["utterances"][0]["words"][0]
    assert word["start_time"] == 0 and word["end_time"] == 200, word
    restored = asr.from_cache_json(cache)
    assert asr.transcript_duration_ms(restored) == 1000
    timed = caption.split_utterance(restored["utterances"][0], 15)
    assert timed and timed[0]["start_ms"] == 0
    return "verbose_json 解析 + 客户端缓存格式往返"


def _test_llm_parse() -> str:
    result = llm.parse_selection(
        '```json\n{"indices":[1,"3"],"title":"标题","description":"介绍",'
        '"topics":["话题","话题2"]}\n```'
    )
    assert result.indices == [1, 3], result.indices
    assert result.title == "标题"
    filled, translations = llm.parse_translations(
        '{"translations":[{"index":0,"text":"hello"},{"index":1,"text":"world"}]}', 2
    )
    assert filled == 2 and translations == ["hello", "world"]
    return "选片 JSON / 译文 JSON 多形态解析"


def _test_sanitize_path() -> str:
    from util import sanitize_file_component
    assert sanitize_file_component('a/b:c*?"<>|%d') == "a_b_c_______d"
    # 与客户端一致：先 simplified（此时 "... abc"），再去掉开头的点，保留空格。
    assert sanitize_file_component("... abc  ") == " abc"
    return "文件名清理与非法字符替换"


def _test_stream_url() -> str:
    assert recording.normalize_stream_url("382915305384") == (
        "https://live.douyin.com/382915305384"
    )
    assert recording.normalize_stream_url("https://live.douyin.com/123456").startswith(
        "https://live.douyin.com/"
    )
    assert recording.normalize_stream_url("not a url") is None
    return "直播间号/URL 归一化"


def _test_utf16() -> str:
    assert utf16_len("abc") == 3
    assert utf16_len("😀") == 2
    return "UTF-16 长度语义（emoji 计 2）"


TESTS: List[Callable[[], str]] = [
    _test_caption_balanced_split,
    _test_caption_tokens,
    _test_caption_punctuation,
    _test_caption_word_timing,
    _test_english_position,
    _test_clip_counts,
    _test_chunks,
    _test_merge_ranges,
    _test_sanitize,
    _test_canvas,
    _test_bgm_loop,
    _test_sfx_batches,
    _test_prompt_catalog,
    _test_asr_parse,
    _test_llm_parse,
    _test_sanitize_path,
    _test_stream_url,
    _test_utf16,
]


def run() -> Dict[str, Any]:
    results = []
    for test in TESTS:
        name = test.__name__.replace("_test_", "")
        try:
            detail = test()
            results.append({"name": name, "ok": True, "detail": detail})
        except Exception as error:  # noqa: BLE001 - 自检需要汇总所有失败
            results.append({
                "name": name,
                "ok": False,
                "detail": str(error),
                "traceback": traceback.format_exc(),
            })
    passed = sum(1 for item in results if item["ok"])
    return {
        "ok": passed == len(results),
        "passed": passed,
        "total": len(results),
        "results": results,
    }
