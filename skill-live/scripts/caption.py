"""字幕断句、词级时间对齐与字数均衡折行。

逐行移植 LiveMix/src/services/CaptionTiming.cpp：
- 按中英文标点拆句，但数字中的小数点/冒号/连字符不拆；
- 单条最多 15 个字，超长句段均衡切成多条（33 字 → 11/11/11）；
- 英文单词与数字（100、100%、29.3、hello、ok）不可从中间截断；
- 优先使用 ASR 词级时间线；任何一处对不上就整句退回按字数分摊。
"""

from __future__ import annotations

import unicodedata
from dataclasses import dataclass
from typing import Any, Dict, List, Optional, Tuple

from util import utf16_len

CAPTION_MAX_CHARS = 15

_CAPTION_BREAKS = "，。！？；：、,.!?;:…—\n"
_NUMERIC_SEPARATORS = ".,:：，-"
_NUMERIC_CONNECTORS = ".,:/，-／"


@dataclass
class CaptionClause:
    display_text: str
    alignment_text: str


@dataclass
class WrapUnit:
    text: str
    chars: int
    space_before: bool = False


def is_punctuation(ch: str) -> bool:
    """QChar 的标点分类（Connector/Dash/Open/Close/Quote/Other）。"""
    return unicodedata.category(ch).startswith("P")


def is_caption_break(ch: str) -> bool:
    return ch in _CAPTION_BREAKS


def is_numeric_separator(text: str, index: int) -> bool:
    if index <= 0 or index + 1 >= len(text):
        return False
    return (
        text[index] in _NUMERIC_SEPARATORS
        and text[index - 1].isdigit()
        and text[index + 1].isdigit()
    )


def strip_caption_punctuation(text: str) -> str:
    """保留文字与英文单词间的空格，去掉其它标点。"""
    cleaned: List[str] = []
    for index, ch in enumerate(text):
        if ch.isspace():
            if cleaned and cleaned[-1] != " ":
                cleaned.append(" ")
            continue
        if is_punctuation(ch) and not is_numeric_separator(text, index):
            continue
        cleaned.append(ch)
    return "".join(cleaned).strip()


def strip_alignment_text(display_text: str) -> str:
    return "".join(ch for ch in display_text if not ch.isspace())


def is_cjk_or_kana(ch: str) -> bool:
    code = ord(ch)
    return (
        0x3040 <= code <= 0x30FF     # 日文假名
        or 0x3400 <= code <= 0x4DBF  # 中文扩展 A
        or 0x4E00 <= code <= 0x9FFF  # 中文基本区
        or 0xAC00 <= code <= 0xD7AF  # 韩文
        or 0xF900 <= code <= 0xFAFF  # 兼容汉字
    )


def is_word_character(ch: str) -> bool:
    return (not is_cjk_or_kana(ch)) and unicodedata.category(ch)[0] in ("L", "N")


def is_percent_sign(ch: str) -> bool:
    return ch in ("%", "％")


def is_numeric_connector(ch: str) -> bool:
    return ch in _NUMERIC_CONNECTORS


def tokenize_for_wrapping(text: str) -> List[WrapUnit]:
    units: List[WrapUnit] = []
    index = 0
    pending_space = False
    while index < len(text):
        ch = text[index]
        if ch.isspace():
            pending_space = bool(units)
            index += 1
            continue

        unit = WrapUnit(text="", chars=0,
                        space_before=pending_space and bool(units))
        pending_space = False

        if ord(ch) > 0xFFFF:
            unit.text = ch
            index += 1
        elif is_word_character(ch):
            start = index
            index += 1
            while index < len(text):
                nxt = text[index]
                if is_word_character(nxt):
                    index += 1
                    continue
                if (is_percent_sign(nxt) and index > start
                        and text[index - 1].isdigit()):
                    index += 1
                    continue
                if (is_numeric_connector(nxt) and index > start
                        and index + 1 < len(text)
                        and text[index - 1].isdigit()
                        and text[index + 1].isdigit()):
                    index += 1
                    continue
                break
            unit.text = text[start:index]
        else:
            unit.text = ch
            index += 1
        unit.chars = utf16_len(unit.text)
        units.append(unit)
    return units


