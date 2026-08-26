import { describe, expect, it } from 'vitest';
import { findActiveSegment, getAdjacentSentenceSeekTime } from '~/utils/videoPlayerTools';
import type { SelectedCopySegment, TranscriptParagraph } from './types';
import {
  asrParagraphsToTranscriptParagraphs,
  alignWordsToTranscriptText,
  attachSourceParagraphIdToCopySegment,
  buildCopySegmentFromParagraphRange,
  buildCopySegmentsFromAiSegment,
  buildSelectedCopyHighlightRanges,
  clampCopySegmentPlaybackBounds,
  deleteSelectedRangeFromSegment,
  getParagraphText,
  getSegmentAdjustableSeconds,
  getSegmentBackPadSeconds,
  getSegmentPlaybackStopTime,
  getTranscriptPadBounds,
  hasValidWordTiming,
  isTimingTranscriptWord,
  isWhitespaceOnlyWordText,
  normalizeTranscriptParagraphs,
  paragraphToCopySegment,
  resolvePreviewCaptionLines,
  resolveSelectedCopyHighlightCharRange,
  resolveSourceParagraphIdForCopySegment,
  resolveTimingForCharRange,
  findInsertIndexByTimelineProximity,
  insertSegmentsByTimelineProximity,
  reorderSegments,
  sanitizeSelectedCopySegments,
  splitCaptionClauses,
  splitTextByHighlightRanges,
  TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC,
  SEGMENT_PLAYBACK_END_GUARD_SEC,
} from './utils';

function makeCopySegment(id: string, start = 0, end = 1): SelectedCopySegment {
  return {
    id,
    speaker: '1',
    speakerName: '说话人1',
    text: id,
    start,
    end,
  };
}

describe('findInsertIndexByTimelineProximity', () => {
  it('inserts at start when new clip is earlier than all existing clips', () => {
    const existing = [
      makeCopySegment('x1', 2874.7, 2913.2),
      makeCopySegment('x2', 2913.9, 2922.1),
    ];
    expect(findInsertIndexByTimelineProximity(existing, 1.5)).toBe(0);
  });

  it('inserts after latest predecessor in timeline', () => {
    const existing = [
      makeCopySegment('x1', 2874.7, 2913.2),
      makeCopySegment('x2', 2913.9, 2922.1),
      makeCopySegment('x3', 2922.9, 2952.5),
    ];
    expect(findInsertIndexByTimelineProximity(existing, 2940)).toBe(2);
  });

  it('inserts after overlapping clip with smallest end gap', () => {
    const existing = [
      makeCopySegment('x1', 2874.7, 2913.2),
      makeCopySegment('x2', 2913.9, 2922.1),
    ];
    expect(findInsertIndexByTimelineProximity(existing, 2900)).toBe(1);
  });

  it('appends when new clip is after all existing clips', () => {
    const existing = [makeCopySegment('x1', 100, 200), makeCopySegment('x2', 250, 300)];
    expect(findInsertIndexByTimelineProximity(existing, 350)).toBe(2);
  });
});

describe('insertSegmentsByTimelineProximity', () => {
  it('inserts multiple incoming segments sequentially by timeline proximity', () => {
    const existing = [makeCopySegment('x1', 100, 200)];
    const incoming = [makeCopySegment('a', 10, 20), makeCopySegment('b', 150, 180)];
    const next = insertSegmentsByTimelineProximity(existing, incoming);
    expect(next.map((item) => item.id)).toEqual(['a', 'b', 'x1']);
  });
});

describe('reorderSegments', () => {
  it('moves an earlier segment to the last position', () => {
    const segments = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((id) => makeCopySegment(id));
    const next = reorderSegments(segments, 3, segments.length);
    expect(next.map((item) => item.id)).toEqual(['a', 'b', 'c', 'e', 'f', 'g', 'd']);
  });

  it('keeps order when dropping after the last item at the last index', () => {
    const segments = ['a', 'b', 'c'].map((id) => makeCopySegment(id));
    const next = reorderSegments(segments, 2, segments.length);
    expect(next.map((item) => item.id)).toEqual(['a', 'b', 'c']);
  });
});

function expectDeleteSegments(
  result: SelectedCopySegment[] | 'delete-all' | null
): SelectedCopySegment[] {
  expect(Array.isArray(result)).toBe(true);
  if (!Array.isArray(result)) {
    throw new Error('expected deleteSelectedRangeFromSegment to return segments');
  }
  return result;
}

describe('alignWordsToTranscriptText', () => {
  it('covers full segment text even when some ASR words fail to align', () => {
    const text = '她两个重要的生命时刻点，来到人间。我作为一个父亲，他有两个女儿。';
    const words = [
      { start: 57.5, end: 58.2, text: '她两个重要的生命时刻点' },
      { start: 58.5, end: 59.0, text: '来到人间' },
      { start: 80.0, end: 85.0, text: '我作为一个父亲' },
      { start: 90.0, end: 95.0, text: '他有两个女儿' },
    ];

    const tokens = alignWordsToTranscriptText(text, words);
    const covered = tokens.map((token) => token.text).join('');

    expect(covered).toBe(text);
    expect(tokens.every((token) => token.charEnd > token.charStart || token.text.length === 0)).toBe(
      true
    );
  });
});

