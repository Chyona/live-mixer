"""大模型网关调用：选片、音效选点、字幕翻译。

逐行移植 LiveMix/src/api/LlmClient.cpp：
- 模型 qwen3.8-flash；
- 选片使用 SSE 流式输出，temperature 0.2，max_tokens 4000；
- 429/5xx 最多重试 3 次；网络超时最多重试 2 次；
- 解析失败由流水线用「严格 JSON」提示词再试一次。
"""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Sequence, Tuple

import http_client as http
from util import qt_round

LLM_MODEL = "qwen3.8-flash"
LLM_IDLE_TIMEOUT_SECONDS = 5 * 60
StreamCallback = Optional[Callable[[str], None]]


class LlmError(RuntimeError):
    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message

    @property
    def parse_failure(self) -> bool:
        return (
            "无法解析" in self.message
            or "返回内容为空" in self.message
            or "未返回最终选片结果" in self.message
        )


@dataclass
class LlmSelectionResult:
    indices: List[int] = field(default_factory=list)
    title: str = ""
    description: str = ""
    topics: List[str] = field(default_factory=list)
    reasoning: str = ""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


OUTPUT_FORMAT = (
    "## 输出格式\n"
    "- 仅输出一个 JSON 对象，不要输出任何额外文字、注释或角色标记。\n"
    "- 字段：\n"
    "  - indices：选中的句段索引（整数数组），按原顺序递增。"
    "索引必须来自 视频ASR 列表的行号（从 0 开始）。\n"
    "  - title：短视频标题，2～12 个汉字。\n"
    "  - description：短视频内容介绍，128 个字以内，概括成片核心观点。\n"
    "  - topics：短视频话题字符串数组，共 2～6 个；每个话题 2～12 个汉字，"
    "不要带 # 号。\n"
    "- 标题、介绍与话题须依据选中句段撰写，不得编造 ASR 未出现的事实。\n"
    "- 示例：{\"indices\":[2,5,9,13],\"title\":\"利率上行的真相\","
    "\"description\":\"用公开数据拆解本轮加息如何影响资产价格与配置思路。\","
    "\"topics\":[\"宏观经济\",\"利率周期\",\"资产配置\"]}"
)


def _chat_url(base_url: str) -> str:
    base = (base_url or "").strip().rstrip("/")
    if not base:
        raise LlmError("API 地址无效：空地址")
    return base + "/chat/completions"


def format_asr_line(index: int, utterance: Dict[str, Any]) -> str:
    start = int(utterance.get("start_ms", 0)) / 1000.0
    end = int(utterance.get("end_ms", 0)) / 1000.0
    text = str(utterance.get("text", "")).strip()
    return f"[{index}] ({start:.2f} - {end:.2f}) {text}"


def build_user_prompt(segments: Sequence[Dict[str, Any]], *,
                      strict_json: bool = False,
                      extra_instruction: str = "",
                      excluded_indices: Optional[Sequence[int]] = None) -> str:
    prompt = ""
    if strict_json:
        prompt += (
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n"
        )
    prompt += "## 视频ASR\n"
    for index, utterance in enumerate(segments):
        prompt += format_asr_line(index, utterance) + "\n"
    prompt += "\n"

    extra = (extra_instruction or "").strip()
    excluded = list(excluded_indices or [])
    if extra or excluded:
        prompt += "## 本次选片要求\n"
        if extra:
            prompt += extra + "\n"
        if excluded:
            prompt += (
                "- 不要选择以下已经用于其他成片的句段索引："
                + ", ".join(str(index) for index in excluded)
                + "\n"
            )
        prompt += "\n"
    prompt += OUTPUT_FORMAT
    return prompt


