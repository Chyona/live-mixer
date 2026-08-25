import type { AsrParagraphs, AsrSummary, LiveAsrSegment } from '~/services/sourceVideo.model';
import type {
  AiSegment,
  SelectedCopySegment,
  TranscriptParagraph,
  TranscriptSegment,
  TranscriptWord,
} from './types';
import { speakerColors } from '~/style/semanticColors';

export const SPEAKER_COLORS = [...speakerColors];

export function getSpeakerColor(speakerId: string, speakerIds: string[]) {
  const index = speakerIds.indexOf(speakerId);
  return SPEAKER_COLORS[index >= 0 ? index % SPEAKER_COLORS.length : 0];
}

export function formatSliceTime(seconds: number): string {
  const totalTenths = Math.round(Math.max(0, seconds) * 10);
  const tenths = totalTenths % 10;
  const totalSecs = Math.floor(totalTenths / 10);
  const secs = totalSecs % 60;
  const totalMins = Math.floor(totalSecs / 60);
  const mins = totalMins % 60;
  const hrs = Math.floor(totalMins / 60);

  // 进位：.round 后 tenths 已是 0-9
  const pad2 = (n: number) => String(n).padStart(2, '0');
  const clock =
    hrs > 0 ? `${hrs}:${pad2(mins)}:${pad2(secs)}` : `${mins}:${pad2(secs)}`;

  return tenths > 0 ? `${clock}.${tenths}` : clock;
}

export function getParagraphText(paragraph: TranscriptParagraph) {
  return paragraph.segments.map((segment) => segment.text).join('');
}

export function getParagraphRange(paragraph: TranscriptParagraph) {
  if (!paragraph.segments.length) {
    return { start: 0, end: 0 };
  }

  const firstSegment = paragraph.segments[0];
  const lastSegment = paragraph.segments[paragraph.segments.length - 1];
  if (!firstSegment || !lastSegment) {
    return { start: 0, end: 0 };
  }

  return {
    start: firstSegment.start,
    end: lastSegment.end,
  };
}

/**
 * 按字符区间切出对应 words。
 * 传入 sourceText 时按完整原文对齐（words 常不含标点）；否则退回「words 顺序拼接」假设。
 */
export function sliceWordsByCharRange(
  words: TranscriptWord[],
  charStart: number,
  charEnd: number,
  sourceText?: string
): TranscriptWord[] {
  if (!words.length || charEnd <= charStart) return [];

  const picked: TranscriptWord[] = [];

  if (sourceText) {
    let textIndex = 0;
    for (const word of words) {
      const wordText = word.text;
      if (!wordText) continue;
      const found = sourceText.indexOf(wordText, textIndex);
      if (found === -1) continue;
      const wStart = found;
      const wEnd = found + wordText.length;
      textIndex = wEnd;
      if (wEnd <= charStart || wStart >= charEnd) continue;
      picked.push(word);
    }
    return picked;
  }

  let cursor = 0;
  for (const word of words) {
    const len = Math.max(word.text.length, 1);
    const wStart = cursor;
    const wEnd = cursor + len;
    cursor = wEnd;
    if (wEnd <= charStart || wStart >= charEnd) continue;
    picked.push(word);
  }
  return picked;
}

export type TranscriptTextToken = {
  text: string;
  start: number;
  end: number;
};

/**
 * 将 words 对齐回完整句段文本：ASR words 常不含标点，选片高亮时若只渲染 words 会丢「，。」等。
 * 以 segment.text 为准，把 words 之间的空隙（标点）补成独立 token。
 */