describe('whitespace word timing placeholders', () => {
  const deepseekWords = [
    { start: 128.06, end: 128.34, text: '到' },
    { start: -0.001, end: -0.001, text: ' ' },
    { start: 128.46, end: 128.78, text: 'Deepseek' },
    { start: -0.001, end: -0.001, text: ' ' },
    { start: 129.29, end: 129.45, text: '我' },
  ];

  it('keeps whitespace tokens for alignment but excludes them from timing helpers', () => {
    expect(isWhitespaceOnlyWordText(' ')).toBe(true);
    expect(hasValidWordTiming(-0.001, -0.001)).toBe(false);
    expect(isTimingTranscriptWord(deepseekWords[1]!)).toBe(false);
    expect(isTimingTranscriptWord(deepseekWords[2]!)).toBe(true);
  });

  it('imports whitespace placeholders from ASR even when start_time is -1', () => {
    const paragraphs = asrParagraphsToTranscriptParagraphs([
      {
        speaker: '1',
        start_time: 128060,
        end_time: 129450,
        text: '到 Deepseek 我',
        words: [
          { start_time: 128060, end_time: 128340, text: '到' },
          { start_time: -1, end_time: -1, text: ' ' },
          { start_time: 128460, end_time: 128780, text: 'Deepseek' },
          { start_time: -1, end_time: -1, text: ' ' },
          { start_time: 129290, end_time: 129450, text: '我' },
        ],
      },
    ]);

    const words = paragraphs[0]!.segments[0]!.words ?? [];
    expect(words.filter((word) => word.text === ' ')).toHaveLength(2);
  });

  it('does not let whitespace placeholders affect char-range timing', () => {
    const text = '到 Deepseek 我';
    const timing = resolveTimingForCharRange({
      sourceText: text,
      words: deepseekWords,
      charStart: text.indexOf('Deepseek'),
      charEnd: text.indexOf('Deepseek') + 'Deepseek'.length,
      fallbackStart: 128.06,
      fallbackEnd: 129.45,
    });

    expect(timing.start).toBeCloseTo(128.46, 2);
    expect(timing.end).toBeCloseTo(128.78, 2);
    expect(timing.words.every((word) => !isWhitespaceOnlyWordText(word.text))).toBe(true);
  });

  it('assigns zero-duration display time to whitespace tokens in alignWordsToTranscriptText', () => {
    const text = '到 Deepseek 我';
    const tokens = alignWordsToTranscriptText(text, deepseekWords);
    const spaceTokens = tokens.filter((token) => token.text === ' ');

    expect(spaceTokens.length).toBeGreaterThan(0);
    expect(spaceTokens.every((token) => token.start >= 0 && token.end >= 0)).toBe(true);
    expect(spaceTokens.every((token) => token.start === token.end)).toBe(true);
  });
});

describe('preview caption lines', () => {
  it('splits caption text by punctuation', () => {
    expect(
      splitCaptionClauses('好开始了吗？好欢迎大家！我们这个现场自带掌声啊！')
    ).toEqual(['好开始了吗？', '好欢迎大家！', '我们这个现场自带掌声啊！']);
  });

  it('shows only the current clause by segment playback progress when words are unavailable', () => {
    const text = '好开始了吗？好欢迎大家！我们这个现场自带掌声啊！';
    expect(resolvePreviewCaptionLines(text, 100, 122, 100)).toEqual(['好开始了吗？']);
    expect(resolvePreviewCaptionLines(text, 100, 122, 114)).toEqual(['好欢迎大家！']);
    expect(resolvePreviewCaptionLines(text, 100, 122, 120)).toEqual(['我们这个现场自带掌声啊！']);
  });

  it('syncs preview caption to word timings when available', () => {
    const text = '好开始了吗？好欢迎大家！我们这个现场自带掌声啊！';
    const words = [
      { start: 100, end: 105, text: '好开始了吗' },
      { start: 105, end: 110, text: '好欢迎大家' },
      { start: 110, end: 122, text: '我们这个现场自带掌声啊' },
    ];

    expect(resolvePreviewCaptionLines(text, 100, 122, 102, words)).toEqual(['好开始了吗？']);
    expect(resolvePreviewCaptionLines(text, 100, 122, 107, words)).toEqual(['好欢迎大家！']);
    expect(resolvePreviewCaptionLines(text, 100, 122, 115, words)).toEqual([
      '我们这个现场自带掌声啊！',
    ]);
  });
});

