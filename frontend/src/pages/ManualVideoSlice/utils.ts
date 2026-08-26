import type { AsrParagraphs, AsrSummary, LiveAsrSegment } from '~/services/sourceVideo.model';
import type {
  AiSegment,
  SelectedCopySegment,
  TranscriptParagraph,
  TranscriptSegment,
  TranscriptWord,
} from './types';
import { speakerColors } from '~/style/semanticColors';

export {
  findActiveSegment,
  flattenTranscriptSegments,
  getAdjacentSentenceSeekTime,
  canSeekAdjacentSentence,
  captureVideoScreenshot,
  type FlatTranscriptSegment,
} from '~/utils/videoPlayerTools';

export const SPEAKER_COLORS = [...speakerColors];

export function getSpeakerColor(speaker: string, speakers: string[]) {
  const index = speakers.indexOf(speaker);
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
function findWordCharIndex(sourceText: string, wordText: string, fromIndex: number): number {
  if (!wordText) return -1;

  let scanFrom = Math.max(0, fromIndex);
  while (scanFrom < sourceText.length) {
    if (sourceText.startsWith(wordText, scanFrom)) return scanFrom;

    const found = sourceText.indexOf(wordText, scanFrom);
    if (found >= 0) return found;

    const nextChar = sourceText[scanFrom];
    if (nextChar && /\s/.test(nextChar)) {
      scanFrom += 1;
      continue;
    }

    break;
  }

  return -1;
}

function collapseSpaces(text: string) {
  return text.replace(/\s+/g, '');
}

/** 纯空白 token（ASR 占位，不参与时间计算） */
export function isWhitespaceOnlyWordText(text: string): boolean {
  return text.length > 0 && [...text].every((ch) => /\s/u.test(ch));
}

/** 字级时间是否有效（后台占位常用 -1） */
export function hasValidWordTiming(start: number, end: number): boolean {
  return Number.isFinite(start) && Number.isFinite(end) && start >= 0 && end >= 0;
}

/** 参与选片/跟读/子句计时的字词（排除空白占位与无效时间） */
export function isTimingTranscriptWord(word: TranscriptWord): boolean {
  return hasValidWordTiming(word.start, word.end) && !isWhitespaceOnlyWordText(word.text);
}

/** 选区是否仅含标点（不含空格与可发音字符；标点无字级时间） */
function isPunctuationOnlySelection(text: string): boolean {
  if (!text) return false;
  return [...text].every((ch) => /\p{P}/u.test(ch));
}

/** 选区是否含可发音字符（排除仅空格/标点，左侧拖选不应加入文案预览） */
function hasSpeakableCopyText(text: string): boolean {
  return [...text].some((ch) => !/\s/u.test(ch) && !/\p{P}/u.test(ch));
}

/** 部分删除后丢弃仅含标点的首尾片段（标点无 ASR 字级时间，不应单独成段） */
function dropPunctuationOnlySplitPieces(beforeText: string, afterText: string) {
  return {
    before: isPunctuationOnlySelection(beforeText) ? '' : beforeText,
    after: isPunctuationOnlySelection(afterText) ? '' : afterText,
  };
}

/** 在段落原文中定位选段文案（兼容 ASR 文本与选段文本的空格差异） */
function findAllSegmentTextStarts(paragraphText: string, segmentText: string): number[] {
  if (!segmentText) return [];

  const indices: number[] = [];
  let searchFrom = 0;
  while (searchFrom <= paragraphText.length) {
    const textIndex = paragraphText.indexOf(segmentText, searchFrom);
    if (textIndex < 0) break;
    indices.push(textIndex);
    searchFrom = textIndex + 1;
  }
  if (indices.length) return indices;

  const normSegment = collapseSpaces(segmentText);
  if (!normSegment) return [];

  let normParagraph = '';
  const normToOrig: number[] = [];
  for (let i = 0; i < paragraphText.length; i += 1) {
    const ch = paragraphText[i]!;
    if (/\s/.test(ch)) continue;
    normToOrig[normParagraph.length] = i;
    normParagraph += ch;
  }

  searchFrom = 0;
  while (searchFrom <= normParagraph.length) {
    const normIndex = normParagraph.indexOf(normSegment, searchFrom);
    if (normIndex < 0) break;
    const origIndex = normToOrig[normIndex];
    if (origIndex != null) indices.push(origIndex);
    searchFrom = normIndex + 1;
  }

  return indices;
}

export function sliceWordsByCharRange(
  words: TranscriptWord[],
  charStart: number,
  charEnd: number,
  sourceText?: string
): TranscriptWord[] {
  if (!words.length || charEnd <= charStart) return [];

  if (sourceText) {
    return alignWordsToSourceText(sourceText, words)
      .filter((item) => item.charStart < charEnd && item.charEnd > charStart)
      .map((item) => item.word);
  }

  let cursor = 0;
  const picked: TranscriptWord[] = [];
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

/**
 * 按时间顺序将 words 对齐到原文 char 区间。
 * 相比 sliceWordsByCharRange，对齐失败时会继续尝试后续字词，避免长停顿处字符少导致截断。
 */
function collectAlignedWordsInCharRange(
  words: TranscriptWord[],
  sourceText: string,
  charStart: number,
  charEnd: number
): TranscriptWord[] {
  return alignWordsToSourceText(sourceText, words)
    .filter((item) => item.charStart < charEnd && item.charEnd > charStart)
    .map((item) => item.word);
}

function filterWordsByTimeRange(
  words: TranscriptWord[],
  timeStart: number,
  timeEnd: number
): TranscriptWord[] {
  return words
    .filter(
      (word) =>
        isTimingTranscriptWord(word) &&
        word.end > timeStart - 0.05 &&
        word.start < timeEnd + 0.05
    )
    .sort((a, b) => a.start - b.start || a.end - b.end);
}

/** 在保留文案前缀中，按字词文本匹配取最晚结束时间（解决长停顿导致字符比例失真） */
function lastWordEndForKeptTextPrefix(
  sourceText: string,
  sourceOffset: number,
  sourceCharEnd: number,
  timeWords: TranscriptWord[]
): number | null {
  const keptSlice = sourceText.slice(sourceOffset, sourceCharEnd);
  if (!keptSlice.trim() || !timeWords.length) return null;

  let bestEnd: number | null = null;
  let textIndex = 0;

  for (const word of timeWords) {
    if (!isTimingTranscriptWord(word)) continue;
    const wordText = word.text ?? '';
    if (!wordText) continue;

    const found = findWordCharIndex(keptSlice, wordText, textIndex);
    if (found === -1) continue;

    if (found >= keptSlice.length) continue;
    if (found + wordText.length > keptSlice.length) continue;

    bestEnd = Math.max(bestEnd ?? word.start, word.end, word.start);
    textIndex = Math.max(textIndex, found + wordText.length);
  }

  return bestEnd;
}

function enhanceBoundsForKeptPrefix(
  bounds: { start: number; end: number },
  segmentText: string,
  charEnd: number,
  timeWords: TranscriptWord[],
  sourceText?: string,
  sourceOffset = 0
): { start: number; end: number } {
  if (charEnd <= 0 || charEnd >= segmentText.length || !timeWords.length) {
    return bounds;
  }

  const keptText = segmentText.slice(0, charEnd);
  const alignSource = sourceText ?? segmentText;
  const sourceCharEnd =
    alignSource === segmentText
      ? sourceOffset + keptText.length
      : mapSegmentCharToParagraphChar(alignSource, segmentText, sourceOffset, charEnd);

  const matchedEnd = lastWordEndForKeptTextPrefix(
    alignSource,
    sourceOffset,
    sourceCharEnd,
    timeWords
  );

  if (matchedEnd == null) return bounds;

  return {
    start: bounds.start,
    end: Math.max(bounds.end, matchedEnd),
  };
}

export type TranscriptTextToken = {
  text: string;
  start: number;
  end: number;
  charStart: number;
  charEnd: number;
};

/**
 * 将 words 对齐回完整句段文本：ASR words 常不含标点，选片高亮时若只渲染 words 会丢「，。」等。
 * 以 segment.text 为准，把 words 之间的空隙（标点）补成独立 token，并保证覆盖全文。
 */
export function alignWordsToTranscriptText(
  text: string,
  words: TranscriptWord[]
): TranscriptTextToken[] {
  if (!text) return [];
  if (!words.length) {
    return [{ text, start: 0, end: 0, charStart: 0, charEnd: text.length }];
  }

  const aligned = alignWordsToSourceText(text, words);
  const tokens: TranscriptTextToken[] = [];
  let textIndex = 0;
  let lastTimedEnd = aligned.find((item) => isTimingTranscriptWord(item.word))?.word.start ?? 0;

  for (const item of aligned) {
    const isTimed = isTimingTranscriptWord(item.word);
    const wordStart = isTimed ? item.word.start : lastTimedEnd;
    const wordEnd = isTimed ? item.word.end : lastTimedEnd;

    if (item.charStart > textIndex) {
      tokens.push({
        text: text.slice(textIndex, item.charStart),
        start: lastTimedEnd,
        end: wordStart,
        charStart: textIndex,
        charEnd: item.charStart,
      });
    }

    tokens.push({
      text: text.slice(item.charStart, item.charEnd),
      start: wordStart,
      end: wordEnd,
      charStart: item.charStart,
      charEnd: item.charEnd,
    });
    if (isTimed) lastTimedEnd = item.word.end;
    textIndex = item.charEnd;
  }

  if (textIndex < text.length) {
    const last = aligned[aligned.length - 1];
    const tailTime = last ? Math.max(last.word.end, last.word.start) : 0;
    tokens.push({
      text: text.slice(textIndex),
      start: tailTime,
      end: tailTime,
      charStart: textIndex,
      charEnd: text.length,
    });
  }

  return tokens;
}

function rangeFromWords(
  words: TranscriptWord[],
  fallbackStart: number,
  fallbackEnd: number
): { start: number; end: number } {
  const timingWords = words.filter(isTimingTranscriptWord);
  if (!timingWords.length) return { start: fallbackStart, end: fallbackEnd };
  const first = timingWords[0];
  if (!first) return { start: fallbackStart, end: fallbackEnd };
  // 取全部 words 的最大 end，避免末尾零时长字（标点）导致区间偏短
  const end = timingWords.reduce(
    (max, word) => Math.max(max, word.end, word.start),
    first.start
  );
  return {
    start: first.start,
    end,
  };
}

/** 字词在原文中的字符区间（含 ASR 空格 token） */
export type AlignedWordSpan = {
  word: TranscriptWord;
  charStart: number;
  charEnd: number;
};

/**
 * 将 ASR words 按时间顺序对齐到原文，保留空格/标点 token，供选片与部分删除共用。
 */
export function alignWordsToSourceText(
  sourceText: string,
  words: TranscriptWord[]
): AlignedWordSpan[] {
  if (!sourceText || !words.length) return [];

  const aligned: AlignedWordSpan[] = [];
  let minTextIndex = 0;

  for (const word of words) {
    const wordText = word.text ?? '';
    if (!wordText) continue;

    let found = findWordCharIndex(sourceText, wordText, minTextIndex);
    if (found === -1 && minTextIndex > 0) {
      found = findWordCharIndex(
        sourceText,
        wordText,
        Math.max(0, minTextIndex - wordText.length)
      );
    }
    if (found === -1) continue;

    aligned.push({
      word,
      charStart: found,
      charEnd: found + wordText.length,
    });
    minTextIndex = Math.max(minTextIndex, found + wordText.length);
  }

  return aligned;
}

function interpolateWordTimeAtSpan(item: AlignedWordSpan, charOffset: number): number {
  const span = Math.max(item.charEnd - item.charStart, 1);
  const ratio = (charOffset - item.charStart) / span;
  return item.word.start + (item.word.end - item.word.start) * ratio;
}

export type CharRangeTiming = {
  start: number;
  end: number;
  words: TranscriptWord[];
};

/**
 * 按字符选区解析字级时间：首字 start、末字 end；选区切过字词时在边界插值。
 */
export function resolveTimingForCharRange(options: {
  sourceText: string;
  words: TranscriptWord[];
  charStart: number;
  charEnd: number;
  fallbackStart: number;
  fallbackEnd: number;
}): CharRangeTiming {
  const { sourceText, words, charStart, charEnd, fallbackStart, fallbackEnd } = options;
  const duration = fallbackEnd - fallbackStart;
  const linearAt = (offset: number) =>
    fallbackStart + (duration * offset) / Math.max(sourceText.length, 1);

  if (charEnd <= charStart || !sourceText) {
    return { start: fallbackStart, end: fallbackEnd, words: [] };
  }

  const aligned = alignWordsToSourceText(sourceText, words);
  const overlapping = aligned.filter(
    (item) => item.charStart < charEnd && item.charEnd > charStart
  );

  if (!overlapping.length) {
    return {
      start: linearAt(charStart),
      end: linearAt(charEnd),
      words: [],
    };
  }

  let start: number | null = null;
  let end: number | null = null;
  const pickedWords: TranscriptWord[] = [];

  for (const item of overlapping) {
    if (!isTimingTranscriptWord(item.word)) continue;

    pickedWords.push(item.word);

    const wordStart =
      item.charStart < charStart
        ? interpolateWordTimeAtSpan(item, charStart)
        : item.word.start;
    const wordEnd =
      item.charEnd > charEnd ? interpolateWordTimeAtSpan(item, charEnd) : item.word.end;

    start = start == null ? wordStart : Math.min(start, wordStart);
    end = end == null ? wordEnd : Math.max(end, wordEnd);
  }

  return {
    start: start ?? linearAt(charStart),
    end: end ?? linearAt(charEnd),
    words: pickedWords,
  };
}

/** 从段落字符区间生成文案预览片段（拖选 / 双击整段共用） */
export function buildCopySegmentFromParagraphRange(
  paragraph: TranscriptParagraph,
  charStart: number,
  charEnd: number
): SelectedCopySegment | null {
  const paragraphText = getParagraphText(paragraph);
  const clampedStart = Math.max(0, Math.min(charStart, paragraphText.length));
  const clampedEnd = Math.max(clampedStart, Math.min(charEnd, paragraphText.length));
  const text = paragraphText.slice(clampedStart, clampedEnd);
  if (!hasSpeakableCopyText(text)) return null;

  const paragraphRange = getParagraphRange(paragraph);
  const isFullParagraphSelection =
    clampedStart === 0 && clampedEnd === paragraphText.length;

  let start: number;
  let end: number;

  if (isFullParagraphSelection) {
    start = paragraphRange.start;
    end = paragraphRange.end;
  } else {
    const allWords = paragraph.segments.flatMap((segment) => segment.words ?? []);
    const paragraphWords = allWords.filter(
      (word) =>
        word.end > paragraphRange.start - 0.05 && word.start < paragraphRange.end + 0.05
    );
    const timing = resolveTimingForCharRange({
      sourceText: paragraphText,
      words: paragraphWords.length ? paragraphWords : allWords,
      charStart: clampedStart,
      charEnd: clampedEnd,
      fallbackStart: paragraphRange.start,
      fallbackEnd: paragraphRange.end,
    });

    start = Math.max(
      paragraphRange.start,
      Math.min(timing.start, paragraphRange.end)
    );
    end = Math.max(start, Math.min(timing.end, paragraphRange.end));
  }

  if (end <= start) return null;

  return {
    id: `copy-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    speaker: paragraph.speaker,
    speakerName: paragraph.speakerName,
    text,
    start,
    end,
    sourceParagraphId: paragraph.id,
    originStart: start,
    originEnd: end,
  };
}

const CLAUSE_SPLIT_RE = /(?<=[，。！？；])/g;
const CAPTION_CLAUSE_SPLIT_RE = /(?<=[，。！？；!?])/g;

/** 预览字幕：按标点拆句（保留标点） */
export function splitCaptionClauses(text: string): string[] {
  const trimmed = text.trim();
  if (!trimmed) return [];

  const parts = trimmed.split(CAPTION_CLAUSE_SPLIT_RE).filter((part) => part.trim());
  return parts.length ? parts : [trimmed];
}

export type PreviewCaptionClause = {
  text: string;
  start: number;
  end: number;
};

/** 为预览字幕子句绑定字级/段级时间 */
export function buildPreviewCaptionClauses(
  text: string,
  segmentStart: number,
  segmentEnd: number,
  words: TranscriptWord[] = []
): PreviewCaptionClause[] {
  const clauses = splitCaptionClauses(text);
  if (!clauses.length) return [];

  const duration = Math.max(segmentEnd - segmentStart, 0.001);
  const linearAt = (offset: number) =>
    segmentStart + (duration * offset) / Math.max(text.length, 1);

  let charOffset = 0;
  return clauses.map((clause) => {
    const charStart = charOffset;
    const charEnd = charOffset + clause.length;
    charOffset = charEnd;

    const timing = resolveTimingForCharRange({
      sourceText: text,
      words,
      charStart,
      charEnd,
      fallbackStart: linearAt(charStart),
      fallbackEnd: linearAt(charEnd),
    });

    const start = Math.max(segmentStart, Math.min(timing.start, segmentEnd));
    const end = Math.max(start, Math.min(timing.end, segmentEnd));

    return { text: clause, start, end };
  });
}

function pickPreviewCaptionClause(
  clauses: PreviewCaptionClause[],
  currentTime: number
): PreviewCaptionClause | null {
  if (!clauses.length) return null;

  const EPS = 0.05;
  let active: PreviewCaptionClause | null = null;

  for (const clause of clauses) {
    if (currentTime >= clause.start - EPS && currentTime < clause.end + EPS) {
      if (!active || clause.start > active.start) active = clause;
    }
  }
  if (active) return active;

  let lastStarted: PreviewCaptionClause | null = null;
  for (const clause of clauses) {
    if (clause.start <= currentTime + EPS) {
      if (!lastStarted || clause.start > lastStarted.start) lastStarted = clause;
    }
  }

  if (lastStarted) {
    const next = clauses.find((clause) => clause.start > lastStarted!.start);
    if (!next || currentTime < next.start - EPS) return lastStarted;
  }

  if (currentTime < clauses[0]!.start - EPS) return null;

  return clauses[clauses.length - 1]!;
}

/**
 * 连续预览字幕：按字级时间展示当前一句；无字级时回退段内比例估算。
 */
export function resolvePreviewCaptionLines(
  text: string,
  segmentStart: number,
  segmentEnd: number,
  currentTime: number,
  words: TranscriptWord[] = []
): string[] {
  const clauses = buildPreviewCaptionClauses(text, segmentStart, segmentEnd, words);
  if (!clauses.length) return [];

  const picked = pickPreviewCaptionClause(clauses, currentTime);
  return picked ? [picked.text] : [];
}

export function splitTextToSegments(
  text: string,
  start: number,
  end: number,
  idPrefix: string,
  words?: TranscriptWord[]
): TranscriptSegment[] {
  const parts = text.split(CLAUSE_SPLIT_RE).filter(Boolean);
  if (parts.length <= 1) {
    return [
      {
        id: idPrefix,
        start,
        end: Math.max(end, start),
        text,
        words: words?.length ? words : undefined,
      },
    ];
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
    const ratio = part.length / text.length;
    const proportionalStart = cursor;
    const proportionalEnd = index === parts.length - 1 ? end : cursor + duration * ratio;

    const fromWords = rangeFromWords(partWords, proportionalStart, proportionalEnd);
    const segmentStart = partWords.length ? fromWords.start : proportionalStart;
    const segmentEnd = partWords.length
      ? fromWords.end
      : index === parts.length - 1
        ? end
        : proportionalEnd;

    const segment: TranscriptSegment = {
      id: `${idPrefix}-${index}`,
      start: segmentStart,
      end: Math.max(segmentStart, segmentEnd),
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
  if (/^\d+$/.test(trimmed)) return `说话人：${trimmed}`;
  return trimmed;
}

function asrParagraphToTranscript(item: LiveAsrSegment, index: number): TranscriptParagraph {
  const speaker = String(item.speaker ?? '').trim() || '0';
  const words: TranscriptWord[] = (item.words ?? [])
    .map((word) => ({
      start: msToSeconds(Number(word.start_time)),
      end: msToSeconds(Number(word.end_time)),
      text: String(word.text ?? ''),
    }))
    .filter((word) => {
      if (!word.text.length) return false;
      if (isWhitespaceOnlyWordText(word.text)) return true;
      return hasValidWordTiming(word.start, word.end);
    });

  const text = item.text || words.map((word) => word.text).join('');

  return {
    id: `asr-p-${index}`,
    speaker,
    speakerName: formatSpeakerName(speaker),
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

/** 文案分段段落是否与时间范围有重叠 */
export function isParagraphOverlappingTimeRange(
  paragraph: TranscriptParagraph,
  timeStart: number,
  timeEnd: number
): boolean {
  const paragraphRange = getParagraphRange(paragraph);
  return paragraphRange.end > timeStart && paragraphRange.start < timeEnd;
}

/**
 * 在文案分段段落中，按时间范围解析对应的字符选区（与手动拖选同一套 words 对齐）。
 * 供部分裁剪场景使用；AI 分段添加请用整段匹配（见 buildCopySegmentsFromAiSegment）。
 */
export function resolveParagraphCharRangeForTimeRange(
  paragraph: TranscriptParagraph,
  timeStart: number,
  timeEnd: number
): { charStart: number; charEnd: number } | null {
  const paragraphText = getParagraphText(paragraph);
  if (!paragraphText) return null;

  const paragraphRange = getParagraphRange(paragraph);
  const clipStart = Math.max(timeStart, paragraphRange.start);
  const clipEnd = Math.min(timeEnd, paragraphRange.end);
  if (clipEnd <= clipStart + 1e-6) return null;

  const allWords = paragraph.segments.flatMap((segment) => segment.words ?? []);
  if (!allWords.length) {
    const duration = paragraphRange.end - paragraphRange.start;
    if (duration <= 0) return { charStart: 0, charEnd: paragraphText.length };

    const ratioStart = (clipStart - paragraphRange.start) / duration;
    const ratioEnd = (clipEnd - paragraphRange.start) / duration;
    const charStart = Math.max(
      0,
      Math.min(paragraphText.length, Math.floor(ratioStart * paragraphText.length))
    );
    const charEnd = Math.max(
      charStart,
      Math.min(paragraphText.length, Math.ceil(ratioEnd * paragraphText.length))
    );
    return charEnd > charStart ? { charStart, charEnd } : null;
  }

  const aligned = alignWordsToSourceText(paragraphText, allWords);
  const overlapping = aligned.filter(
    (item) =>
      isTimingTranscriptWord(item.word) &&
      item.word.end > clipStart - 0.05 &&
      item.word.start < clipEnd + 0.05
  );
  if (!overlapping.length) return null;

  const charStart = overlapping[0]!.charStart;
  let charEnd = overlapping[overlapping.length - 1]!.charEnd;
  while (charEnd < paragraphText.length) {
    const ch = paragraphText[charEnd]!;
    if (/\s/u.test(ch) || /\p{P}/u.test(ch)) {
      charEnd += 1;
      continue;
    }
    break;
  }
  return charEnd > charStart ? { charStart, charEnd } : null;
}

/**
 * 按 AI 分段时间范围，在文案分段中逐段匹配并生成预览片段（每段对应一个 sourceParagraphId）。
 * 与时间范围有重叠的文案分段整段加入，与左侧双击选整段一致，避免 words 对齐失败导致缺字。
 */
export function buildCopySegmentsFromAiSegment(
  paragraphs: TranscriptParagraph[],
  aiSegment: AiSegment
): SelectedCopySegment[] {
  const stamp = Date.now();

  return paragraphs.flatMap((paragraph) => {
    if (!isParagraphOverlappingTimeRange(paragraph, aiSegment.start, aiSegment.end)) {
      return [];
    }

    const paragraphText = getParagraphText(paragraph);
    const copySegment = buildCopySegmentFromParagraphRange(paragraph, 0, paragraphText.length);
    if (!copySegment) return [];

    return [
      {
        ...copySegment,
        id: `copy-ai-${aiSegment.id}-${paragraph.id}-${stamp}`,
      },
    ];
  });
}

/** @deprecated 使用 buildCopySegmentsFromAiSegment */
export function buildCopySegmentFromAiSegment(
  paragraphs: TranscriptParagraph[],
  aiSegment: AiSegment
): SelectedCopySegment | null {
  const segments = buildCopySegmentsFromAiSegment(paragraphs, aiSegment);
  return segments[0] ?? null;
}

/** 打开已有项目时：仅使用 clips1（文案预览片段），clips0 不参与展示 */
export function resolveLoadedProjectSegments(
  project: { segments?: SelectedCopySegment[] },
  paragraphs: TranscriptParagraph[],
  videoDurationSec: number
): SelectedCopySegment[] {
  const raw = project.segments ?? [];
  if (!raw.length) return [];
  return sanitizeSelectedCopySegments(raw, paragraphs, videoDurationSec);
}

/** 扁平化全部字级时间轴（按开始时间排序） */
export function flattenTranscriptWords(paragraphs: TranscriptParagraph[]): TranscriptWord[] {
  return paragraphs
    .flatMap((paragraph) => paragraph.segments.flatMap((segment) => segment.words ?? []))
    .slice()
    .sort((a, b) => a.start - b.start || a.end - b.end);
}

/** 后方留白与下一句发声之间保留的音频安全间隔（秒，ASR 起点常晚于实际前音） */
export const TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC = 0.25;

/** 预览播放时在 end 前再提前截止，减轻解码 overshoot 带来的尾音 */
export const SEGMENT_PLAYBACK_END_GUARD_SEC = 0.08;

/** 收集各句/字的发声起点，用于判断后方留白上限（含无字级时间的邻句） */
function collectTranscriptSpokenStarts(paragraphs: TranscriptParagraph[]): number[] {
  const starts: number[] = [];

  for (const paragraph of paragraphs) {
    for (const segment of paragraph.segments) {
      const words = segment.words ?? [];
      if (words.length) {
        for (const word of words) {
          if (isTimingTranscriptWord(word)) starts.push(word.start);
        }
      } else if (Number.isFinite(segment.start)) {
        starts.push(segment.start);
      }
    }
  }

  return starts.sort((a, b) => a - b);
}

/** 收集各句/字的发声终点，用于判断前方留白下限 */
function collectTranscriptSpokenEnds(paragraphs: TranscriptParagraph[]): number[] {
  const ends: number[] = [];

  for (const paragraph of paragraphs) {
    for (const segment of paragraph.segments) {
      const words = segment.words ?? [];
      if (words.length) {
        for (const word of words) {
          if (!isTimingTranscriptWord(word)) continue;
          if (Number.isFinite(word.end)) ends.push(word.end);
          else if (Number.isFinite(word.start)) ends.push(word.start);
        }
      } else if (Number.isFinite(segment.end)) {
        ends.push(segment.end);
      }
    }
  }

  return ends.sort((a, b) => a - b);
}

function resolveTranscriptSegmentsTimeBounds(segments: TranscriptSegment[]) {
  const first = segments[0];
  const last = segments[segments.length - 1];
  if (!first || !last) return null;

  const firstFromWords = rangeFromWords(first.words ?? [], first.start, first.start);
  const lastFromWords = rangeFromWords(last.words ?? [], last.end, last.end);
  const start = first.words?.length ? firstFromWords.start : first.start;
  const end = last.words?.length ? lastFromWords.end : last.end;

  return {
    start,
    end: Math.max(end, start),
  };
}

/** 下一句最早发声时间：时间轴扫描 + 文案顺序（应对 ASR 句段 start 重叠） */
function findMinNextSpokenStart(
  paragraphs: TranscriptParagraph[],
  originEnd: number
): number | null {
  const EPS = 1e-3;
  let minNext = Number.POSITIVE_INFINITY;

  for (const start of collectTranscriptSpokenStarts(paragraphs)) {
    if (start > originEnd + EPS) {
      minNext = Math.min(minNext, start);
    }
  }

  const ordered = paragraphs.flatMap((paragraph) => paragraph.segments);
  for (let i = 0; i < ordered.length; i += 1) {
    const seg = ordered[i];
    if (!seg) continue;
    const bounds = resolveTranscriptSegmentsTimeBounds([seg]);
    if (!bounds) continue;
    if (originEnd + EPS < bounds.start || originEnd - EPS > bounds.end) continue;

    for (let j = i + 1; j < ordered.length; j += 1) {
      const next = ordered[j];
      if (!next) continue;
      if (next.words?.length) {
        const fromWords = rangeFromWords(next.words, next.start, next.start);
        minNext = Math.min(minNext, fromWords.start);
        break;
      }
      if (next.start > originEnd + EPS) {
        minNext = Math.min(minNext, next.start);
        break;
      }
    }
    break;
  }

  return minNext < Number.POSITIVE_INFINITY ? minNext : null;
}

function tightenOriginEndAgainstNextSpeech(
  paragraphs: TranscriptParagraph[],
  originEnd: number
): number {
  const minNext = findMinNextSpokenStart(paragraphs, originEnd);
  if (minNext == null) return originEnd;
  return Math.min(originEnd, minNext - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC);
}

/**
 * 按文案分段原始数据，计算当前选段前后可扩留白的边界。
 * - lowerBound：前方最近一段原始文字的结束时间（无则 0）
 * - upperBound：在 originEnd 与下一句之间保留安全间隔后，允许向后扩留白的上限
 */
export function getTranscriptPadBounds(
  paragraphs: TranscriptParagraph[],
  originStart: number,
  originEnd: number,
  _videoDuration: number
): { lowerBound: number; upperBound: number } {
  const EPS = 1e-3;
  const gap = TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC;

  let lowerBound = 0;
  for (const end of collectTranscriptSpokenEnds(paragraphs)) {
    if (end < originStart - EPS) {
      lowerBound = Math.max(lowerBound, end);
    }
  }
  lowerBound = Math.min(lowerBound, originStart);

  const minNextStart = findMinNextSpokenStart(paragraphs, originEnd);
  let upperBound = originEnd;
  if (minNextStart != null) {
    const interGap = minNextStart - originEnd;
    upperBound = interGap > gap ? originEnd + (interGap - gap) : originEnd;
  }

  return { lowerBound, upperBound };
}

/** 连续预览/播放应截止的源视频时间（略早于 segment.end，避免尾音） */
export function getSegmentPlaybackStopTime(segment: SelectedCopySegment): number {
  return Math.max(segment.start + MIN_SEGMENT_DURATION, segment.end - SEGMENT_PLAYBACK_END_GUARD_SEC);
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
  return buildCopySegmentFromParagraphRange(paragraph, offsets.start, offsets.end);
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
  speaker: string,
  speakerName: string
): SelectedCopySegment | null {
  if (!segments.length) return null;

  const text = segments.map((item) => item.text).join('');
  if (!hasSpeakableCopyText(text)) return null;

  const bounds = resolveTranscriptSegmentsTimeBounds(segments);
  if (!bounds) return null;

  const allWords = segments.flatMap((segment) => segment.words ?? []);
  const timing = resolveTimingForCharRange({
    sourceText: text,
    words: allWords,
    charStart: 0,
    charEnd: text.length,
    fallbackStart: bounds.start,
    fallbackEnd: bounds.end,
  });

  if (timing.end <= timing.start) return null;

  return {
    id: `copy-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    speaker,
    speakerName,
    text,
    start: timing.start,
    end: timing.end,
    originStart: timing.start,
    originEnd: timing.end,
  };
}

export function paragraphToCopySegment(paragraph: TranscriptParagraph): SelectedCopySegment | null {
  const paragraphText = getParagraphText(paragraph);
  return buildCopySegmentFromParagraphRange(paragraph, 0, paragraphText.length);
}

type SegmentParagraphAnchor = {
  paragraphId: string;
  paragraphText: string;
  textIndex: number;
  allWords: TranscriptWord[];
};

/** ASR word / 对齐 token 是否含可发音字符（排除纯空格与标点） */
function isSpeakableWordText(text: string): boolean {
  return [...text].some((ch) => !/\s/u.test(ch) && !/\p{P}/u.test(ch));
}

export type SelectedCopyHighlightRange = {
  paragraphId: string;
  charStart: number;
  charEnd: number;
  segmentId: string;
};

export type TextHighlightPart = {
  text: string;
  highlighted: boolean;
};

/** 合并重叠或相邻的字符区间，避免重复高亮同一段原文 */
function mergeCharRanges(ranges: Array<{ start: number; end: number }>) {
  if (!ranges.length) return [];

  const sorted = [...ranges].sort((a, b) => a.start - b.start || a.end - b.end);
  const merged: Array<{ start: number; end: number }> = [{ ...sorted[0]! }];

  for (let i = 1; i < sorted.length; i += 1) {
    const current = sorted[i]!;
    const last = merged[merged.length - 1]!;
    if (current.start <= last.end) {
      last.end = Math.max(last.end, current.end);
    } else {
      merged.push({ ...current });
    }
  }

  return merged;
}

/** 按段落内字符区间切分 clause 文本，供已选片段高亮渲染 */
export function splitTextByHighlightRanges(
  text: string,
  textOffset: number,
  ranges: Array<{ charStart: number; charEnd: number }>
): TextHighlightPart[] {
  if (!text) return [];
  if (!ranges.length) return [{ text, highlighted: false }];

  const localRanges = mergeCharRanges(
    ranges
      .map((range) => ({
        start: Math.max(0, range.charStart - textOffset),
        end: Math.min(text.length, range.charEnd - textOffset),
      }))
      .filter((range) => range.end > range.start)
  );

  if (!localRanges.length) return [{ text, highlighted: false }];

  const parts: TextHighlightPart[] = [];
  let cursor = 0;

  for (const range of localRanges) {
    if (range.start > cursor) {
      parts.push({ text: text.slice(cursor, range.start), highlighted: false });
    }
    parts.push({ text: text.slice(range.start, range.end), highlighted: true });
    cursor = range.end;
  }

  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor), highlighted: false });
  }

  return parts;
}