def split_text_balanced(text: str, max_chars_per_caption: int) -> List[str]:
    """把一段文字按字数均衡拆成多条，英文单词/数字保持完整。"""
    if max_chars_per_caption <= 0 or not text:
        return [text.strip()]
    units = tokenize_for_wrapping(text)
    total_chars = sum(unit.chars for unit in units)
    if total_chars <= max_chars_per_caption or not units:
        return [text.strip()]

    pieces: List[str] = []
    unit_index = 0
    remaining_chars = total_chars
    while unit_index < len(units):
        pieces_left = max(
            1, (remaining_chars + max_chars_per_caption - 1) // max_chars_per_caption
        )
        target = (remaining_chars + pieces_left - 1) // pieces_left

        piece = ""
        piece_chars = 0
        while unit_index < len(units):
            unit = units[unit_index]
            if piece and piece_chars + unit.chars > target:
                break
            if piece and unit.space_before:
                piece += " "
            piece += unit.text
            piece_chars += unit.chars
            unit_index += 1
        pieces.append(piece)
        remaining_chars -= piece_chars
    return pieces


def split_clauses(text: str, max_chars_per_caption: int) -> List[CaptionClause]:
    clauses: List[CaptionClause] = []
    if not text:
        return clauses
    clause_start = 0
    for index, ch in enumerate(text):
        boundary = index + 1 == len(text)
        if not boundary and is_caption_break(ch):
            boundary = not is_numeric_separator(text, index)
        if not boundary:
            continue

        clause_end = index + 1
        display = strip_caption_punctuation(text[clause_start:clause_end])
        for piece in split_text_balanced(display, max_chars_per_caption):
            if not piece:
                continue
            clauses.append(CaptionClause(
                display_text=piece,
                alignment_text=strip_alignment_text(piece),
            ))
        clause_start = clause_end
    return clauses


def _word_valid(word: Dict[str, Any]) -> bool:
    try:
        return int(word.get("start_ms", 0)) >= 0 and int(
            word.get("end_ms", 0)
        ) > int(word.get("start_ms", 0))
    except (TypeError, ValueError):
        return False


def _interpolated_start(word: Dict[str, Any], char_offset: int) -> int:
    text = str(word.get("text", ""))
    length = utf16_len(text)
    start = int(word.get("start_ms", 0))
    end = int(word.get("end_ms", 0))
    if length <= 1:
        return start
    offset16 = utf16_len(text[:char_offset])
    return start + (end - start) * offset16 // length


def _interpolated_end(word: Dict[str, Any], char_offset: int) -> int:
    text = str(word.get("text", ""))
    length = utf16_len(text)
    start = int(word.get("start_ms", 0))
    end = int(word.get("end_ms", 0))
    if length <= 0:
        return end
    offset16 = utf16_len(text[: char_offset + 1])
    if offset16 >= length:
        return end
    return start + (end - start) * offset16 // length


def _is_word_filler(ch: str) -> bool:
    return ch.isspace() or is_punctuation(ch)


class WordCursor:
    def __init__(self, words: List[Dict[str, Any]]) -> None:
        self.words = words
        self.word_index = 0
        self.char_offset = 0

    def consume(self, need: str) -> Optional[Tuple[int, int]]:
        if not need:
            return None
        has_start = False
        start = 0
        end = 0
        for ch in need:
            matched = False
            while self.word_index < len(self.words):
                word = self.words[self.word_index]
                text = str(word.get("text", ""))
                if not text or self.char_offset >= len(text):
                    self.word_index += 1
                    self.char_offset = 0
                    continue

                word_char = text[self.char_offset]
                if word_char == ch:
                    if _word_valid(word):
                        char_start = _interpolated_start(word, self.char_offset)
                        char_end = _interpolated_end(word, self.char_offset)
                        if not has_start:
                            start = char_start
                            has_start = True
                        end = char_end
                    self.char_offset += 1
                    matched = True
                    break
                if _is_word_filler(word_char):
                    self.char_offset += 1
                    continue
                # 真正的文字对不上：词级时间线不可信，整句退回按长度分摊。
                return None
            if not matched:
                return None
        if not has_start or end <= start:
            return None
        return start, end


