"""豆包语音识别（OpenAI 兼容 /audio/transcriptions）。

与 LiveMix/src/api/AsrClient.cpp 对齐：
- 模型 volc.seedasr.auc；
- language=zh，response_format=verbose_json；
- timestamp_granularities[]=word 请求逐词时间线（网关不支持时忽略）；
- 只接受公网 URL（file_url），超时 35 分钟。
"""

from __future__ import annotations

from typing import Any, Dict, List

import http_client as http
from util import qt_round, utf16_len

ASR_MODEL = "volc.seedasr.auc"
ASR_TIMEOUT_SECONDS = 35 * 60


def _parse_speaker(segment: Dict[str, Any]) -> str:
    for key in ("speaker", "speaker_id", "speaker_label"):
        value = segment.get(key)
        if isinstance(value, str):
            text = value.strip()
            if text:
                return text
        elif isinstance(value, (int, float)) and not isinstance(value, bool):
            return str(int(value))
    return ""


def _parse_confidence(segment: Dict[str, Any]) -> float:
    if "confidence" not in segment:
        return -1.0
    value = segment.get("confidence")
    try:
        confidence = float(value)
    except (TypeError, ValueError):
        return -1.0
    if 0.0 <= confidence <= 1.0:
        confidence *= 100.0
    return confidence if 0.0 <= confidence <= 100.0 else -1.0


def transcript_from_verbose_json(payload: Dict[str, Any]) -> Dict[str, Any]:
    """对应 AsrTranscript::fromOpenAiVerboseJson。时间单位秒 → 毫秒。"""
    transcript: Dict[str, Any] = {"text": str(payload.get("text", "")).strip(),
                                  "utterances": []}
    segments = payload.get("segments")
    if not isinstance(segments, list):
        segments = []

    for segment in segments:
        if not isinstance(segment, dict):
            continue
        text = str(segment.get("text", "")).strip()
        try:
            start_ms = qt_round(float(segment.get("start", 0.0)) * 1000.0)
            end_ms = qt_round(float(segment.get("end", 0.0)) * 1000.0)
        except (TypeError, ValueError):
            start_ms = 0
            end_ms = 0
        if not text or end_ms <= start_ms:
            continue

        words: List[Dict[str, Any]] = []
        raw_words = segment.get("words")
        if isinstance(raw_words, list):
            for raw in raw_words:
                if not isinstance(raw, dict):
                    continue
                word_text = str(raw.get("word", "") or raw.get("text", ""))
                try:
                    word_start = qt_round(float(raw.get("start", 0.0)) * 1000.0)
                    word_end = qt_round(float(raw.get("end", 0.0)) * 1000.0)
                except (TypeError, ValueError):
                    continue
                if not word_text or word_start < 0 or word_end <= word_start:
                    continue
                words.append({
                    "text": word_text,
                    "start_ms": word_start,
                    "end_ms": word_end,
                })

        transcript["utterances"].append({
            "text": text,
            "start_ms": start_ms,
            "end_ms": end_ms,
            "words": words,
            "speaker": _parse_speaker(segment),
            "confidence": _parse_confidence(segment),
        })

    if not transcript["utterances"] and transcript["text"]:
        try:
            duration_ms = qt_round(float(payload.get("duration", 0.0)) * 1000.0)
        except (TypeError, ValueError):
            duration_ms = 0
        transcript["utterances"].append({
            "text": transcript["text"],
            "start_ms": 0,
            "end_ms": duration_ms,
            "words": [],
            "speaker": "",
            "confidence": -1.0,
        })
    return transcript


def to_cache_json(transcript: Dict[str, Any]) -> Dict[str, Any]:
    """转换成客户端 AsrTranscript::toJson 的缓存格式。

    词级时间线字段为 start_time / end_time，便于与客户端共用 asr_results。
    """
    utterances = []
    for utterance in transcript.get("utterances") or []:
        words = []
        for word in utterance.get("words") or []:
            words.append({
                "text": str(word.get("text", "")),
                "start_time": int(word.get("start_ms", 0)),
                "end_time": int(word.get("end_ms", 0)),
            })
        item: Dict[str, Any] = {
            "text": str(utterance.get("text", "")),
            "start_ms": int(utterance.get("start_ms", 0)),
            "end_ms": int(utterance.get("end_ms", 0)),
            "words": words,
        }
        speaker = str(utterance.get("speaker", "")).strip()
        if speaker:
            item["speaker"] = speaker
        try:
            confidence = float(utterance.get("confidence", -1.0))
        except (TypeError, ValueError):
            confidence = -1.0
        if confidence >= 0.0:
            item["confidence"] = confidence
        utterances.append(item)
    return {
        "text": str(transcript.get("text", "")),
        "utterances": utterances,
    }