function resolveHighlightFromAnchor(
  anchor: SegmentParagraphAnchor,
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[]
): SelectedCopyHighlightRange {
  const segmentTextEnd = anchor.textIndex + segment.text.length;
  const fallbackRange: SelectedCopyHighlightRange = {
    paragraphId: anchor.paragraphId,
    charStart: anchor.textIndex,
    charEnd: segmentTextEnd,
    segmentId: segment.id,
  };

  const segmentWords = resolveCopySegmentWords(segment, paragraphs);
  const speakableSegmentWords = segmentWords.filter((word) => isSpeakableWordText(word.text));
  if (!speakableSegmentWords.length) return fallbackRange;

  const tokens = alignWordsToTranscriptText(anchor.paragraphText, anchor.allWords);
  let tokenIdx = tokens.findIndex((token) => token.charEnd > anchor.textIndex);
  if (tokenIdx < 0) tokenIdx = 0;

  let wordIdx = 0;
  let charEnd: number | null = null;
  let matching = false;

  for (let i = tokenIdx; i < tokens.length; i += 1) {
    const token = tokens[i]!;

    if (wordIdx >= speakableSegmentWords.length) {
      if (matching && token.charStart < segmentTextEnd && !isSpeakableWordText(token.text)) {
        charEnd = token.charEnd;
        continue;
      }
      break;
    }

    const targetWord = speakableSegmentWords[wordIdx]!;

    if (!isSpeakableWordText(token.text)) {
      if (matching) charEnd = token.charEnd;
      continue;
    }

    const tokenText = collapseSpaces(token.text);
    const targetText = collapseSpaces(targetWord.text);
    if (tokenText === targetText) {
      charEnd = token.charEnd;
      matching = true;
      wordIdx += 1;
    } else if (!matching) {
      continue;
    } else {
      break;
    }
  }

  if (!matching || wordIdx < speakableSegmentWords.length) {
    return fallbackRange;
  }

  return {
    paragraphId: anchor.paragraphId,
    charStart: anchor.textIndex,
    charEnd: Math.min(charEnd ?? segmentTextEnd, segmentTextEnd),
    segmentId: segment.id,
  };
}