def _assign_word_times(utterance: Dict[str, Any],
                       clauses: List[CaptionClause]) -> Optional[List[Dict[str, Any]]]:
    words = utterance.get("words") or []
    cursor = WordCursor(words)
    result: List[Dict[str, Any]] = []
    utt_start = int(utterance.get("start_ms", 0))
    utt_end = int(utterance.get("end_ms", 0))
    previous_end = utt_start

    for clause in clauses:
        if not clause.alignment_text:
            continue
        consumed = cursor.consume(clause.alignment_text)
        if consumed is None:
            return None
        start, end = consumed
        start = max(utt_start, min(start, utt_end))
        end = max(utt_start, min(end, utt_end))
        start = max(start, previous_end)
        if end <= start:
            return None
        result.append({
            "text": clause.display_text,
            "start_ms": start,
            "end_ms": end,
        })
        previous_end = end

    return result or None


def _assign_proportional_times(utterance: Dict[str, Any],
                               clauses: List[CaptionClause]) -> List[Dict[str, Any]]:
    weights = [max(1, utf16_len(clause.alignment_text)) for clause in clauses]
    total_weight = sum(weights)
    if total_weight <= 0:
        return []

    utt_start = int(utterance.get("start_ms", 0))
    utt_end = int(utterance.get("end_ms", 0))
    duration = utt_end - utt_start
    consumed = 0
    result: List[Dict[str, Any]] = []
    for clause, weight in zip(clauses, weights):
        start = utt_start + duration * consumed // total_weight
        consumed += weight
        end = utt_start + duration * consumed // total_weight
        if end <= start:
            end = min(utt_end, start + 1)
        if end > start:
            result.append({
                "text": clause.display_text,
                "start_ms": start,
                "end_ms": end,
            })
    return result


def split_utterance(utterance: Dict[str, Any],
                    max_chars_per_caption: int = CAPTION_MAX_CHARS
                    ) -> List[Dict[str, Any]]:
    """把一条 ASR 句段拆成字幕，并给出每条字幕的绝对起止时间。"""
    text = str(utterance.get("text", ""))
    try:
        start_ms = int(utterance.get("start_ms", 0))
        end_ms = int(utterance.get("end_ms", 0))
    except (TypeError, ValueError):
        return []
    if not text or end_ms <= start_ms:
        return []

    clauses = split_clauses(text, max_chars_per_caption)
    if not clauses:
        return []

    words = utterance.get("words") or []
    if words:
        captions = _assign_word_times(utterance, clauses)
        if captions:
            return captions
        # 与客户端一致：任何一处对不上就整句退回按长度分摊。
    return _assign_proportional_times(utterance, clauses)


def contains_cjk_character(text: str) -> bool:
    for ch in text:
        code = ord(ch)
        if (0x3400 <= code <= 0x4DBF
                or 0x4E00 <= code <= 0x9FFF
                or 0xF900 <= code <= 0xFAFF):
            return True
    return False


def english_caption_transform_y(chinese_style: Dict[str, Any],
                                english_font_size: int,
                                chinese_lines: int,
                                canvas_height: int) -> float:
    """英文轨跟随中文字幕：按中文行数估算位置（与客户端公式一致）。"""
    chinese_line_px = float(chinese_style.get("font_size", 15)) * 5.0
    english_line_px = float(english_font_size) * 5.0
    chinese_center_px = -float(chinese_style.get("transform_y", -800.0)) / 2.0
    english_center_px = (
        chinese_center_px
        + chinese_line_px * chinese_lines / 2.0
        + 25.0
        + english_line_px / 2.0
    )
    max_center_px = canvas_height / 2.0 - 10.0 - english_line_px / 2.0
    english_center_px = min(english_center_px, max_center_px)
    return -2.0 * english_center_px