export function alignWordsToTranscriptText(
  text: string,
  words: TranscriptWord[]
): TranscriptTextToken[] {
  if (!text) return [];
  if (!words.length) {
    return [{ text, start: 0, end: 0 }];
  }

  const tokens: TranscriptTextToken[] = [];
  let textIndex = 0;

  for (const word of words) {
    const wordText = word.text;
    if (!wordText) continue;

    let found = text.indexOf(wordText, textIndex);
    if (found === -1 && text.startsWith(wordText, textIndex)) {
      found = textIndex;
    }
    if (found === -1) {
      // 无法对齐时仍输出该 word，避免整段丢失
      tokens.push({ text: wordText, start: word.start, end: word.end });
      continue;
    }

    if (found > textIndex) {
      const gap = text.slice(textIndex, found);
      // 空隙（多为标点）挂到当前字起点，便于选中态一并高亮
      tokens.push({ text: gap, start: word.start, end: word.start });
    }

    tokens.push({ text: wordText, start: word.start, end: word.end });
    textIndex = found + wordText.length;
  }

  if (textIndex < text.length) {
    const last = words[words.length - 1];
    const tailTime = last ? Math.max(last.end, last.start) : 0;
    tokens.push({
      text: text.slice(textIndex),
      start: tailTime,
      end: tailTime,
    });
  }

  return tokens;
}

function rangeFromWords(
  words: TranscriptWord[],
  fallbackStart: number,
  fallbackEnd: number
): { start: number; end: number } {
  if (!words.length) return { start: fallbackStart, end: fallbackEnd };
  const first = words[0];
  if (!first) return { start: fallbackStart, end: fallbackEnd };
  // 取全部 words 的最大 end，避免末尾零时长字（标点）导致区间偏短
  const end = words.reduce(
    (max, word) => Math.max(max, word.end, word.start),
    first.start
  );
  return {
    start: first.start,
    end,
  };
}

const CLAUSE_SPLIT_RE = /(?<=[，。！？；])/g;

export function splitTextToSegments(
  text: string,
  start: number,
  end: number,
  idPrefix: string,
  words?: TranscriptWord[]
): TranscriptSegment[] {
  const parts = text.split(CLAUSE_SPLIT_RE).filter(Boolean);
  if (parts.length <= 1) {
    return [{ id: idPrefix, start, end, text, words: words?.length ? words : undefined }];
  }

  const duration = end - start;
  let cursor = start;
  let charOffset = 0;

  return parts.map((part, index) => {
    const partCharStart = charOffset;
    const partCharEnd = charOffset + part.length;
    charOffset = partCharEnd;

    const partWords = words?.length
      ? sliceWordsByCharRange(words, partCharStart, partCharEnd, text)
      : [];
    const fromWords = rangeFromWords(partWords, -1, -1);

    let segmentStart: number;
    let segmentEnd: number;
    if (partWords.length && fromWords.start >= 0) {
      segmentStart = fromWords.start;
      segmentEnd = fromWords.end;
    } else {
      // 无字级时间时退回按字数比例插值
      const ratio = part.length / text.length;
      segmentStart = cursor;
      segmentEnd = index === parts.length - 1 ? end : cursor + duration * ratio;
    }

    if (index === parts.length - 1) {
      segmentEnd = Math.max(segmentEnd, end);
    }

    const segment: TranscriptSegment = {
      id: `${idPrefix}-${index}`,
      start: segmentStart,
      end: Math.max(segmentEnd, segmentStart),
      text: part,
      words: partWords.length ? partWords : undefined,
    };
    cursor = segment.end;
    return segment;
  });
}

export function normalizeParagraphSegments(paragraph: TranscriptParagraph): TranscriptParagraph {
  if (paragraph.segments.length > 1) {
    return {
      ...paragraph,
      segments: paragraph.segments.flatMap((segment) =>
        splitTextToSegments(segment.text, segment.start, segment.end, segment.id, segment.words)
      ),
    };
  }

  const only = paragraph.segments[0];
  if (!only) return paragraph;

  return {
    ...paragraph,
    segments: splitTextToSegments(only.text, only.start, only.end, only.id, only.words),
  };
}

export function normalizeTranscriptParagraphs(paragraphs: TranscriptParagraph[]) {
  return paragraphs.map(normalizeParagraphSegments);
}

function msToSeconds(ms: number) {
  return ms / 1000;
}

function formatSpeakerName(speaker: string) {
  const trimmed = speaker.trim();
  if (!trimmed) return '说话人';
  if (/^\d+$/.test(trimmed)) return `说话人${trimmed}`;
  return trimmed;
}