def _content_text(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        text = ""
        for part in value:
            if isinstance(part, dict):
                piece = part.get("text")
                if isinstance(piece, str):
                    text += piece
        return text
    return ""


def _strip_markdown_fence(content: str) -> str:
    content = content.strip()
    if not content.startswith("```"):
        return content
    content = content[3:]
    if content.lower().startswith("json"):
        content = content[4:]
    end = content.rfind("```")
    if end >= 0:
        content = content[:end]
    return content.strip()


def _extract_balanced(text: str, open_char: str, close_char: str) -> str:
    start = text.find(open_char)
    if start < 0:
        return ""
    depth = 0
    in_string = False
    escape = False
    for index in range(start, len(text)):
        ch = text[index]
        if in_string:
            if escape:
                escape = False
            elif ch == "\\":
                escape = True
            elif ch == '"':
                in_string = False
            continue
        if ch == '"':
            in_string = True
        elif ch == open_char:
            depth += 1
        elif ch == close_char:
            depth -= 1
            if depth == 0:
                return text[start:index + 1]
    return ""


def _parse_int_array(array: Any) -> List[int]:
    indices: List[int] = []
    if not isinstance(array, list):
        return indices
    for value in array:
        if isinstance(value, bool):
            continue
        if isinstance(value, (int, float)):
            indices.append(int(value))
        elif isinstance(value, str):
            try:
                indices.append(int(value.strip()))
            except ValueError:
                continue
    return indices


def _parse_string_array(array: Any) -> List[str]:
    if not isinstance(array, list):
        return []
    return [
        value.strip() for value in array
        if isinstance(value, str) and value.strip()
    ]


def _try_parse_object(content: str) -> Optional[LlmSelectionResult]:
    try:
        obj = json.loads(content)
    except ValueError:
        return None
    if not isinstance(obj, dict):
        return None
    has_indices = "indices" in obj
    has_indexes = "indexes" in obj
    if not has_indices and not has_indexes:
        return None
    result = LlmSelectionResult()
    result.indices = _parse_int_array(obj.get("indices" if has_indices else "indexes"))
    title = obj.get("title")
    description = obj.get("description")
    result.title = title.strip() if isinstance(title, str) else ""
    result.description = description.strip() if isinstance(description, str) else ""
    result.topics = _parse_string_array(obj.get("topics"))
    return result


def parse_selection(raw_content: str) -> LlmSelectionResult:
    content = _strip_markdown_fence(raw_content)
    if not content:
        raise LlmError("LLM 返回内容为空。")

    result = _try_parse_object(content)
    if result is not None:
        return result

    object_text = _extract_balanced(content, "{", "}")
    if object_text and object_text != content:
        result = _try_parse_object(object_text)
        if result is not None:
            return result

    array_text = _extract_balanced(content, "[", "]")
    if array_text:
        try:
            parsed = json.loads(array_text)
        except ValueError:
            parsed = None
        if isinstance(parsed, list):
            result = LlmSelectionResult()
            result.indices = _parse_int_array(parsed)
            return result

    raise LlmError(f"无法解析 LLM 返回的选片 JSON：{content[:256]}")


def _request_error_message(response: http.HttpResponse) -> str:
    payload = response.json if isinstance(response.json, dict) else {}
    error = payload.get("error")
    if isinstance(error, dict):
        message = str(error.get("message", "")).strip()
        if message:
            return message
    message = str(payload.get("message", "")).strip()
    if message:
        return message
    if response.error:
        return response.error
    return f"HTTP {response.status}"


def _apply_usage(usage: Any, state: Dict[str, int]) -> None:
    if not isinstance(usage, dict):
        return
    prompt_tokens = int(usage.get("prompt_tokens", 0) or 0)
    completion_tokens = int(usage.get("completion_tokens", 0) or 0)
    total_tokens = int(usage.get("total_tokens", 0) or 0)
    if prompt_tokens > 0 or completion_tokens > 0 or total_tokens > 0:
        state["prompt_tokens"] = prompt_tokens
        state["completion_tokens"] = completion_tokens
        state["total_tokens"] = total_tokens or prompt_tokens + completion_tokens


def select_project(
    base_url: str,
    api_key: str,
    system_prompt: str,
    segments: Sequence[Dict[str, Any]],
    *,
    strict_json: bool = False,
    on_thought: StreamCallback = None,
    on_status: StreamCallback = None,
) -> LlmSelectionResult:
    """调用 chat/completions 选片；失败抛 LlmError。"""
    if not (api_key or "").strip():
        raise LlmError("尚未配置 API 密钥。")
    url = _chat_url(base_url)

    messages: List[Dict[str, Any]] = []
    if (system_prompt or "").strip():
        messages.append({"role": "system", "content": system_prompt})
    messages.append({
        "role": "user",
        "content": build_user_prompt(segments, strict_json=strict_json),
    })

    payload = {
        "model": LLM_MODEL,
        "messages": messages,
        "temperature": 0.2,
        "max_tokens": 4000,
        "stream": True,
        "stream_options": {"include_usage": True},
    }

    max_attempts = 3
    last_error = ""

    for attempt in range(max_attempts):
        state_text: List[str] = []
        state_reasoning: List[str] = []
        usage_state = {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
        done_seen = False
        content_started = False

        def handle_object(obj: Dict[str, Any]) -> None:
            nonlocal content_started
            _apply_usage(obj.get("usage"), usage_state)
            choices = obj.get("choices")
            if not isinstance(choices, list):
                return
            for choice in choices:
                if not isinstance(choice, dict):
                    continue
                delta = choice.get("delta")
                if not isinstance(delta, dict):
                    continue
                reasoning = _content_text(delta.get("reasoning_content"))
                if not reasoning:
                    reasoning = _content_text(delta.get("reasoning"))
                if reasoning:
                    state_reasoning.append(reasoning)
                    if on_thought:
                        on_thought(reasoning)
                content = _content_text(delta.get("content"))
                if content:
                    state_text.append(content)
                    if not content_started:
                        content_started = True
                        if on_status:
                            on_status("正在生成选片结果…")

        def on_line(line: bytes) -> bool:
            nonlocal done_seen
            text = line.decode("utf-8", errors="replace")
            if text.endswith("\n"):
                text = text[:-1]
            if text.endswith("\r"):
                text = text[:-1]
            if not text.startswith("data:"):
                return False
            data = text[5:].strip()
            if not data:
                return False
            if data == "[DONE]":
                done_seen = True
                return True
            try:
                obj = json.loads(data)
            except ValueError:
                return False
            if isinstance(obj, dict):
                handle_object(obj)
            return False

        response = http.post_json_stream(
            url, payload,
            authorization=f"Bearer {api_key}",
            idle_timeout=LLM_IDLE_TIMEOUT_SECONDS,
            on_line=on_line,
        )

        if response.is_http_ok:
            content = "".join(state_text).strip()
            reasoning = "".join(state_reasoning)
            # 兼容网关忽略 stream=true、直接返回完整 completion JSON。
            if not content:
                payload_json = response.json if isinstance(response.json, dict) else {}
                choices = payload_json.get("choices")
                if isinstance(choices, list) and choices:
                    message = (choices[0] or {}).get("message") or {}
                    content = _content_text(message.get("content")).strip()
                    extra_reasoning = _content_text(message.get("reasoning_content"))
                    if not extra_reasoning:
                        extra_reasoning = _content_text(message.get("reasoning"))
                    if extra_reasoning:
                        reasoning = extra_reasoning
                        if on_thought:
                            on_thought(extra_reasoning)
                    _apply_usage(payload_json.get("usage"), usage_state)
            if usage_state["total_tokens"] <= 0:
                payload_json = response.json if isinstance(response.json, dict) else {}
                _apply_usage(payload_json.get("usage"), usage_state)

            if not content:
                raise LlmError("LLM 未返回最终选片结果。")

            result = parse_selection(content)
            result.reasoning = reasoning
            result.prompt_tokens = usage_state["prompt_tokens"]
            result.completion_tokens = usage_state["completion_tokens"]
            result.total_tokens = usage_state["total_tokens"]
            return result

        last_error = f"LLM 请求失败：{_request_error_message(response)}"
        overloaded = response.status == 429 or 500 <= response.status < 600
        network = not response.network_ok
        if overloaded or network:
            allowed_attempts = 2 if (network and not overloaded) else max_attempts
            if attempt + 1 < allowed_attempts:
                wait_seconds = (2 if attempt == 0 else 5) if overloaded else 2
                if on_status:
                    if response.status == 429:
                        reason = "模型服务繁忙"
                    elif overloaded:
                        reason = "模型服务暂时不可用"
                    else:
                        reason = "请求未完成"
                    on_status(f"{reason}，{wait_seconds} 秒后重试…")
                time.sleep(wait_seconds)
                continue
        raise LlmError(last_error)

    raise LlmError(last_error or "LLM 请求失败。")


# ---------------------------------------------------------------------------
# 音效选点
# ---------------------------------------------------------------------------


def sfx_system_prompt(categories: Sequence[Dict[str, str]]) -> str:
    ids = "、".join(str(index) for index in range(len(categories)))
    lines = "\n".join(
        f"{index} - {category.get('usage_hint', '')}"
        f"（示例音效：{category.get('name', '')}）"
        for index, category in enumerate(categories)
    )
    return (
        "你是短视频字幕包装策划。你的任务是从整片字幕中稀疏挑选少量值得加音效的字幕行，"
        "而不是给每一行都配音效。\n\n"
        f"可选音效类别（category 只能取以下整数之一：{ids}）：\n{lines}\n\n"
        "核心原则：\n"
        "1. 宁少勿多：音效过多会干扰观感；通常 1 分钟成片约 5 处音效即可。\n"
        "2. 只对值得提示的字幕加音效：如关键卖点、数字结论、场景氛围、互动号召等；"
        "普通过渡、闲聊、重复叙述不要加。\n"
        "3. 语义必须匹配 usage_hint：只有字幕内容与某类别的 usage_hint 高度契合时"
        "才选用该 category；没有合适匹配时可以不加任何音效。\n"
        "4. 不要凑数：若整片没有值得加音效的字幕，selections 返回空数组 []。\n\n"
        "输出要求：\n"
        "- 只返回 JSON：{\"selections\":[{\"index\":0,\"category\":2}]}\n"
        "- selections 仅包含需要加音效的字幕；每条含 index（从 0 起）与 category\n"
        "- 同一 index 不要重复出现\n"
        "- 不要输出未在列表中的 category"
    )


def recommended_sfx_count(timeline_end_ms: int) -> int:
    if timeline_end_ms <= 0:
        return 0
    return max(0, min(8, qt_round(timeline_end_ms / 60000.0 * 5.0)))


def build_sfx_user_prompt(segments: Sequence[Dict[str, Any]], *,
                          strict_json: bool = False) -> str:
    prompt = ""
    if strict_json:
        prompt += (
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n"
        )
    prompt += "## 字幕\n"
    for index, segment in enumerate(segments):
        start = int(segment.get("start_ms", 0)) / 1000.0
        end = int(segment.get("end_ms", 0)) / 1000.0
        text = str(segment.get("text", "")).strip()
        prompt += f"[{index}] ({start:.2f}s - {end:.2f}s) {text}\n"

    timeline_end_ms = int(segments[-1].get("end_ms", 0)) if segments else 0
    prompt += (
        "\n## 本次要求\n"
        f"- 本片字幕覆盖约 {timeline_end_ms / 1000.0:.1f} 秒，"
        f"建议音效总数不超过 {recommended_sfx_count(timeline_end_ms)} 处"
        "（可更少或为零）。\n"
        "- 只选择语义与音效类别高度匹配的字幕，宁少勿多。\n"
    )
    return prompt


def _json_int_value(value: Any) -> Optional[int]:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return int(value)
    if isinstance(value, str):
        try:
            return int(value.strip())
        except ValueError:
            return None
    return None


def _try_parse_sfx_object(obj: Dict[str, Any], segment_count: int,
                          category_count: int) -> Optional[List[int]]:
    selections = obj.get("selections")
    if not isinstance(selections, list):
        return None
    result = [-1] * segment_count
    for value in selections:
        if not isinstance(value, dict):
            continue
        index = _json_int_value(value.get("index"))
        category = _json_int_value(value.get("category"))
        if index is None or category is None:
            continue
        if index < 0 or index >= segment_count:
            continue
        if category < 0 or category >= category_count:
            continue
        result[index] = category
    return result


def parse_sfx_selections(raw_content: str, segment_count: int,
                         category_count: int) -> Optional[List[int]]:
    content = _strip_markdown_fence(raw_content)
    if not content:
        return None
    try:
        obj = json.loads(content)
        if isinstance(obj, dict):
            parsed = _try_parse_sfx_object(obj, segment_count, category_count)
            if parsed is not None:
                return parsed
    except ValueError:
        pass
    object_text = _extract_balanced(content, "{", "}")
    if object_text and object_text != content:
        try:
            obj = json.loads(object_text)
        except ValueError:
            return None
        if isinstance(obj, dict):
            return _try_parse_sfx_object(obj, segment_count, category_count)
    return None


def select_sfx(base_url: str, api_key: str,
               segments: Sequence[Dict[str, Any]],
               categories: Sequence[Dict[str, str]],
               *, on_status: StreamCallback = None) -> List[int]:
    """返回与字幕等长的 category 数组；-1 表示不加音效。"""
    if not (api_key or "").strip():
        raise LlmError("尚未配置 API 密钥。")
    segment_count = len(segments)
    category_count = len(categories)
    if segment_count <= 0:
        return []
    if category_count <= 0:
        return [-1] * segment_count

    url = _chat_url(base_url)
    last_error = ""
    strict_json = False

    for attempt in range(2):
        messages = [
            {"role": "system", "content": sfx_system_prompt(categories)},
            {"role": "user", "content": build_sfx_user_prompt(
                segments, strict_json=strict_json)},
        ]
        payload = {
            "model": LLM_MODEL,
            "messages": messages,
            "temperature": 0.2,
            "max_tokens": 4000,
            "stream": False,
        }
        if on_status:
            on_status("正在挑选字幕音效…" if attempt == 0
                      else "音效选点格式异常，正在重试…")

        response = http.post_json(
            url, payload, authorization=f"Bearer {api_key}", timeout=180
        )
        if response.is_http_ok:
            content = ""
            payload_json = response.json if isinstance(response.json, dict) else {}
            choices = payload_json.get("choices")
            if isinstance(choices, list) and choices:
                message = (choices[0] or {}).get("message") or {}
                content = _content_text(message.get("content"))
            if not content.strip():
                last_error = "LLM 未返回音效选点。"
                strict_json = True
                continue
            parsed = parse_sfx_selections(content, segment_count, category_count)
            if parsed is not None:
                return parsed
            last_error = f"无法解析 LLM 返回的音效 JSON：{content[:256]}"
            strict_json = True
            continue

        last_error = f"LLM 音效请求失败：{_request_error_message(response)}"
        overloaded = response.status == 429 or 500 <= response.status < 600
        network = not response.network_ok
        if (overloaded or network) and attempt + 1 < 2:
            if on_status:
                on_status("模型服务繁忙，2 秒后重试…" if overloaded
                          else "请求未完成，2 秒后重试…")
            time.sleep(2)
            continue
        break

    raise LlmError(last_error or "音效选点失败。")


# ---------------------------------------------------------------------------
# 字幕翻译
# ---------------------------------------------------------------------------


def translation_system_prompt() -> str:
    return (
        "你是直播短视频的英文字幕翻译。用户会给出按序号排列的中文字幕，"
        "每条都是按标点和时间切分后的口播片段。\n"
        "请逐条翻译成自然、简洁、适合做视频字幕的英文：\n"
        "- 条数、序号和顺序必须与输入完全一致，不合并、不拆分、不增删。\n"
        "- 每条只翻译对应那一句，尽量简短；数字、品牌名与专有名词保留可读写法。\n"
        "- 不要输出解释、注释或 Markdown 代码块，只输出一个 JSON 对象。"
    )


def build_translation_prompt(texts: Sequence[str], strict_json: bool) -> str:
    prompt = ""
    if strict_json:
        prompt += (
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n"
        )
    prompt += "## 中文字幕\n"
    for index, text in enumerate(texts):
        prompt += f"[{index}] {text.strip()}\n"
    prompt += (
        "\n## 输出格式\n"
        "- 仅输出 JSON 对象："
        "{\"translations\":[{\"index\":0,\"text\":\"英文译文\"}]}\n"
        f"- translations 必须包含全部 {len(texts)} 条，"
        "index 与输入序号一一对应，text 只放英文译文。\n"
    )
    return prompt


def _apply_translation_value(value: Any, index: int, count: int,
                             translations: List[str]) -> int:
    if not isinstance(value, str) or index < 0 or index >= count:
        return 0
    text = " ".join(value.split())
    if not text:
        return 0
    translations[index] = text
    return 1


def _apply_translation_array(array: Sequence[Any], count: int,
                             translations: List[str]) -> int:
    filled = 0
    positional = 0
    for value in array:
        if isinstance(value, dict):
            index = positional
            for key in ("index", "id", "i", "no"):
                index_value = value.get(key)
                parsed = _json_int_value(index_value)
                if parsed is not None:
                    index = parsed
                    break
            text = None
            for key in ("text", "translation", "english", "en", "value"):
                candidate = value.get(key)
                if isinstance(candidate, str):
                    text = candidate
                    break
            filled += _apply_translation_value(text, index, count, translations)
        else:
            filled += _apply_translation_value(value, positional, count, translations)
        positional += 1
    return filled


def _apply_translations_object(obj: Dict[str, Any], count: int,
                               translations: List[str]) -> int:
    for key in ("translations", "items", "data", "results"):
        value = obj.get(key)
        if isinstance(value, list):
            filled = _apply_translation_array(value, count, translations)
            if filled > 0:
                return filled
        elif isinstance(value, dict):
            filled = _apply_translations_object(value, count, translations)
            if filled > 0:
                return filled

    filled = 0
    for key, value in obj.items():
        try:
            index = int(str(key).strip())
        except ValueError:
            continue
        filled += _apply_translation_value(value, index, count, translations)
    return filled


def parse_translations(raw_content: str, count: int) -> Tuple[int, List[str]]:
    translations = [""] * count
    content = _strip_markdown_fence(raw_content)
    if not content:
        return 0, translations

    def fill(document: Any) -> int:
        if isinstance(document, dict):
            return _apply_translations_object(document, count, translations)
        if isinstance(document, list):
            return _apply_translation_array(document, count, translations)
        return 0

    try:
        document = json.loads(content)
        filled = fill(document)
        if filled > 0:
            return filled, translations
    except ValueError:
        pass

    object_text = _extract_balanced(content, "{", "}")
    if object_text and object_text != content:
        try:
            document = json.loads(object_text)
            filled = fill(document)
            if filled > 0:
                return filled, translations
        except ValueError:
            pass

    array_text = _extract_balanced(content, "[", "]")
    if array_text:
        try:
            document = json.loads(array_text)
            filled = fill(document)
            if filled > 0:
                return filled, translations
        except ValueError:
            pass
    return 0, translations


def translate_captions(base_url: str, api_key: str, texts: Sequence[str],
                       *, on_status: StreamCallback = None) -> List[str]:
    """批量翻译字幕；返回与输入等长的列表，缺失项为空字符串。"""
    if not (api_key or "").strip():
        raise LlmError("尚未配置 API 密钥。")
    if not texts:
        return []
    url = _chat_url(base_url)
    max_tokens = max(2000, min(8000, len(texts) * 160))
    last_error = ""
    strict_json = False

    for attempt in range(3):
        messages = [
            {"role": "system", "content": translation_system_prompt()},
            {"role": "user", "content": build_translation_prompt(
                texts, strict_json)},
        ]
        payload = {
            "model": LLM_MODEL,
            "messages": messages,
            "temperature": 0.1,
            "max_tokens": max_tokens,
            "stream": False,
        }
        if on_status:
            on_status("正在生成英文译文…" if attempt == 0
                      else "英文译文格式异常，正在重试…")

        response = http.post_json(
            url, payload, authorization=f"Bearer {api_key}", timeout=180
        )
        if response.is_http_ok:
            content = ""
            payload_json = response.json if isinstance(response.json, dict) else {}
            choices = payload_json.get("choices")
            if isinstance(choices, list) and choices:
                message = (choices[0] or {}).get("message") or {}
                content = _content_text(message.get("content"))
            if not content.strip():
                last_error = "LLM 未返回英文译文。"
                strict_json = True
                continue
            filled, translations = parse_translations(content, len(texts))
            if filled > 0:
                return translations
            last_error = f"无法解析 LLM 返回的译文 JSON：{content[:256]}"
            strict_json = True
            continue

        last_error = f"LLM 翻译请求失败：{_request_error_message(response)}"
        overloaded = response.status == 429 or 500 <= response.status < 600
        network = not response.network_ok
        if overloaded or network:
            allowed_attempts = 2 if (network and not overloaded) else 3
            if attempt + 1 < allowed_attempts:
                wait_seconds = (2 if attempt == 0 else 5) if overloaded else 2
                if on_status:
                    if response.status == 429:
                        reason = "模型服务繁忙"
                    elif overloaded:
                        reason = "模型服务暂时不可用"
                    else:
                        reason = "请求未完成"
                    on_status(f"{reason}，{wait_seconds} 秒后重试…")
                time.sleep(wait_seconds)
                continue
        break

    raise LlmError(last_error or "字幕翻译失败。")