/** 解析单个已选片段在文案分段中的字符高亮区间（words 匹配，标点随相邻字一并高亮） */
export function resolveSelectedCopyHighlightCharRange(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[]
): SelectedCopyHighlightRange | null {
  const anchor = resolveSegmentParagraphAnchor(segment, paragraphs);
  if (anchor) return resolveHighlightFromAnchor(anchor, segment, paragraphs);

  if (!segment.sourceParagraphId) return null;

  const paragraph = paragraphs.find((item) => item.id === segment.sourceParagraphId);
  if (!paragraph) return null;

  const paragraphText = getParagraphText(paragraph);
  const allWords = paragraph.segments.flatMap((item) => item.words ?? []);
  const textIndex = findBestSegmentTextIndex(
    paragraphText,
    segment.text,
    getCopySegmentOriginStart(segment),
    getCopySegmentOriginEnd(segment),
    allWords
  );

  if (textIndex < 0) {
    const plainIndex = paragraphText.indexOf(segment.text);
    if (plainIndex < 0) return null;
    return {
      paragraphId: paragraph.id,
      charStart: plainIndex,
      charEnd: plainIndex + segment.text.length,
      segmentId: segment.id,
    };
  }

  return resolveHighlightFromAnchor(
    {
      paragraphId: paragraph.id,
      paragraphText,
      textIndex,
      allWords,
    },
    segment,
    paragraphs
  );
}