function asrParagraphToTranscript(item: LiveAsrSegment, index: number): TranscriptParagraph {
  const speakerId = String(item.speaker ?? '').trim() || '0';
  const words: TranscriptWord[] = (item.words ?? [])
    .map((word) => ({
      start: msToSeconds(Number(word.start_time)),
      end: msToSeconds(Number(word.end_time)),
      text: String(word.text ?? ''),
    }))
    .filter((word) => word.text.length > 0 && Number.isFinite(word.start) && Number.isFinite(word.end));

  const text = item.text || words.map((word) => word.text).join('');

  return {
    id: `asr-p-${index}`,
    speakerId,
    speakerName: formatSpeakerName(speakerId),
    // 句级入参 + 字级 words；随后由 normalize 按标点拆分并继承字级时间
    segments: [
      {
        id: `asr-${index}`,
        start: msToSeconds(item.start_time),
        end: msToSeconds(item.end_time),
        text,
        words: words.length ? words : undefined,
      },
    ],
  };
}

/** 将详情接口 `asr_paragraphs`（ms）转为剪辑页内部段落结构（秒） */
export function asrParagraphsToTranscriptParagraphs(
  paragraphs: AsrParagraphs | null | undefined
): TranscriptParagraph[] {
  if (!paragraphs?.length) return [];
  return paragraphs.map(asrParagraphToTranscript);
}

/** 将详情接口 `asr_summaries`（ms）转为 AI 分段（秒），与智能选片时间轴一致 */
export function asrSummariesToAiSegments(
  summaries: AsrSummary[] | null | undefined
): AiSegment[] {
  if (!summaries?.length) return [];
  return summaries.map((item, index) => {
    const start = (item.start_time ?? 0) / 1000;
    const end = (item.end_time ?? 0) / 1000;
    return {
      id: `ai-seg-${index}-${Math.round(start * 1000)}-${Math.round(end * 1000)}`,
      title: item.title?.trim() || `片段 ${index + 1}`,
      start,
      end,
    };
  });
}

/**
 * 按 AI 分段时间范围从文案分段抽取文案，生成可加入「文案预览」的片段。
 * 取与 [start, end] 有重叠的句段，合并为一段。
 */
export function buildCopySegmentFromAiSegment(
  paragraphs: TranscriptParagraph[],
  aiSegment: AiSegment
): SelectedCopySegment | null {
  const overlapped: Array<{
    text: string;
    start: number;
    end: number;
    speakerId: string;
    speakerName: string;
  }> = [];

  for (const paragraph of paragraphs) {
    for (const segment of paragraph.segments) {
      if (segment.end <= aiSegment.start || segment.start >= aiSegment.end) continue;
      const text = segment.text.trim();
      if (!text) continue;
      overlapped.push({
        text,
        start: segment.start,
        end: segment.end,
        speakerId: paragraph.speakerId,
        speakerName: paragraph.speakerName,
      });
    }
  }

  if (!overlapped.length) return null;

  const start = Math.min(...overlapped.map((item) => item.start));
  const end = Math.max(...overlapped.map((item) => item.end));
  const first = overlapped[0]!;

  return {
    id: `copy-ai-${aiSegment.id}-${Date.now()}`,
    speakerId: first.speakerId,
    speakerName: first.speakerName,
    text: overlapped.map((item) => item.text).join(''),
    start,
    end,
    originStart: start,
    originEnd: end,
  };
}

/** 扁平化全部字级时间轴（按开始时间排序） */
export function flattenTranscriptWords(paragraphs: TranscriptParagraph[]): TranscriptWord[] {
  return paragraphs
    .flatMap((paragraph) => paragraph.segments.flatMap((segment) => segment.words ?? []))
    .slice()
    .sort((a, b) => a.start - b.start || a.end - b.end);
}

/** 文案分段中的原始文字时间轴：优先字级 words，否则用句段 */
function flattenTranscriptSpokenRanges(
  paragraphs: TranscriptParagraph[]
): Array<{ start: number; end: number }> {
  const words = flattenTranscriptWords(paragraphs);
  if (words.length > 0) {
    return words
      .filter((word) => Number.isFinite(word.start) && Number.isFinite(word.end) && word.end > word.start)
      .map((word) => ({ start: word.start, end: word.end }));
  }

  return paragraphs
    .flatMap((paragraph) => paragraph.segments)
    .filter(
      (segment) =>
        Number.isFinite(segment.start) && Number.isFinite(segment.end) && segment.end > segment.start
    )
    .map((segment) => ({ start: segment.start, end: segment.end }))
    .sort((a, b) => a.start - b.start || a.end - b.end);
}