describe('copy segment selection', () => {
  const paragraphText = '属 AI 股票顾问，可以帮你盯盘、聊财报，陪你见证每一次涨停。';
  const words = [
    { start: 2218.59, end: 2218.63, text: '属' },
    { start: 2218.63, end: 2218.71, text: ' ' },
    { start: 2218.71, end: 2218.87, text: 'AI' },
    { start: 2218.87, end: 2218.95, text: ' ' },
    { start: 2218.95, end: 2219.11, text: '股票' },
    { start: 2219.11, end: 2219.27, text: '顾问' },
    { start: 2224.15, end: 2224.35, text: '涨停' },
  ];
  const paragraph: TranscriptParagraph = {
    id: 'p1',
    speaker: '1',
    speakerName: '说话人1',
    segments: [
      {
        id: 's1',
        start: 2218.59,
        end: 2224.35,
        text: paragraphText,
        words,
      },
    ],
  };

  it('uses first and last word timings for full paragraph double-click', () => {
    const segment = paragraphToCopySegment(paragraph);
    expect(segment).not.toBeNull();
    expect(segment!.start).toBeCloseTo(2218.59, 2);
    expect(segment!.end).toBeCloseTo(2224.35, 2);
    expect(segment!.text).toBe(paragraphText);
  });

  it('uses strict word timings for partial drag selection', () => {
    const aiStart = paragraphText.indexOf('AI');
    const aiEnd = aiStart + 'AI'.length;
    const segment = buildCopySegmentFromParagraphRange(paragraph, aiStart, aiEnd);

    expect(segment).not.toBeNull();
    expect(segment!.text).toBe('AI');
    expect(segment!.start).toBeCloseTo(2218.71, 2);
    expect(segment!.end).toBeCloseTo(2218.87, 2);
  });

  it('clamps copy segment timing to paragraph range when words have outlier start', () => {
    const paragraph: TranscriptParagraph = {
      id: 'p-time',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 's-time',
          start: 266.3,
          end: 270.5,
          text: 'UBC，哥伦比亚大学，多伦多大学还是 Queen?',
          words: [
            { start: 0, end: 270.5, text: 'UBC，哥伦比亚大学，多伦多大学还是 Queen' },
          ],
        },
      ],
    };

    const segment = buildCopySegmentFromParagraphRange(
      paragraph,
      0,
      paragraph.segments[0]!.text.length
    );

    expect(segment).not.toBeNull();
    expect(segment!.start).toBeCloseTo(266.3, 1);
    expect(segment!.end).toBeCloseTo(270.5, 1);
  });

  it('uses paragraph range for full selection when words only cover trailing text', () => {
    const paragraph: TranscriptParagraph = {
      id: 'p-partial-words',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 's-partial',
          start: 266.3,
          end: 270.5,
          text: 'UBC，哥伦比亚大学，多伦多大学还是 Queen?',
          words: [{ start: 270.3, end: 270.5, text: 'Queen' }],
        },
      ],
    };

    const segment = buildCopySegmentFromParagraphRange(
      paragraph,
      0,
      paragraph.segments[0]!.text.length
    );

    expect(segment).not.toBeNull();
    expect(segment!.start).toBeCloseTo(266.3, 1);
    expect(segment!.end).toBeCloseTo(270.5, 1);
  });

  it('returns null when drag selection is punctuation or whitespace only', () => {
    const commaIndex = paragraphText.indexOf('，');
    expect(commaIndex).toBeGreaterThanOrEqual(0);
    expect(buildCopySegmentFromParagraphRange(paragraph, commaIndex, commaIndex + 1)).toBeNull();

    const spaceStart = paragraphText.indexOf(' ');
    expect(spaceStart).toBeGreaterThanOrEqual(0);
    expect(buildCopySegmentFromParagraphRange(paragraph, spaceStart, spaceStart + 1)).toBeNull();
    expect(buildCopySegmentFromParagraphRange(paragraph, 0, 0)).toBeNull();
  });

  it('highlights selected copy by words and includes punctuation between matched words', () => {
    const letterParagraphText =
      '大家是我的听众，也是这次交流的朋友。我想每一个父母跟他的子女的关系，可能是人间最温暖和最重要的关系。';
    const letterWords = [
      { start: 30.4, end: 31.0, text: '大家' },
      { start: 31.0, end: 31.4, text: '是' },
      { start: 31.4, end: 31.8, text: '我的' },
      { start: 31.8, end: 32.2, text: '听众' },
      { start: 32.2, end: 32.6, text: '也是' },
      { start: 32.6, end: 33.0, text: '这次' },
      { start: 33.0, end: 33.4, text: '交流' },
      { start: 33.4, end: 33.8, text: '的' },
      { start: 33.8, end: 34.2, text: '朋友' },
      { start: 34.2, end: 34.6, text: '我想' },
      { start: 34.6, end: 35.0, text: '每一个' },
      { start: 35.0, end: 35.4, text: '父母' },
    ];
    const letterParagraph: TranscriptParagraph = {
      id: 'letter-p1',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 'letter-s1',
          start: 30.4,
          end: 50.1,
          text: letterParagraphText,
          words: letterWords,
        },
      ],
    };

    const selectionStart = letterParagraphText.indexOf('大家是我的听众');
    const selectionEnd =
      letterParagraphText.indexOf('可能是人间最温暖和最重要的关系') +
      '可能是人间最温暖和最重要的关系'.length;
    const selectionText = letterParagraphText.slice(selectionStart, selectionEnd);
    const copySegment: SelectedCopySegment = {
      id: 'copy-selected-1',
      speaker: '1',
      speakerName: '说话人1',
      text: selectionText,
      start: 30.4,
      end: 50.1,
      sourceParagraphId: 'letter-p1',
      originStart: 30.4,
      originEnd: 50.1,
    };

    const range = resolveSelectedCopyHighlightCharRange(copySegment, [letterParagraph]);
    expect(range).not.toBeNull();
    expect(range!.paragraphId).toBe('letter-p1');
    expect(range!.charStart).toBe(selectionStart);

    const highlighted = letterParagraphText.slice(range!.charStart, range!.charEnd);
    expect(highlighted).toContain('朋友。我想');
    expect(highlighted.startsWith('大家是我的听众')).toBe(true);
  });

  it('highlights from anchor text start when only trailing words align in ASR', () => {
    const paragraphText = '探寻未来十年财富流向，构建人生跃迁指南。欢迎来到财富增长课。';
    const paragraph: TranscriptParagraph = {
      id: 'intro-p1',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 'intro-s1',
          start: 0.8,
          end: 5.3,
          text: paragraphText,
          words: [
            { start: 0.8, end: 2.0, text: '探寻未来十年财富流向，' },
            { start: 2.0, end: 5.3, text: '构建人生跃迁指南' },
            { start: 7.3, end: 10.8, text: '欢迎来到财富增长课' },
          ],
        },
      ],
    };

    const copySegment: SelectedCopySegment = {
      id: 'copy-intro',
      speaker: '1',
      speakerName: '说话人1',
      text: '探寻未来十年财富流向，构建人生跃迁指南。',
      start: 0.8,
      end: 5.3,
      sourceParagraphId: 'intro-p1',
      originStart: 0.8,
      originEnd: 5.3,
    };

    const range = resolveSelectedCopyHighlightCharRange(copySegment, [paragraph]);
    expect(range).not.toBeNull();
    expect(range!.charStart).toBe(0);
    expect(range!.charEnd).toBe('探寻未来十年财富流向，构建人生跃迁指南。'.length);
    expect(paragraphText.slice(range!.charStart, range!.charEnd)).toBe(copySegment.text);
  });

  it('builds highlight ranges for all selected segments', () => {
    const p: TranscriptParagraph = {
      id: 'p-multi',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 's-multi',
          start: 0,
          end: 10,
          text: '你好，世界。',
          words: [
            { start: 0, end: 1, text: '你好' },
            { start: 1, end: 2, text: '世界' },
          ],
        },
      ],
    };
    const segA = buildCopySegmentFromParagraphRange(p, 0, 2)!;
    const segB = buildCopySegmentFromParagraphRange(p, 3, 5)!;
    const ranges = buildSelectedCopyHighlightRanges([segA, segB], [p]);
    expect(ranges).toHaveLength(2);
    expect(ranges.every((item) => item.paragraphId === 'p-multi')).toBe(true);
  });

  it('attaches sourceParagraphId when echoing clips1 without paragraph linkage', () => {
    const paragraph: TranscriptParagraph = {
      id: 'asr-p-12',
      speaker: '3',
      speakerName: '说话人：3',
      segments: [
        {
          id: 'asr-12',
          start: 2542.8,
          end: 2585.2,
          text: '而且我们刚刚过去的2026年，作为一个 K 显时代。',
          words: [
            { start: 2542.8, end: 2543.2, text: '而且' },
            { start: 2543.2, end: 2544.0, text: '我们' },
            { start: 2544.0, end: 2545.5, text: '刚刚' },
          ],
        },
      ],
    };

    const loadedClip: SelectedCopySegment = {
      id: 'manual-0-2542800-2585200',
      speaker: '1',
      speakerName: '',
      text: '而且我们刚刚过去的2026年，作为一个 K 显时代。',
      start: 2542.8,
      end: 2585.2,
      originStart: 2542.8,
      originEnd: 2585.2,
    };

    expect(resolveSourceParagraphIdForCopySegment(loadedClip, [paragraph])).toBe('asr-p-12');

    const attached = attachSourceParagraphIdToCopySegment(loadedClip, [paragraph]);
    expect(attached.sourceParagraphId).toBe('asr-p-12');
    expect(attached.speaker).toBe('3');
    expect(attached.speakerName).toBe('说话人：3');

    const sanitized = sanitizeSelectedCopySegments([loadedClip], [paragraph], 7200);
    expect(sanitized[0]!.sourceParagraphId).toBe('asr-p-12');
  });

  it('does not include adjacent word timing when selecting inner chars', () => {
    const stockStart = paragraphText.indexOf('股票');
    const stockEnd = stockStart + '股票'.length;
    const timing = resolveTimingForCharRange({
      sourceText: paragraphText,
      words,
      charStart: stockStart,
      charEnd: stockEnd,
      fallbackStart: 2218.59,
      fallbackEnd: 2224.35,
    });

    expect(timing.start).toBeCloseTo(2218.95, 2);
    expect(timing.end).toBeCloseTo(2219.11, 2);
    expect(timing.start).toBeGreaterThan(2218.87);
  });

  it('interpolates when selection splits inside a multi-char ASR word', () => {
    const fatherText = '我作为一个父亲，有的时候很难用语言来表达。';
    const fatherWords = [
      { start: 57.5, end: 80.0, text: '前文' },
      { start: 80.0, end: 98.9, text: '我作为一个父亲，有的时候' },
      { start: 98.9, end: 101.8, text: '很难用语言来表达' },
    ];
    const deleteFrom = fatherText.indexOf('为一个父亲');
    const deleteTo = deleteFrom + '为一个父亲'.length;

    const timing = resolveTimingForCharRange({
      sourceText: fatherText,
      words: fatherWords,
      charStart: deleteFrom,
      charEnd: deleteTo,
      fallbackStart: 57.5,
      fallbackEnd: 101.8,
    });

    expect(timing.start).toBeGreaterThan(80);
    expect(timing.end).toBeLessThan(98.9);
    expect(timing.end).toBeGreaterThan(timing.start);
  });
});

