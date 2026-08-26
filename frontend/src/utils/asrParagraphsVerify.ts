import type { LiveAsrSegment, LiveAsrWord } from '~/services/sourceVideo.model';

const TIME_EPS_MS = 50;

export interface AsrParagraphsVerifyIssue {
  paragraphIndex?: number;
  liveIndexStart?: number;
  liveIndexEnd?: number;
  wordIndex?: number;
  message: string;
  paragraph?: LiveAsrWord | LiveAsrSegment | Record<string, unknown>;
  live?: LiveAsrWord | LiveAsrSegment | Record<string, unknown>;
  diffFields?: string[];
}

export interface AsrParagraphsVerifyReport {
  ok: boolean;
  /** 首个差异；无问题则为 null */
  firstIssue: AsrParagraphsVerifyIssue | null;
}

export interface WordSegmentLocation {
  /** 排序后段数组下标（0-based） */
  segmentIndex: number;
  /** 段内 words 下标（0-based） */
  wordIndexInSegment: number;
  text: string;
  start_time: number;
  end_time: number;
  speaker: string;
}

export interface AsrParagraphsVerifyDebugPayload {
  materialId: string | number;
  message: string | null;
  issueLevel: 'word' | 'paragraph' | 'live_segment' | null;
  wordIndex: number | null;
  paragraphIndex: number | null;
  diffFields?: string[];
  comparison: Array<Record<string, unknown>> | null;
  /** word 差异时，两边各自所属段的一维定位 */
  wordSegmentContext: {
    asr_paragraphs: WordSegmentLocation | null;
    live_asr: WordSegmentLocation | null;
  } | null;
  /** 段级差异时，asr_paragraphs 段与 live_asr 对应子句的完整内容（含各段 words） */
  paragraphSegmentContext: {
    asr_paragraphs: LiveAsrSegment | null;
    live_asr: LiveAsrSegment[];
  } | null;
  /** 按段顺序扁平化后的 words（校验实际使用的数据） */
  paragraphWords: LiveAsrWord[];
  liveWords: LiveAsrWord[];
  firstIssue: AsrParagraphsVerifyIssue | null;
}

declare global {
  interface Window {
    __ASR_VERIFY_DEBUG__?: AsrParagraphsVerifyDebugPayload;
  }
}

function sortAsrSegments(segments: LiveAsrSegment[]): LiveAsrSegment[] {
  return [...segments].sort(
    (a, b) => a.start_time - b.start_time || a.end_time - b.end_time || a.text.localeCompare(b.text)
  );
}

/** 将扁平 words 下标映射回所属段（按 start_time 排序后的段序） */
export function locateWordInSegments(
  segments: LiveAsrSegment[],
  flatWordIndex: number
): WordSegmentLocation | null {
  const sorted = sortAsrSegments(segments);
  let cursor = 0;

  for (let segmentIndex = 0; segmentIndex < sorted.length; segmentIndex += 1) {
    const segment = sorted[segmentIndex]!;
    const words = segment.words ?? [];
    if (flatWordIndex < cursor + words.length) {
      return {
        segmentIndex,
        wordIndexInSegment: flatWordIndex - cursor,
        text: segment.text,
        start_time: segment.start_time,
        end_time: segment.end_time,
        speaker: segment.speaker,
      };
    }
    cursor += words.length;
  }

  return null;
}

function buildWordSegmentContext(
  issue: AsrParagraphsVerifyIssue,
  paragraphs: LiveAsrSegment[],
  live: LiveAsrSegment[]
) {
  if (issue.wordIndex == null) return null;

  return {
    asr_paragraphs: locateWordInSegments(paragraphs, issue.wordIndex),
    live_asr: locateWordInSegments(live, issue.wordIndex),
  };
}

function buildParagraphSegmentContext(
  issue: AsrParagraphsVerifyIssue,
  paragraphs: LiveAsrSegment[],
  live: LiveAsrSegment[]
): AsrParagraphsVerifyDebugPayload['paragraphSegmentContext'] {
  if (issue.paragraphIndex == null) return null;

  const sortedParagraphs = sortAsrSegments(paragraphs);
  const sortedLive = sortAsrSegments(live);
  const asrParagraph = sortedParagraphs[issue.paragraphIndex] ?? null;
  const liveAsr =
    issue.liveIndexStart != null && issue.liveIndexEnd != null
      ? sortedLive.slice(issue.liveIndexStart, issue.liveIndexEnd + 1)
      : [];

  return { asr_paragraphs: asrParagraph, live_asr: liveAsr };
}