/**
 * 按文案分段原始数据，计算当前选段前后可扩留白的边界。
 * - lowerBound：前方最近一段原始文字的结束时间（无则 0）
 * - upperBound：后方最近一段原始文字的开始时间（无则视频时长）
 */
export function getTranscriptPadBounds(
  paragraphs: TranscriptParagraph[],
  originStart: number,
  originEnd: number,
  videoDuration: number
): { lowerBound: number; upperBound: number } {
  const EPS = 1e-3;
  const ranges = flattenTranscriptSpokenRanges(paragraphs);
  let lowerBound = 0;
  let upperBound =
    Number.isFinite(videoDuration) && videoDuration > 0 ? videoDuration : Number.POSITIVE_INFINITY;

  for (const range of ranges) {
    // 完全落在选段原文之前 → 可作为前方文字终点
    if (range.end <= originStart + EPS) {
      lowerBound = Math.max(lowerBound, range.end);
      continue;
    }
    // 完全落在选段原文之后 → 可作为后方文字起点
    if (range.start >= originEnd - EPS) {
      upperBound = Math.min(upperBound, range.start);
    }
  }

  if (lowerBound > originStart) lowerBound = 0;
  if (upperBound < originEnd) {
    upperBound =
      Number.isFinite(videoDuration) && videoDuration > 0 ? videoDuration : Number.POSITIVE_INFINITY;
  }

  return { lowerBound, upperBound };
}

export function findActiveSegment(
  paragraphs: TranscriptParagraph[],
  currentTime: number
): { paragraphId: string; segmentId: string } | null {
  for (const paragraph of paragraphs) {
    for (const segment of paragraph.segments) {
      if (currentTime >= segment.start && currentTime < segment.end) {
        return { paragraphId: paragraph.id, segmentId: segment.id };
      }
    }
  }
  return null;
}

export type TranscriptHighlightMode = 'playback';

export interface TranscriptHighlight {
  paragraphId: string;
  segmentIds: string[];
  mode: TranscriptHighlightMode;
}

export function paragraphSelectionToCopySegment(
  container: HTMLElement,
  paragraph: TranscriptParagraph
): SelectedCopySegment | null {
  const offsets = getTextSelectionOffsets(container);
  if (!offsets) return null;

  const paragraphText = getParagraphText(paragraph);
  const text = paragraphText.slice(offsets.start, offsets.end);
  if (!text.trim()) return null;

  const paragraphRange = getParagraphRange(paragraph);
  const duration = paragraphRange.end - paragraphRange.start;
  if (duration <= 0) return null;

  const allWords = paragraph.segments.flatMap((segment) => segment.words ?? []);
  const selectedWords = sliceWordsByCharRange(
    allWords,
    offsets.start,
    offsets.end,
    paragraphText
  );
  const fromWords = rangeFromWords(selectedWords, -1, -1);

  const start =
    selectedWords.length && fromWords.start >= 0
      ? fromWords.start
      : paragraphRange.start + (duration * offsets.start) / paragraphText.length;
  const end =
    selectedWords.length && fromWords.end >= 0
      ? fromWords.end
      : paragraphRange.start + (duration * offsets.end) / paragraphText.length;

  if (end <= start) return null;

  return {
    id: `copy-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    speakerId: paragraph.speakerId,
    speakerName: paragraph.speakerName,
    text,
    start,
    end,
    originStart: start,
    originEnd: end,
  };
}

export function buildTranscriptHighlight(options: {
  playbackSync: { paragraphId: string; segmentId: string } | null;
}): TranscriptHighlight | null {
  if (!options.playbackSync) return null;

  return {
    paragraphId: options.playbackSync.paragraphId,
    segmentIds: [options.playbackSync.segmentId],
    mode: 'playback',
  };
}

export function findActiveCopySegment(
  segments: SelectedCopySegment[],
  currentTime: number
): SelectedCopySegment | null {
  return (
    segments.find((segment) => currentTime >= segment.start && currentTime < segment.end) ?? null
  );
}

export function collectSegmentsFromSelection(
  container: HTMLElement,
  selection: Selection
): TranscriptSegment[] {
  if (selection.isCollapsed || !selection.rangeCount) return [];

  const range = selection.getRangeAt(0);
  const nodes = container.querySelectorAll<HTMLElement>('[data-segment-id]');
  const picked: TranscriptSegment[] = [];

  nodes.forEach((node) => {
    if (!range.intersectsNode(node)) return;

    const start = Number(node.dataset.start);
    const end = Number(node.dataset.end);
    const text = node.textContent ?? '';

    if (!Number.isFinite(start) || !Number.isFinite(end) || !text) return;

    picked.push({
      id: node.dataset.segmentId ?? `${start}-${end}`,
      start,
      end,
      text,
    });
  });

  return picked.sort((a, b) => a.start - b.start);
}

export function segmentsToCopySegment(
  segments: TranscriptSegment[],
  speakerId: string,
  speakerName: string
): SelectedCopySegment | null {
  if (!segments.length) return null;

  const first = segments[0];
  const last = segments[segments.length - 1];
  if (!first || !last) return null;

  return {
    id: `copy-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    speakerId,
    speakerName,
    text: segments.map((item) => item.text).join(''),
    start: first.start,
    end: last.end,
    originStart: first.start,
    originEnd: last.end,
  };
}