def from_cache_json(payload: Dict[str, Any]) -> Dict[str, Any]:
    """读取客户端缓存格式；同时兼容技能自己写出的 start_ms / end_ms。"""
    transcript: Dict[str, Any] = {
        "text": str(payload.get("text", "")),
        "utterances": [],
    }
    for utterance in payload.get("utterances") or []:
        words = []
        for word in utterance.get("words") or []:
            word_text = str(word.get("text", ""))
            try:
                start_ms = int(
                    word.get("start_ms", word.get("start_time", 0)) or 0
                )
                end_ms = int(
                    word.get("end_ms", word.get("end_time", 0)) or 0
                )
            except (TypeError, ValueError):
                start_ms, end_ms = 0, 0
            if not word_text or start_ms < 0 or end_ms <= start_ms:
                continue
            words.append({
                "text": word_text,
                "start_ms": start_ms,
                "end_ms": end_ms,
            })
        try:
            start_ms = int(utterance.get("start_ms", 0) or 0)
            end_ms = int(utterance.get("end_ms", 0) or 0)
        except (TypeError, ValueError):
            start_ms, end_ms = 0, 0
        if end_ms <= start_ms and words:
            start_ms = words[0]["start_ms"]
            end_ms = words[-1]["end_ms"]
        if end_ms <= start_ms and transcript["utterances"]:
            # 没有任何时间线时退化为紧随上一句，避免时间线出现空洞。
            start_ms = int(transcript["utterances"][-1]["end_ms"])
            end_ms = start_ms
        text = str(utterance.get("text", ""))
        if not text.strip():
            continue
        try:
            confidence = float(utterance.get("confidence", -1.0))
        except (TypeError, ValueError):
            confidence = -1.0
        transcript["utterances"].append({
            "text": text,
            "start_ms": start_ms,
            "end_ms": end_ms,
            "words": words,
            "speaker": str(utterance.get("speaker", "")).strip(),
            "confidence": confidence,
        })
    return transcript


def transcript_duration_ms(transcript: Dict[str, Any]) -> int:
    utterances = transcript.get("utterances") or []
    if not utterances:
        return 0
    return int(utterances[-1].get("end_ms", 0))


def transcript_char_count(transcript: Dict[str, Any]) -> int:
    return sum(utf16_len(str(u.get("text", "")))
               for u in transcript.get("utterances") or [])


def speaker_count(transcript: Dict[str, Any]) -> int:
    speakers = {
        str(u.get("speaker", "")).strip()
        for u in transcript.get("utterances") or []
        if str(u.get("speaker", "")).strip()
    }
    return len(speakers)


def average_confidence(transcript: Dict[str, Any]) -> float:
    values = [
        float(u.get("confidence", -1.0))
        for u in transcript.get("utterances") or []
        if float(u.get("confidence", -1.0)) >= 0.0
    ]
    return sum(values) / len(values) if values else -1.0


def _error_message(response: http.HttpResponse) -> str:
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


def transcribe(audio_url: str, gateway_base_url: str, api_key: str,
               *, timeout: float = ASR_TIMEOUT_SECONDS) -> Dict[str, Any]:
    """提交整片识别任务并返回转写 JSON。

    网关为同步接口：请求返回时一次性拿到完整 verbose_json。
    """
    if not audio_url.strip() or not api_key.strip():
        raise ValueError("ASR 需要音频 URL 与 API 密钥。")
    base = gateway_base_url.strip().rstrip("/")
    if not base:
        raise ValueError("API 地址为空。")
    url = base + "/audio/transcriptions"

    response = http.post_multipart(
        url,
        [
            ("file_url", audio_url),
            ("model", ASR_MODEL),
            ("language", "zh"),
            ("response_format", "verbose_json"),
            ("timestamp_granularities[]", "word"),
        ],
        authorization=f"Bearer {api_key}",
        timeout=timeout,
    )

    if not response.is_http_ok or (
        isinstance(response.json, dict) and "error" in response.json
    ):
        raise RuntimeError(f"ASR 识别失败：{_error_message(response)}")

    if not isinstance(response.json, dict):
        raise RuntimeError("ASR 返回格式异常。")

    transcript = transcript_from_verbose_json(response.json)
    if not transcript["utterances"] and "text" not in response.json:
        raise RuntimeError("ASR 返回格式异常。")
    return transcript