function summarizeLiveGroup(group: LiveAsrSegment[]) {
  const first = group[0];
  const last = group[group.length - 1];

  return {
    text: group.map((item) => item.text).join(''),
    start_time: first?.start_time,
    end_time: last?.end_time,
    segments: group.map((item) => ({
      text: item.text,
      start_time: item.start_time,
      end_time: item.end_time,
      speaker: item.speaker,
      words: item.words ?? [],
    })),
  };
}

function summarizeAsrParagraph(paragraph: LiveAsrSegment) {
  return {
    text: paragraph.text,
    start_time: paragraph.start_time,
    end_time: paragraph.end_time,
    speaker: paragraph.speaker,
    words: paragraph.words ?? [],
  };
}

function getIssueLevel(issue: AsrParagraphsVerifyIssue): 'word' | 'paragraph' | 'live_segment' {
  if (issue.wordIndex != null) return 'word';
  if (issue.paragraphIndex != null) return 'paragraph';
  return 'live_segment';
}

function buildFirstIssueComparisonRows(
  issue: AsrParagraphsVerifyIssue,
  wordSegmentContext: AsrParagraphsVerifyDebugPayload['wordSegmentContext'],
  paragraphSegmentContext: AsrParagraphsVerifyDebugPayload['paragraphSegmentContext']
): Array<Record<string, unknown>> | null {
  const level = getIssueLevel(issue);

  if (level === 'word') {
    const paragraph = issue.paragraph;
    const live = issue.live;
    if (!paragraph || !live) return null;

    const paragraphRow = typeof paragraph === 'object' ? paragraph : { value: paragraph };
    const liveRow = typeof live === 'object' ? live : { value: live };
    const wordIndex = issue.wordIndex!;
    const paragraphSegment = wordSegmentContext?.asr_paragraphs;
    const liveSegment = wordSegmentContext?.live_asr;

    return [
      {
        差异类型: 'word',
        wordIndex,
        来源: 'asr_paragraphs',
        段index: paragraphSegment?.segmentIndex ?? '-',
        段内wordIndex: paragraphSegment?.wordIndexInSegment ?? '-',
        段text: paragraphSegment?.text ?? '-',
        段start_time: paragraphSegment?.start_time ?? '-',
        段end_time: paragraphSegment?.end_time ?? '-',
        ...paragraphRow,
      },
      {
        差异类型: 'word',
        wordIndex,
        来源: 'live_asr',
        段index: liveSegment?.segmentIndex ?? '-',
        段内wordIndex: liveSegment?.wordIndexInSegment ?? '-',
        段text: liveSegment?.text ?? '-',
        段start_time: liveSegment?.start_time ?? '-',
        段end_time: liveSegment?.end_time ?? '-',
        ...liveRow,
      },
    ];
  }

  if (level === 'paragraph') {
    const asrParagraph = paragraphSegmentContext?.asr_paragraphs;
    const liveSegments = paragraphSegmentContext?.live_asr ?? [];
    const asrSummary = asrParagraph ? summarizeAsrParagraph(asrParagraph) : null;
    const liveSummary = liveSegments.length ? summarizeLiveGroup(liveSegments) : null;

    return [
      {
        差异类型: '段级',
        paragraphIndex: issue.paragraphIndex,
        来源: 'asr_paragraphs',
        text: asrSummary?.text ?? '-',
        start_time: asrSummary?.start_time ?? '-',
        end_time: asrSummary?.end_time ?? '-',
        speaker: asrSummary?.speaker ?? '-',
        wordCount: asrSummary?.words.length ?? 0,
      },
      {
        差异类型: '段级',
        paragraphIndex: issue.paragraphIndex,
        来源: 'live_asr',
        liveIndexStart: issue.liveIndexStart ?? '-',
        liveIndexEnd: issue.liveIndexEnd ?? '-',
        text: liveSummary?.text ?? '-',
        start_time: liveSummary?.start_time ?? '-',
        end_time: liveSummary?.end_time ?? '-',
        liveSegmentCount: liveSegments.length,
        wordCount: liveSummary?.segments.reduce(
          (sum, segment) => sum + (segment.words?.length ?? 0),
          0
        ) ?? 0,
        liveSegments: liveSummary?.segments ?? [],
      },
    ];
  }

  const live = issue.live;
  if (!live) return null;
  const liveRow = typeof live === 'object' ? live : { value: live };

  return [
    {
      差异类型: 'live 段级',
      来源: 'live_asr',
      liveIndexStart: issue.liveIndexStart ?? '-',
      liveIndexEnd: issue.liveIndexEnd ?? '-',
      ...liveRow,
    },
  ];
}