describe('splitTextByHighlightRanges', () => {
  it('splits clause text by paragraph-level char offsets', () => {
    const parts = splitTextByHighlightRanges('朋友。我想', 10, [{ charStart: 12, charEnd: 15 }]);
    expect(parts).toEqual([
      { text: '朋友', highlighted: false },
      { text: '。我想', highlighted: true },
    ]);
  });

  it('merges duplicate overlapping ranges without duplicating text', () => {
    const duplicateRanges = [
      { charStart: 0, charEnd: 2 },
      { charStart: 0, charEnd: 2 },
      { charStart: 0, charEnd: 2 },
    ];
    const parts = splitTextByHighlightRanges('探寻未来十年', 0, duplicateRanges);

    expect(parts.map((part) => part.text).join('')).toBe('探寻未来十年');
    expect(parts).toEqual([
      { text: '探寻', highlighted: true },
      { text: '未来十年', highlighted: false },
    ]);
  });
});

describe('buildCopySegmentsFromAiSegment', () => {
  it('adds full transcript paragraphs that overlap AI time range', () => {
    const paragraphText =
      '前面的内容。我建议你先去做一个市场调研，你真的跑了20多个宠物店，然后弄出了一份挺漂亮的可行性报告。后面的内容。';
    const paragraph: TranscriptParagraph = {
      id: 'ai-p1',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 'ai-s1',
          start: 700,
          end: 780,
          text: paragraphText,
          words: [
            { start: 700, end: 710, text: '前面的内容' },
            { start: 710, end: 730, text: '我建议你先去做一个市场调研' },
            { start: 730, end: 750, text: '你真的跑了20多个宠物店' },
            { start: 750, end: 770, text: '然后弄出了一份挺漂亮的可行性报告' },
            { start: 770, end: 780, text: '后面的内容' },
          ],
        },
      ],
    };
    const normalized = normalizeTranscriptParagraphs([paragraph])[0]!;

    const aiSegment = {
      id: 'ai-seg-1',
      title: '二十八岁信',
      start: 728,
      end: 768,
    };

    const fullParagraph = paragraphToCopySegment(paragraph);
    const fromAi = buildCopySegmentsFromAiSegment([paragraph], aiSegment);

    expect(fromAi).toHaveLength(1);
    expect(fullParagraph).not.toBeNull();
    expect(fromAi[0]!.text).toBe(fullParagraph!.text);
    expect(fromAi[0]!.text).toBe(paragraphText);
    expect(fromAi[0]!.sourceParagraphId).toBe('ai-p1');

    const fromNormalizedAi = buildCopySegmentsFromAiSegment([normalized], aiSegment);
    expect(fromNormalizedAi).toHaveLength(1);
    expect(fromNormalizedAi[0]!.text).toBe(getParagraphText(normalized));
  });

  it('keeps full paragraph text when trailing words fail to align to source text', () => {
    const paragraphText = '开头段落。中间段落。结尾在对齐时容易丢失的句子。';
    const paragraph: TranscriptParagraph = {
      id: 'tail-p1',
      speaker: '1',
      speakerName: '说话人1',
      segments: [
        {
          id: 'tail-s1',
          start: 100,
          end: 200,
          text: paragraphText,
          words: [
            { start: 100, end: 120, text: '开头段落' },
            { start: 120, end: 140, text: '中间段落' },
          ],
        },
      ],
    };

    const segments = buildCopySegmentsFromAiSegment([paragraph], {
      id: 'ai-tail',
      title: '测试',
      start: 100,
      end: 200,
    });

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe(paragraphText);
  });

  it('returns one segment per transcript paragraph with matching sourceParagraphId', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'tp-1',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 'ts-1',
            start: 700,
            end: 720,
            text: '第一段完整内容。',
            words: [{ start: 700, end: 720, text: '第一段完整内容' }],
          },
        ],
      },
      {
        id: 'tp-2',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 'ts-2',
            start: 721,
            end: 740,
            text: '第二段完整内容。',
            words: [{ start: 721, end: 740, text: '第二段完整内容' }],
          },
        ],
      },
      {
        id: 'tp-3',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 'ts-3',
            start: 741,
            end: 760,
            text: '第三段完整内容。',
            words: [{ start: 741, end: 760, text: '第三段完整内容' }],
          },
        ],
      },
    ];

    const segments = buildCopySegmentsFromAiSegment(paragraphs, {
      id: 'ai-range',
      title: '测试',
      start: 710,
      end: 750,
    });

    expect(segments).toHaveLength(3);
    expect(segments[0]!.sourceParagraphId).toBe('tp-1');
    expect(segments[1]!.sourceParagraphId).toBe('tp-2');
    expect(segments[2]!.sourceParagraphId).toBe('tp-3');
    expect(segments[0]!.text).toBe('第一段完整内容。');
    expect(segments[1]!.text).toBe('第二段完整内容。');
    expect(segments[2]!.text).toBe('第三段完整内容。');
    expect(new Set(segments.map((item) => item.sourceParagraphId)).size).toBe(segments.length);
  });
});