export function buildSelectedCopyHighlightRanges(
  segments: SelectedCopySegment[],
  paragraphs: TranscriptParagraph[]
): SelectedCopyHighlightRange[] {
  return segments
    .map((segment) => resolveSelectedCopyHighlightCharRange(segment, paragraphs))
    .filter((range): range is SelectedCopyHighlightRange => range != null);
}

function getCopySegmentOriginStart(segment: SelectedCopySegment) {
  return Number.isFinite(segment.originStart) ? Number(segment.originStart) : segment.start;
}

function getCopySegmentOriginEnd(segment: SelectedCopySegment) {
  return Number.isFinite(segment.originEnd) ? Number(segment.originEnd) : segment.end;
}

/** 将选段内字符偏移映射到段落原文（兼容空格差异） */
function mapSegmentCharToParagraphChar(
  paragraphText: string,
  segmentText: string,
  textIndex: number,
  segmentCharOffset: number
): number {
  if (segmentCharOffset <= 0) return textIndex;
  if (segmentCharOffset >= segmentText.length) return textIndex + segmentText.length;

  const paragraphSlice = paragraphText.slice(textIndex, textIndex + segmentText.length);
  if (paragraphSlice === segmentText) return textIndex + segmentCharOffset;

  const targetNormLen = collapseSpaces(segmentText.slice(0, segmentCharOffset)).length;
  if (!targetNormLen) return textIndex;

  let normCount = 0;
  for (let i = textIndex; i < paragraphText.length; i += 1) {
    if (/\s/.test(paragraphText[i]!)) continue;
    normCount += 1;
    if (normCount >= targetNormLen) return i + 1;
  }

  return textIndex + segmentCharOffset;
}