function buildAsrVerifyDebugPayload(
  materialId: string | number,
  report: AsrParagraphsVerifyReport,
  paragraphs: LiveAsrSegment[],
  live: LiveAsrSegment[],
  paragraphWords: LiveAsrWord[],
  liveWords: LiveAsrWord[]
): AsrParagraphsVerifyDebugPayload {
  const issue = report.firstIssue;
  const wordSegmentContext = issue ? buildWordSegmentContext(issue, paragraphs, live) : null;
  const paragraphSegmentContext = issue
    ? buildParagraphSegmentContext(issue, paragraphs, live)
    : null;
  const issueLevel = issue ? getIssueLevel(issue) : null;

  return {
    materialId,
    message: issue?.message ?? null,
    issueLevel,
    wordIndex: issue?.wordIndex ?? null,
    paragraphIndex: issue?.paragraphIndex ?? null,
    diffFields: issue?.diffFields,
    wordSegmentContext,
    paragraphSegmentContext,
    paragraphWords,
    liveWords,
    comparison: issue
      ? buildFirstIssueComparisonRows(issue, wordSegmentContext, paragraphSegmentContext)
      : null,
    firstIssue: issue,
  };
}

function formatFlatWordsForLog(words: LiveAsrWord[], offset = 0) {
  return words.map((word, index) => ({
    index: offset + index,
    text: word.text,
    start_time: word.start_time,
    end_time: word.end_time,
  }));
}

function logFlatWords(
  paragraphWords: LiveAsrWord[],
  liveWords: LiveAsrWord[],
  wordIndex: number | null
) {
  // console.log('asr_paragraphs words', formatFlatWordsForLog(paragraphWords));
  // console.log('live_asr words', formatFlatWordsForLog(liveWords));

  if (wordIndex == null) return;

  const radius = 5;
  const start = Math.max(0, wordIndex - radius);
  const end = Math.min(paragraphWords.length, liveWords.length, wordIndex + radius + 1);
  console.log(`差异附近 words [${start}, ${end})`, {
    asr_paragraphs: formatFlatWordsForLog(paragraphWords.slice(start, end), start),
    live_asr: formatFlatWordsForLog(liveWords.slice(start, end), start),
  });
}

function logFirstIssueComparison(
  issue: AsrParagraphsVerifyIssue,
  wordSegmentContext: AsrParagraphsVerifyDebugPayload['wordSegmentContext'],
  paragraphSegmentContext: AsrParagraphsVerifyDebugPayload['paragraphSegmentContext']
) {
  const level = getIssueLevel(issue);
  if (level === 'word' && wordSegmentContext) {
    console.log('word 所属段', wordSegmentContext);
  } else if (level === 'paragraph') {
    console.log(
      '段级差异定位',
      `asr_paragraphs index ${issue.paragraphIndex}`,
      issue.liveIndexStart != null
        ? `↔ live_asr index ${issue.liveIndexStart}${issue.liveIndexEnd != null && issue.liveIndexEnd !== issue.liveIndexStart ? `–${issue.liveIndexEnd}` : ''}`
        : ''
    );
    if (paragraphSegmentContext?.asr_paragraphs) {
      console.log('asr_paragraphs 段内容', summarizeAsrParagraph(paragraphSegmentContext.asr_paragraphs));
    }
    if (paragraphSegmentContext?.live_asr.length) {
      console.log(
        'live_asr 对应段内容',
        summarizeLiveGroup(paragraphSegmentContext.live_asr)
      );
    }
  }

  const rows = buildFirstIssueComparisonRows(issue, wordSegmentContext, paragraphSegmentContext);
  if (!rows) return;
  console.table(rows);
}

function parseLiveAsrWords(raw: unknown): LiveAsrWord[] | undefined {
  if (!Array.isArray(raw)) return undefined;

  const words = raw
    .map((item) => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      const text = String(row.text ?? '');

      const start = Number(row.start_time);
      const end = Number(row.end_time);
      if (!Number.isFinite(start) || !Number.isFinite(end)) return null;

      return {
        start_time: start,
        end_time: end,
        text,
      } satisfies LiveAsrWord;
    })
    .filter((item): item is LiveAsrWord => item != null);

  return words.length ? words : undefined;
}

export function parseLiveAsrSegments(raw: unknown): LiveAsrSegment[] {
  if (!Array.isArray(raw)) return [];

  return raw
    .map((item) => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      const text = String(row.text ?? '');
      if (!text) return null;

      const start = Number(row.start_time);
      const end = Number(row.end_time);
      if (!Number.isFinite(start) || !Number.isFinite(end)) return null;

      const words = parseLiveAsrWords(row.words);
      return {
        speaker: String(row.speaker ?? ''),
        start_time: start,
        end_time: end,
        text,
        ...(words ? { words } : {}),
      } satisfies LiveAsrSegment;
    })
    .filter((item): item is LiveAsrSegment => item != null);
}