export function paragraphToCopySegment(paragraph: TranscriptParagraph): SelectedCopySegment | null {
  return segmentsToCopySegment(paragraph.segments, paragraph.speakerId, paragraph.speakerName);
}

export function deleteSelectedRangeFromSegment(
  segment: SelectedCopySegment,
  selectionStart: number,
  selectionEnd: number
): SelectedCopySegment[] | 'delete-all' | null {
  const text = segment.text;

  if (selectionStart < 0 || selectionEnd > text.length || selectionStart >= selectionEnd) {
    return null;
  }

  if (selectionStart === 0 && selectionEnd === text.length) {
    return 'delete-all';
  }

  const duration = segment.end - segment.start;
  if (duration <= 0.5) return null;

  const timeAt = (offset: number) => segment.start + (duration * offset) / text.length;
  const deleteStartTime = timeAt(selectionStart);
  const deleteEndTime = timeAt(selectionEnd);

  const beforeText = text.slice(0, selectionStart);
  const afterText = text.slice(selectionEnd);

  if (beforeText && afterText) {
    if (deleteStartTime - segment.start < 0.5 || segment.end - deleteEndTime < 0.5) {
      return null;
    }

    return [
      {
        ...segment,
        id: `${segment.id}-a-${Date.now()}`,
        text: beforeText,
        end: deleteStartTime,
        originStart: segment.start,
        originEnd: deleteStartTime,
      },
      {
        ...segment,
        id: `${segment.id}-b-${Date.now()}`,
        text: afterText,
        start: deleteEndTime,
        originStart: deleteEndTime,
        originEnd: segment.end,
      },
    ];
  }

  if (beforeText) {
    if (deleteStartTime - segment.start < 0.5) return null;
    return [
      {
        ...segment,
        text: beforeText,
        end: deleteStartTime,
        originStart: segment.originStart ?? segment.start,
        originEnd: deleteStartTime,
      },
    ];
  }

  if (afterText) {
    if (segment.end - deleteEndTime < 0.5) return null;
    return [
      {
        ...segment,
        text: afterText,
        start: deleteEndTime,
        originStart: deleteEndTime,
        originEnd: segment.originEnd ?? segment.end,
      },
    ];
  }

  return null;
}

export function getTextSelectionOffsets(container: HTMLElement): { start: number; end: number } | null {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !selection.rangeCount) return null;

  const range = selection.getRangeAt(0);
  if (!container.contains(range.commonAncestorContainer)) return null;

  const selectedText = range.toString();
  if (!selectedText) return null;

  const preRange = range.cloneRange();
  preRange.selectNodeContents(container);
  preRange.setEnd(range.startContainer, range.startOffset);

  const start = preRange.toString().length;
  const end = start + selectedText.length;

  if (start >= end) return null;

  return { start, end };
}