describe('findActiveSegment', () => {
  const paragraphs: TranscriptParagraph[] = [
    {
      id: 'p1',
      speaker: '1',
      speakerName: '说话人1',
      segments: [{ id: 's1', start: 0, end: 6962.3, text: '开头段落' }],
    },
    {
      id: 'p2',
      speaker: '1',
      speakerName: '说话人1',
      segments: [{ id: 's2', start: 2218.6, end: 6962.3, text: '中段段落' }],
    },
    {
      id: 'p3',
      speaker: '2',
      speakerName: '说话人2',
      segments: [{ id: 's3', start: 2241.1, end: 6962.3, text: '后段段落' }],
    },
  ];

  it('prefers the latest-start segment when ASR ranges overlap', () => {
    expect(findActiveSegment(paragraphs, 37 * 60 + 2)).toEqual({
      paragraphId: 'p2',
      segmentId: 's2',
    });
  });

  it('returns the next paragraph once playback passes its start', () => {
    expect(findActiveSegment(paragraphs, 37 * 60 + 21.1)).toEqual({
      paragraphId: 'p3',
      segmentId: 's3',
    });
  });

  it('returns the earliest paragraph before later ones start', () => {
    expect(findActiveSegment(paragraphs, 10)).toEqual({
      paragraphId: 'p1',
      segmentId: 's1',
    });
  });

  it('does not jump to earlier paragraph during gaps between clause segments', () => {
    const overlappingParagraphs: TranscriptParagraph[] = [
      {
        id: 'p-top',
        speaker: '3',
        speakerName: '说话人3',
        segments: [{ id: 'top-last', start: 3139, end: 6962.3, text: '上面段落' }],
      },
      {
        id: 'p-bottom',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          { id: 'b1', start: 3175.9, end: 3184, text: '下面第一句' },
          { id: 'b2', start: 3185, end: 3200, text: '下面第二句' },
        ],
      },
    ];

    expect(findActiveSegment(overlappingParagraphs, 3184.5)).toEqual({
      paragraphId: 'p-bottom',
      segmentId: 'b1',
    });
    expect(findActiveSegment(overlappingParagraphs, 3185)).toEqual({
      paragraphId: 'p-bottom',
      segmentId: 'b2',
    });
  });

  it('uses word timing within paragraph when clause starts are misordered', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 'c1',
            start: 458,
            end: 465,
            text: '第一句。',
            words: [
              { start: 458, end: 460, text: '第一' },
              { start: 460, end: 462, text: '句' },
            ],
          },
          {
            id: 'c2',
            start: 459,
            end: 468,
            text: '第二句。',
            words: [
              { start: 462.5, end: 464, text: '第二' },
              { start: 464, end: 466, text: '句' },
            ],
          },
          {
            id: 'c3',
            start: 457,
            end: 472,
            text: '第三句。',
            words: [
              { start: 466.5, end: 468, text: '第三' },
              { start: 468, end: 470, text: '句' },
            ],
          },
        ],
      },
    ];

    expect(findActiveSegment(paragraphs, 461)).toEqual({
      paragraphId: 'p1',
      segmentId: 'c1',
    });
    expect(findActiveSegment(paragraphs, 463)).toEqual({
      paragraphId: 'p1',
      segmentId: 'c2',
    });
    expect(findActiveSegment(paragraphs, 467)).toEqual({
      paragraphId: 'p1',
      segmentId: 'c3',
    });
  });

  it('uses backend word times for clause segments after normalize', () => {
    const text = 'UBC，哥伦比亚大学，多伦多大学还是 Queen?';
    const paragraphs: TranscriptParagraph[] = normalizeTranscriptParagraphs([
      {
        id: 'p-ubc',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 's-ubc',
            start: 266.3,
            end: 270.5,
            text,
            words: [
              { start: 266.3, end: 268.0, text: '多伦多大学' },
              { start: 270.3, end: 270.5, text: 'Queen' },
            ],
          },
        ],
      },
    ]);

    const ubcClause = paragraphs[0]!.segments.find((segment) => segment.text.startsWith('UBC'));
    const torontoClause = paragraphs[0]!.segments.find((segment) =>
      segment.text.startsWith('多伦多大学')
    );
    expect(ubcClause).toBeDefined();
    expect(torontoClause).toBeDefined();
    expect(torontoClause!.start).toBe(266.3);

    expect(findActiveSegment(paragraphs, 266.5)).toEqual({
      paragraphId: 'p-ubc',
      segmentId: torontoClause!.id,
    });
  });

  it('aligns clause segment start with words after normalize for click seek', () => {
    const text =
      '今天的主题叫做打造个人IP的。今天我们继续职业篇的讲授，我是吴晓波。';
    const paragraphs = normalizeTranscriptParagraphs([
      {
        id: 'p-ip',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 's-ip',
            start: 0.8,
            end: 38.9,
            text,
            words: [
              { start: 0.8, end: 8.5, text: '今天的主题叫做打造个人IP的' },
              { start: 18.2, end: 26.0, text: '今天我们继续职业篇的讲授' },
              { start: 26.0, end: 30.0, text: '我是吴晓波' },
            ],
          },
        ],
      },
    ]);

    const secondClause = paragraphs[0]!.segments.find((segment) =>
      segment.text.startsWith('今天我们继续')
    );
    expect(secondClause).toBeDefined();
    expect(secondClause!.start).toBeGreaterThan(17);
    expect(findActiveSegment(paragraphs, 20)).toEqual({
      paragraphId: 'p-ip',
      segmentId: secondClause!.id,
    });
  });
});

describe('getAdjacentSentenceSeekTime', () => {
  const paragraphs: TranscriptParagraph[] = [
    {
      id: 'p-bottom',
      speaker: '3',
      speakerName: '说话人3',
      segments: [
        { id: 'b1', start: 10, end: 20, text: '第一句' },
        { id: 'b2', start: 21, end: 30, text: '第二句' },
        { id: 'b3', start: 31, end: 40, text: '第三句' },
      ],
    },
  ];

  it('seeks to the previous sentence start', () => {
    expect(getAdjacentSentenceSeekTime(paragraphs, 25, 'prev')).toBe(10);
    expect(getAdjacentSentenceSeekTime(paragraphs, 21, 'prev')).toBe(10);
  });

  it('seeks to the next sentence start', () => {
    expect(getAdjacentSentenceSeekTime(paragraphs, 15, 'next')).toBe(21);
    expect(getAdjacentSentenceSeekTime(paragraphs, 25, 'next')).toBe(31);
  });

  it('returns null at boundaries', () => {
    expect(getAdjacentSentenceSeekTime(paragraphs, 10, 'prev')).toBeNull();
    expect(getAdjacentSentenceSeekTime(paragraphs, 35, 'next')).toBeNull();
  });
});