/** 按段顺序扁平化 words（段内保持接口返回顺序） */
export function flattenSegmentWords(segments: LiveAsrSegment[]): LiveAsrWord[] {
  return sortAsrSegments(segments).flatMap((segment) => segment.words ?? []);
}

/** 空格类 word：两边 text 一致时忽略 start/end 时间差异 */
function shouldIgnoreWordTiming(text: string): boolean {
  return /^\s+$/.test(text);
}

function findFirstWordIssue(
  paragraphWords: LiveAsrWord[],
  liveWords: LiveAsrWord[]
): AsrParagraphsVerifyIssue | null {
  const paragraphWordCount = paragraphWords.length;
  const liveWordCount = liveWords.length;
  if (!paragraphWordCount || !liveWordCount) {
    return {
      message: `缺少 words：asr_paragraphs ${paragraphWordCount} 个，live_asr ${liveWordCount} 个`,
    };
  }

  if (paragraphWordCount !== liveWordCount) {
    return {
      message: `words 数量不一致：asr_paragraphs ${paragraphWordCount} 个，live_asr ${liveWordCount} 个`,
    };
  }

  for (let index = 0; index < paragraphWordCount && index < liveWordCount; index += 1) {
    const paragraph = paragraphWords[index]!;
    const live = liveWords[index]!;
    const diffFields: string[] = [];

    if (paragraph.text !== live.text) diffFields.push('text');

    const ignoreTiming =
      paragraph.text === live.text &&
      shouldIgnoreWordTiming(paragraph.text);

    if (!ignoreTiming) {
      if (paragraph.start_time !== live.start_time) diffFields.push('start_time');
      if (paragraph.end_time !== live.end_time) diffFields.push('end_time');
    }

    if (!diffFields.length) continue;

    return {
      wordIndex: index,
      diffFields,
      message: `words index ${index} 首个差异（${diffFields.join('、')}）`,
      paragraph,
      live,
    };
  }

  return null;
}

function matchLiveGroupForParagraph(
  paragraph: LiveAsrSegment,
  liveAsr: LiveAsrSegment[],
  startIndex: number
): { matched: boolean; endIndex: number; group: LiveAsrSegment[]; mergedText: string } {
  const target = paragraph.text;
  if (!target) {
    return { matched: true, endIndex: startIndex, group: [], mergedText: '' };
  }

  let merged = '';
  const group: LiveAsrSegment[] = [];

  for (let index = startIndex; index < liveAsr.length; index += 1) {
    const segment = liveAsr[index]!;
    group.push(segment);
    merged += segment.text;

    if (merged === target) {
      return { matched: true, endIndex: index + 1, group, mergedText: merged };
    }
    if (merged.length > target.length) {
      return { matched: false, endIndex: index + 1, group, mergedText: merged };
    }
  }

  return {
    matched: false,
    endIndex: liveAsr.length,
    group,
    mergedText: merged,
  };
}