export function adjustSegmentTime(
  segment: SelectedCopySegment,
  field: 'start' | 'end',
  delta: number,
  videoDuration: number
): SelectedCopySegment {
  const next = { ...segment };
  const value = next[field] + delta;

  if (field === 'start') {
    next.start = Math.max(0, Math.min(value, next.end - 0.5));
  } else {
    next.end = Math.min(videoDuration || value, Math.max(value, next.start + 0.5));
  }

  return next;
}

const MIN_SEGMENT_DURATION = 0.5;

/** 每次点击调节步长（秒） */
export const SEGMENT_EXTEND_STEP_SEC = 0.1;

function getSegmentOriginStart(segment: SelectedCopySegment) {
  return Number.isFinite(segment.originStart) ? Number(segment.originStart) : segment.start;
}

function getSegmentOriginEnd(segment: SelectedCopySegment) {
  return Number.isFinite(segment.originEnd) ? Number(segment.originEnd) : segment.end;
}

/** 前方已扩留白秒数 */
export function getSegmentFrontPadSeconds(segment: SelectedCopySegment) {
  return Math.max(0, getSegmentOriginStart(segment) - segment.start);
}

/** 后方已扩留白秒数 */
export function getSegmentBackPadSeconds(segment: SelectedCopySegment) {
  return Math.max(0, segment.end - getSegmentOriginEnd(segment));
}

/** 留白秒数展示，如 0.1s */
export function formatPadSeconds(seconds: number) {
  const value = Math.max(0, Number.isFinite(seconds) ? seconds : 0);
  return `${value.toFixed(1)}s`;
}

/**
 * 某一侧还能调多少秒（仅调时间留白，不改文案）。
 * - expand：向外扩留白，上限以文案分段原始文字为准（不覆盖前后文字）
 * - shrink：收回已扩的留白（不超过选入时的原始边界）
 */
export function getSegmentAdjustableSeconds(
  segments: SelectedCopySegment[],
  index: number,
  edge: 'start' | 'end',
  direction: 'expand' | 'shrink',
  videoDuration: number,
  paragraphs: TranscriptParagraph[] = []
): number {
  const segment = segments[index];
  if (!segment) return 0;

  if (edge === 'start') {
    if (direction === 'expand') {
      const originStart = getSegmentOriginStart(segment);
      const originEnd = getSegmentOriginEnd(segment);
      const { lowerBound } = getTranscriptPadBounds(
        paragraphs,
        originStart,
        originEnd,
        videoDuration
      );
      return Math.max(0, segment.start - lowerBound);
    }
    // 仅收回前方已扩留白，不切入原始文案区间
    return Math.max(0, getSegmentOriginStart(segment) - segment.start);
  }

  if (direction === 'expand') {
    const originStart = getSegmentOriginStart(segment);
    const originEnd = getSegmentOriginEnd(segment);
    const { upperBound } = getTranscriptPadBounds(
      paragraphs,
      originStart,
      originEnd,
      videoDuration
    );
    return Math.max(0, upperBound - segment.end);
  }
  // 仅收回后方已扩留白
  return Math.max(0, segment.end - getSegmentOriginEnd(segment));
}

/** @deprecated 使用 getSegmentAdjustableSeconds(..., 'expand') */
export function getSegmentExtendableSeconds(
  segments: SelectedCopySegment[],
  index: number,
  edge: 'start' | 'end',
  videoDuration: number,
  paragraphs: TranscriptParagraph[] = []
): number {
  return getSegmentAdjustableSeconds(segments, index, edge, 'expand', videoDuration, paragraphs);
}

/**
 * 调节片段前/后时间留白。deltaSec > 0 向外扩，deltaSec < 0 向内收。
 * 只改 start/end，不增删文案。边界以文案分段原始数据为准。
 */