describe('getTranscriptPadBounds', () => {
  it('caps rear padding before the next spoken unit when ASR segments overlap', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 2728,
            end: 6962.3,
            text: '我们今天的这个直播分享会分成四个部分',
          },
          {
            id: 's2',
            start: 2730,
            end: 6962.3,
            text: '下一句话',
          },
        ],
      },
    ];

    expect(getTranscriptPadBounds(paragraphs, 2728, 2732.5, 3600)).toEqual({
      lowerBound: 0,
      upperBound: 2732.5,
    });
  });

  it('uses word timings to stop rear padding before the next word', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 2728,
            end: 6962.3,
            text: '我们今天的这个直播分享会分成四个部分',
            words: [{ start: 2728, end: 2732.5, text: '我们今天的这个直播分享会分成四个部分' }],
          },
          {
            id: 's2',
            start: 2730,
            end: 6962.3,
            text: '下一句话',
            words: [{ start: 2732.8, end: 2740, text: '下一句话' }],
          },
        ],
      },
    ];

    expect(getTranscriptPadBounds(paragraphs, 2728, 2732.5, 3600)).toEqual({
      lowerBound: 0,
      upperBound: 2732.5 + (2732.8 - 2732.5 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC),
    });
  });

  it('detects next clause start even when only the current sentence has word timings', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 2728,
            end: 6962.3,
            text: '我们今天的这个直播分享会分成四个部分',
            words: [{ start: 2728, end: 2732.5, text: '我们今天的这个直播分享会分成四个部分' }],
          },
          {
            id: 's2',
            start: 2733.1,
            end: 6962.3,
            text: '下一句话',
          },
        ],
      },
    ];

    expect(getTranscriptPadBounds(paragraphs, 2728, 2732.5, 3600)).toEqual({
      lowerBound: 0,
      upperBound: 2732.5 + (2733.1 - 2732.5 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC),
    });
  });

  it('limits rear expand for the live-sharing sentence scenario', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 2728.2,
            end: 6962.3,
            text: '呃，我们今天的这个直播分享会分成四个部分。',
            words: [{ start: 2728.2, end: 2732.1, text: '呃我们今天的这个直播分享会分成四个部分' }],
          },
          {
            id: 's2',
            start: 2730,
            end: 6962.3,
            text: '下一句话',
            words: [{ start: 2732.5, end: 2740, text: '下一句话' }],
          },
        ],
      },
    ];

    const { upperBound } = getTranscriptPadBounds(paragraphs, 2728.2, 2732.1, 3600);
    expect(upperBound).toBeCloseTo(2732.1 + (2732.5 - 2732.1 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC), 5);
    expect(upperBound).toBeLessThan(2732.4);
  });

  it('clamps over-expanded rear padding to the safe upper bound', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 2728,
            end: 6962.3,
            text: '我们今天的这个直播分享会分成四个部分',
            words: [{ start: 2728, end: 2732.5, text: '我们今天的这个直播分享会分成四个部分' }],
          },
          {
            id: 's2',
            start: 2730,
            end: 6962.3,
            text: '下一句话',
            words: [{ start: 2732.8, end: 2740, text: '下一句话' }],
          },
        ],
      },
    ];

    const clamped = clampCopySegmentPlaybackBounds(
      {
        id: 'copy-1',
        speaker: '3',
        speakerName: '说话人3',
        text: '我们今天的这个直播分享会分成四个部分',
        start: 2728,
        end: 2732.9,
        originStart: 2728,
        originEnd: 2732.5,
      },
      paragraphs,
      3600
    );

    expect(clamped.end).toBeCloseTo(
      2732.5 + (2732.8 - 2732.5 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC),
      5
    );
  });

  it('limits rear expand to the gap before the next clause', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p-bottom',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          { id: 'b1', start: 3175.9, end: 3184, text: '下面第一句' },
          { id: 'b2', start: 3185, end: 3200, text: '下面第二句' },
        ],
      },
    ];

    expect(getTranscriptPadBounds(paragraphs, 3175.9, 3184, 3600)).toEqual({
      lowerBound: 0,
      upperBound: 3184 + (3185 - 3184 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC),
    });

    const segment = {
      id: 'copy-1',
      speaker: '3',
      speakerName: '说话人3',
      text: '下面第一句',
      start: 3175.9,
      end: 3184.4,
      originStart: 3175.9,
      originEnd: 3184,
    };

    expect(
      getSegmentAdjustableSeconds([segment], 0, 'end', 'expand', 3600, paragraphs)
    ).toBeCloseTo(3184 + (3185 - 3184 - TRANSCRIPT_BACK_PAD_SAFETY_GAP_SEC) - 3184.4, 5);
  });

  it('does not auto-extend rear padding for short word selections', () => {
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p-short',
        speaker: '1',
        speakerName: '说话人1',
        segments: [
          {
            id: 's-short',
            start: 16.6,
            end: 17.2,
            text: '前面的内容财富后面的内容',
            words: [
              { start: 16.6, end: 16.9, text: '前面的内容' },
              { start: 16.9, end: 17.0, text: '财富' },
              { start: 17.0, end: 17.2, text: '后面的内容' },
            ],
          },
        ],
      },
    ];

    const segment = buildCopySegmentFromParagraphRange(
      paragraphs[0]!,
      paragraphs[0]!.segments[0]!.text.indexOf('财富'),
      paragraphs[0]!.segments[0]!.text.indexOf('财富') + '财富'.length
    );
    expect(segment).not.toBeNull();

    const clamped = clampCopySegmentPlaybackBounds(segment!, paragraphs, 3600);
    expect(clamped.end).toBeCloseTo(clamped.originEnd!, 5);
    expect(getSegmentBackPadSeconds(clamped)).toBeCloseTo(0, 5);
  });
});

describe('getSegmentPlaybackStopTime', () => {
  it('stops playback slightly before segment.end', () => {
    expect(
      getSegmentPlaybackStopTime({
        id: 'copy-1',
        speaker: '1',
        speakerName: '说话人1',
        text: '测试',
        start: 10,
        end: 20,
      })
    ).toBeCloseTo(20 - SEGMENT_PLAYBACK_END_GUARD_SEC, 5);
  });
});