function resolveSegmentParagraphAnchor(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[],
  options?: { ignoreSpeaker?: boolean }
): SegmentParagraphAnchor | null {
  const timeStart = getCopySegmentOriginStart(segment);
  const timeEnd = getCopySegmentOriginEnd(segment);

  const candidates = segment.sourceParagraphId
    ? paragraphs.filter((paragraph) => paragraph.id === segment.sourceParagraphId)
    : paragraphs;

  for (const paragraph of candidates) {
    if (!options?.ignoreSpeaker && paragraph.speaker !== segment.speaker) continue;

    const paragraphText = getParagraphText(paragraph);
    const allWords = paragraph.segments.flatMap((item) => item.words ?? []);
    if (!allWords.length) continue;

    const textIndex = findBestSegmentTextIndex(
      paragraphText,
      segment.text,
      timeStart,
      timeEnd,
      allWords
    );
    if (textIndex >= 0) {
      return { paragraphId: paragraph.id, paragraphText, textIndex, allWords };
    }
  }

  return null;
}

/** 为已选片段解析对应文案分段 id（clips1 回显、高亮定位共用） */
export function resolveSourceParagraphIdForCopySegment(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[]
): string | null {
  if (segment.sourceParagraphId) {
    const existing = paragraphs.find((paragraph) => paragraph.id === segment.sourceParagraphId);
    if (existing) return existing.id;
  }

  const anchor = resolveSegmentParagraphAnchor(segment, paragraphs, { ignoreSpeaker: true });
  if (anchor) return anchor.paragraphId;

  const timeStart = getCopySegmentOriginStart(segment);
  const timeEnd = getCopySegmentOriginEnd(segment);
  const trimmedText = segment.text.trim();

  for (const paragraph of paragraphs) {
    if (!isParagraphOverlappingTimeRange(paragraph, timeStart, timeEnd)) continue;
    const paragraphText = getParagraphText(paragraph);
    if (trimmedText && paragraphText.includes(trimmedText)) {
      return paragraph.id;
    }
  }

  return null;
}