export function adjustSegmentEdge(
  segments: SelectedCopySegment[],
  index: number,
  edge: 'start' | 'end',
  deltaSec: number,
  videoDuration: number,
  paragraphs: TranscriptParagraph[] = []
): { segments: SelectedCopySegment[]; applied: number } | null {
  const segment = segments[index];
  if (!segment || deltaSec === 0) return null;

  const direction: 'expand' | 'shrink' = deltaSec > 0 ? 'expand' : 'shrink';
  const step = Math.abs(deltaSec);
  const available = getSegmentAdjustableSeconds(
    segments,
    index,
    edge,
    direction,
    videoDuration,
    paragraphs
  );
  const applied = Math.min(step, available);
  if (applied <= 0) return null;

  const nextSegment: SelectedCopySegment = {
    ...segment,
    originStart: getSegmentOriginStart(segment),
    originEnd: getSegmentOriginEnd(segment),
  };
  const EPS = 1e-3;
  const { lowerBound, upperBound } = getTranscriptPadBounds(
    paragraphs,
    nextSegment.originStart!,
    nextSegment.originEnd!,
    videoDuration
  );

  if (edge === 'start') {
    if (direction === 'expand') {
      const targetStart = Math.max(lowerBound, segment.start - applied);
      nextSegment.start = Math.min(targetStart, segment.end - MIN_SEGMENT_DURATION);
    } else {
      nextSegment.start = Math.min(
        segment.start + applied,
        nextSegment.originStart!,
        segment.end - MIN_SEGMENT_DURATION
      );
    }
  } else if (direction === 'expand') {
    const targetEnd = Math.min(upperBound, segment.end + applied);
    nextSegment.end = Math.max(targetEnd, segment.start + MIN_SEGMENT_DURATION);
  } else {
    nextSegment.end = Math.max(
      segment.end - applied,
      nextSegment.originEnd!,
      segment.start + MIN_SEGMENT_DURATION
    );
  }

  if (
    nextSegment.start >= nextSegment.end - EPS ||
    (Math.abs(nextSegment.start - segment.start) < EPS &&
      Math.abs(nextSegment.end - segment.end) < EPS)
  ) {
    return null;
  }

  const next = [...segments];
  next[index] = nextSegment;
  const actualApplied =
    edge === 'start'
      ? Math.abs(segment.start - nextSegment.start)
      : Math.abs(nextSegment.end - segment.end);

  return { segments: next, applied: Math.max(0, actualApplied) };
}

/** @deprecated 使用 adjustSegmentEdge(..., +deltaSec) */
export function extendSegmentEdge(
  segments: SelectedCopySegment[],
  index: number,
  edge: 'start' | 'end',
  deltaSec: number,
  videoDuration: number,
  paragraphs: TranscriptParagraph[] = []
): { segments: SelectedCopySegment[]; applied: number } | null {
  return adjustSegmentEdge(
    segments,
    index,
    edge,
    Math.abs(deltaSec),
    videoDuration,
    paragraphs
  );
}

export function getTotalSelectedDuration(segments: SelectedCopySegment[]) {
  return segments.reduce((sum, item) => sum + (item.end - item.start), 0);
}

/** 已选文案的原文时间区间（不含前后留白），用于在文案分段中标色 */
export function getSelectedCopyOriginRanges(
  segments: SelectedCopySegment[]
): Array<{ start: number; end: number }> {
  return segments
    .map((segment) => {
      const start = Number.isFinite(segment.originStart)
        ? Number(segment.originStart)
        : segment.start;
      const end = Number.isFinite(segment.originEnd) ? Number(segment.originEnd) : segment.end;
      return { start, end };
    })
    .filter((range) => Number.isFinite(range.start) && Number.isFinite(range.end) && range.end > range.start);
}

/** 文案分段时间轴是否与已选原文区间重叠 */
export function isTranscriptRangeSelected(
  start: number,
  end: number,
  selectedRanges: Array<{ start: number; end: number }>
): boolean {
  if (!selectedRanges.length || !Number.isFinite(start) || !Number.isFinite(end)) {
    return false;
  }

  const EPS = 1e-3;
  return selectedRanges.some((range) => {
    // ASR 末尾字/标点常见 start===end；点落在半开区间 [start, end)
    // 避免下一段起点/首字标点（恰等于 end）被误高亮而「超出」
    if (!(end > start)) {
      return start >= range.start - EPS && start < range.end;
    }
    // 正长度区间同样按半开 [start, end)
    return start < range.end && range.start < end;
  });
}