describe('deleteSelectedRangeFromSegment', () => {
  it('uses word-level timings when deleting leading text', () => {
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text: 'AAAA BBBB',
      start: 100,
      end: 200,
      originStart: 100,
      originEnd: 200,
    };
    const words = [
      { start: 100, end: 120, text: 'AAAA' },
      { start: 150, end: 180, text: 'BBBB' },
    ];

    const segments = expectDeleteSegments(deleteSelectedRangeFromSegment(segment, 0, 5, words));

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('BBBB');
    expect(segments[0]!.start).toBe(150);
  });

  it('falls back to linear interpolation when words are unavailable', () => {
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text: 'ABCDEF',
      start: 100,
      end: 160,
      originStart: 100,
      originEnd: 160,
    };

    const segments = expectDeleteSegments(deleteSelectedRangeFromSegment(segment, 0, 3, []));

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('DEF');
    expect(segments[0]!.start).toBe(130);
  });

  it('uses word-level end time when deleting middle text with punctuation', () => {
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: '投到了AI里面。来，我们XXXX做个调查，你认为巴菲特对的扣个一',
      start: 3314.9,
      end: 3349.2,
      originStart: 3314.9,
      originEnd: 3349.2,
    };
    const words = [
      { start: 3314.9, end: 3315.5, text: '投' },
      { start: 3315.5, end: 3316.2, text: '到' },
      { start: 3316.2, end: 3316.8, text: '了' },
      { start: 3316.8, end: 3317.6, text: 'AI' },
      { start: 3317.6, end: 3318.4, text: '里面' },
      { start: 3328.0, end: 3328.6, text: '来' },
      { start: 3328.6, end: 3329.4, text: '我们' },
      { start: 3330.0, end: 3331.0, text: 'X' },
      { start: 3331.0, end: 3332.0, text: 'X' },
      { start: 3332.0, end: 3333.0, text: 'X' },
      { start: 3333.0, end: 3334.0, text: 'X' },
      { start: 3338.6, end: 3339.2, text: '做' },
      { start: 3339.2, end: 3339.8, text: '个' },
      { start: 3339.8, end: 3340.6, text: '调查' },
    ];

    const deleteFrom = segment.text.indexOf('XXXX');
    const deleteTo = deleteFrom + 'XXXX'.length;
    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, deleteTo, words)
    );

    expect(segments).toHaveLength(2);
    expect(segments[0]!.text.endsWith('来，我们')).toBe(true);
    expect(segments[0]!.end).toBeCloseTo(3329.4, 1);
    expect(segments[1]!.text.startsWith('做个调查')).toBe(true);
    expect(segments[1]!.start).toBeCloseTo(3338.6, 1);
  });

  it('uses word-level end time when deleting trailing text with spaced ASR tokens', () => {
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: '等到他人生的最后一次猎取，把所有的钱已经投到了 AI 里面。来，我们做个调查，你认为巴菲特对的扣个一',
      start: 3314.9,
      end: 3349.2,
      originStart: 3314.9,
      originEnd: 3349.2,
    };
    const words = [
      { start: 3314.9, end: 3315.4, text: '等到' },
      { start: 3315.4, end: 3316.0, text: '他人' },
      { start: 3316.0, end: 3316.6, text: '生的' },
      { start: 3325.0, end: 3325.8, text: 'AI' },
      { start: 3325.8, end: 3326.6, text: '里面' },
      { start: 3328.0, end: 3328.6, text: '来' },
      { start: 3328.6, end: 3329.4, text: '我们' },
      { start: 3338.6, end: 3339.2, text: '做' },
      { start: 3339.2, end: 3339.8, text: '个' },
      { start: 3339.8, end: 3340.6, text: '调查' },
    ];

    const deleteFrom = segment.text.indexOf('做个调查');
    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, segment.text.length, words)
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text.endsWith('来，我们')).toBe(true);
    expect(segments[0]!.end).toBeCloseTo(3329.4, 1);
    expect(segments[0]!.end).toBeGreaterThan(3328);
  });

  it('aligns partial-delete boundaries via paragraph text when copy text has extra spaces', () => {
    const paragraphText =
      '等到他人生的最后一次猎取，把所有的钱已经投到了AI里面。来，我们做个调查，你认为巴菲特对的扣个一';
    const segmentText =
      '等到他人生的最后一次猎取，把所有的钱已经投到了 AI 里面。来，我们做个调查，你认为巴菲特对的扣个一';
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 3314.9,
            end: 3349.2,
            text: paragraphText,
            words: [
              { start: 3314.9, end: 3317.6, text: '等到他人生的最后一次猎取' },
              { start: 3320.0, end: 3325.8, text: '把所有的钱已经投到了' },
              { start: 3325.8, end: 3326.6, text: 'AI' },
              { start: 3326.6, end: 3327.4, text: '里面' },
              { start: 3328.0, end: 3328.6, text: '来' },
              { start: 3328.6, end: 3329.4, text: '我们' },
              { start: 3338.6, end: 3339.2, text: '做' },
              { start: 3339.2, end: 3340.6, text: '个调查' },
            ],
          },
        ],
      },
    ];
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: segmentText,
      start: 3314.9,
      end: 3349.2,
      originStart: 3314.9,
      originEnd: 3349.2,
    };

    const deleteFrom = segmentText.indexOf('做个调查');
    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, segmentText.length, [], paragraphs)
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text.endsWith('来，我们')).toBe(true);
    expect(segments[0]!.end).toBeCloseTo(3329.4, 1);
    expect(segments[0]!.end).not.toBeCloseTo(3334.8, 0);
  });

  it('extends end time to last matched word when pauses skew char-to-time ratio', () => {
    const paragraphText =
      `${'等到他人生的最后一次猎取，'.repeat(3)}把所有的钱已经投到了AI里面。来，我们做个调查`;
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: paragraphText,
      start: 3314.9,
      end: 3349.2,
      originStart: 3314.9,
      originEnd: 3349.2,
    };
    const paragraphs: TranscriptParagraph[] = [
      {
        id: 'p1',
        speaker: '3',
        speakerName: '说话人3',
        segments: [
          {
            id: 's1',
            start: 3314.9,
            end: 3349.2,
            text: paragraphText,
            words: [
              { start: 3314.9, end: 3320.0, text: '等到他人生的最后一次猎取' },
              { start: 3320.0, end: 3334.8, text: '把所有的钱已经投到了AI里面' },
              { start: 3338.6, end: 3339.2, text: '来' },
              { start: 3339.2, end: 3340.0, text: '我们' },
              { start: 3340.6, end: 3341.2, text: '做' },
            ],
          },
        ],
      },
    ];

    const deleteFrom = paragraphText.indexOf('做个调查');
    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, paragraphText.length, [], paragraphs)
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text.endsWith('来，我们')).toBe(true);
    expect(segments[0]!.end).toBeGreaterThan(3336);
    expect(segments[0]!.end).toBeCloseTo(3340.0, 0);
    expect(segments[0]!.end).not.toBeCloseTo(3334.8, 0);
  });

  it('interpolates timing when partial delete splits inside an ASR word', () => {
    const text = '前文。我作为一个父亲，有的时候很难用语言来表达。';
    const deleteFrom = text.indexOf('为一个父亲');
    const deleteTo = deleteFrom + '为一个父亲，有的时候'.length;
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text,
      start: 57.5,
      end: 101.8,
      originStart: 57.5,
      originEnd: 101.8,
    };
    const words = [
      { start: 57.5, end: 80.0, text: '前文' },
      { start: 80.0, end: 98.9, text: '我作为一个父亲，有的时候' },
      { start: 98.9, end: 101.8, text: '很难用语言来表达' },
    ];

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, deleteTo, words)
    );

    expect(segments).toHaveLength(2);
    expect(segments[0]!.text.endsWith('我作')).toBe(true);
    expect(segments[0]!.end).toBeGreaterThan(82);
    expect(segments[0]!.end).toBeLessThan(98.9);
    expect(segments[1]!.text.startsWith('很难')).toBe(true);
    expect(segments[1]!.start).toBeCloseTo(98.9, 1);
    expect(segments[0]!.end).toBeLessThan(segments[1]!.start);
  });

  it('keeps word-level timing when deleting punctuation only in the middle', () => {
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text: '来，我们做个调查',
      start: 3328.0,
      end: 3340.6,
      originStart: 3328.0,
      originEnd: 3340.6,
    };
    const commaIndex = segment.text.indexOf('，');

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, commaIndex, commaIndex + 1, [
        { start: 3328.0, end: 3328.6, text: '来' },
        { start: 3328.6, end: 3329.4, text: '我们' },
        { start: 3338.6, end: 3340.6, text: '做个调查' },
      ])
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('来我们做个调查');
    expect(segments[0]!.start).toBe(3328.0);
    expect(segments[0]!.end).toBe(3340.6);
    expect(segments[0]!.originStart).toBe(3328.0);
    expect(segments[0]!.originEnd).toBe(3340.6);
  });

  it('keeps word-level timing when deleting trailing punctuation only', () => {
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text: '做个调查。',
      start: 3338.6,
      end: 3340.6,
      originStart: 3338.6,
      originEnd: 3340.6,
    };

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, 4, 5, [
        { start: 3338.6, end: 3340.6, text: '做个调查' },
      ])
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('做个调查');
    expect(segments[0]!.start).toBe(3338.6);
    expect(segments[0]!.end).toBe(3340.6);
  });

  it('allows middle delete when spoken words end before segment originStart', () => {
    const text =
      'AI泡沫的破灭，一个把所有的钱已经投到了AI里面。来我们做个直播间里做个调查，你认为巴菲特对的扣个一';
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text,
      start: 3333.3,
      end: 3347.5,
      originStart: 3333.3,
      originEnd: 3347.5,
    };
    const words = [
      { start: 3328.0, end: 3328.6, text: '来' },
      { start: 3328.6, end: 3329.4, text: '我们' },
      { start: 3338.6, end: 3339.2, text: '做' },
      { start: 3339.2, end: 3340.6, text: '个调查' },
    ];
    const deleteFrom = text.indexOf('做个直播间里');

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(
        segment,
        deleteFrom,
        deleteFrom + '做个直播间里'.length,
        words
      )
    );

    expect(segments).toHaveLength(2);
    expect(segments[0]!.text.endsWith('来我们')).toBe(true);
    expect(segments[1]!.text.startsWith('做个调查')).toBe(true);
  });

  it('still splits segment when deleting spoken text rather than punctuation', () => {
    const segment = {
      id: 'seg',
      speaker: '1',
      speakerName: '说话人1',
      text: '来，我们做个调查',
      start: 3328.0,
      end: 3340.6,
      originStart: 3328.0,
      originEnd: 3340.6,
    };
    const deleteFrom = segment.text.indexOf('我们');

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, deleteFrom + 2, [
        { start: 3328.0, end: 3328.6, text: '来' },
        { start: 3328.6, end: 3329.4, text: '我们' },
        { start: 3338.6, end: 3340.6, text: '做个调查' },
      ])
    );

    expect(segments.length).toBeGreaterThan(1);
  });

  it('does not keep a punctuation-only trailing piece after partial delete', () => {
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: '新能源汽车跌了14%，手机的销量跌了10%。',
      start: 3497.7,
      end: 3506.6,
      originStart: 3497.7,
      originEnd: 3506.6,
    };
    const words = [
      { start: 3497.7, end: 3499.0, text: '新能源' },
      { start: 3499.0, end: 3500.2, text: '汽车' },
      { start: 3500.2, end: 3501.0, text: '跌' },
      { start: 3501.0, end: 3502.0, text: '了' },
      { start: 3502.0, end: 3503.0, text: '14%' },
      { start: 3503.5, end: 3504.5, text: '手机' },
      { start: 3504.5, end: 3505.5, text: '销量' },
      { start: 3505.5, end: 3506.6, text: '跌' },
    ];
    const deleteFrom = segment.text.indexOf('手机的销量跌了10%');
    const deleteTo = deleteFrom + '手机的销量跌了10%'.length;

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, deleteFrom, deleteTo, words)
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('新能源汽车跌了14%，');
    expect(segments[0]!.end).toBeLessThan(3506.6);
  });

  it('deletes segment when partial delete leaves only punctuation', () => {
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: '新能源汽车跌了14%，手机的销量跌了10%。',
      start: 3497.7,
      end: 3506.6,
      originStart: 3497.7,
      originEnd: 3506.6,
    };

    expect(
      deleteSelectedRangeFromSegment(
        segment,
        0,
        segment.text.length - 1,
        [],
        []
      )
    ).toBe('delete-all');
  });

  it('drops punctuation-only leading piece after partial delete', () => {
    const segment = {
      id: 'seg',
      speaker: '3',
      speakerName: '说话人3',
      text: '，手机的销量跌了10%',
      start: 3503.5,
      end: 3506.6,
      originStart: 3503.5,
      originEnd: 3506.6,
    };
    const commaLen = 1;

    const segments = expectDeleteSegments(
      deleteSelectedRangeFromSegment(segment, 0, commaLen, [
        { start: 3503.5, end: 3504.5, text: '手机' },
        { start: 3504.5, end: 3505.5, text: '销量' },
        { start: 3505.5, end: 3506.6, text: '跌' },
      ])
    );

    expect(segments).toHaveLength(1);
    expect(segments[0]!.text).toBe('手机的销量跌了10%');
    expect(segments[0]!.start).toBeCloseTo(3503.5, 1);
  });
});