/** 回显/加载时为片段补齐 sourceParagraphId 及说话人信息 */
export function attachSourceParagraphIdToCopySegment(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[]
): SelectedCopySegment {
  const paragraphId = resolveSourceParagraphIdForCopySegment(segment, paragraphs);
  if (!paragraphId) return segment;

  const paragraph = paragraphs.find((item) => item.id === paragraphId);
  if (!paragraph) return segment;

  if (
    segment.sourceParagraphId === paragraphId &&
    segment.speaker === paragraph.speaker &&
    segment.speakerName === paragraph.speakerName
  ) {
    return segment;
  }

  return {
    ...segment,
    sourceParagraphId: paragraphId,
    speaker: paragraph.speaker,
    speakerName: paragraph.speakerName,
  };
}

function boundsFromParagraphCharRange(
  anchor: SegmentParagraphAnchor,
  segmentText: string,
  segmentCharStart: number,
  segmentCharEnd: number,
  fallbackStart: number,
  fallbackEnd: number
): { start: number; end: number } {
  const paragraphCharStart = mapSegmentCharToParagraphChar(
    anchor.paragraphText,
    segmentText,
    anchor.textIndex,
    segmentCharStart
  );
  const paragraphCharEnd = mapSegmentCharToParagraphChar(
    anchor.paragraphText,
    segmentText,
    anchor.textIndex,
    segmentCharEnd
  );

  const duration = fallbackEnd - fallbackStart;
  const linearAt = (offset: number) =>
    fallbackStart + (duration * offset) / Math.max(segmentText.length, 1);

  const timeWords = filterWordsByTimeRange(anchor.allWords, fallbackStart, fallbackEnd);
  const rangeWords = collectAlignedWordsInCharRange(
    timeWords.length ? timeWords : anchor.allWords,
    anchor.paragraphText,
    paragraphCharStart,
    paragraphCharEnd
  );

  let bounds = rangeFromWords(
    rangeWords,
    linearAt(segmentCharStart),
    linearAt(segmentCharEnd)
  );

  bounds = enhanceBoundsForKeptPrefix(
    bounds,
    segmentText,
    segmentCharEnd,
    timeWords.length ? timeWords : anchor.allWords,
    anchor.paragraphText,
    anchor.textIndex
  );

  return bounds;
}

type PartialDeleteBoundsContext = {
  segmentText: string;
  sourceText: string;
  words: TranscriptWord[];
  fallbackStart: number;
  fallbackEnd: number;
  mapCharOffset: (offset: number) => number;
};