function findFirstSegmentIssue(
  paragraphs: LiveAsrSegment[],
  live: LiveAsrSegment[]
): AsrParagraphsVerifyIssue | null {
  let liveIndex = 0;

  for (let paragraphIndex = 0; paragraphIndex < paragraphs.length; paragraphIndex += 1) {
    const paragraph = paragraphs[paragraphIndex]!;
    const match = matchLiveGroupForParagraph(paragraph, live, liveIndex);
    liveIndex = match.endIndex;

    if (!match.group.length && paragraph.text) {
      return {
        paragraphIndex,
        message: `asr_paragraphs index ${paragraphIndex} 找不到可合并的 live_asr 子句`,
        paragraph: summarizeAsrParagraph(paragraph),
      };
    }

    if (match.group.length && !match.matched) {
      return {
        paragraphIndex,
        liveIndexStart: liveIndex - match.group.length,
        liveIndexEnd: liveIndex - 1,
        message: `asr_paragraphs index ${paragraphIndex} 文案与 live_asr 子句合并不一致`,
        paragraph: summarizeAsrParagraph(paragraph),
        live: summarizeLiveGroup(match.group),
      };
    }

    const firstLive = match.group[0];
    const lastLive = match.group[match.group.length - 1];

    if (firstLive && Math.abs(paragraph.start_time - firstLive.start_time) > TIME_EPS_MS) {
      return {
        paragraphIndex,
        liveIndexStart: liveIndex - match.group.length,
        liveIndexEnd: liveIndex - 1,
        message: `asr_paragraphs index ${paragraphIndex} start_time 不一致`,
        paragraph: summarizeAsrParagraph(paragraph),
        live: summarizeLiveGroup(match.group),
      };
    }

    if (lastLive && Math.abs(paragraph.end_time - lastLive.end_time) > TIME_EPS_MS) {
      return {
        paragraphIndex,
        liveIndexStart: liveIndex - match.group.length,
        liveIndexEnd: liveIndex - 1,
        message: `asr_paragraphs index ${paragraphIndex} end_time 不一致`,
        paragraph: summarizeAsrParagraph(paragraph),
        live: summarizeLiveGroup(match.group),
      };
    }

    const liveSpeakers = new Set(match.group.map((item) => item.speaker));
    if (
      match.group.length &&
      (liveSpeakers.size > 1 ||
        (liveSpeakers.size === 1 && !liveSpeakers.has(paragraph.speaker)))
    ) {
      return {
        paragraphIndex,
        message: `asr_paragraphs index ${paragraphIndex} speaker 不一致`,
        paragraph: { speaker: paragraph.speaker },
        live: { speakers: [...liveSpeakers] },
      };
    }
  }

  if (liveIndex < live.length) {
    const orphan = live[liveIndex]!;
    return {
      liveIndexStart: liveIndex,
      liveIndexEnd: live.length - 1,
      message: `live_asr index ${liveIndex} 起未并入 asr_paragraphs`,
      live: orphan,
    };
  }

  return null;
}

/**
 * 校对 asr_paragraphs 与 live_asr，仅返回首个差异点。
 */
export function verifyAsrParagraphsAgainstLiveAsr(
  asrParagraphs: LiveAsrSegment[],
  liveAsr: LiveAsrSegment[]
): AsrParagraphsVerifyReport {
  const paragraphs = sortAsrSegments(asrParagraphs);
  const live = sortAsrSegments(liveAsr);
  const paragraphWords = flattenSegmentWords(paragraphs);
  const liveWords = flattenSegmentWords(live);


  const wordIssue = findFirstWordIssue(paragraphWords, liveWords);
  if (wordIssue) {
    return { ok: false, firstIssue: wordIssue };
  }

  const segmentIssue = findFirstSegmentIssue(paragraphs, live);
  if (segmentIssue) {
    return { ok: false, firstIssue: segmentIssue };
  }

  return { ok: true, firstIssue: null };
}

export function logAsrParagraphsVerifyReport(
  materialId: string | number,
  report: AsrParagraphsVerifyReport,
  paragraphs: LiveAsrSegment[],
  live: LiveAsrSegment[]
) {
  const title = `[isdebug] ASR diff report`;
  const paragraphWords = flattenSegmentWords(paragraphs);
  const liveWords = flattenSegmentWords(live);
  const payload = buildAsrVerifyDebugPayload(
    materialId,
    report,
    paragraphs,
    live,
    paragraphWords,
    liveWords
  );

  if (typeof window !== 'undefined') {
    window.__ASR_VERIFY_DEBUG__ = payload;
  }

  const issue = report.firstIssue;
  console.group(`${title}：源视频 ${materialId} - 首个差异`);
  console.log('words 数量', `${paragraphWords.length} / ${liveWords.length}`);
  logFlatWords(paragraphWords, liveWords, issue?.wordIndex ?? null);
  if (issue) {
    console.log('说明', issue.message);
    logFirstIssueComparison(issue, payload.wordSegmentContext, payload.paragraphSegmentContext);
  }
  console.groupEnd();
  console.info(
    `[isdebug] ASR 校对已完成（源视频 ${materialId}），在控制台输入 window.__ASR_VERIFY_DEBUG__ 查看完整调试数据`
  );
}

export function verifyAsrParagraphsFieldsFromRaw(
  materialId: string | number,
  raw: Record<string, unknown>
): AsrParagraphsVerifyReport | null {
  try {
    const asrParagraphs = parseLiveAsrSegments(raw.asr_paragraphs);
    const liveAsr = parseLiveAsrSegments(raw.live_asr);
    if (!asrParagraphs.length || !liveAsr.length) {
      return null;
    }

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);
    if (!report.ok) {
      logAsrParagraphsVerifyReport(materialId, report, asrParagraphs, liveAsr);
    }
    return report;
  } catch (error) {
    console.warn(
      `[isdebug] ASR 校对失败（源视频 ${materialId}），已跳过`,
      error
    );
    return null;
  }
}