export function reorderSegments(
  segments: SelectedCopySegment[],
  fromIndex: number,
  toIndex: number
) {
  if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0) return segments;
  if (fromIndex >= segments.length || toIndex >= segments.length) return segments;

  const next = [...segments];
  const [moved] = next.splice(fromIndex, 1);
  if (!moved) return segments;

  let targetIndex = toIndex;
  if (fromIndex < toIndex) {
    targetIndex -= 1;
  }

  next.splice(targetIndex, 0, moved);
  return next;
}

function escapeHtml(text: string) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** 生成用于 dangerouslySetInnerHTML 的高亮 HTML；先转义再包 mark，避免 XSS */
export function highlightKeyword(text: string, keyword: string) {
  const safeText = escapeHtml(text);
  if (!keyword.trim()) return safeText;

  const safeKeyword = escapeHtml(keyword);
  const escaped = safeKeyword.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const regex = new RegExp(`(${escaped})`, 'gi');
  return safeText.replace(regex, '<mark>$1</mark>');
}

function formatSrtTimestamp(seconds: number): string {
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);
  const ms = Math.round((seconds % 1) * 1000);

  return `${String(hrs).padStart(2, '0')}:${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')},${String(ms).padStart(3, '0')}`;
}

export function buildTranscriptSrt(paragraphs: TranscriptParagraph[]): string {
  const cues = paragraphs.flatMap((paragraph) =>
    paragraph.segments.map((segment) => ({
      start: segment.start,
      end: segment.end,
      text: `${paragraph.speakerName}：${segment.text}`.trim(),
    }))
  );

  return cues
    .map((cue, index) => {
      return `${index + 1}\n${formatSrtTimestamp(cue.start)} --> ${formatSrtTimestamp(cue.end)}\n${cue.text}`;
    })
    .join('\n\n');
}

export function downloadTextFile(content: string, filename: string) {
  const blob = new Blob([`\uFEFF${content}`], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

export function sanitizeDownloadFilename(name: string) {
  return name.replace(/[\\/:*?"<>|]/g, '_').trim() || 'subtitle';
}

/** 文案列表滚动时，将目标元素置于视口偏上位置（默认约 36%，居中为 50%） */
export function scrollElementIntoViewPreferUpper(
  container: HTMLElement,
  element: HTMLElement,
  options?: { behavior?: ScrollBehavior; viewportRatio?: number }
) {
  const behavior = options?.behavior ?? 'smooth';
  const viewportRatio = options?.viewportRatio ?? 0.36;
  const containerRect = container.getBoundingClientRect();
  const elementRect = element.getBoundingClientRect();
  const elementTop = elementRect.top - containerRect.top + container.scrollTop;
  const elementCenter = elementTop + element.offsetHeight / 2;
  const targetScrollTop = elementCenter - container.clientHeight * viewportRatio;

  container.scrollTo({
    top: Math.max(0, targetScrollTop),
    behavior,
  });
}

const FOLLOW_COMFORT_TOP_RATIO = 0.24;
const FOLLOW_COMFORT_BOTTOM_RATIO = 0.68;

/** 播放跟随时，仅在目标离开舒适区时做最小增量滚动，避免段落切换大幅跳转 */
export function scrollFollowElement(
  container: HTMLElement,
  element: HTMLElement,
  options?: { behavior?: ScrollBehavior }
) {
  const behavior = options?.behavior ?? 'smooth';
  const containerRect = container.getBoundingClientRect();
  const elementRect = element.getBoundingClientRect();
  const comfortTop = containerRect.top + container.clientHeight * FOLLOW_COMFORT_TOP_RATIO;
  const comfortBottom = containerRect.top + container.clientHeight * FOLLOW_COMFORT_BOTTOM_RATIO;
  const elementTop = elementRect.top;
  const elementBottom = elementRect.bottom;

  if (elementTop >= comfortTop && elementBottom <= comfortBottom) {
    return false;
  }

  let scrollDelta = 0;
  if (elementBottom > comfortBottom) {
    scrollDelta = elementBottom - comfortBottom;
  } else if (elementTop < comfortTop) {
    scrollDelta = elementTop - comfortTop;
  }

  if (Math.abs(scrollDelta) < 6) {
    return false;
  }

  container.scrollBy({ top: scrollDelta, behavior });
  return true;
}