function resolvePartialDeleteBoundsContext(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[],
  explicitWords: TranscriptWord[] = []
): PartialDeleteBoundsContext {
  const segmentText = segment.text;
  const fallbackStart = getCopySegmentOriginStart(segment);
  const fallbackEnd = getCopySegmentOriginEnd(segment);
  const anchor = resolveSegmentParagraphAnchor(segment, paragraphs);

  if (anchor) {
    const timeWords = filterWordsByTimeRange(anchor.allWords, fallbackStart, fallbackEnd);
    return {
      segmentText,
      sourceText: anchor.paragraphText,
      words: timeWords.length ? timeWords : anchor.allWords,
      fallbackStart,
      fallbackEnd,
      mapCharOffset: (offset) =>
        mapSegmentCharToParagraphChar(
          anchor.paragraphText,
          segmentText,
          anchor.textIndex,
          offset
        ),
    };
  }

  const resolvedWords = resolveCopySegmentWords(segment, paragraphs);
  const candidateWords = resolvedWords.length ? resolvedWords : explicitWords;
  const timeWords = filterWordsByTimeRange(candidateWords, fallbackStart, fallbackEnd);

  return {
    segmentText,
    sourceText: segmentText,
    words: timeWords.length ? timeWords : candidateWords,
    fallbackStart,
    fallbackEnd,
    mapCharOffset: (offset) => offset,
  };
}

function linearTimeAtCharOffset(ctx: PartialDeleteBoundsContext, charOffset: number): number {
  const duration = ctx.fallbackEnd - ctx.fallbackStart;
  return (
    ctx.fallbackStart +
    (duration * charOffset) / Math.max(ctx.segmentText.length, 1)
  );
}

type AlignedWordPosition = AlignedWordSpan;

/** 在原文区间内按时间顺序对齐字词字符位置 */
function alignWordsInSourceRange(
  sourceText: string,
  words: TranscriptWord[],
  rangeStart: number,
  rangeEnd: number
): AlignedWordPosition[] {
  if (!words.length || rangeEnd <= rangeStart || !sourceText) return [];

  return alignWordsToSourceText(sourceText, words).filter(
    (item) => item.charStart < rangeEnd && item.charEnd > rangeStart
  );
}

function interpolateWordTimeAtChar(item: AlignedWordPosition, charOffset: number): number {
  return interpolateWordTimeAtSpan(item, charOffset);
}

/** 删除边界之前：保留前缀的最晚发声结束时间 */
function endTimeBeforeCharOffset(ctx: PartialDeleteBoundsContext, charOffset: number): number {
  if (charOffset <= 0) return ctx.fallbackStart;
  if (charOffset >= ctx.segmentText.length) return ctx.fallbackEnd;

  const rangeStart = ctx.mapCharOffset(0);
  const rangeEnd = ctx.mapCharOffset(ctx.segmentText.length);
  const boundary = ctx.mapCharOffset(charOffset);
  const aligned = alignWordsInSourceRange(ctx.sourceText, ctx.words, rangeStart, rangeEnd);

  let bestEnd: number | null = null;
  let bestCharEnd = -1;

  for (const item of aligned) {
    if (!isTimingTranscriptWord(item.word)) continue;
    if (item.charEnd <= boundary && item.charEnd > bestCharEnd) {
      bestCharEnd = item.charEnd;
      bestEnd = Math.max(item.word.end, item.word.start);
    }
  }

  for (const item of aligned) {
    if (!isTimingTranscriptWord(item.word)) continue;
    if (item.charStart < boundary && item.charEnd > boundary) {
      const interpolated = interpolateWordTimeAtChar(item, boundary);
      bestEnd = bestEnd == null ? interpolated : Math.max(bestEnd, interpolated);
    }
  }

  if (bestEnd != null) return bestEnd;
  return linearTimeAtCharOffset(ctx, charOffset);
}

/** 删除边界之后：保留后缀的最早发声开始时间 */
function startTimeAfterCharOffset(ctx: PartialDeleteBoundsContext, charOffset: number): number {
  if (charOffset <= 0) return ctx.fallbackStart;
  if (charOffset >= ctx.segmentText.length) return ctx.fallbackEnd;

  const rangeStart = ctx.mapCharOffset(0);
  const rangeEnd = ctx.mapCharOffset(ctx.segmentText.length);
  const boundary = ctx.mapCharOffset(charOffset);
  const aligned = alignWordsInSourceRange(ctx.sourceText, ctx.words, rangeStart, rangeEnd);

  for (const item of aligned) {
    if (!isTimingTranscriptWord(item.word)) continue;
    if (item.charStart >= boundary) {
      return item.word.start;
    }
  }

  for (const item of aligned) {
    if (!isTimingTranscriptWord(item.word)) continue;
    if (item.charStart < boundary && item.charEnd > boundary) {
      return interpolateWordTimeAtChar(item, boundary);
    }
  }

  return linearTimeAtCharOffset(ctx, charOffset);
}

/** 部分删除拆分片段的时间边界（与文案选片分离） */
function boundsForPartialDeletePiece(
  ctx: PartialDeleteBoundsContext,
  charStart: number,
  charEnd: number
): { start: number; end: number } {
  const atStart = charStart <= 0;
  const atEnd = charEnd >= ctx.segmentText.length;

  if (atStart && atEnd) {
    return { start: ctx.fallbackStart, end: ctx.fallbackEnd };
  }

  if (atStart) {
    return {
      start: ctx.fallbackStart,
      end: endTimeBeforeCharOffset(ctx, charEnd),
    };
  }

  if (atEnd) {
    return {
      start: startTimeAfterCharOffset(ctx, charStart),
      end: endTimeBeforeCharOffset(ctx, charEnd),
    };
  }

  return {
    start: startTimeAfterCharOffset(ctx, charStart),
    end: endTimeBeforeCharOffset(ctx, charEnd),
  };
}

/** 从 transcript 中解析 copy 片段对应的字级时间轴 */
export function resolveCopySegmentWords(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[]
): TranscriptWord[] {
  const timeStart = getCopySegmentOriginStart(segment);
  const timeEnd = getCopySegmentOriginEnd(segment);
  const anchor = resolveSegmentParagraphAnchor(segment, paragraphs);

  if (anchor) {
    return sliceWordsByCharRange(
      anchor.allWords,
      anchor.textIndex,
      anchor.textIndex + segment.text.length,
      anchor.paragraphText
    );
  }

  for (const paragraph of paragraphs) {
    if (paragraph.speaker !== segment.speaker) continue;

    const paragraphText = getParagraphText(paragraph);
    const allWords = paragraph.segments.flatMap((item) => item.words ?? []);
    if (!allWords.length) continue;

    const textIndex = findBestSegmentTextIndex(
      paragraphText,
      segment.text,
      timeStart,
      timeEnd,
      allWords
    );
    if (textIndex >= 0) {
      return sliceWordsByCharRange(allWords, textIndex, textIndex + segment.text.length, paragraphText);
    }
  }

  for (const paragraph of paragraphs) {
    if (paragraph.speaker !== segment.speaker) continue;

    const allWords = paragraph.segments.flatMap((item) => item.words ?? []);
    const inRange = allWords
      .filter(
        (word) =>
          isTimingTranscriptWord(word) &&
          word.end > timeStart - 0.1 &&
          word.start < timeEnd + 0.1
      )
      .sort((a, b) => a.start - b.start || a.end - b.end);

    if (!inRange.length) continue;

    const aligned = sliceWordsByCharRange(inRange, 0, segment.text.length, segment.text);
    if (aligned.length) return aligned;

    return inRange;
  }

  return [];
}

function findBestSegmentTextIndex(
  paragraphText: string,
  segmentText: string,
  segmentStart: number,
  segmentEnd: number,
  allWords: TranscriptWord[]
): number {
  if (!segmentText) return -1;

  let bestIndex = -1;
  let bestOverlap = -1;

  for (const textIndex of findAllSegmentTextStarts(paragraphText, segmentText)) {
    const candidateWords = sliceWordsByCharRange(
      allWords,
      textIndex,
      textIndex + segmentText.length,
      paragraphText
    );
    const range = rangeFromWords(candidateWords, segmentStart, segmentEnd);
    const overlapStart = Math.max(range.start, segmentStart);
    const overlapEnd = Math.min(range.end, segmentEnd);
    const overlap = overlapEnd - overlapStart;

    if (overlap > bestOverlap) {
      bestOverlap = overlap;
      bestIndex = textIndex;
    }
  }

  return bestIndex;
}

function buildSplitCopySegment(
  segment: SelectedCopySegment,
  text: string,
  charStart: number,
  charEnd: number,
  words: TranscriptWord[],
  paragraphs: TranscriptParagraph[],
  overrides: Partial<SelectedCopySegment>
): SelectedCopySegment {
  const ctx = resolvePartialDeleteBoundsContext(segment, paragraphs, words);
  const bounds = boundsForPartialDeletePiece(ctx, charStart, charEnd);

  return {
    ...segment,
    ...overrides,
    text,
    start: bounds.start,
    end: bounds.end,
    originStart: bounds.start,
    originEnd: bounds.end,
  };
}

export function deleteSelectedRangeFromSegment(
  segment: SelectedCopySegment,
  selectionStart: number,
  selectionEnd: number,
  _words: TranscriptWord[] = [],
  paragraphs: TranscriptParagraph[] = []
): SelectedCopySegment[] | 'delete-all' | null {
  const text = segment.text;

  if (selectionStart < 0 || selectionEnd > text.length || selectionStart >= selectionEnd) {
    return null;
  }

  if (selectionStart === 0 && selectionEnd === text.length) {
    return 'delete-all';
  }

  const beforeText = text.slice(0, selectionStart);
  const afterText = text.slice(selectionEnd);
  const selectedText = text.slice(selectionStart, selectionEnd);

  // 仅删标点：不拆分、不重算字级时间（标点本身无 ASR 时间轴）
  if (isPunctuationOnlySelection(selectedText)) {
    const newText = beforeText + afterText;
    if (!newText.trim() || isPunctuationOnlySelection(newText)) return 'delete-all';
    return [{ ...segment, text: newText }];
  }

  const { before: keptBefore, after: keptAfter } = dropPunctuationOnlySplitPieces(
    beforeText,
    afterText
  );

  if (!keptBefore && !keptAfter) return 'delete-all';

  if (keptBefore && keptAfter) {
    return [
      buildSplitCopySegment(segment, keptBefore, 0, selectionStart, _words, paragraphs, {
        id: `${segment.id}-a-${Date.now()}`,
      }),
      buildSplitCopySegment(segment, keptAfter, selectionEnd, text.length, _words, paragraphs, {
        id: `${segment.id}-b-${Date.now()}`,
      }),
    ];
  }

  if (keptBefore) {
    return [
      buildSplitCopySegment(segment, keptBefore, 0, selectionStart, _words, paragraphs, {}),
    ];
  }

  if (keptAfter) {
    return [
      buildSplitCopySegment(segment, keptAfter, selectionEnd, text.length, _words, paragraphs, {}),
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

/** 将播放区间收束到文案留白边界内，避免历史数据或 ASR 误差带入下一句前音 */
export function clampCopySegmentPlaybackBounds(
  segment: SelectedCopySegment,
  paragraphs: TranscriptParagraph[],
  videoDuration: number
): SelectedCopySegment {
  const originStart = getSegmentOriginStart(segment);
  const originEnd = getSegmentOriginEnd(segment);
  const originEndForPad = tightenOriginEndAgainstNextSpeech(paragraphs, originEnd);
  const { lowerBound, upperBound } = getTranscriptPadBounds(
    paragraphs,
    originStart,
    originEndForPad,
    videoDuration
  );

  const EPS = 1e-3;
  let start = segment.start;
  let end = segment.end;

  if (start < lowerBound - EPS) start = lowerBound;
  if (start > originStart + EPS) start = originStart;
  if (end > upperBound + EPS) end = upperBound;
  if (end < originEnd - EPS) end = originEnd;

  if (Math.abs(start - segment.start) < EPS && Math.abs(end - segment.end) < EPS) {
    return segment;
  }

  return {
    ...segment,
    originStart,
    originEnd,
    start,
    end,
  };
}

export function sanitizeSelectedCopySegments(
  segments: SelectedCopySegment[],
  paragraphs: TranscriptParagraph[],
  videoDuration: number
): SelectedCopySegment[] {
  if (!segments.length) return segments;
  return segments.map((segment) => {
    const clamped = clampCopySegmentPlaybackBounds(segment, paragraphs, videoDuration);
    return attachSourceParagraphIdToCopySegment(clamped, paragraphs);
  });
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

/**
 * 计算新片段 A 应插入到已选列表中的下标。
 * 插入到片段 X 之后，使 X.end 与 A.start 的时间间隔尽量小：
 * 1. 优先取满足 X.end <= A.start 且 X.end 最大的 X（最近前序）；
 * 2. 若 A 早于全部已有片段，则插到开头；
 * 3. 若与已有片段重叠，取 X.end - A.start 最小且 >= 0 的 X。
 */
export function findInsertIndexByTimelineProximity(
  segments: SelectedCopySegment[],
  aStart: number
): number {
  if (segments.length === 0) return 0;

  const minStart = Math.min(...segments.map((item) => item.start));
  if (aStart < minStart) return 0;

  let predecessorIndex = -1;
  let maxEnd = -Infinity;
  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i];
    if (!segment) continue;
    const end = segment.end;
    if (end <= aStart && end > maxEnd) {
      maxEnd = end;
      predecessorIndex = i;
    }
  }
  if (predecessorIndex >= 0) {
    return predecessorIndex + 1;
  }

  let bestIndex = segments.length;
  let bestGap = Infinity;
  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i];
    if (!segment) continue;
    const gap = segment.end - aStart;
    if (gap >= 0 && gap < bestGap) {
      bestGap = gap;
      bestIndex = i;
    }
  }
  if (bestGap < Infinity) {
    return bestIndex + 1;
  }

  return segments.length;
}

/** 按时间邻近规则将新片段插入已选列表（支持批量） */
export function insertSegmentsByTimelineProximity(
  segments: SelectedCopySegment[],
  incoming: SelectedCopySegment | SelectedCopySegment[]
): SelectedCopySegment[] {
  const items = Array.isArray(incoming) ? incoming : [incoming];
  return items.reduce<SelectedCopySegment[]>((acc, segment) => {
    const index = findInsertIndexByTimelineProximity(acc, segment.start);
    const next = [...acc];
    next.splice(index, 0, segment);
    return next;
  }, segments);
}

export function reorderSegments(
  segments: SelectedCopySegment[],
  fromIndex: number,
  toIndex: number
) {
  if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0) return segments;
  if (fromIndex >= segments.length || toIndex > segments.length) return segments;

  const next = [...segments];
  const [moved] = next.splice(fromIndex, 1);
  if (!moved) return segments;

  if (toIndex >= next.length) {
    next.push(moved);
    return next;
  }

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
